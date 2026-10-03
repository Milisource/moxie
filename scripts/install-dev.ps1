<#
.SYNOPSIS
    Installs the moxie DEV build (moxie-dev.exe) from source.
.DESCRIPTION
    The dev counterpart to install.ps1 (the stable/main channel). The two are
    deliberately distinct and can coexist:

        channel   binary          data directory
        stable    moxie.exe       %APPDATA%\moxie
        dev       moxie-dev.exe   %APPDATA%\moxie-dev

    Because the dev build has its own database, a dev schema migration can
    never strand a stable install.

    Builds from a local checkout by default, or shallow-clones the dev branch
    with -Clone. Requires a Go toolchain (1.26+). No prebuilt dev binary is
    downloaded yet; .github\workflows\dev.yml is the placeholder for a rolling
    dev prerelease this script can consume later.
.PARAMETER Source
    Path to a moxie checkout to build. Defaults to this repository.
.PARAMETER Clone
    Shallow-clone the dev branch into a temp directory and build that.
.PARAMETER Ref
    Branch/tag to clone with -Clone. Defaults to "dev".
.PARAMETER Install
    Install directory. Defaults to %LOCALAPPDATA%\moxie-dev\bin.
.PARAMETER NoModifyPath
    Skip adding the install directory to your user PATH.
.EXAMPLE
    .\scripts\install-dev.ps1
    .\scripts\install-dev.ps1 -Clone
    .\scripts\install-dev.ps1 -Install D:\bin -NoModifyPath
#>
[CmdletBinding()]
param(
    [string]$Source,
    [switch]$Clone,
    [string]$Ref = 'dev',
    [string]$Install,
    [switch]$NoModifyPath,
    [switch]$Help
)

$ErrorActionPreference = 'Stop'

$BinaryName = 'moxie-dev'
$RepoUrl    = 'https://github.com/Milisource/moxie'

if ($Help) {
    @"
moxie dev installer — build and install the $BinaryName binary

USAGE
    .\scripts\install-dev.ps1 [OPTIONS]

OPTIONS
    -Source <dir>       Build from an existing checkout (default: this repo).
    -Clone              Shallow-clone the dev branch into a temp dir and build.
    -Ref <ref>          Branch/tag to clone with -Clone (default: dev).
    -Install <dir>      Install directory (default: %LOCALAPPDATA%\moxie-dev\bin).
    -NoModifyPath       Skip adding the install directory to your user PATH.
    -Help               Show this help message.

NOTES
    Installs as $BinaryName with data in %APPDATA%\moxie-dev, so it never
    touches a stable (moxie) install or its database.
"@
    exit 0
}

function Write-Step { Write-Host "==> $args" -ForegroundColor Cyan }
function Write-Ok   { Write-Host "==> $args" -ForegroundColor Green }
function Write-Info { Write-Host "    $args" -ForegroundColor DarkGray }
function Write-Warn { Write-Host "==> $args" -ForegroundColor Yellow }

$InstallDir  = if ($Install) { $Install } elseif ($env:MOXIE_INSTALL) { $env:MOXIE_INSTALL } else { "$env:LOCALAPPDATA\$BinaryName\bin" }
$InstallPath = Join-Path $InstallDir "$BinaryName.exe"

function Add-ToUserPath {
    param([string]$Directory)
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if ($userPath -and ($userPath -like "*$Directory*")) {
        Write-Info "$Directory is already in your PATH."
        return
    }
    if ([string]::IsNullOrEmpty($userPath)) { $newPath = $Directory }
    elseif ($userPath.EndsWith(';')) { $newPath = "$userPath$Directory;" }
    else { $newPath = "$userPath;$Directory;" }
    [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
    $env:Path = "$env:Path;$Directory"
    Write-Ok "Added $Directory to user PATH."
    Write-Warn 'Restart other terminal windows for PATH changes to take full effect.'
}

function Install-MoxieDev {
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        throw "Go toolchain not found. The dev channel builds from source.`n  Install Go 1.26+ from https://go.dev/dl/ and retry."
    }

    $tmpClone = $null
    if ($Clone) {
        if (-not (Get-Command git -ErrorAction SilentlyContinue)) { throw 'git not found; required for -Clone.' }
        $tmpClone = Join-Path $env:TEMP "moxie-dev-$([guid]::NewGuid().ToString('N').Substring(0,8))"
        New-Item -ItemType Directory -Force -Path $tmpClone | Out-Null
        $repoDir = Join-Path $tmpClone 'moxie'
        Write-Step "Cloning $Ref -> $repoDir"
        & git clone --depth 1 --branch $Ref $RepoUrl $repoDir
        if ($LASTEXITCODE -ne 0) { throw "git clone failed. Is the ref '$Ref' reachable?" }
        $Source = $repoDir
    }

    if (-not $Source) { $Source = Split-Path -Parent $PSScriptRoot }
    $Source = (Resolve-Path -LiteralPath $Source).Path

    if (-not (Test-Path -LiteralPath (Join-Path $Source 'go.mod'))) {
        throw "No go.mod in $Source`n  Point at a moxie checkout with -Source, or clone one with -Clone."
    }

    # ── Version ────────────────────────────────────────────────
    $version = 'dev'
    try {
        $desc = & git -C $Source describe --tags --always --dirty 2>$null
        if ($LASTEXITCODE -eq 0 -and $desc) { $version = "$desc".Trim() }
    } catch { }

    # ── Build ──────────────────────────────────────────────────
    Write-Step "Building $BinaryName ($version) from $Source"
    Write-Info 'channel: dev   data: %APPDATA%\moxie-dev'
    $staging = Join-Path $env:TEMP "$BinaryName-build.exe"
    Push-Location $Source
    try {
        $env:CGO_ENABLED = '0'
        & go build -ldflags "-s -w -X main.version=$version -X main.channel=dev" -o $staging .
        if ($LASTEXITCODE -ne 0) { throw 'go build failed.' }
    } finally {
        Pop-Location
    }

    # ── Install ────────────────────────────────────────────────
    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    Copy-Item -LiteralPath $staging -Destination $InstallPath -Force
    Remove-Item -LiteralPath $staging -Force -ErrorAction SilentlyContinue
    Write-Ok "Installed $InstallPath"

    # ── Verify ─────────────────────────────────────────────────
    try {
        $out = & $InstallPath '--version' 2>&1
        if ($LASTEXITCODE -eq 0) { Write-Ok "$("$out".Trim())" }
        else { Write-Warn "Installed but exited $LASTEXITCODE during verification." }
    } catch {
        Write-Warn "Installed but failed to execute: $($_.Exception.Message)"
    }

    if ($tmpClone) { Remove-Item -Recurse -Force -Path $tmpClone -ErrorAction SilentlyContinue }

    if (-not $NoModifyPath) { Add-ToUserPath -Directory $InstallDir }
    else { Write-Info "Skipping PATH modification. Add $InstallDir manually." }

    Write-Host ''
    Write-Ok "$BinaryName $version installed."
    Write-Info "Run:  $BinaryName --version"
    Write-Info "Data: %APPDATA%\moxie-dev   (stable keeps %APPDATA%\moxie)"
    Write-Info 'Rebuild after dev updates:  .\scripts\install-dev.ps1'
    Write-Host ''
}

try {
    Install-MoxieDev
    exit 0
} catch {
    Write-Host ''
    Write-Host "✗ $($_.Exception.Message)" -ForegroundColor Red
    Write-Host ''
    exit 1
}
