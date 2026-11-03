"""Shared helpers: synthetic models, a live server fixture, and an HTTP client.

The server runs as a subprocess with every SPINEWALL_* path inside a temp directory,
so a test never touches the real home directory or the real port.
"""

import base64
import contextlib
import http.client
import importlib.util
import io
import itertools
import json
import os
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import unittest
import urllib.parse
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SERVER = ROOT / "server" / "spinewall-server.py"
CLI = ROOT / "bin" / "spinewall"

PNG_1X1 = base64.b64decode(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="
)

_counter = itertools.count()


def cli_path():
    # bin/spinewall runs `python3` under a temp HOME. A version manager shim fails there, so the
    # interpreter that runs the tests goes first.
    return os.path.dirname(sys.executable) + os.pathsep + os.environ.get("PATH", "")


def binary_skel(version, layout="4x"):
    """A skeleton header that carries only the version, which is all the server reads."""
    text = version.encode()
    if layout == "4x":
        # 4.x: an 8-byte hash, then the version string (length plus 1, then the text).
        return b"\xff" * 8 + bytes([len(text) + 1]) + text + b"\x00" * 16
    # 3.x: the hash is a string too, so the version string starts after it.
    digest = b"hash1234"
    return bytes([len(digest) + 1]) + digest + bytes([len(text) + 1]) + text + b"\x00" * 16


def atlas_text(regions, page="page.png"):
    lines = [page, "size: 1,1", "filter: Linear,Linear"]
    for name in regions:
        lines += [name, "  bounds: 0,0,1,1"]
    return "\n".join(lines) + "\n"


def make_model(
    models_dir,
    model_id,
    version="4.1.24",
    fmt="skel",
    regions=("body",),
    slots=None,
    model_json=None,
    atlas=True,
    spine_value=None,
    json_root=None,
):
    """Create one model directory. `fmt` is "skel" (4x layout), "skel3" (3.x layout), or "json"."""
    directory = Path(models_dir) / model_id
    directory.mkdir(parents=True, exist_ok=True)
    if fmt == "json":
        value = version if spine_value is None else spine_value
        data = json_root if json_root is not None else {
            "skeleton": {"spine": value},
            "slots": [{"name": s} for s in (slots or [])],
            "bones": [],
        }
        (directory / f"{model_id}.json").write_text(json.dumps(data))
    else:
        layout = "3x" if fmt == "skel3" else "4x"
        (directory / f"{model_id}.skel").write_bytes(binary_skel(version, layout))
    if atlas:
        (directory / f"{model_id}.atlas").write_text(atlas_text(regions))
        (directory / "page.png").write_bytes(PNG_1X1)
    if model_json is not None:
        text = model_json if isinstance(model_json, str) else json.dumps(model_json)
        (directory / "model.json").write_text(text)
    return directory


def build_default_models(models_dir):
    """The model set most API tests share. Sorted ids start with `alpha`, a character."""
    make_model(models_dir, "alpha", "4.0.31")
    make_model(models_dir, "beta", "4.1.56", fmt="json", slots=["body", "head"])
    make_model(models_dir, "gamma", "4.2.120")
    make_model(models_dir, "delta", "3.8.99", fmt="skel3")
    make_model(models_dir, "hero_01", "4.1.56")
    make_model(models_dir, "hero_02", "4.1.56")
    make_model(models_dir, "solo_01", "4.1.56")
    make_model(models_dir, "lobby", "4.1.56", regions=("bg_a", "bg_b", "bg_c", "body"))
    make_model(
        models_dir,
        "named",
        "4.2.120",
        model_json={
            "name": "Pretty Name",
            "group": "Group A",
            "character": "custom-char",
            "kind": "scene",
            "hideSlots": ["bg", 7, ""],
            "skin": "outfit",
            "defaultAnimation": "wave",
        },
    )
    # Models the scan must skip, each for a different reason.
    make_model(models_dir, "old", "3.6.53")
    make_model(models_dir, "legacy37", "3.7.94", fmt="json")
    make_model(models_dir, "legacy37bin", "3.7.94", fmt="skel3")
    make_model(models_dir, "noatlas", "4.1.56", atlas=False)
    make_model(models_dir, "badjson", "4.1.56", model_json="{not json")
    make_model(models_dir, "listjson", "4.1.56", model_json=[])
    make_model(models_dir, "sepbad", "4.1.56", model_json={"skeleton": "../x.skel"})
    make_model(models_dir, "missingfile", "4.1.56", model_json={"atlas": "nope.atlas"})
    make_model(models_dir, "garbage", "4.1.56").joinpath("garbage.skel").write_bytes(b"\x01\x02\x03")


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


