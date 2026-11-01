# Page tests

Headless Playwright tests for `theme/index.html`. They run the real page in Chromium against the real server and the real Spine runtimes.

## What runs

| Spec | Covers |
|---|---|
| `specs/runtimes.spec.mjs` | Loads 3.8, 4.0, 4.1, and 4.2 models. Model switch. Poll applies a change within 2 s. Missing-runtime notice. `webglcontextlost` reload. |
| `specs/input.spec.mjs` | Normal-mode click and `input.fired`. Edit-mode drag and wheel. Scale clamp. The 30 px click rule. |
| `specs/multi.spec.mjs` | Add and remove characters. Topmost hit test. Empty-space click. Scene order and click pass-through. Canvas z-order. |
| `specs/picker.spec.mjs` | Windowed list, fuzzy search, kind filter, Alt+F favorite, variants, keyboard, `/models` retry when the server starts late. |
| `specs/a11y.spec.mjs` | axe-core scan of the picker. Fails on serious or critical WCAG 2.2 AA violations, color contrast included. Console errors. |

Models are synthetic. `lib/models.mjs` writes flat colored squares with a one-bone skeleton, so no game data is involved.

## Run

```bash
cd tests/page
npm install
npx playwright install --with-deps chromium   # CI and fresh machines
npm test
```

`setup.sh` runs `scripts/install.sh` into a throwaway `HOME` under `.work/`. It fetches the runtimes from npm and GitHub, so the first run needs network. It stubs `systemctl`. Run `./setup.sh --force` to reinstall.

Each test starts its own server with its own state directory and removes it after.

## Port

The page hardcodes `http://127.0.0.1:8777`. The suite copies the page to `.work/` and rewrites the port from `SPINEWALL_PORT_TEST`. It never modifies `theme/`.

The suite exits with an error if the port is taken. A running wallpaper server uses 8777, so run locally with a free port:

```bash
SPINEWALL_PORT_TEST=18777 npm test
```

CI uses the default 8777.

## WebGL

Chromium runs with SwiftShader (`--use-gl=angle --use-angle=swiftshader --enable-unsafe-swiftshader`). No GPU is needed. The page loads from `file://`, so `--allow-file-access-from-files` is set.

## Known-failing tests

None. If a test marks a real page bug, use `test.fail` and list it here. Remove the mark when you fix the bug.

## Not covered

- Real Plasma input. Click delivery to a wallpaper needs the containment patch and a real desktop.
- Real GPU rendering. SwiftShader checks that the page draws, not how it looks.
- `webwallpaper:resume` and fullscreen pause. These need the `webwallpaper-kde` plugin.
- A pixel check of the drawn model. Tests read state and DOM.
