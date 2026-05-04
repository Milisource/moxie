#!/usr/bin/env bash
# ──────────────────────────────────────────────────────────────────────────────
# moxie — Game Library Manager
# Installer script for Linux and macOS
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/mili/moxie/main/scripts/install.sh | bash
#   curl -fsSL https://raw.githubusercontent.com/mili/moxie/main/scripts/install.sh | bash -s -- --version v0.3.3-alpha
#   curl -fsSL https://raw.githubusercontent.com/mili/moxie/main/scripts/install.sh | bash -s -- --binary ./moxie
#
# Environment variables:
#   MOXIE_VERSION    Version to install (e.g., v0.3.3-alpha). Takes priority,
#                    overridden by --version flag.
#   MOXIE_INSTALL    Install directory (default: $HOME/.local/bin)
#
# Flags:
#   --version <ver>     Install specific version (e.g., v0.3.3-alpha)
#   --binary <path>     Install from a local binary file
#   --no-modify-path    Skip shell config PATH modification
#   --help              Show this help message
#
# License: MIT
# ──────────────────────────────────────────────────────────────────────────────
set -euo pipefail

# ── Constants ─────────────────────────────────────────────────────────────────
BINARY="moxie"
GH_REPO="mili/moxie"
GH_URL="https://github.com/${GH_REPO}"
API_URL="https://api.github.com/repos/${GH_REPO}/releases"
DEFAULT_INSTALL_DIR="${HOME}/.local/bin"

# ── Terminal Colors ───────────────────────────────────────────────────────────
# Only enable colors when stdout is a terminal (skip for pipes, CI, etc.)
if [ -t 1 ]; then
    RED="\033[31m"
    ORANGE="\033[38;5;214m"
    GREEN="\033[32m"
    MUTED="\033[90m"
    BOLD="\033[1m"
    RESET="\033[0m"
else
    RED=""
    ORANGE=""
    GREEN=""
    MUTED=""
    BOLD=""
    RESET=""
fi

info()    { printf "${GREEN}${BOLD}==>${RESET}${BOLD} %s${RESET}\n" "$*"; }
success() { printf "${GREEN}${BOLD}==>${RESET} ${GREEN}%s${RESET}\n" "$*"; }
warn()    { printf "${ORANGE}${BOLD}==>${RESET}${BOLD} %s${RESET}\n" "$*" 1>&2; }
error()   { printf "${RED}${BOLD}==>${RESET}${BOLD} %s${RESET}\n" "$*" 1>&2; }
muted()   { printf "${MUTED}%s${RESET}\n" "$*"; }
die()     { error "$*"; exit 1; }

# ── Usage ─────────────────────────────────────────────────────────────────────
usage() {
    cat <<EOF
${BOLD}moxie installer${RESET} — install the moxie game library manager

${BOLD}USAGE${RESET}
    $(basename "$0") [OPTIONS]

${BOLD}OPTIONS${RESET}
    ${BOLD}--version${RESET} <ver>     Install a specific version (e.g., v0.3.3-alpha).
                        Defaults to the latest release.
    ${BOLD}--binary${RESET} <path>     Install from a local binary file instead of
                        downloading from GitHub.
    ${BOLD}--no-modify-path${RESET}    Skip adding ${DEFAULT_INSTALL_DIR} to your shell
                        configuration files (PATH modification).
    ${BOLD}--help${RESET}              Show this help message and exit.

${BOLD}ENVIRONMENT${RESET}
    ${BOLD}MOXIE_VERSION${RESET}       Version to install (overridden by --version flag).
    ${BOLD}MOXIE_INSTALL${RESET}       Install directory (default: ${DEFAULT_INSTALL_DIR}).

${BOLD}EXAMPLES${RESET}
    curl -fsSL https://raw.githubusercontent.com/mili/moxie/main/scripts/install.sh | bash
    curl -fsSL https://raw.githubusercontent.com/mili/moxie/main/scripts/install.sh | bash -s -- --version v0.3.3-alpha
    curl -fsSL https://raw.githubusercontent.com/mili/moxie/main/scripts/install.sh | bash -s -- --binary ./moxie
    MOXIE_VERSION=v0.3.3-alpha MOXIE_INSTALL=/usr/local/bin ./scripts/install.sh

EOF
    exit 0
}

