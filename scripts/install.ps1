# Install moxie to $env:LOCALAPPDATA\moxie\bin\ from GitHub Releases
param()

$ErrorActionPreference = "Stop"
$Binary = "moxie"
$GhRepo = "mili/moxie"
$InstallDir = "$env:LOCALAPPDATA\$Binary\bin"

# --- detect architecture ---
$Arch = "amd64"
if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") {
    $Arch = "arm64"
} elseif ([Environment]::Is64BitOperatingSystem) {
    $Arch = "amd64"
} else {
    Write-Host "Unsupported architecture: 32-bit not supported." -ForegroundColor Red
    exit 1
}

# --- determine download URL ---
$Asset = "${Binary}-windows-${Arch}.exe"
if ($env:MOXIE_VERSION) {
    $DownloadUrl = "https://github.com/${GhRepo}/releases/download/$env:MOXIE_VERSION/$Asset"
} else {
    $DownloadUrl = "https://github.com/${GhRepo}/releases/latest/download/$Asset"
}

# --- download ---
$TempFile = "$env:TEMP\${Binary}-$(Get-Random).exe"

try {
    Write-Host "📦 Downloading $Binary for Windows/$Arch..."
    Invoke-WebRequest -Uri $DownloadUrl -OutFile $TempFile -UseBasicParsing
} catch {
    Write-Host ""
    Write-Host "No release found. Build from source: go build -o moxie.exe ." -ForegroundColor Yellow
    exit 1
}

# --- install ---
try {
    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    Copy-Item -Force $TempFile "$InstallDir\$Binary.exe"
    Write-Host "⬇ Installed to $InstallDir\$Binary.exe"
} finally {
    Remove-Item -Force $TempFile -ErrorAction SilentlyContinue
}

# --- add to PATH (user scope) ---
$CurrentUserPath = [Environment]::GetEnvironmentVariable("Path", "User") ?? ""
if ($CurrentUserPath -notlike "*$InstallDir*") {
    Write-Host "✅ Adding $InstallDir to user PATH..."
    [Environment]::SetEnvironmentVariable(
        "Path",
        "$CurrentUserPath;$InstallDir",
        "User"
    )
    # Update current session PATH immediately
    $env:Path = "$env:Path;$InstallDir"
    Write-Host "⚠  Restart your terminal for PATH changes to take full effect."
}

# --- verify ---
try {
    & "$InstallDir\$Binary.exe" --version
} catch {
    Write-Host "⚠  Installed but binary failed to run." -ForegroundColor Red
    exit 1
}

Write-Host "🚀 Done."
