# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Scanner version extraction from files** — `ExtractVersionFromDir` reads `Game.ini` `Title=` field (RPG Maker), `package.json` `"version"` field (HTML/NW.js), and `game/options.rpy` `config.version` (Ren'Py) when directory name has no version (F95-kq8)
- **Parent directory name fallback** — scanner now checks parent directory name for version when game dir has none, catching nested game structures like `Game v1.0/Game Windows/Game.exe` (F95-kq8)
- **Executable filename version extraction** — scanner checks exe names for embedded versions like `[Full]EmberDoors_v0.1.7_Linux.x86_64` → `0.1.7` (F95-kq8)
- **Compact YYYYMMDD date pattern** — `Data20260403` detected as valid date version with month/day validation (F95-kq8)
- **Single/double-digit version pattern** — `v5`, `v01`, `v0` now detected from directory names (F95-kq8)
- **Trailing build letter support** — `v0.7.7i` captured as `"0.7.7i"` instead of missed (F95-kq8)
- **Expanded bracketed-title version extraction** — per F95Zone title format rules: supports `[YYYY-MM-DD]`, bare `[X.Y]`, `[Final]`, `[Ch. 2 v3.0]`, and `[v1.0 Alpha]` patterns (F95-kq8)

### Fixed

- **TUI 🔄 update indicator on empty versions** — required `Version != ""` guard prevents false update markers on every game with scraped metadata but no local version (F95-kq8)
- **Scan now updates existing games** — `moxie scan` updates version/engine/size/exe on re-scan instead of skipping, so improved detection takes effect immediately (F95-kq8)
- **`RefreshVersions` file-content fallback** — now calls `ExtractVersionFromDir` matching full scanner logic, not just directory name (F95-kq8)
- **Stale `? no version detected` output** — suppressed in `RunUpdateCheck()` and `SyncGame()` since no user action is needed (F95-kq8)

### Changed

- **`\b` replaced with `[^a-zA-Z0-9]` boundaries** — Go's regex `\b` treats `_` as word character, breaking underscore-delimited versions like `FullEmberDoors_v0.1.7_Linux` (F95-kq8)
- **Display-layer version fallback** — TUI and CLI show `LatestVersion` when `Version` is empty (no DB backfill), preserving `LatestVersion != Version` update detection (F95-kq8)
- **`shouldSkip` optimized** — exact-match map for `config`/`saved`/`logs`/`crashes`, substring slice for prefix patterns (F95-kq8)
- **Walk path optimized** — single `os.ReadDir` reused across game marker and category checks instead of double-read (F95-kq8)
- **Regex compilation hoisted** — `verIniRE`, `pkgVerRE`, `rpyVerRE` compiled once at package init instead of per-call (F95-kq8)

## [0.3.5-alpha] - 2026-05-04

### Added

- **Store links persistence** — `StoreLinks` (JSON map) and `SteamAppID` columns on `games` table. Store links (Steam, itch.io, DL-Site) are now extracted from F95Zone threads, persisted to the database, and used for precise SteamGridDB artwork lookup (F95-1z8)
- **SGDB icon/logo support** — 4 new SGDB client methods (`GetIconsBySteamAppID`, `GetIconsBySGDBGameID`, `GetLogosBySteamAppID`, `GetLogosBySGDBGameID`). `TrySGDBArtworkByName` and `DownloadSGDBArtwork` now download all 5 artwork types: vertical grid, horizontal grid, hero, icon, and logo (F95-8ai)
- **ICO-to-PNG via SGDB thumb fallback** — `BestGridImage` returns the `thumb` field (a PNG) when the best match is an `.ico` file, avoiding format conversion dependencies (F95-8ai)
- **Self-update command** — `moxie update` fetches the latest release from GitHub, compares versions, downloads the correct platform binary, and atomically replaces itself with rollback support (F95-update)
- **Welcome screen overhaul** — first-run message now includes SteamGridDB setup, `steam add`/`fix-artwork`/`list`, `check-updates`/`sync`, and the `update` command
- **TUI help overlay** — CLI quick-start commands (scan, scrape, steam add, fix-artwork) added to the `?` help screen
- **31 new tests** across `db`, `scraper`, `commands`, and `steam` packages — marshal/unmarshal store links, DB round-trip, parser store link matching, ApplyThreadData wiring, BestGridImage thumb fallback