# ── Banner ────────────────────────────────────────────────────────────────────
print_banner() {
    local version="$1"
    local install_dir="$2"
    cat <<EOF
${GREEN}
   ╔══════════════════════════════════════════════════╗
   ║                moxie v${version}                 ║
   ║           Game Library Manager for TUI           ║
   ╚══════════════════════════════════════════════════╝
${RESET}
${BOLD}Installed to:${RESET} ${install_dir}/${BINARY}
${BOLD}Quick start:${RESET}
${MUTED}
   moxie tui                     Open the interactive library browser
   moxie scan ~/Downloads        Scan a directory for games
   moxie list                    List all scanned games
   moxie --help                  Show all commands
${RESET}
EOF
}

# ── Cleanup ───────────────────────────────────────────────────────────────────
CLEANUP_FILES=()
cleanup() {
    if [ ${#CLEANUP_FILES[@]} -gt 0 ]; then
        for f in "${CLEANUP_FILES[@]}"; do
            [ -f "$f" ] && rm -f "$f"
        done
    fi
}
trap cleanup EXIT

# ── Platform Detection ────────────────────────────────────────────────────────
detect_platform() {
    local os arch

    os=$(uname -s | tr '[:upper:]' '[:lower:]')
    case "$os" in
        linux)  os="linux" ;;
        darwin) os="macos" ;;
        *)
            die "Unsupported operating system: ${os}. This script only supports Linux and macOS.
  Windows users: run the PowerShell installer instead:
  irm https://raw.githubusercontent.com/${GH_REPO}/main/scripts/install.ps1 | iex"
            ;;
    esac

    arch=$(uname -m)
    case "$arch" in
        x86_64|amd64)  arch="amd64" ;;
        aarch64|arm64) arch="arm64" ;;
        *)
            die "Unsupported architecture: ${arch}. Only x86_64 and arm64 are supported."
            ;;
    esac

    echo "${os}-${arch}"
}

# ── Asset Name ────────────────────────────────────────────────────────────────
asset_name() {
    local platform="$1"
    local os arch
    os="${platform%-*}"
    arch="${platform#*-}"
    if [ "$os" = "macos" ]; then
        echo "${BINARY}-macos-${arch}"
    else
        echo "${BINARY}-${os}-${arch}"
    fi
}

# ── Parse Arguments ───────────────────────────────────────────────────────────
REQUESTED_VERSION=""
LOCAL_BINARY=""
NO_MODIFY_PATH=0

while [ $# -gt 0 ]; do
    case "$1" in
        --version)
            shift
            [ $# -eq 0 ] && die "Option --version requires a version argument (e.g., v0.3.3-alpha)"
            REQUESTED_VERSION="$1"
            shift
            ;;
        --binary)
            shift
            [ $# -eq 0 ] && die "Option --binary requires a file path argument"
            LOCAL_BINARY="$1"
            if [ ! -f "$LOCAL_BINARY" ]; then
                die "Not a regular file: ${LOCAL_BINARY}
  --binary requires a compiled moxie binary, not a directory.
  To build from source: go build -o moxie .
  Then:  ./install.sh --binary ./moxie"
            fi
            shift
            ;;
        --no-modify-path)
            NO_MODIFY_PATH=1
            shift
            ;;
        --help|-h)
            usage
            ;;
        *)
            die "Unknown option: $1
  Use --help to see available options.

  Note: this is the INSTALLER script — it downloads and installs pre-built
  binaries. To BUILD from source, use the build script instead:
    ./scripts/build.sh"
            ;;
    esac
done

# ── Resolve Install Directory ─────────────────────────────────────────────────
INSTALL_DIR="${MOXIE_INSTALL:-$DEFAULT_INSTALL_DIR}"

# ── Detect Platform ───────────────────────────────────────────────────────────
PLATFORM=$(detect_platform)
info "Detected platform: ${PLATFORM}"

