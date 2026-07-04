# `internal/steam/` — Steam Library Integration Package Design

**Status:** Phase 1 Implemented (May 2026)  
**Author:** Architecture Design  
**Date:** 2026-05-02

## Phase 1 Implementation Notes

Phase 1 is complete. All 6 planned files implemented (1,027 source lines), CLI integration
in `main.go` (`steam add` command), and 18 code-review fixes applied post-implementation.
Key safety features: backup-before-write on both shortcuts.vdf and config.vdf, atomic
temp-file+rename writes, unknown VDF field preservation via `RawFields`, and mandatory
Steam-running re-verification after user confirmation prompt.

### Deviations from design
- `EnsureGridDir` removed (redundant — `MkdirAll` already called in write paths)
- `RawFields` map added to `ShortcutEntry` for round-trip safety (per review feedback)
- `getCompatToolMapping` helper extracted to DRY config.vdf navigation
- `SetAllArtwork` vectorized: downloads once, resizes to 3 dimensions
- Shared `httpClient` at package level instead of per-request creation

---

## 1. Package Structure & Public API

### 1.1 File Listing

```
internal/steam/
├── steam.go          # Steam discovery, user enumeration, multi-platform paths
├── types.go           # Core shared types and constants
├── shortcuts.go       # shortcuts.vdf read/write/query
├── grid.go            # Artwork download, resize, placement
├── proton.go          # Proton/Wine compatibility config (Linux)
├── steamgriddb.go     # SteamGridDB API client (optional; can be deferred)
├── appid.go           # Steam App ID extraction from store URLs
└── steam_test.go      # Test fixtures and unit tests
```

### 1.2 `types.go` — Core Types & Constants

```go
package steam

import "time"

// ---------------------------------------------------------------------------
// Artwork dimensions (Steam standard sizes)
// ---------------------------------------------------------------------------

type ArtType int

const (
    ArtVertical  ArtType = iota // 600x900 portrait grid  ("p"  suffix)
    ArtHorizontal                // 460x215 landscape grid (no   suffix)
    ArtHero                      // 1920x620 hero banner  ("_hero" suffix)
    ArtLogo                      // logo                   ("_logo" suffix)
    ArtIcon                      // icon                   ("_icon" suffix)
)

func (a ArtType) Suffix() string {
    switch a {
    case ArtVertical:   return "p"
    case ArtHorizontal: return ""
    case ArtHero:       return "_hero"
    case ArtLogo:       return "_logo"
    case ArtIcon:       return "_icon"
    }
    return ""
}

func (a ArtType) Dimensions() (int, int) {
    switch a {
    case ArtVertical:   return 600, 900
    case ArtHorizontal: return 460, 215
    case ArtHero:       return 1920, 620
    case ArtLogo:       return 0, 0   // variable
    case ArtIcon:       return 0, 0   // variable
    }
    return 0, 0
}

// ---------------------------------------------------------------------------
// ShortcutEntry — matches the binary VDF shortcut structure
// ---------------------------------------------------------------------------

type ShortcutEntry struct {
    AppID              uint32   `vdf:"appid"`
    AppName            string   `vdf:"AppName"`
    Exe                string   `vdf:"exe"`
    StartDir           string   `vdf:"StartDir"`
    Icon               string   `vdf:"icon"`               // path to icon file
    LaunchOptions      string   `vdf:"LaunchOptions"`
    Tags               []string `vdf:"tags"`
    IsHidden           bool     `vdf:"IsHidden"`
    AllowDesktopConfig bool     `vdf:"AllowDesktopConfig"`
    AllowOverlay       bool     `vdf:"AllowOverlay"`
    OpenVR             bool     `vdf:"OpenVR"`
    Devkit             bool     `vdf:"Devkit"`
    LastPlayTime       uint32   `vdf:"LastPlayTime"`
    FlatpakAppID       string   `vdf:"FlatpakAppID"`
    SortAs             string   `vdf:"sortas"`
}

// ShortcutFile is the top-level structure containing all shortcuts.
type ShortcutFile struct {
    Shortcuts []ShortcutEntry `vdf:"shortcuts"`
}

// ---------------------------------------------------------------------------
// Steam paths for a given user
// ---------------------------------------------------------------------------

type SteamPaths struct {
    SteamRoot    string   // ~/.steam/steam or ~/.var/app/com.valvesoftware.Steam/.local/share/Steam
    UserDataDir  string   // <SteamRoot>/userdata/<steamID3>
    ShortcutsVDF string   // <UserDataDir>/config/shortcuts.vdf
    GridDir      string   // <UserDataDir>/config/grid/
    ConfigVDF    string   // <SteamRoot>/config/config.vdf
    CompatVDF    string   // <SteamRoot>/compatibilitytools.d/  (custom proton dir)
}

// ---------------------------------------------------------------------------
// SteamGridDB types (deferrable)
// ---------------------------------------------------------------------------

type SGDBGameResult struct {
    ID         int    `json:"id"`
    Name       string `json:"name"`
    SteamAppID int    `json:"steam_appid"`
}

type SGDBImageResult struct {
    ID        int    `json:"id"`
    Score     int    `json:"score"`
    Style     string `json:"style"`
    URL       string `json:"url"`
    Thumbnail string `json:"thumb"`
    Width     int    `json:"width"`
    Height    int    `json:"height"`
}
```

### 1.3 `steam.go` — Steam Discovery & Session Management

**Platform strategy:** Linux is the primary target. Windows and macOS awareness is for path resolution but the design does not require full testing on those platforms initially.

