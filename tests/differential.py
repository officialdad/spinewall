#!/usr/bin/env python3
"""Differential test: run the Python server and the Go server on one state copy.

Both servers read the same COPY of a state directory and the same model
directory, on two spare ports. The script compares decoded JSON for the read
endpoints and for a scripted run of every /cmd/*, so a behavior drift between
the two implementations shows up as a real diff.

Usage:
    python3 tests/differential.py --go-bin /path/to/spinewall \
        --state-dir ~/.local/state/spinewall \
        --models-dir ~/.local/share/spinewall/models \
        [--python-server server/spinewall-server.py]

The live server on 8777 is never touched. Both servers use throwaway ports and a
throwaway copy of the state files. The models directory is read only.
"""

import argparse
import json
import os
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.parse
import urllib.request

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

# Every /cmd/* from bin/spinewall and the server's command table, with an
# argument where one applies. The run is scripted, so both servers see the same
# sequence and must end in the same state.
COMMANDS = [
    ("zoom-in", []),
    ("zoom-out", []),
    ("move-left", []),
    ("move-right", []),
    ("move-up", []),
    ("move-down", []),
    ("reset-view", []),
    ("next-model", []),
    ("prev-model", []),
    ("set-model", ["alpha"]),
    ("set-model", ["does-not-exist"]),
    ("set-anim", ["walk fast #1"]),
    ("set-anim", ["x" * 250]),
    ("add-character", ["gamma"]),
    ("add-character", []),
    ("auto-cycle", ["on"]),
    ("auto-cycle", ["off"]),
    ("save-preset", ["differential a/b"]),
    ("update-preset", ["differential a/b"]),
    ("rename-preset", ["differential a/b", "renamed name"]),
    ("load-preset", ["renamed name"]),
    ("next-preset", []),
    ("prev-preset", []),
    ("delete-preset", ["renamed name"]),
    ("clear-characters", []),
    ("add-character", ["beta"]),
    ("remove-character", ["uid-that-does-not-exist"]),
    ("next-char-anim", ["uid-that-does-not-exist"]),
    ("set-char-anim", ["uid", "wave"]),
    ("set-char-model", ["uid", "gamma"]),
    ("bogus", []),
    ("bogus", ["arg"]),
]

READ_ENDPOINTS = ["/state", "/models", "/prefs", "/presets", "/screens", "/info"]


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


class Server:
    def __init__(self, argv, state_dir, models_dir, port, log_path):
        self.argv = argv
        self.state_dir = state_dir
        self.models_dir = models_dir
        self.port = port
        self.log_path = log_path
        self.proc = None

    def env(self, home):
        return {
            "PATH": os.environ.get("PATH", ""),
            "HOME": home,
            "SPINEWALL_PORT": str(self.port),
            "SPINEWALL_MODELS": self.models_dir,
            "SPINEWALL_STATE": os.path.join(self.state_dir, "state.json"),
            "SPINEWALL_PREFS": os.path.join(self.state_dir, "prefs.json"),
            "SPINEWALL_LAYOUT": os.path.join(self.state_dir, "layout.json"),
            "SPINEWALL_SCREENS": os.path.join(self.state_dir, "screens.json"),
            "SPINEWALL_THUMBS": os.path.join(self.state_dir, "thumbs"),
            "SPINEWALL_CONFIG": os.path.join(self.state_dir, "config.json"),
            # The Go server fetches runtimes on first run. The differential stays
            # offline with an empty temp cache; the API comparison needs no players.
            "SPINEWALL_RUNTIME_DIR": os.path.join(home, "runtime"),
            "SPINEWALL_OFFLINE": "1",
        }

    def start(self, home):
        log = open(self.log_path, "wb")
        self.proc = subprocess.Popen(self.argv, env=self.env(home), stdout=log, stderr=log, cwd=home)
        log.close()
        deadline = time.time() + 30
        while time.time() < deadline:
            if self.proc.poll() is not None:
                break
            try:
                if self.get("/state")[0] == 200:
                    return
            except OSError:
                time.sleep(0.05)
        log_text = open(self.log_path, errors="replace").read() if os.path.exists(self.log_path) else ""
        raise RuntimeError(f"server did not start: {' '.join(self.argv)}\n{log_text}")

    def stop(self):
        if self.proc and self.proc.poll() is None:
            self.proc.terminate()
            try:
                self.proc.wait(10)
            except subprocess.TimeoutExpired:
                self.proc.kill()
                self.proc.wait()
        self.proc = None

    def request(self, path):
        url = f"http://127.0.0.1:{self.port}{path}"
        try:
            with urllib.request.urlopen(url, timeout=30) as resp:
                return resp.status, resp.read().decode("utf-8", "replace")
        except urllib.error.HTTPError as e:
            return e.code, e.read().decode("utf-8", "replace")

    def get(self, path):
        return self.request(path)


def decode(text):
    """Decode a JSON reply, or note the raw text when it is not JSON so a diff shows it."""
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        return {"__not_json__": text}


def command_path(name, args):
    quoted = "/".join(urllib.parse.quote(a, safe="") for a in args)
    return f"/cmd/{name}" + (f"/{quoted}" if args else "")


