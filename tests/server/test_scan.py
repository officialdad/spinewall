"""Model scan: version detection, skip reasons, kind, character grouping, model.json overrides."""

import shutil
import tempfile
import unittest
from pathlib import Path

from tests.server.support import binary_skel, build_default_models, load_server_module, make_model


class ScanTestCase(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tmp = Path(tempfile.mkdtemp(prefix="spinewall-scan-"))
        cls.models = cls.tmp / "models"
        cls.models.mkdir()
        build_default_models(cls.models)
        cls.mod = load_server_module(cls.models, cls.tmp)
        cls.by_id = {m["id"]: m for m in cls.mod.MODELS}

    @classmethod
    def tearDownClass(cls):
        shutil.rmtree(cls.tmp, True)


class VersionDetection(ScanTestCase):
    def test_binary_4x_layout(self):
        path = self.tmp / "a.skel"
        for version in ("4.0.31", "4.1.56", "4.2.120"):
            path.write_bytes(binary_skel(version, "4x"))
            self.assertEqual(self.mod.read_binary_version(path), version)

    def test_binary_3x_layout(self):
        path = self.tmp / "a.skel"
        path.write_bytes(binary_skel("3.8.99", "3x"))
        self.assertEqual(self.mod.read_binary_version(path), "3.8.99")

    def test_binary_garbage_gives_none(self):
        path = self.tmp / "a.skel"
        for data in (b"", b"\x01\x02\x03", b"\xff" * 64):
            path.write_bytes(data)
            self.assertIsNone(self.mod.read_binary_version(path))

    def test_json_spine_key(self):
        path = self.tmp / "a.json"
        path.write_text('{"skeleton": {"spine": "4.1.24"}}')
        self.assertEqual(self.mod.read_json_version(path), "4.1.24")

    def test_json_version_key(self):
        path = self.tmp / "a.json"
        path.write_text('{"skeleton": {"version": "3.8.75"}}')
        self.assertEqual(self.mod.read_json_version(path), "3.8.75")

    def test_json_numeric_version_becomes_text(self):
        path = self.tmp / "a.json"
        path.write_text('{"skeleton": {"spine": 4.2}}')
        self.assertEqual(self.mod.read_json_version(path), "4.2")

    def test_json_without_skeleton_header(self):
        path = self.tmp / "a.json"
        for text in ("{}", "[]", '{"skeleton": 5}', '{"skeleton": {}}'):
            path.write_text(text)
            self.assertIsNone(self.mod.read_json_version(path))

    def test_numeric_version_model_loads_instead_of_crashing_the_scan(self):
        models = self.tmp / "numeric"
        make_model(models, "num", fmt="json", spine_value=4.2)
        (models / "num" / "model.json").write_text("{}")
        found, skipped = self._scan(models)
        self.assertEqual([m["id"] for m in found], ["num"])
        self.assertEqual(found[0]["runtime"], "spine42")
        self.assertEqual(skipped, {})

    def _scan(self, models_dir):
        old = self.mod.MODELS_DIR
        self.mod.MODELS_DIR = str(models_dir)
        try:
            return self.mod.scan_models()
        finally:
            self.mod.MODELS_DIR = old


class RuntimeMapping(ScanTestCase):
    def test_each_version_maps_to_its_runtime(self):
        expected = {
            "alpha": ("4.0.31", "spine40"),
            "beta": ("4.1.56", "spine41"),
            "gamma": ("4.2.120", "spine42"),
            "delta": ("3.8.99", "spine38"),
            "legacy37": ("3.7.94", "spine37"),
        }
        for model_id, (version, runtime) in expected.items():
            with self.subTest(model=model_id):
                self.assertEqual(self.by_id[model_id]["version"], version)
                self.assertEqual(self.by_id[model_id]["runtime"], runtime)


class SkipReasons(ScanTestCase):
    def test_skipped_models_are_not_listed(self):
        for model_id in ("old", "legacy37bin", "noatlas", "badjson", "listjson", "sepbad", "missingfile", "garbage"):
            with self.subTest(model=model_id):
                self.assertNotIn(model_id, self.by_id)
                self.assertIn(model_id, self.mod.SKIPPED)

    def test_old_version_reason_names_supported_versions(self):
        reason = self.mod.SKIPPED["old"]
        self.assertIn("3.6.53", reason)
        self.assertIn("3.7.x", reason)
        self.assertIn("3.8.x", reason)
        self.assertIn("4.2.x", reason)

    def test_spine_37_binary_reason_asks_for_json(self):
        self.assertIn("JSON export", self.mod.SKIPPED["legacy37bin"])

    def test_missing_atlas_reason(self):
        self.assertIn("needs one skeleton", self.mod.SKIPPED["noatlas"])

    def test_bad_model_json_is_recorded_not_fatal(self):
        self.assertIn("JSONDecodeError", self.mod.SKIPPED["badjson"])

    def test_model_json_must_be_an_object(self):
        self.assertIn("must be a JSON object", self.mod.SKIPPED["listjson"])

    def test_path_separator_in_override_is_rejected(self):
        self.assertIn("must be a file name", self.mod.SKIPPED["sepbad"])

    def test_override_naming_a_missing_file(self):
        self.assertIn("does not exist", self.mod.SKIPPED["missingfile"])

    def test_unreadable_skeleton_reports_the_version_as_unsupported(self):
        self.assertIn("unsupported Spine version", self.mod.SKIPPED["garbage"])

    def test_plain_filename_rule(self):
        ok = self.mod.is_plain_filename
        self.assertTrue(ok("a.skel"))
        for bad in ("", ".", "..", "a/b", "a\\b", "../a", None, 5, ["a"]):
            with self.subTest(value=bad):
                self.assertFalse(ok(bad))

    def test_backslash_separator_is_rejected(self):
        models = self.tmp / "bs"
        make_model(models, "bs", model_json={"skeleton": "..\\x.skel"})
        self.mod.MODELS_DIR = str(models)
        try:
            found, skipped = self.mod.scan_models()
        finally:
            self.mod.MODELS_DIR = str(self.models)
        self.assertEqual(found, [])
        self.assertIn("bs", skipped)

    def test_missing_models_directory_gives_empty_scan(self):
        self.mod.MODELS_DIR = str(self.tmp / "absent")
        try:
            self.assertEqual(self.mod.scan_models(), ([], {}))
        finally:
            self.mod.MODELS_DIR = str(self.models)

    def test_files_beside_model_directories_are_ignored(self):
        models = self.tmp / "stray"
        make_model(models, "ok")
        (models / "readme.txt").write_text("x")
        self.mod.MODELS_DIR = str(models)
        try:
            found, skipped = self.mod.scan_models()
        finally:
            self.mod.MODELS_DIR = str(self.models)
        self.assertEqual([m["id"] for m in found], ["ok"])
        self.assertEqual(skipped, {})


class Overrides(ScanTestCase):
    def test_defaults(self):
        m = self.by_id["alpha"]
        self.assertEqual(m["name"], "alpha")
        self.assertIsNone(m["group"])
        self.assertEqual(m["skin"], "default")
        self.assertEqual(m["defaultAnimation"], "idle")
        self.assertEqual(m["hideSlots"], [])
        self.assertEqual(m["skeleton"], "alpha.skel")
        self.assertEqual(m["atlas"], "alpha.atlas")

    def test_model_json_overrides(self):
        m = self.by_id["named"]
        self.assertEqual(m["name"], "Pretty Name")
        self.assertEqual(m["group"], "Group A")
        self.assertEqual(m["character"], "custom-char")
        self.assertEqual(m["skin"], "outfit")
        self.assertEqual(m["defaultAnimation"], "wave")

    def test_hide_slots_keeps_only_non_empty_strings(self):
        self.assertEqual(self.by_id["named"]["hideSlots"], ["bg"])

    def test_non_string_overrides_fall_back(self):
        models = self.tmp / "types"
        make_model(
            models,
            "t",
            model_json={"name": 5, "group": "", "character": ["x"], "skin": None, "hideSlots": "bg"},
        )
        self.mod.MODELS_DIR = str(models)
        try:
            found, _ = self.mod.scan_models()
        finally:
            self.mod.MODELS_DIR = str(self.models)
        m = found[0]
        self.assertEqual(m["name"], "t")
        self.assertIsNone(m["group"])
        self.assertEqual(m["skin"], "default")
        self.assertEqual(m["hideSlots"], [])
        self.assertEqual(m["character"], "t")

    def test_skel_wins_over_json(self):
        models = self.tmp / "both"
        d = make_model(models, "both", "4.1.56")
        (d / "z.json").write_text('{"skeleton": {"spine": "4.0.1"}}')
        self.mod.MODELS_DIR = str(models)
        try:
            found, _ = self.mod.scan_models()
        finally:
            self.mod.MODELS_DIR = str(self.models)
        self.assertEqual(found[0]["skeleton"], "both.skel")
        self.assertEqual(found[0]["runtime"], "spine41")

    def test_model_json_is_never_picked_as_the_skeleton(self):
        models = self.tmp / "onlymj"
        d = models / "onlymj"
        d.mkdir(parents=True)
        (d / "model.json").write_text("{}")
        (d / "a.atlas").write_text("page.png\n")
        self.mod.MODELS_DIR = str(models)
        try:
            found, skipped = self.mod.scan_models()
        finally:
            self.mod.MODELS_DIR = str(self.models)
        self.assertEqual(found, [])
        self.assertIn("needs one skeleton", skipped["onlymj"])

    def test_skeleton_override_picks_between_several(self):
        models = self.tmp / "several"
        d = make_model(models, "several", "4.1.56")
        (d / "other.skel").write_bytes(binary_skel("4.2.120"))
        (d / "model.json").write_text('{"skeleton": "other.skel"}')
        self.mod.MODELS_DIR = str(models)
        try:
            found, _ = self.mod.scan_models()
        finally:
            self.mod.MODELS_DIR = str(self.models)
        self.assertEqual(found[0]["runtime"], "spine42")


class ClassifyKind(ScanTestCase):
    def test_empty_is_a_character(self):
        self.assertEqual(self.mod.classify_kind([]), "character")

    def test_mostly_background_is_a_scene(self):
        self.assertEqual(self.mod.classify_kind(["bg_a", "bg_b", "body"]), "scene")

    def test_exactly_half_is_still_a_character(self):
        self.assertEqual(self.mod.classify_kind(["bg_a", "body"]), "character")

    def test_bare_back_stays_a_character(self):
        self.assertEqual(self.mod.classify_kind(["F_Coat_Back", "back", "back_hair"]), "character")

    def test_name_prefixes(self):
        for name in ("bg", "BG_sky", "background1", "Scene_01", "diorama"):
            with self.subTest(name=name):
                self.assertEqual(self.mod.classify_kind([name]), "scene")

    def test_bg_underscore_inside_a_name_counts(self):
        self.assertEqual(self.mod.classify_kind(["a_bg_1", "b_bg_2"]), "scene")

    def test_scanned_kinds(self):
        self.assertEqual(self.by_id["lobby"]["kind"], "scene")
        self.assertEqual(self.by_id["alpha"]["kind"], "character")

    def test_kind_override_wins_over_detection(self):
        self.assertEqual(self.by_id["named"]["kind"], "scene")

    def test_json_slots_are_preferred_over_the_atlas(self):
        models = self.tmp / "slots"
        make_model(models, "s", fmt="json", slots=["bg_1", "bg_2", "bg_3"], regions=("body",))
        self.mod.MODELS_DIR = str(models)
        try:
            found, _ = self.mod.scan_models()
        finally:
            self.mod.MODELS_DIR = str(self.models)
        self.assertEqual(found[0]["kind"], "scene")

    def test_atlas_region_reader_skips_pages_and_properties(self):
        path = self.tmp / "r.atlas"
        path.write_text("page.png\nsize: 1,1\nbody\n  bounds: 0,0,1,1\nhead\n\nsecond.WEBP\narm\n")
        self.assertEqual(self.mod.read_atlas_regions(path), ["body", "head", "arm"])


class AssignCharacters(ScanTestCase):
    def test_variants_share_a_stem(self):
        self.assertEqual(self.by_id["hero_01"]["character"], "hero")
        self.assertEqual(self.by_id["hero_02"]["character"], "hero")

    def test_a_lone_suffix_is_not_renamed(self):
        self.assertEqual(self.by_id["solo_01"]["character"], "solo_01")

    def test_plain_ids_are_their_own_character(self):
        self.assertEqual(self.by_id["alpha"]["character"], "alpha")

    def test_stem_equal_to_another_model_id_groups(self):
        models = [
            {"id": "ann", "character": None},
            {"id": "ann_01", "character": None},
        ]
        self.mod.assign_characters(models)
        self.assertEqual([m["character"] for m in models], ["ann", "ann"])

    def test_explicit_character_is_kept(self):
        self.assertEqual(self.by_id["named"]["character"], "custom-char")

    def test_dash_suffix_counts(self):
        models = [{"id": "x-1", "character": None}, {"id": "x-2", "character": None}]
        self.mod.assign_characters(models)
        self.assertEqual({m["character"] for m in models}, {"x"})

    def test_first_character_skips_scenes(self):
        models = [{"id": "bg", "kind": "scene"}, {"id": "who", "kind": "character"}]
        self.assertEqual(self.mod.first_character_id(models), "who")
        self.assertEqual(self.mod.first_character_id([models[0]]), "bg")
        self.assertIsNone(self.mod.first_character_id([]))

    def test_models_are_sorted_by_directory_name(self):
        ids = [m["id"] for m in self.mod.MODELS]
        self.assertEqual(ids, sorted(ids))


class ClampHelpers(ScanTestCase):
    def test_clamp(self):
        c = self.mod.clamp
        self.assertEqual(c(5, 0, 1, 9), 1)
        self.assertEqual(c(-5, 0, 1, 9), 0)
        self.assertEqual(c("0.5", 0, 1, 9), 0.5)
        for bad in ("abc", "", "  ", None, [], {}, float("nan"), "nan"):
            with self.subTest(value=bad):
                self.assertEqual(c(bad, 0, 1, 9), 9)

    def test_clamp_infinity_is_bounded(self):
        self.assertEqual(self.mod.clamp(float("inf"), 0, 1, 9), 1)
        self.assertEqual(self.mod.clamp("-inf", 0, 1, 9), 0)

    def test_plain_int_rejects_bool(self):
        self.assertEqual(self.mod.plain_int(True, 7), 7)
        self.assertEqual(self.mod.plain_int(3, 7), 3)
        self.assertEqual(self.mod.plain_int("3", 7), 7)
        self.assertEqual(self.mod.plain_int(3.5, 7), 7)


if __name__ == "__main__":
    unittest.main()
