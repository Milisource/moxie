#!/bin/bash
# Install moxie to ~/.local/bin/
set -euo pipefail

BIN_DIR="$HOME/.local/bin"
BINARY="moxie"

echo "Building $BINARY..."
cd "$(dirname "$0")/.."
go build -ldflags="-s -w" -o "$BINARY" .

mkdir -p "$BIN_DIR"
cp "$BINARY" "$BIN_DIR/"
echo "Installed to $BIN_DIR/$BINARY"

# Check if ~/.local/bin is in PATH
if ! echo "$PATH" | tr ':' '\n' | grep -qF "$BIN_DIR"; then
    echo ""
    echo "⚠  $BIN_DIR is not in your PATH."
    echo "   Add this to your fish config:"
    echo "   fish_add_path $BIN_DIR"
fi

echo "Done."
