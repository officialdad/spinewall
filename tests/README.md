# Tests

## Server, CLI, and scripts (Python, stdlib only)

Run from the repository root:

```
python3 -I -m unittest discover -s tests -t . -v
```

The run takes about 30 s and needs no network, no Plasma, and no models.

Set `SPINEWALL_BIN` to run the black-box server tests against the Go binary instead of the Python server:

```
CGO_ENABLED=0 go build -o spinewall ./cmd/spinewall
SPINEWALL_BIN=$PWD/spinewall python3 -I -m unittest discover -s tests -t . -v
```

The CLI tests run the Go binary in the repo root by default. `SPINEWALL_CLI` names a different one, and the tests skip when no Go binary is built. The Python CLI in `bin/spinewall` is now a shim that runs a built binary, so it holds no logic to test.

The in-process unit tests in `test_scan.py` import the Python module, so they stay Python only.

The differential test starts the Python server and the Go server on one state copy and compares decoded JSON for the read endpoints and every `/cmd/*`:

```
python3 tests/differential.py --go-bin ./spinewall --state-dir <state copy> --models-dir <models>
```

| File | Covers |
|---|---|
| `tests/server/test_scan.py` | Version detection, skip reasons, `model.json` overrides, `classify_kind`, `assign_characters`. |
| `tests/server/test_api.py` | Every `/cmd` name, characters, layout, prefs, recents, thumbnails, `/report`, `/input`, rescan, concurrency. |
| `tests/server/test_security.py` | Host, Origin, and `Sec-Fetch-Site` guards, CORS, 415, 413, file whitelist, traversal, corrupt state files, legacy state migration. |
| `tests/server/test_cli.py` | The Go CLI against a live server: encoding, usage errors, exit codes, shortcut log and rollover. |
| `tests/scripts/test_install.py` | `scripts/install.sh`, the POSIX bootstrap, with stub `curl`. Arch detection, checksum and signature handling, install location, and the `spinewall setup` handoff. |
| `tests/installer/test_setup.py` | `spinewall setup` end to end under a throwaway HOME with every XDG path overridden: `--yes` install, the interactive wizard driven over a pty, no models, no network, and a byte-identical home after uninstall. Builds the Go binary; skips without Go. |
| `tests/scripts/test_patch.py` | `scripts/patch-plasmoid.sh` against a fake containment built from the patch. |
| `tests/scripts/test_static.py` | `py_compile`, `sh -n`/`bash -n`, shellcheck, tracked-file denylist. |

Run one file:

```
python3 -I -B -m unittest discover -s tests -t . -p 'test_cli.py' -v
```

How it works:

- Each server test starts `server/spinewall-server.py` as a subprocess on a free port. Every `SPINEWALL_*` path and `HOME` point into a temp directory.
- Models are synthetic. `tests/server/support.py` writes minimal `.skel`, `.json`, and `.atlas` files. No real model is used.
- `tests/scripts/test_patch.py` runs a copy of the script with only the `system_dir` line changed. The real script hardcodes `/usr/share/plasma/plasmoids/org.kde.desktopcontainment`.
- `tests/installer/test_setup.py` builds the Go binary, then runs it under a pty and with `--yes`, each in its own throwaway `HOME` and on its own port. The live wallpaper on `8777` is never touched.
- `tests/scripts/test_install.py` runs the bootstrap with a stub `curl` that serves the release files from disk, so no network is used. The real `sha256sum` still checks the fake binary, so a wrong `SHA256SUMS` is caught for real.

Known gaps:

- The `install.sh` bootstrap test cannot verify a real minisign signature: the release is unsigned until the captain adds the secret key, so the test covers the placeholder and checksum paths only. The runtime pin check is the Go test, gated by `SPINEWALL_TEST_NETWORK=1` (`go test ./internal/runtime`).
- The patch is tested against files rebuilt from its own old side. It is not tested against a real `plasma-desktop` 6.8.1 tree.
- shellcheck tests skip when `shellcheck` is not on PATH.

Known bugs, recorded as `expectedFailure`. Remove the decorator when the bug is fixed:

- `server/spinewall-server.py:529-532` - a `POST /layout` merge lets a pushed `model` overwrite the stored one. The comment at line 527 says the stored one wins.
- `server/spinewall-server.py` file serving - a `%00` in a model file path drops the connection instead of returning 404. The server stays up.
