#!/usr/bin/env bash
# ──────────────────────────────────────────────────────────────────────────────
# moxie — Desktop App Installer
# Installs the Wails desktop binary and registers it as a system application
# so it appears in start menus and can be launched like any other app.
#
# Usage:
#   make desktop && ./scripts/install-desktop.sh
#   ./scripts/install-desktop.sh --binary ./dist/moxie-desktop
#
# Platform targets:
#   Linux   → ~/.local/bin/moxie-desktop + ~/.local/share/applications/moxie.desktop
#   macOS   → /Applications/moxie.app (TODO: .app bundle generation)
#   Windows → %APPDATA%/moxie/bin/moxie-desktop.exe (TODO: Start Menu shortcut)
#
# License: MIT
# ──────────────────────────────────────────────────────────────────────────────
set -euo pipefail

BINARY_NAME="moxie-desktop"
HUMAN_NAME="Moxie"
DESCRIPTION="Game Library Manager for Desktop"

# ── Colors ────────────────────────────────────────────────────────────────────
if [ -t 1 ]; then
    RED="\033[31m"; GREEN="\033[32m"; ORANGE="\033[38;5;214m"
    MUTED="\033[90m"; BOLD="\033[1m"; RESET="\033[0m"
else
    RED=""; GREEN=""; ORANGE=""; MUTED=""; BOLD=""; RESET=""
fi
info()    { printf "${GREEN}${BOLD}==>${RESET}${BOLD} %s${RESET}\n" "$*"; }
success() { printf "${GREEN}${BOLD}==>${RESET} ${GREEN}%s${RESET}\n" "$*"; }
warn()    { printf "${ORANGE}${BOLD}==>${RESET}${BOLD} %s${RESET}\n" "$*" 1>&2; }
die()     { printf "${RED}${BOLD}==>${RESET}${BOLD} %s${RESET}\n" "$*" 1>&2; exit 1; }

# ── Detect platform ───────────────────────────────────────────────────────────
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
    linux)  PLATFORM="linux" ;;
    darwin) PLATFORM="macos" ;;
    mingw*|msys*|cygwin*) PLATFORM="windows" ;;
    *)      die "Unsupported OS: $OS" ;;
esac

# ── Parse arguments ───────────────────────────────────────────────────────────
LOCAL_BINARY=""
while [ $# -gt 0 ]; do
    case "$1" in
        --binary) shift; LOCAL_BINARY="$1"; shift ;;
        --help|-h)
            echo "Usage: $0 [--binary <path>]"
            echo "  --binary <path>    Path to pre-built moxie-desktop binary"
            echo "                     (default: ./dist/moxie-desktop)"
            exit 0 ;;
        *) die "Unknown option: $1" ;;
    esac
done

# ── Locate the binary ─────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BINARY_PATH="${LOCAL_BINARY:-${SCRIPT_DIR}/dist/moxie-desktop}"

if [ ! -f "$BINARY_PATH" ]; then
    die "Binary not found at ${BINARY_PATH}
  Build it first:  make desktop
  Or specify:      $0 --binary /path/to/moxie-desktop"
fi
if [ ! -x "$BINARY_PATH" ]; then
    warn "Making binary executable..."
    chmod +x "$BINARY_PATH"
fi

# ── Version ────────────────────────────────────────────────────────────────────
VERSION="$("${BINARY_PATH}" --version 2>/dev/null || echo "dev")"

# ═══════════════════════════════════════════════════════════════════════════════
# Linux Install
# ═══════════════════════════════════════════════════════════════════════════════
install_linux() {
    local bin_dir="${HOME}/.local/bin"
    local apps_dir="${HOME}/.local/share/applications"
    local icons_dir="${HOME}/.local/share/icons/hicolor/256x256/apps"
    local bin_path="${bin_dir}/${BINARY_NAME}"
    local desktop_path="${apps_dir}/moxie.desktop"

    info "Installing to ~/.local (Linux)"

    # ── Binary ─────────────────────────────────────────────────────────────
    mkdir -p "$bin_dir"
    cp -f "$BINARY_PATH" "$bin_path"
    chmod +x "$bin_path"
    success "Binary: ${bin_path}"

    # ── Try to extract embedded icon ───────────────────────────────────────
    # Wails embeds the app icon (desktop/build/appicon.png) during build.
    # If available, copy it as the desktop icon.
    local icon_src="${SCRIPT_DIR}/desktop/build/appicon.png"
    local icon_dst=""
    if [ -f "$icon_src" ]; then
        mkdir -p "$icons_dir"
        icon_dst="${icons_dir}/moxie.png"
        cp -f "$icon_src" "$icon_dst"
        success "Icon: ${icon_dst}"
    fi

    # ── .desktop file ──────────────────────────────────────────────────────
    mkdir -p "$apps_dir"
    cat > "$desktop_path" <<DESKTOP
[Desktop Entry]
Type=Application
Name=${HUMAN_NAME}
Comment=${DESCRIPTION}
Exec=${bin_path}
Icon=${icon_dst:-moxie}
Terminal=false
Categories=Game;Utility;
Keywords=game;library;manager;f95zone;
StartupNotify=true
DESKTOP
    chmod +x "$desktop_path"
    success "Desktop entry: ${desktop_path}"

    # ── Update desktop database ────────────────────────────────────────────
    if command -v update-desktop-database &>/dev/null; then
        update-desktop-database "$apps_dir" &>/dev/null || true
    fi

    # ── Check PATH ────────────────────────────────────────────────────────
    if ! echo "$PATH" | tr ':' '\n' | grep -qFx "$bin_dir"; then
        warn "${bin_dir} is not in your PATH."
        warn "Add it:  export PATH=\"${bin_dir}:\$PATH\""
    fi

    success "Moxie Desktop installed! Launcher should appear in your app menu."
    warn "You may need to log out and back in for the desktop entry to appear."
}

