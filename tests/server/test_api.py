"""HTTP API: commands, characters, prefs, thumbnails, reports, persistence, rescan."""

import base64
import json
import threading
import unittest

from tests.server.support import PNG_1X1, ServerCase, SharedServerCase, make_model


class ReadEndpoints(SharedServerCase):
    def test_models_lists_scanned_models(self):
        ids = [m["id"] for m in self.get("/models").json]
        self.assertIn("alpha", ids)
        self.assertNotIn("old", ids)
        self.assertEqual(ids, sorted(ids))

    def test_state_has_the_shared_keys(self):
        state = self.get("/state").json
        for key in ("model", "animation", "rev", "scale", "x", "y", "vrev", "characters", "crev", "edit"):
            self.assertIn(key, state)

    def test_default_model_is_the_first_character(self):
        self.assertEqual(self.get("/state").json["model"], "alpha")

    def test_info_reports_skipped_and_counts(self):
        info = self.get("/info").json
        self.assertEqual(info["models"], len(self.get("/models").json))
        self.assertIn("old", info["skipped"])
        self.assertIn("input", info)
        self.assertEqual(set(info["input"]), {"wheel", "pointerdown", "pointermove", "pointerup", "fired", "keydown"})

    def test_prefs_defaults(self):
        prefs = self.get("/prefs").json
        self.assertEqual(prefs["layout"], "grid")
        self.assertEqual(prefs["sort"], "name")
        self.assertEqual(prefs["kind"], "character")
        self.assertIs(prefs["variants"], True)
        self.assertEqual(prefs["favorites"], [])

    def test_unknown_get_path_is_404_json(self):
        resp = self.get("/nope")
        self.assertEqual(resp.status, 404)
        self.assertEqual(resp.json, {"error": "not found"})

    def test_cache_control_header(self):
        self.assertEqual(self.get("/state").header("Cache-Control"), "no-cache")


class ViewCommands(ServerCase):
    def test_zoom_in_out_and_reset(self):
        before = self.state()
        after = self.cmd("zoom-in")
        self.assertAlmostEqual(after["scale"], before["scale"] * 1.1)
        self.assertEqual(after["vrev"], before["vrev"] + 1)
        self.assertAlmostEqual(self.cmd("zoom-out")["scale"], before["scale"])
        self.cmd("move-left")
        reset = self.cmd("reset-view")
        self.assertEqual((reset["scale"], reset["x"], reset["y"]), (1.0, 0.0, 0.0))

    def test_zoom_is_clamped(self):
        for _ in range(60):
            state = self.cmd("zoom-in")
        self.assertEqual(state["scale"], 8.0)
        for _ in range(120):
            state = self.cmd("zoom-out")
        self.assertEqual(state["scale"], 0.3)

    def test_moves(self):
        self.assertAlmostEqual(self.cmd("move-left")["x"], -0.05)
        self.assertAlmostEqual(self.cmd("move-right")["x"], 0.0)
        self.assertAlmostEqual(self.cmd("move-up")["y"], -0.05)
        self.assertAlmostEqual(self.cmd("move-down")["y"], 0.0)

    def test_move_is_clamped(self):
        for _ in range(60):
            state = self.cmd("move-right")
        self.assertEqual(state["x"], 2.0)

    def test_view_post_sets_and_clamps(self):
        self.post("/view", {"scale": 99, "x": -9, "y": 0.5})
        state = self.state()
        self.assertEqual((state["scale"], state["x"], state["y"]), (8.0, -2.0, 0.5))

    def test_view_post_bad_values_keep_the_current_ones(self):
        self.post("/view", {"scale": 2.0, "x": 0.25})
        for bad in ("nan", "abc", None, [], "inf"):
            resp = self.post("/view", {"scale": bad})
            self.assertEqual(resp.status, 200)
        state = self.state()
        self.assertEqual(state["x"], 0.25)
        self.assertTrue(0.3 <= state["scale"] <= 8.0)
        self.assertEqual(state["scale"], state["scale"])

    def test_view_post_bumps_vrev_not_rev(self):
        before = self.state()
        self.post("/view", {"x": 0.1})
        after = self.state()
        self.assertEqual(after["vrev"], before["vrev"] + 1)
        self.assertEqual(after["rev"], before["rev"])


