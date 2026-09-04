#!/bin/bash
# Cross-compilation script for moxie
# Usage: ./scripts/build.sh [all|linux|mac|windows]
#
# Produces static binaries named:
#   moxie-linux-amd64     moxie-linux-arm64
#   moxie-macos-amd64     moxie-macos-arm64
#   moxie-windows-amd64.exe  moxie-windows-arm64.exe

set -euo pipefail

BINARY="moxie"
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"
LDFLAGS="-s -w -X main.version=${VERSION}"

build_linux() {
    echo "Building Linux amd64..."
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="$LDFLAGS" -o "dist/${BINARY}-linux-amd64" .
    echo "  -> dist/${BINARY}-linux-amd64"

    echo "Building Linux arm64..."
    CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="$LDFLAGS" -o "dist/${BINARY}-linux-arm64" .
    echo "  -> dist/${BINARY}-linux-arm64"
}

build_mac() {
    echo "Building macOS amd64..."
    CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="$LDFLAGS" -o "dist/${BINARY}-macos-amd64" .
    echo "  -> dist/${BINARY}-macos-amd64"

    echo "Building macOS arm64 (Apple Silicon)..."
    CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="$LDFLAGS" -o "dist/${BINARY}-macos-arm64" .
    echo "  -> dist/${BINARY}-macos-arm64"
}

build_windows() {
    echo "Building Windows amd64..."
    CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="$LDFLAGS" -o "dist/${BINARY}-windows-amd64.exe" .
    echo "  -> dist/${BINARY}-windows-amd64.exe"

    echo "Building Windows arm64..."
    CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -ldflags="$LDFLAGS" -o "dist/${BINARY}-windows-arm64.exe" .
    echo "  -> dist/${BINARY}-windows-arm64.exe"
}

case "${1:-all}" in
    all)     mkdir -p dist; build_linux; build_mac; build_windows ;;
    linux)   mkdir -p dist; build_linux ;;
    mac)     mkdir -p dist; build_mac ;;
    windows) mkdir -p dist; build_windows ;;
    *)       echo "Usage: $0 [all|linux|mac|windows]"; exit 1 ;;
esac

echo "Done."
