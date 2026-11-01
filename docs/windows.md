# Spinewall on Windows

Status: unconfirmed. No one has run this on Windows yet. Results go to [issue #2](https://github.com/officialdad/spinewall/issues/2).

The Windows build runs the page in local mode. It needs no server and no Python on Windows. Click handling, `file://` loading, WebGL, and saved state on Windows are untested.

## Before you start

| Item | Detail |
|---|---|
| Spine license | Each end user needs a Spine Editor license. The runtime license forbids sharing the built package. |
| Models | You must have the right to use each model. |
| Build machine | Linux, macOS, or Windows with Git Bash. It needs `python3`, `npm`, `curl`, `sha256sum`, `tar`, and `sed`. |
| Host app | [Lively Wallpaper](https://github.com/rocksdanister/lively) or Wallpaper Engine. |

## Step 1 - get models

Public model pack for testing: <https://github.com/I6RaVeN9I/nikke-wallpaper-packs>, release `v1.0`. Each `cNNN.zip` holds one character. The pack has no license. The assets belong to SHIFT UP. Spinewall has no affiliation with them.

1. Open the `v1.0` release and download the zips you want.
2. Put the zips in one directory, for example `zips/`.

Your own Spine exports also work. Put one directory per model in `models/` (see [assets.md](assets.md)).

## Step 2 - build the package

Run in the repo root:

```bash
python3 -I scripts/package-windows.py --models models --import-zip zips/*.zip --out dist
```

- `--import-zip` extracts each zip into `--models`. It keeps the main model files and skips the `aim` and `cover` subdirectories.
- `--host lively`, `--host wallpaper-engine`, or `--host both` (default) picks the output.
- The script fetches the Spine runtimes from npm and checks their hashes.
- The script reports skipped models and the reason, for example an unsupported Spine version.

Output:

| Path | Use |
|---|---|
| `dist/lively/spinewall.zip` | Lively package. |
| `dist/lively/spinewall/` | The same files unpacked. |
| `dist/wallpaper-engine/spinewall/index.html` | Wallpaper Engine main file. |

Copy `dist/` to the Windows machine.

## Step 3a - Lively

1. Drag `spinewall.zip` into the Lively window.
2. Click the new Spinewall tile to set it as the wallpaper.
3. Click the character to change its animation.
4. Click the Edit button at the top left to open the model picker. Mouse input is on by default in Lively.

## Step 3b - Wallpaper Engine

Steps unconfirmed.

1. Open the Wallpaper Engine editor and click Create Wallpaper.
2. Drag `dist/wallpaper-engine/spinewall/index.html` onto the dialog.
3. Save the wallpaper. The editor copies the files to `wallpaper_engine\projects\myprojects\`.
4. Keep the `models` directory and the `.js` files beside `index.html`. Copy any file the editor leaves out into the project directory.

## Report results

Post on [issue #2](https://github.com/officialdad/spinewall/issues/2): Windows version, host and version, monitor count, and one of yes or no for each item:

- The model draws.
- A click changes the animation.
- The Edit button opens the picker.
- The model and layout survive a restart.
- The wallpaper pauses under a fullscreen app.

## Known limits

- Local mode keeps state in `localStorage`. Lively or Wallpaper Engine can clear it.
- Thumbnails stay in memory, so the picker rebuilds them each start.
- The CLI and keyboard shortcuts do not exist on Windows.
