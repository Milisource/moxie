# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[0.3.3-alpha]: https://github.com/Milisource/moxie/releases/tag/v0.3.3-alpha
[0.3.2-alpha]: https://github.com/Milisource/moxie/releases/tag/v0.3.2-alpha
