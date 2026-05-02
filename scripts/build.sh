#!/bin/bash
# Cross-compilation script for moxie
# Usage: ./scripts/build.sh [all|linux|mac|windows]

set -euo pipefail

BINARY="moxie"
LDFLAGS="-s -w" # strip debug info, shrink binary

build_linux() {
    echo "Building Linux amd64..."
    GOOS=linux GOARCH=amd64 go build -ldflags="$LDFLAGS" -o "dist/${BINARY}-linux-amd64" .
    echo "  → dist/${BINARY}-linux-amd64"
}

build_mac() {
    echo "Building macOS amd64..."
    GOOS=darwin GOARCH=amd64 go build -ldflags="$LDFLAGS" -o "dist/${BINARY}-darwin-amd64" .
    echo "  → dist/${BINARY}-darwin-amd64"

    echo "Building macOS arm64 (Apple Silicon)..."
    GOOS=darwin GOARCH=arm64 go build -ldflags="$LDFLAGS" -o "dist/${BINARY}-darwin-arm64" .
    echo "  → dist/${BINARY}-darwin-arm64"
}

build_windows() {
    echo "Building Windows amd64..."
    GOOS=windows GOARCH=amd64 go build -ldflags="$LDFLAGS" -o "dist/${BINARY}-windows-amd64.exe" .
    echo "  → dist/${BINARY}-windows-amd64.exe"
}

case "${1:-all}" in
    all)     mkdir -p dist; build_linux; build_mac; build_windows ;;
    linux)   mkdir -p dist; build_linux ;;
    mac)     mkdir -p dist; build_mac ;;
    windows) mkdir -p dist; build_windows ;;
    *)       echo "Usage: $0 [all|linux|mac|windows]"; exit 1 ;;
esac

echo "Done."
