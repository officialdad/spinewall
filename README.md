# spinewall

Live Spine 2D animations as a KDE Plasma 6 wallpaper, on a black background.

Spinewall works with any Spine 3.8, 4.0, 4.1, or 4.2 model. It is not tied to one game.

## How it works

- The `webwallpaper-kde` plugin shows a web page as the desktop background.
- The `spinewall` theme is that page. It draws the model with the Spine WebGL player.
- A local Python server on `127.0.0.1:8777` holds the state: model, animation, zoom, position.
- The page polls the server. The `spinewall` CLI and global shortcuts send commands to it.

## Input

| Mode | Input | Action |
|---|---|---|
| Normal | Left click | Next animation of the topmost character under the pointer |
| Edit | Left drag | Move the selected character |
| Edit | Mouse wheel | Zoom the selected character |
| Edit | Short left click | Next animation, or select the character |
| Edit | Keyboard in the picker | Search, arrow keys, Enter, Ctrl+Enter to add a character |

Edit mode opens a model picker with fuzzy search, a source filter, favorites, recents, character-variant grouping, a Characters / Scenes / All filter, an animation list with hover preview, and a strip for several characters at once. See [docs/assets.md](docs/assets.md).

Enter edit mode from the Plasma desktop menu: right-click the desktop, then choose "Enter Edit Mode". Hold-to-edit is disabled by the plasmoid patch.

## Requirements

