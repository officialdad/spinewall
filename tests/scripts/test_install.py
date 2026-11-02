"""scripts/install.sh with stub npm, curl, systemctl, and sha256sum, so no network is used.

The stub sha256sum stands in for the pinned hashes, which only match the real packages.
A run against the real registry and the real hashes is gated by SPINEWALL_TEST_NETWORK=1.
"""

import os
import shutil
import subprocess
import unittest
from pathlib import Path

from tests.scripts.support import ROOT, SCRIPTS, ScriptCase, write_stub

TOOLS = ("npm", "curl", "sha256sum", "python3", "systemctl", "tar", "sed")
STUBBED = ("npm", "curl", "sha256sum", "systemctl")
CORE = ("bash", "dirname", "mktemp", "mkdir", "cp", "rm", "cat", "grep", "install", "echo", "printf")

NPM_STUB = r"""
# usage: npm pack @esotericsoftware/spine-player@VERSION --silent
version="${2##*@}"
if [ -n "${STUB_NPM_FAIL:-}" ]; then echo "npm error" >&2; exit 1; fi
d="$(mktemp -d)"
mkdir -p "$d/package/dist/iife"
if [ -n "${STUB_NO_VAR_SPINE:-}" ]; then
  echo "window.spine = {};" > "$d/package/dist/iife/spine-player.js"
else
  printf 'var spine = {version: "%s"};\nconsole.log(spine);\n' "$version" > "$d/package/dist/iife/spine-player.js"
fi
echo ".spine-player{}" > "$d/package/dist/spine-player.css"
tar czf "esotericsoftware-spine-player-$version.tgz" -C "$d" package
rm -rf "$d"
"""

CURL_STUB = r"""
out=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then out="$2"; shift; fi
  shift
done
if [ -n "${STUB_CURL_FAIL:-}" ]; then echo "curl: (22) 404" >&2; exit 22; fi
printf 'var spine = {};\n' > "$out"
"""

SHA_STUB = r"""
cat >/dev/null
if [ -n "${STUB_SHA_FAIL:-}" ]; then echo "sha256sum: WARNING: 1 computed checksum did NOT match" >&2; exit 1; fi
exit 0
"""

SYSTEMCTL_STUB = r"""
echo "$*" >> "$STUB_LOG"
"""


class InstallCase(ScriptCase):
    def setUp(self):
        super().setUp()
        self.log = self.tmp / "systemctl.log"
        write_stub(self.stubs, "npm", NPM_STUB)
        write_stub(self.stubs, "curl", CURL_STUB)
        write_stub(self.stubs, "sha256sum", SHA_STUB)
        write_stub(self.stubs, "systemctl", SYSTEMCTL_STUB)
        self.theme = self.home / ".local/share/webwallpaper/themes/spinewall"

    def install(self, **env):
        env.setdefault("STUB_LOG", str(self.log))
        return self.run_script(SCRIPTS / "install.sh", env_extra=env)


