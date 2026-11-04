"""Screens: one stage per monitor, a preset per stage, and preset cycling."""

import json
import subprocess
import unittest
import urllib.parse

from tests.server.support import CLI, ServerCase, cli_path

LEFT, RIGHT = "DP-2", "HDMI-A-1"


class ScreenCase(ServerCase):
    def state_of(self, screen):
        return self.get(f"/state?screen={urllib.parse.quote(screen, safe='')}").json

    def scoped(self, screen, name, *args):
        quoted = [urllib.parse.quote(a, safe="") for a in args]
        resp = self.get("/".join(["/cmd", name, *quoted]) + f"?screen={urllib.parse.quote(screen, safe='')}")
        self.assertEqual(resp.status, 200, resp.body)
        return resp.json

    def preset_of(self, screen):
        return self.state_of(screen)["preset"]


class Screens(ScreenCase):

    def test_a_new_screen_starts_as_a_copy_of_the_default_stage(self):
        self.cmd("set-model", "gamma")
        copy = self.state_of(LEFT)
        self.assertEqual(copy["model"], "gamma")
        self.assertEqual(copy["characters"], [])
        self.assertEqual(self.get("/screens").json, [{"screen": LEFT, "preset": None, "characters": 0}])

    def test_a_named_command_changes_only_that_screen(self):
        self.state_of(LEFT)
        self.state_of(RIGHT)
        self.scoped(LEFT, "set-model", "gamma")
        self.assertEqual(self.state_of(LEFT)["model"], "gamma")
        self.assertNotEqual(self.state_of(RIGHT)["model"], "gamma")
        self.assertNotEqual(self.state()["model"], "gamma")

    def test_a_command_without_a_screen_reaches_every_stage(self):
        self.state_of(LEFT)
        self.state_of(RIGHT)
        self.cmd("set-model", "gamma")
        self.assertEqual([self.state_of(LEFT)["model"], self.state_of(RIGHT)["model"], self.state()["model"]], ["gamma"] * 3)

    def test_an_empty_screen_key_is_the_default_stage(self):
        self.state_of(LEFT)
        self.scoped("", "set-model", "gamma")
        self.assertEqual(self.state()["model"], "gamma")
        self.assertNotEqual(self.state_of(LEFT)["model"], "gamma")

    def test_a_pushed_view_and_layout_stay_on_their_screen(self):
        self.state_of(LEFT)
        self.state_of(RIGHT)
        self.post(f"/view?screen={LEFT}", {"scale": 2.0, "x": 0.5})
        self.post(f"/layout?screen={LEFT}", {"replace": True, "characters": [{"model": "beta"}, {"model": "gamma"}]})
        left, right = self.state_of(LEFT), self.state_of(RIGHT)
        self.assertEqual((left["scale"], left["x"], len(left["characters"])), (2.0, 0.5, 2))
        self.assertEqual((right["scale"], right["x"], len(right["characters"])), (1.0, 0.0, 0))
        self.assertEqual(self.state()["scale"], 1.0)

    def test_a_report_fills_the_animation_of_the_reporting_screen_only(self):
        self.state_of(LEFT)
        self.state_of(RIGHT)
        model = self.state_of(LEFT)["model"]
        resp = self.post(f"/report?screen={LEFT}", {"model": model, "animations": ["idle", "wave"], "playing": "idle"})
        self.assertEqual(resp.status, 200)
        self.assertEqual(self.state_of(LEFT)["animation"], "idle")
        self.assertIsNone(self.state_of(RIGHT)["animation"])

    def test_a_command_for_an_unknown_screen_creates_nothing(self):
        resp = self.get(f"/cmd/set-model/gamma?screen={LEFT}")
        self.assertEqual(resp.status, 404)
        self.assertEqual(self.get("/screens").json, [])

    def test_a_bad_screen_key_is_refused(self):
        for key in ("x" * 61, "a%00b", "a%0Ab"):
            self.assertEqual(self.get(f"/state?screen={key}").status, 400, key)
            self.assertEqual(self.get(f"/cmd/next-model?screen={key}").status, 400, key)
        self.assertEqual(self.get("/screens").json, [])

    def test_the_number_of_screens_is_capped(self):
        for i in range(16):
            self.assertEqual(self.get(f"/state?screen=s{i}").status, 200)
        self.assertEqual(self.get("/state?screen=one-more").status, 400)
        self.assertEqual(self.get("/state?screen=s0").status, 200)
        self.assertEqual(len(self.get("/screens").json), 16)

    def test_screens_survive_a_restart(self):
        self.state_of(LEFT)
        self.scoped(LEFT, "set-model", "gamma")
        self.server.restart()
        self.assertEqual(self.state_of(LEFT)["model"], "gamma")
        self.assertEqual([s["screen"] for s in self.get("/screens").json], [LEFT])

    def test_a_corrupt_screens_file_is_dropped(self):
        self.server.stop()
        (self.server.state_dir / "screens.json").write_text(json.dumps({LEFT: "junk", "": {"model": "beta"}, "a\u0000b": {}}))
        self.server.start()
        self.assertEqual([s["screen"] for s in self.get("/screens").json], [LEFT])
        self.assertIn(self.state_of(LEFT)["model"], self.model_ids())

    def test_a_model_removed_by_rescan_leaves_every_screen(self):
        self.state_of(LEFT)
        self.scoped(LEFT, "set-model", "gamma")
        (self.server.models / "gamma").rename(self.server.models / "gamma-gone")
        self.get("/rescan")
        self.assertNotEqual(self.state_of(LEFT)["model"], "gamma")


