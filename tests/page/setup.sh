#!/usr/bin/env bash
# Installs spinewall into a throwaway HOME so the tests use the real runtimes.
# scripts/install.sh fetches them over the network. systemctl is stubbed, because CI has no user session.
# usage: tests/page/setup.sh [--force]
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
work="$here/.work"
home="$work/home"
marker="$home/.local/share/webwallpaper/themes/spinewall/spine-player38.js"

if [ "${1:-}" = "--force" ]; then rm -rf "$work"; fi
if [ -f "$marker" ]; then
  echo "setup: runtimes already installed in $home"
  exit 0
fi

mkdir -p "$work/bin" "$home"
cat > "$work/bin/systemctl" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
chmod +x "$work/bin/systemctl"

HOME="$home" PATH="$work/bin:$PATH" bash "$repo/scripts/install.sh"

for global in 38 40 41 42; do
  test -s "$home/.local/share/webwallpaper/themes/spinewall/spine-player$global.js" \
    || { echo "error: spine-player$global.js is missing after install." >&2; exit 1; }
done
echo "setup: installed into $home"
