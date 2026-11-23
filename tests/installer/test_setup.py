"""End-to-end tests for `spinewall setup` driven over a real terminal (pty).

Each test builds the Go binary once, runs it under a throwaway HOME with every
XDG path overridden, and drives the terminal form with keystrokes. The live
wallpaper on the default port is never touched: every run uses its own port.

These tests need a Go toolchain to build the binary. They skip without one.
"""

import json
import os
import pty
import select
import shutil
import signal
import subprocess
import tempfile
import time
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
_BUILD_DIR = None
BINARY = None


def go_available():
    return shutil.which("go") is not None


class Harness(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        global _BUILD_DIR, BINARY
        if not go_available():
            raise unittest.SkipTest("go is not on PATH")
        _BUILD_DIR = Path(tempfile.mkdtemp(prefix="spinewall-e2e-build-"))
        BINARY = _BUILD_DIR / "spinewall"
        env = dict(os.environ, CGO_ENABLED="0")
        done = subprocess.run(
            ["go", "build", "-o", str(BINARY), "./cmd/spinewall"],
            cwd=ROOT,
            env=env,
            capture_output=True,
            text=True,
        )
        if done.returncode != 0:
            raise RuntimeError("go build failed:\n" + done.stderr)

    @classmethod
    def tearDownClass(cls):
        if _BUILD_DIR is not None:
            shutil.rmtree(_BUILD_DIR, True)

    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp(prefix="spinewall-e2e-"))
        self.addCleanup(shutil.rmtree, self.tmp, True)
        self.home = self.tmp / "home"
        self.home.mkdir()
        self.port = self.free_port()
        self.addCleanup(self._stop_server)

    def free_port(self):
        import socket

        s = socket.socket()
        s.bind(("127.0.0.1", 0))
        port = s.getsockname()[1]
        s.close()
        return port

    def env(self, **extra):
        base = {
            "HOME": str(self.home),
            "XDG_DATA_HOME": str(self.home / ".local/share"),
            "XDG_STATE_HOME": str(self.home / ".local/state"),
            "XDG_CACHE_HOME": str(self.home / ".cache"),
            "XDG_CONFIG_HOME": str(self.home / ".config"),
            "XDG_RUNTIME_DIR": str(self.tmp / "runtime"),
            "SPINEWALL_PORT": str(self.port),
            "TERM": "xterm-256color",
        }
        base.update(extra)
        return base

    def _stop_server(self):
        """Stop the server setup started, so nothing holds the port between tests."""
        pid_file = self.home / ".local/state/spinewall/server.pid"
        if pid_file.exists():
            try:
                pid = int(pid_file.read_text().strip())
                os.kill(pid, signal.SIGTERM)
            except (ValueError, ProcessLookupError, OSError):
                pass

    def snapshot(self):
        out = {}
        for path in sorted(self.home.rglob("*")):
            rel = str(path.relative_to(self.home))
            if path.is_dir():
                out[rel + "/"] = "dir"
            else:
                out[rel] = path.read_bytes()
        return out

    def run_setup_yes(self, *flags):
        return subprocess.run(
            [str(BINARY), "setup", "--yes", *flags],
            env=self.env(),
            capture_output=True,
            text=True,
            timeout=60,
        )

    def drive_pty(self, keys, *flags, timeout=45):
        """Run setup under a pty and send keys. Returns the terminal output."""
        argv = [str(BINARY), "setup", *flags]
        pid, fd = pty.fork()
        if pid == 0:
            os.execvpe(argv[0], argv, self.env())
        output = b""

        def drain(seconds):
            nonlocal output
            end = time.time() + seconds
            while time.time() < end:
                ready, _, _ = select.select([fd], [], [], 0.1)
                if not ready:
                    continue
                try:
                    chunk = os.read(fd, 4096)
                except OSError:
                    return False
                if not chunk:
                    return False
                output += chunk
            return True

        def settle(limit=8.0, quiet=0.8):
            # A key sent before its form is drawn is lost, so wait for new
            # output, then for the screen to go quiet, before the next key.
            start = len(output)
            end = time.time() + limit
            while time.time() < end:
                seen = len(output)
                if not drain(quiet):
                    return False
                if len(output) > start and len(output) == seen:
                    return True
            return True

        try:
            settle(limit=15.0)
            for key in keys:
                try:
                    os.write(fd, key)
                except OSError:
                    break
                if not settle():
                    break
            for _ in range(60):
                if not drain(0.3):
                    break
        finally:
            try:
                os.kill(pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                os.waitpid(pid, 0)
            except ChildProcessError:
                pass
            os.close(fd)
        return output.decode("utf-8", "replace")


class SetupYes(Harness):
    def test_setup_yes_installs_then_uninstall_restores_home(self):
        before = self.snapshot()
        done = self.run_setup_yes("--offline")
        self.assertEqual(done.returncode, 0, done.stdout + done.stderr)
        self.assertIn("setup complete", done.stdout)

        manifest_path = self.home / ".local/state/spinewall/install-manifest.json"
        self.assertTrue(manifest_path.is_file(), "manifest not written")
        manifest = json.loads(manifest_path.read_text())
        self.assertTrue(manifest["files"], "manifest lists no files")
        for entry in manifest["files"]:
            self.assertTrue(Path(entry["path"]).is_file(), f"missing {entry['path']}")

        self._stop_server()
        done = subprocess.run(
            [str(BINARY), "uninstall", "--yes"],
            env=self.env(),
            capture_output=True,
            text=True,
            timeout=60,
        )
        self.assertEqual(done.returncode, 0, done.stdout + done.stderr)
        after = self.snapshot()
        self.assertEqual(before, after, "home is not byte identical after uninstall")

    def test_setup_yes_reports_zero_models(self):
        done = self.run_setup_yes("--offline")
        self.assertEqual(done.returncode, 0, done.stdout + done.stderr)
        self.assertIn("0 model(s)", done.stdout)
        self.assertIn("No models were found", done.stdout)


class SetupInteractive(Harness):
    # Consent "y", Enter to keep the default models folder, Enter on the backend
    # select, then "n" for the systemd unit when that prompt appears. A key sent
    # when no form is waiting is ignored, so the same keys fit both environments.
    KEYS = [b"y", b"\r", b"\r", b"n"]

    def test_happy_path_accepts_licence_and_installs(self):
        output = self.drive_pty(self.KEYS, "--offline")
        self.assertIn("setup complete", output)
        self.assertTrue((self.home / ".local/bin/spinewall").is_file())
        self.assertTrue((self.home / ".config/autostart/spinewall.desktop").is_file())

    def test_no_models_path_offers_a_folder(self):
        output = self.drive_pty(self.KEYS, "--offline")
        self.assertIn("No models were found", output)
        self.assertIn("spinewall rescan", output)


class NoNetwork(Harness):
    def test_offline_install_still_completes(self):
        # A refused proxy makes every fetch fail, which exercises the no-network path.
        done = subprocess.run(
            [str(BINARY), "setup", "--yes"],
            env=self.env(HTTPS_PROXY="http://127.0.0.1:1", HTTP_PROXY="http://127.0.0.1:1"),
            capture_output=True,
            text=True,
            timeout=60,
        )
        self.assertEqual(done.returncode, 0, done.stdout + done.stderr)
        self.assertIn("setup complete", done.stdout)
        self.assertTrue((self.home / ".local/bin/spinewall").is_file())


if __name__ == "__main__":
    unittest.main()