class ScreenPresets(ScreenCase):
    def setUp(self):
        super().setUp()
        # The screens exist before the presets, so neither starts with a preset pointer.
        self.state_of(LEFT)
        self.state_of(RIGHT)
        for name, model in (("one", "alpha"), ("two", "beta"), ("three", "gamma")):
            self.cmd("set-model", model)
            self.cmd("save-preset", name)

    def test_each_screen_loads_its_own_preset(self):
        self.scoped(LEFT, "load-preset", "one")
        self.scoped(RIGHT, "load-preset", "two")
        self.assertEqual((self.preset_of(LEFT), self.preset_of(RIGHT)), ("one", "two"))
        self.assertEqual(self.state_of(LEFT)["model"], "alpha")
        self.assertEqual(self.state_of(RIGHT)["model"], "beta")

    def test_next_preset_moves_one_screen_and_wraps(self):
        self.scoped(LEFT, "load-preset", "two")
        self.scoped(RIGHT, "load-preset", "one")
        self.assertEqual(self.scoped(LEFT, "next-preset")["preset"], "three")
        self.assertEqual(self.scoped(LEFT, "next-preset")["preset"], "one")
        self.assertEqual(self.preset_of(RIGHT), "one")

    def test_prev_preset_wraps_to_the_last(self):
        self.scoped(LEFT, "load-preset", "one")
        self.assertEqual(self.scoped(LEFT, "prev-preset")["preset"], "three")
        self.assertEqual(self.scoped(LEFT, "prev-preset")["preset"], "two")

    def test_a_stage_without_a_preset_starts_at_the_first_or_the_last(self):
        self.assertEqual(self.scoped(LEFT, "next-preset")["preset"], "one")
        self.assertEqual(self.scoped(RIGHT, "prev-preset")["preset"], "three")

    def test_next_preset_without_a_screen_moves_every_screen(self):
        self.scoped(LEFT, "load-preset", "one")
        self.scoped(RIGHT, "load-preset", "two")
        self.cmd("next-preset")
        self.assertEqual((self.preset_of(LEFT), self.preset_of(RIGHT)), ("two", "three"))

    def test_cycling_skips_a_preset_whose_models_are_gone(self):
        self.server.stop()
        (self.server.models / "beta").rename(self.server.models / "beta-gone")
        self.server.start()
        self.scoped(LEFT, "load-preset", "one")
        self.assertEqual(self.scoped(LEFT, "next-preset")["preset"], "three")

    def test_cycling_with_no_presets_changes_nothing(self):
        for name in ("one", "two", "three"):
            self.cmd("delete-preset", name)
        before = self.state_of(LEFT)
        before.pop("edit")
        self.assertEqual(self.scoped(LEFT, "next-preset"), before)

    def test_deleting_a_preset_clears_it_from_every_screen(self):
        self.scoped(LEFT, "load-preset", "two")
        self.cmd("delete-preset", "two")
        self.assertIsNone(self.preset_of(LEFT))

    def test_saving_on_a_screen_stores_that_screens_layout(self):
        self.scoped(LEFT, "set-model", "gamma")
        self.scoped(RIGHT, "set-model", "alpha")
        self.scoped(LEFT, "save-preset", "left layout")
        [saved] = [p for p in self.get("/prefs").json["presets"] if p["name"] == "left layout"]
        self.assertEqual(saved["characters"][0]["model"], "gamma")
        self.assertEqual(self.preset_of(LEFT), "left layout")
        self.assertNotEqual(self.preset_of(RIGHT), "left layout")

    def test_a_preset_pointer_survives_a_restart(self):
        self.scoped(LEFT, "load-preset", "two")
        self.server.restart()
        self.assertEqual(self.preset_of(LEFT), "two")
        self.assertEqual(self.scoped(LEFT, "next-preset")["preset"], "three")

    def test_a_multi_character_preset_keeps_the_pointer_on_a_restart(self):
        self.post(f"/layout?screen={LEFT}", {"replace": True, "characters": [{"model": "beta"}, {"model": "gamma"}]})
        self.scoped(LEFT, "save-preset", "pair")
        self.scoped(LEFT, "clear-characters")
        self.scoped(LEFT, "load-preset", "pair")
        self.server.restart()
        self.assertEqual(self.preset_of(LEFT), "pair")
        self.assertEqual(len(self.state_of(LEFT)["characters"]), 2)

    def test_the_default_stage_cycles_like_any_other(self):
        self.assertEqual(self.cmd("next-preset")["preset"], "one")
        self.assertEqual(self.cmd("next-preset")["preset"], "two")
        self.server.restart()
        self.assertEqual(self.state()["preset"], "two")


