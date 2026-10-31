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

# Spine 4.0 and 4.1 binaries are not compatible, so the theme loads one player per minor version.
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

mkdir -p "$theme_dir" "$models_dir" "$bin_dir" "$HOME/.config/systemd/user"
cp "$root/theme/index.html" "$root/theme/spinewall.theme" "$theme_dir/"
fetch_runtime 4.0.31 spine40
fetch_runtime 4.1.56 spine41
grep -q '^var spine40 = ' "$theme_dir/spine-player40.js"
grep -q '^var spine41 = ' "$theme_dir/spine-player41.js"

install -m 755 "$root/server/spinewall-server.py" "$bin_dir/spinewall-server"
install -m 755 "$root/bin/spinewall" "$bin_dir/spinewall"
cp "$root/systemd/spinewall.service" "$HOME/.config/systemd/user/"
systemctl --user daemon-reload
systemctl --user enable spinewall.service
systemctl --user restart spinewall.service

echo "Installed. Put models in $models_dir (see docs/assets.md)."
echo "Next: run scripts/patch-plasmoid.sh, then select the spinewall theme in the WebWallpaper config."