# ── Resolve Version ───────────────────────────────────────────────────────────
resolve_version() {
    # Priority: --version flag > MOXIE_VERSION env var > fetch latest from GitHub
    local api_url="${API_URL}"

    if [ -n "$REQUESTED_VERSION" ]; then
        echo "$REQUESTED_VERSION"
        return
    fi

    if [ -n "${MOXIE_VERSION:-}" ]; then
        echo "$MOXIE_VERSION"
        return
    fi

    # Fetch latest release tag from GitHub API
    info "Fetching latest release from ${GH_REPO}..."
    local tag
    if command -v curl &>/dev/null; then
        tag=$(curl -fsSL "${api_url}/latest" 2>/dev/null | sed -n 's/.*"tag_name": "\(.*\)".*/\1/p') || true
    elif command -v wget &>/dev/null; then
        tag=$(wget -q -O - "${api_url}/latest" 2>/dev/null | sed -n 's/.*"tag_name": "\(.*\)".*/\1/p') || true
    else
        die "Neither curl nor wget found. Install one and try again."
    fi

    if [ -z "$tag" ]; then
        die "Failed to determine latest version from GitHub API.
  Check your internet connection or specify a version manually:
    curl -fsSL .../install.sh | bash -s -- --version v0.3.3-alpha"
    fi

    echo "$tag"
}

# ── Verify Release Exists ─────────────────────────────────────────────────────
verify_release() {
    local version="$1"
    local asset="$2"
    local url="${GH_URL}/releases/download/${version}/${asset}"
    local status

    info "Verifying release ${version} exists..."
    if command -v curl &>/dev/null; then
        status=$(curl -fsSL -o /dev/null -w "%{http_code}" --head "$url" 2>/dev/null) || true
    elif command -v wget &>/dev/null; then
        status=$(wget --spider -S "$url" 2>&1 | grep "HTTP/" | tail -1 | awk '{print $2}') || true
    else
        die "Neither curl nor wget found."
    fi

    if [ "$status" != "200" ]; then
        die "Release ${version} not found or asset ${asset} unavailable.
  HTTP status: ${status}
  Verify the release exists at: ${GH_URL}/releases/tag/${version}
  Available builds: linux-amd64, linux-arm64, macos-amd64, macos-arm64, windows-amd64, windows-arm64"
    fi
    success "Release ${version} verified."
}

# ── Download ──────────────────────────────────────────────────────────────────
download() {
    local url="$1" dest="$2"
    local success=1

    if command -v curl &>/dev/null; then
        info "Downloading with curl..."
        muted "  ${url}"
        if curl -fsSL -# -o "$dest" "$url"; then
            success=0
        fi
    elif command -v wget &>/dev/null; then
        info "Downloading with wget..."
        muted "  ${url}"
        if wget -q --show-progress -O "$dest" "$url"; then
            success=0
        fi
    else
        die "Neither curl nor wget found. Install one and try again."
    fi

    return $success
}

# ── Check Already Installed ───────────────────────────────────────────────────
is_already_installed() {
    local install_path="$1"
    local target_version="$2"

    [ ! -f "$install_path" ] && return 1
    [ ! -x "$install_path" ] && return 1

    # Try to get the installed version. The binary prints "moxie <version>".
    local installed_version
    installed_version=$("$install_path" --version 2>/dev/null | awk '{print $2}') || return 1

    [ -z "$installed_version" ] && return 1
    [ "v${installed_version#v}" != "v${target_version#v}" ] && return 1

    return 0
}

# ── Install from Local Binary ─────────────────────────────────────────────────
install_local() {
    local src="$1" install_dir="$2"

    if [ ! -f "$src" ]; then
        die "Local binary not found: ${src}"
    fi
    if [ ! -x "$src" ]; then
        warn "Local binary is not executable; making it executable..."
        chmod +x "$src"
    fi

    info "Installing from local binary: ${src}"
    mkdir -p "$install_dir"
    cp -f "$src" "${install_dir}/${BINARY}"
    chmod +x "${install_dir}/${BINARY}"
    success "Binary copied to ${install_dir}/${BINARY}"
}