class ScreenCli(ServerCase):
    def run_cli(self, *args):
        env = {"PATH": cli_path(), "HOME": str(self.tmp), "SPINEWALL_PORT": str(self.server.port)}
        return subprocess.run([str(CLI), *args], env=env, capture_output=True, text=True, timeout=20)

    def test_screens_lists_the_known_screens(self):
        self.get(f"/state?screen={LEFT}")
        self.assertEqual(json.loads(self.run_cli("screens").stdout), [{"screen": LEFT, "preset": None, "characters": 0}])

    def test_screen_flag_scopes_a_command_and_cycles_presets(self):
        self.cmd("set-model", "alpha")
        self.cmd("save-preset", "one")
        self.cmd("set-model", "beta")
        self.cmd("save-preset", "two")
        self.get(f"/state?screen={LEFT}")
        self.get(f"/state?screen={RIGHT}")
        self.assertEqual(self.run_cli("next-preset", "--screen", LEFT).returncode, 0)
        self.assertEqual(self.run_cli("--screen", RIGHT, "load-preset", "two").returncode, 0)
        presets = {s["screen"]: s["preset"] for s in json.loads(self.run_cli("screens").stdout)}
        self.assertEqual(presets, {LEFT: "one", RIGHT: "two"})

    def test_an_unknown_screen_is_an_error(self):
        self.assertNotEqual(self.run_cli("next-preset", "--screen", "nope").returncode, 0)

    def test_screen_flag_without_a_key_is_an_error(self):
        result = self.run_cli("next-preset", "--screen")
        self.assertEqual(result.returncode, 2)
        self.assertIn("--screen needs a screen key", result.stderr)


if __name__ == "__main__":
    unittest.main()
