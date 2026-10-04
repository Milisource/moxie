#!/usr/bin/env bash
# ──────────────────────────────────────────────────────────────────────────────
# moxie local installer — build the CLI and the Wails desktop app from the
# current checkout and install both.
#
# This is the "build it here, right now" counterpart to:
#   scripts/install.sh       — downloads a prebuilt release binary
#   scripts/install-dev.sh   — builds the CLI only, as moxie-dev (separate DB)
#
# What it does:
#   1. Builds the CLI      → dist/moxie
#   2. Builds the desktop  → dist/moxie-desktop   (requires the wails CLI)
#   3. Installs the CLI into a bin dir on PATH (default: ~/.local/bin)
#   4. Registers the desktop app via scripts/install-desktop.sh
#      (Linux .desktop entry, macOS /Applications bundle, Windows Start Menu)
#   5. Adds the bin dir to your shell config if needed
#   6. Verifies both installs
#
# Usage:
#   ./scripts/install-local.sh
#   ./scripts/install-local.sh --skip-desktop
#   ./scripts/install-local.sh --bin-dir ~/bin --no-modify-path
#   ./scripts/install-local.sh --version v0.4.0-alpha
#
# Requirements: git and a Go toolchain (Go 1.26+). The desktop build additionally
# needs the Wails CLI and, on Linux, webkit2gtk 4.1:
#   go install github.com/wailsapp/wails/v2/cmd/wails@latest
#
# Environment:
#   MOXIE_INSTALL    Bin directory for the CLI (overridden by --bin-dir)
#   MOXIE_VERSION    Version stamped into both binaries (overridden by --version)
#
# License: MIT
# ──────────────────────────────────────────────────────────────────────────────
set -euo pipefail

BINARY="moxie"
DESKTOP_BINARY="moxie-desktop"
DEFAULT_BIN_DIR="${HOME}/.local/bin"

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
muted()   { printf "${MUTED}%s${RESET}\n" "$*"; }

usage() {
    cat <<EOF
${BOLD}moxie local installer${RESET} — build and install the CLI + desktop from source

${BOLD}USAGE${RESET}
    $(basename "$0") [OPTIONS]

${BOLD}OPTIONS${RESET}
    ${BOLD}--bin-dir${RESET} <dir>      Bin directory for the CLI (default: \$HOME/.local/bin).
    ${BOLD}--version${RESET} <ver>      Version stamped into both binaries
                        (default: git describe --tags --always --dirty).
    ${BOLD}--skip-cli${RESET}           Don't build or install the CLI.
    ${BOLD}--skip-desktop${RESET}       Don't build or install the desktop app.
    ${BOLD}--no-modify-path${RESET}     Skip adding the bin dir to your shell config.
    ${BOLD}--help${RESET}               Show this help message.

${BOLD}ENVIRONMENT${RESET}
    ${BOLD}MOXIE_INSTALL${RESET}        Bin directory for the CLI (overridden by --bin-dir).
    ${BOLD}MOXIE_VERSION${RESET}        Version to stamp (overridden by --version).

${BOLD}EXAMPLES${RESET}
    ./scripts/install-local.sh
    ./scripts/install-local.sh --skip-desktop
    MOXIE_INSTALL=/usr/local/bin ./scripts/install-local.sh

${BOLD}NOTES${RESET}
    The desktop app registers at the platform-standard location (Linux
    ~/.local/bin + app menu entry; macOS /Applications; Windows Start Menu),
    independently of --bin-dir. The CLI defaults to the stable ${BOLD}moxie${RESET} name
    and ${BOLD}~/.config/moxie${RESET} data directory; the dev channel (moxie-dev) is
    handled by scripts/install-dev.sh.
EOF
    exit 0
}

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_DIR="${MOXIE_INSTALL:-$DEFAULT_BIN_DIR}"
VERSION="${MOXIE_VERSION:-}"
SKIP_CLI=0
SKIP_DESKTOP=0
NO_MODIFY_PATH=0

while [ $# -gt 0 ]; do
    case "$1" in
        --bin-dir)  shift; [ $# -eq 0 ] && die "--bin-dir requires a directory"; BIN_DIR="$1"; shift ;;
        --version)  shift; [ $# -eq 0 ] && die "--version requires a value"; VERSION="$1"; shift ;;
        --skip-cli) SKIP_CLI=1; shift ;;
        --skip-desktop) SKIP_DESKTOP=1; shift ;;
        --no-modify-path) NO_MODIFY_PATH=1; shift ;;
        --help|-h) usage ;;
        *) die "Unknown option: $1
  Use --help to see available options." ;;
    esac
done

[ "$SKIP_CLI" -eq 0 ] || [ "$SKIP_DESKTOP" -eq 0 ] || die "Nothing to do: --skip-cli and --skip-desktop were both given."

# ── Sanity checks ─────────────────────────────────────────────────────────────
[ -f "$ROOT_DIR/go.mod" ] || die "No go.mod in ${ROOT_DIR}
  Run this script from a moxie checkout: scripts/install-local.sh"

command -v go >/dev/null 2>&1 || die "Go toolchain not found.
  Install Go 1.26+ and retry. See https://go.dev/dl/"

# ── Version ───────────────────────────────────────────────────────────────────
if [ -z "$VERSION" ]; then
    VERSION="$(git -C "$ROOT_DIR" describe --tags --always --dirty 2>/dev/null \
        || git -C "$ROOT_DIR" rev-parse --short HEAD 2>/dev/null \
        || echo dev)"
fi
info "Installing moxie ${VERSION}"
muted "  source: ${ROOT_DIR}"

