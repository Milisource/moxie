#!/usr/bin/env bash
# ──────────────────────────────────────────────────────────────────────────────
# moxie dev-channel installer — builds the dev build from source.
#
# This is the DEV counterpart to scripts/install.sh (the stable/main channel).
# The two are deliberately distinct and can coexist:
#
#                 binary        data directory
#   stable  →     moxie         ~/.config/moxie        (install.sh)
#   dev     →     moxie-dev     ~/.config/moxie-dev    (this script)
#
# Because the dev build has its own database, a schema migration in a dev
# build can never strand the stable install.
#
# Usage:
#   ./scripts/install-dev.sh                     # build this checkout
#   ./scripts/install-dev.sh --source ~/src/moxie
#   ./scripts/install-dev.sh --clone             # shallow-clone the dev branch
#   ./scripts/install-dev.sh --clone --ref main  # clone a different ref
#   ./scripts/install-dev.sh --install ~/bin --no-modify-path
#
# Requirements: git and a Go toolchain (Go 1.26+). No prebuilt dev binary is
# downloaded yet; .github/workflows/dev.yml is the placeholder for a rolling
# dev prerelease that this script can consume later.
#
# Environment:
#   MOXIE_INSTALL    Install directory (default: $HOME/.local/bin)
#
# License: MIT
# ──────────────────────────────────────────────────────────────────────────────
set -euo pipefail

BINARY="moxie-dev"
REPO_URL="https://github.com/Milisource/moxie"
DEFAULT_INSTALL_DIR="${HOME}/.local/bin"

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
${BOLD}moxie dev installer${RESET} — build and install the ${BOLD}moxie-dev${RESET} binary

${BOLD}USAGE${RESET}
    $(basename "$0") [OPTIONS]

${BOLD}OPTIONS${RESET}
    ${BOLD}--source${RESET} <dir>     Build from an existing checkout (default: this repo).
    ${BOLD}--clone${RESET}             Shallow-clone the dev branch into a temp dir and
                        build that instead of a local checkout.
    ${BOLD}--ref${RESET} <ref>         Branch/tag to clone with --clone (default: dev).
    ${BOLD}--install${RESET} <dir>     Install directory (default: \$HOME/.local/bin).
    ${BOLD}--no-modify-path${RESET}    Skip adding the install dir to your shell config.
    ${BOLD}--help${RESET}              Show this help message.

${BOLD}ENVIRONMENT${RESET}
    ${BOLD}MOXIE_INSTALL${RESET}       Install directory (overridden by --install).

${BOLD}EXAMPLES${RESET}
    ./scripts/install-dev.sh
    ./scripts/install-dev.sh --clone
    MOXIE_INSTALL=/usr/local/bin ./scripts/install-dev.sh

${BOLD}NOTES${RESET}
    Installs as ${BOLD}${BINARY}${RESET} with data in ${BOLD}~/.config/moxie-dev${RESET}, so it
    never touches a stable (moxie) install or its database.
EOF
    exit 0
}

SOURCE_DIR=""
DO_CLONE=0
REF="dev"
INSTALL_DIR="${MOXIE_INSTALL:-$DEFAULT_INSTALL_DIR}"
NO_MODIFY_PATH=0

while [ $# -gt 0 ]; do
    case "$1" in
        --source) shift; [ $# -eq 0 ] && die "--source requires a directory"; SOURCE_DIR="$1"; shift ;;
        --clone) DO_CLONE=1; shift ;;
        --ref) shift; [ $# -eq 0 ] && die "--ref requires a value"; REF="$1"; shift ;;
        --install) shift; [ $# -eq 0 ] && die "--install requires a directory"; INSTALL_DIR="$1"; shift ;;
        --no-modify-path) NO_MODIFY_PATH=1; shift ;;
        --help|-h) usage ;;
        *) die "Unknown option: $1
  Use --help to see available options." ;;
    esac
done

TMP_CLONE=""
TMP_BIN=""
cleanup() {
    if [ -n "$TMP_BIN" ]; then rm -f "$TMP_BIN"; fi
    if [ -n "$TMP_CLONE" ]; then rm -rf "$TMP_CLONE"; fi
}
trap cleanup EXIT

command -v go >/dev/null 2>&1 || die "Go toolchain not found.
  The dev channel builds from source. Install Go ${BOLD}1.26+${RESET} and retry."

