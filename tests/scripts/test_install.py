"""scripts/install.sh, the POSIX bootstrap, with stub curl/sha256sum/minisign/install.

The runtime fetch and the theme build moved into the Go binary (`spinewall setup`
and `spinewall build-theme`), so this file only checks the bootstrap: arch
detection, checksum and signature verification, install location, and that it
hands off to `spinewall setup`.
"""

import os
import pty
import subprocess
import unittest

from tests.scripts.support import SCRIPTS, ScriptCase, write_stub

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


class BootstrapCase(ScriptCase):
    def setUp(self):
        super().setUp()
        self.log = self.tmp / "calls.log"
        # A fake release binary that logs the `setup` call it receives.
        self.binary = self.tmp / "fake-binary"
        self.binary.write_text(
            "#!/bin/sh\n"
            'echo "$@" >> "${STUB_LOG:-/dev/null}"\n'
            'if [ -t 0 ]; then echo tty > "${STUB_STDIN_LOG:-/dev/null}"; '
            'else echo not-tty > "${STUB_STDIN_LOG:-/dev/null}"; fi\n'
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


class Bootstrap(BootstrapCase):
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

    def test_a_placeholder_key_skips_the_signature_and_says_so(self):
        done = self.run_bootstrap()
        self.assertEqual(done.returncode, 0, done.stdout + done.stderr)
        self.assertIn("placeholder signature key", done.stderr)
        self.assertTrue((self.home / ".local/bin/spinewall").is_file())

    def test_an_unsupported_architecture_is_named(self):
        # Shadow uname so it reports an arch the bootstrap rejects.
        uname = write_stub(self.stubs, "uname", 'echo sparc\n')
        self.assertTrue(uname.exists())
        done = self.run_bootstrap()
        self.assertNotEqual(done.returncode, 0)
        self.assertIn("unsupported architecture", done.stderr)


REAL_KEY = "RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3"

# minisign verifies with the key on the command line. The stub records its
# arguments and passes or fails as the test asks.
MINISIGN_STUB = r"""
echo "$@" > "$STUB_MINISIGN_LOG"
[ -n "${STUB_MINISIGN_BAD:-}" ] && { echo "Signature verification failed" >&2; exit 1; }
exit 0
"""

# The tools the bootstrap and the curl stub need, so a test can leave minisign
# off PATH even on a machine that has it.
BOOTSTRAP_TOOLS = (
    "bash", "env", "sh", "sha256sum", "awk", "sed", "head", "mktemp", "rm",
    "mkdir", "cp", "chmod", "uname", "cat",
)


class SignedBootstrap(BootstrapCase):
    """The bootstrap once the captain has put the real key in place."""

    def setUp(self):
        super().setUp()
        text = (SCRIPTS / "install.sh").read_text()
        start = text.index('MINISIGN_PUBLIC_KEY="') + len('MINISIGN_PUBLIC_KEY="')
        end = text.index('"', start)
        self.script = self.tmp / "install.sh"
        self.script.write_text(text[:start] + REAL_KEY + text[end:])
        self.minisign_log = self.tmp / "minisign.log"

    def run_signed(self, with_minisign=True, **env):
        if with_minisign:
            write_stub(self.stubs, "minisign", MINISIGN_STUB)
        env.setdefault("STUB_SUMS", str(self.sums))
        env.setdefault("STUB_BINARY", str(self.binary))
        env.setdefault("STUB_LOG", str(self.log))
        env.setdefault("STUB_MINISIGN_LOG", str(self.minisign_log))
        limited = self.path_without("minisign", tools=BOOTSTRAP_TOOLS)
        env["PATH"] = f"{self.stubs}:{limited}"
        return self.run_script(self.script, env_extra=env)

    def assert_not_installed(self, done, message):
        self.assertNotEqual(done.returncode, 0, done.stdout + done.stderr)
        self.assertIn(message, done.stderr)
        self.assertFalse((self.home / ".local/bin/spinewall").exists())
        self.assertFalse(self.log.exists(), "setup ran after a failed signature check")

    def test_a_good_signature_installs(self):
        done = self.run_signed()
        self.assertEqual(done.returncode, 0, done.stdout + done.stderr)
        self.assertIn("signature ok", done.stderr)
        args = self.minisign_log.read_text().split()
        self.assertEqual(args[0], "-Vm")
        self.assertEqual(args[-2:], ["-P", REAL_KEY])
        self.assertEqual(self.log.read_text().strip(), "setup")

    def test_missing_minisign_stops_and_says_what_to_do(self):
        done = self.run_signed(with_minisign=False)
        self.assert_not_installed(done, "minisign is required")
        self.assertIn(f"minisign -Vm SHA256SUMS -P {REAL_KEY}", done.stderr)
        self.assertIn("SPINEWALL_SKIP_VERIFY=1", done.stderr)

    def test_a_missing_signature_stops_the_install(self):
        done = self.run_signed(STUB_SIG_FAIL="1")
        self.assert_not_installed(done, "has no signature")

    def test_a_bad_signature_stops_the_install(self):
        done = self.run_signed(STUB_MINISIGN_BAD="1")
        self.assert_not_installed(done, "signature check failed")

    def test_the_opt_out_installs_with_a_warning(self):
        done = self.run_signed(with_minisign=False, STUB_SIG_FAIL="1", SPINEWALL_SKIP_VERIFY="1")
        self.assertEqual(done.returncode, 0, done.stdout + done.stderr)
        self.assertIn("WARNING", done.stderr)
        self.assertIn("NOT verified", done.stderr)
        self.assertTrue((self.home / ".local/bin/spinewall").is_file())


class PipedBootstrap(BootstrapCase):
    """`curl | sh`: the script arrives on stdin, so the wizard must read the terminal."""

    def run_piped(self, controlling_tty):
        stdin_log = self.tmp / "stdin.log"
        env = {
            "HOME": str(self.home),
            "PATH": f"{self.stubs}:{os.environ.get('PATH', '')}",
            "TMPDIR": str(self.tmp),
            "STUB_SUMS": str(self.sums),
            "STUB_BINARY": str(self.binary),
            "STUB_LOG": str(self.log),
            "STUB_STDIN_LOG": str(stdin_log),
        }
        primary, secondary = pty.openpty()
        tty_name = os.ttyname(secondary)

        def new_session():
            os.setsid()
            if controlling_tty:
                # Opening a terminal as a new session leader makes it the
                # controlling terminal, as a login shell has.
                os.close(os.open(tty_name, os.O_RDWR))

        try:
            done = subprocess.run(
                ["sh"], input=(SCRIPTS / "install.sh").read_text(), env=env,
                capture_output=True, text=True, timeout=60, cwd=str(self.tmp),
                preexec_fn=new_session,
            )
        finally:
            os.close(primary)
            os.close(secondary)
        return done, stdin_log.read_text().strip() if stdin_log.exists() else ""

    def test_setup_reads_the_terminal_under_a_pipe(self):
        done, stdin = self.run_piped(controlling_tty=True)
        self.assertEqual(done.returncode, 0, done.stdout + done.stderr)
        self.assertEqual(stdin, "tty")

    def test_setup_without_a_terminal_still_runs(self):
        done, stdin = self.run_piped(controlling_tty=False)
        self.assertEqual(done.returncode, 0, done.stdout + done.stderr)
        self.assertEqual(stdin, "not-tty")
        self.assertEqual(self.log.read_text().strip(), "setup")


class BootstrapScript(unittest.TestCase):
    def test_the_bootstrap_is_posix_sh(self):
        # It must run under plain sh, not bash. A bash-only construct fails -n on sh.
        done = subprocess.run(["sh", "-n", str(SCRIPTS / "install.sh")], capture_output=True, text=True)
        self.assertEqual(done.returncode, 0, done.stderr)


if __name__ == "__main__":
    unittest.main()
