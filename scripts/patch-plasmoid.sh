#!/usr/bin/env bash
# Copies the system desktop containment to ~/.local and applies plasma/desktopcontainment.patch.
# Run it again after every plasma-desktop update, because the copy shadows the system one.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
system_dir=/usr/share/plasma/plasmoids/org.kde.desktopcontainment
local_dir="$HOME/.local/share/plasma/plasmoids/org.kde.desktopcontainment"

if [ ! -d "$system_dir" ]; then
  echo "error: $system_dir not found. Plasma 6 desktop containment is required." >&2
  exit 1
fi

backup=""
if [ -d "$local_dir" ]; then
  backup="$local_dir.bak.$(date +%s)"
  mv "$local_dir" "$backup"
  echo "Moved the previous local copy to $backup"
fi

mkdir -p "$(dirname "$local_dir")"
cp -r "$system_dir" "$local_dir"
if ! patch -p1 -d "$local_dir" < "$root/plasma/desktopcontainment.patch"; then
  rm -rf "$local_dir"
  [ -n "$backup" ] && mv "$backup" "$local_dir"
  echo "error: patch does not apply to this plasma-desktop version. Update plasma/desktopcontainment.patch." >&2
  exit 1
fi

echo "Patched. Run: systemctl --user restart plasma-plasmashell.service"
