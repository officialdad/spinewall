# spinewall

Live Spine 2D animations as a KDE Plasma 6 wallpaper, on a black background.

Spinewall works with any Spine 4.0 or 4.1 model. It is not tied to one game.

## How it works

- The `webwallpaper-kde` plugin shows a web page as the desktop background.
- The `spinewall` theme is that page. It draws the model with the Spine WebGL player.
- A local Python server on `127.0.0.1:8777` holds the state: model, animation, zoom, position.
- The page polls the server. The `spinewall` CLI and global shortcuts send commands to it.

## Input

| Mode | Input | Action |
|---|---|---|
| Normal | Left click | Next animation |
| Edit | Left drag | Move the model |
| Edit | Mouse wheel | Zoom the model |

Enter edit mode from the Plasma desktop menu: right-click the desktop, then choose "Enter Edit Mode". Hold-to-edit is disabled by the plasmoid patch.

## Requirements

- KDE Plasma 6 on Wayland or X11. Tested on Plasma 6.8.1, Wayland.
- [`webwallpaper-kde`](https://github.com/ekoputrapratama/webwallpaper-kde), installed and working.
- `python3`, `npm`, `curl`, `patch`, `systemd` (user session).
- Spine models. See [docs/assets.md](docs/assets.md).

## Install

1. Install `webwallpaper-kde` and restart plasmashell. Follow its README.
2. Run `scripts/install.sh`. It fetches the Spine runtimes from npm, installs the theme, the server, the CLI, and the user service.
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
```

Bind these to global shortcuts in System Settings, under Shortcuts, with a custom command.

## Plasmoid patch warning

`plasma/desktopcontainment.patch` changes a copy of the Plasma desktop containment. The copy lives in `~/.local/share/plasma/plasmoids/org.kde.desktopcontainment` and shadows the system one.

The patch has three effects:
- Desktop icons no longer take left clicks, so the click reaches the wallpaper.
- Hold-to-enter-edit-mode is off.
- The containment tells the server when edit mode starts and stops.

Run `scripts/patch-plasmoid.sh` again after every `plasma-desktop` update. A stale copy can break the desktop. To undo, remove the local directory and restart plasmashell.

## Not included

- Spine models. You supply them.
- The Spine runtime. `scripts/install.sh` fetches it from npm. Its license is the Spine Runtimes License, so you need a Spine license to use it in your own product.

## Status

Early. The first release covers the Plasma web wallpaper with edit mode. A model picker in edit mode is the next step.

## License

MIT, for the code in this repository. The Spine runtimes and your models keep their own licenses.
