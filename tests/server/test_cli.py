"""bin/spinewall against a live server on a temp port."""

import json
import os
import subprocess
import unittest

from tests.server.support import CLI, ServerCase


class CliCase(ServerCase):
    def run_cli(self, *args, port=None):
        env = {"PATH": os.environ.get("PATH", ""), "HOME": str(self.tmp), "SPINEWALL_PORT": str(port or self.server.port)}
        return subprocess.run([str(CLI), *args], env=env, capture_output=True, text=True, timeout=20)


class Commands(CliCase):
    def test_no_argument_runs_info(self):
        done = self.run_cli()
        self.assertEqual(done.returncode, 0, done.stderr)
        self.assertIn("models", json.loads(done.stdout))

    def test_info_reports_skipped_models(self):
        info = json.loads(self.run_cli("info").stdout)
        self.assertIn("skipped", info)
        self.assertIn("input", info)

    def test_output_ends_with_a_newline(self):
        self.assertTrue(self.run_cli("info").stdout.endswith("\n"))

    def test_simple_command_changes_the_server(self):
        done = self.run_cli("zoom-in")
        self.assertEqual(done.returncode, 0, done.stderr)
        self.assertGreater(self.state()["scale"], 1.0)

    def test_set_model_takes_one_argument(self):
        self.assertEqual(self.run_cli("set-model", "gamma").returncode, 0)
        self.assertEqual(self.state()["model"], "gamma")

    def test_rescan_finds_a_new_model(self):
        from tests.server.support import make_model

        make_model(self.server.models, "fresh", "4.1.56")
        self.assertNotIn("fresh", self.model_ids())
        done = self.run_cli("rescan")
        self.assertEqual(done.returncode, 0, done.stderr)
        self.assertIn("fresh", self.model_ids())

    def test_add_and_remove_character_use_two_words(self):
        self.assertEqual(self.run_cli("add-character", "gamma").returncode, 0)
        self.assertEqual(self.run_cli("add-character", "beta").returncode, 0)
        before = self.state()["characters"]
        uid = before[0]["uid"]
        self.assertEqual(self.run_cli("remove-character", uid).returncode, 0)
        after = self.state()["characters"]
        self.assertEqual(len(after), len(before) - 1)
        self.assertNotIn(uid, [c["uid"] for c in after])

    def test_two_value_command_carries_uid_and_value(self):
        self.run_cli("add-character", "gamma")
        self.run_cli("add-character", "beta")
        uid = self.state()["characters"][0]["uid"]
        self.assertEqual(self.run_cli("set-char-model", uid, "delta").returncode, 0)
        self.assertEqual(self.state()["characters"][0]["model"], "delta")

    def test_names_with_spaces_and_symbols_are_encoded(self):
        self.report("alpha", ["walk fast #1", "idle/loop", "100%"])
        for name in ("walk fast #1", "idle/loop", "100%", "über ünï"):
            with self.subTest(name=name):
                done = self.run_cli("set-anim", name)
                self.assertEqual(done.returncode, 0, done.stderr)
                self.assertEqual(self.state()["animation"], name)

    def test_a_value_that_looks_like_a_query_stays_one_value(self):
        self.run_cli("set-anim", "a?b=c&d")
        self.assertEqual(self.state()["animation"], "a?b=c&d")


class Errors(CliCase):
    def test_more_than_three_arguments_exits_2_and_sends_nothing(self):
        before = self.state()
        done = self.run_cli("set-anim", "a", "b", "c")
        self.assertEqual(done.returncode, 2)
        self.assertIn("at most 3 arguments", done.stderr)
        self.assertEqual(self.state(), before)

    def test_three_arguments_are_allowed(self):
        self.assertNotEqual(self.run_cli("set-char-anim", "uid", "walk").returncode, 2)

    def test_unknown_command_changes_nothing(self):
        before = self.state()
        self.run_cli("no-such-command")
        self.assertEqual(self.state(), before)

    def test_dead_server_is_a_nonzero_exit(self):
        from tests.server.support import free_port

        done = self.run_cli("info", port=free_port())
        self.assertNotEqual(done.returncode, 0)

    def test_the_port_comes_from_spinewall_port(self):
        self.assertEqual(self.run_cli("info", port=self.server.port).returncode, 0)


if __name__ == "__main__":
    unittest.main()
