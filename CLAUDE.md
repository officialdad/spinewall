# spinewall

Live Spine 2D wallpaper for KDE Plasma 6. Any Spine 3.8, 4.0, 4.1, or 4.2 model works. Do not add game-specific code or names. Test models and credits are in `README.md` and `docs/credits.md`.

## Layout

| Path | Job |
|---|---|
| `theme/index.html` | The wallpaper page. Draws the model, handles click, drag, wheel. |
| `server/spinewall-server.py` | Local HTTP server on `127.0.0.1:8777`. Holds state, scans models, serves model files. |
| `bin/spinewall` | CLI. Sends commands to the server. |
| `plasma/desktopcontainment.patch` | Patch for a local copy of the Plasma desktop containment. |
| `scripts/install.sh` | Installs theme, server, CLI, user service. Fetches the Spine runtimes from npm. |
| `scripts/patch-plasmoid.sh` | Applies the patch to a local copy of the containment. |
| `systemd/spinewall.service` | User service for the server. |
| `docs/assets.md` | Model directory layout and `model.json` keys. |
| `docs/credits.md` | Sources, tools, and test models. Add a row when you use a new source. |
| `README.md` | Install steps, CLI, and the code map by function. Keep the code map in step with the code. |
| `~/.local/state/spinewall/prefs.json` | Favorites, recents, layout, sort, variants. Server-owned, apart from `STATE`, never bumps `rev` or `vrev`. Endpoints: `GET /prefs`, `POST /prefs`, `POST /fav/<id>`. |

## Data flow

1. A click, shortcut, or CLI call sends `GET /cmd/<name>[/arg]` to the server.
2. The server changes `STATE` and bumps `rev` (model, animation) or `vrev` (view).
3. The page polls `/state` every 700 ms and applies the change when a revision differs.
4. The page reports the animation list to `POST /report` after each model load.
5. The patched containment calls `GET /edit/0|1` when edit mode changes. The page reads `edit` from `/state`.

## Rules

- Never commit models, `.skel`, `.atlas`, atlas images, or Spine runtime files. The runtime license forbids redistribution. The install script fetches it.
- The Spine players define the global `spine`. The install script renames the 4.x players to `spine40`, `spine41`, and `spine42` with `sed`. It wraps the 3.8 player in a function that returns `spine38`, because that player names `spine` internally. Keep the theme and the script in step.
- Server state keys are `model`, `animation`, `rev`, `scale`, `x`, `y`, `vrev`, `characters`, `crev`. The page and server share them. Change both together.
- `characters` is a list of `{uid, model, animation, x, y, scale}` in z order, last on top. `crev` bumps on each change. An empty list means single-character mode, which uses the old keys alone. The list persists in `layout.json` (env `SPINEWALL_LAYOUT`), apart from `STATE_FILE`.
- Several characters share one canvas per Spine runtime. Layers stack by their topmost character, so characters of different runtimes interleave by layer only. Two overlapping characters of different versions can draw in the wrong order.
- Removing down to one character folds it back into `model`, `animation`, `scale`, `x`, `y`.
- A click changes only the topmost character under the pointer. Hit testing uses triangle tests on world vertices, so a transparent area does nothing.
- The server scans models at start and on `GET /rescan` (`spinewall rescan`). A new model appears after a rescan.
- `model.json` `kind` is `character` or `scene`. The server detects it from region names when absent. A scene draws first in multi mode and never takes a click. `hideSlots` removes backdrop slots from drawing, framing, thumbnails, and hit tests.
- The picker filter `kind` (Characters, Scenes, All) lives in `prefs.json`.
- Position `x` and `y` are fractions of the window size. Scale ranges from 0.3 to 8.
- In multi mode, edit-mode drag and wheel move the selected character. A press on a character selects it.
- Normal mode takes left click only. Edit mode takes drag, wheel, and a short left click. A click moves under 30 px, so a drag never counts as one.

## Plasma constraints

- Left clicks do not reach a wallpaper by default. The containment patch exists for this.
- The patch targets `plasma-desktop` 6.8.1. If it fails, regenerate it against the new `FolderView.qml` and `main.qml`.
- `webwallpaper-kde` pauses on fullscreen windows. The draw loop stays dead after resume, so the page reloads on `webwallpaper:resume`.
- WebGL context loss blanks the wallpaper. The page reloads on `webglcontextlost`.
- Plasma on Wayland: global shortcuts live in `kwin_wayland`. Register them with `kglobalshortcutsrc` or D-Bus `org.kde.kglobalaccel`.

## Check a change

1. Run `scripts/install.sh`. It restarts the server.
2. Run `spinewall info`. Check `models`, `skipped`, and `input`.
3. Test on the real desktop. Click in normal mode and check `input.fired` rises. Drag and wheel in edit mode.
4. A bug fix needs a reproduction in the real wallpaper first, not only in a browser tab.

## Next phase

- Check multi-character and the picker on the real desktop after each change.
- Fetch the Nikke ids that are background-only, only where `hideSlots` can leave a character.
- Add Spine 3.7 support, and an `additional_model_dirs` config.

## Style

- Write short sentences. Use a plain dash, never an em dash.
- Comments explain why, not what.
- Fix lint, test, and flaky-test errors you find, even if you did not cause them.