class ModelAndAnimationCommands(ServerCase):
    def test_set_model(self):
        before = self.state()
        state = self.cmd("set-model", "gamma")
        self.assertEqual(state["model"], "gamma")
        self.assertEqual(state["rev"], before["rev"] + 1)
        self.assertIsNone(state["animation"])

    def test_set_model_same_model_is_a_no_op(self):
        before = self.state()
        self.assertEqual(self.cmd("set-model", before["model"])["rev"], before["rev"])

    def test_set_model_unknown_id_is_ignored(self):
        before = self.state()
        self.assertEqual(self.cmd("set-model", "nope")["model"], before["model"])
        self.assertEqual(self.cmd("set-model", "../etc")["model"], before["model"])

    def test_next_and_prev_model_wrap(self):
        ids = self.model_ids()
        self.assertEqual(self.cmd("next-model")["model"], ids[1])
        self.assertEqual(self.cmd("prev-model")["model"], ids[0])
        self.assertEqual(self.cmd("prev-model")["model"], ids[-1])
        self.assertEqual(self.cmd("next-model")["model"], ids[0])

    def test_random_model_picks_a_known_model(self):
        for _ in range(5):
            self.assertIn(self.cmd("random-model")["model"], self.model_ids())

    def test_animation_commands_need_a_report(self):
        before = self.state()
        self.assertEqual(self.cmd("next-anim")["rev"], before["rev"])

    def test_next_prev_random_anim(self):
        self.report("alpha", ["idle", "walk", "run"])
        self.assertEqual(self.cmd("next-anim")["animation"], "idle")
        self.assertEqual(self.cmd("next-anim")["animation"], "walk")
        self.assertEqual(self.cmd("prev-anim")["animation"], "idle")
        self.assertEqual(self.cmd("prev-anim")["animation"], "run")
        self.assertIn(self.cmd("random-anim")["animation"], ["idle", "walk", "run"])

    def test_set_anim_and_length_cap(self):
        self.assertEqual(self.cmd("set-anim", "walk fast #1")["animation"], "walk fast #1")
        self.assertEqual(len(self.cmd("set-anim", "x" * 500)["animation"]), 200)

    def test_report_records_the_playing_animation_once(self):
        self.report("alpha", ["idle", "walk"], playing="idle")
        self.assertEqual(self.state()["animation"], "idle")
        self.report("alpha", ["idle", "walk"], playing="walk")
        self.assertEqual(self.state()["animation"], "idle")

    def test_report_does_not_bump_rev(self):
        before = self.state()["rev"]
        self.report("alpha", ["idle"], playing="idle")
        self.assertEqual(self.state()["rev"], before)

    def test_unknown_command_changes_nothing(self):
        before = self.state()
        before.pop("edit")
        self.assertEqual(self.cmd("bogus"), before)
        self.assertEqual(self.cmd("bogus", "arg"), before)

    def test_percent_encoded_arg_round_trips(self):
        self.assertEqual(self.cmd("set-anim", "a b/c")["animation"], "a b/c")


class Recents(ServerCase):
    def test_model_changes_record_recents_newest_first(self):
        self.cmd("set-model", "beta")
        self.cmd("set-model", "gamma")
        self.cmd("set-model", "beta")
        self.assertEqual(self.get("/prefs").json["recent"][:2], ["beta", "gamma"])

    def test_recents_are_capped(self):
        ids = [i for i in self.model_ids()]
        for model_id in ids * 3:
            self.cmd("set-model", model_id)
        self.assertLessEqual(len(self.get("/prefs").json["recent"]), 20)

    def test_recents_do_not_bump_state_revisions(self):
        self.cmd("set-model", "beta")
        rev = self.state()["rev"]
        self.post("/fav/beta")
        self.assertEqual(self.state()["rev"], rev)


