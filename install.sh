#!/usr/bin/env bash
# Install llm-filesystem and its MCP server llm-filesystem-mcp from a GitHub
# release. No sudo, no Go toolchain.
#
#   curl -fsSL https://raw.githubusercontent.com/samestrin/llm-filesystem-axi/main/install.sh | bash
#
# Downloads the binaries for this platform and SHA256SUMS.txt, verifies each
# binary against it, runs each with --version, and installs to $HOME/.local/bin.
# On a failed download, a checksum mismatch, or a binary that does not run
# here it installs nothing and exits non-zero; an existing binary is only ever
# replaced by one that verified and ran. Re-running it is the upgrade.
#
# Releases that predate the MCP server have no llm-filesystem-mcp asset: the
# installer says the server was not installed, installs the verified CLI
# alone, and exits 0. A server asset that is present but fails its checks is
# fatal and installs neither binary.
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
server_asset="$BIN-mcp-$os-$arch"

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
    # Like fetch, but reports the HTTP status instead of failing on it, so a
    # release without the server asset (404) can be told from a download that
    # failed. Without -f an error page lands in "$2"; it is only used on 200.
    fetch_status() { curl -sSL --connect-timeout 15 --max-time "$timeout" -o "$2" -w '%{http_code}' "$1"; }
elif command -v wget >/dev/null 2>&1; then
    # wget has no overall limit; --timeout bounds each connect and read stall.
    fetch() { wget -q --timeout="$timeout" --tries=1 -O "$2" "$1"; }
    fetch_status() {
        local log rc=0 code
        log="$(wget -q --server-response --timeout="$timeout" --tries=1 -O "$2" "$1" 2>&1 >/dev/null)" || rc=$?
        # The last response line wins: redirects answer more than once.
        code="$(printf '%s\n' "$log" | awk '$1 ~ /^HTTP\// { c = $2 } END { print c }')"
        [[ -n "$code" ]] || return "$rc"
        printf '%s' "$code"
    }
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
verify_checksum() { # $1: asset name. Dies unless the sums hold exactly one matching line.
    local line
    line="$(awk -v n="$1" '$2 == n || $2 == "*" n' "$tmp/$SUMS")"
    [[ -n "$line" ]] || die "$SUMS has no checksum for $1"
    [[ "$(printf '%s\n' "$line" | wc -l)" -eq 1 ]] || die "$SUMS has more than one checksum for $1"
    (cd "$tmp" && printf '%s\n' "$line" | sha256_check) ||
        die "checksum mismatch for $1: the download does not match $SUMS"
}
verify_checksum "$asset"
echo "Checksum verified."

# The MCP server ships beside the CLI. A release without its asset (404)
# predates the server: a visible skip, with the verified CLI still installed.
# Any other download failure installs nothing, like every other failure here.
have_server=1
server_status="$(fetch_status "$base/$server_asset" "$tmp/$server_asset")" ||
    die "could not download $base/$server_asset"
if [[ "$server_status" == 404 ]]; then
    have_server=0
    echo "Note: $BIN-mcp was not installed: this release has no $server_asset (it predates the MCP server)."
elif [[ "$server_status" != 200 ]]; then
    die "could not download $base/$server_asset (HTTP $server_status)"
fi
if [[ "$have_server" -eq 1 ]]; then
    verify_checksum "$server_asset"
    echo "Checksum verified ($BIN-mcp)."
fi

# Stage beside the destination, then rename, so the install is all or nothing:
# both binaries are verified and run before either is renamed.
mkdir -p "$install_dir" || die "could not create $install_dir"
staged="$install_dir/.$BIN.tmp.$$"
staged_mcp="$install_dir/.$BIN-mcp.tmp.$$"
trap 'rm -rf "$tmp"; rm -f "$staged" "$staged_mcp"' EXIT
cp "$tmp/$asset" "$staged" || die "could not write to $install_dir"
chmod 755 "$staged"
if [[ "$have_server" -eq 1 ]]; then
    cp "$tmp/$server_asset" "$staged_mcp" || die "could not write to $install_dir"
    chmod 755 "$staged_mcp"
fi

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
new_mcp_version=""
if [[ "$have_server" -eq 1 ]]; then
    new_mcp_version="$(run_version "$staged_mcp")" ||
        die "the downloaded $server_asset does not run on this machine: '$BIN-mcp --version' $new_mcp_version"
fi

old_version=""
if [[ -e "$install_dir/$BIN" ]]; then
    old_version="$(run_version "$install_dir/$BIN")" || old_version="an unknown version (its --version $old_version)"
fi
old_mcp_version=""
if [[ "$have_server" -eq 1 && -e "$install_dir/$BIN-mcp" ]]; then
    old_mcp_version="$(run_version "$install_dir/$BIN-mcp")" || old_mcp_version="an unknown version (its --version $old_mcp_version)"
fi

mv -f "$staged" "$install_dir/$BIN" || die "could not install to $install_dir/$BIN"
if [[ "$have_server" -eq 1 ]]; then
    mv -f "$staged_mcp" "$install_dir/$BIN-mcp" || die "could not install to $install_dir/$BIN-mcp"
fi

echo "Installed $install_dir/$BIN: $new_version"
[[ -z "$old_version" ]] || echo "Replaced: $old_version"
if [[ "$have_server" -eq 1 ]]; then
    echo "Installed $install_dir/$BIN-mcp: $new_mcp_version"
    [[ -z "$old_mcp_version" ]] || echo "Replaced: $old_mcp_version"
fi
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
# The MCP server ships beside the CLI now, and an older copy earlier on PATH
# still wins. -ef compares the files, so a symlink to the install does not
# count as shadowing it. When the server was skipped there is no new one to
# shadow, and an old copy is left alone rather than named.
if [[ "$have_server" -eq 1 ]]; then
    found="$(command -v "$BIN-mcp" 2>/dev/null || true)"
    if [[ -n "$found" && ! "$found" -ef "$install_dir/$BIN-mcp" ]]; then
        echo "Warning: $found shadows $install_dir/$BIN-mcp, so '$BIN-mcp' runs that one instead. Remove it, or put $install_dir earlier on your PATH." >&2
    fi
fi