# ── Install from GitHub ───────────────────────────────────────────────────────
install_github() {
    local version="$1" asset="$2" install_dir="$3"
    local temp_file asset_url

    asset_url="${GH_URL}/releases/download/${version}/${asset}"
    temp_file="$(mktemp -t "${BINARY}-download-XXXXXX")"
    CLEANUP_FILES+=("$temp_file")

    info "Downloading ${BINARY} ${version} ($(echo "$asset" | sed 's/.*-//'))..."
    if ! download "$asset_url" "$temp_file"; then
        die "Download failed.
  URL: ${asset_url}
  Check your internet connection and try again."
    fi

    # Verify the downloaded file is not empty and is a binary
    local file_size
    file_size=$(stat -c%s "$temp_file" 2>/dev/null || stat -f%z "$temp_file" 2>/dev/null)
    if [ "$file_size" -lt 1000 ]; then
        die "Downloaded file is suspiciously small (${file_size} bytes). The release asset may be corrupt.
  Check: ${asset_url}"
    fi

    # Verify it's an executable binary (check for ELF/Mach-O magic bytes)
    local magic
    magic=$(xxd -p -l 4 "$temp_file" 2>/dev/null || od -A n -t x1 -N 4 "$temp_file" 2>/dev/null | tr -d ' \n')
    case "$magic" in
        7f454c46|feedfacf|cffaedfe|cefaedfe)
            # ELF (7f454c46), Mach-O 64 (feedfacf, cffaedfe, cefaedfe) — all valid
            ;;
        *)
            warn "File does not appear to be a valid binary (magic: ${magic}). Proceeding anyway..."
            ;;
    esac

    mkdir -p "$install_dir"
    cp -f "$temp_file" "${install_dir}/${BINARY}"
    chmod +x "${install_dir}/${BINARY}"
    success "Downloaded and installed to ${install_dir}/${BINARY}"
}

# ── PATH Modification ─────────────────────────────────────────────────────────
modify_path() {
    local install_dir="$1"

    # GitHub Actions — just add to GITHUB_PATH
    if [ -n "${GITHUB_PATH:-}" ]; then
        info "Detected GitHub Actions environment."
        if ! echo "$PATH" | tr ':' '\n' | grep -qFx "$install_dir"; then
            echo "$install_dir" >> "$GITHUB_PATH"
            success "Added ${install_dir} to GITHUB_PATH."
        else
            muted "${install_dir} is already in GITHUB_PATH."
        fi
        return
    fi

    # Skip if --no-modify-path was specified
    if [ "$NO_MODIFY_PATH" -eq 1 ]; then
        muted "Skipping PATH modification (--no-modify-path)."
        return
    fi

    # Skip if already in PATH
    if echo "$PATH" | tr ':' '\n' | grep -qFx "$install_dir"; then
        muted "${install_dir} is already in your PATH."
        return
    fi

    info "Adding ${install_dir} to your PATH..."

    # Detect shell and pick the right config file
    local shell_name config_file config_line header footer ext

    shell_name=$(basename "${SHELL:-}")
    if [ -z "$shell_name" ] || [ "$shell_name" = "sh" ]; then
        # Try to detect from parent process
        shell_name=$(ps -o comm= -p "$PPID" 2>/dev/null | sed 's/^-//' | head -1) || true
        shell_name="${shell_name:-bash}"
    fi

    case "$shell_name" in
        fish)
            config_file="${HOME}/.config/fish/config.fish"
            header="# >>> moxie >>>
"
            config_line="fish_add_path ${install_dir}
"
            footer="# <<< moxie <<<"
            ext="fish"
            ;;
        zsh)
            config_file="${HOME}/.zshrc"
            header="# >>> moxie >>>
"
            config_line="export PATH=\"${install_dir}:\$PATH\"
"
            footer="# <<< moxie <<<"
            ext="zsh"
            ;;
        bash)
            config_file="${HOME}/.bashrc"
            header="# >>> moxie >>>
"
            config_line="export PATH=\"${install_dir}:\$PATH\"
"
            footer="# <<< moxie <<<"
            ext="bash"
            ;;
        *)
            warn "Unknown shell: ${shell_name}. Falling back to ~/.profile."
            config_file="${HOME}/.profile"
            header="# >>> moxie >>>
"
            config_line="export PATH=\"${install_dir}:\$PATH\"
"
            footer="# <<< moxie <<<"
            ext="sh"
            ;;
    esac

    # Ensure parent directory exists
    mkdir -p "$(dirname "$config_file")"

    # Check if our block already exists in the config
    if [ -f "$config_file" ] && grep -qF "# >>> moxie >>>" "$config_file" 2>/dev/null; then
        # Already has our block — skip to avoid duplicates
        muted "PATH modification already present in ${config_file}."
        return
    fi

    # Append PATH modification block
    {
        echo ""
        printf "%s" "$header"
        printf "%s" "$config_line"
        printf "%s" "$footer"
        echo ""
    } >> "$config_file"

    success "Added to PATH in ${config_file}."
    warn "Restart your terminal or run 'source ${config_file}' for PATH changes to take effect."
}