class HappyPath(InstallCase):
    def test_install_succeeds_and_prints_next_steps(self):
        done = self.install()
        self.assertEqual(done.returncode, 0, done.stdout + done.stderr)
        self.assertIn("Installed.", done.stdout)
        self.assertIn("patch-plasmoid.sh", done.stdout)

    def test_theme_has_one_renamed_player_per_runtime(self):
        self.install()
        for suffix, global_name in (("40", "spine40"), ("41", "spine41"), ("42", "spine42")):
            text = (self.theme / f"spine-player{suffix}.js").read_text()
            self.assertTrue(text.startswith(f"var {global_name} = "), suffix)
            self.assertNotIn("var spine = ", text)

    def test_rename_touches_only_the_declaration_line(self):
        self.install()
        text = (self.theme / "spine-player42.js").read_text()
        self.assertIn("console.log(spine);", text)

    def test_legacy_players_are_wrapped_in_a_function_scope(self):
        self.install()
        for global_name in ("spine37", "spine38"):
            with self.subTest(runtime=global_name):
                text = (self.theme / f"spine-player{global_name[5:]}.js").read_text()
                self.assertTrue(text.startswith(f"var {global_name} = (function () {{"))
                self.assertIn("var spine = {};", text)
                self.assertTrue(text.rstrip().endswith("return spine;\n})();".rstrip()))

    def test_theme_files_are_copied(self):
        self.install()
        for name in ("index.html", "local-host.js", "spinewall.theme", "spine-player.css"):
            self.assertTrue((self.theme / name).is_file(), name)
        self.assertEqual((self.theme / "index.html").read_bytes(), (ROOT / "theme/index.html").read_bytes())

    def test_server_and_cli_are_installed_executable(self):
        self.install()
        for name in ("spinewall-server", "spinewall"):
            path = self.home / ".local/bin" / name
            self.assertTrue(os.access(path, os.X_OK), name)

    def test_directories_are_created(self):
        self.install()
        for rel in (".local/share/spinewall/models", ".local/state/spinewall", ".cache/spinewall", ".config/systemd/user"):
            self.assertTrue((self.home / rel).is_dir(), rel)

    def test_unit_is_installed_and_the_service_restarted(self):
        self.install()
        self.assertTrue((self.home / ".config/systemd/user/spinewall.service").is_file())
        self.assertEqual(
            self.log.read_text().splitlines(),
            ["--user daemon-reload", "--user enable spinewall.service", "--user restart spinewall.service"],
        )

    def test_no_pinned_tarball_leaks_into_the_theme(self):
        self.install()
        self.assertEqual([p.name for p in self.theme.glob("*.tgz")], [])

    def test_the_temp_work_directory_is_removed(self):
        before = {p.name for p in self.tmp.iterdir()}
        self.install()
        leftovers = [p for p in self.tmp.iterdir() if p.name not in before and p.name.startswith("tmp")]
        self.assertEqual(leftovers, [])

    def test_a_second_run_gives_the_same_theme(self):
        self.install()
        first = {p.name: p.read_bytes() for p in self.theme.iterdir()}
        done = self.install()
        self.assertEqual(done.returncode, 0)
        self.assertEqual({p.name: p.read_bytes() for p in self.theme.iterdir()}, first)

    def test_models_already_in_the_models_directory_are_kept(self):
        keep = self.home / ".local/share/spinewall/models/mine"
        keep.mkdir(parents=True)
        (keep / "mine.skel").write_text("x")
        self.install()
        self.assertTrue((keep / "mine.skel").exists())

    def test_nothing_outside_home_and_tmp_is_written(self):
        self.install()
        self.assertEqual(sorted(p.name for p in self.home.iterdir()), [".cache", ".config", ".local"])