```go
package steam

// FindSteamRoot locates the Steam installation directory for the current platform.
// On Linux, checks XDG paths and Flatpak. Returns an empty string if not found.
//
// Platform search order:
//   Linux:   $HOME/.steam/steam  →  Flatpak path  →  error
//   Windows: %ProgramFiles(x86)%/Steam  →  error
//   macOS:   ~/Library/Application Support/Steam  →  error
func FindSteamRoot() (string, error)

// FindSteamUsers scans <steamRoot>/userdata/ for numeric directory names
// (Steam ID 3, the per-account folder). Returns the sorted list of UserDataDir
// paths. Each path is <steamRoot>/userdata/<steamID3>.
//
// Raises a warning if zero users are found (Steam may not be installed or
// no accounts have been logged in).
func FindSteamUsers(steamRoot string) ([]uint32, error)

// ResolveSteamPaths builds the full SteamPaths struct for a given user.
// Validates that the user directory exists. Returns an error if paths are
// missing (e.g., no config/grid/ dir — which can be created on first write).
func ResolveSteamPaths(steamRoot string, userID3 uint32) (*SteamPaths, error)

// IsSteamRunning checks whether the Steam client process is currently running.
// This is a safety check — modifying shortcuts.vdf while Steam is running
// will result in Steam overwriting the file on exit.
//
// On Linux, checks for "steam" in /proc and/or checks the running D-Bus service.
// Returns (true, nil) if Steam is running, (false, nil) if not.
func IsSteamRunning() (bool, error)
```

**Design notes:**
- `FindSteamRoot` uses `os.UserHomeDir()` for cross-platform $HOME resolution
- Flatpak detection checks `os.Getenv("FLATPAK_ID")` or probes the path directly
- `FindSteamUsers` uses `os.ReadDir` and filters by numeric directory names (SteamID3 is always numeric)
- `IsSteamRunning` on Linux: can check `/proc/*/comm` for "steam" or use `os.FindProcess` — keep it simple, no dependency needed
- The `SteamPaths` struct is immutable once resolved; it's a pure value type

### 1.4 `shortcuts.go` — shortcuts.vdf Management

Binary VDF parsing is **vendored** in `vdf.go` (replacing the unmaintained `github.com/wakeful-cloud/vdf`). The vendored code uses a custom binary VDF reader/writer (`readVdf`, `writeVdf`, `vdfMap` type) providing the same functionality without external dependency risk.

```go
package steam

// ReadShortcuts reads and parses the binary VDF shortcuts file at the given path.
// Returns a parsed ShortcutFile or an error if the file is missing, corrupt,
// or otherwise unreadable.
//
// On file-not-exists, returns a ShortcutFile with an empty Shortcuts slice.
func ReadShortcuts(path string) (*ShortcutFile, error)

// WriteShortcuts serializes the ShortcutFile back to binary VDF format and
// writes it to the given path.
//
// SAFETY: Automatically creates a backup of the original file before writing.
// The backup is named "<path>.backup-<timestamp>". If the write succeeds,
// the backup is retained for 1 hour (caller can delete it). If the write
// fails, the original file is preserved.
//
// Caller MUST ensure Steam is not running before calling this.
func WriteShortcuts(path string, sf *ShortcutFile) error

// AddGame appends a ShortcutEntry to the ShortcutFile. It generates the
// deterministic AppID via GenerateAppID before adding.
//
// Returns ErrDuplicate if a shortcut with the same title already exists.
func AddGame(sf *ShortcutFile, entry *ShortcutEntry) error

// RemoveGame removes a shortcut by its AppID. Returns nil if the appid
// is not found (idempotent).
func RemoveGame(sf *ShortcutFile, appID uint32) error

// FindGame searches for a shortcut by title (case-insensitive, exact match).
// Returns nil if not found.
func FindGame(sf *ShortcutFile, title string) *ShortcutEntry

// FindGameByAppID searches for a shortcut by its generated AppID.
func FindGameByAppID(sf *ShortcutFile, appID uint32) *ShortcutEntry

// GenerateAppID computes the deterministic Steam AppID for a non-Steam game.
// Algorithm: uint32(crc32(append([]byte(exe), []byte(appname)...))) | 0x80000000
//
// The 0x80000000 mask ensures the ID appears in the non-Steam game range
// (high bit set), avoiding collisions with real Steam App IDs.
//
// exe and appname should be the same values stored in the shortcut entry.
func GenerateAppID(exe, appName string) uint32
```

**Critical implementation details:**

1. **Backup strategy:** Before `WriteShortcuts`, copy the existing file to `<path>.backup-<iso8601>`. This is essential because a malformed VDF write (e.g., data type mismatch) will cause Steam to silently wipe the file on next launch. The backup allows manual recovery.

2. **CRC32 implementation:** Use Go's `hash/crc32` with the IEEE table (`crc32.IEEETable`). No external dependency needed.

3. **Binary VDF schema:** The `wakeful-cloud/vdf` library handles binary VDF encode/decode. During deserialization, map the wire format into `ShortcutEntry` structs. Unknown fields should be preserved (round-tripped) via a `RawFields` map to avoid data loss.

4. **Tag format:** In binary VDF, tags are stored as repeated int32 keys in a sub-map (e.g., `{ "0" = "F95Zone", "1" = "RPGM" }`). The `Tags` slice in our struct maps cleanly to this. We always inject `"F95Zone"` as the first tag so users can filter in Steam's library search.

5. **Windows path handling:** On Linux, the `Exe` and `StartDir` fields should use Unix paths (e.g., `/home/user/Games/foo/game.exe`). Steam/Proton resolves these internally. On Windows, native Windows paths. The caller (main.go) is responsible for path format.

