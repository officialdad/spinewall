#!/usr/bin/env bash
# Installs spinewall user-local. Run from any directory.
# The Spine runtime is fetched from npm because its license forbids redistribution.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
theme_dir="$HOME/.local/share/webwallpaper/themes/spinewall"
models_dir="$HOME/.local/share/spinewall/models"
bin_dir="$HOME/.local/bin"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# Spine 4.0, 4.1 and 4.2 binaries are not compatible, so the theme loads one player per minor version.
fetch_runtime() {
  local version="$1" global="$2"
  (cd "$work" && npm pack "@esotericsoftware/spine-player@$version" --silent >/dev/null)
  local dir="$work/$global"
  mkdir -p "$dir"
  tar xzf "$work/esotericsoftware-spine-player-$version.tgz" -C "$dir"
  # Both players define the global `spine`, so rename it to keep them apart.
  sed "s/^var spine = /var $global = /" "$dir/package/dist/iife/spine-player.js" \
    > "$theme_dir/spine-player${global#spine}.js"
  cp "$dir/package/dist/spine-player.css" "$theme_dir/spine-player.css"
}

# The 3.8 player is not on npm. It comes from a pinned commit of the official runtimes repo.
# The player refers to the global `spine` internally, so a rename with sed breaks it.
# A function scope keeps the global private and exposes it as `spine38`.
spine38_commit=8b4844bd4b193ba9e54487ed397a777993cbad56
spine38_sha256=3f335337a8fa9c51c6502a7557ea44cd2bcb4f22483adae275e4ecf5ec69faec
fetch_runtime_38() {
  local url="https://raw.githubusercontent.com/EsotericSoftware/spine-runtimes/$spine38_commit/spine-ts/build/spine-player.js"
  curl -fsSL "$url" -o "$work/spine-player38.js"
  echo "$spine38_sha256  $work/spine-player38.js" | sha256sum -c --quiet
  { echo "var spine38 = (function () {"; cat "$work/spine-player38.js"; printf '\nreturn spine;\n})();\n'; } \
    > "$theme_dir/spine-player38.js"
}

mkdir -p "$theme_dir" "$models_dir" "$bin_dir" "$HOME/.config/systemd/user"
cp "$root/theme/index.html" "$root/theme/spinewall.theme" "$theme_dir/"
fetch_runtime 4.0.31 spine40
fetch_runtime 4.1.56 spine41
fetch_runtime 4.2.120 spine42
fetch_runtime_38
grep -q '^var spine40 = ' "$theme_dir/spine-player40.js"
grep -q '^var spine41 = ' "$theme_dir/spine-player41.js"
grep -q '^var spine42 = ' "$theme_dir/spine-player42.js"
grep -q '^var spine38 = ' "$theme_dir/spine-player38.js"

install -m 755 "$root/server/spinewall-server.py" "$bin_dir/spinewall-server"
install -m 755 "$root/bin/spinewall" "$bin_dir/spinewall"
cp "$root/systemd/spinewall.service" "$HOME/.config/systemd/user/"
systemctl --user daemon-reload
systemctl --user enable spinewall.service
systemctl --user restart spinewall.service

echo "Installed. Put models in $models_dir (see docs/assets.md)."
echo "Next: run scripts/patch-plasmoid.sh, then select the spinewall theme in the WebWallpaper config."