class Resp:
    def __init__(self, status, headers, body):
        self.status = status
        self.headers = headers
        self.body = body

    @property
    def json(self):
        return json.loads(self.body)

    def header(self, name):
        return self.headers.get(name)


class ServerProcess:
    def __init__(self, root, models_dir=None, extra_env=None, port=None):
        self.root = Path(root)
        self.models = Path(models_dir) if models_dir else self.root / "models"
        self.home = self.root / "home"
        self.state_dir = self.root / "state"
        self.cache_dir = self.root / "cache"
        self.state_file = self.state_dir / "state.json"
        self.prefs_file = self.state_dir / "prefs.json"
        self.layout_file = self.state_dir / "layout.json"
        self.thumbs = self.cache_dir / "thumbs"
        self.log_file = self.root / "server.log"
        self.extra_env = extra_env or {}
        self.port = port
        self.proc = None
        for d in (self.models, self.home, self.state_dir, self.cache_dir):
            d.mkdir(parents=True, exist_ok=True)

    def env(self):
        env = {
            "PATH": os.environ.get("PATH", ""),
            "HOME": str(self.home),
            "SPINEWALL_PORT": str(self.port),
            "SPINEWALL_MODELS": str(self.models),
            "SPINEWALL_STATE": str(self.state_file),
            "SPINEWALL_PREFS": str(self.prefs_file),
            "SPINEWALL_LAYOUT": str(self.layout_file),
            "SPINEWALL_SCREENS": str(self.state_dir / "screens.json"),
            "SPINEWALL_THUMBS": str(self.thumbs),
        }
        env.update(self.extra_env)
        return env

    def start(self):
        last = ""
        for _ in range(3):
            self.port = free_port()
            log = open(self.log_file, "wb")
            self.proc = subprocess.Popen(
                [sys.executable, "-I", "-B", str(SERVER)],
                env=self.env(),
                stdout=log,
                stderr=log,
                cwd=str(self.root),
            )
            log.close()
            deadline = time.time() + 10
            while time.time() < deadline:
                if self.proc.poll() is not None:
                    break
                try:
                    if self.request("GET", "/state").status == 200:
                        return self
                except OSError:
                    time.sleep(0.03)
            last = self.log_file.read_text(errors="replace")
            self.stop()
            if "Address already in use" not in last:
                break
        raise RuntimeError(f"server did not start:\n{last}")

    def stop(self):
        if self.proc and self.proc.poll() is None:
            self.proc.terminate()
            try:
                self.proc.wait(5)
            except subprocess.TimeoutExpired:
                self.proc.kill()
                self.proc.wait()
        self.proc = None

    def restart(self):
        self.stop()
        return self.start()

    def log(self):
        return self.log_file.read_text(errors="replace") if self.log_file.exists() else ""

    def request(self, method, path, body=None, headers=None, raw=None):
        hdrs = dict(headers or {})
        data = None
        if body is not None:
            data = json.dumps(body).encode()
            hdrs.setdefault("Content-Type", "application/json")
        if raw is not None:
            data = raw
        conn = http.client.HTTPConnection("127.0.0.1", self.port, timeout=10)
        try:
            conn.request(method, path, body=data, headers=hdrs)
            resp = conn.getresponse()
            return Resp(resp.status, {k: v for k, v in resp.getheaders()}, resp.read())
        finally:
            conn.close()