mkdir -p "$ROOT_DIR/dist"

# ── Build CLI ─────────────────────────────────────────────────────────────────
if [ "$SKIP_CLI" -eq 0 ]; then
    info "Building CLI (CGO_ENABLED=0)"
    ( cd "$ROOT_DIR" && CGO_ENABLED=0 go build \
        -ldflags "-s -w -X main.version=${VERSION}" \
        -o "dist/${BINARY}" . ) || die "go build failed."
    success "Built dist/${BINARY}"
fi

# ── Build desktop (Wails) ─────────────────────────────────────────────────────
if [ "$SKIP_DESKTOP" -eq 0 ]; then
    if ! command -v wails >/dev/null 2>&1; then
        die "The wails CLI was not found — the desktop build needs it.
  Install it:  go install github.com/wailsapp/wails/v2/cmd/wails@latest
  Then retry, or pass --skip-desktop to install the CLI only.
  On Linux the desktop build also needs webkit2gtk 4.1 (Arch: webkit2gtk-4.1)."
    fi

    info "Building desktop with wails"
    muted "  (first build can take a while; frontend deps are installed by wails)"
    ( cd "$ROOT_DIR/desktop" && wails build \
        -tags webkit2_41 \
        -ldflags "-X main.appVersion=${VERSION}" ) || die "wails build failed."

    [ -f "$ROOT_DIR/desktop/build/bin/moxie" ] \
        || die "wails build finished but desktop/build/bin/moxie was not produced."

    cp -f "$ROOT_DIR/desktop/build/bin/moxie" "$ROOT_DIR/dist/${DESKTOP_BINARY}"
    chmod +x "$ROOT_DIR/dist/${DESKTOP_BINARY}"
    success "Built dist/${DESKTOP_BINARY}"
fi

# ── Install CLI ───────────────────────────────────────────────────────────────
if [ "$SKIP_CLI" -eq 0 ]; then
    info "Installing CLI to ${BIN_DIR}"

    if ! mkdir -p "$BIN_DIR" 2>/dev/null; then
        warn "Cannot create ${BIN_DIR} as the current user."
        sudo mkdir -p "$BIN_DIR"
    fi

    if [ -w "$BIN_DIR" ]; then
        install -m 0755 "$ROOT_DIR/dist/${BINARY}" "${BIN_DIR}/${BINARY}"
    else
        warn "${BIN_DIR} is not writable — using sudo."
        sudo install -m 0755 "$ROOT_DIR/dist/${BINARY}" "${BIN_DIR}/${BINARY}"
    fi
    success "CLI: ${BIN_DIR}/${BINARY}"
fi

# ── Install desktop ───────────────────────────────────────────────────────────
if [ "$SKIP_DESKTOP" -eq 0 ]; then
    info "Registering desktop app"
    "$ROOT_DIR/scripts/install-desktop.sh" --binary "$ROOT_DIR/dist/${DESKTOP_BINARY}"
fi

# ── PATH ──────────────────────────────────────────────────────────────────────
add_to_path() {
    local dir="$1" shell_name config_file line

    if printf '%s' "$PATH" | tr ':' '\n' | grep -qFx "$dir"; then
        muted "${dir} is already in your PATH."
        return
    fi

    shell_name="$(basename "${SHELL:-bash}")"
    case "$shell_name" in
        fish) config_file="${HOME}/.config/fish/config.fish"; line="fish_add_path ${dir}" ;;
        zsh)  config_file="${HOME}/.zshrc";                   line="export PATH=\"${dir}:\$PATH\"" ;;
        bash) config_file="${HOME}/.bashrc";                  line="export PATH=\"${dir}:\$PATH\"" ;;
        *)    config_file="${HOME}/.profile";                 line="export PATH=\"${dir}:\$PATH\"" ;;
    esac

    mkdir -p "$(dirname "$config_file")"
    if grep -qF "# >>> moxie >>>" "$config_file" 2>/dev/null; then
        muted "PATH entry already present in ${config_file}."
        return
    fi

    {
        echo ""
        echo "# >>> moxie >>>"
        echo "$line"
        echo "# <<< moxie <<<"
        echo ""
    } >> "$config_file"

    success "Added ${dir} to PATH in ${config_file}"
    warn "Restart your terminal or run 'source ${config_file}' for it to take effect."
}

if [ "$SKIP_CLI" -eq 0 ] && [ "$NO_MODIFY_PATH" -eq 0 ]; then
    add_to_path "$BIN_DIR"
fi

# ── Verify ────────────────────────────────────────────────────────────────────
if [ "$SKIP_CLI" -eq 0 ]; then
    if ACTUAL="$("${BIN_DIR}/${BINARY}" --version 2>&1)"; then
        success "${ACTUAL}"
    else
        warn "CLI installed but failed to execute — check the binary/architecture."
    fi
fi

if [ "$SKIP_DESKTOP" -eq 0 ] && [ ! -x "$ROOT_DIR/dist/${DESKTOP_BINARY}" ]; then
    warn "Desktop binary is missing after install — check the output above."
fi

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
success "moxie ${VERSION} installed."
if [ "$SKIP_CLI" -eq 0 ]; then
    muted "  CLI:     ${BIN_DIR}/${BINARY}"
    muted "  Data:    ~/.config/moxie"
fi
if [ "$SKIP_DESKTOP" -eq 0 ]; then
    muted "  Desktop: launcher entry registered (moxie-desktop)"
fi
muted "  Rebuild after source changes:  ./scripts/install-local.sh"
echo ""