class Prefs(ServerCase):
    def test_favorite_toggles(self):
        self.assertEqual(self.post("/fav/alpha").json["favorites"], ["alpha"])
        self.assertEqual(self.post("/fav/alpha").json["favorites"], [])

    def test_favorite_unknown_model_is_404(self):
        self.assertEqual(self.post("/fav/nope").status, 404)

    def test_favorite_accepts_an_encoded_id(self):
        self.assertEqual(self.post("/fav/hero_01").status, 200)

    def test_prefs_set_valid_values(self):
        prefs = self.post("/prefs", {"layout": "list", "sort": "recent", "kind": "all", "variants": False}).json
        self.assertEqual((prefs["layout"], prefs["sort"], prefs["kind"], prefs["variants"]), ("list", "recent", "all", False))

    def test_prefs_reject_unknown_values(self):
        self.assertEqual(self.post("/prefs", {"layout": "tiles"}).status, 400)
        self.assertEqual(self.post("/prefs", {"variants": "yes"}).status, 400)
        self.assertEqual(self.post("/prefs", {}).status, 400)
        self.assertEqual(self.get("/prefs").json["layout"], "grid")

    def test_prefs_persist_across_a_restart(self):
        self.post("/fav/beta")
        self.post("/prefs", {"sort": "source"})
        self.server.restart()
        prefs = self.get("/prefs").json
        self.assertEqual(prefs["favorites"], ["beta"])
        self.assertEqual(prefs["sort"], "source")

    def test_prefs_file_is_valid_json_after_a_write(self):
        self.post("/fav/beta")
        self.assertEqual(json.loads(self.server.prefs_file.read_text())["favorites"], ["beta"])

    def test_no_temp_files_are_left_behind(self):
        for _ in range(5):
            self.post("/fav/beta")
        leftovers = [p.name for p in self.server.state_dir.iterdir() if p.name.endswith(".tmp")]
        self.assertEqual(leftovers, [])


class Characters(ServerCase):
    def uids(self):
        return [c["uid"] for c in self.state()["characters"]]

    def test_add_character_starts_multi_mode_with_two_characters(self):
        state = self.cmd("add-character", "gamma")
        self.assertEqual(len(state["characters"]), 2)
        self.assertEqual(state["characters"][0]["model"], "alpha")
        self.assertEqual(state["characters"][1]["model"], "gamma")
        self.assertEqual(state["crev"], 1)

    def test_first_character_keeps_the_single_layout_position(self):
        self.post("/view", {"x": 0.3, "y": -0.2, "scale": 2})
        first = self.cmd("add-character", "gamma")["characters"][0]
        self.assertEqual((first["x"], first["y"], first["scale"]), (0.3, -0.2, 2.0))

    def test_new_characters_do_not_stack_on_each_other(self):
        for _ in range(5):
            state = self.cmd("add-character", "gamma")
        xs = [c["x"] for c in state["characters"][1:]]
        self.assertEqual(len(set(xs)), len(xs))

    def test_add_character_with_unknown_model_uses_the_current_one(self):
        state = self.cmd("add-character", "nope")
        self.assertEqual(state["characters"][1]["model"], "alpha")

    def test_add_character_without_an_arg_uses_the_current_model(self):
        self.assertEqual(self.cmd("add-character")["characters"][1]["model"], "alpha")

    def test_uids_are_unique_and_well_formed(self):
        for _ in range(10):
            self.cmd("add-character", "beta")
        uids = self.uids()
        self.assertEqual(len(uids), len(set(uids)))
        for uid in uids:
            self.assertRegex(uid, r"^[0-9a-f]{6}$")

    def test_remove_down_to_one_folds_back_into_the_single_keys(self):
        self.cmd("add-character", "gamma")
        first, second = self.uids()
        before = self.state()
        state = self.cmd("remove-character", first)
        self.assertEqual(state["characters"], [])
        self.assertEqual(state["model"], "gamma")
        self.assertEqual(state["scale"], 0.7)
        self.assertGreater(state["rev"], before["rev"])
        self.assertGreater(state["vrev"], before["vrev"])
        self.assertGreater(state["crev"], before["crev"])

    def test_remove_with_three_leaves_two(self):
        self.cmd("add-character", "gamma")
        self.cmd("add-character", "beta")
        first = self.uids()[0]
        state = self.cmd("remove-character", first)
        self.assertEqual(len(state["characters"]), 2)
        self.assertNotIn(first, [c["uid"] for c in state["characters"]])

    def test_remove_unknown_uid_changes_nothing(self):
        self.cmd("add-character", "gamma")
        before = self.state()
        before.pop("edit")
        self.assertEqual(self.cmd("remove-character", "zzzzzz"), before)

    def test_clear_characters(self):
        self.cmd("add-character", "gamma")
        state = self.cmd("clear-characters")
        self.assertEqual(state["characters"], [])

    def test_clear_with_none_does_not_bump_crev(self):
        before = self.state()["crev"]
        self.assertEqual(self.cmd("clear-characters")["crev"], before)

    def test_set_char_model_clears_its_animation(self):
        self.cmd("add-character", "gamma")
        uid = self.uids()[1]
        self.cmd("set-char-anim", uid, "wave")
        state = self.cmd("set-char-model", uid, "beta")
        char = next(c for c in state["characters"] if c["uid"] == uid)
        self.assertEqual((char["model"], char["animation"]), ("beta", None))

    def test_set_char_model_unknown_model_is_ignored(self):
        self.cmd("add-character", "gamma")
        uid = self.uids()[1]
        state = self.cmd("set-char-model", uid, "nope")
        self.assertEqual(next(c for c in state["characters"] if c["uid"] == uid)["model"], "gamma")

    def test_set_char_anim_caps_length(self):
        self.cmd("add-character", "gamma")
        uid = self.uids()[1]
        state = self.cmd("set-char-anim", uid, "y" * 500)
        self.assertEqual(len(next(c for c in state["characters"] if c["uid"] == uid)["animation"]), 200)

    def test_cycle_character_animation(self):
        self.cmd("add-character", "gamma")
        uid = self.uids()[1]
        self.report("gamma", ["idle", "walk"])
        # With no animation set, the default one ("idle") counts as the current one, so next moves past it.
        state = self.cmd("next-char-anim", uid)
        self.assertEqual(next(c for c in state["characters"] if c["uid"] == uid)["animation"], "walk")
        state = self.cmd("next-char-anim", uid)
        self.assertEqual(next(c for c in state["characters"] if c["uid"] == uid)["animation"], "idle")
        state = self.cmd("prev-char-anim", uid)
        self.assertEqual(next(c for c in state["characters"] if c["uid"] == uid)["animation"], "walk")

    def test_cycle_without_a_report_does_nothing(self):
        self.cmd("add-character", "gamma")
        crev = self.state()["crev"]
        self.assertEqual(self.cmd("next-char-anim", self.uids()[1])["crev"], crev)

    def test_layout_persists_across_a_restart(self):
        self.cmd("add-character", "gamma")
        before = self.state()
        self.server.restart()
        after = self.state()
        self.assertEqual(after["characters"], before["characters"])
        self.assertEqual(after["crev"], before["crev"])

    def test_layout_lives_in_its_own_file(self):
        self.cmd("add-character", "gamma")
        self.assertEqual(len(json.loads(self.server.layout_file.read_text())["characters"]), 2)
        self.cmd("zoom-in")
        self.assertNotIn("characters", json.loads(self.server.state_file.read_text()))


