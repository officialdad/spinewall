"""Per-character auto-cycle flag: validation, commands, folding, persistence, and the CLI."""

import json
import os
import subprocess
import unittest

from tests.server.support import CLI, ServerCase


class AutoCycle(ServerCase):
    def two(self):
        self.cmd("add-character", "gamma")
        return self.state()["characters"]

    def test_single_mode_defaults_to_off(self):
        self.assertIs(self.state()["auto"], False)

    def test_single_mode_toggle_bumps_rev(self):
        rev = self.state()["rev"]
        state = self.cmd("auto-cycle", "on")
        self.assertIs(state["auto"], True)
        self.assertEqual(state["rev"], rev + 1)

    def test_repeating_the_same_value_changes_nothing(self):
        self.cmd("auto-cycle", "on")
        before = self.state()
        self.cmd("auto-cycle", "on")
        self.assertEqual(self.state(), before)

    def test_invalid_values_change_nothing(self):
        before = self.state()
        for arg in ("maybe", "1", "true", "ON", ""):
            with self.subTest(arg=arg):
                self.get(f"/cmd/auto-cycle/{arg}")
                self.assertEqual(self.state(), before)

    def test_multi_mode_uid_toggles_one_character_and_bumps_crev(self):
        a, b = self.two()
        crev = self.state()["crev"]
        state = self.cmd("auto-cycle", b["uid"], "on")
        self.assertEqual([c["auto"] for c in state["characters"]], [False, True])
        self.assertEqual(state["crev"], crev + 1)

    def test_multi_mode_without_uid_toggles_every_character(self):
        self.two()
        state = self.cmd("auto-cycle", "on")
        self.assertTrue(all(c["auto"] for c in state["characters"]))
        state = self.cmd("auto-cycle", "off")
        self.assertFalse(any(c["auto"] for c in state["characters"]))

    def test_unknown_uid_changes_nothing(self):
        self.two()
        before = self.state()
        self.cmd("auto-cycle", "nobody", "on")
        self.assertEqual(self.state(), before)

    def test_invalid_value_with_uid_changes_nothing(self):
        a, _ = self.two()
        before = self.state()
        self.cmd("auto-cycle", a["uid"], "maybe")
        self.assertEqual(self.state(), before)

    def test_adding_a_character_carries_the_single_flag_into_the_first(self):
        self.cmd("auto-cycle", "on")
        a, b = self.two()
        self.assertIs(a["auto"], True)
        self.assertIs(b["auto"], False)

    def test_folding_back_to_one_character_carries_the_flag_out(self):
        a, b = self.two()
        self.cmd("auto-cycle", b["uid"], "on")
        state = self.cmd("remove-character", a["uid"])
        self.assertEqual(state["characters"], [])
        self.assertIs(state["auto"], True)

    def test_folding_back_carries_an_off_flag_too(self):
        a, b = self.two()
        self.cmd("auto-cycle", a["uid"], "on")
        state = self.cmd("remove-character", a["uid"])
        self.assertIs(state["auto"], False)

    def test_layout_push_ignores_a_non_bool_flag(self):
        a, _ = self.two()
        self.cmd("auto-cycle", a["uid"], "on")
        for bad in (1, 0, "true", None, [True]):
            with self.subTest(bad=bad):
                out = self.post("/layout", {"characters": [{"uid": a["uid"], "auto": bad, "x": 0.1}]}).json
                self.assertIs(next(c for c in out["characters"] if c["uid"] == a["uid"])["auto"], True)

    def test_layout_push_applies_a_real_bool(self):
        a, _ = self.two()
        out = self.post("/layout", {"characters": [{"uid": a["uid"], "auto": True}]}).json
        self.assertIs(next(c for c in out["characters"] if c["uid"] == a["uid"])["auto"], True)

    def test_replace_layout_drops_a_non_bool_flag_to_false(self):
        out = self.post("/layout", {"characters": [{"model": "beta", "auto": 1}, {"model": "gamma", "auto": True}], "replace": True}).json
        self.assertEqual([c["auto"] for c in out["characters"]], [False, True])

    def test_multi_mode_flag_survives_a_restart(self):
        a, b = self.two()
        self.cmd("auto-cycle", b["uid"], "on")
        self.server.restart()
        self.assertEqual([c["auto"] for c in self.state()["characters"]], [False, True])
        layout = json.loads((self.tmp / "state" / "layout.json").read_text()) if (self.tmp / "state" / "layout.json").exists() else None
        if layout is not None:
            self.assertEqual([c["auto"] for c in layout["characters"]], [False, True])

    def test_single_mode_flag_survives_a_restart(self):
        self.cmd("auto-cycle", "on")
        self.server.restart()
        self.assertIs(self.state()["auto"], True)

    def test_corrupt_flag_in_a_saved_layout_loads_as_false(self):
        a, b = self.two()
        self.server.stop()
        for path in self.tmp.rglob("layout.json"):
            data = json.loads(path.read_text())
            for entry in data["characters"]:
                entry["auto"] = 1
            path.write_text(json.dumps(data))
        self.server.start()
        self.assertFalse(any(c["auto"] for c in self.state()["characters"]))


class AutoCycleCli(ServerCase):
    def run_cli(self, *args):
        env = {"PATH": os.environ.get("PATH", ""), "HOME": str(self.tmp), "SPINEWALL_PORT": str(self.server.port)}
        return subprocess.run([str(CLI), *args], env=env, capture_output=True, text=True, timeout=20)

    def test_cli_toggles_single_mode(self):
        done = self.run_cli("auto-cycle", "on")
        self.assertEqual(done.returncode, 0, done.stderr)
        self.assertIs(self.state()["auto"], True)

    def test_cli_toggles_one_character_by_uid(self):
        self.cmd("add-character", "gamma")
        uid = self.state()["characters"][1]["uid"]
        done = self.run_cli("auto-cycle", uid, "on")
        self.assertEqual(done.returncode, 0, done.stderr)
        self.assertEqual([c["auto"] for c in self.state()["characters"]], [False, True])

    def test_cli_with_a_bad_value_changes_nothing(self):
        before = self.state()
        self.run_cli("auto-cycle", "maybe")
        self.assertEqual(self.state(), before)


if __name__ == "__main__":
    unittest.main()