### 1.5 `grid.go` — Artwork Management

```go
package steam

import "image"

// GridFilePath builds the absolute path for a grid artwork file.
// Example: GridFilePath(steamRoot, 123456789, 0x81234567, ArtVertical)
//   → "/home/user/.steam/steam/userdata/123456789/config/grid/2166883687p.png"
func GridFilePath(steamRoot string, userID3 uint32, appID uint32, artType ArtType) string

// DownloadAndSetCover downloads the image from the given URL, resizes it to
// 600x900 (Steam vertical grid standard), and writes it as <appid>p.png
// into the grid directory.
//
// Uses a standard HTTP client with 30-second timeout. Supported formats:
// JPEG, PNG, GIF, WebP (via standard library + x/image decoder if needed).
//
// If coverURL is empty, returns nil (no-op) — caller should check.
func DownloadAndSetCover(steamRoot string, userID3 uint32, appID uint32, coverURL string) error

// DownloadAndSetHero downloads and resizes to 1920x620 for the hero banner.
func DownloadAndSetHero(steamRoot string, userID3 uint32, appID uint32, url string) error

// DownloadAndSetHorizontal downloads and resizes to 460x215 for the horizontal grid.
func DownloadAndSetHorizontal(steamRoot string, userID3 uint32, appID uint32, url string) error

// SetAllArtwork is a convenience function that downloads the cover image from
// the given URL and sets it for all three grid art types (vertical, horizontal,
// hero). The same source image is resized to each dimension.
//
// This is the recommended high-level function for the CLI.
func SetAllArtwork(steamRoot string, userID3 uint32, appID uint32, coverURL string) error

// EnsureGridDir creates the grid directory if it doesn't exist.
// Must be called before writing any grid artwork.
func EnsureGridDir(steamRoot string, userID3 uint32) error
```

**Image resize strategy:**

Recommended approach: use **Go standard library + `golang.org/x/image/draw`**.

- `image/jpeg`, `image/png` from stdlib handle decode/encode
- `golang.org/x/image/draw` provides the `draw.CatmullRom` scaler for high-quality resizing (Lanczos-like, better than bilinear default)
- No CGO needed — pure Go, cross-compilable
- Dependency: already indirect through `ncruces/go-sqlite3`, won't add new transitive deps

The resize algorithm is:
1. Decode source image (`image.Decode`)
2. Create new RGBA image at target dimensions
3. `draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)`
4. Encode as PNG to destination path

**Artwork source priority** (implemented in the CLI integration, not in this package):
1. **SteamGridDB by real Steam App ID** — if `StoreLinks["steam"]` exists and contains a Steam App ID, query SGDB for the best-match grid assets
2. **F95Zone CoverURL** — the scraped `ScrapedMeta.CoverURL` (already stored in DB)
3. **No artwork** — skip; Steam will show a blank grid entry

### 1.6 `proton.go` — Proton/Wine Compatibility Config (Linux Only)

```go
package steam

// SetProtonVersion writes a Proton compatibility mapping for the given AppID
// into Steam's config.vdf (text VDF format at <steamRoot>/config/config.vdf).
//
// This makes Steam run the non-Steam game through the specified Proton/Wine
// compatibility tool instead of its default (which would fail for Windows EXEs
// on Linux).
//
// Example: SetProtonVersion(steamRoot, appID, "proton_experimental")
//
// On non-Linux platforms, this returns ErrNotLinux.
func SetProtonVersion(steamRoot string, appID uint32, protonVersion string) error

// GetProtonVersion reads the current Proton mapping for the given AppID.
// Returns ("", nil) if no mapping exists.
//
// On non-Linux platforms, returns ErrNotLinux.
func GetProtonVersion(steamRoot string, appID uint32) (string, error)

// RemoveProtonVersion removes the Proton mapping for the given AppID.
// Returns nil if no mapping exists (idempotent).
func RemoveProtonVersion(steamRoot string, appID uint32) error

// ListProtonVersions scans Steam's compatibilitytools.d directory for
// installed Proton versions. Returns a list of version strings like
// ["proton_9", "proton_experimental", "GE-Proton9-7"].
//
// This is informational; the caller can present the list to the user.
//
// Scan locations:
//   - <steamRoot>/compatibilitytools.d/    (custom, e.g. GE-Proton)
//   - <steamRoot>/steamapps/common/Proton*/ (official)
//
// On non-Linux platforms, returns ErrNotLinux.
func ListProtonVersions(steamRoot string) ([]string, error)

// KnownProtonVersions is the default set of Proton version identifiers.
// Used as a fallback when ListProtonVersions can't scan.
var KnownProtonVersions = []string{
    "proton_9",
    "proton_experimental",
    "proton_hotfix",
    "proton_8",
    "proton_7",
}
```

**config.vdf format details:**

The file `~/.steam/steam/config/config.vdf` is a text VDF document. The `CompatToolMapping` block looks like:

```
"CompatToolMapping"
{
    "2166883687"    // our generated AppID (decimal)
    {
        "name"      "Game Name"
        "config"    ""
        "Priority"  "250"
    }
}
```

To set Proton for a specific AppID, we need to add/update `"<appid>/CompatToolMapping/<appid>/name"` to the proton tool name. The library `github.com/andygrunwald/vdf` handles text VDF parsing.

**Important note:** The actual Proton assignment also requires the `"CompatToolMapping"` block to be present. If it doesn't exist yet, we must create it. Additionally, each tool entry requires a `name`, `config`, and `Priority` key. The `config` is usually empty for non-Steam games.