# ── Post-Install Verification ─────────────────────────────────────────────────
verify_install() {
    local install_path="$1"
    local expected_version="$2"

    info "Verifying installation..."

    if [ ! -f "$install_path" ]; then
        die "Binary not found at ${install_path}. Installation may have failed."
    fi

    if [ ! -x "$install_path" ]; then
        die "Binary at ${install_path} is not executable. Try: chmod +x ${install_path}"
    fi

    # Try to run the binary
    local actual_version
    actual_version=$("$install_path" --version 2>&1) || {
        warn "Binary installed but failed to execute.
  This may indicate:
    - The binary is incompatible with your system architecture
    - Missing shared library dependencies (unlikely — moxie is a static binary)
  Check: ${install_path}"
        return 1
    }

    success "${actual_version}"

    # Version sanity check
    if [ -n "$expected_version" ]; then
        local installed_ver
        installed_ver=$(echo "$actual_version" | awk '{print $2}')
        installed_ver="v${installed_ver#v}"
        expected_version="v${expected_version#v}"

        if [ "$installed_ver" != "$expected_version" ]; then
            warn "Version mismatch: expected ${expected_version}, got ${installed_ver}"
        fi
    fi

    return 0
}

# ── Main ──────────────────────────────────────────────────────────────────────
main() {
    # Welcome
    info "moxie installer — Game Library Manager for the terminal"
    muted "  Repo: ${GH_URL}"
    echo ""

    # Determine install path
    INSTALL_PATH="${INSTALL_DIR}/${BINARY}"

    # ── Mode 1: Install from local binary (no network needed) ───────────────
    if [ -n "$LOCAL_BINARY" ]; then
        install_local "$LOCAL_BINARY" "$INSTALL_DIR"

        # Try to detect version from the binary itself.
        local local_ver
        local_ver=$("$INSTALL_PATH" --version 2>/dev/null | awk '{print $2}') || true
        if [ -n "$local_ver" ]; then
            print_banner "$local_ver" "$INSTALL_DIR"
        else
            print_banner "local" "$INSTALL_DIR"
        fi

        verify_install "$INSTALL_PATH" ""
        modify_path "$INSTALL_DIR"
        success "Installation complete!"
        return
    fi

    # ── Mode 2: Install from GitHub Releases (needs network) ────────────────

    # Resolve version
    VERSION=$(resolve_version)
    if [ -z "$VERSION" ]; then
        die "Could not determine which version to install."
    fi
    info "Target version: ${VERSION}"
    ASSET_NAME=$(asset_name "$PLATFORM")
    ASSET_URL="${GH_URL}/releases/download/${VERSION}/${ASSET_NAME}"

    # Check if already installed with the same version
    if is_already_installed "$INSTALL_PATH" "$VERSION"; then
        info "${BINARY} ${VERSION} is already installed at ${INSTALL_PATH}."
        if [ "$NO_MODIFY_PATH" -eq 0 ]; then
            modify_path "$INSTALL_DIR"
        fi
        print_banner "$VERSION" "$INSTALL_DIR"
        success "Nothing to do — already up to date!"
        return
    fi

    # Verify the release exists before downloading
    verify_release "$VERSION" "$ASSET_NAME"

    # Download and install
    install_github "$VERSION" "$ASSET_NAME" "$INSTALL_DIR"

    # Verify the installation
    verify_install "$INSTALL_PATH" "$VERSION"

    # Modify PATH
    modify_path "$INSTALL_DIR"

    # Print banner
    print_banner "$VERSION" "$INSTALL_DIR"

    # Check if dir is in PATH
    if [ "$NO_MODIFY_PATH" -eq 0 ] && ! echo "$PATH" | tr ':' '\n' | grep -qFx "$INSTALL_DIR"; then
        warn "${INSTALL_DIR} is not yet in your current PATH."
        muted "  Restart your terminal or run:"
        case "$(basename "${SHELL:-bash}")" in
            fish) muted "    fish_add_path ${INSTALL_DIR}" ;;
            *)    muted "    export PATH=\"${INSTALL_DIR}:\$PATH\"" ;;
        esac
        echo ""
    fi

    success "Installation complete!"
}

main "$@"