# ── Resolve the source tree ───────────────────────────────────────────────────
if [ "$DO_CLONE" -eq 1 ]; then
    command -v git >/dev/null 2>&1 || die "git not found; required for --clone."
    TMP_CLONE="$(mktemp -d -t moxie-dev-XXXXXX)"
    info "Cloning ${REF} → ${TMP_CLONE}/moxie"
    git clone --depth 1 --branch "$REF" "$REPO_URL" "$TMP_CLONE/moxie" \
        || die "git clone failed. Is the ref '${REF}' reachable?"
    SOURCE_DIR="$TMP_CLONE/moxie"
else
    if [ -z "$SOURCE_DIR" ]; then
        SOURCE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
    fi
fi

[ -f "$SOURCE_DIR/go.mod" ] || die "No go.mod in ${SOURCE_DIR}
  Point at a moxie checkout with --source, or clone one with --clone."

# ── Version ───────────────────────────────────────────────────────────────────
VERSION="dev"
if command -v git >/dev/null 2>&1 && git -C "$SOURCE_DIR" rev-parse --git-dir >/dev/null 2>&1; then
    VERSION="$(git -C "$SOURCE_DIR" describe --tags --always --dirty 2>/dev/null \
        || git -C "$SOURCE_DIR" rev-parse --short HEAD 2>/dev/null \
        || echo dev)"
fi

# ── Build ─────────────────────────────────────────────────────────────────────
TMP_BIN="$(mktemp -t "${BINARY}-build-XXXXXX")"

info "Building ${BINARY} (${VERSION}) from ${SOURCE_DIR}"
muted "  channel: dev   data: ~/.config/moxie-dev"
if ! ( cd "$SOURCE_DIR" && CGO_ENABLED=0 go build \
        -ldflags "-s -w -X main.version=${VERSION} -X main.channel=dev" \
        -o "$TMP_BIN" . ); then
    die "go build failed."
fi

# ── Install ───────────────────────────────────────────────────────────────────
mkdir -p "$INSTALL_DIR"
INSTALL_PATH="${INSTALL_DIR}/${BINARY}"
cp -f "$TMP_BIN" "$INSTALL_PATH"
chmod +x "$INSTALL_PATH"
success "Installed ${INSTALL_PATH}"

# ── Verify ────────────────────────────────────────────────────────────────────
if ACTUAL="$("$INSTALL_PATH" --version 2>&1)"; then
    success "${ACTUAL}"
else
    warn "Installed but failed to execute — check the binary/architecture."
fi

# ── PATH ──────────────────────────────────────────────────────────────────────
if [ "$NO_MODIFY_PATH" -eq 0 ] && ! echo "$PATH" | tr ':' '\n' | grep -qFx "$INSTALL_DIR"; then
    SHELL_NAME="$(basename "${SHELL:-bash}")"
    case "$SHELL_NAME" in
        fish) CONFIG_FILE="${HOME}/.config/fish/config.fish"; LINE="fish_add_path ${INSTALL_DIR}" ;;
        zsh)  CONFIG_FILE="${HOME}/.zshrc";                   LINE="export PATH=\"${INSTALL_DIR}:\$PATH\"" ;;
        *)    CONFIG_FILE="${HOME}/.bashrc";                  LINE="export PATH=\"${INSTALL_DIR}:\$PATH\"" ;;
    esac
    mkdir -p "$(dirname "$CONFIG_FILE")"
    if ! grep -qF "# >>> moxie-dev >>>" "$CONFIG_FILE" 2>/dev/null; then
        {
            echo ""
            echo "# >>> moxie-dev >>>"
            echo "$LINE"
            echo "# <<< moxie-dev <<<"
            echo ""
        } >> "$CONFIG_FILE"
        success "Added ${INSTALL_DIR} to PATH in ${CONFIG_FILE}"
        warn "Restart your terminal or 'source ${CONFIG_FILE}'."
    fi
fi

echo ""
success "moxie-dev ${VERSION} installed."
muted "  Run:  ${BINARY} --version"
muted "  Data: ~/.config/moxie-dev   (stable keeps ~/.config/moxie)"
muted "  Rebuild after dev updates:  ./scripts/install-dev.sh"
echo ""
