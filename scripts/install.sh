#!/bin/sh
# spinewall bootstrap. Downloads the release binary, verifies it, installs it to
# ~/.local/bin, then runs `spinewall setup`.
#
# One command:
#   curl -fsSL https://github.com/officialdad/spinewall/releases/latest/download/install.sh | sh
#
# It needs sh, curl or wget, sha256sum or shasum, and minisign. Set
# SPINEWALL_SKIP_VERIFY=1 to install without the signature check.
#
# The Spine runtimes are never in the release: their licence forbids
# redistribution. `spinewall setup` fetches them on this machine.
set -eu

REPO="officialdad/spinewall"
# Public minisign key for the release signature. The matching secret key is the
# MINISIGN_SECRET_KEY repository secret that the release workflow signs with.
MINISIGN_PUBLIC_KEY="RWQfxnTcpZHCTkn3a2pNTCqT56AOGC7/hE9ZevJc+h8Jvn3T4AozgPaT"

say() { printf '%s\n' "$*" >&2; }
die() { say "error: $*"; exit 1; }

# --- download helpers: curl or wget ---
if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1"; }
  fetch_to() { curl -fsSL -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -qO- "$1"; }
  fetch_to() { wget -qO "$2" "$1"; }
else
  die "curl or wget is required."
fi

# --- checksum helper: sha256sum or shasum ---
if command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | awk '{print $1}'; }
else
  die "sha256sum or shasum is required."
fi

# --- arch ---
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) die "unsupported architecture: $(uname -m). Build from source with: go install github.com/$REPO/cmd/spinewall@latest" ;;
esac

# --- latest release tag, and the base URL for that release ---
tag="${SPINEWALL_VERSION:-}"
if [ -z "$tag" ]; then
  latest_url="https://api.github.com/repos/$REPO/releases/latest"
  tag="$(fetch "$latest_url" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)"
fi
[ -n "$tag" ] || die "could not find the latest release. Set SPINEWALL_VERSION=vX.Y.Z to pick one."
version="${tag#v}"
base="https://github.com/$REPO/releases/download/$tag"

binary="spinewall-$version-linux-$arch"
url="$base/$binary"
sums_url="$base/SHA256SUMS"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT INT TERM

say "spinewall $version ($arch)"
say "downloading $binary"
fetch_to "$url" "$work/$binary" || die "download failed: $url"
fetch_to "$sums_url" "$work/SHA256SUMS" || die "download failed: $sums_url"

# --- checksum ---
want="$(awk -v f="$binary" '$2 == f {print $1}' "$work/SHA256SUMS")"
[ -n "$want" ] || die "$binary is not listed in SHA256SUMS. The release is incomplete."
got="$(sha256 "$work/$binary")"
[ "$want" = "$got" ] || die "checksum mismatch for $binary: expected $want, got $got"
say "checksum ok"

# --- signature ---
# SHA256SUMS comes from the same place as the binary, so only the minisign
# signature proves the release is authentic. A missing tool, a missing
# signature, or a bad signature stops the install.
sig_url="$base/SHA256SUMS.minisig"
if [ "${SPINEWALL_SKIP_VERIFY:-}" = "1" ]; then
  say "WARNING: SPINEWALL_SKIP_VERIFY=1 is set. The release signature was NOT verified."
  say "WARNING: nothing proves this binary came from the spinewall release."
else
  command -v minisign >/dev/null 2>&1 \
    || die "minisign is required to verify the release signature. Install the minisign package, then run this again.
       To verify by hand, download SHA256SUMS and SHA256SUMS.minisig from $base and run:
         minisign -Vm SHA256SUMS -P $MINISIGN_PUBLIC_KEY
       To skip the check at your own risk, set SPINEWALL_SKIP_VERIFY=1."
  fetch_to "$sig_url" "$work/SHA256SUMS.minisig" \
    || die "download failed: $sig_url. The release has no signature, so it is not installed."
  minisign -Vm "$work/SHA256SUMS" -P "$MINISIGN_PUBLIC_KEY" >/dev/null \
    || die "signature check failed for SHA256SUMS. The download is not installed."
  say "signature ok"
fi

# --- install ---
bin_dir="$HOME/.local/bin"
mkdir -p "$bin_dir"
cp "$work/$binary" "$bin_dir/spinewall"
chmod 755 "$bin_dir/spinewall"
say "installed $bin_dir/spinewall"

# --- put ~/.local/bin on PATH for this run, so `spinewall setup` resolves ---
PATH="$bin_dir:$PATH"
export PATH

# --- run the wizard ---
# Not exec: the exit trap must still clear the download directory. The setup
# exit code is the bootstrap's exit code. Under `curl | sh` stdin is the
# download pipe, so the wizard reads the terminal instead when one exists.
# With no terminal at all, it reads nothing, so it never eats the rest of a
# piped script.
status=0
if [ -t 0 ]; then
  spinewall setup "$@" || status=$?
elif (: </dev/tty) 2>/dev/null; then
  spinewall setup "$@" </dev/tty || status=$?
else
  spinewall setup "$@" </dev/null || status=$?
fi
trap - EXIT INT TERM
rm -rf "$work"
exit "$status"