def normalize(value):
    """Normalize the parts of a reply that are allowed to differ between servers.

    - Numbers: Python prints 1.0 and Go prints 1, and JS reads both. Compare them
      as numbers.
    - `uid`: a character uid is random per run, so the values differ while the
      shape is equal. The placeholder keeps the count and order checkable.
    - `skipped` reasons: a parser error message is diagnostic prose. The Go and
      Python JSON parsers word it differently, so only the leading type compares.
    """
    return _normalize(value, None)


def _normalize(value, key):
    if isinstance(value, dict):
        out = {}
        for k, v in value.items():
            if k == "uid" and isinstance(v, str):
                out[k] = "<uid>"
            elif k == "skipped" and isinstance(v, dict):
                out[k] = {name: _normalize_reason(reason) for name, reason in v.items()}
            else:
                out[k] = _normalize(v, k)
        return out
    if isinstance(value, list):
        return [_normalize(v, key) for v in value]
    if isinstance(value, bool):
        return value
    if isinstance(value, (int, float)):
        return float(value)
    return value


def _normalize_reason(reason):
    if not isinstance(reason, str):
        return reason
    # Only the error type is stable across the two JSON parsers.
    if reason.startswith("JSONDecodeError:"):
        return "JSONDecodeError:"
    return reason


def deep_diff(py, go, path=""):
    """Return the dotted paths where two normalized decoded JSON values differ."""
    if type(py) is not type(go):
        return [f"{path}: type {type(py).__name__} != {type(go).__name__}"]
    if isinstance(py, dict):
        diffs = []
        for key in sorted(set(py) | set(go)):
            if key not in py:
                diffs.append(f"{path}.{key}: missing in python")
            elif key not in go:
                diffs.append(f"{path}.{key}: missing in go")
            else:
                diffs += deep_diff(py[key], go[key], f"{path}.{key}")
        return diffs
    if isinstance(py, list):
        diffs = []
        if len(py) != len(go):
            diffs.append(f"{path}: len {len(py)} != {len(go)}")
        for i in range(min(len(py), len(go))):
            diffs += deep_diff(py[i], go[i], f"{path}[{i}]")
        return diffs
    if py != go:
        diffs.append(f"{path}: {py!r} != {go!r}")
    return diffs


def compare(label, py, go, failures):
    """Compare two decoded replies after normalization."""
    py_norm = normalize(py)
    go_norm = normalize(go)
    if py_norm == go_norm:
        return
    if isinstance(py_norm, (dict, list)) and isinstance(go_norm, (dict, list)):
        detail = deep_diff(py_norm, go_norm)
    else:
        detail = [f"{py_norm!r} != {go_norm!r}"]
    failures.append((label, detail))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go-bin", required=True, help="path to the Go spinewall binary")
    parser.add_argument("--state-dir", required=True, help="the live state directory to copy")
    parser.add_argument("--models-dir", required=True, help="the models directory, read only")
    parser.add_argument("--python-server", default=os.path.join(ROOT, "server", "spinewall-server.py"))
    parser.add_argument("--python", default=sys.executable)
    options = parser.parse_args()

    if not os.path.isdir(options.models_dir):
        sys.exit(f"error: models directory {options.models_dir} does not exist")

    work = tempfile.mkdtemp(prefix="spinewall-diff-")
    py_state = os.path.join(work, "py-state")
    go_state = os.path.join(work, "go-state")
    shutil.copytree(options.state_dir, py_state)
    shutil.copytree(options.state_dir, go_state)
    py_home = os.path.join(work, "py-home")
    go_home = os.path.join(work, "go-home")
    for home in (py_home, go_home):
        os.makedirs(home, exist_ok=True)

    py_port = free_port()
    go_port = free_port()
    python_server = Server(
        [options.python, "-I", "-B", options.python_server],
        py_state, options.models_dir, py_port, os.path.join(work, "python.log"),
    )
    go_server = Server([os.path.abspath(options.go_bin), "serve"], go_state, options.models_dir, go_port, os.path.join(work, "go.log"))

    failures = []
    try:
        python_server.start(py_home)
        go_server.start(go_home)

        for path in READ_ENDPOINTS:
            py_status, py_body = python_server.get(path)
            go_status, go_body = go_server.get(path)
            compare(f"status {path}", py_status, go_status, failures)
            compare(f"body {path}", decode(py_body), decode(go_body), failures)

        for name, args in COMMANDS:
            path = command_path(name, args)
            py_status, py_body = python_server.get(path)
            go_status, go_body = go_server.get(path)
            compare(f"status {path}", py_status, go_status, failures)
            if name not in ("random-model", "random-anim"):
                compare(f"body {path}", decode(py_body), decode(go_body), failures)

        for path in READ_ENDPOINTS:
            py_status, py_body = python_server.get(path)
            go_status, go_body = go_server.get(path)
            compare(f"final status {path}", py_status, go_status, failures)
            compare(f"final body {path}", decode(py_body), decode(go_body), failures)
    finally:
        python_server.stop()
        go_server.stop()
        shutil.rmtree(work, True)

    if failures:
        print(f"DIFFERENTIAL FAILED: {len(failures)} difference(s)\n")
        for label, detail in failures:
            print(f"--- {label}")
            for line in detail:
                print(f"    {line}")
        return 1
    print(f"differential ok: {len(READ_ENDPOINTS)} read endpoints and {len(COMMANDS)} commands matched")
    return 0


if __name__ == "__main__":
    sys.exit(main())