**Integration with existing `play` command logic:**
The `play` command (`cmdPlay` in main.go) already has the heuristic:
```
.exe → wine (on Linux)
.sh → sh
.AppImage → direct
.x86_64 → direct
```

For Proton determination in the CLI integration:
- If game engine is Unity/RenPy/RPGM and ExePath ends in `.exe`, OR if the `play` command would use Wine → suggest Proton
- Default to `proton_experimental` unless the user specifies a version via flag
- On Windows, skip Proton configuration entirely

### 1.7 `steamgriddb.go` — SteamGridDB API Client (Deferrable)

```go
package steam

import (
    "net/http"
    "sync"
    "time"
)

// SGDBClient is a rate-limited HTTP client for the SteamGridDB v2 API.
type SGDBClient struct {
    apiKey     string
    http       *http.Client
    mu         sync.Mutex
    lastReq    time.Time
    minDelay   time.Duration // 1 second per official docs
}

// NewSGDBClient creates a new SteamGridDB API client.
// apiKey must be a valid SteamGridDB API key (free tier: 1 req/s, 200/day).
func NewSGDBClient(apiKey string) *SGDBClient

// SearchGame searches SteamGridDB for a game by name. Returns up to 10 results
// with their Steam App IDs and SGDB internal IDs.
func (c *SGDBClient) SearchGame(term string) ([]SGDBGameResult, error)

// GetGridsBySteamAppID fetches available grid images for a real Steam App ID,
// optionally filtered by dimensions (e.g., "600x900").
func (c *SGDBClient) GetGridsBySteamAppID(steamAppID int, dimensions string) ([]SGDBImageResult, error)

// DownloadImage downloads an image from the given URL and saves it to destPath.
// Creates parent directories if needed.
func (c *SGDBClient) DownloadImage(url, destPath string) error
```

**Rate limiting:** SteamGridDB free tier allows 1 req/s. The client enforces a 1050ms minimum delay between requests using the same mutex pattern as the existing `scraper.Client`.

**Deferral strategy:** This file is marked as "phase 2". It should be initially implemented as stubs that return `ErrNotImplemented` so the rest of the package compiles and the CLI can function with just F95Zone artwork. Phase 2 enables the `--steamgriddb-key` flag for premium artwork.

### 1.8 Error Sentinel Values

```go
package steam

import "errors"

var (
    ErrSteamNotFound    = errors.New("steam: installation not found")
    ErrSteamRunning     = errors.New("steam: Steam is running — close it before modifying shortcuts")
    ErrNoUsers          = errors.New("steam: no Steam users found under userdata/")
    ErrDuplicate        = errors.New("steam: a shortcut with this title already exists")
    ErrNotLinux         = errors.New("steam: Proton configuration is only available on Linux")
    ErrNotImplemented   = errors.New("steam: feature not yet implemented")
    ErrInvalidProton    = errors.New("steam: unknown Proton version")
)
```

---

## 2. Data Flow Diagram: `moxie steam add <game-id>`

```
┌─────────────────────────────────────────────────────────────────────┐
│                     moxie steam add 42                             │
└─────────────────────────────────────────────────────────────────────┘
         │
         ▼
┌──────────────────┐     ┌─────────────────────┐
│ 1. Open DB       │────▶│ db.GetGame(42)       │
│    (~/.config/   │     │ db.GetScrapedMeta(42) │
│     moxie/     │     └─────────┬─────────────┘
│     games.db)    │               │ game: { Title, ExePath, Engine, StoreLinks }
└──────────────────┘               │ meta: { CoverURL }
                                   ▼
┌─────────────────────────────────────────────────────────────────────┐
│ 2. Sanity Checks                                                     │
│    - game.ExePath must be set (warn if not, but proceed with Path)  │
│    - If ExePath doesn't exist on disk → warn, ask continue?         │
└─────────────────────────────────────────────────────────────────────┘
         │
         ▼
┌──────────────────┐
│ 3. steam.         │
│    FindSteamRoot()│──▶ ~/.steam/steam/  (or flatpak path)
│    IsSteamRunning()│──▶ if true → ERROR "close Steam first"
│    FindSteamUsers()│──▶ [123456789]     (could be multiple)
└──────┬───────────┘
       │
       ▼
┌──────────────────────────────────────────────────────────────────┐
│ 4. For each Steam user (or prompt if multiple):                   │
│                                                                    │
│    a. ResolveSteamPaths(root, userID3)                             │
│       → shortcuts.vdf path, grid/ dir, config.vdf path           │
│                                                                    │
│    b. ReadShortcuts(shortcuts.vdf path)                            │
│       → ShortcutFile{Shortcuts: [...]}                            │
│                                                                    │
│    c. Generate the ShortcutEntry:                                  │
│       - exe = game.ExePath  (the actual .exe on disk)             │
│       - StartDir = game.Path  (the game directory)                 │
│       - AppName = game.Title                                      │
│       - Tags = ["F95Zone"] + game.Tags[:3]  (cap at 4 total)     │
│       - AllowOverlay = true                                       │
│       - AllowDesktopConfig = true                                 │
│       - AppID = GenerateAppID(exe, AppName)                       │
│                                                                    │
│    d. AddGame(&shortcutFile, &entry)                               │
│       → checks for duplicate title, generates AppID               │
│                                                                    │
│    e. Download artwork:                                            │
│       coverURL = meta.CoverURL                                    │
│       storeLinks = threadData.StoreLinks                           │
│       if storeLinks["steam"] and steamAppID extractable:          │
│         → query SGDB for grids (deferred, fall through)           │
│       if coverURL != "":                                           │
│         → SetAllArtwork(root, userID3, appID, coverURL)           │
│                                                                    │
│    f. Proton setup (Linux only, if ExePath ends in .exe):         │
│       → SetProtonVersion(root, appID, "proton_experimental")      │
│                                                                    │
│    g. WriteShortcuts(shortcuts.vdf path, &shortcutFile)            │
│       → creates .backup-<timestamp> first                         │
│       → writes binary VDF                                          │
│                                                                    │
│    h. [Windows only] Skip proton step, set Windows paths           │
└──────────────────────────────────────────────────────────────────┘
       │
       ▼
┌──────────────────────────────────────────────────────────────────┐
│ 5. Print summary:                                                  │
│    ✓ Added "Game Title" to Steam library                          │
│      AppID: 0x81234567                                            │
│      User: 123456789                                              │
│      Proton: proton_experimental (Linux)                          │
│      Artwork: from F95Zone cover                                  │
│    ⚠ Restart Steam to see the game in your library.               │
└──────────────────────────────────────────────────────────────────┘
```

