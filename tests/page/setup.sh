#!/usr/bin/env bash
# Builds the theme with the real runtimes into a throwaway HOME for the page tests.
# It uses the spinewall binary built from this tree, so the tests never depend on a published release.
# `spinewall build-theme` fetches the runtimes over the network on the first run.
# usage: tests/page/setup.sh [--force]
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
work="$here/.work"
home="$work/home"
theme="$home/.local/share/webwallpaper/themes/spinewall"
marker="$theme/spine-player37.js"

if [ "${1:-}" = "--force" ]; then rm -rf "$work"; fi
if [ -f "$marker" ]; then
  echo "setup: runtimes already installed in $home"
  exit 0
fi

mkdir -p "$work/bin" "$home"
(cd "$repo" && CGO_ENABLED=0 go build -o "$work/bin/spinewall" ./cmd/spinewall)
# XDG_CACHE_HOME is cleared so the runtime cache lands in the throwaway HOME, not the real one.
env -u XDG_CACHE_HOME HOME="$home" "$work/bin/spinewall" build-theme "$theme"

for global in 37 38 40 41 42; do
  test -s "$theme/spine-player$global.js" \
    || { echo "error: spine-player$global.js is missing after build-theme." >&2; exit 1; }
done
echo "setup: installed into $home"