class LayoutPost(ServerCase):
    def setUp(self):
        super().setUp()
        self.cmd("add-character", "gamma")
        self.chars = self.state()["characters"]

    def test_push_moves_characters_and_bumps_crev(self):
        crev = self.state()["crev"]
        moved = [{"uid": self.chars[0]["uid"], "x": 0.9}]
        out = self.post("/layout", {"characters": moved}).json
        self.assertEqual(out["characters"][0]["x"], 0.9)
        self.assertEqual(out["crev"], crev + 1)

    def test_push_keeps_characters_it_does_not_mention(self):
        out = self.post("/layout", {"characters": [{"uid": self.chars[0]["uid"], "x": 0.4}]}).json
        self.assertEqual(len(out["characters"]), 2)

    def test_push_cannot_resurrect_a_removed_character(self):
        stale = list(self.chars)
        self.cmd("add-character", "beta")
        self.cmd("remove-character", self.chars[1]["uid"])
        out = self.post("/layout", {"characters": stale}).json
        self.assertNotIn(self.chars[1]["uid"], [c["uid"] for c in out["characters"]])

    def test_push_cannot_overwrite_model_changes_made_by_a_command(self):
        uid = self.chars[1]["uid"]
        self.cmd("set-char-model", uid, "beta")
        out = self.post("/layout", {"characters": [{"uid": uid, "model": "gamma", "x": 0.1}]}).json
        self.assertEqual(next(c for c in out["characters"] if c["uid"] == uid)["model"], "beta")

    def test_replace_swaps_the_whole_layout(self):
        entries = [{"model": "beta", "x": 0.1}, {"model": "gamma", "x": -0.1}, {"model": "bogus"}]
        out = self.post("/layout", {"characters": entries, "replace": True}).json
        self.assertEqual([c["model"] for c in out["characters"]], ["beta", "gamma"])
        self.assertTrue(all(c["uid"] for c in out["characters"]))

    def test_push_order_sets_z_order(self):
        a, b = self.chars
        out = self.post("/layout", {"characters": [{"uid": b["uid"]}, {"uid": a["uid"]}]}).json
        self.assertEqual([c["uid"] for c in out["characters"]], [b["uid"], a["uid"]])

    def test_unchanged_push_does_not_bump_crev(self):
        crev = self.state()["crev"]
        self.post("/layout", {"characters": self.chars})
        self.assertEqual(self.state()["crev"], crev)

    def test_push_clamps_values(self):
        out = self.post("/layout", {"characters": [{"uid": self.chars[0]["uid"], "scale": 99, "x": "bad"}]}).json
        char = out["characters"][0]
        self.assertEqual(char["scale"], 8.0)
        self.assertEqual(char["x"], 0.0)

    def test_bad_layout_bodies(self):
        self.assertEqual(self.post("/layout", {"characters": "no"}).status, 400)
        self.assertEqual(self.post("/layout", {}).status, 400)
        out = self.post("/layout", {"characters": [5, None, "x", {"uid": "../x", "model": "gamma"}], "replace": True}).json
        self.assertEqual(out["characters"], [])