class Failures(InstallCase):
    def assert_theme_untouched(self):
        self.assertFalse(self.theme.exists())
        self.assertFalse((self.home / ".local/bin").exists())
        self.assertFalse(self.log.exists())

    def test_each_missing_tool_is_named_and_stops_the_install(self):
        for tool in TOOLS:
            with self.subTest(tool=tool):
                # The stubs stand in for the network tools, so the test needs no real npm. The directory is
                # removed even when an assertion fails, because a leftover link hides the next tool as present.
                path = self.path_without(tool, tools=[t for t in TOOLS + CORE if t not in STUBBED])
                self.addCleanup(shutil.rmtree, path, True)
                for stub in STUBBED:
                    if stub != tool:
                        (Path(path) / stub).symlink_to(self.stubs / stub)
                try:
                    done = self.run_script(SCRIPTS / "install.sh", path=path, env_extra={"STUB_LOG": str(self.log)})
                    self.assertEqual(done.returncode, 1)
                    self.assertIn(f"error: {tool} is required", done.stderr)
                    self.assert_theme_untouched()
                finally:
                    shutil.rmtree(path, True)

    def test_a_bad_hash_stops_the_install_before_anything_is_written(self):
        done = self.install(STUB_SHA_FAIL="1")
        self.assertNotEqual(done.returncode, 0)
        self.assert_theme_untouched()

    def test_a_bad_hash_leaves_an_existing_theme_untouched(self):
        self.assertEqual(self.install().returncode, 0)
        before = {p.name: p.read_bytes() for p in self.theme.iterdir()}
        done = self.install(STUB_SHA_FAIL="1")
        self.assertNotEqual(done.returncode, 0)
        self.assertEqual({p.name: p.read_bytes() for p in self.theme.iterdir()}, before)

    def test_a_player_without_the_rename_target_fails_loudly(self):
        done = self.install(STUB_NO_VAR_SPINE="1")
        self.assertEqual(done.returncode, 1)
        self.assertIn("has no 'var spine = ' line", done.stderr)
        self.assertIn("Update this script", done.stderr)
        self.assert_theme_untouched()

    def test_an_npm_error_stops_the_install(self):
        done = self.install(STUB_NPM_FAIL="1")
        self.assertNotEqual(done.returncode, 0)
        self.assert_theme_untouched()

    def test_a_curl_error_stops_the_install(self):
        done = self.install(STUB_CURL_FAIL="1")
        self.assertNotEqual(done.returncode, 0)
        self.assert_theme_untouched()

    def test_a_systemctl_error_is_not_swallowed(self):
        write_stub(self.stubs, "systemctl", 'echo "$*" >> "$STUB_LOG"; exit 1')
        done = self.install()
        self.assertNotEqual(done.returncode, 0)
        self.assertNotIn("Installed.", done.stdout)


class Pins(unittest.TestCase):
    def test_every_runtime_is_pinned_by_a_64_digit_hash(self):
        import re

        text = (SCRIPTS / "install.sh").read_text()
        calls = re.findall(r"^fetch_runtime (\S+) (\S+) (\S+)$", text, re.M)
        self.assertEqual([c[1] for c in calls], ["spine40", "spine41", "spine42"])
        for _, _, digest in calls:
            self.assertRegex(digest, r"^[0-9a-f]{64}$")
        legacy = re.findall(r"^fetch_runtime_legacy (\S+) (\S+) (\S+)$", text, re.M)
        self.assertEqual([c[0] for c in legacy], ["spine38", "spine37"])
        for _, commit, digest in legacy:
            self.assertRegex(commit, r"^[0-9a-f]{40}$")
            self.assertRegex(digest, r"^[0-9a-f]{64}$")

    def test_the_theme_loads_every_player_the_script_installs(self):
        page = (ROOT / "theme/index.html").read_text()
        for name in ("spine-player37.js", "spine-player38.js", "spine-player40.js", "spine-player41.js", "spine-player42.js"):
            self.assertIn(name, page)
        for global_name in ("spine37", "spine38", "spine40", "spine41", "spine42"):
            self.assertIn(global_name, page)


@unittest.skipUnless(os.environ.get("SPINEWALL_TEST_NETWORK") == "1", "set SPINEWALL_TEST_NETWORK=1 to run against the real registry")
class RealNetwork(ScriptCase):
    def test_real_install_matches_every_pinned_hash(self):
        write_stub(self.stubs, "systemctl", 'echo "$*" >> "$STUB_LOG"')
        done = self.run_script(SCRIPTS / "install.sh", env_extra={"STUB_LOG": str(self.tmp / "log")})
        self.assertEqual(done.returncode, 0, done.stdout + done.stderr)
        theme = self.home / ".local/share/webwallpaper/themes/spinewall"
        for name in ("spine-player37.js", "spine-player38.js", "spine-player40.js", "spine-player41.js", "spine-player42.js"):
            self.assertGreater((theme / name).stat().st_size, 10000, name)


if __name__ == "__main__":
    unittest.main()
