#!/usr/bin/env bash
# Install llm-filesystem from a GitHub release. No sudo, no Go toolchain.
#
#   curl -fsSL https://raw.githubusercontent.com/samestrin/llm-filesystem-axi/main/install.sh | bash
#
# Downloads the binary for this platform and SHA256SUMS.txt, verifies the
# binary against it, runs it with --version, and installs to $HOME/.local/bin.
# On a failed download, a checksum mismatch, or a binary that does not run
# here it installs nothing and exits non-zero; an existing binary is only ever
# replaced by one that verified and ran. Re-running it is the upgrade.
#
# Environment:
#   INSTALL_DIR              where to install (default: $HOME/.local/bin)
#   LLM_FILESYSTEM_VERSION   release tag, e.g. v1.0.0 (default: latest)
#   LLM_FILESYSTEM_BASE_URL  URL the assets are fetched from; overrides the
#                            version (used by the tests)
#   LLM_FILESYSTEM_DOWNLOAD_TIMEOUT
#                            seconds before a download is abandoned (default: 300)
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
timeout="${LLM_FILESYSTEM_DOWNLOAD_TIMEOUT:-300}"
[[ "$timeout" =~ ^[1-9][0-9]*$ ]] || die "LLM_FILESYSTEM_DOWNLOAD_TIMEOUT must be a whole number of seconds, got '$timeout'"

if command -v curl >/dev/null 2>&1; then
    # A stalled connection must fail, not hang the install.
    fetch() { curl -fsSL --connect-timeout 15 --max-time "$timeout" -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
    # wget has no overall limit; --timeout bounds each connect and read stall.
    fetch() { wget -q --timeout="$timeout" --tries=1 -O "$2" "$1"; }
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

# A binary can match its checksum and still not run here: the wrong
# architecture, or a noexec install directory. Run the staged copy, bounded so
# one that hangs cannot hang the install. macOS has no timeout(1), so a
# watcher kills it instead. Output goes to a file: a command substitution
# would wait on the watcher's sleep, which holds the pipe open.
version_limit=10
run_version() { # $1: binary. Prints its first --version line; fails if it does not run.
    local pid watcher rc=0
    rm -f "$tmp/timed_out"
    "$1" --version >"$tmp/version.out" 2>&1 </dev/null &
    pid=$!
    (sleep "$version_limit" && touch "$tmp/timed_out" && kill -9 "$pid") >/dev/null 2>&1 &
    watcher=$!
    wait "$pid" 2>/dev/null || rc=$?
    kill "$watcher" 2>/dev/null || true
    wait "$watcher" 2>/dev/null || true
    if [[ -e "$tmp/timed_out" ]]; then
        echo "did not finish within $version_limit seconds"
        return 1
    fi
    if [[ "$rc" -ne 0 ]]; then
        echo "exited $rc: $(head -n 1 "$tmp/version.out")"
        return 1
    fi
    if [[ -s "$tmp/version.out" ]]; then head -n 1 "$tmp/version.out"; else echo "(no version output)"; fi
}
new_version="$(run_version "$staged")" ||
    die "the downloaded $asset does not run on this machine: '$BIN --version' $new_version"

old_version=""
if [[ -e "$install_dir/$BIN" ]]; then
    old_version="$(run_version "$install_dir/$BIN")" || old_version="an unknown version (its --version $old_version)"
fi

mv -f "$staged" "$install_dir/$BIN" || die "could not install to $install_dir/$BIN"

echo "Installed $install_dir/$BIN: $new_version"
[[ -z "$old_version" ]] || echo "Replaced: $old_version"
case ":$PATH:" in
    *":$install_dir:"*) ;;
    *) echo "Note: $install_dir is not on your PATH. Add it, e.g.: export PATH=\"$install_dir:\$PATH\"" ;;
esac

# The install only helps if it is what runs. An older copy earlier on PATH
# still wins, and a pre-AXI one rejects --confirm. -ef compares the files, so
# a symlink to the install does not count as shadowing it.
found="$(command -v "$BIN" 2>/dev/null || true)"
if [[ -n "$found" && ! "$found" -ef "$install_dir/$BIN" ]]; then
    echo "Warning: $found shadows $install_dir/$BIN, so '$BIN' runs that one instead. Remove it, or put $install_dir earlier on your PATH." >&2
fi
# The MCP server was removed from this project; an old install of it still
# runs. This script did not install it, so it names it rather than delete it.
found="$(command -v "$BIN-mcp" 2>/dev/null || true)"
if [[ -n "$found" ]]; then
    echo "Warning: $found is a leftover $BIN-mcp, which is no longer part of $BIN and is not updated. Remove it: rm $found" >&2
fi
