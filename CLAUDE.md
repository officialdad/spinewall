# spinewall

Live Spine 2D wallpaper for KDE Plasma 6. Any Spine 4.0 or 4.1 model works. Do not add game-specific code or names.

## Layout

| Path | Job |
|---|---|
| `theme/index.html` | The wallpaper page. Draws the model, handles click, drag, wheel. |
| `server/spinewall-server.py` | Local HTTP server on `127.0.0.1:8777`. Holds state, scans models, serves model files. |
| `bin/spinewall` | CLI. Sends commands to the server. |
| `plasma/desktopcontainment.patch` | Patch for a local copy of the Plasma desktop containment. |
| `scripts/install.sh` | Installs theme, server, CLI, user service. Fetches the Spine runtimes from npm. |
| `scripts/patch-plasmoid.sh` | Applies the patch to a local copy of the containment. |
| `docs/assets.md` | Model directory layout and `model.json` keys. |

## Data flow

1. A click, shortcut, or CLI call sends `GET /cmd/<name>[/arg]` to the server.
2. The server changes `STATE` and bumps `rev` (model, animation) or `vrev` (view).
3. The page polls `/state` every 700 ms and applies the change when a revision differs.
4. The page reports the animation list to `POST /report` after each model load.
5. The patched containment calls `GET /edit/0|1` when edit mode changes. The page reads `edit` from `/state`.

## Rules

- Never commit models, `.skel`, `.atlas`, atlas images, or Spine runtime files. The runtime license forbids redistribution. The install script fetches it.
- The two Spine players define the global `spine`. The install script renames them to `spine40` and `spine41`. Keep the theme and the script in step.
- Server state keys are `model`, `animation`, `rev`, `scale`, `x`, `y`, `vrev`. The page and server share them. Change both together.
- The server scans models at start only. A new model needs a server restart until a rescan endpoint exists.
- Position `x` and `y` are fractions of the window size. Scale ranges from 0.3 to 8.
- Normal mode takes left click only. Edit mode takes drag and wheel only. Keep the two apart.

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

A model picker UI in edit mode. Plan:
- Add a `/models` consumer in `theme/index.html` that shows only while `edit` is on.
- Switch models with `GET /cmd/set-model/<id>`.
- Keep the picker out of the normal mode view, so the wallpaper stays clean.
- Add a server rescan endpoint so new models appear without a restart.

## Style

- Write short sentences. Use a plain dash, never an em dash.
- Comments explain why, not what.
- Fix lint, test, and flaky-test errors you find, even if you did not cause them.
