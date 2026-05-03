<#
.SYNOPSIS
    Installs moxie — a local game library manager for F95Zone games.
.DESCRIPTION
    Downloads the moxie binary from GitHub Releases to $env:LOCALAPPDATA\moxie\bin\
    and optionally adds it to your user PATH. Windows 64-bit only (AMD64, ARM64).
.PARAMETER Version
    Specific version to install (e.g. "0.3.3", "v0.3.3", "0.3.3-alpha").
    Defaults to latest. Takes priority over $env:MOXIE_VERSION.
.PARAMETER Binary
    Path to a pre-downloaded moxie binary. Skips the download step.
.PARAMETER NoModifyPath
    Skip adding the install directory to your user PATH.
.EXAMPLE
    .\install.ps1                          # Latest release
    .\install.ps1 -Version v0.3.3          # Specific version
    .\install.ps1 -Binary .\moxie.exe      # Local binary
    .\install.ps1 -NoModifyPath            # No PATH changes
#>
[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [string]$Version,
    [string]$Binary,
    [switch]$NoModifyPath,
    [switch]$Help
)

$ErrorActionPreference = 'Stop'

# ── Help ──────────────────────────────────────────────────────────
if ($Help) {
    @"
moxie installer — install the moxie game library manager

USAGE
    .\install.ps1 [OPTIONS]

OPTIONS
    -Version <ver>      Install a specific version (e.g., v0.3.3-alpha).
                        Defaults to the latest release.
    -Binary <path>      Install from a local binary file instead of
                        downloading from GitHub.
    -NoModifyPath       Skip adding the install directory to your
                        user PATH.
    -Help               Show this help message and exit.

ENVIRONMENT
    MOXIE_VERSION       Version to install (overridden by -Version flag).
    MOXIE_INSTALL       Install directory (default: `$env:LOCALAPPDATA\moxie\bin).

EXAMPLES
    iwr https://raw.githubusercontent.com/mili/moxie/main/scripts/install.ps1 -OutFile install.ps1
    .\install.ps1
    .\install.ps1 -Version v0.3.3-alpha
    .\install.ps1 -Binary .\moxie.exe
    `$env:MOXIE_VERSION = 'v0.3.3-alpha'; .\install.ps1 -NoModifyPath

"@
    exit 0
}

# ── Configuration ─────────────────────────────────────────────────
$BinaryName  = 'moxie'
$GhRepo      = 'mili/moxie'
$defaultDir  = "$env:LOCALAPPDATA\$BinaryName\bin"
$InstallDir  = if ($env:MOXIE_INSTALL) { $env:MOXIE_INSTALL } else { $defaultDir }
$InstallPath = "$InstallDir\$BinaryName.exe"
$ReleaseUrl  = "https://github.com/$GhRepo/releases"
$TempDir     = "$env:TEMP\$BinaryName-install"

# ── Colored output helpers ────────────────────────────────────────
function Write-Step  { Write-Host "→ $args" -ForegroundColor Cyan }
function Write-Ok    { Write-Host "✓ $args" -ForegroundColor Green }
function Write-Info  { Write-Host "  $args" -ForegroundColor DarkGray }
function Write-Warn  { Write-Host "⚠ $args" -ForegroundColor Yellow }
function Write-Err   { Write-Host "✗ $args" -ForegroundColor Red }

# ── Architecture detection ────────────────────────────────────────
function Get-Architecture {
    if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { return 'arm64' }
    if (-not [Environment]::Is64BitOperatingSystem) {
        throw '32-bit Windows is not supported. moxie requires a 64-bit OS.'
    }
    return 'amd64'
}

# ── Version resolution (param > env > latest) ─────────────────────
function Resolve-Version {
    $raw = $Version
    if (-not $raw) { $raw = $env:MOXIE_VERSION }
    if ($raw) {
        $display = $raw -replace '^v', ''
        return @{ Tag = "v$display"; Display = $display }
    }
    return @{ Tag = 'latest'; Display = 'latest' }
}

# ── Check installed version ───────────────────────────────────────
function Get-InstalledVersion {
    if (-not (Test-Path -LiteralPath $InstallPath)) { return $null }
    try {
        $result = & $InstallPath '--version' 2>&1
        if ($LASTEXITCODE -eq 0 -and $result) { return "$result".Trim() }
    } catch { }
    return $null
}

# ── HEAD request to verify release URL ────────────────────────────
function Test-UrlExists {
    param([string]$Url)
    try {
        $r = Invoke-WebRequest -Uri $Url -Method Head -UseBasicParsing `
            -TimeoutSec 15 -MaximumRedirection 5 -ErrorAction Stop
        return $r.StatusCode -eq 200
    } catch {
        if ($_.Exception.Response -and $_.Exception.Response.StatusCode -eq 404) {
            return $false
        }
        throw
    }
}

# ── Add directory to user PATH ────────────────────────────────────
function Add-ToUserPath {
    param([string]$Directory)
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if ($userPath -and ($userPath -like "*$Directory*")) {
        Write-Info "$Directory is already in your PATH."
        return
    }
    if ([string]::IsNullOrEmpty($userPath)) {
        $newPath = $Directory
    } elseif ($userPath.EndsWith(';')) {
        $newPath = "$userPath$Directory;"
    } else {
        $newPath = "$userPath;$Directory;"
    }
    [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
    Write-Ok "Added $Directory to user PATH."
    $env:Path = "$env:Path;$Directory"
    Write-Warn 'Restart other terminal windows for PATH changes to take full effect.'
}

# ═══════════════════════════════════════════════════════════════════
# Main installation
# ═══════════════════════════════════════════════════════════════════

function Install-Moxie {
    # ── Welcome ─────────────────────────────────────────────────
    Write-Step "moxie installer — Game Library Manager for the terminal"

    # ── Detect architecture ─────────────────────────────────────
    Write-Step 'Detecting system architecture...'
    $arch = Get-Architecture
    Write-Ok "Windows ($arch)"

    # ── Resolve version ─────────────────────────────────────────
    Write-Step 'Resolving version...'
    $ver = Resolve-Version
    Write-Ok "moxie $($ver.Display)"

    # ── Already-installed check ─────────────────────────────────
    if ($ver.Tag -ne 'latest') {
        $installed = Get-InstalledVersion
        $nInstalled = if ($installed) { $installed -replace '^v', '' } else { $null }
        if ($installed -and $nInstalled -eq $ver.Display) {
            Write-Ok "moxie $($ver.Display) is already installed at:"
            Write-Ok "  $InstallPath"
            Write-Info 'Use -Version to install a different version, or delete the binary to reinstall.'
            return $ver
        }
        if ($installed) { Write-Info "Currently: $installed → Requested: $($ver.Display)" }
    }

    # ── Stage the binary ────────────────────────────────────────
    $null = New-Item -ItemType Directory -Force -Path $TempDir
    $stagingFile = "$TempDir\$BinaryName.exe"

    if ($Binary) {
        # ── Local binary path ──────────────────────────────────
        Write-Step "Installing from: $Binary"
        if (-not (Test-Path -LiteralPath $Binary)) {
            throw "File not found at '$Binary'."
        }
        if ((Get-Item -LiteralPath $Binary) -isnot [System.IO.FileInfo]) {
            throw "'$Binary' is not a regular file (directories not supported).`n" +
                  "  Build from source first: go build -o moxie.exe ."
        }
        Copy-Item -LiteralPath (Resolve-Path -LiteralPath $Binary) -Destination $stagingFile -Force
        Write-Ok 'Local binary staged.'
    } else {
        # ── Download from GitHub Releases ──────────────────────
        $assetName = "$BinaryName-windows-$arch.exe"
        $downloadUrl = if ($ver.Tag -eq 'latest') {
            "$ReleaseUrl/latest/download/$assetName"
        } else {
            "$ReleaseUrl/download/$($ver.Tag)/$assetName"
        }

        Write-Step 'Checking release...'
        $exists = Test-UrlExists -Url $downloadUrl
        if (-not $exists) {
            throw "Release $($ver.Display) not found for Windows/$arch.`n" +
                  "  URL checked: $downloadUrl`n" +
                  "  Verify at: $ReleaseUrl"
        }
        Write-Ok 'Release found.'

        Write-Step "Downloading moxie $($ver.Display) ($arch)..."
        $ProgressPreference = 'SilentlyContinue'
        try {
            Invoke-WebRequest -Uri $downloadUrl -OutFile $stagingFile -UseBasicParsing
        } finally {
            $ProgressPreference = 'Continue'
        }
        $size = [math]::Round((Get-Item $stagingFile).Length / 1MB, 2)
        Write-Ok "Downloaded $size MB"
    }

    # ── Install ────────────────────────────────────────────────
    Write-Step 'Installing binary...'
    $null = New-Item -ItemType Directory -Force -Path $InstallDir
    Copy-Item -Path $stagingFile -Destination $InstallPath -Force
    Write-Ok "Installed to $InstallPath"
    Remove-Item -Path $TempDir -Recurse -Force -ErrorAction SilentlyContinue

    # ── Verify ─────────────────────────────────────────────────
    Write-Step 'Verifying installation...'
    try {
        $versionOut = & $InstallPath '--version' 2>&1
        if ($LASTEXITCODE -eq 0) { Write-Ok "$($versionOut.Trim())" }
        else { throw "Exit code: $LASTEXITCODE" }
    } catch {
        Write-Err "Binary at $InstallPath failed to execute."
        Write-Err "Error: $($_.Exception.Message)"
        throw 'Verification failed. The binary may be corrupted or incompatible.'
    }

    # ── PATH modification ──────────────────────────────────────
    if (-not $NoModifyPath) {
        # GitHub Actions — add to GITHUB_PATH
        if ($env:GITHUB_PATH) {
            Write-Step 'Detected GitHub Actions environment.'
            Add-Content -Path $env:GITHUB_PATH -Value $InstallDir
            Write-Ok "Added $InstallDir to GITHUB_PATH."
        } else {
            Write-Step 'Configuring PATH...'
            Add-ToUserPath -Directory $InstallDir
        }
    } else {
        Write-Info 'Skipping PATH modification (-NoModifyPath).'
        Write-Info "Add $InstallDir to your PATH manually."
    }

    return $ver
}

