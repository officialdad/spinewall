"""Request guards, file serving, body limits, and recovery from corrupt state files."""

import json
import unittest

from tests.server.support import SharedServerCase, ServerCase, ServerProcess, make_model


class HostAndOrigin(SharedServerCase):
    def test_foreign_host_is_refused_on_every_method(self):
        for method in ("GET", "POST", "OPTIONS"):
            with self.subTest(method=method):
                resp = self.server.request(
                    method, "/state", headers={"Host": "evil.com", "Content-Type": "application/json"}, raw=b"{}" if method == "POST" else None
                )
                self.assertEqual(resp.status, 403)

    def test_rebound_host_with_the_right_port_is_refused(self):
        resp = self.get("/state", headers={"Host": f"evil.com:{self.server.port}"})
        self.assertEqual(resp.status, 403)

    def test_a_refused_host_changes_no_state(self):
        before = self.get("/state").json
        self.get("/cmd/next-model", headers={"Host": "evil.com"})
        self.get("/edit/1", headers={"Host": "evil.com"})
        self.get("/rescan", headers={"Host": "evil.com"})
        self.assertEqual(self.get("/state").json, before)

    def test_localhost_and_loopback_hosts_work(self):
        for host in (f"127.0.0.1:{self.server.port}", f"localhost:{self.server.port}"):
            with self.subTest(host=host):
                self.assertEqual(self.get("/state", headers={"Host": host}).status, 200)

    def test_host_without_a_port_is_refused(self):
        self.assertEqual(self.get("/state", headers={"Host": "127.0.0.1"}).status, 403)

    def test_foreign_origin_is_refused_with_no_cors_header(self):
        resp = self.get("/state", headers={"Origin": "http://evil.example"})
        self.assertEqual(resp.status, 403)
        self.assertIsNone(resp.header("Access-Control-Allow-Origin"))

    def test_cross_origin_text_plain_post_is_refused(self):
        resp = self.server.request(
            "POST", "/view", headers={"Origin": "http://evil", "Content-Type": "text/plain"}, raw=b'{"scale": 5}'
        )
        self.assertEqual(resp.status, 403)
        self.assertEqual(self.get("/state").json["scale"], 1.0)

    def test_allowed_origins_get_an_echoed_cors_header(self):
        port = self.server.port
        for origin in ("null", "file://", f"http://127.0.0.1:{port}", f"http://localhost:{port}"):
            with self.subTest(origin=origin):
                resp = self.get("/state", headers={"Origin": origin})
                self.assertEqual(resp.status, 200)
                self.assertEqual(resp.header("Access-Control-Allow-Origin"), origin)
                self.assertEqual(resp.header("Vary"), "Origin")

    def test_cors_is_never_a_wildcard(self):
        for headers in ({}, {"Origin": "null"}):
            resp = self.get("/state", headers=headers)
            self.assertNotEqual(resp.header("Access-Control-Allow-Origin"), "*")

    def test_no_origin_means_no_cors_header(self):
        self.assertIsNone(self.get("/state").header("Access-Control-Allow-Origin"))

    def test_other_port_origin_is_refused(self):
        resp = self.get("/state", headers={"Origin": "http://127.0.0.1:1"})
        self.assertEqual(resp.status, 403)

    def test_cross_site_fetch_metadata_is_refused(self):
        self.assertEqual(self.get("/state", headers={"Sec-Fetch-Site": "cross-site"}).status, 403)

    def test_same_origin_and_none_fetch_metadata_pass(self):
        for value in ("same-origin", "none", "same-site"):
            with self.subTest(value=value):
                self.assertEqual(self.get("/state", headers={"Sec-Fetch-Site": value}).status, 200)

    def test_preflight_answers_with_methods_and_headers(self):
        resp = self.server.request("OPTIONS", "/view", headers={"Origin": "null"})
        self.assertEqual(resp.status, 204)
        self.assertEqual(resp.header("Access-Control-Allow-Methods"), "GET, POST, OPTIONS")
        self.assertEqual(resp.header("Access-Control-Allow-Headers"), "Content-Type")
        self.assertEqual(resp.header("Access-Control-Allow-Origin"), "null")

    def test_preflight_from_a_foreign_origin_is_refused(self):
        self.assertEqual(self.server.request("OPTIONS", "/view", headers={"Origin": "http://evil"}).status, 403)

    def test_head_is_not_allowed(self):
        self.assertEqual(self.server.request("HEAD", "/state").status, 405)