- KDE Plasma 6 on Wayland or X11. Tested on Plasma 6.8.1, Wayland.
- [`webwallpaper-kde`](https://github.com/ekoputrapratama/webwallpaper-kde), installed and working.
- `python3`, `npm`, `curl`, `patch`, `systemd` (user session).
- Spine models. See [docs/assets.md](docs/assets.md).

## Install

1. Install `webwallpaper-kde` and restart plasmashell. Follow its README.
2. Run `scripts/install.sh`. It fetches the Spine runtimes (npm for 4.x, a pinned `spine-runtimes` commit for 3.8), installs the theme, the server, the CLI, and the user service.
3. Run `scripts/patch-plasmoid.sh`. It lets left clicks reach the wallpaper. See the warning below.
4. Restart plasmashell: `systemctl --user restart plasma-plasmashell.service`.
5. Put models in `~/.local/share/spinewall/models/`. See [docs/assets.md](docs/assets.md).
6. Restart the server: `systemctl --user restart spinewall.service`.
7. Right-click the desktop, open "Configure Desktop and Wallpaper", choose "WebWallpaper", and pick the `spinewall` theme.

## CLI

```
spinewall info
spinewall next-model | prev-model | random-model | set-model ID
spinewall next-anim | prev-anim | random-anim | set-anim NAME
spinewall zoom-in | zoom-out | move-left | move-right | move-up | move-down | reset-view
spinewall rescan
spinewall add-character [ID] | remove-character UID | clear-characters
spinewall next-char-anim UID | prev-char-anim UID | set-char-anim UID NAME | set-char-model UID ID
```

Bind these to global shortcuts in System Settings, under Shortcuts, with a custom command.

## Code map

| Path | Job |
|---|---|
| `theme/index.html` | The wallpaper page. One file, no build step. |
| `server/spinewall-server.py` | Local HTTP server. State, model scan, prefs, layout, thumbnails. |
| `bin/spinewall` | CLI. Thin `curl` wrapper over the server. |
| `scripts/install.sh` | Installs theme, server, CLI, and user service. Fetches the runtimes. |
| `scripts/patch-plasmoid.sh` | Applies `plasma/desktopcontainment.patch`. |
| `systemd/spinewall.service` | User service for the server. |
| `docs/assets.md` | Model layout and `model.json` keys. |
| `docs/credits.md` | Sources, tools, and test models. |

### `theme/index.html`

| Area | Functions |
|---|---|
| Config and runtimes | `RUNTIMES` (`spine38`, `spine40`, `spine41`, `spine42`), `API`, `POLL_MS`, click and scale limits |
| View and polling | `applyView`, `pushView`, `poll`, `load`, `setEditMode` |
| Picker data | `refreshPrefs`, `fuzzy`, `matchModel`, `comparator`, `buildItems` |
| Picker UI | `ensureChrome`, `renderPicker`, `computeLayout`, `renderWindow`, `makeCell`, `drawRail`, `setupRailScrub`, `onSearchKey` |
| Animation list | `animGroups`, `renderAnims`, `previewAnim`, `endPreview` |
| Thumbnails | `wantThumb`, `pumpThumbs`, `loadOrMakeThumb`, `renderThumb`, `headBox`, `unionBox` |
| Stage (all characters) | `makeLayer`, `createStageChar`, `stageFrame`, `frameOf`, `applyCharacters`, `destroyLayers` |
| Hit testing | `hitTest`, `charContains`, `inTriangle`, `selectChar` |
| Scenes | `hideSlotsOf`, `kind` filter in `buildItems` |

### `server/spinewall-server.py`

| Area | Functions |
|---|---|
| Model scan | `read_binary_version`, `read_json_version`, `read_atlas_regions`, `classify_kind`, `scan_model`, `scan_models`, `assign_characters`, `rescan` |
| State | `STATE`, `load_state`, `save`, `change_model`, `change_animation`, `set_view` |
| Prefs | `load_prefs`, `save_prefs`, `toggle_favorite`, `record_recent`, `set_prefs` |
| Characters | `add_character`, `remove_character`, `update_character`, `cycle_character_animation`, `set_layout`, `clean_characters` |
| HTTP | `command`, `Handler`: `/state`, `/models`, `/prefs`, `/info`, `/rescan`, `/thumb/<id>.png`, `/cmd/...`, `/edit/0\|1`, `/report`, `/fav/<id>`, `/layout`, `/view`, `/input` |

Data flow, state keys, and rules are in `CLAUDE.md`.

## Test models

Models are never committed. Test with the Esoteric Software example projects from the [spine-runtimes](https://github.com/EsotericSoftware/spine-runtimes) repo (`spineboy`, `raptor`, `goblins`, `hero`, `vine`, `stretchyman`, in 4.0 and 4.1), and with models you own.

1. Copy a model directory into `~/.local/share/spinewall/models/`.
2. Run `spinewall rescan`.
3. Run `spinewall set-model spineboy-4.1`.

See [docs/assets.md](docs/assets.md) for the layout.

## Credits

See [docs/credits.md](docs/credits.md).

- [Esoteric Software](https://esotericsoftware.com): Spine and [spine-runtimes](https://github.com/EsotericSoftware/spine-runtimes).
- [webwallpaper-kde](https://github.com/ekoputrapratama/webwallpaper-kde): the web page wallpaper plugin.
- [Nikke-db](https://github.com/Nikke-db) and [Waifuland](https://github.com/shinkuan/Waifuland): open-source viewers studied for design.

## Plasmoid patch warning

`plasma/desktopcontainment.patch` changes a copy of the Plasma desktop containment. The copy lives in `~/.local/share/plasma/plasmoids/org.kde.desktopcontainment` and shadows the system one.

The patch has three effects:
- Desktop icons no longer take left clicks, so the click reaches the wallpaper.
- Hold-to-enter-edit-mode is off.
- The containment tells the server when edit mode starts and stops.

Run `scripts/patch-plasmoid.sh` again after every `plasma-desktop` update. A stale copy can break the desktop. To undo, remove the local directory and restart plasmashell.

## Legal notice

Spinewall is a viewer. It includes no models and no Spine runtime. `scripts/install.sh` fetches the runtimes from Esoteric Software. Each user needs their own Spine Editor license under the Spine Runtimes License. Game characters and art belong to their owners, and Spinewall has no affiliation with them. Use only models you own or have the right to use, for personal non-commercial use. The MIT license covers this repository's code only.

## Configuration

| Variable | Default | Use |
|---|---|---|
| `SPINEWALL_PORT` | `8777` | Server and CLI port. The theme and the Plasma patch hardcode `8777`, so a changed port breaks the wallpaper. |
| `SPINEWALL_MODELS` | `~/.local/share/spinewall/models` | Model directory. |
| `SPINEWALL_STATE` | `~/.local/state/spinewall/state.json` | Current model, animation, and view. |
| `SPINEWALL_PREFS` | `~/.local/state/spinewall/prefs.json` | Favorites, recents, sort, filters. |
| `SPINEWALL_LAYOUT` | `~/.local/state/spinewall/layout.json` | Several-character layout. |
| `SPINEWALL_THUMBS` | `~/.cache/spinewall/thumbs` | Thumbnail cache. |

The server listens on `127.0.0.1` only and rejects requests with another `Host` header. It has no login. Any local user can send it commands.

## Troubleshooting

| Symptom | Action |
|---|---|
| Blank wallpaper | Run `spinewall info`. Check `models` and `skipped`. Restart plasmashell. |
| A model is missing | Run `spinewall rescan`. Read its reason under `skipped`. |
| A model has no picture | Run the atlas page check in [docs/assets.md](docs/assets.md). |
| Clicks do nothing | Run `scripts/patch-plasmoid.sh` again and restart plasmashell. Check that `input.fired` rises. |
| Black or broken after a screen lock | Restart plasmashell. The page reloads on resume. |

## Uninstall

1. Run `systemctl --user disable --now spinewall.service`.
2. Remove `~/.local/bin/spinewall`, `~/.local/bin/spinewall-server`, and `~/.config/systemd/user/spinewall.service`.
3. Remove `~/.local/share/webwallpaper/themes/spinewall`.
4. Remove `~/.local/share/plasma/plasmoids/org.kde.desktopcontainment`.
5. Remove `~/.local/state/spinewall` and `~/.cache/spinewall` if you want to drop your prefs.
6. Restart plasmashell.

## Not included

- Spine models. You supply them. Use only models you have the right to use.
- The Spine runtime. `scripts/install.sh` fetches it from npm. Its license is the Spine Runtimes License, so you need a Spine license to use it in your own product.

## Status

The wallpaper, edit mode, model picker, several characters at once, and scene filtering work. Headless tests passed. Check the real desktop after each change.

## License

MIT, for the code in this repository. The Spine runtimes and your models keep their own licenses.