# ═══════════════════════════════════════════════════════════════════
# Entry point
# ═══════════════════════════════════════════════════════════════════

try {
    $versionInfo = Install-Moxie

    # ── Post-install banner ───────────────────────────────────────
    Write-Host ''
    Write-Host "  ┌──────────────────────────────────────────────────────────┐" -ForegroundColor Cyan
    Write-Host "  │  moxie $($versionInfo.Display)" -ForegroundColor Green
    Write-Host "  │  Game Library Manager for the terminal" -ForegroundColor Cyan
    Write-Host "  └──────────────────────────────────────────────────────────┘" -ForegroundColor Cyan
    Write-Host ''
    Write-Host "  Installed to: $InstallPath" -ForegroundColor DarkGray
    Write-Host '  Quick start:' -ForegroundColor Yellow
    Write-Host "    moxie tui             Launch the terminal UI" -ForegroundColor White
    Write-Host "    moxie scan ~\Downloads  Scan a directory for games" -ForegroundColor White
    Write-Host "    moxie list            List all scanned games" -ForegroundColor White
    Write-Host "    moxie --help          Show all commands" -ForegroundColor White
    Write-Host ''
    Write-Info "Documentation: https://github.com/$GhRepo"
    Write-Host ''

    exit 0
} catch {
    Write-Host ''
    Write-Err $_.Exception.Message
    Write-Host ''
    Write-Warn 'Need help? Open an issue at https://github.com/mili/moxie/issues/new'
    Write-Host ''
    exit 1
}