### Fixed

- **SGDB parse error** — all v2 endpoints return `{success, data, errors}` wrapper objects, not bare arrays. Added `sgdbImageResponse` wrapper type for grids, heroes, icons, and logos (F95-8ai)
- **SGDB icon mime filter** — icons use `image/vnd.microsoft.icon` (`.ico`), not PNG. Removed `?mimes=image/png` from icon endpoints (F95-8ai)
- **Parser store link false positives** — DL-Site help articles (`/hc/`, `/help/`, `/home/`), Steam curator pages (`/curator/`), and bare itch.io publisher pages no longer matched as store links. Replaced domain substring matching with function-based matchers (F95-1z8)
- **Store links not saved during sync update check** — Phase 2 (update check) now saves `StoreLinks` and `SteamAppID` from scraped thread data, not just Phase 1 (association) (F95-1z8)

### Changed

- **Artwork priority chain** — `SteamAdd` and `SteamFixArtwork` now try `DownloadSGDBArtwork` by real Steam App ID first, then `TrySGDBArtworkByName`, then F95Zone cover fallback
- **`TrySGDBArtworkByName` signature** — now accepts `*steam.SGDBClient` instead of raw `apiKey` string, avoiding redundant client creation
- **F95Zone fallback consistency** — `SteamAdd` now sets `artDone = true` on success and handles `ErrUnsupportedFormat` silently, matching `SteamFixArtwork` behavior
- **Pre-compiled regexes** — `ExtractSteamAppID` and parser's Steam store matcher now use package-level `regexp.MustCompile` instead of compiling on every call
- **Steam AppID regex** — trailing slash made optional (`(?:/|$)`), matching bare `/app/12345` URLs
- **SGDB key hints unified** — all user-facing messages use `"Tip: Set a SteamGridDB API key for higher-quality artwork!"` with no "premium" terminology. Clearer one-line hint shown upfront in Steam commands; full setup instructions only when artwork fails entirely
- **Onboarding documentation** — README quick start expanded to 4-step workflow, welcome screen restructured with section groups, usage footer includes SGDB tip

## [0.3.4-alpha] - 2026-05-03

### Fixed

- **Version comparison inconsistency** — `SyncGameLogic` now uses `NormalizeVersion()` for version comparison, matching `RunUpdateCheck` behavior (F95-9ll)
- **Phase 1 cooldown prevents Phase 2** — `VersionCheckedAt` no longer set during association, allowing immediate version checks for newly associated games (F95-5ah)
- **TUI blocks on DB I/O** — `detailView()` now loads game data asynchronously via `tea.Cmd`, showing a loading indicator instead of blocking the render loop (F95-dx7)
- **Path-prefix collision in scanner** — `strings.HasPrefix` now uses separator-aware comparison, preventing `/games/foobar` from being falsely skipped when `/games/foo` is a game (F95-g29)
- **`ErrSteamRunning` never enforced** — `WriteShortcuts`, `SetProtonVersion`, and `RemoveProtonVersion` now all guard against Steam running (F95-o5w)
- **Missing `fsync()` before file rename** — all 5 Steam write paths now call `Sync()` before close/rename, preventing corrupt files on system crash (F95-4it)
- **Partial-write error recovery** — `resizeAndSave`, `downloadAndResize`, and `DownloadImage` now remove destination files on encode/copy failure (F95-9jg)
- **Steam backup accumulation** — backup filenames changed from timestamped to fixed rotation (one backup per file) (F95-5sy)
- **`ComputeMatchScore` degraded by unsanitized titles** — `resultTitle` now sanitized via `SanitizeTitle`, restoring proper 1.0 scores for exact matches with bracketed tags (F95-h5r)
- **Silent kooky error discard** — `GetF95Cookies` now surfaces kooky read errors in the diagnostic message (F95-oy8)

### Security

