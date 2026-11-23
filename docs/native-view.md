# The native layer-shell view

The native view draws the wallpaper without the `webwallpaper-kde` plugin and
without the patched Plasma desktop containment. It runs one layer-shell window
per monitor, each holding the same page in Qt WebEngine. The page keeps working:
the click, the picker, the presets, and the multi-character stage are unchanged.

It is the default backend where `spinewall doctor` reports the whole chain
present. The [KDE legacy backend](../README.md) stays selectable until the cutover.

## How it fits together

- The server (`spinewall serve`) holds the state and serves the page at
  `http://127.0.0.1:8777/`, exactly as it does for a browser.
- The launcher (`spinewall-view`) is a small C++ program. It loads `view.qml`,
  which puts one layer-shell surface on one output, and a `WebEngineView` that loads
  the page. The page carries `?view=native&screen=<key>` so it knows to report its
  click mask and to show an Edit button.
- `spinewall serve --with-view` runs both in one process tree: the server in the
  foreground, the launcher windows in a supervisor that restarts a window that
  exits on its own. The autostart entry uses this command.

## Why a C++ launcher, and why it builds on your machine

QtWebEngine cannot link into a `CGO_ENABLED=0` Go binary, so the binary never
holds a web engine. What the launcher needs from C++ is one call QML cannot make:
`QQuickWindow::setMask`, the input region. QML `Window` has no mask property, so a
QML-only surface can draw but cannot pass clicks through.

`spinewall setup` writes the launcher source and `view.qml` to
`~/.cache/spinewall/view` and builds the binary there with the system toolchain
(`c++`, `moc`, `pkg-config`, and the Qt Quick, Qml, WebEngineQuick, WebChannel,
Network, and DBus development files). `/no-mistakes` and the unit tests never build
it; the build runs on the user's machine, from source kept in the Go binary.

A build failure is not fatal. `spinewall serve --with-view` builds the launcher on
first start, and the message names the missing piece. `spinewall doctor` reports
whether the toolchain is present.

## The input mask

The layer-shell surface takes a click only inside its input region. That is what
replaces the Plasma containment patch: a click on a character reaches the page,
and a click on empty wallpaper reaches Plasma, so desktop icons and the desktop
menu keep working.

- In normal mode the page reports one rectangle per on-screen character, plus one
  for the Edit button, in window pixels, to `POST /mask`. The Edit button is the way
  into edit mode, so a click on it must never fall through to the desktop. The launcher polls `GET /mask` four times a second and
  calls `QWindow::setMask` only when the region's `rev` changed.
- In edit mode the page reports `{full: true}`, so the whole window takes clicks and
  the dock, the chip bar, and the drag handle work.
- A page with no character on screen reports only the Edit button, which lets every
  other click through. The mask starts full, so a page that has not reported yet never loses a
  click.

The mask is stored per screen, never written to disk, and reset to full when a
screen's stage is created. `spinewall mask --screen KEY` prints the region one
monitor's window uses, for a bug report. `spinewall screens` lists the keys, and
`--screen current` names the monitor under the pointer on KDE.

## Per-monitor windows

One window per monitor, each on its own layer-shell surface, each with its own stage
key. `spinewall view --list-monitors` prints the outputs the launcher sees. Each
window anchors to all four edges with `exclusionZone: -1`, so it never reserves
space on the desktop.

The launcher uses `LayerBottom`, proven on KDE Plasma 6 Wayland: the surface sits
above Plasma's desktop windows and below normal windows. `--layer background` is
available for a compositor that wants the wallpaper layer instead.

## Reload on resume

The old webwallpaper plugin paused on fullscreen windows and never restarted the
draw loop, so the page reloaded on `webwallpaper:resume`. The native surface has no
such pause: a fullscreen window hides it and it stays alive underneath. A lock, a suspend or
a fullscreen client can still leave the draw loop dead without the window ever
becoming invisible, so the launcher reloads the page on the session's own signals:
logind `PrepareForSleep` and the session `Unlock`, and `ActiveChanged` from
`org.freedesktop.ScreenSaver` or `org.kde.screensaver`. A burst of signals makes one
reload, and the launcher logs `SPVIEW reload on resume`. The page still reloads on
`webglcontextlost` as well.

## Edit mode without the patch

There is no desktop context menu to enter edit mode, so the page shows an Edit
button when it is hosted by the native view (`?view=native`). Bind `spinewall edit`
to a global shortcut to flip it from any monitor; a bare `edit` toggles, and
`spinewall edit on` or `spinewall edit off` sets it. The server's `/edit/0` and
`/edit/1` stay the API the page and the shortcut both use.

## What still needs a person at the keyboard

The proven pieces are the layer-shell surface, WebGL on the GPU, click delivery, the
input mask, and multi-window placement. These are checked on a real desktop with a
person present, not by the automated suite:

- A real logout and login, to see the autostart window come up on the right monitor.
- A fullscreen game or video, then back, to see the wallpaper return.
- The screen lock and unlock.
- A left click on a desktop icon reaching Plasma through the mask.
