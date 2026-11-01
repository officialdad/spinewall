#!/usr/bin/env bash
# Installs spinewall user-local. Run from any directory.
# The Spine runtime is fetched from npm because its license forbids redistribution.
set -euo pipefail

# --build-theme DIR only builds the theme into DIR. package-windows.py uses it so the runtime pins stay in this file.
build_only=""
if [ "${1:-}" = "--build-theme" ]; then
  build_only="${2:?error: --build-theme needs a directory.}"
fi

tools=(npm curl sha256sum tar sed)
[ -z "$build_only" ] && tools+=(python3 systemctl)
for tool in "${tools[@]}"; do
  command -v "$tool" >/dev/null || { echo "error: $tool is required and was not found on PATH." >&2; exit 1; }
done

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
theme_dir="$HOME/.local/share/webwallpaper/themes/spinewall"
models_dir="$HOME/.local/share/spinewall/models"
state_dir="$HOME/.local/state/spinewall"
cache_dir="$HOME/.cache/spinewall"
bin_dir="$HOME/.local/bin"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
# Files are built here first, so a failed check never leaves a half-installed theme.
stage="$work/theme"
mkdir -p "$stage"

# Spine 4.0, 4.1 and 4.2 binaries are not compatible, so the theme loads one player per minor version.
# Each tarball is pinned by sha256, so a changed registry copy stops the install.
fetch_runtime() {
  local version="$1" global="$2" sha256="$3"
  local tarball="$work/esotericsoftware-spine-player-$version.tgz"
  (cd "$work" && npm pack "@esotericsoftware/spine-player@$version" --silent >/dev/null)
  echo "$sha256  $tarball" | sha256sum -c --quiet
  local dir="$work/$global"
  mkdir -p "$dir"
  tar xzf "$tarball" -C "$dir"
  # Both players define the global `spine`, so rename it to keep them apart.
  sed "s/^var spine = /var $global = /" "$dir/package/dist/iife/spine-player.js" \
    > "$stage/spine-player${global#spine}.js"
  grep -q "^var $global = " "$stage/spine-player${global#spine}.js" \
    || { echo "error: the $version player has no 'var spine = ' line to rename. Update this script." >&2; exit 1; }
  cp "$dir/package/dist/spine-player.css" "$stage/spine-player.css"
}

# The 3.7 and 3.8 players are not on npm. Each comes from a pinned commit of the official runtimes repo.
# The player refers to the global `spine` internally, so a rename with sed breaks it.
# A function scope keeps the global private and exposes it as `spine37` or `spine38`.
fetch_runtime_legacy() {
  local global="$1" commit="$2" sha256="$3"
  local file="spine-player${global#spine}.js"
  local url="https://raw.githubusercontent.com/EsotericSoftware/spine-runtimes/$commit/spine-ts/build/spine-player.js"
  curl -fsSL "$url" -o "$work/$file"
  echo "$sha256  $work/$file" | sha256sum -c --quiet
  { echo "var $global = (function () {"; cat "$work/$file"; printf '\nreturn spine;\n})();\n'; } \
    > "$stage/$file"
  grep -q "^var $global = " "$stage/$file"
}

fetch_runtime 4.0.31 spine40 db13daf8219b1de211785ed6bf70588b66879168cd9b7406aa62b30cef56d8b1
fetch_runtime 4.1.56 spine41 5c6d530857d6eb18d8bb5f8b10e4ba62fed42e887bf874088e91fe57f2307a80
fetch_runtime 4.2.120 spine42 f553d60910e6069b121555d4575c60f31c65d6fd2a3e349328ba9d2882dd79f1
fetch_runtime_legacy spine38 8b4844bd4b193ba9e54487ed397a777993cbad56 3f335337a8fa9c51c6502a7557ea44cd2bcb4f22483adae275e4ecf5ec69faec
fetch_runtime_legacy spine37 9639bcc81722d7178fd9d1cdc1a3d55a4c91f989 7e94ec3760c98013b2eb4449fc9ad0fe84b7a3cbe9c4368e326c7339628a3feb
cp "$root/theme/index.html" "$root/theme/local-host.js" "$root/theme/spinewall.theme" "$stage/"

if [ -n "$build_only" ]; then
  mkdir -p "$build_only"
  cp "$stage"/* "$build_only/"
  exit 0
fi

# Everything checked. Only now does the install touch the home directory.
mkdir -p "$theme_dir" "$models_dir" "$state_dir" "$cache_dir" "$bin_dir" "$HOME/.config/systemd/user"
cp "$stage"/* "$theme_dir/"

install -m 755 "$root/server/spinewall-server.py" "$bin_dir/spinewall-server"
install -m 755 "$root/bin/spinewall" "$bin_dir/spinewall"
cp "$root/systemd/spinewall.service" "$HOME/.config/systemd/user/"
systemctl --user daemon-reload
systemctl --user enable spinewall.service
systemctl --user restart spinewall.service

echo "Installed. Put models in $models_dir (see docs/assets.md)."
echo "Next: run scripts/patch-plasmoid.sh, then select the spinewall theme in the WebWallpaper config."
