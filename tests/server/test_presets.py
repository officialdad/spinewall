"""Presets: save, load, replace, delete, folding, missing models, persistence, validation, and the CLI."""

import json
import subprocess
import urllib.parse
import unittest

from tests.server.support import CLI, ServerCase, cli_path


class Presets(ServerCase):
    def presets(self):
        return self.get("/prefs").json["presets"]

    def layout_two(self):
        # `replace` sets exact values, so a restore is easy to tell from the starting layout.
        self.post(
            "/layout",
            {
                "replace": True,
                "characters": [
                    {"model": "beta", "x": -0.4, "y": 0.1, "scale": 1.5, "auto": True},
                    {"model": "gamma", "x": 0.3, "y": -0.2, "scale": 0.5},
                ],
            },
        )

    def test_starts_empty(self):
        self.assertEqual(self.presets(), [])
        self.assertEqual(self.get("/presets").json, [])

    def test_saves_a_multi_character_layout_without_uids(self):
        self.layout_two()
        self.cmd("save-preset", "duo")
        [preset] = self.presets()
        self.assertEqual(preset["name"], "duo")
        self.assertEqual([c["model"] for c in preset["characters"]], ["beta", "gamma"])
        self.assertEqual(preset["characters"][0]["scale"], 1.5)
        self.assertNotIn("uid", preset["characters"][0])
        self.assertEqual(self.get("/presets").json, [{"name": "duo", "characters": 2}])

    def test_saving_does_not_touch_the_state(self):
        self.layout_two()
        before = self.state()
        self.cmd("save-preset", "duo")
        self.assertEqual(self.state(), before)

    def test_loads_a_multi_character_layout_with_new_uids(self):
        self.layout_two()
        self.cmd("save-preset", "duo")
        saved = self.state()["characters"]
        self.cmd("clear-characters")
        state = self.cmd("load-preset", "duo")
        self.assertEqual([(c["model"], c["x"], c["y"], c["scale"], c["auto"]) for c in state["characters"]],
                         [(c["model"], c["x"], c["y"], c["scale"], c["auto"]) for c in saved])
        self.assertTrue({c["uid"] for c in state["characters"]}.isdisjoint(c["uid"] for c in saved))

    def test_load_bumps_crev_even_when_the_layout_is_the_same(self):
        self.layout_two()
        self.cmd("save-preset", "duo")
        crev = self.state()["crev"]
        self.assertGreater(self.cmd("load-preset", "duo")["crev"], crev)

    def test_single_character_roundtrip(self):
        self.cmd("set-model", "gamma")
        self.report("gamma", ["idle", "wave"])
        self.cmd("set-anim", "wave")
        self.post("/view", {"scale": 2.0, "x": 0.25, "y": -0.1})
        self.cmd("save-preset", "solo")
        self.cmd("set-model", "beta")
        self.post("/view", {"scale": 1.0, "x": 0, "y": 0})
        before = self.state()
        state = self.cmd("load-preset", "solo")
        self.assertEqual((state["model"], state["animation"]), ("gamma", "wave"))
        self.assertEqual((state["scale"], state["x"], state["y"]), (2.0, 0.25, -0.1))
        self.assertEqual(state["characters"], [])
        self.assertGreater(state["rev"], before["rev"])
        self.assertGreater(state["vrev"], before["vrev"])

    def test_single_preset_replaces_a_multi_layout(self):
        self.cmd("set-model", "gamma")
        self.cmd("save-preset", "solo")
        self.layout_two()
        state = self.cmd("load-preset", "solo")
        self.assertEqual(state["characters"], [])
        self.assertEqual(state["model"], "gamma")

    def test_multi_preset_replaces_a_single_layout(self):
        self.layout_two()
        self.cmd("save-preset", "duo")
        self.cmd("clear-characters")
        self.assertEqual(self.cmd("load-preset", "duo")["characters"][0]["model"], "beta")

    def test_same_name_replaces_in_place(self):
        self.cmd("save-preset", "a")
        self.cmd("save-preset", "b")
        self.layout_two()
        self.cmd("save-preset", "a")
        self.assertEqual([p["name"] for p in self.presets()], ["a", "b"])
        self.assertEqual(len(self.presets()[0]["characters"]), 2)

    def test_name_is_trimmed(self):
        self.cmd("save-preset", "  desk  ")
        self.assertEqual([p["name"] for p in self.presets()], ["desk"])

    def test_name_with_slash_and_spaces_roundtrips(self):
        self.cmd("save-preset", "a/b c")
        self.assertEqual([p["name"] for p in self.presets()], ["a/b c"])
        self.layout_two()
        self.assertEqual(self.cmd("load-preset", "a/b c")["characters"], [])

    def test_bad_names_are_rejected(self):
        for name in ("x" * 41, "tab\there", "line\nbreak", "   "):
            with self.subTest(name=name):
                self.get("/cmd/save-preset/" + urllib.parse.quote(name, safe=""))
                self.assertEqual(self.presets(), [])

    def test_a_preset_list_is_capped(self):
        for i in range(52):
            self.cmd("save-preset", f"p{i}")
        self.assertEqual(len(self.presets()), 50)
        self.cmd("save-preset", "p0")
        self.assertEqual(len(self.presets()), 50)

    def test_delete_removes_one_preset(self):
        self.cmd("save-preset", "a")
        self.cmd("save-preset", "b")
        self.cmd("delete-preset", "a")
        self.assertEqual([p["name"] for p in self.presets()], ["b"])

    def test_unknown_name_changes_nothing(self):
        self.layout_two()
        before = self.state()
        self.cmd("load-preset", "nope")
        self.cmd("delete-preset", "nope")
        self.assertEqual(self.state(), before)

    def test_load_skips_a_model_that_is_gone(self):
        self.layout_two()
        self.cmd("save-preset", "duo")
        (self.server.models / "gamma").rename(self.server.models / "gamma-gone")
        self.get("/rescan")
        state = self.cmd("load-preset", "duo")
        # One model left is the single-character layout.
        self.assertEqual(state["characters"], [])
        self.assertEqual(state["model"], "beta")
        self.assertEqual(state["scale"], 1.5)

    def test_load_with_every_model_gone_changes_nothing(self):
        self.cmd("set-model", "gamma")
        self.cmd("save-preset", "solo")
        self.cmd("set-model", "beta")
        (self.server.models / "gamma").rename(self.server.models / "gamma-gone")
        self.get("/rescan")
        before = self.state()
        self.cmd("load-preset", "solo")
        self.assertEqual(self.state(), before)

    def test_a_preset_keeps_a_missing_model_for_later(self):
        self.cmd("set-model", "gamma")
        self.cmd("save-preset", "solo")
        self.server.stop()
        (self.server.models / "gamma").rename(self.server.models / "gamma-gone")
        self.server.start()
        self.assertEqual(self.presets()[0]["characters"][0]["model"], "gamma")

    def test_presets_survive_a_restart(self):
        self.layout_two()
        self.cmd("save-preset", "duo")
        self.server.restart()
        self.assertEqual(self.presets()[0]["name"], "duo")
        self.assertEqual(len(self.cmd("load-preset", "duo")["characters"]), 2)

    def test_load_records_a_recent_for_a_single_character(self):
        self.cmd("set-model", "gamma")
        self.cmd("save-preset", "solo")
        self.cmd("set-model", "beta")
        self.cmd("load-preset", "solo")
        self.assertEqual(self.get("/prefs").json["recent"][0], "gamma")

    def test_corrupt_stored_presets_are_dropped(self):
        self.server.stop()
        junk = [
            "text", 7, None, {"name": "no chars"}, {"name": "", "characters": [{"model": "beta"}]},
            {"name": "x" * 41, "characters": [{"model": "beta"}]}, {"name": "ta\tb", "characters": [{"model": "beta"}]},
            {"name": "empty", "characters": []}, {"name": 5, "characters": [{"model": "beta"}]},
            {"name": "bad model", "characters": [{"model": 5}, {"model": ""}, "x"]},
            {"name": "good", "characters": [{"model": "beta", "x": "oops", "scale": 99, "auto": 1}]},
            {"name": "good", "characters": [{"model": "gamma"}]},
        ]
        self.server.prefs_file.write_text(json.dumps({"presets": junk}))
        self.server.start()
        self.assertEqual([p["name"] for p in self.presets()], ["good"])
        preset = self.presets()[0]
        self.assertEqual(preset["characters"], [{"model": "beta", "animation": None, "x": 0.0, "y": 0.0, "scale": 8.0, "auto": False}])

    def test_a_non_list_presets_value_is_ignored(self):
        self.server.stop()
        self.server.prefs_file.write_text(json.dumps({"presets": {"a": 1}}))
        self.server.start()
        self.assertEqual(self.presets(), [])

    def test_prefs_post_cannot_write_presets(self):
        resp = self.post("/prefs", {"presets": [{"name": "x", "characters": [{"model": "beta"}]}], "sort": "recent"})
        self.assertEqual(resp.status, 200)
        self.assertEqual(self.presets(), [])

    def test_favorites_and_layout_pushes_leave_presets_alone(self):
        self.cmd("save-preset", "a")
        self.post("/fav/beta")
        self.post("/prefs", {"sort": "recent"})
        self.assertEqual([p["name"] for p in self.presets()], ["a"])


class PresetCli(ServerCase):
    def run_cli(self, *args):
        env = {"PATH": cli_path(), "HOME": str(self.tmp), "SPINEWALL_PORT": str(self.server.port)}
        return subprocess.run([str(CLI), *args], env=env, capture_output=True, text=True, timeout=20)

    def test_save_list_load_delete(self):
        self.cmd("set-model", "gamma")
        self.assertEqual(self.run_cli("save-preset", "my desk").returncode, 0)
        listed = self.run_cli("presets")
        self.assertEqual(json.loads(listed.stdout), [{"name": "my desk", "characters": 1}])
        self.cmd("set-model", "beta")
        self.assertEqual(self.run_cli("load-preset", "my desk").returncode, 0)
        self.assertEqual(self.state()["model"], "gamma")
        self.assertEqual(self.run_cli("delete-preset", "my desk").returncode, 0)
        self.assertEqual(json.loads(self.run_cli("presets").stdout), [])


if __name__ == "__main__":
    unittest.main()