- **SSRF via scraped artwork URLs** — `isValidDownloadURL()` validates HTTPS-only, blocks private/loopback/link-local IPs and known cloud metadata endpoints before any HTTP download (F95-3g2)
- **Database and config file permissions** — `games.db` and `config.json` now created with `0600` permissions (F95-5yp)
- **Response body truncation in errors** — SteamGridDB error messages no longer include full response body (truncated to 200 chars) (F95-tsq)

### Performance

- **Single-pass scanner** — `Scan()` now accumulates directory sizes during the initial walk instead of re-walking each game directory, eliminating O(N×F) redundant filesystem calls (F95-2ju)
- **Parallel engine detection** — scanner second pass uses bounded worker pool (`runtime.NumCPU()` goroutines) for per-game detection (F95-v2z)
- **Scraper double body read eliminated** — `do()` returns body string directly; callers use it instead of re-reading (F95-bbz)
- **Engine detection caches extensions** — `extSet` computed once from initial `ReadDir`, avoiding up to 9 redundant reads per directory (F95-zt2)
- **DOM selection cached** — `article.message-content .bbWrapper` selector computed once per page, passed to 3 extractors (F95-3yz)
- **TUI filter debounce** — 150ms `tea.Tick` debounce prevents full sort+rebuild on every keystroke (F95-9ue)
- **Lipgloss style cache** — 14 pre-built engine styles eliminate per-cell `NewStyle()` allocations (F95-bhu)

### Changed

