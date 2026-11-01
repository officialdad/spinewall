"""scripts/patch-plasmoid.sh against a fake system containment.

The script hardcodes the system path. The tests run a copy of it with only that
line pointed at a temp directory, and assert the swap happened so a changed line fails loudly.
"""

import shutil
import unittest
from pathlib import Path

from tests.scripts.support import PATCH, ROOT, SCRIPTS, SYSTEM_DIR_LINE, ScriptCase, old_files_from_patch


class PatchCase(ScriptCase):
    def setUp(self):
        super().setUp()
        self.repo = self.tmp / "repo"
        (self.repo / "scripts").mkdir(parents=True)
        (self.repo / "plasma").mkdir()
        shutil.copy(PATCH, self.repo / "plasma" / "desktopcontainment.patch")
        self.system = self.tmp / "system" / "org.kde.desktopcontainment"
        for name, text in old_files_from_patch(PATCH.read_text()).items():
            target = self.system / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(text)
        source = (SCRIPTS / "patch-plasmoid.sh").read_text()
        self.assertIn(SYSTEM_DIR_LINE, source, "patch-plasmoid.sh changed its system_dir line. Update the test.")
        self.script = self.repo / "scripts" / "patch-plasmoid.sh"
        self.script.write_text(source.replace(SYSTEM_DIR_LINE, f"system_dir={self.system}"))
        self.local = self.home / ".local/share/plasma/plasmoids/org.kde.desktopcontainment"

    def fake_system_version_change(self):
        qml = self.system / "contents/ui/FolderView.qml"
        qml.write_text(qml.read_text().replace("MouseEventListener {", "NewMouseListener {"))


class Apply(PatchCase):
    def test_patch_applies_to_a_matching_containment(self):
        done = self.run_script(self.script)
        self.assertEqual(done.returncode, 0, done.stdout + done.stderr)
        folder = (self.local / "contents/ui/FolderView.qml").read_text()
        self.assertIn("enabled: false", folder)
        self.assertIn("http://127.0.0.1:8777/edit/", folder)
        self.assertIn("ContainmentLayoutManager.AppletsLayout.Locked", (self.local / "contents/ui/main.qml").read_text())

    def test_the_system_copy_is_never_modified(self):
        before = (self.system / "contents/ui/FolderView.qml").read_text()
        self.run_script(self.script)
        self.assertEqual((self.system / "contents/ui/FolderView.qml").read_text(), before)

    def test_no_leftover_candidate_or_reject_files(self):
        self.run_script(self.script)
        leftovers = [p.name for p in self.local.parent.iterdir() if p.name != self.local.name]
        self.assertEqual(leftovers, [])
        self.assertEqual([p for p in self.local.rglob("*") if p.suffix in (".rej", ".orig")], [])

    def test_first_run_makes_no_backup(self):
        done = self.run_script(self.script)
        self.assertNotIn(".bak", done.stdout)
        self.assertFalse(Path(f"{self.local}.bak").exists())

    def test_second_run_is_idempotent_and_keeps_one_backup(self):
        self.run_script(self.script)
        first = (self.local / "contents/ui/FolderView.qml").read_text()
        done = self.run_script(self.script)
        self.assertEqual(done.returncode, 0, done.stderr)
        self.assertEqual((self.local / "contents/ui/FolderView.qml").read_text(), first)
        self.assertTrue(Path(f"{self.local}.bak").is_dir())
        self.run_script(self.script)
        backups = [p.name for p in self.local.parent.iterdir() if ".bak" in p.name]
        self.assertEqual(backups, [self.local.name + ".bak"])

    def test_the_backup_holds_the_previous_local_copy(self):
        self.run_script(self.script)
        marker = self.local / "marker.txt"
        marker.write_text("previous")
        self.run_script(self.script)
        self.assertEqual((Path(f"{self.local}.bak") / "marker.txt").read_text(), "previous")
        self.assertFalse((self.local / "marker.txt").exists())

    def test_other_files_of_the_containment_are_copied(self):
        (self.system / "metadata.json").write_text("{}")
        (self.system / "contents/config").mkdir()
        (self.system / "contents/config/main.xml").write_text("<x/>")
        self.run_script(self.script)
        self.assertTrue((self.local / "metadata.json").exists())
        self.assertTrue((self.local / "contents/config/main.xml").exists())


class Failure(PatchCase):
    def test_a_changed_system_file_fails_with_a_clear_message(self):
        self.fake_system_version_change()
        done = self.run_script(self.script)
        self.assertEqual(done.returncode, 1)
        self.assertIn("patch does not apply", done.stderr)

    def test_a_failed_patch_leaves_the_current_local_copy_untouched(self):
        self.run_script(self.script)
        (self.local / "marker.txt").write_text("keep")
        before = self.tree(self.local)
        self.fake_system_version_change()
        done = self.run_script(self.script)
        self.assertNotEqual(done.returncode, 0)
        self.assertEqual(self.tree(self.local), before)
        self.assertFalse(Path(f"{self.local}.bak").exists())

    def test_a_failed_patch_leaves_no_candidate_directory(self):
        self.fake_system_version_change()
        self.run_script(self.script)
        leftovers = [p.name for p in self.local.parent.iterdir()]
        self.assertEqual(leftovers, [])

    def test_a_failed_patch_keeps_the_previous_backup(self):
        self.run_script(self.script)
        self.run_script(self.script)
        backup_before = self.tree(f"{self.local}.bak")
        self.fake_system_version_change()
        self.run_script(self.script)
        self.assertEqual(self.tree(f"{self.local}.bak"), backup_before)

    def test_missing_system_directory_is_a_clear_error(self):
        shutil.rmtree(self.system)
        done = self.run_script(self.script)
        self.assertEqual(done.returncode, 1)
        self.assertIn("not found", done.stderr)
        self.assertIn("Plasma 6", done.stderr)
        self.assertFalse(self.local.exists())

    def test_missing_patch_tool_is_an_error_not_a_half_install(self):
        tools = ("dirname", "mkdir", "mktemp", "cp", "rm", "find", "mv", "cat", "patch")
        done = self.run_script(self.script, path=self.path_without("patch", tools=tools))
        self.assertNotEqual(done.returncode, 0)
        self.assertFalse(self.local.exists())


@unittest.skipIf(Path("/usr/share/plasma/plasmoids/org.kde.desktopcontainment").exists(), "a real Plasma containment is installed")
class RealScript(ScriptCase):
    def test_the_real_script_reports_a_missing_containment(self):
        done = self.run_script(SCRIPTS / "patch-plasmoid.sh")
        self.assertEqual(done.returncode, 1)
        self.assertIn("org.kde.desktopcontainment not found", done.stderr)
        self.assertEqual(self.tree(self.home), [])


class PatchFile(unittest.TestCase):
    def test_patch_targets_only_the_two_known_files(self):
        targets = {l[6:] for l in PATCH.read_text().splitlines() if l.startswith("--- a/")}
        self.assertEqual(targets, {"contents/ui/FolderView.qml", "contents/ui/main.qml"})

    def test_patch_endpoint_matches_the_default_server_port(self):
        self.assertIn("127.0.0.1:8777/edit/", PATCH.read_text())
        self.assertIn('"8777"', (ROOT / "server/spinewall-server.py").read_text().replace("'", '"'))


if __name__ == "__main__":
    unittest.main()
