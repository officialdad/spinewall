"""Static checks: syntax, shellcheck, and nothing in the repo that must never be published."""

import py_compile
import re
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

from tests.scripts.support import ROOT

# Binary or generated paths that the denylist scan skips.
BINARY_SUFFIXES = {".png", ".jpg", ".jpeg", ".gif", ".mp4", ".webp", ".ico", ".woff", ".woff2"}
FORBIDDEN_NAMES = re.compile(r"(\.skel|\.atlas)$|(^|/)spine-player[^/]*\.(js|css)$|(^|/)spine-(webgl|canvas)[^/]*\.js$")
# Demo sample models are the one place assets are allowed. The runtime ban has no exception.
DEMO_MODELS = "docs/demo/models/"
RUNTIME_NAMES = re.compile(r"(^|/)spine-player[^/]*\.(js|css)$|(^|/)spine-(webgl|canvas)[^/]*\.js$")
# Personal paths and machine names. Add a name here when a new one must stay out of the repo.
DENYLIST = ("/home/", "/Users/", "cachy", "ariff@", "192.168.")
# This file holds the denylist itself.
SCAN_EXEMPT = {"tests/scripts/test_static.py"}


def project_files():
    """Tracked files plus untracked files that git does not ignore, which is what a commit would add."""
    out = subprocess.run(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"],
        cwd=ROOT,
        capture_output=True,
        timeout=30,
    )
    if out.returncode != 0:
        raise unittest.SkipTest("not a git checkout")
    return [p for p in out.stdout.decode().split("\0") if p and (ROOT / p).is_file()]


def files_with(suffixes=(), shebang=None):
    found = []
    for rel in project_files():
        path = ROOT / rel
        if suffixes and path.suffix in suffixes:
            found.append(path)
        elif shebang:
            with path.open("rb") as f:
                if f.read(64).startswith(shebang):
                    found.append(path)
    return found


def bash_scripts():
    scripts = [p for p in files_with(suffixes=(".sh",))]
    scripts += [p for p in files_with(shebang=b"#!/usr/bin/env bash") if p not in scripts]
    return sorted(scripts)


def python_files():
    scripts = files_with(suffixes=(".py",))
    scripts += [p for p in files_with(shebang=b"#!/usr/bin/env python3") if p not in scripts]
    return sorted(scripts)


class Syntax(unittest.TestCase):
    def test_python_files_compile(self):
        files = python_files()
        self.assertTrue(files)
        with tempfile.TemporaryDirectory() as tmp:
            for path in files:
                with self.subTest(path=path.relative_to(ROOT)):
                    py_compile.compile(str(path), cfile=str(Path(tmp) / "x.pyc"), doraise=True)

    def test_bash_scripts_parse(self):
        files = bash_scripts()
        self.assertTrue(files)
        for path in files:
            with self.subTest(path=path.relative_to(ROOT)):
                done = subprocess.run(["bash", "-n", str(path)], capture_output=True, text=True)
                self.assertEqual(done.returncode, 0, done.stderr)

    def test_bash_scripts_are_executable(self):
        for path in bash_scripts():
            if path.parent.name in ("scripts", "bin"):
                with self.subTest(path=path.relative_to(ROOT)):
                    self.assertTrue(path.stat().st_mode & 0o111, "run: chmod +x")

    def test_bash_scripts_use_strict_mode(self):
        for path in bash_scripts():
            if path.parent.name in ("scripts", "bin"):
                with self.subTest(path=path.relative_to(ROOT)):
                    self.assertIn("set -euo pipefail", path.read_text())

    def test_server_has_a_python_shebang_and_is_executable(self):
        server = ROOT / "server/spinewall-server.py"
        self.assertTrue(server.read_text().startswith("#!/usr/bin/env python3"))

    def test_systemd_unit_has_the_required_sections(self):
        text = (ROOT / "systemd/spinewall.service").read_text()
        for section in ("[Unit]", "[Service]", "[Install]"):
            self.assertIn(section, text)
        self.assertIn("ExecStart=", text)


class Shellcheck(unittest.TestCase):
    def test_shellcheck_is_clean(self):
        tool = shutil.which("shellcheck")
        if not tool:
            self.skipTest("shellcheck is not installed")
        files = [str(p) for p in bash_scripts() if p.parent.name in ("scripts", "bin")]
        self.assertTrue(files)
        done = subprocess.run([tool, *files], capture_output=True, text=True, cwd=ROOT)
        self.assertEqual(done.returncode, 0, done.stdout)


class NoForbiddenContent(unittest.TestCase):
    def test_no_model_or_runtime_file_is_tracked(self):
        bad = [p for p in project_files() if FORBIDDEN_NAMES.search(p) and not p.startswith(DEMO_MODELS)]
        self.assertEqual(bad, [], "the runtime license forbids redistribution, and models are only allowed under docs/demo/models/")

    def test_no_runtime_file_is_tracked_anywhere(self):
        bad = [p for p in project_files() if RUNTIME_NAMES.search(p)]
        self.assertEqual(bad, [], "the runtime license forbids redistribution, with no exception for the demo")

    def test_no_file_is_big_enough_to_be_a_bundled_runtime(self):
        hits = []
        for rel in project_files():
            path = ROOT / rel
            if path.suffix in BINARY_SUFFIXES or rel in SCAN_EXEMPT or rel.startswith(("tests/page/node_modules", DEMO_MODELS)):
                continue
            if path.stat().st_size > 400_000:
                hits.append(f"{rel} is over 400 KB, so it may be a bundled runtime")
        self.assertEqual(hits, [])

    def test_no_personal_path_or_hostname(self):
        hits = []
        for rel in project_files():
            path = ROOT / rel
            if path.suffix in BINARY_SUFFIXES or rel in SCAN_EXEMPT or rel.endswith("package-lock.json"):
                continue
            try:
                text = path.read_text()
            except UnicodeDecodeError:
                continue
            for line_no, line in enumerate(text.splitlines(), 1):
                for word in DENYLIST:
                    if word in line:
                        hits.append(f"{rel}:{line_no}: {word}")
        self.assertEqual(hits, [])

    def test_gitignore_blocks_models_and_runtimes(self):
        for name in ("model.skel", "model.atlas", "spine-player.js"):
            done = subprocess.run(["git", "check-ignore", "-q", name], cwd=ROOT)
            with self.subTest(name=name):
                self.assertEqual(done.returncode, 0, f"{name} is not ignored")

    def test_no_bytecode_is_left_in_the_source_tree(self):
        for rel in project_files():
            self.assertNotIn("__pycache__", rel)


if __name__ == "__main__":
    unittest.main()
