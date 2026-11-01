"""additional_model_dirs in the config file: scan, serving, duplicates, bad config, and path safety."""

import json
import shutil
import tempfile
import unittest
from pathlib import Path

from tests.server.support import ServerProcess, make_model


class ExtraDirsCase(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp(prefix="spinewall-extra-"))
        self.addCleanup(shutil.rmtree, self.tmp, True)
        self.extra = self.tmp / "elsewhere"
        self.extra.mkdir()
        self.config = self.tmp / "config.json"

    def start(self, config, main=("main-model",), extra=("far-model",)):
        server = ServerProcess(self.tmp, extra_env={"SPINEWALL_CONFIG": str(self.config)})
        for model_id in main:
            make_model(server.models, model_id)
        for model_id in extra:
            make_model(self.extra, model_id)
        if config is not None:
            self.config.write_text(config if isinstance(config, str) else json.dumps(config))
        self.addCleanup(server.stop)
        return server.start()


class Scan(ExtraDirsCase):
    def test_models_from_an_extra_directory_are_listed_and_served(self):
        server = self.start({"additional_model_dirs": [str(self.extra)]})
        ids = [m["id"] for m in server.request("GET", "/models").json]
        self.assertEqual(sorted(ids), ["far-model", "main-model"])
        self.assertEqual(server.request("GET", "/far-model/far-model.atlas").status, 200)
        self.assertEqual(server.request("GET", "/main-model/main-model.atlas").status, 200)

    def test_no_config_file_changes_nothing(self):
        server = self.start(None)
        self.assertEqual([m["id"] for m in server.request("GET", "/models").json], ["main-model"])

    def test_a_new_extra_model_appears_after_a_rescan(self):
        server = self.start({"additional_model_dirs": [str(self.extra)]})
        make_model(self.extra, "later-model")
        server.request("GET", "/rescan")
        self.assertIn("later-model", [m["id"] for m in server.request("GET", "/models").json])

    def test_the_main_directory_wins_a_duplicate_id(self):
        server = self.start({"additional_model_dirs": [str(self.extra)]}, extra=("main-model",))
        self.assertEqual([m["id"] for m in server.request("GET", "/models").json], ["main-model"])
        skipped = server.request("GET", "/info").json["skipped"]
        self.assertTrue(any("duplicate model ID" in reason for reason in skipped.values()), skipped)

    def test_info_lists_the_extra_directories(self):
        server = self.start({"additional_model_dirs": [str(self.extra)]})
        self.assertEqual(server.request("GET", "/info").json["additionalModelDirs"], [str(self.extra)])


class BadConfig(ExtraDirsCase):
    def check_ignored(self, config):
        server = self.start(config)
        self.assertEqual([m["id"] for m in server.request("GET", "/models").json], ["main-model"])
        return server

    def test_invalid_json_is_ignored(self):
        server = self.check_ignored("{not json")
        self.assertIn("not valid JSON", server.log())

    def test_a_non_list_value_is_ignored(self):
        server = self.check_ignored({"additional_model_dirs": str(self.extra)})
        self.assertIn("must be a list", server.log())

    def test_relative_missing_and_non_string_entries_are_skipped(self):
        server = self.check_ignored({"additional_model_dirs": ["relative/dir", str(self.tmp / "absent"), 5, None]})
        self.assertEqual(server.log().count("skipped"), 4)


class Safety(ExtraDirsCase):
    def test_a_traversal_path_never_leaves_the_model_directory(self):
        server = self.start({"additional_model_dirs": [str(self.extra)]})
        (self.tmp / "secret.json").write_text("{}")
        for path in ("/far-model/../../secret.json", "/%2e%2e/secret.json", "/far-model/%2e%2e/%2e%2e/secret.json", "/not-a-model/x.json"):
            self.assertEqual(server.request("GET", path).status, 404, path)

    def test_model_json_stays_hidden_in_an_extra_directory(self):
        server = self.start({"additional_model_dirs": [str(self.extra)]})
        (self.extra / "far-model" / "model.json").write_text("{}")
        self.assertEqual(server.request("GET", "/far-model/model.json").status, 404)

    def test_an_extra_directory_is_never_listed(self):
        server = self.start({"additional_model_dirs": [str(self.extra)]})
        self.assertEqual(server.request("GET", "/far-model/").status, 404)


if __name__ == "__main__":
    unittest.main()
