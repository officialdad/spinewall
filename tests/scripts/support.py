"""Helpers for the shell script tests: stub commands and a throwaway HOME."""

import os
import re
import shutil
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SCRIPTS = ROOT / "scripts"
PATCH = ROOT / "plasma" / "desktopcontainment.patch"
SYSTEM_DIR_LINE = "system_dir=/usr/share/plasma/plasmoids/org.kde.desktopcontainment"


def write_stub(directory, name, body):
    path = Path(directory) / name
    path.write_text("#!/usr/bin/env bash\n" + body + "\n")
    path.chmod(path.stat().st_mode | stat.S_IXUSR)
    return path


def old_files_from_patch(patch_text):
    """Build the pre-patch version of each file, from the old side of every hunk, at its line number."""
    files = {}
    current = None
    hunk = None
    for line in patch_text.splitlines():
        if line.startswith("--- a/"):
            current = line[6:]
            files[current] = {}
        elif line.startswith("+++ "):
            continue
        elif line.startswith("@@"):
            start = int(re.match(r"@@ -(\d+)", line).group(1))
            hunk = files[current].setdefault(start, [])
        elif hunk is not None and line[:1] in (" ", "-"):
            hunk.append(line[1:])
    result = {}
    for name, hunks in files.items():
        lines = []
        for start in sorted(hunks):
            while len(lines) < start - 1:
                lines.append(f"// filler line {len(lines) + 1}")
            lines.extend(hunks[start])
        result[name] = "\n".join(lines) + "\n"
    return result


class ScriptCase(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp(prefix="spinewall-script-test-"))
        self.addCleanup(shutil.rmtree, self.tmp, True)
        self.home = self.tmp / "home"
        self.home.mkdir()
        self.stubs = self.tmp / "stubs"
        self.stubs.mkdir()

    def run_script(self, script, extra_path_first=None, path=None, env_extra=None, args=()):
        env = {
            "HOME": str(self.home),
            "PATH": path if path is not None else f"{self.stubs}:{os.environ.get('PATH', '')}",
            "TMPDIR": str(self.tmp),
        }
        env.update(env_extra or {})
        return subprocess.run(
            [shutil.which("bash"), str(script), *args], env=env, capture_output=True, text=True, timeout=60, cwd=str(self.tmp)
        )

    def path_without(self, *missing, tools=()):
        """A PATH directory that links the named tools and omits `missing`, so a tool looks absent."""
        directory = self.tmp / "limited-bin"
        directory.mkdir(exist_ok=True)
        for tool in tools:
            if tool in missing:
                continue
            found = shutil.which(tool)
            if found and not (directory / tool).exists():
                (directory / tool).symlink_to(found)
        return str(directory)

    def tree(self, directory):
        directory = Path(directory)
        if not directory.exists():
            return None
        return sorted(str(p.relative_to(directory)) for p in directory.rglob("*"))
