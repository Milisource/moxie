# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
