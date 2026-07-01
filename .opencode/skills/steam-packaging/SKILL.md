# Steam Integration — VDF, Artwork, AppID, Proton

Lives in `internal/steam/` (files: steam.go, shortcuts.go, proton.go, grid.go, steamgriddb.go, appid.go, types.go + tests). Manages Steam shortcuts via VDF manipulation, grid artwork, and Proton configuration.

## Key Concepts

- **Shortcuts VDF:** `~/.steam/steam/userdata/<id3>/config/shortcuts.vdf` — binary VDF format
- **Grid directory:** `~/.steam/steam/userdata/<id3>/config/grid/` — custom artwork images
- **Config VDF:** `~/.steam/steam/config/config.vdf` — Steam-wide settings
- **AppID:** Deterministic unsigned 32-bit integer generated in `appid.go` (hash-based, not a real Steam AppID). Uses `crc32` or similar deterministic hash of the game title.
- **SteamGridDB:** External API for high-quality artwork (free API key required)

## Steam Detection

`FindSteamRoot()` scans platform-specific paths (see `steam.go`):

| Platform | Paths |
|----------|-------|
| Linux | `~/.steam/steam`, `~/.var/app/com.valvesoftware.Steam/.../Steam` |
| macOS | `~/Library/Application Support/Steam` |
| Windows | `%ProgramFiles(x86)%/Steam` |

`FindSteamUsers()` reads `userdata/` for numeric directory names (Steam ID3 values), validates by checking for `config/` subdirectory.

`ResolveSteamPaths()` builds the `SteamPaths` struct (defined in `types.go`).

## VDF Shortcuts Management

`shortcuts.go` handles the `shortcuts.vdf` file:

- **Read:** `ReadShortcuts(path)` → `[]ShortcutEntry` — parses binary VDF via `andygrunwald/vdf`
- **Write:** `WriteShortcuts(path, entries)` — atomic write to temp file + rename, with `.bak` backup
- **Round-trip safety:** Unknown fields preserved via VDF library's RawFields

`ShortcutEntry` struct:
```go
type ShortcutEntry struct {
    AppID       uint32
    AppName     string
    Exe         string
    StartDir    string
    Icon        string
    ShortcutPath string
    LaunchOptions string
    IsHidden    bool
    AllowDesktopConfig bool
    AllowOverlay bool
    OpenVR      bool
    Devkit      bool
    DevkitGameID string
    LastPlayTime uint32
    FlatpakAppID string
    Tags        []string
}
```

## AppID Generation

`appid.go` implements deterministic AppID generation. Given the same game title, it always produces the same AppID. This keeps Steam shortcuts stable across re-imports. The hash function is lossy (32-bit) so collisions are possible but unlikely.

## Artwork Pipeline

`grid.go` manages Steam grid artwork:

1. Download cover from F95Zone or SteamGridDB
2. Resize to Steam grid dimensions (920×430 for horizontal, 600×900 for vertical)
3. Save as `<appid>p.png` (horizontal), `<appid>_hero.png`, `<appid>_logo.png`

`steamgriddb.go` implements the SteamGridDB API client:
- `SearchGridArtwork(title)` → finds matching artwork
- `DownloadGridArtwork(url)` → downloads and resizes

## Proton Configuration

`proton.go` manages per-game Proton version overrides via `config.vdf`:

```go
// SteamAutoCompatConfig section in config.vdf
"SteamAutoCompatConfig" {
    "<appid>" { "ProtonVersion" "5.0" }
}
```

Available Proton versions listed via:
- `proton-list` command: checks `~/.steam/steam/compatibilitytools.d/` for custom Proton versions
- Default Proton: checked in `config.vdf` under `CompatToolMapping`

## Adding New Steam Features

1. Create handler in `internal/commands/steam_<feature>.go`
2. Add dispatch in `internal/commands/steam.go`
3. Implement logic in `internal/steam/` domain package
4. Register command in `main.go` switch
5. Update `docs/steam-package-design.md`

## Known Caveats

- Steam must NOT be running when modifying shortcuts.vdf (changes will be overwritten)
- Deterministic AppID collisions are possible but unlikely for game titles
- SteamGridDB requires a free API key (`moxie config set steamgriddb-key <key>`)
- Flatpak Steam paths differ from native Steam paths
- Writing config.vdf requires careful backup handling (shared file with Steam)
