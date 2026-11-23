# Spinewall on other desktops

Status: the X11 backend, the kiosk fallback, and the `--screen current`
providers for Hyprland and sway are best effort and **unverified**. No one has
run them on real hardware yet. The proven backend is the layer-shell view on KDE
Plasma 6 Wayland; see [native-view.md](native-view.md). Report results on
[issue #2](https://github.com/officialdad/spinewall/issues/2).

`spinewall doctor` prints what this machine offers and names the line to run.
The lines below are what it prints for the unverified parts.

## What is proven, and what is not

| Desktop | View | `--screen current` | Status |
|---|---|---|---|
| KDE Plasma 6, Wayland | layer-shell view | KWin over D-Bus | Proven on Plasma 6.8 |
| sway, Hyprland, river, labwc, Wayfire | layer-shell view | sway and Hyprland providers | View unverified off Plasma; providers unverified |
| Any real X11 session (i3, XFCE, openbox) | X11 desktop-type view | needs `--screen KEY` | Unverified |
| GNOME (Wayland) | none | none | Unsupported |
| Anything else | kiosk browser `spinewall view --browser` | needs `--screen KEY` | Unverified |

## The X11 backend

X11 has no `zwlr_layer_shell_v1`, so the native view draws a desktop-type window
instead: one per monitor, kept at the bottom and out of focus by the window
manager. It is the same launcher and the same page as the layer-shell view, with
a different QML surface (`view-x11.qml`). The click mask still works, because
`QWindow::setMask` exists on X11 too.

Try it against a running server:

```bash
spinewall view --backend x11
```

Or start both halves in one process tree:

```bash
spinewall serve --with-view --view-backend x11
```

`spinewall setup` recommends the X11 backend on a real X11 session when the
launcher can build, and pins it in the autostart entry. Under Wayland, XWayland
is not a desktop, so the X11 backend is not offered there.

Known limits, untested:

- A desktop-type window only receives clicks on window managers that honour
  `_NET_WM_WINDOW_TYPE_DESKTOP`. Some fight the desktop icon manager over it.
- A compositing window manager may draw normal windows over it, but not always
  under the desktop icons.
- Per-monitor placement relies on each output's geometry; a window manager that
  forces a desktop window across the whole virtual screen would show one page.

## The kiosk browser fallback

Where no wallpaper layer is available, the page can run in a fullscreen browser.
It is not a wallpaper: it is a normal window above everything, with a taskbar
entry unless the compositor hides it. Hyprland and sway can pin it with a
window rule; spinewall never edits a compositor config.

```bash
spinewall view --browser
```

It uses the first of `chromium`, `chromium-browser`, `google-chrome-stable`,
`google-chrome`, or `firefox`, with `--kiosk`. The page still shows the Edit
button, so edit mode works with no containment patch. Nothing else is needed:
no launcher build and no layer-shell.

## GNOME

Unsupported. GNOME on Wayland does not offer `zwlr_layer_shell_v1`, and Mutter
does not honour a desktop-type XWayland window as a wallpaper. A real GNOME
backend needs a per-version GJS shell extension that embeds a WebKit surface,
which this project does not ship. On GNOME the only option today is the kiosk
browser fallback.

## `--screen current`

`spinewall --screen current <command>` acts on the monitor in use. The provider
is chosen from the session:

| Session | Source |
|---|---|
| KDE | `org.kde.KWin.activeOutputName` over D-Bus |
| Hyprland (`HYPRLAND_INSTANCE_SIGNATURE` set) | `hyprctl -j activeworkspace` and `hyprctl -j monitors` |
| sway (`SWAYSOCK` set) | `swaymsg -t get_outputs`, the focused output |
| Anything else | none; pass `--screen KEY` |

The provider matches the monitor's geometry to a stage key the server knows, the
same way KDE does. A lone monitor, or a page that has not connected yet, uses the
default stage.

## Report results

Post on [issue #2](https://github.com/officialdad/spinewall/issues/2): desktop,
compositor and version, monitor count, and one of yes or no for each item:

- The model draws on the wallpaper layer.
- A click on a character changes the animation.
- A click on empty wallpaper reaches the desktop.
- The Edit button opens the picker.
- Each monitor draws its own stage.
- `spinewall --screen current next-preset` changes the monitor under the pointer.
