#!/bin/bash
# Install moxie to ~/.local/bin/ from GitHub Releases
set -euo pipefail

BIN_DIR="$HOME/.local/bin"
BINARY="moxie"
GH_REPO="mili/moxie"

cleanup() { rm -f "/tmp/${BINARY}-$$" "/tmp/${BINARY}-$$.exe"; }
trap cleanup EXIT

# --- detect OS ---
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
    linux)   PLATFORM="linux" ;;
    darwin)  PLATFORM="macos" ;;
    *)
        echo "Unsupported OS: $OS"
        echo "This script supports Linux and macOS."
        exit 1
        ;;
esac

# --- detect architecture ---
ARCH=$(uname -m)
case "$ARCH" in
    x86_64|amd64)   GOARCH="amd64" ;;
    aarch64|arm64)  GOARCH="arm64" ;;
    *)
        echo "Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

# --- determine download URL ---
ASSET="${BINARY}-${PLATFORM}-${GOARCH}"
if [ -n "${MOXIE_VERSION:-}" ]; then
    DOWNLOAD_URL="https://github.com/${GH_REPO}/releases/download/${MOXIE_VERSION}/${ASSET}"
else
    DOWNLOAD_URL="https://github.com/${GH_REPO}/releases/latest/download/${ASSET}"
fi

# --- download helper ---
download() {
    local url="$1" out="$2"
    if command -v curl &>/dev/null; then
        curl -fsSL -o "$out" "$url"
    elif command -v wget &>/dev/null; then
        wget -q -O "$out" "$url"
    else
        echo "Neither curl nor wget found. Install one and try again."
        exit 1
    fi
}

# --- download and install ---
echo "Downloading ${BINARY} ${PLATFORM}/${GOARCH}..."
TMPFILE="/tmp/${BINARY}-$$"
if download "$DOWNLOAD_URL" "$TMPFILE"; then
    mkdir -p "$BIN_DIR"
    cp -f "$TMPFILE" "$BIN_DIR/$BINARY"
    chmod +x "$BIN_DIR/$BINARY"
    echo "Installed to $BIN_DIR/$BINARY"
else
    echo ""
    echo "No release found. Build from source: go build -o moxie ."
    exit 1
fi

# --- verify ---
if "$BIN_DIR/$BINARY" --version &>/dev/null; then
    "$BIN_DIR/$BINARY" --version
else
    echo "⚠  Installed but binary failed to run. It may not be compatible with your system."
    exit 1
fi

# --- PATH check ---
if ! echo "$PATH" | tr ':' '\n' | grep -qF "$BIN_DIR"; then
    echo ""
    echo "⚠  $BIN_DIR is not in your PATH."
    SHELL_NAME=$(basename "${SHELL:-$SHELL}")
    case "$SHELL_NAME" in
        fish)
            echo "   Add this to your fish config:"
            echo "   fish_add_path $BIN_DIR"
            ;;
        zsh)
            echo "   Add this to your ~/.zshrc:"
            echo "   export PATH=\"$BIN_DIR:\$PATH\""
            ;;
        *)
            echo "   Add this to your ~/.bashrc (or ~/.profile):"
            echo "   export PATH=\"$BIN_DIR:\$PATH\""
            ;;
    esac
fi

echo "Done."
