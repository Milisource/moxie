# Scanner

## What

The scanner walks a directory tree, identifies which subdirectories are games, detects the engine for each one, and computes total size and executable path. It lives in `internal/scanner/scanner.go` (one file, ~250 lines) and calls into `internal/engine/detector.go` for engine identification.

## How

### Directory Walk

`scanner.Scan(root)` calls `filepath.WalkDir` on the root directory. For each visited directory:

1. **Skip excluded dirs** — checks `shouldSkip(name)` against a hardcoded list of names like `unins`, `python`, `dxsetup`, `config`, `saved`, `logs`. These catch installer remnants, redistributables, and common false positives.

2. **Skip __MACOSX** — macOS resource fork directories are skipped entirely.

3. **Skip already-detected game subdirectories** — once a directory is identified as a game root, its children are not walked. This prevents detecting engine subdirectories (like a `renpy/` folder inside a Ren'Py game) as separate games.

4. **Check if it's a game root** — `looksLikeGameRoot(path)` via `hasGameMarkers(dir)` checks for the presence of executables (`.exe`, `.sh`, `.AppImage`, `.x86_64`, `.x86`) or engine markers (`renpy/`, `www/`, `_Data/`, `package.json`, `.rpyc`, `.rpa`, `Game.rgss*`).

5. **Check for category directories** — if the directory name matches a known engine (`Unity`, `Ren'Py`, `RPGM`, `HTML`, etc.) **and** it contains subdirectories that look like games, it's treated as a category folder (not a game itself). The walk continues into its children.

### Engine Detection

On the detected game directory, `engine.Detect(dir)` is called. It reads the directory listing once and checks profiles in priority order. The profiles are a combination of:
- **Built-in profiles** — 14 canonical engines + 3 community engines (see engine table below)
- **Custom profiles** — loaded from `~/.config/moxie/engines/*.json` drop-in files, allowing users to add patterns for niche engines or override built-in profiles by name

Custom profiles are loaded from `config.EngineProfilesDir()` (which returns `~/.config/moxie/engines/`). Each JSON file defines:
```json
{
  "name": "MyEngine",
  "engine": "Unity",         // maps to one of the 15 canonical engine values
  "confidence": 80,           // 0-100, divided by 100 for internal usage
  "subdirs": ["game"],        // required subdirectories
  "filenames": ["game.exe"],  // required files
  "extensions": [".exe"]      // required extensions
}
```
At least one detection criterion is required. Custom profiles can override built-in profiles by name (e.g., providing a higher-confidence Unity detection pattern).

### Version Extraction

`ExtractVersion(name)` extracts a version string from a directory or file name using regex patterns tried in priority order:

1. **Date** — `\d{4}-\d{2}-\d{2}` (e.g. `2025-11-14`, `Game-2025-11-14`)
2. **Compact date** — `YYYYMMDD` without separators (e.g. `Data20260403`, `Game-20260403`). Uses `\D` boundary so dates attached to words are matched. Year/month/day validation prevents false positives on arbitrary 8-digit numbers.
3. **Dot-separated** — `[vV]?[a-zA-Z]?\d+\.\d+(?:\.\d+)*` with optional trailing build letter (e.g. `v1.0.3`, `1.0`, `V5.4.91`, `v0.7.7i`)
4. **Dash/underscore** — `[vV]?\d+(?:[._-]\d+)+` converted to dots (e.g. `v1-0-3` → `1.0.3`, `1_0_0` → `1.0.0`)
5. **Underscore-only fallback** — `[vV]?\d+_\d+(?:_\d+)*` converted to dots (e.g. `v1_0_3` → `1.0.3`)
6. **Single/double-digit** — `[vV]\d{1,2}` (e.g. `v5`, `v01`, `v0`)

When the directory name yields no version, the scanner escalates through additional fallbacks:

- **File contents** (`ExtractVersionFromDir`) — checks known files inside the game directory:
  - `Game.ini` (RPG Maker) — parses the `Title=` line, normalizes common version prefixes (`ver` → `v`, `version` → `v`) and applies the same regex patterns
  - `package.json` (HTML/NW.js) — reads the `"version"` field
  - `game/options.rpy` (Ren'Py) — reads `config.version`
- **Parent directory name** — many games are nested (e.g. `Game v1.0/Game Windows/Game.exe`), so the scanner checks the parent dir for version when the game dir itself has none
- **Executable filename** — some games only have the version in the executable name (e.g. `[Full]EmberDoors_v0.1.7_Linux.x86_64` → `0.1.7`)

**Boundary handling:** Go's regex `\b` treats `_` as a word character. Since most F95Zone game directories use underscores around versions (`FullEmberDoors_v0.1.7_Linux`), the patterns use explicit `(?:^|[^a-zA-Z0-9])` / `(?:$|[^a-zA-Z0-9])` instead of `\b` to prevent underscore-delimited versions from being missed.

**Known limitations:**
- Versions without a non-alphanumeric delimiter before `v`/`V` are missed (e.g. `WINv01` — `N` and `v` are adjacent letters)
- Trailing build letters are kept in the extracted version (`v0.7.7i` → `"0.7.7i"`)
- No semver parsing — `NormalizeVersion()` in the sync code handles comparison

### Progress Reporting

`ScanFiltered(root, skipPaths, progressFn)` accepts an optional `ScanProgressFunc` callback:
```go
type ScanProgressFunc func(dirsExamined, gamesFound int)
```

The CLI calls this to display live progress on stderr using `\r` (carriage return) for in-place updates, showing directories examined and games found so far.

### Size Calculation

`dirSize(dir)` does a secondary `WalkDir` over the game directory to sum all file sizes. This is independent of the primary scan walk.

### Executable Discovery

`findGameExe(dir)` scans for the largest executable (`.exe`, `.sh`, `.x86_64`, `.x86`) after filtering out crash handlers, uninstallers, and setup utilities. On Linux this also finds AppImage files.

## Why Pattern Matching Over Binary Parsing

PE/ELF binary scanning would be more precise but is 10x more complex. Pattern matching covers the vast majority of adult games: Unity files have predictable layouts (`UnityPlayer.dll`, `_Data/`), Ren'Py games always have a `renpy/` directory, RPG Maker games ship with `Game.rgss*` archives. The approach hits 95%+ accuracy with zero CGO dependencies and trivial cross-compilation.

## Engine Detection Profiles

### Canonical (14 engines)

| Engine | Signal | Confidence |
|---|---|---|
| **Unity** | `UnityPlayer.dll` | 0.98 |
| | `UnityCrashHandler*.exe` | 0.95 |
| | `globalgamemanagers` / `data.unity3d` | 0.90 |
| | `<exe>_Data/` folder matching an .exe | 0.93 |
| **Ren'Py** | `renpy/` directory | 0.98 |
| | `.rpyc` / `.rpa` files + `game/` | 0.85 |
| **RPGM** | `www/` + `package.json` (MV/MZ disambiguated) | 0.95 |
| | `Game.rgss3a` (VX Ace) | 0.93 |
| | `Game.rgss2a` (VX) | 0.90 |
| | `Game.rgssad` (XP) | 0.88 |
| | `Game.exe` + `Game.ini` + `Data/` | 0.75 |
| | `Game.ini` only | 0.65 |
| **Unreal Engine** | `Engine/` directory | 0.92 |
| **WebGL** | `index.html` + `Build/` | 0.75 |
| **HTML** | `index.html` | 0.70 |
| | `.html` files | 0.60 |
| **Java** | `.jar` files | 0.90 |
| **Flash** | `.swf` files | 0.90 |
| **WolfRPG** | `WolfRPG.exe` + `Game.ini` + `Data/` | 0.90 |
| | `.wolf` files | 0.80 |
| **QSP** | `.qsp` / `.qsps` files | 0.90 |
| | `qspgui.exe` / `quest.exe` | 0.85 |
| **ADRIFT** | `.taf` files | 0.90 |
| | `adrift.exe` / `ADRIFT.exe` | 0.80 |
| **RAGS** | `RAGS.exe` / `RAGS Player.exe` | 0.85 |
| **Tads** | `.gam` / `.t3` files | 0.90 |

### Community (3 engines → Others)

| Detected As | Actual Engine | Signal | Confidence |
|---|---|---|---|
| Others | Godot | `.pck` files | 0.85 |
| | Electron / nw.js | `resources.pak` + `package.json` | 0.80 |
| | M.U.G.E.N. | 3+ of `{chars, data, stages, font, sound}` dirs | 0.92 |

### Custom Profiles

Users can add custom engine detection patterns by placing JSON files in `~/.config/moxie/engines/`. Each file can define new profiles or override built-in ones. Custom profiles are validated on load (name required, at least one detection criterion, confidence 0-100).

### Known Limitations

- **False positives** from tool directories (RPG Maker editor, Unity Editor) and generic folder names (`Linux`, `output`, `PC_Version`). Handled via the exclusion list, but edge cases leak through.
- **No archive scanning** — `.zip`/`.rar`/`.7z` files at the scan root are ignored. Only directories are walked. Packed games won't be detected.
- **Duplicate entries** — the same game in multiple paths (extracted + archived copy; top-level + category subdirectory) creates duplicate records. No content-based dedup.
- **Non-UTF-8 filenames** — Latin1/Shift-JIS encoded directory names display incorrectly in the TUI. A charset detection + conversion step could normalize them.