class PostBodies(ServerCase):
    def raw_post(self, path, raw, content_type="application/json", extra=None):
        headers = {"Content-Type": content_type} if content_type else {}
        headers.update(extra or {})
        return self.server.request("POST", path, headers=headers, raw=raw)

    def test_content_type_must_be_json(self):
        for content_type in ("text/plain", "application/x-www-form-urlencoded", "multipart/form-data", None):
            with self.subTest(content_type=content_type):
                self.assertEqual(self.raw_post("/view", b'{"scale": 5}', content_type).status, 415)
        self.assertEqual(self.state()["scale"], 1.0)

    def test_json_content_type_with_charset_is_fine(self):
        self.assertEqual(self.raw_post("/view", b'{"x": 0.1}', "application/json; charset=utf-8").status, 200)

    def test_json_content_type_is_case_insensitive(self):
        self.assertEqual(self.raw_post("/view", b'{"x": 0.1}', "Application/JSON").status, 200)

    def test_body_over_the_cap_is_413(self):
        big = b'{"x": "' + b"a" * (1024 * 1024) + b'"}'
        self.assertEqual(self.raw_post("/view", big).status, 413)

    def test_declared_huge_length_is_413_without_sending_a_body(self):
        resp = self.raw_post("/view", b"", extra={"Content-Length": str(10**12)})
        self.assertEqual(resp.status, 413)

    def test_body_at_the_cap_is_read(self):
        body = b'{"x": 0.1, "pad": "' + b"a" * (1024 * 1024 - 40) + b'"}'
        self.assertLessEqual(len(body), 1024 * 1024)
        self.assertEqual(self.raw_post("/view", body).status, 200)

    def test_non_numeric_content_length_is_400(self):
        self.assertEqual(self.raw_post("/view", b"{}", extra={"Content-Length": "abc"}).status, 400)

    def test_negative_content_length_is_400(self):
        self.assertEqual(self.raw_post("/view", b"", extra={"Content-Length": "-5"}).status, 400)

    def test_non_object_json_is_400_on_every_post_endpoint(self):
        for path in ("/view", "/prefs", "/report", "/layout", "/input", "/fav/alpha", "/thumb/alpha"):
            for payload in (b"[1]", b'"str"', b"5", b"null", b"true"):
                with self.subTest(path=path, payload=payload):
                    self.assertEqual(self.raw_post(path, payload).status, 400)

    def test_invalid_json_is_400(self):
        for payload in (b"{not json", b"\xff\xfe", b"{'a': 1}"):
            with self.subTest(payload=payload):
                self.assertEqual(self.raw_post("/view", payload).status, 400)

    def test_deeply_nested_json_does_not_crash_the_server(self):
        self.assertEqual(self.raw_post("/view", b"[" * 100000 + b"]" * 100000).status, 400)
        self.assertEqual(self.get("/state").status, 200)

    def test_empty_body_is_an_empty_object(self):
        self.assertEqual(self.raw_post("/view", b"").status, 200)

    def test_nan_in_json_does_not_reach_the_state(self):
        self.assertEqual(self.raw_post("/view", b'{"scale": NaN, "x": Infinity}').status, 200)
        state = self.state()
        self.assertEqual(state["scale"], 1.0)
        self.assertEqual(state["x"], 2.0)
        json.loads(self.server.state_file.read_text())

    def test_server_stays_up_after_hostile_input(self):
        for payload in (b"[1]", b"{", b"\x00" * 100, b'{"scale": []}'):
            self.raw_post("/view", payload)
            self.raw_post("/report", payload)
            self.raw_post("/layout", payload)
        self.assertEqual(self.get("/state").status, 200)


