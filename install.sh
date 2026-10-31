#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

INSTALL_DIR="/usr/local/bin"
BUILD_DIR="build"

# Binaries to build and install.
BINARIES=(
    "llm-filesystem"
)

VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
CLI_LDFLAGS="-s -w -X github.com/samestrin/llm-filesystem-axi/internal/filesystem/commands.Version=$VERSION"

echo -e "${YELLOW}llm-filesystem installer${NC}"
echo "========================"

# Check if running as root (needed for /usr/local/bin)
if [[ $EUID -ne 0 ]]; then
    echo -e "${RED}Error: This script must be run with sudo${NC}"
    echo "Usage: sudo ./install.sh"
    exit 1
fi

# Get the directory where the script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Check for go
if ! command -v go &> /dev/null; then
    echo -e "${RED}Error: go is not installed${NC}"
    exit 1
fi

echo -e "${GREEN}Building binaries...${NC}"
mkdir -p "$BUILD_DIR"

echo "  Building llm-filesystem..."
go build -ldflags "$CLI_LDFLAGS" -o "$BUILD_DIR/llm-filesystem" ./cmd/llm-filesystem

echo -e "${GREEN}Installing to $INSTALL_DIR...${NC}"
for bin in "${BINARIES[@]}"; do
    echo "  Installing $bin..."
    # Clear xattrs on source before copying
    xattr -cr "$BUILD_DIR/$bin" 2>/dev/null || true
    # Remove old binary first
    rm -f "$INSTALL_DIR/$bin"
    # Copy binary
    cp "$BUILD_DIR/$bin" "$INSTALL_DIR/$bin"
    # Clear all extended attributes recursively
    xattr -cr "$INSTALL_DIR/$bin" 2>/dev/null || true
    # Re-sign with ad-hoc signature (required for Apple Silicon)
    codesign --force --sign - "$INSTALL_DIR/$bin" 2>/dev/null || true
    chmod 755 "$INSTALL_DIR/$bin"
done

echo ""
echo -e "${GREEN}Installation complete!${NC}"
echo "Installed ${#BINARIES[@]} binaries to $INSTALL_DIR"

# Verify installation
echo ""
echo "Verifying installation..."
FAILED=0
for bin in "${BINARIES[@]}"; do
    if [[ -x "$INSTALL_DIR/$bin" ]]; then
        # Try --version first, fall back to --help
        if timeout 2 "$INSTALL_DIR/$bin" --version >/dev/null 2>&1 || \
           timeout 2 "$INSTALL_DIR/$bin" --help >/dev/null 2>&1; then
            echo -e "  ${GREEN}✓${NC} $bin"
        else
            echo -e "  ${RED}✗${NC} $bin"
            FAILED=1
        fi
    else
        echo -e "  ${RED}✗${NC} $bin (not found)"
        FAILED=1
    fi
done

if [[ $FAILED -eq 1 ]]; then
    echo -e "${RED}Some binaries failed to install${NC}"
    exit 1
fi

echo ""
echo -e "${GREEN}All binaries installed successfully!${NC}"
