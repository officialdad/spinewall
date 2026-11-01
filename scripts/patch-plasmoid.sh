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

mkdir -p "$(dirname "$local_dir")"
# The patch runs on a copy beside the target, so a failure leaves the current local copy untouched.
candidate="$(mktemp -d "$local_dir.new.XXXXXX")"
trap 'rm -rf "$candidate"' EXIT
cp -r "$system_dir/." "$candidate/"
if ! patch -p1 -d "$candidate" < "$root/plasma/desktopcontainment.patch"; then
  echo "error: patch does not apply to this plasma-desktop version. Update plasma/desktopcontainment.patch." >&2
  exit 1
fi
find "$candidate" \( -name '*.rej' -o -name '*.orig' \) -delete

# Only the latest backup stays.
if [ -d "$local_dir" ]; then
  rm -rf "$local_dir.bak"
  mv "$local_dir" "$local_dir.bak"
  echo "Moved the previous local copy to $local_dir.bak"
fi
mv "$candidate" "$local_dir"
trap - EXIT

echo "Patched. Run: systemctl --user restart plasma-plasmashell.service"
