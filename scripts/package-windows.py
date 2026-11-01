#!/usr/bin/env python3
"""Builds the Spinewall page for Windows hosts (Lively, Wallpaper Engine). See docs/windows.md.

The page runs in local mode, so no server and no Python are needed on Windows.
Run this on Linux or macOS, or on Windows with Git Bash. Windows support is unconfirmed (issue #2).
"""
import argparse
import importlib.util
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SERVED_EXTENSIONS = (".skel", ".json", ".atlas", ".png", ".jpg", ".jpeg", ".webp", ".txt")
PLAYER_FILES = ("spine-player37.js", "spine-player38.js", "spine-player40.js", "spine-player41.js", "spine-player42.js", "spine-player.css")
TITLE = "Spinewall"
DESCRIPTION = "Live 2D wallpaper that plays Spine models."
NOTICE = """Spinewall for Windows

This package holds the Spinewall page, the Spine player runtimes, and the models you chose.

Spine runtimes: the players are subject to the Spine Runtimes License,
https://esotericsoftware.com/spine-runtimes-license
The license forbids redistribution of the runtimes. Do not share this package.
Each end user needs a Spine Editor license.

Models: you must have the right to use each model. Spinewall has no affiliation with the model owners.
"""


def load_server_scanner(models_dir):
    # Reuses the server scan so the index matches what the Linux server lists.
    scratch = tempfile.mkdtemp(prefix="spinewall-pack-")
    env = {
        "SPINEWALL_MODELS": str(models_dir),
        "SPINEWALL_STATE": f"{scratch}/state.json",
        "SPINEWALL_PREFS": f"{scratch}/prefs.json",
        "SPINEWALL_LAYOUT": f"{scratch}/layout.json",
        "SPINEWALL_THUMBS": f"{scratch}/thumbs",
    }
    os.environ.update(env)
    spec = importlib.util.spec_from_file_location("spinewall_server_pack", ROOT / "server/spinewall-server.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def import_zip(archive, models_dir):
    """Extracts one model directory from a zip. Skips nested directories such as aim/ and cover/."""
    count = 0
    with zipfile.ZipFile(archive) as z:
        for info in z.infolist():
            # Some zips use backslash separators.
            parts = [p for p in re.split(r"[\\/]", info.filename) if p]
            if info.is_dir() or not parts or any(p in (".", "..") or ":" in p for p in parts):
                continue
            if len(parts) != 2 or not parts[1].lower().endswith(SERVED_EXTENSIONS):
                continue
            target = Path(models_dir) / parts[0] / parts[1]
            target.parent.mkdir(parents=True, exist_ok=True)
            with z.open(info) as src, open(target, "wb") as dst:
                shutil.copyfileobj(src, dst)
            count += 1
    return count


def build_theme(destination):
    subprocess.run(["bash", str(ROOT / "scripts/install.sh"), "--build-theme", str(destination)], check=True)


def write_page(theme, out):
    for name in PLAYER_FILES + ("local-host.js",):
        shutil.copy(theme / name, out / name)
    page = (theme / "index.html").read_text(encoding="utf-8")
    marker = '<script src="spine-player40.js"></script>'
    if marker not in page:
        sys.exit("error: theme/index.html has no spine-player40.js tag to inject before. Update this script.")
    hook = '<script>window.SPINEWALL_LOCAL = {modelsUrl: "models/index.json"};</script>\n'
    (out / "index.html").write_text(page.replace(marker, hook + marker, 1), encoding="utf-8")


def write_models(models_dir, out):
    server = load_server_scanner(models_dir)
    if not server.MODELS:
        sys.exit(f"error: no usable models in {models_dir}. Skipped: {json.dumps(server.SKIPPED)}")
    keys = ("id", "name", "group", "character", "skeleton", "atlas", "version", "runtime", "skin", "kind", "hideSlots", "defaultAnimation")
    index = {"models": [{k: m[k] for k in keys if m.get(k) is not None} for m in server.MODELS]}
    for m in server.MODELS:
        source = Path(models_dir) / m["id"]
        target = out / "models" / m["id"]
        target.mkdir(parents=True)
        for item in sorted(source.iterdir()):
            if item.is_file() and item.suffix.lower() in SERVED_EXTENSIONS:
                shutil.copy(item, target / item.name)
    (out / "models" / "index.json").write_text(json.dumps(index, indent=1), encoding="utf-8")
    for name, reason in server.SKIPPED.items():
        print(f"skipped model {name}: {reason}", file=sys.stderr)
    print(f"{len(server.MODELS)} models, {len(server.SKIPPED)} skipped")


def write_lively(out):
    info = {
        "AppVersion": "1.0.0.0",
        "Title": TITLE,
        "Desc": DESCRIPTION,
        "Author": "officialdad",
        "License": "See NOTICE.txt",
        "Type": 1,
        "FileName": "index.html",
        "Arguments": None,
        "IsAbsolutePath": False,
    }
    (out / "LivelyInfo.json").write_text(json.dumps(info, indent=2), encoding="utf-8")


def zip_dir(directory, archive):
    with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED) as z:
        for path in sorted(directory.rglob("*")):
            if path.is_file():
                z.write(path, path.relative_to(directory).as_posix())


def main():
    ap = argparse.ArgumentParser(description="Build Spinewall for Lively or Wallpaper Engine.")
    ap.add_argument("--models", required=True, help="Directory with one subdirectory per model.")
    ap.add_argument("--out", required=True, help="Output directory. It must not exist or must be empty.")
    ap.add_argument("--host", choices=("lively", "wallpaper-engine", "both"), default="both")
    ap.add_argument("--import-zip", nargs="*", default=[], metavar="ZIP", help="Extract model zips into --models first.")
    args = ap.parse_args()

    models_dir = Path(args.models)
    models_dir.mkdir(parents=True, exist_ok=True)
    for archive in args.import_zip:
        print(f"{archive}: {import_zip(archive, models_dir)} files")

    out_root = Path(args.out)
    if out_root.exists() and any(out_root.iterdir()):
        sys.exit(f"error: {out_root} is not empty. Pick a new directory.")

    with tempfile.TemporaryDirectory(prefix="spinewall-theme-") as work:
        theme = Path(work) / "theme"
        build_theme(theme)
        shutil.copy(ROOT / "theme/index.html", theme / "index.html")
        shutil.copy(ROOT / "theme/local-host.js", theme / "local-host.js")
        hosts = ("lively", "wallpaper-engine") if args.host == "both" else (args.host,)
        for host in hosts:
            out = out_root / host / "spinewall"
            out.mkdir(parents=True)
            write_page(theme, out)
            write_models(models_dir, out)
            (out / "NOTICE.txt").write_text(NOTICE, encoding="utf-8")
            if host == "lively":
                write_lively(out)
                zip_dir(out, out_root / "lively" / "spinewall.zip")
                print(f"Lively: {out_root / 'lively' / 'spinewall.zip'}")
            else:
                # The Wallpaper Engine editor creates project.json itself when it imports index.html.
                print(f"Wallpaper Engine: {out / 'index.html'}")


if __name__ == "__main__":
    main()