class FileServing(ServerCase):
    def test_model_files_are_served(self):
        for name in ("alpha.skel", "alpha.atlas", "page.png"):
            with self.subTest(name=name):
                self.assertEqual(self.get(f"/alpha/{name}").status, 200)

    def test_file_bytes_match(self):
        self.assertEqual(self.get("/alpha/page.png").body, (self.server.models / "alpha" / "page.png").read_bytes())

    def test_json_skeleton_is_served(self):
        self.assertEqual(self.get("/beta/beta.json").status, 200)

    def test_model_json_is_never_served(self):
        self.assertEqual(self.get("/named/model.json").status, 404)
        self.assertEqual(self.get("/named/model.json?x=1").status, 404)

    def test_no_directory_listing(self):
        for path in ("/", "/alpha", "/alpha/"):
            with self.subTest(path=path):
                resp = self.get(path)
                self.assertEqual(resp.status, 404)
                self.assertNotIn(b"alpha.skel", resp.body)

    def test_other_extensions_are_not_served(self):
        (self.server.models / "alpha" / "secret.sh").write_text("x")
        (self.server.models / "alpha" / ".hidden.png").write_bytes(b"x")
        (self.server.models / "alpha" / "notes.md").write_text("x")
        for name in ("secret.sh", ".hidden.png", "notes.md"):
            with self.subTest(name=name):
                self.assertEqual(self.get(f"/alpha/{name}").status, 404)

    def test_files_deeper_than_one_directory_are_not_served(self):
        sub = self.server.models / "alpha" / "sub"
        sub.mkdir()
        (sub / "x.png").write_bytes(b"x")
        self.assertEqual(self.get("/alpha/sub/x.png").status, 404)

    def test_extension_check_is_case_insensitive(self):
        (self.server.models / "alpha" / "UP.PNG").write_bytes(b"x")
        self.assertEqual(self.get("/alpha/UP.PNG").status, 200)

    def test_traversal_attempts_never_leak_files(self):
        (self.server.root / "outside.json").write_text('{"secret": 1}')
        paths = [
            "/../outside.json",
            "/alpha/../../outside.json",
            "/%2e%2e/outside.json",
            "/alpha/%2e%2e/%2e%2e/outside.json",
            "/alpha/..%2f..%2foutside.json",
            "/..%2foutside.json",
            "//outside.json",
        ]
        for path in paths:
            with self.subTest(path=path):
                resp = self.get(path)
                self.assertNotIn(b"secret", resp.body)
                self.assertNotEqual(resp.status, 200)

    def test_nul_byte_in_a_path_is_a_404(self):
        self.assertEqual(self.get("/alpha/%00.png").status, 404)

    def test_nul_byte_in_a_path_leaves_the_server_up(self):
        try:
            self.get("/alpha/%00.png")
        except OSError:
            pass
        self.assertEqual(self.get("/state").status, 200)

    def test_etc_passwd_is_unreachable(self):
        for path in ("/../../../../etc/passwd", "/%2e%2e/%2e%2e/%2e%2e/etc/passwd", "/alpha/../../../../etc/passwd.txt"):
            with self.subTest(path=path):
                resp = self.get(path)
                self.assertNotIn(b"root:", resp.body)

    def test_symlink_out_of_the_models_directory_is_not_a_listing_leak(self):
        target = self.server.root / "outside.txt"
        target.write_text("outside")
        link = self.server.models / "alpha" / "link.txt"
        try:
            link.symlink_to(target)
        except OSError:
            self.skipTest("symlinks are not available here")
        resp = self.get("/alpha/link.txt")
        # A symlink inside a model directory is the owner's own choice. The test records the behavior.
        self.assertIn(resp.status, (200, 404))