class ReportAndInput(ServerCase):
    def test_report_validation(self):
        bad = [
            {"model": ["alpha"], "animations": []},
            {"model": "nope", "animations": []},
            {"model": "alpha", "animations": "idle"},
            {"model": "alpha", "animations": [1]},
            {"model": "alpha", "animations": [""]},
            {"model": "alpha", "animations": ["x" * 201]},
            {"model": "alpha", "animations": ["a"] * 501},
            {},
        ]
        for body in bad:
            with self.subTest(body=str(body)[:60]):
                self.assertEqual(self.post("/report", body).status, 400)

    def test_report_accepts_the_limits(self):
        self.assertEqual(self.report("alpha", ["a"] * 500).status, 200)
        self.assertEqual(self.report("alpha", ["x" * 200]).status, 200)

    def test_report_feeds_the_info_animation_list(self):
        self.report("alpha", ["idle", "walk"])
        self.assertEqual(self.get("/info").json["animations"], ["idle", "walk"])

    def test_input_counters_add_up(self):
        self.post("/input", {"wheel": 3, "fired": 1})
        self.post("/input", {"wheel": 2})
        info = self.get("/info").json["input"]
        self.assertEqual((info["wheel"], info["fired"], info["keydown"]), (5, 1, 0))

    def test_input_rejects_bad_values_without_crashing(self):
        self.post("/input", {"wheel": "x", "fired": True, "keydown": -5, "pointerdown": 1.5, "pointerup": [1]})
        self.assertEqual(set(self.get("/info").json["input"].values()), {0})

    def test_input_step_is_capped(self):
        self.post("/input", {"wheel": 10**9})
        self.assertEqual(self.get("/info").json["input"]["wheel"], 1000)

    def test_unknown_post_path_is_404(self):
        self.assertEqual(self.post("/nope", {}).status, 404)

    def test_edit_mode_toggle(self):
        self.assertEqual(self.get("/edit/1").json, {"on": 1})
        self.assertEqual(self.state()["edit"], 1)
        self.assertEqual(self.get("/edit/0").json, {"on": 0})
        self.assertEqual(self.state()["edit"], 0)

    def test_edit_mode_does_not_persist_or_bump_revisions(self):
        before = self.state()
        self.get("/edit/1")
        after = self.state()
        self.assertEqual((after["rev"], after["vrev"], after["crev"]), (before["rev"], before["vrev"], before["crev"]))


class Thumbnails(ServerCase):
    def upload(self, model_id, data, **kw):
        return self.post(f"/thumb/{model_id}", {"png": base64.b64encode(data).decode()}, **kw)

    def test_upload_then_fetch(self):
        self.assertEqual(self.upload("alpha", PNG_1X1).status, 200)
        resp = self.get("/thumb/alpha.png")
        self.assertEqual(resp.status, 200)
        self.assertEqual(resp.header("Content-Type"), "image/png")
        self.assertEqual(resp.body, PNG_1X1)

    def test_missing_thumbnail_is_404(self):
        self.assertEqual(self.get("/thumb/alpha.png").status, 404)

    def test_unknown_model_is_rejected_both_ways(self):
        self.assertEqual(self.upload("nope", PNG_1X1).status, 400)
        self.assertEqual(self.get("/thumb/nope.png").status, 404)

    def test_traversal_in_the_id_is_rejected(self):
        self.assertEqual(self.upload("..%2F..%2Fx", PNG_1X1).status, 400)
        self.assertEqual(self.get("/thumb/..%2F..%2Fstate.png").status, 404)
        self.assertFalse((self.server.cache_dir.parent / "x.png").exists())

    def test_non_png_is_rejected(self):
        self.assertEqual(self.upload("alpha", b"GIF89a....").status, 400)

    def test_oversize_is_rejected(self):
        big = PNG_1X1 + b"\0" * (512 * 1024)
        self.assertEqual(self.upload("alpha", big).status, 400)

    def test_bad_base64_and_wrong_types(self):
        self.assertEqual(self.post("/thumb/alpha", {"png": "***not base64***"}).status, 400)
        self.assertEqual(self.post("/thumb/alpha", {"png": 5}).status, 400)
        self.assertEqual(self.post("/thumb/alpha", {}).status, 400)

    def test_upload_survives_a_restart(self):
        self.upload("alpha", PNG_1X1)
        self.server.restart()
        self.assertEqual(self.get("/thumb/alpha.png").body, PNG_1X1)


