# moxie — MVP Specification

**Version:** 0.3.5-alpha (May 2026)
**Status:** Alpha — 0.3.5 (SGDB parse fix, icon/logo support, store links persistence, Steam AppID artwork pipeline, self-update command)
**Target:** CLI/TUI → Multi-platform Wails desktop app

---

## Overview

A local game library manager for adult games. Scans directories, detects engines (14 canonical + 3 community → Others), matches games to F95Zone threads for metadata, and presents results in a terminal UI. Built as a single static Go binary with embedded SQLite.

---

## Component Documentation

| Component | Document | Covers |
|---|---|---|
| **Architecture** | [architecture.md](architecture.md) | Package diagram, data flow, design rationale, future path |
| **Scanner** | [scanner.md](scanner.md) | Directory walk, engine detection profiles, exclusion list, limitations |
| **Scraper** | [scraper.md](scraper.md) | HTTP client, rate limiting, HTML parsing, auto-association |
| **Downloader** | [downloader.md](downloader.md) | HTTP downloads with resume, progress tracking, SSRF protection |
| **Archive** | [archive.md](archive.md) | Archive extraction (.zip, .7z, .rar, .tar.gz), platform detection |
| **Commands** | (this doc) | 30+ CLI handlers: crud, scrape, sync, cleanup, play, steam, rename, config, download |
| **Config** | (this doc) | Config I/O (`ConfigDir`, `DbPath`, `ReadConfig`, `WriteConfig`) in `internal/config/` |
| **Utilities** | (this doc) | Formatters, version normalization, helpers in `internal/util/` |
| **Logging** | (this doc) | Structured logging wrapper around `log/slog` in `internal/log/` |
| **TUI** | [tui.md](tui.md) | Bubble Tea model/update/view, keyboard shortcuts, filters |
| **Database** | [database.md](database.md) | SQLite schema, version tracking, migration strategy |
| **Browser** | [browser.md](browser.md) | Cross-browser cookie extraction with kooky + SQLite fallback |
| **Steam** | [steam-package-design.md](steam-package-design.md) | Steam shortcut management, Proton config, artwork, SteamGridDB |

---

## Implementation Status

### Completed (MVP)