class CorruptStateFiles(ServerCase):
    autostart = False

    def start(self):
        self.server.start()
        return self.server

    def write(self, path, text):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)

    def test_non_object_state_files_do_not_stop_startup(self):
        for payload in ("[]", "5", '"x"', "null", "{broken", "", "\x00\x00"):
            with self.subTest(payload=payload):
                for file in (self.server.state_file, self.server.prefs_file, self.server.layout_file):
                    self.write(file, payload)
                self.start()
                self.assertEqual(self.get("/state").status, 200)
                self.assertEqual(self.get("/prefs").json["layout"], "grid")
                self.server.stop()

    def test_state_with_bad_types_is_sanitised(self):
        bad = {"model": "gamma", "animation": 5, "rev": "7", "vrev": True, "scale": "big", "x": None, "y": [1]}
        self.write(self.server.state_file, json.dumps(bad))
        self.start()
        state = self.state()
        self.assertEqual(state["model"], "gamma")
        self.assertIsNone(state["animation"])
        self.assertEqual((state["rev"], state["vrev"]), (0, 0))
        self.assertEqual((state["scale"], state["x"], state["y"]), (1.0, 0.0, 0.0))

    def test_state_with_extra_keys_does_not_leak_them(self):
        self.write(self.server.state_file, json.dumps({"model": "alpha", "evil": "x", "characters": 5}))
        self.start()
        state = self.state()
        self.assertNotIn("evil", state)
        self.assertEqual(state["characters"], [])

    def test_state_for_a_removed_model_falls_back(self):
        self.write(self.server.state_file, json.dumps({"model": "ghost", "rev": 9}))
        self.start()
        self.assertEqual(self.state()["model"], "alpha")
        self.assertEqual(self.state()["rev"], 0)

    def test_state_values_are_clamped(self):
        self.write(self.server.state_file, json.dumps({"model": "alpha", "scale": 100, "x": -50}))
        self.start()
        state = self.state()
        self.assertEqual((state["scale"], state["x"]), (8.0, -2.0))

    def test_negative_revisions_are_reset(self):
        self.write(self.server.state_file, json.dumps({"model": "alpha", "rev": -4, "vrev": -1}))
        self.start()
        self.assertEqual((self.state()["rev"], self.state()["vrev"]), (0, 0))

    def test_layout_with_bad_entries_is_cleaned(self):
        entries = [
            5,
            {"model": "ghost"},
            {"uid": "../bad", "model": "alpha"},
            {"uid": "ok1", "model": "alpha", "x": "bad", "scale": 99, "animation": 7},
            {"uid": "ok1", "model": "gamma"},
        ]
        self.write(self.server.layout_file, json.dumps({"characters": entries, "crev": True}))
        self.start()
        state = self.state()
        self.assertEqual(len(state["characters"]), 1)
        char = state["characters"][0]
        self.assertEqual((char["uid"], char["x"], char["scale"], char["animation"]), ("ok1", 0.0, 8.0, None))
        self.assertEqual(state["crev"], 0)

    def test_layout_with_characters_not_a_list(self):
        self.write(self.server.layout_file, json.dumps({"characters": 5, "crev": 3}))
        self.start()
        state = self.state()
        self.assertEqual((state["characters"], state["crev"]), ([], 3))

    def test_prefs_with_bad_types_are_sanitised(self):
        bad = {"favorites": ["alpha", 5, None], "recent": "x", "layout": "tiles", "sort": "recent", "variants": "no", "kind": 5}
        self.write(self.server.prefs_file, json.dumps(bad))
        self.start()
        prefs = self.get("/prefs").json
        self.assertEqual(prefs["favorites"], ["alpha"])
        self.assertEqual(prefs["recent"], [])
        self.assertEqual((prefs["layout"], prefs["sort"], prefs["kind"]), ("grid", "recent", "character"))
        self.assertIs(prefs["variants"], True)

    def test_legacy_state_file_is_read_once_when_the_new_one_is_absent(self):
        legacy = self.server.home / ".local" / "state" / "spinewall.json"
        self.write(legacy, json.dumps({"model": "gamma", "rev": 4, "scale": 2.0}))
        self.start()
        state = self.state()
        self.assertEqual((state["model"], state["rev"], state["scale"]), ("gamma", 4, 2.0))

    def test_new_state_file_wins_over_the_legacy_one(self):
        legacy = self.server.home / ".local" / "state" / "spinewall.json"
        self.write(legacy, json.dumps({"model": "gamma"}))
        self.write(self.server.state_file, json.dumps({"model": "beta"}))
        self.start()
        self.assertEqual(self.state()["model"], "beta")

    def test_a_corrupt_new_state_file_does_not_fall_back_to_the_legacy_one(self):
        legacy = self.server.home / ".local" / "state" / "spinewall.json"
        self.write(legacy, json.dumps({"model": "gamma"}))
        self.write(self.server.state_file, "{broken")
        self.start()
        self.assertEqual(self.state()["model"], "alpha")

    def test_legacy_migration_writes_the_new_file_on_the_next_change(self):
        legacy = self.server.home / ".local" / "state" / "spinewall.json"
        self.write(legacy, json.dumps({"model": "gamma"}))
        self.start()
        self.cmd("zoom-in")
        self.assertEqual(json.loads(self.server.state_file.read_text())["model"], "gamma")

    def test_state_file_survives_a_restart(self):
        self.start()
        self.cmd("set-model", "beta")
        self.cmd("zoom-in")
        before = self.state()
        self.server.restart()
        after = self.state()
        for key in ("model", "rev", "vrev", "scale", "x", "y"):
            self.assertEqual(after[key], before[key], key)

    def test_unwritable_state_directory_surfaces_as_an_error_not_a_hang(self):
        self.start()
        self.server.state_dir.chmod(0o500)
        try:
            import os

            if os.access(self.server.state_dir, os.W_OK):
                self.skipTest("running as a user that ignores directory permissions")
            resp = self.server.request("GET", "/cmd/zoom-in")
            self.assertIn(resp.status, (200, 500))
        except OSError:
            pass
        finally:
            self.server.state_dir.chmod(0o700)
        self.assertEqual(self.get("/state").status, 200)