**Key integration points with existing code:**

| Existing Component | How Steam Package Uses It |
|---|---|
| `db.Database.GetGame(id)` | Retrieves `ExePath`, `Path`, `Title`, `Engine`, `StoreLinks` |
| `db.Database.GetScrapedMeta(id)` | Retrieves `CoverURL` |
| `scraper.ThreadData.StoreLinks` | Map with `"steam"`, `"itch"`, `"dlsite"` keys — if steam link exists, extract App ID |
| `main.go` `resolveExecutable()` | Reference for determining if a game needs Wine/Proton |
| `main.go` `launchCommand()` | Shows that `.exe` on Linux → `wine` wrapper — same logic drives Proton requirement |

**Steam App ID extraction from store link:**

```go
// In the CLI integration layer (main.go), not in the steam package:
func extractSteamAppID(storeLink string) (int, bool) {
    // URL: https://store.steampowered.com/app/12345/GameName/
    // Extract the numeric ID after /app/
    re := regexp.MustCompile(`/app/(\d+)/`)
    matches := re.FindStringSubmatch(storeLink)
    if len(matches) == 2 {
        id, err := strconv.Atoi(matches[1])
        return id, err == nil
    }
    return 0, false
}
```

---

## 3. Dependencies

### 3.1 New Go Modules to Add

| Module | Version | Purpose | File |
|---|---|---|---|
| `github.com/wakeful-cloud/vdf` ~~latest stable~~ **VENDORED** | Binary VDF read/write for shortcuts.vdf — replaced with vendored `internal/steam/vdf.go` due to unmaintained dependency (zero tagged releases since 2021) | `vdf.go` (vendored) |
| `github.com/andygrunwald/vdf` | latest stable | Text VDF parse/write for config.vdf (Proton) | `proton.go` |
| `golang.org/x/image` | latest | High-quality image resizing (CatmullRom scaler) | `grid.go` |

### 3.2 Standard Library Usage

| Package | Used In | Purpose |
|---|---|---|
| `hash/crc32` | `shortcuts.go` | Deterministic AppID generation |
| `image`, `image/png`, `image/jpeg` | `grid.go` | Decode/encode grid artwork |
| `image/draw` | `grid.go` | Basic resizing fallback |
| `net/http` | `grid.go`, `steamgriddb.go` | Download cover images, SGDB API calls |
| `os`, `path/filepath` | `steam.go`, all | Filesystem operations, path building |
| `sync` | `steamgriddb.go` | Rate limiting |
| `time` | `steamgriddb.go` | Rate limiting |
| `runtime` | `steam.go`, `proton.go` | Platform detection (`GOOS`) |
| `io`, `fmt`, `errors` | all | General purpose |

### 3.3 Dependency Justification

- **`wakeful-cloud/vdf`**: The only production-quality Go library for Valve's binary VDF format (used by Steam shortcuts.vdf). Alternatives: manual binary parsing (too fragile) or Valve's C++ code via CGO (non-portable). The library has a small surface area — we only use the binary VDF codec, not the text VDF parts.
- **`andygrunwald/vdf`**: Handles text-based VDF (config.vdf for Proton). Could be done with regex/manual parsing, but the nested structure makes the library worth it for correctness. Small, zero-dependency library.
- **`golang.org/x/image`**: Sub-repo of the Go project, well-maintained. The `CatmullRom` scaler produces output indistinguishable from Photoshop's "Bicubic" at 600×900. Without it, we'd use `draw.ApproxBiLinear` (standard library) which produces noticeably softer grids. The ~200KB additional binary size is acceptable.

### 3.4 Not Adding

| Proposed | Rejected Because |
|---|---|
| `nfnt/resize` | `golang.org/x/image` is the official Go sub-repo, same quality, no external dependency |
| `disintegration/imaging` | Too large an API surface; we only need crop-free resize to fixed dimensions |
| Any CGO-based image lib | Breaks cross-compilation; proton config is Linux-only but the rest should compile everywhere |
| `levigross/grequests` | Existing codebase uses raw `net/http` with custom transports — stay consistent |

---

## 4. Risk Assessment

### 4.1 CRITICAL: Corrupted shortcuts.vdf (Silent Data Loss)

**Scenario:** If `WriteShortcuts` writes a malformed binary VDF, Steam will **silently wipe the file** on next launch, destroying all user shortcuts (potentially hundreds of manually added games from other sources).

**Probability:** Medium (binary format edge cases, library bugs, struct mapping errors)