# ═══════════════════════════════════════════════════════════════════════════════
# macOS Install
# ═══════════════════════════════════════════════════════════════════════════════
install_macos() {
    local app_dir="/Applications"
    local app_path="${app_dir}/${HUMAN_NAME}.app"
    local contents_dir="${app_path}/Contents"
    local macos_dir="${contents_dir}/MacOS"
    local resources_dir="${contents_dir}/Resources"

    info "Installing to /Applications (macOS)"

    if [ ! -d "$app_dir" ]; then
        die "/Applications not found. This script expects standard macOS."
    fi

    # ── Create .app bundle structure ───────────────────────────────────────
    mkdir -p "$macos_dir" "$resources_dir"

    # Copy binary into the bundle
    cp -f "$BINARY_PATH" "${macos_dir}/${HUMAN_NAME}"
    chmod +x "${macos_dir}/${HUMAN_NAME}"

    # ── Info.plist ─────────────────────────────────────────────────────────
    cat > "${contents_dir}/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key>
    <string>${HUMAN_NAME}</string>
    <key>CFBundleIdentifier</key>
    <string>io.github.milisource.moxie</string>
    <key>CFBundleName</key>
    <string>${HUMAN_NAME}</string>
    <key>CFBundleDisplayName</key>
    <string>${HUMAN_NAME}</string>
    <key>CFBundleVersion</key>
    <string>${VERSION}</string>
    <key>CFBundleShortVersionString</key>
    <string>${VERSION}</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>LSMinimumSystemVersion</key>
    <string>11.0</string>
    <key>NSHighResolutionCapable</key>
    <true/>
</dict>
</plist>
PLIST

    # ── Icon ───────────────────────────────────────────────────────────────
    local icon_src="${SCRIPT_DIR}/desktop/build/appicon.png"
    if [ -f "$icon_src" ]; then
        cp -f "$icon_src" "${resources_dir}/appicon.png"
    fi

    success "App bundle: ${app_path}"
    warn "You may need to run: xattr -dr com.apple.quarantine '${app_path}'"
}

# ═══════════════════════════════════════════════════════════════════════════════
# Windows Install
# ═══════════════════════════════════════════════════════════════════════════════
install_windows() {
    local appdata="${APPDATA:-$HOME/AppData/Roaming}"
    local bin_dir="${appdata}/moxie/bin"
    local bin_path="${bin_dir}/${BINARY_NAME}.exe"

    info "Installing to %APPDATA% (Windows)"
    mkdir -p "$bin_dir"
    cp -f "$BINARY_PATH" "$bin_path"
    chmod +x "$bin_path"
    success "Binary: ${bin_path}"

    # Create Start Menu shortcut via PowerShell.
    # This creates a .lnk in the Start Menu so the app appears in search/start.
    if command -v powershell.exe &>/dev/null; then
        powershell.exe -Command "
            \$WshShell = New-Object -ComObject WScript.Shell
            \$Shortcut = \$WshShell.CreateShortcut(\"$env:APPDATA\Microsoft\Windows\Start Menu\Programs\Moxie.lnk\")
            \$Shortcut.TargetPath = \"$bin_path\"
            \$Shortcut.Save()
        " && success "Start Menu shortcut created" || warn "Failed to create Start Menu shortcut (non-fatal)"
    else
        warn "PowerShell not found. Start Menu shortcut not created."
        warn "Add ${bin_dir} to your PATH to launch from terminal."
    fi
}

# ═══════════════════════════════════════════════════════════════════════════════
# Main
# ═══════════════════════════════════════════════════════════════════════════════
case "$PLATFORM" in
    linux)   install_linux ;;
    macos)   install_macos ;;
    windows) install_windows ;;
esac

echo ""
success "Installation complete! ${HUMAN_NAME} ${VERSION}"
