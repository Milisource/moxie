# Game Engine Detection — Profile-Based Detection

Lives in `internal/engine/` (files: detector.go, engine_tags.go + tests). Detects game engines from directory contents using priority-ordered profiles.

## Detection Profiles

The `Detect(dir)` function reads a directory listing once and checks it against all profiles in priority order. First match wins.

Profile structure:
```go
type profile struct {
    engine     Engine
    confidence float64 // 0.0 - 1.0
    subdirs    []string // at least one must exist
    files      []string // must exist in directory
    extensions []string // file extensions to check
    name       string   // human-readable rule name
}
```

## Supported Engines

| Engine | Type | Key Signals |
|--------|------|-------------|
| Ren'Py | Canonical | `renpy/` dir, `.rpyc`/`.rpa` files, `game/` dir |
| Unity | Canonical | `UnityPlayer.dll`, `*_Data/` folder + matching exe, `globalgamemanagers` |
| RPG Maker (all) | Canonical | `icudtl.dat`+`Game.exe` (MV/MZ), `Game.rgss3a` (VX Ace), `Game.rgss2a` (VX), `Game.rgssad` (XP), `www/`+`package.json`, `Game.ini`+`Data/` |
| HTML | Canonical | `index.html`, `.html` files |
| Flash | Canonical | `.swf` files |
| Java | Canonical | `.jar` files |
| Unreal Engine | Canonical | `Engine/` directory |
| WebGL | Canonical | `index.html` + `Build/` directory |
| WolfRPG | Canonical | `WolfRPG.exe`, `.wolf` files |
| QSP | Canonical | `.qsp`/`.qsps` files, `qspgui.exe` |
| ADRIFT | Canonical | `.taf` files, `adrift.exe` |
| RAGS | Canonical | `RAGS.exe` |
| TADS | Canonical | `.gam`/`.t3` files |
| Others | Community | `.pck` (Godot), `resources.pak`+`package.json` (Electron), M.U.G.E.N. dirs |

## Adding a New Engine

1. Add `Engine` const in `detector.go` (canonical or `Others`).
2. Add detection profile(s) in `profiles` slice at appropriate priority.
3. If community engine → map to `Others` Engine type.
4. Add distinct color in `internal/tui/styles.go:engineColor()`.
5. Add to `AllEngines()` list in `detector.go`.
6. Add to SQLite CHECK constraint in `internal/db/db.go`.
7. Add engine tag pattern to `internal/engine/engine_tags.go` (maps engine names to F95Zone thread tags for association).
8. Update `docs/scanner.md` engine table.

## Priority Guidelines

- **0.90–0.98:** Definitive signals (e.g., `UnityPlayer.dll` at 0.98, Ren'Py `renpy/` folder at 0.98)
- **0.85–0.89:** Strong signals (multiple indicators, e.g., `icudtl.dat` + `Game.exe` at 0.96)
- **0.70–0.84:** Moderate signals (e.g., `index.html` + `Build/` for WebGL at 0.75)
- **0.50–0.69:** Weak signals (single file, needs confirmation — e.g., lone `Game.ini` at 0.65 triggers `checkRPGMakerINI()`)

Place higher-confidence profiles first within each engine group. The 0.65 RPGM `Game.ini` match triggers `checkRPGMakerINI()` for content-based confirmation (reads `Game.ini` for `RGSS*` markers).

## Special Detection Logic

- **Unity `_Data` folder:** `detectUnityDataFolder()` matches `<exe>_Data/` with corresponding `.exe`
- **RPG Maker variants:** `checkRPGMakerPackage()` reads `www/package.json` for "RPGMV"/"RPGMZ" markers; `checkRPGMakerINI()` reads `Game.ini` for `RGSS*` markers
- **M.U.G.E.N.:** Requires 3 of 5 directories (chars, data, stages, font, sound)
- **Subdirectory extension search:** If extensions aren't found in root, checks listed subdirs

## Engine Tags

`engine_tags.go` maps canonical engine names to F95Zone thread tags. Used during auto-association to filter search results — if a thread's tags include "Ren'Py", only games detected as Ren'Py are matched against it, and vice versa.

## Testing

`detector_test.go` (31 tests) uses table-driven patterns:
- Temp directories populated with specific files/dirs matching profiles
- Edge cases: empty dirs, mixed signals, ambiguous matches
- `t.Parallel()` on all test functions
