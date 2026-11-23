#!/bin/sh
# spinewall bootstrap. Downloads the release binary, verifies it, installs it to
# ~/.local/bin, then runs `spinewall setup`.
#
# One command:
#   curl -fsSL https://github.com/officialdad/spinewall/releases/latest/download/install.sh | sh
#
# It needs only sh, curl or wget, and sha256sum or shasum. minisign is used when
# it is installed for a real signature check; otherwise the SHA256SUMS check
# still catches a corrupt download.
#
# The Spine runtimes are never in the release: their licence forbids
# redistribution. `spinewall setup` fetches them on this machine.
set -eu

REPO="officialdad/spinewall"
# Public minisign key for the release signature. PLACEHOLDER: the captain must
# replace this with the real key and add the secret key to the release workflow
# before the first signed release. While it is the placeholder, the signature
# step prints a clear message instead of failing.
MINISIGN_PUBLIC_KEY="RWSPINEWALLPLACEHOLDERKEYREPLACETHISWITHTHEREALKEY00000000000000000="

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

# --- signature (minisign when present) ---
sig_url="$base/SHA256SUMS.minisig"
if fetch_to "$sig_url" "$work/SHA256SUMS.minisig" 2>/dev/null; then
  if command -v minisign >/dev/null 2>&1; then
    if [ "${MINISIGN_PUBLIC_KEY#RWSPINEWALLPLACEHOLDER}" != "$MINISIGN_PUBLIC_KEY" ]; then
      say "note: this build has a placeholder signature key, so the signature is not verified."
      say "      the SHA256SUMS check above was still run. Check the release page for the key."
    else
      printf '%s\n' "$MINISIGN_PUBLIC_KEY" > "$work/minisign.pub"
      minisign -Vm "$work/SHA256SUMS" -p "$work/minisign.pub" \
        || die "signature check failed for SHA256SUMS"
      say "signature ok"
    fi
  else
    say "note: minisign is not installed, so only the SHA256SUMS check was run."
    say "      install minisign to verify the release signature."
  fi
else
  say "note: no signature file in this release; only the SHA256SUMS check was run."
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
# exit code is the bootstrap's exit code.
spinewall setup "$@"
status=$?
trap - EXIT INT TERM
rm -rf "$work"
exit "$status"
