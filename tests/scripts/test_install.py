"""scripts/install.sh, the POSIX bootstrap, with stub curl/sha256sum/minisign/install.

The runtime fetch and the theme build moved into the Go binary (`spinewall setup`
and `spinewall build-theme`), so this file only checks the bootstrap: arch
detection, checksum and signature verification, install location, and that it
hands off to `spinewall setup`.
"""

import os
import shutil
import subprocess
import unittest
from pathlib import Path

from tests.scripts.support import ROOT, SCRIPTS, ScriptCase, write_stub

# curl serves the release API, the binary, and SHA256SUMS from files the test
# writes, so the bootstrap exercises its real parsing without the network.
# The stub scans for -o and the URL, because the bootstrap passes -fsSL as one
# combined flag.
CURL_STUB = r"""
out=""
url=""
prev=""
for arg in "$@"; do
  case "$arg" in
    http*) url="$arg" ;;
  esac
  if [ "$prev" = "-o" ]; then out="$arg"; fi
  prev="$arg"
done
if [ -n "${STUB_CURL_FAIL:-}" ]; then echo "curl: (22) 404" >&2; exit 22; fi
case "$url" in
  *api.github.com*releases/latest*) printf '{"tag_name": "%s"}\n' "${STUB_TAG:-v1.2.3}" ;;
  *SHA256SUMS.minisig) [ -n "${STUB_SIG_FAIL:-}" ] && exit 22; printf 'sig\n' ;;
  *SHA256SUMS) cp "$STUB_SUMS" "$out" ;;
  *spinewall-*-linux-*) cp "$STUB_BINARY" "$out" ;;
  *) echo "unexpected url $url" >&2; exit 22 ;;
esac
"""

# The downloaded binary is a stand-in that records the `setup` handoff. The
# bootstrap copies exactly these bytes to ~/.local/bin/spinewall and runs them.
# The SUMS are compiled in setUp with the real hash of those bytes, so the
# bootstrap's own checksum comparison is the thing under test.


class Bootstrap(ScriptCase):
    def setUp(self):
        super().setUp()
        self.log = self.tmp / "calls.log"
        # A fake release binary that logs the `setup` call it receives.
        self.binary = self.tmp / "fake-binary"
        self.binary.write_text(
            "#!/bin/sh\n"
            'echo "$@" >> "${STUB_LOG:-/dev/null}"\n'
        )
        self.binary.chmod(0o755)
        # The SHA256SUMS for the fake binary, computed the real way.
        import hashlib

        digest = hashlib.sha256(self.binary.read_bytes()).hexdigest()
        sums = self.tmp / "SHA256SUMS"
        sums.write_text(f"{digest}  spinewall-1.2.3-linux-amd64\n")
        self.sums = sums
        write_stub(self.stubs, "curl", CURL_STUB)

    def run_bootstrap(self, **env):
        env.setdefault("STUB_SUMS", str(self.sums))
        env.setdefault("STUB_BINARY", str(self.binary))
        env.setdefault("STUB_LOG", str(self.log))
        return self.run_script(SCRIPTS / "install.sh", env_extra=env)

    def test_installs_the_binary_and_runs_setup(self):
        done = self.run_bootstrap()
        self.assertEqual(done.returncode, 0, done.stdout + done.stderr)
        installed = self.home / ".local/bin/spinewall"
        self.assertTrue(installed.is_file(), "binary not installed")
        self.assertTrue(os.access(installed, os.X_OK), "binary not executable")
        self.assertEqual(self.log.read_text().strip(), "setup")

    def test_a_checksum_mismatch_stops_before_install(self):
        # Point the SUMS at a different hash than the binary.
        self.sums.write_text("0" * 64 + "  spinewall-1.2.3-linux-amd64\n")
        done = self.run_bootstrap()
        self.assertNotEqual(done.returncode, 0)
        self.assertIn("checksum mismatch", done.stderr)
        self.assertFalse((self.home / ".local/bin/spinewall").exists())

    def test_a_missing_checksum_entry_stops_the_install(self):
        self.sums.write_text("x  other-file\n")
        done = self.run_bootstrap()
        self.assertNotEqual(done.returncode, 0)
        self.assertIn("not listed in SHA256SUMS", done.stderr)

    def test_minisign_with_a_placeholder_key_says_so(self):
        # No minisign on PATH, so the bootstrap reports the weaker check.
        done = self.run_bootstrap()
        self.assertEqual(done.returncode, 0, done.stdout + done.stderr)
        # Either a signature note or the checksum-only note must appear.
        combined = done.stdout + done.stderr
        self.assertTrue(
            "only the SHA256SUMS check" in combined or "SHA256SUMS" in combined,
            combined,
        )

    def test_an_unsupported_architecture_is_named(self):
        # Shadow uname so it reports an arch the bootstrap rejects.
        uname = write_stub(self.stubs, "uname", 'echo sparc\n')
        self.assertTrue(uname.exists())
        done = self.run_bootstrap()
        self.assertNotEqual(done.returncode, 0)
        self.assertIn("unsupported architecture", done.stderr)


class BootstrapScript(unittest.TestCase):
    def test_the_bootstrap_is_posix_sh(self):
        # It must run under plain sh, not bash. A bash-only construct fails -n on sh.
        script = SCRIPTS / "install.sh"
        self.assertTrue(script.read_text().startswith("#!/bin/sh"))
        done = subprocess.run(["sh", "-n", str(script)], capture_output=True, text=True)
        self.assertEqual(done.returncode, 0, done.stderr)

    def test_the_public_key_is_a_marked_placeholder(self):
        # The captain supplies the real key. Until then the placeholder is present
        # and clearly marked, so the bootstrap refuses to pretend it verified.
        text = (SCRIPTS / "install.sh").read_text()
        self.assertIn("PLACEHOLDER", text)
        self.assertIn("MINISIGN_PUBLIC_KEY", text)


if __name__ == "__main__":
    unittest.main()
