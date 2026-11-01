# Tests

## Server, CLI, and scripts (Python, stdlib only)

Run from the repository root:

```
python3 -I -m unittest discover -s tests -t . -v
```

The run takes about 30 s and needs no network, no Plasma, and no models.

| File | Covers |
|---|---|
| `tests/server/test_scan.py` | Version detection, skip reasons, `model.json` overrides, `classify_kind`, `assign_characters`. |
| `tests/server/test_api.py` | Every `/cmd` name, characters, layout, prefs, recents, thumbnails, `/report`, `/input`, rescan, concurrency. |
| `tests/server/test_security.py` | Host, Origin, and `Sec-Fetch-Site` guards, CORS, 415, 413, file whitelist, traversal, corrupt state files, legacy state migration. |
| `tests/server/test_cli.py` | `bin/spinewall` against a live server: encoding, usage errors, exit codes. |
| `tests/scripts/test_install.py` | `scripts/install.sh` with stub `npm`, `curl`, `sha256sum`, `systemctl`. |
| `tests/scripts/test_patch.py` | `scripts/patch-plasmoid.sh` against a fake containment built from the patch. |
| `tests/scripts/test_static.py` | `py_compile`, `bash -n`, shellcheck, tracked-file denylist. |

Run one file:

```
python3 -I -B -m unittest discover -s tests -t . -p 'test_cli.py' -v
```

How it works:

- Each server test starts `server/spinewall-server.py` as a subprocess on a free port. Every `SPINEWALL_*` path and `HOME` point into a temp directory.
- Models are synthetic. `tests/server/support.py` writes minimal `.skel`, `.json`, and `.atlas` files. No real model is used.
- `tests/scripts/test_patch.py` runs a copy of the script with only the `system_dir` line changed. The real script hardcodes `/usr/share/plasma/plasmoids/org.kde.desktopcontainment`.

Known gaps:

- The `install.sh` happy path uses a stub `sha256sum`, so the pinned hashes are not checked offline. Run the real install check with `SPINEWALL_TEST_NETWORK=1`.
- The patch is tested against files rebuilt from its own old side. It is not tested against a real `plasma-desktop` 6.8.1 tree.
- shellcheck tests skip when `shellcheck` is not on PATH.

Known bugs, recorded as `expectedFailure`. Remove the decorator when the bug is fixed:

- `server/spinewall-server.py:529-532` - a `POST /layout` merge lets a pushed `model` overwrite the stored one. The comment at line 527 says the stored one wins.
- `server/spinewall-server.py` file serving - a `%00` in a model file path drops the connection instead of returning 404. The server stays up.