class SharedServerCase(unittest.TestCase):
    """One server for a whole class. Use it only for tests that never change state."""

    models_builder = staticmethod(build_default_models)
    extra_env = None

    @classmethod
    def setUpClass(cls):
        cls.tmp = Path(tempfile.mkdtemp(prefix="spinewall-test-"))
        cls.server = ServerProcess(cls.tmp, extra_env=cls.extra_env)
        try:
            cls.models_builder(cls.server.models)
            cls.server.start()
        except BaseException:
            cls.tearDownClass()
            raise

    @classmethod
    def tearDownClass(cls):
        cls.server.stop()
        shutil.rmtree(cls.tmp, True)

    def get(self, path, **kw):
        return self.server.request("GET", path, **kw)

    def post(self, path, body=None, **kw):
        return self.server.request("POST", path, body=body if body is not None else {}, **kw)


class ServerCase(unittest.TestCase):
    """A fresh server per test, so no test depends on another test's state."""

    models_builder = staticmethod(build_default_models)
    autostart = True
    extra_env = None

    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp(prefix="spinewall-test-"))
        self.addCleanup(shutil.rmtree, self.tmp, True)
        self.server = ServerProcess(self.tmp, extra_env=self.extra_env)
        self.addCleanup(self.server.stop)
        if self.models_builder:
            self.models_builder(self.server.models)
        if self.autostart:
            self.server.start()

    def get(self, path, **kw):
        return self.server.request("GET", path, **kw)

    def post(self, path, body=None, **kw):
        return self.server.request("POST", path, body=body if body is not None else {}, **kw)

    def cmd(self, name, *args):
        quoted = "/".join(urllib.parse.quote(a, safe="") for a in args)
        path = f"/cmd/{name}" + (f"/{quoted}" if args else "")
        resp = self.get(path)
        self.assertEqual(resp.status, 200, resp.body)
        return resp.json

    def state(self):
        return self.get("/state").json

    def model_ids(self):
        return [m["id"] for m in self.get("/models").json]

    def report(self, model, animations, playing=None):
        body = {"model": model, "animations": animations}
        if playing is not None:
            body["playing"] = playing
        return self.post("/report", body)


def load_server_module(models_dir, root):
    """Import the server file as a module with temp paths, for unit tests of its functions."""
    root = Path(root)
    names = {
        "HOME": str(root / "home"),
        "SPINEWALL_MODELS": str(models_dir),
        "SPINEWALL_STATE": str(root / "state" / "state.json"),
        "SPINEWALL_PREFS": str(root / "state" / "prefs.json"),
        "SPINEWALL_LAYOUT": str(root / "state" / "layout.json"),
        "SPINEWALL_SCREENS": str(root / "state" / "screens.json"),
        "SPINEWALL_THUMBS": str(root / "cache" / "thumbs"),
        "SPINEWALL_PORT": "1",
    }
    (root / "home").mkdir(parents=True, exist_ok=True)
    saved = {k: os.environ.get(k) for k in names}
    saved_flag = sys.dont_write_bytecode
    os.environ.update(names)
    # Bytecode would land in server/__pycache__, which belongs to nobody.
    sys.dont_write_bytecode = True
    try:
        spec = importlib.util.spec_from_file_location(f"spinewall_server_{next(_counter)}", SERVER)
        module = importlib.util.module_from_spec(spec)
        # The scan prints one line per skipped model, which is noise in a test run.
        with contextlib.redirect_stderr(io.StringIO()):
            spec.loader.exec_module(module)
    finally:
        sys.dont_write_bytecode = saved_flag
        for key, value in saved.items():
            if value is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = value
    return module