class Rescan(ServerCase):
    def test_rescan_picks_up_a_new_model(self):
        make_model(self.server.models, "newcomer", "4.1.56")
        self.assertNotIn("newcomer", self.model_ids())
        out = self.get("/rescan").json
        self.assertIn("newcomer", [m["id"] for m in out])
        self.assertIn("newcomer", self.model_ids())

    def test_rescan_moves_the_selection_off_a_removed_model(self):
        self.cmd("set-model", "gamma")
        import shutil

        shutil.rmtree(self.server.models / "gamma")
        before = self.state()
        self.get("/rescan")
        after = self.state()
        self.assertEqual(after["model"], "alpha")
        self.assertGreater(after["rev"], before["rev"])

    def test_rescan_drops_characters_of_removed_models(self):
        self.cmd("add-character", "gamma")
        import shutil

        shutil.rmtree(self.server.models / "gamma")
        self.get("/rescan")
        self.assertEqual([c["model"] for c in self.state()["characters"]], ["alpha"])

    def test_rescan_reports_new_skips(self):
        make_model(self.server.models, "broken", "9.9.9")
        self.get("/rescan")
        self.assertIn("broken", self.get("/info").json["skipped"])

    def test_rescan_with_everything_removed_does_not_crash(self):
        import shutil

        for child in self.server.models.iterdir():
            shutil.rmtree(child)
        self.assertEqual(self.get("/rescan").json, [])
        self.assertEqual(self.get("/state").status, 200)
        self.assertEqual(self.get("/cmd/next-model").status, 200)


class EmptyModels(ServerCase):
    models_builder = None

    def test_server_runs_with_no_models(self):
        self.assertEqual(self.get("/models").json, [])
        self.assertIsNone(self.state()["model"])
        for name in ("next-model", "zoom-in", "add-character", "set-anim"):
            self.assertEqual(self.get(f"/cmd/{name}/x").status, 200)

    def test_missing_models_directory_is_fine(self):
        import shutil

        shutil.rmtree(self.server.models)
        self.server.restart()
        self.assertEqual(self.get("/models").json, [])


class Concurrency(ServerCase):
    def test_parallel_adds_lose_no_character(self):
        errors = []

        def add():
            try:
                self.get("/cmd/add-character/gamma")
            except Exception as e:  # noqa: BLE001
                errors.append(e)

        threads = [threading.Thread(target=add) for _ in range(12)]
        for t in threads:
            t.start()
        for t in threads:
            t.join()
        self.assertEqual(errors, [])
        state = self.state()
        self.assertEqual(len(state["characters"]), 13)
        self.assertEqual(state["crev"], 12)
        uids = [c["uid"] for c in state["characters"]]
        self.assertEqual(len(set(uids)), 13)

    def test_parallel_view_changes_keep_the_files_valid(self):
        threads = [threading.Thread(target=lambda: self.get("/cmd/zoom-in")) for _ in range(16)]
        for t in threads:
            t.start()
        for t in threads:
            t.join()
        self.assertEqual(self.state()["vrev"], 16)
        json.loads(self.server.state_file.read_text())

    def test_parallel_favorites_toggle_cleanly(self):
        threads = [threading.Thread(target=lambda: self.post("/fav/alpha")) for _ in range(10)]
        for t in threads:
            t.start()
        for t in threads:
            t.join()
        self.assertEqual(self.get("/prefs").json["favorites"], [])
        self.assertEqual(json.loads(self.server.prefs_file.read_text())["favorites"], [])


if __name__ == "__main__":
    unittest.main()
