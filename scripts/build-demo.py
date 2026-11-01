#!/usr/bin/env python3
"""Builds the Pages demo into OUT_DIR from theme/index.html, so the demo is the real page in local mode.

The demo has only Spine 4.1 models, so it loads one player from the npm CDN with a pinned version and an SRI hash.
The runtime license forbids committing the player.
"""
import shutil
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
PLAYER = "https://cdn.jsdelivr.net/npm/@esotericsoftware/spine-player@4.1.56/dist"
PLAYER_SHA384 = "sha384-CuOaoYKedSVxBvygRU/fMocN3QIqyYs88B9SZ7Ci4NZoFo5Vyhf7/zWYzrBKA+tA"
CSS_SHA384 = "sha384-SAzRnvZaanzeQyV9076wVkiVpz8Ps2uicu0yff7GJdBU2Pr65zYUg5tYbXT8Uzaz"

SCRIPTS = (
    '<script src="spine-player40.js"></script>\n'
    '<script src="spine-player41.js"></script>\n'
    '<script src="spine-player42.js"></script>\n'
    '<script src="spine-player38.js"></script>\n'
    '<script src="local-host.js"></script>\n'
)
STYLESHEET = '<link rel="stylesheet" href="spine-player.css">'

HEAD = f"""<title>Spinewall demo</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="stylesheet" href="{PLAYER}/spine-player.css" integrity="{CSS_SHA384}" crossorigin="anonymous">"""

LOADER = f"""<script>window.SPINEWALL_LOCAL = {{modelsUrl: "models/index.json"}};</script>
<script src="{PLAYER}/iife/spine-player.js" integrity="{PLAYER_SHA384}" crossorigin="anonymous"></script>
<script>window.spine41 = window.spine;</script>
<script src="local-host.js"></script>
"""

# The page treats a press anywhere as a click on the model, so the notice keeps its events to itself.
NOTICE = """<footer id="legal">
<p>These sample characters and their art are the property of SHIFT UP Corporation, shown as samples only. Spinewall is a fan project. SHIFT UP does not sponsor, endorse, or affiliate with it. Spinewall installs no models. <a href="https://github.com/officialdad/spinewall">Spinewall on GitHub</a> and the <a href="https://github.com/officialdad/spinewall#readme">README</a>.</p>
</footer>
<style>
#legal{position:fixed;left:16px;bottom:16px;z-index:3;max-width:min(520px,calc(100% - 32px));padding:8px 12px;background:#000;color:#fff;border:2px solid #ffd54a;border-radius:8px;font:14px/1.4 system-ui,sans-serif}
#legal p{margin:0}
#legal a{color:#8ab4ff}
#legal a:focus-visible{outline:3px solid #ffd54a;outline-offset:2px}
</style>
<script>
["pointerdown", "pointerup", "pointermove", "wheel"].forEach(type =>
  document.getElementById("legal").addEventListener(type, e => e.stopPropagation()));
</script>
"""


def build(out):
    page = (ROOT / "theme/index.html").read_text(encoding="utf-8")
    for needle in (SCRIPTS, STYLESHEET, "</body>"):
        if needle not in page:
            sys.exit(f"error: theme/index.html no longer contains {needle.strip()!r}. Update scripts/build-demo.py.")
    page = page.replace(SCRIPTS, LOADER, 1).replace(STYLESHEET, HEAD, 1).replace("</body>", NOTICE + "</body>", 1)
    out = Path(out)
    out.mkdir(parents=True, exist_ok=True)
    shutil.copytree(ROOT / "docs/demo/models", out / "models", dirs_exist_ok=True)
    shutil.copy(ROOT / "theme/local-host.js", out / "local-host.js")
    (out / "index.html").write_text(page, encoding="utf-8")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        sys.exit("usage: build-demo.py OUT_DIR")
    build(sys.argv[1])