- **Engine-aware scoring** — both `SyncGameLogic` and `RunScrapeAuto` now boost candidates whose titles contain engine keywords matching the detected game engine (+0.15). This prefers release threads (e.g., `RPGM Completed Demons Roots`) over request threads (`[Translation Request] Demons Roots`)
- **Async TUI detail loading** — `detailGame` cached in model, loaded via `loadDetailGame` async command, refreshed after edits
- **TUI info/error separation** — `notice` field added to model; informational messages no longer abuse the `error` type
- **Context cancellation support** — `ScrapeThreadWithContext` and `SearchF95ZoneWithContext` added (existing methods use `context.Background()` for backward compatibility)
- **SGDB CDN rate limiting** — `DownloadImage` has independent 200ms rate limiter (separate from API's 1050ms limit)
- **SteamGridDB error handling** — `ErrInvalidURL` added; `doGet` logs structured errors via `internal/log`
- **Database `busy_timeout`** — `PRAGMA busy_timeout = 5000` prevents `SQLITE_BUSY` under concurrent access

### Architecture

- **`internal/config/` package** — config I/O (`ConfigDir`, `DbPath`, `ReadConfig`, `WriteConfig`) extracted from `internal/util`, eliminating `util→db` dependency
- **`internal/log/` package** — structured logging wrapper around `log/slog` with `Debug`/`Info`/`Warn`/`Error` levels
- **Scraper decoupled from database** — `FindMatches` now accepts `ScrapeInput` instead of `db.Game`; `associate.go` no longer imports `internal/db`
- **Engine matching deduplicated** — `findEngineInText` helper replaces 4 inline `EngineTagVariants` iteration loops (~30 lines net reduction)

### Tests

- **Scanner path-prefix collision test** — `TestScanPathPrefixCollision` verifies sibling-dir prefix bug is fixed
- **TUI state transition tests** — `TestCycleSort`, `TestCycleEngineFilter`, `TestCycleStatusFilter`
- **Steam `BestGridImage` tests** — 6 cases covering empty, single, highest-score, data:URI, SVG skip

## [0.3.3-alpha] - 2026-05-03

### Added

- **Test suite overhaul** — 223 test functions across 14 test files (up from 161/12), 0%→tested coverage for browser and TUI packages
- **Browser package tests** — `sanitizeHeaderValue` (9 cases) and `buildCookieHeader` (5 cases) for cookie value sanitization
- **TUI helper tests** — 15 tests covering `filterAndSort`, `truncate`, `orDash`, `renderTags`, `nextStatus`, `formatSize`, `statusColor`, `engineColor`, and `SortField` string representations
- **Scanner integration tests** — `TestScanCategoryDirectory` and `TestScanCategoryDirNested` verify category folder (Unity/, RPGM/) skip logic during directory walks
- **Scanner `ExtractVersion` tests** — 19 cases covering date, dot, dash, and underscore version patterns with priority ordering
- **Scanner `isCategoryDir` / `isEngineName` tests** — 23+5 cases for engine-name matching and category folder detection
- **Steam Proton pure logic tests** — 12 tests for `vdfEscape`, `isValidProton`, `getOrCreateMap`, `getCompatToolMapping`, `encodeVDF`, and `writeVDFMap` (all 0%→85-100%)
- **Scraper HTTP client injectability** — `NewClientWithHTTP(cookie, *http.Client)` enables testing rate-limiting with `httptest.Server`
- **Scraper `Client.do()` tests** — 6 tests covering rate-limit backoff, context cancellation, cooldown, 403 detection, Cloudflare blocking, and cookie injection
- **Config path tests** — `DbPath`, `ConfigPath`, `ConfigDir` path verification; `ReadConfig`/`WriteConfig` round-trip via temp files
- **`RunUpdateCheck` integration tests** — 4 tests for no-games, cooldown skip, and force bypass scenarios
- **`SyncGameLogic` extraction** — Business logic extracted from `SyncGame` into testable function with `SyncGameResult` struct; 5 integration tests added

### Fixed

- **7 silent error discards** — `_ = database.UpdateGame()` / `_ = database.UpsertScrapedMeta()` in `sync.go` and `scrape.go` now log errors to stderr
- **`TestDetectMugenTooFewDirs`** — replaced vacuous `_ = result` no-op with real assertion verifying Mugen threshold behavior
- **`TestIsLinux`** — removed tautological test (tested Go runtime, not project code)
- **`TestExtractDeveloper`** — fixed regex `^Developer` anchor preventing mid-sentence false matches; test updated from `"info here"`→`""`
- **`TestComputeMatchScore_ExactAfterSanitize`** — removed confusing early-return that bypassed assertions
- **`isNumeric("")`** — changed from `true` to `false` (empty string is not numeric)
- **`scanner.Scan()` error suppression** — now returns `fmt.Errorf("scan: %w", err)` for root-level walk errors instead of empty slice
- **`isValidProton("Proton 9.0")`** — added whitespace rejection (Proton version identifiers never contain spaces)
- **`TestNewClient`** — upgraded from nil-check to behavioral assertions (delay, unsafe client zero-delay)

### Changed

- **Test quality:** 4 trivial tests deleted (`TestEngineString`, `TestIsLinux`, `TestTruncateVer`, `TestGridFilePath_AbsolutePaths`)
- **Coverage improvements:** scanner 76%→90%, scraper 60%→71%, steam 30%→39%, util 45%→52%, browser 0%→18%, tui 0%→14%
- **`developerPattern1` regex** — added `^` anchor for line-start matching only
- **Installer scripts rewritten** — `install.sh` (592 lines) and `install.ps1` (287 lines) now feature download progress bars, already-installed version detection, `--version`/`--binary`/`--no-modify-path` flags, release verification via HEAD request, automatic PATH modification (shell config on Unix, user PATH on Windows), GitHub Actions CI support, local binary version detection, directory-vs-file validation, and consistent post-install banners with quick-start tips
- **GitHub Actions release workflow** — `.github/workflows/release.yml` auto-builds all 6 platform/arch binaries on tag push, stamps version via `-ldflags`, and creates a GitHub Release with `softprops/action-gh-release`
- **README** — updated install section, build-from-source instructions with version stamping, test count (223), and binary size (~16 MB)

[Unreleased]: https://github.com/Milisource/moxie/compare/v0.3.5-alpha...HEAD
[0.3.5-alpha]: https://github.com/Milisource/moxie/compare/v0.3.4-alpha...v0.3.5-alpha
[0.3.4-alpha]: https://github.com/Milisource/moxie/compare/v0.3.3-alpha...v0.3.4-alpha
[0.3.3-alpha]: https://github.com/Milisource/moxie/compare/v0.3.1-alpha...v0.3.3-alpha
[0.3.2-alpha]: https://github.com/Milisource/moxie/releases/tag/v0.3.2-alpha
