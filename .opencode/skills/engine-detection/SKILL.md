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
    filesAll   bool     // files must ALL exist (default: ANY match)
    extensions []string // file extensions to check
    name       string   // human-readable rule name
}
```

**`files` is an ANY-match by default.** When a profile lists several files as a single signature, set `filesAll: true` — otherwise the weakest member matches alone. (The `icudtl.dat`/RPGM bug: `icudtl.dat` ships with every Chromium/Electron app, so requiring `icudtl.dat` + `nw.dll` together is what distinguishes NW.js/RPG Maker from Electron.)

## Canonical vs Others

The canonical engine set is exactly F95Zone's engine taxonomy (15 names in `internal/scraper/latestapi.go`). Engines outside it **must map to `Others`** with a descriptive `MatchedBy` — do not add new `Engine` constants or DB CHECK values for them. Non-F95 profiles are placed *before* weaker canonical profiles so they can't leak (e.g. KiriKiri `.xp3` before HTML).

## Supported Engines

| Engine | Type | Key Signals |
|--------|------|-------------|
| Ren'Py | Canonical | `renpy/` dir, `.rpyc`/`.rpa` files, `game/` dir |
| Unity | Canonical | `UnityPlayer.dll`, `*_Data/` folder + matching launcher (`.exe`/`.x86_64`/`.sh`), `globalgamemanagers` |
| RPG Maker (all) | Canonical | `icudtl.dat` **+ `nw.dll`** (MV/MZ NW.js; `filesAll`), `Game.rgss3a` (VX Ace), `Game.rgss2a` (VX), `Game.rgssad` (XP), `www/`+`package.json`, `Game.ini`+`Data/` |
| HTML | Canonical | `index.html`, `.html` files — the *scanner gate* additionally requires an HTML game signature (see below) |
| Flash | Canonical | `.swf` files |
| Java | Canonical | `.jar` files |
| Unreal Engine | Canonical | `Engine/` directory |
| WebGL | Canonical | `index.html` + `Build/` directory |
| WolfRPG | Canonical | `WolfRPG.exe`, `.wolf` files |
| QSP | Canonical | `.qsp`/`.qsps` files, `qspgui.exe` |
| ADRIFT | Canonical | `.taf` files, `adrift.exe` |
| RAGS | Canonical | `RAGS.exe` |
| TADS | Canonical | `.gam`/`.t3` files |
| Others | Non-F95 | `.pck` (Godot), Chromium/Electron (`chrome_100_percent.pak`/`LICENSE.electron.txt`), GameMaker (`data.win`), KiriKiri (`.xp3`/`krkr.console.log`), NScripter (`nscript.dat`/`.nsa`), HSP (`hspext.dll`/`.hpi`), Flutter (`flutter_windows.dll`), `resources.pak`+`package.json`, M.U.G.E.N. dirs |

## Scanner Gate vs Detector

`engine.Detect` classifies a directory; the **scanner** decides whether a directory is a game root at all (`internal/scanner/scanner.go`: `isGameRoot`). They must agree. The gate adds:
- non-executable formats (`.swf`, `.jar`, `.qsp`, `.taf`, `.gam`, `.t3`, `.wolf`, `data.win`, ...)
- an **HTML content sniff**: a root-level `.html` is read (bounded head+tail) and must contain a game signature (`tw-storydata`, SugarCube/Harlowe/Snowman, `<canvas`, PixiJS, Phaser, CreateJS, RPG Maker web, Unity WebGL, `gamefiles`, `js/engine/`). Bare `index.html` is not enough.
- **container detection**: a dir with ≥2 complete games is not registered; its children are.
- **release-wrapper collapse**: a non-game dir holding exactly one name-matching game registers the wrapper and detects into the child.

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
- **0.85–0.89:** Strong signals (multiple indicators, e.g., `icudtl.dat` + `nw.dll` at 0.96)
- **0.70–0.84:** Moderate signals (e.g., `index.html` + `Build/` for WebGL at 0.75)
- **0.50–0.69:** Weak signals (single file, needs confirmation — e.g., lone `Game.ini` at 0.65 triggers `checkRPGMakerINI()`)

Place higher-confidence profiles first within each engine group. The 0.65 RPGM `Game.ini` match triggers `checkRPGMakerINI()` for content-based confirmation (reads `Game.ini` for `RGSS*` markers).

## Special Detection Logic

- **Unity `_Data` folder:** `detectUnityDataFolder()` matches `<exe>_Data/` with the corresponding launcher (`.exe`/`.x86_64`/`.x86`/`.sh`/`.app`)
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