- [x] 28 CLI entry points: all previous + `cleanup`, `refresh-versions`, `scrape-batch`, `set-path`, `set-exe`
- [x] Recursive directory scanning with smart `SkipDir` on game roots
- [x] Engine detection for 14 canonical engines + NW.js (RPG Maker MV/MZ) detection
- [x] SQLite database with WAL mode, foreign keys, CHECK constraints, LatestVersion tracking
- [x] Cookie-based F95Zone scraping (Firefox auto-detect, explicit, SQLite fallback)
- [x] Auto-association via F95Zone search with title scoring + engine mismatch prevention
- [x] Bubble Tea TUI with list/detail views, engine/status filters, engine colors
- [x] Version tracking with `LatestVersion` fallback for unknown local versions
- [x] Version normalization (trailing .0 stripping, v-prefix handling)
- [x] `--force` flag for bypassing 24h update-check cooldown
- [x] Cleanup command: engine mismatch detection, exe mismatch detection, interactive disassociation
- [x] Engine compatibility map (RPGM↔HTML via NW.js, WolfRPG↔HTML)
- [x] `--warnings` flag on `list` command
- [x] 223 tests across 14 test files (scanner, engine, scraper, DB, helpers, commands, browser, tui, steam, util)
- [x] Browser package tested — cookie value sanitization and header building (100% pure logic coverage)
- [x] TUI package tested — filter/sort, status/engine colors, formatting helpers (100% pure logic coverage)
- [x] Steam Proton VDF pure logic tested — vdfEscape, isValidProton, getOrCreateMap, encodeVDF/writeVDFMap (85-100%)
- [x] Scraper HTTP client injectable via `NewClientWithHTTP` for testing with `httptest.Server`
- [x] Scraper rate-limiting/bot-detection tested — 6 tests for backoff, context cancel, cooldown, 403, Cloudflare
- [x] `SyncGameLogic` extracted — business logic separated from CLI I/O, 5 integration tests
- [x] `RunUpdateCheck` integration tested — no-games, cooldown skip, force bypass (57% coverage)
- [x] Config read/write tested — path-injected helpers with temp files for round-trip verification
- [x] Scanner category folder skip logic verified — integration tests for Unity/RPGM category directories
- [x] `scanner.Scan()` error propagation fixed — root-level walk errors now returned instead of silently suppressed
- [x] `isNumeric("")` fixed — returns `false` instead of `true` for empty string
- [x] 7 silent `_ = database.*` error discards replaced with logged stderr warnings
- [x] `developerPattern1` regex fixed — `^Developer` anchor prevents mid-sentence false matches
- [x] Installer scripts rewritten — `install.sh` (592 lines) and `install.ps1` (287 lines) with progress bars, version pinning, PATH auto-modification, release verification, and GitHub Actions support
- [x] GitHub Actions release workflow — auto-builds 6 platform binaries on tag push, creates release with `softprops/action-gh-release`
- [x] Engine-aware scoring for auto-association — thread candidates with matching engine keywords get +0.15 score boost
- [x] Single-pass scanner with inline size accumulation — eliminates O(N×F) redundant filesystem calls
- [x] Parallel engine detection — bounded worker pool (`runtime.NumCPU()`) for per-game detection in scanner second pass
- [x] Async TUI detail loading — `detailGame` cached in model, loaded asynchronously to prevent render-loop blocking
- [x] TUI filter debounce — 150ms throttle on search filter rebuilds
- [x] Security: SSRF protection via `isValidDownloadURL()` — HTTPS-only, blocks private/loopback IPs and metadata endpoints
- [x] Security: `games.db` and `config.json` permissions set to `0600`
- [x] Data integrity: `fsync()` before rename on all Steam file writes
- [x] Data integrity: `ErrSteamRunning` enforced in all Steam mutation functions
- [x] Data integrity: Partial-write cleanup — destination files removed on encode/copy failure
- [x] `busy_timeout = 5000` on SQLite connections for concurrent access safety
- [x] Context cancellation support — `ScrapeThreadWithContext` / `SearchF95ZoneWithContext`
- [x] `internal/config/` package extracted from `internal/util/` — eliminates `util→db` dependency
- [x] `internal/log/` package — `log/slog` wrapper with Debug/Info/Warn/Error levels
- [x] Scraper decoupled from database — `ScrapeInput` replaces `db.Game` in `FindMatches`
- [x] Engine matching deduplicated — `findEngineInText` helper replaces 4 inline loops
- [x] Lipgloss style cache — 14 pre-built engine styles eliminate per-cell allocations
- [x] Steam backup rotation — fixed-name backups replace unbounded timestamped accumulation
- [x] Browser cookie error surfaced — kooky read errors included in diagnostic messages
- [x] `--version` flag with git describe injection via ldflags
- [x] First-run welcome message when no database exists
- [x] Platform-aware Firefox User-Agent (Linux/macOS/Windows)
- [x] macOS native Mach-O executable detection in `play` command
- [x] TUI CrossOver wine support on macOS (matches CLI behavior)
- [x] Cross-compilation: `CGO_ENABLED=0` static builds, `windows/arm64`, `macos/arm64`
- [x] NBSP handling in `IsNonGameThread` — F95Zone prefix labels use non-breaking spaces
- [x] TUI URL update triggers live metadata scrape (not just DB reload)
- [x] `FindMatches` non-game thread filtering (was missing from associate.go)
- [x] CHANGELOG.md and expanded AGENTS.md with project conventions
- [x] `make install` and `make clean` targets
- [x] Version extraction from directory names fixed — `\b` replaced with explicit non-alphanumeric boundaries to handle underscore-delimited versions (e.g. `FullEmberDoors_v0.1.7_Linux`, `Game_V1.0.0_HotFix`)
- [x] Single/double-digit version pattern added — `v5`, `v01`, `v0` now detected
- [x] Trailing build letter support — `v0.7.7i` captured as `"0.7.7i"` instead of missed
- [x] TUI `🔄` update indicator fixed — requires both `Version` and `LatestVersion` non-empty (previously triggered on empty local version, falsely marking every game with scraped metadata as having an update)
- [x] Empty versions display as `"unknown"` in TUI table, detail view, and `moxie list` CLI output (replaces bare `-`)
- [x] Stale `? no version detected` output suppressed in `RunUpdateCheck()` and `SyncGame()` during sync — no action needed from user
- [x] Bracketed-title version extraction expanded per F95Zone title format rules — supports `[YYYY-MM-DD]`, `[X.Y]` bare versions, `[Final]` sentinel, `[Ch. 2 v3.0]` embedded chapter+version, and `[v1.0 Alpha]` prerelease suffixes

### Upcoming

- [x] DB migration: store_links + steam_app_id columns for persistent Steam/Itch.io links
- [x] SGDB artwork activation by real Steam App ID (DownloadSGDBArtwork priority 1)
- [x] Download manager / file organizer with resume support, progress bars, platform priority
- [x] Archive extraction (.zip, .7z, .rar, .tar.gz) with auto-detection
- [x] Download links table with platform detection (Linux/Windows/MacOS)
- [x] Dead link validation (404/5XX/DMCA detection)
- [ ] FTS5 full-text search
- [ ] Cover image download and local caching
- [ ] Directory watcher (auto-scan on file changes)
- [ ] Export/import library (JSON backup)
- [ ] Wails desktop GUI

### Known Limitations

- **No FTS5** — uses `LIKE '%query%'` on title only
- **False positives** — tool/editor directories and generic folder names may be misdetected as games
- **No archive scanning** — `.zip`/`.rar`/`.7z` at scan roots are not inspected (but can be extracted after download)
- **No content-based dedup** — same game in multiple paths creates duplicate records
- **No cover caching** — cover URLs are stored but images are not downloaded
- **Non-UTF-8 filenames** — Latin1/Shift-JIS display incorrectly in the TUI
- **Commands package** — 100+ `os.Exit(1)` calls in CLI wrappers make the I/O layer untestable; future refactor should extract remaining business logic from command functions
- **No Wine detection** — `play` command tries `wine` on PATH and CrossOver on macOS, but doesn't auto-detect Wine installations or offer to install it
