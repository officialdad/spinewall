"""scripts/package-windows.py model zip import. The theme build needs the network and is not run here."""

import importlib.util
import tempfile
import unittest
import zipfile
from pathlib import Path

from tests.scripts.support import SCRIPTS


def load():
    spec = importlib.util.spec_from_file_location("package_windows", SCRIPTS / "package-windows.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class ImportZip(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp())
        self.addCleanup(lambda: __import__("shutil").rmtree(self.tmp, ignore_errors=True))
        self.pack = load()

    def make_zip(self, names):
        path = self.tmp / "m.zip"
        with zipfile.ZipFile(path, "w") as z:
            for name in names:
                z.writestr(name, b"x")
        return path

    def test_backslash_paths_extract_and_nested_directories_are_skipped(self):
        archive = self.make_zip(["c1\\c1.skel", "c1\\c1.atlas", "c1\\c1.png", "c1\\aim\\a.skel", "c1\\cover\\c.png", "c1\\readme.exe"])
        out = self.tmp / "models"
        self.assertEqual(self.pack.import_zip(archive, out), 3)
        self.assertEqual(sorted(p.name for p in (out / "c1").iterdir()), ["c1.atlas", "c1.png", "c1.skel"])

    def test_parent_directory_entries_never_leave_the_models_directory(self):
        archive = self.make_zip(["../evil/x.skel", "c1/../../y.skel", "C:\\z\\a.skel"])
        out = self.tmp / "models"
        self.assertEqual(self.pack.import_zip(archive, out), 0)
        self.assertFalse((self.tmp / "evil").exists())


if __name__ == "__main__":
    unittest.main()
