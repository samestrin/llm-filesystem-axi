#!/usr/bin/env bash
# Install llm-filesystem from a GitHub release. No sudo, no Go toolchain.
#
#   curl -fsSL https://raw.githubusercontent.com/samestrin/llm-filesystem-axi/main/install.sh | bash
#
# Downloads the binary for this platform and SHA256SUMS.txt, verifies the
# binary against it, and installs to $HOME/.local/bin. On a failed download or
# a checksum mismatch it installs nothing and exits non-zero; an existing
# binary is only ever replaced by one that verified.
#
# Environment:
#   INSTALL_DIR              where to install (default: $HOME/.local/bin)
#   LLM_FILESYSTEM_VERSION   release tag, e.g. v1.0.0 (default: latest)
#   LLM_FILESYSTEM_BASE_URL  URL the assets are fetched from; overrides the
#                            version (used by the tests)
set -euo pipefail

REPO="samestrin/llm-filesystem-axi"
BIN="llm-filesystem"
SUMS="SHA256SUMS.txt"

die() {
    echo "Error: $*" >&2
    echo "Nothing was installed." >&2
    exit 1
}

case "$(uname -s)" in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    *) die "unsupported OS $(uname -s); download a binary from https://github.com/$REPO/releases" ;;
esac
case "$(uname -m)" in
    x86_64 | amd64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *) die "unsupported architecture $(uname -m); download a binary from https://github.com/$REPO/releases" ;;
esac
asset="$BIN-$os-$arch"

version="${LLM_FILESYSTEM_VERSION:-latest}"
if [[ -n "${LLM_FILESYSTEM_BASE_URL:-}" ]]; then
    base="${LLM_FILESYSTEM_BASE_URL%/}"
elif [[ "$version" == "latest" ]]; then
    base="https://github.com/$REPO/releases/latest/download"
else
    base="https://github.com/$REPO/releases/download/$version"
fi
install_dir="${INSTALL_DIR:-$HOME/.local/bin}"

if command -v curl >/dev/null 2>&1; then
    fetch() { curl -fsSL -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
    fetch() { wget -q -O "$2" "$1"; }
else
    die "neither curl nor wget is installed"
fi

if command -v sha256sum >/dev/null 2>&1; then
    sha256_check() { sha256sum -c --status -; }
elif command -v shasum >/dev/null 2>&1; then
    sha256_check() { shasum -a 256 -c --status -; }
else
    die "neither sha256sum nor shasum is installed, so the download cannot be verified"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $asset ($version) from $base"
fetch "$base/$asset" "$tmp/$asset" || die "could not download $base/$asset"
fetch "$base/$SUMS" "$tmp/$SUMS" || die "could not download $base/$SUMS"

# Exactly this asset's line. sha256sum writes "hash  name", or "hash *name" in
# binary mode.
line="$(awk -v n="$asset" '$2 == n || $2 == "*" n' "$tmp/$SUMS")"
[[ -n "$line" ]] || die "$SUMS has no checksum for $asset"
[[ "$(printf '%s\n' "$line" | wc -l)" -eq 1 ]] || die "$SUMS has more than one checksum for $asset"
(cd "$tmp" && printf '%s\n' "$line" | sha256_check) ||
    die "checksum mismatch for $asset: the download does not match $SUMS"
echo "Checksum verified."

# Stage beside the destination, then rename, so the install is all or nothing.
mkdir -p "$install_dir" || die "could not create $install_dir"
staged="$install_dir/.$BIN.tmp.$$"
trap 'rm -rf "$tmp"; rm -f "$staged"' EXIT
cp "$tmp/$asset" "$staged" || die "could not write to $install_dir"
chmod 755 "$staged"
mv -f "$staged" "$install_dir/$BIN" || die "could not install to $install_dir/$BIN"

echo "Installed $install_dir/$BIN"
case ":$PATH:" in
    *":$install_dir:"*) ;;
    *) echo "Note: $install_dir is not on your PATH. Add it, e.g.: export PATH=\"$install_dir:\$PATH\"" ;;
esac