**Mitigation:**
1. **Always backup before write** — non-negotiable. Backup path: `<original>.backup-<iso8601>`
2. **Validate after write** — after writing, immediately `ReadShortcuts` the written file and compare entry count
3. **Atomic write** — write to a temp file, validate it, then `os.Rename` (Steam doesn't monitor inotify for this file, so atomic rename is safe)
4. **Write timeout guard** — if Steam starts during the write (race condition), the rename won't happen
5. **User-facing warning** — print "If your shortcuts disappear, restore from: <backup-path>" in the CLI output

**Recovery:** User can restore from the timestamped backup file if things go wrong.

### 4.2 HIGH: Steam Overwrites shortcuts.vdf

**Scenario:** User or system launcher starts Steam while `moxie steam add` is running, or Steam is already running.

**Probability:** Low (we check `IsSteamRunning()`), but a race exists: Steam could start between our check and the write.

**Mitigation:**
1. `IsSteamRunning()` check at the start — refuse to proceed if true
2. Atomic write with temp file reduces the race window to ~milliseconds
3. If the worst happens, the backup is available
4. Print "CLOSE STEAM FIRST" in bold red in the CLI

### 4.3 MEDIUM: AppID Collisions

**Scenario:** Two different games happen to produce the same CRC32 hash, resulting in the same AppID. Steam would show only one shortcut.

**Probability:** Low (CRC32 with `| 0x80000000` provides 2^31 namespace; birthday paradox: ~77,000 entries before 50% collision chance). F95Zone games are unlikely to exceed 1,000 per user.

**Mitigation:**
1. `AddGame` checks for duplicate AppID and appends a counter suffix to the AppName (e.g., "Game Title (2)") to produce a different CRC32
2. Log a warning to stderr if a collision is detected

### 4.4 MEDIUM: Windows Path Format Mismatch

**Scenario:** On Linux with Wine, the `Exe` field in shortcuts.vdf should use the Linux filesystem path (e.g., `/home/user/Games/foo/game.exe`), not a Wine-style `Z:\home\user\...` path. If we use Windows-style paths, Proton can't find the file.

**Investigation:** Steam stores non-Steam game paths in **host OS format** in shortcuts.vdf. Proton translates them internally. Confirmed by examining existing Linux Steam installations.

**Mitigation:**
1. Always store paths in the native OS format (which the scanner already provides)
2. `StartDir` follows the same principle — native OS path
3. No path translation layer needed in the steam package

### 4.5 LOW: Network Failure During Artwork Download

**Scenario:** The cover image URL is dead, or the user has no internet. `DownloadAndSetCover` fails, blocking the entire operation.

**Mitigation:** Artwork download is best-effort. If `SetAllArtwork` returns an error, we log a warning and continue with the shortcut addition. The game appears in Steam with the default blank grid (which Steam will attempt to fetch on its own later).

### 4.6 LOW: config.vdf Format Change

**Scenario:** Valve changes the config.vdf format for Proton compatibility tool mapping.

**Probability:** Low — the text VDF format has been stable for 10+ years. The `CompatToolMapping` structure hasn't changed since Proton was introduced.

**Mitigation:** The `andygrunwald/vdf` library provides structured parsing. If specific keys change, we can update the key constants. If the entire format changes, the library handles unknown blobs gracefully.

### 4.7 LOW: Steam Installed via Flatpak

**Scenario:** User's Steam is a Flatpak install at `~/.var/app/com.valvesoftware.Steam/.local/share/Steam/`. The standard `~/.steam/steam/` symlink may point here, but Flatpak apps have sandboxed filesystems.

**Mitigation:** `FindSteamRoot` checks both paths. If both exist, prefer the standard path (which is usually the Flatpak symlink target). Test with Flatpak Steam explicitly.

---

## 5. CLI Interface Design

### 5.1 Command Syntax

```
moxie steam <subcommand> [flags]
```

### 5.2 Subcommands

#### `moxie steam add <game-id>` — Add a game to Steam library

**Flags:**

| Flag | Type | Default | Description |
|---|---|---|---|
| `--user` | uint32 | auto-detect | Steam user ID3 (the numeric folder name under userdata/) |
| `--all-users` | bool | false | Add to all Steam user accounts found |
| `--proton` | string | "proton_experimental" | Proton version (Linux only); use "none" to skip |
| `--no-artwork` | bool | false | Skip downloading cover artwork |
| `--tags` | string | "F95Zone" | Additional comma-separated tags |
| `--exe` | string | from DB | Override the executable path |
| `--name` | string | from DB | Override the display name in Steam |

**Examples:**
```bash
# Basic: add game #42 to the default Steam user
moxie steam add 42

# Add to a specific Steam user (useful for shared machines)
moxie steam add 42 --user 123456789

# Add to all Steam accounts on this machine
moxie steam add 42 --all-users

# Use a specific Proton version
moxie steam add 42 --proton GE-Proton9-7

# Skip artwork (faster, for batch operations)
moxie steam add 42 --no-artwork

# Override the displayed name
moxie steam add 42 --name "My Custom Title"
```

**Output (success):**
```
⚙  Checking Steam... installed at ~/.steam/steam
⚠  Steam is running — please close Steam before continuing.
   (Press Enter after closing Steam, or Ctrl+C to cancel)

✓ Steam is closed.

📂 Game: "Summer Scent" (Unity, ID 42)
   Path: /home/user/Games/SummerScent-v1.0
   Exe:  /home/user/Games/SummerScent-v1.0/SummerScent.exe

🔍 Steam User: 123456789
   Backup: ~/.steam/steam/userdata/123456789/config/shortcuts.vdf.backup-20260502T143000Z

📝 Adding to shortcuts.vdf...
   AppID: 0x81234567
   Tags: [F95Zone, Unity]

🖼  Downloading cover art...
   Source: https://attachments.f95zone.to/2024/12/1234567_cover.png
   ✓ Vertical grid (600×900)
   ✓ Horizontal grid (460×215)
   ✓ Hero banner (1920×620)

🐧 Setting Proton: proton_experimental

✓ Added "Summer Scent" to Steam library
  AppID: 0x81234567
  Proton: proton_experimental

⚠  RESTART STEAM to see the game in your library.
   If shortcuts disappear, restore from:
   ~/.steam/steam/userdata/123456789/config/shortcuts.vdf.backup-20260502T143000Z
```

#### `moxie steam remove <game-id>` — Remove a game from Steam library

```bash
moxie steam remove 42
moxie steam remove 42 --user 123456789
moxie steam remove 42 --all-users
```

Matches by title in shortcuts.vdf (since AppID is deterministic from exe+title, we can compute and match).

#### `moxie steam list` — List non-Steam games added by moxie

```bash
moxie steam list
moxie steam list --user 123456789
```

Reads shortcuts.vdf and shows only entries tagged with "F95Zone".

#### `moxie steam proton-list` — List available Proton versions

```bash
moxie steam proton-list
```

Scans compatibilitytools.d and shows installed Proton versions.

#### `moxie steam proton-set <game-id> --version <proton>` — Change Proton version

```bash
moxie steam proton-set 42 --version GE-Proton9-7
```

Updates the Proton mapping for an already-added game.

#### `moxie steam fix-artwork <game-id>` — Re-download/refresh artwork

```bash
moxie steam fix-artwork 42
moxie steam fix-artwork 42 --steamgriddb-key <key>  # use SGDB if available
```

### 5.3 Integration in `main.go`

The existing `switch` in `main()` gains:

```go
case "steam":
    cmdSteam(os.Args[2:])
```

The `cmdSteam` function dispatches subcommands:

```go
func cmdSteam(args []string) {
    if len(args) < 1 {
        fmt.Fprintf(os.Stderr, "Usage: moxie steam <add|remove|list|proton-list|proton-set|fix-artwork>\n")
        os.Exit(1)
    }
    switch args[0] {
    case "add":          cmdSteamAdd(args[1:])
    case "remove":       cmdSteamRemove(args[1:])
    case "list":         cmdSteamList(args[1:])
    case "proton-list":  cmdSteamProtonList(args[1:])
    case "proton-set":   cmdSteamProtonSet(args[1:])
    case "fix-artwork":  cmdSteamFixArtwork(args[1:])
    default:
        fmt.Fprintf(os.Stderr, "Unknown steam subcommand: %s\n", args[0])
        os.Exit(1)
    }
}
```

### 5.4 Required DB Schema Changes

The `games` table needs a new column to track which games have been added to Steam (so we can avoid duplicates and support `remove`/`list`):

```sql
ALTER TABLE games ADD COLUMN steam_appid INTEGER;
ALTER TABLE games ADD COLUMN steam_user TEXT;   -- comma-separated user IDs or JSON array
```

Or, more cleanly, a separate table:

```sql
CREATE TABLE IF NOT EXISTS steam_shortcuts (
    game_id    INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    steam_user INTEGER NOT NULL,  -- Steam ID3
    steam_appid INTEGER NOT NULL, -- Our generated AppID
    proton_version TEXT,           -- e.g., "proton_experimental"
    added_at   TEXT DEFAULT (datetime('now')),
    PRIMARY KEY (game_id, steam_user)
);
```

This is **optional** — the CLI can work without it by reading shortcuts.vdf on every operation, but having a DB record enables:
- `steam remove <id>` without needing the title match
- `steam list` without parsing shortcuts.vdf
- Preventing double-adds

Recommendation: **defer the DB table** to phase 2. For phase 1, read shortcuts.vdf directly for all queries.

---

## 6. Testing Strategy

### 6.1 Unit Tests (No Steam Required)

These tests use **fixture files** stored in `internal/steam/testdata/`:

| Test | File(s) | What It Validates |
|---|---|---|
| `TestGenerateAppID` | None (pure computation) | CRC32 produces expected values, high bit is set, same inputs → same output |
| `TestReadShortcuts` | `testdata/shortcuts_valid.vdf` | Binary VDF parses correctly into `ShortcutFile` |
| `TestReadShortcuts_Empty` | `testdata/shortcuts_empty.vdf` | Empty file returns empty `ShortcutFile` |
| `TestReadShortcuts_Missing` | N/A (simulated) | Non-existent file returns empty (not error) |
| `TestWriteShortcuts_RoundTrip` | Temp file | Write → Read produces identical content |
| `TestAddGame` | In-memory ShortcutFile | AppID generated, entry appended, duplicate detection |
| `TestRemoveGame` | In-memory ShortcutFile | Remove by AppID, idempotent on missing |
| `TestFindGame` | In-memory ShortcutFile | Case-insensitive title match |
| `TestGenerateAppID_NoCollisions` | Computed | 10,000 unique inputs produce unique AppIDs |
| `TestGridFilePath` | None (pure path building) | Correct suffix, correct dimensions |
| `TestExtractSteamAppID` | None (regex test) | Parse Steam URL patterns correctly |
| `TestParseConfigVDF` | `testdata/config_valid.vdf` | Text VDF with CompatToolMapping parses |
| `TestProtonSet_NewMapping` | Temp config.vdf | Adds mapping when none exists |
| `TestProtonSet_UpdateMapping` | Temp config.vdf | Updates existing mapping |
| `TestFindSteamRoot_Linux` | Mocked HOME/XDG | Finds standard and Flatpak paths |
| `TestFindSteamUsers` | `testdata/userdata/` | Finds numeric directories, ignores non-numeric |
| `TestIsSteamRunning` | Mocked /proc check | Returns true/false based on mock fs |

### 6.2 Fixture File Generation

To create binary VDF fixtures without a Steam installation, use a Go test helper:

```go
// In shortcuts_test.go
func generateTestShortcutsVDF(t *testing.T, entries []ShortcutEntry) []byte {
    sf := &ShortcutFile{Shortcuts: entries}
    data, err := vdf.Marshal(sf)  // using wakeful-cloud/vdf encoder
    require.NoError(t, err)
    return data
}
```

This lets us programmatically generate valid binary VDF test data for edge cases:
- Zero entries
- Many entries (100+)
- Special characters in AppName
- Unicode paths in Exe/StartDir
- Entries with all optional fields populated
- Malformed entries (negative tests — we verify graceful handling)

For text VDF fixtures (`config.vdf`), hand-craft valid and edge-case files:
```
testdata/
├── config_empty.vdf
├── config_no_compat.vdf
├── config_with_compat.vdf
├── config_multiple_compat.vdf
├── shortcuts_empty.vdf
├── shortcuts_single.vdf
├── shortcuts_many.vdf
└── userdata/
    ├── 123456789/
    │   └── config/
    │       └── shortcuts.vdf  → symlink to ../shortcuts_single.vdf
    └── notauser/               → should be ignored
```

### 6.3 Integration Tests (Requires Docker or Test Steam)

For CI, create a Docker-based test that:
1. Spawns a container with the Steam runtime installed (or a mock directory structure)
2. Runs `moxie steam add <test-game>` against mock fixtures
3. Validates shortcuts.vdf was modified correctly

For local testing, provide a `testdata/setup.sh` that creates a minimal `.steam` directory structure:
```bash
# Creates:
# ~/.steam/steam/userdata/999999999/config/
# ~/.steam/steam/userdata/999999999/config/shortcuts.vdf  (empty)
# ~/.steam/steam/userdata/999999999/config/grid/
# ~/.steam/steam/config/config.vdf  (minimal)
```

### 6.4 Manual Testing Checklist

| Test Scenario | Expected Result |
|---|---|
| Steam running → `steam add` | Error "close Steam first" |
| Steam closed → `steam add` | Success, game appears after restart |
| `steam add` twice (same game) | Second call warns "already added" |
| `steam remove` | Game disappears from shortcuts.vdf |
| Invalid proton version | Error "unknown Proton version" |
| Steam restart → grid artwork | Cover image displayed in library |
| Launch game from Steam | Proton/Wine launches correctly |
| Flatpak Steam | Paths resolve to Flatpak location |

### 6.5 Test Coverage Targets

- Core logic (`shortcuts.go`, `steam.go`, `grid.go`, `proton.go`): **90%+** line coverage
- `steamgriddb.go`: **70%+** (deferred, many external calls)
- Integration layer in `main.go`: tested via table-driven "happy path" tests with mock DB + mock filesystem

---

## 7. Implementation Phases

### Phase 1 — Core (MVP)
- `types.go` — all type definitions
- `steam.go` — FindSteamRoot, FindSteamUsers, ResolveSteamPaths, IsSteamRunning
- `shortcuts.go` — Read, Write, Add, Remove, Find, GenerateAppID (with backup logic)
- `grid.go` — DownloadAndSetCover, SetAllArtwork (using F95Zone CoverURL only)
- `proton.go` — SetProtonVersion, GetProtonVersion
- `main.go` — `steam add`, `steam remove`, `steam list` subcommands
- Unit tests with fixtures

### Phase 2 — Enhancement
- `steamgriddb.go` — full SGDB client
- Artwork source priority: SGDB by real Steam App ID → F95Zone CoverURL
- `steam fix-artwork` subcommand
- `steam proton-list`, `steam proton-set` subcommands
- `steam_shortcuts` DB table for tracking
- Integration tests with Docker

### Phase 3 — Polish
- `--all-users` flag implementation
- Batch operations (`steam add --batch game1 game2`)
- TUI integration (add "Send to Steam" option in game detail view)
- Windows/macOS path testing (CI matrix)
- Artwork caching (don't re-download same URL)

---

## 8. Summary of Design Decisions

| Decision | Rationale |
|---|---|
| Binary VDF library (`wakeful-cloud/vdf`) vs manual parsing | VDF binary format is non-trivial; a tested library avoids silent data corruption |
| Separate `andygrunwald/vdf` for text VDF | Text and binary VDF are fundamentally different formats; one library handles each well |
| `golang.org/x/image` for resizing | Official Go sub-repo, no new external dependency chain, CatmullRom quality |
| Backup before every write | Steam silently wipes malformed VDF — backup is our only safety net |
| Atomic write (temp file + rename) | Minimizes race window vs Steam startup |
| `IsSteamRunning()` check mandatory | Prevents the most common failure mode |
| Artwork is best-effort | Network failures shouldn't block adding the shortcut |
| Proton configuration Linux-only | Windows/macOS don't need it — `proton.go` returns `ErrNotLinux` on other platforms |
| No DB table for tracking (Phase 1) | Shortcuts.vdf is the source of truth; DB tracking can be added later without API changes |
| CRC32 for AppID (not hash/fnv) | CRC32 matches Steam's own documented algorithm for non-Steam game IDs |

---

**End of Design Document**

This design is ready for review. No code has been written — this is a pure architecture document. Once approved, implementation can begin from `internal/steam/types.go` outward.