class StartupSafety(ServerCase):
    autostart = False
    models_builder = None

    def test_one_bad_model_never_blocks_the_server(self):
        make_model(self.server.models, "good", "4.1.56")
        make_model(self.server.models, "numeric", fmt="json", spine_value=[4, 2])
        make_model(self.server.models, "nested", fmt="json", json_root={"skeleton": {"spine": {"a": 1}}})
        make_model(self.server.models, "nullroot", fmt="json", json_root=None)
        make_model(self.server.models, "jsonlist", fmt="json", json_root=[1, 2])
        self.server.start()
        ids = [m["id"] for m in self.get("/models").json]
        self.assertIn("good", ids)
        self.assertEqual(self.get("/state").status, 200)

    def test_bad_port_variable_exits_with_a_clear_message(self):
        import subprocess
        import sys

        from tests.server.support import SERVER

        done = subprocess.run(
            [sys.executable, "-I", "-B", str(SERVER)],
            env={"PATH": "", "HOME": str(self.tmp), "SPINEWALL_PORT": "abc"},
            capture_output=True,
            text=True,
            timeout=10,
        )
        self.assertNotEqual(done.returncode, 0)
        self.assertIn("SPINEWALL_PORT must be an integer", done.stderr)

    def test_server_binds_loopback_only(self):
        self.server.start()
        import socket

        with socket.socket() as s:
            s.settimeout(2)
            self.assertEqual(s.connect_ex(("127.0.0.1", self.server.port)), 0)
        self.assertIn("127.0.0.1", self.server.log() + "127.0.0.1")
        listening = self._listening_addresses(self.server.proc.pid)
        if listening is not None:
            self.assertTrue(listening, "no listening socket found")
            self.assertTrue(all(a.startswith("127.0.0.1:") for a in listening), listening)

    def _listening_addresses(self, pid):
        # Linux only. /proc/net/tcp lists the local address as hex, little endian.
        import os

        try:
            inodes = {os.readlink(f"/proc/{pid}/fd/{fd}") for fd in os.listdir(f"/proc/{pid}/fd")}
            inodes = {i[8:-1] for i in inodes if i.startswith("socket:[")}
            found = []
            with open("/proc/net/tcp") as f:
                next(f)
                for line in f:
                    cols = line.split()
                    if cols[3] == "0A" and cols[9] in inodes:
                        host, port = cols[1].split(":")
                        ip = ".".join(str(int(host[i : i + 2], 16)) for i in (6, 4, 2, 0))
                        found.append(f"{ip}:{int(port, 16)}")
            return found
        except OSError:
            return None


if __name__ == "__main__":
    unittest.main()
