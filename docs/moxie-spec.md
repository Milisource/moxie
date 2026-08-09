# moxie — MVP Specification

**Version:** 0.4.0-alpha (July 2026)
**Status:** Alpha — 0.4.0 (moxie update fix — correct GitHub repo URL). Download: Beta (8 host resolvers: Pixeldrain, Buzzheavier, Gofile, Google Drive, DataNodes, MixDrop, Mega, VikingFile (beta))
**Target:** CLI/TUI → Multi-platform Wails desktop app

---

## Overview

A local game library manager for adult games. Scans directories, detects engines (15 canonical + 3 community → Others), matches games to F95Zone threads for metadata, and presents results in a terminal UI. Built as a single static Go binary with embedded SQLite.

---

## Component Documentation

| Component | Document | Covers |
|---|---|---|
| **Architecture** | [architecture.md](architecture.md) | Package diagram, data flow, design rationale, future path |
| **Scanner** | [scanner.md](scanner.md) | Directory walk, engine detection profiles, exclusion list, limitations |
| **Scraper** | [scraper.md](scraper.md) | HTTP client, rate limiting, HTML parsing, auto-association |
| **Downloader** | [downloader.md](downloader.md) | HTTP downloads with resume, host-specific resolvers, progress tracking, SSRF protection, platform priority (Wine/Proton chain), dead link validation |
| **Archive** | [archive.md](archive.md) | Archive extraction (.zip, .7z, .rar, .tar.gz), zip-slip protection, system tool fallback |
| **Commands** | (this doc) | 30+ CLI handlers: crud, scrape, sync, cleanup, play, steam, rename, config, download |
| **Config** | (this doc) | Config I/O (`ConfigDir`, `DbPath`, `ReadConfig`, `WriteConfig`) in `internal/config/` |
| **Utilities** | (this doc) | Formatters, version normalization, helpers in `internal/util/` |
| **Logging** | (this doc) | Structured logging wrapper around `log/slog` in `internal/log/` |
| **TUI** | [tui.md](tui.md) | Bubble Tea model/update/view, keyboard shortcuts, filters |
| **Database** | [database.md](database.md) | SQLite schema, version tracking, migration strategy |
| **Browser** | [browser.md](browser.md) | Cross-browser cookie extraction with kooky + SQLite fallback |
| **Steam** | [steam-package-design.md](steam-package-design.md) | Steam shortcut management, Proton config, artwork, SteamGridDB |
| **Desktop** | [architecture.md](architecture.md) | Wails v2 + Svelte 5 desktop GUI architecture, component tree, build workflow |
| **F95Zone Browser** | [f95zone-browser-design.md](f95zone-browser-design.md) | F95Zone latest_alpha page analysis, UX patterns, data model, recommendations for F95Browser.svelte |

---

## Implementation Status

### Completed (MVP)

- [x] 29 CLI entry points: all previous + `cleanup`, `refresh-versions`, `scrape-batch`, `set-path`, `set-exe`, `set-wine-prefix`
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
- [x] 235 tests across 15 test files (scanner, engine, scraper, DB, helpers, commands, browser, tui, steam, util)
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
- [x] Engine domain logic extracted — `EngineTagVariants`, `EngineMatchesThread`, `FindF95Engine`, `ExtractEngineFromTitle`, `FormatTagsBrief` moved from `commands/cleanup.go` to `engine/engine_tags.go`
- [x] Scraper domain logic extracted — `StripThreadPrefix` moved to `scraper/title.go`, `ApplyThreadData` moved to `scraper/apply.go`
- [x] Steam domain logic extracted — `ExtractSteamAppID` moved from `commands/steam_artwork.go` to `steam/appid.go`
- [x] Downloader domain logic extracted — `DetectPlatformFromLink` moved from `commands/scrape.go` to `downloader/detect.go`
- [x] Cross-package deduplication — `IsOnlineOnly`, `ScoreLinkHost`, `ScoreDownloadLink`, `FindMostRecentFile` unified in `downloader/links.go`
- [x] File separation — 7 monolith files split into 22+ entity-grouped files across all packages (db: entity CRUD, downloader: per-host resolvers, commands: sync/steam/download subcommand files, scraper: parser sub-files)
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
- [x] Compact YYYYMMDD date pattern added — `Data20260403` detected as valid date version (with month/day validation to avoid false positives on arbitrary 8-digit numbers)
- [x] File-based version extraction from `Game.ini` (RPG Maker), `package.json` (HTML/NW.js), and `game/options.rpy` (Ren'Py) — catches versions missed in directory names
- [x] Single/double-digit version pattern added — `v5`, `v01`, `v0` now detected
- [x] Trailing build letter support — `v0.7.7i` captured as `"0.7.7i"` instead of missed
- [x] TUI `🔄` update indicator fixed — requires both `Version` and `LatestVersion` non-empty (previously triggered on empty local version, falsely marking every game with scraped metadata as having an update)
- [x] Empty versions display as `"unknown"` in TUI table, detail view, and `moxie list` CLI output (replaces bare `-`)
- [x] Stale `? no version detected` output suppressed in `RunUpdateCheck()` and `SyncGame()` during sync — no action needed from user
- [x] Bracketed-title version extraction expanded per F95Zone title format rules — supports `[YYYY-MM-DD]`, `[X.Y]` bare versions, `[Final]` sentinel, `[Ch. 2 v3.0]` embedded chapter+version, and `[v1.0 Alpha]` prerelease suffixes
- [x] Display-layer fallback shows `LatestVersion` when `Version` is empty (instead of backfilling DB) — preserves update detection while eliminating "unknown" display
- [x] Parent directory name fallback for nested games (e.g. `Game v1.0/Game Windows/` detects `1.0` from parent)
- [x] Executable filename version extraction (e.g. `[Full]EmberDoors_v0.1.7_Linux.x86_64` → `0.1.7`)
- [x] Scan command now updates existing games instead of skipping them — improved version detection takes effect on re-scan
- [x] `RefreshVersions` command uses `ExtractVersionFromDir` as fallback, matching scanner logic
- [x] `shouldSkip` optimized — exact-match map for O(1) lookup, substring slice fallback for prefix patterns
- [x] Walk path optimized — single `os.ReadDir` per directory reused across game marker and category checks (was double-read)
- [x] Regex compilation hoisted to package level — `verIniRE`, `pkgVerRE`, `rpyVerRE` compiled once at init instead of per-call
- [x] Download fallback — when a link fails (e.g. Mega encrypted protocol), the CLI and TUI automatically try the next-best link in platform-priority order. Mega links deprioritized to -200 in host scoring.
- [x] Masked URL unmasking — `f95zone.to/masked/<host>/...` URLs are automatically extracted to real host URLs before resolution
- [x] Application-wide logging — per-day log files (`moxie-YYYY-MM-DD.log`) written to `~/.config/moxie/logs/` via `log.Init()`; instruments download attempts, fallbacks, resolve failures, and completion
- [x] Update merge — downloaded+extracted game files are merged into the existing game directory, preserving user saves, mods, and configs based on engine-aware preserve patterns (14 engines). Optional .old backup.
- [x] `moxie install <id> <path>` command — manual archive→extract→merge→DB-update pipeline for games whose download links all failed
- [x] `moxie play <id|name>` fuzzy name search — tries numeric ID first, falls back to title `LIKE` search with multi-word retry, interactive picker for multiple results
- [x] Fuzzy name search across ALL commands — shared `ResolveGame`/`ResolveFirstArg` helpers in `internal/commands/game_lookup.go` (143 lines, 12 tests) powering 17 command entry points (info, scrape, download, install, sync, remove, set-exe, set-path, config set-thread, steam add/remove/fix-artwork/proton-set)
- [x] `ConfirmDestructive` helper — guards destructive operations (remove, set-exe, set-path, config set-thread) with name-match confirmation prompt; respects `--assume-yes` and non-interactive (piped) stdin
- [x] TTY detection (`isInteractive()`) — skips interactive prompts when stdin is piped/redirected; prints matches to stderr and exits with code 1 instead of blocking
- [x] `ResolveFirstArg` variant — for multi-arg commands like `install <id|name> <archive-path>` that must not consume subsequent positional args as part of the game name
- [x] `LaunchCommand` working directory fix — `cmd.Dir` set to game root so Windows games under Wine resolve relative asset paths correctly
- [x] `SelectBestExe` scoring overhaul — skips known runtime engines (`nwjc`, `nw`, `node`) and launchers (`unitycrashhandler`, `unins`, `setup`); awards +1 GB score bonus for `Game.exe`
- [x] Host scoring reorganized — verified hosts (Pixeldrain, Buzzheavier, Gofile, Catbox) weighted +25, may-work hosts (DataNodes, Google Drive, MixDrop) weighted +10, borked hosts (Mega, VikingFile, WorkUpload, KrakenFiles, Bunkrr) penalized -200
- [x] Google Drive resolver — two-step confirm token extraction for >100 MB large files; `GET /uc?export=download&id=<ID>` → parse HTML for `confirm=` token → re-request with `&confirm=<TOKEN>`
- [x] DataNodes resolver — cookie + POST flow: `GET /download/<CODE>` for session cookies → parse hidden form fields → `POST` same URL with cookies → follow 302 redirect to CDN download URL
- [x] VikingFile resolver — form POST flow: GET page for hidden fields → POST with op/download1/id/rand/method_free → follow 302 redirect (blocked by Cloudflare Turnstile captcha, classified as beta)
- [x] MixDrop resolver — passthrough with User-Agent header (may be blocked by interstitial file pages, classified as beta)
- [x] Download validation (`IsValidGameFile`) — rejects files < 4096 bytes or that aren't archives/executables; catches interstitial HTML pages that fake hosts serve instead of real files
- [x] TUI step-by-step download status — `stepMsg` field on `activeDownload` shows host-finding phase, per-host attempt, failure reasons, extract/merge progress; rendered in `downloadSection()` in real-time via 500 ms poll tick
- [x] **Sync rework — cookie-free primary path** — version checks now run through F95Zone's public JSON endpoints instead of cookie-dependent thread scraping:
  - `checker.php` bulk version lookup — up to 100 thread IDs per request, one or two requests cover the whole library (verified: 103 games in ~2s, vs ~1m40s of scraping)
  - `latest_data.php?cmd=list` title search — cookie-free auto-association with stopword-stripped queries (Redis search semantics), title-based non-game rejection and cache-type-field engine validation
  - F95Checker cache API (`/fast`, `/full`) — untracked-thread coverage, status/tags/metadata refresh, thread-missing detection (privated/deleted threads no longer surface as mysterious 403s)
  - `StripVersionQualifier` — checker/cache versions (`"v21.0.0 wip.7944"`) normalized to numeric core before comparison; phantom updates eliminated
  - 3-layer fallback: bulk API → cache API → direct scrape (with cookies). Cookies are no longer required for sync at all
  - Desktop sync reworked to the same cookie-free flow with proper candidate scoring (previously took `results[0]` blindly) and the cache-API metadata refresh; phase 1 (auto-association) runs on 3 parallel workers (per-worker pacing), skips no-match games within a 24h cooldown, and reuses the persistent association cache so repeat syncs are near-instant
  - Public endpoints now carry the session cookie when available (`NewPublicAPIWithCookie`) — F95Zone's anonymous hourly quota no longer stalls full-library syncs; cookie-free construction remains the fallback (F95-8vlc)
  - Cache-API `type` enum drives engine detection (correct numbering) for association and the engine-consistency check; weak title matches (score < 0.7 or no token containment) never overwrite a curated title (F95-6duz, F95-ytru)
- [x] **Desktop security hardening** — `GetThreadPreview`/`AddGameFromF95Zone` validate `https` + `f95zone.to` host before any cookie-carrying request, closing the SSRF/cookie-exfiltration hole for forged URLs (F95-vavb)
- [x] **Scraper hardening** — retry with backoff (transient 5xx/network, single 403 retry), circuit breaker after 3 consecutive blocks (dead sessions fail fast instead of game-by-game), `Preflight()` session check, browser-identical `Sec-Fetch-*` headers, Cloudflare-marker detection inside 403/503 bodies
- [x] Cookie wiring through download pipeline — `f95Cookie` parameter threaded through `Download()` → `DownloadWithHost()` → `HostResolver.SetF95Cookie()` → `followRedirect()` for authenticating F95Zone masked URL HEAD requests
- [x] **Browser cookie reuse for download hosts (cf_clearance)** — `browser.GetCookiesForHost(hostname)` extracts any browser's cookies for a download host (RFC 6265 domain match, 60 s kooky cache, 6 KB header cap) and the `HostResolver` merges them into every resolver GET/POST and the final download request. Cloudflare-protected hosts (buzzheavier, datanodes, vikingfile) pass their challenge when the user has visited the host in a browser; host-scoped so cookies never leak cross-domain
- [x] **Masked URL JSON unwrap** — F95Zone's masked pages are now a JS+reCAPTCHA interstitial with no redirect for HTTP clients; `unwrapMasked()` POSTs `{xhr:1, download:1}` to the masked path with the session cookie and parses `{"status":"ok","msg":"<real url>"}` — masked links resolve without a browser (legacy GET redirect fallback retained)
- [x] **Masked URL unwrap cache** — successful unwrap results cached in the DB `resolved_urls` table (7-day TTL): the masked branch of `Resolve()` consults the cache first, so repeated downloads/checks of the same `/masked/` link skip the rate-limited unwrap endpoint (captcha rotation). Wired at the TUI (`startDownloadCmd`) and CLI `download` call sites via `downloader.SetDefaultResolvedCache`; stale entries read as misses and are auto-pruned on DB open (F95-z690)
- [x] **2026-08-09 downloader unlock wave** (beads F95-yipm/2owu/ggyw/nml7/46x0/ry1d/ugim/j3b5/yyes/cp07/yp6s/vho8):
  - Mediafire resolver — page CDN extraction (`#downloadButton` href + base64 `data-scrambled-url` with host allow-list) + API fallback (F95-2owu)
  - Workupload resolver — the "puzzle captcha" is a trivially solvable SHA-256 proof-of-work (~4 ms): `/puzzle` → solve → `/captcha` → cookie flow → `getDownloadServer` API (F95-ggyw)
  - Gofile resolver — guest token + dynamic `X-Website-Token` (SHA-256 of `UA::lang::token::4h-slot::secret`); the old direct subdomain pattern is dead (F95-nml7)
  - Mega downloads via megatools subprocess when the binary is installed; informative error otherwise (F95-46x0)
  - `CheckLink` routes resolver-backed and masked URLs through `HostResolver` before HEAD — no more false dead-link flags (F95-ry1d)
  - Clearance-aware host scoring — pixeldrain/catbox/mediafire +25, buzzheavier/workupload +10, gofile +5, vikingfile/krakenfiles/mega −200 (F95-ugim)
  - uTLS transport (opt-in `MOXIE_UTLS=1`) — browser-grade TLS fingerprint with ALPN forced to h1 via extension replacement; **live verdict: NO-GO for this machine's cookies** (user's Firefox 153 clearance is unmatchable — uTLS v1.8.2 max profiles FF120/Chrome133; CF challenges the fingerprint), but the transport now produces clean typed challenge errors instead of broken connections (F95-j3b5); challenge-graded hosts are the browser-fallback's job (F95-yyes follow-up)
  - `browserresolve` package — headless Chrome download resolver with real-profile copy, rod download events + dir polling (F95-yyes, pending live Chrome verification)
  - Browser-open fallback + download-dir `ArchiveWatcher` — failed downloads offer `[y]` browser-open; browser-saved files are auto-detected, validated, extracted and merged (F95-cp07)
  - Scraped size wired into downloads (`download_links.size`, migration v9) — post-body truncation verification, host caps enforced on known sizes (F95-yp6s)
  - Multi-part archive support — part1.rar/part2.rar and .7z.001-style link sets group, download every part with per-part host fallback, concatenate splits; single-link games unchanged (F95-vho8)
- [x] Host identification fixes — `bzzhr.to` (buzzheavier's short domain, 91 links in library), `miixdrop.net`, and `proton.me` now route to the correct resolvers; buzzheavier HTMX resolution accepts 204 responses per the documented contract
- [x] Archive progress improvements — `totalFiles` excludes directory entries so progress shows only real file extractions; filenames truncated to 60 chars in CLI progress output
- [x] `internal/updater/` package — `Merge()` copies new files from extracted archive to game directory, preserves user saves/configs/mods via engine-aware glob patterns; optional `.old` backup with automatic restore
- [x] `internal/log/` per-day log files — `log.Init(config.LogDir())` writes structured logs to `~/.config/moxie/logs/moxie-YYYY-MM-DD.log`; instruments download attempts, fallbacks, resolve failures, and completions. `Init*` also re-points stdlib `slog.Default` at the same sink, so the desktop app's `slog.*` calls reach the file too. `MOXIE_LOG_LEVEL=debug|info|warn|error` raises/lowers the level at startup (default `info`); cover-fetch runs log one line per game (`cover resolved`), per-cover download results (`cover cached`/failures), and run summaries (`cover fetch started`/`complete`)
- [x] **AVIF covers** — F95Zone's CDN serves AVIF-encoded images under `.png`/`.jpg` cover URLs (Cloudflare conversion); `knownImageFormat`/`imageMimeFromPrefix` sniff the ISOBMFF `ftyp`+`avif`/`avis` brand so AVIF covers are cached and served (`image/avif`). Thumbnails are skipped for AVIF (no pure-Go decoder; cover server falls back to the full image)
- [x] **Security & robustness review (2026-08-09)** — 26 review findings closed: F95Zone session cookie host-scoped (no more leaks to Google/redirect chains); CLI self-update staged in 0700 dir with SHA-256 verification; zip-bomb defense; per-connection FK enforcement via DSN `_pragma`; cancellable scans & downloads (no shutdown hangs, no TUI freezes); Google Drive label fix; Proton tool-ID casing; Steam `shortcuts.vdf` validated against a real Steam-generated fixture (deterministic, canonical field order); exec-bit preservation on game merges; hostname-based download-host identification; word-boundary title matching. Full details in `CHANGELOG.md`

### Upcoming

- [x] DB migration: store_links + steam_app_id columns for persistent Steam/Itch.io links
- [x] SGDB artwork activation by real Steam App ID (DownloadSGDBArtwork priority 1)
- [x] Download manager / file organizer with resume support, progress bars, platform priority
- [x] Archive extraction (.zip, .7z, .rar, .tar.gz) with auto-detection
- [x] Download links table with platform detection (Linux/Windows/MacOS)
- [x] Dead link validation (404/5XX/DMCA detection)
- [ ] Mega download support (native SDK or megatools subprocess wrapper)
- [x] Protect user-curated fields (Version/Engine/ExePath) during rescan — only overwritten when empty or "Unknown"
- [x] Scan tracking columns (`last_scanned_at`, `dir_mtime`) — per-game directory mtime tracking for incremental scanning
- [x] Incremental scan by default — `moxie scan <dir>` skips known, unchanged directories; `--force` for full rescan
- [x] `.old` directory exclusion — scanner skips updater backup dirs; `ListActiveGames()` filters them from all commands (list, sync, rename, download, tui, etc.)
- [x] Help text reorganization — commands grouped into Core, F95Zone, Downloads, Steam, Admin sections
- [x] FTS5 full-text search — virtual table over title/tags/developer/overview with ranked results
- [x] Export/import library — `moxie export [--output file.json]` and `moxie import <file.json>`
- [x] Play history tracking — `moxie history [count]` shows recently played games
- [x] Per-game Wine prefix support — `moxie set-wine-prefix <id> <path>` to persist, `--wine-prefix` flag on `play` to override, TUI launch respects DB-stored prefix
- [x] Game series support — `game_series` table and `series_id`/`series_order` on games
- [x] Game collections — `moxie collection add/list/add-game` with TUI `[c]` filter
- [x] Soft delete — `moxie remove` sets `deleted_at`; `moxie restore`/`moxie purge`; `list --deleted`
- [x] Batch status management — `moxie set-status --engine/--all <status>`
- [x] Parallel scraping — `moxie sync --parallel N` for concurrent auto-association
- [x] Post-scan action hooks — `moxie scan --sync` / `moxie scan --scrape`
- [x] Custom engine detection profiles — JSON drop-in files in `~/.config/moxie/engines/`
- [x] Shared launcher package — `internal/launcher/` unifies CLI + TUI game launching
- [x] Global `--verbose`/`-v` flag on all commands
- [x] Commands testability — `RunPlay`, `RunScan`, `RunSync` logic functions extracted
- [x] Version-gated safe migrations — `PRAGMA user_version` with per-step transactions
- [x] Lightweight `ListGameSummaries` for TUI table view (7 columns instead of 22)
- [x] Targeted column updates — `UpdateGameTitle/Status/F95URL/ExePath` (one column per query)
- [x] TUI keyboard shortcut discoverability — grouped help overlay, startup tip, dynamic footer
- [x] Progress feedback — live `\r` scan progress, ETA in sync, TUI spinner during downloads
- [x] TUI detail view information density — sectioned layout, direct status selector
- [x] TUI/UX audit completed — 18 specific improvements documented
- [x] Download size limits — 50 GB Content-Length cap, 6h timeout, `io.LimitReader`
- [x] Content-Type validation — rejects `text/html` downloads before body write
- [x] Log file security — `0600` permissions, URL query redaction, 30-day rotation
- [x] VDF dependency replaced — vendored binary VDF parser, timestamped backups
- [x] Association cache permissions — `0600` on `associations.json`
- [x] Cover image download and local caching
- [x] Wails Desktop App — Alpha (F95-1132)
  - [x] Wails v2 + Svelte 5 scaffold (desktop/ + frontend/)
  - [x] App struct binding layer wrapping all internal/ packages
  - [x] Game list view with search, engine/status filters, sortable columns, context menu
  - [x] Game detail view with cover art, metadata, inline editing, sync, download links
  - [x] Scan directory dialog with saved paths and live Wails event progress
  - [x] F95Zone sync dialog with per-game progress and completion summary — sync state lives in the app shell, so progress/results survive tab switches; backend guard rejects a second concurrent run
  - [x] Dark/light mode (system preference auto-detect)
  - [x] Cover art thumbnails in game list — lazy-loaded from a loopback HTTP cover server (320px JPEG thumbs generated at cache time, full image in the detail view)
  - [x] Cover art backfill — Covers view fetches missing covers in bulk (stored URL → F95Checker cache API → cookie scrape), 4-worker concurrent downloads with singleflight dedupe, live progress and completion events
  - [x] Game version update view with per-game and batch update
  - [x] F95Zone game browser with search, preview panel, add-to-library
  - [x] Download management view with expandable game cards, open-in-browser
  - [x] App self-update checker with download and apply
  - [x] Manual add game dialog with engine detection
  - [x] Duplicate game detection and resolution
  - [x] Game rename, status change, notes, exe path editing
  - [x] Trash view with restore/purge
  - [x] Wails production build: `make desktop` → `dist/moxie-desktop` (4.6s)
  - [x] Wails dev: `make desktop-dev` (hot-reload)
  - [x] Desktop installer: `make install-desktop` → per-platform app registration (Linux .desktop, macOS .app)
  - [x] Launch games from the desktop app — Play button in detail view, play history recording, launch errors surfaced in UI
  - [x] Wine prefix support in desktop app — editable per-game prefix, `PlayGame` honors the DB-stored prefix
  - [x] Directory watcher — fsnotify watches configured scan paths; debounced file changes trigger incremental rescans (insert/update/remove) with live library refresh
  - [x] Nullable game-field editing — EditGame fields are `null` = unchanged / `''` = clear, so wrong exe paths and stale notes can finally be cleared from the detail view without wiping other fields (F95-o3lr)
  - [x] Update check errors surface in the UI — a failed GitHub API check shows the error instead of a false "Moxie is up to date" (F95-hbpq)
  - [x] Single-flight update pipeline — per-run guard shared by single/batch update and install; the batch view recovers cleanly when the batch fails before starting (F95-p9xl, F95-r1sx)
  - [x] Manual update fallback in desktop — when auto-download fails (Cloudflare-blocked host, dead link, missing cookies), the update pipeline emits `game-update:manual-required` and the UI offers "Provide file…" / "Choose Downloaded Archive…"; `ProvideUpdateFile` opens a native file picker and resumes the pipeline from extraction, mirroring the CLI's `moxie install <id> <path>` (F95-*)
  - [x] Context menu "Set Status" submenu reachable — click propagation stopped at the menu container (F95-qxui)
  - [x] **Buzzheavier token-based resolver (F95-hs4y)** — live-verified end-to-end 2026-08-09: share page (retry through adaptive 403s) → server-signed `t=` token from `hx-get` → HTMX `/download?t=&alt=true` → `hx-redirect` to fafda.to (challenge-free, app-layer token gate); `Range: bytes=0-0` probe with escalating 503 backoff, 403/404 JSON → share-page refetch for a fresh token (bounded). 10 new tests incl. token-extraction table, retry-on-403, alt=true preference, JSON error mapping; live acceptance: bzzhr.to/e2yt4zd66jq3 → fafda.to → 206 with 64 KiB of the 943 MB 7z
  - [x] **Browser fallback for challenge-graded hosts (F95-675j)** — `HostResolver.SetBrowserFallback`/`SetDefaultBrowserFallback` hook invoked once per download on Cloudflare challenges (file-hop Cf-Mitigated / 403 challenge body, or resolver-stage challenge/captcha failure); `browserresolve` gains a **raw-launch Firefox engine** (zero deps: profiles.ini `Default=1` discovery, profile copy keeping cookies.sqlite+WAL, user.js download prefs, `--headless -profile <copy> -no-remote`, `.part`-aware download polling, headless→headful escalation with xvfb-run, process-tree teardown per OS), Edge/Brave profile roots for the Chrome-family engine, and per-URL engine selection (cookie-holding browser → Chrome → Firefox; `MOXIE_BROWSER=auto|chrome|firefox`, `MOXIE_FIREFOX_BIN`/`MOXIE_FIREFOX_PROFILE_DIR` overrides). Wired into TUI download, CLI `download`, and the desktop app. Live-verified 2026-08-09: real Firefox 153 headless through the full pipeline; known limitation — hosts needing a button click (vikingfile/datanodes forms) need future CDP click automation

### Known Limitations

- **Sync public endpoints are undocumented** — `checker.php` and `latest_data.php` are F95Zone's own endpoints used by its Latest Updates page; they could change or be locked down. The 3-layer fallback (bulk API → F95Checker cache API → direct scrape) degrades gracefully, and `checker.php` hard-caps at 100 thread IDs per request.
- **F95Checker cache API is third-party** — `api.f95checker.dev` is run by the F95Checker project (one maintainer, open source) and is the same service that project's thousands of users rely on. Its `/full` endpoint 404s transiently (handled with one retry) and returns numeric fields as strings (handled by lenient parsing). If it goes down, sync falls back to direct scraping.
- **Threads not in the Latest Updates index** — games whose threads checker.php doesn't track (about 20% of a typical library) rely on the cache API or, failing that, cookie-based scraping. A tiny fraction of threads (deleted/privated) are unresolvable through any path.
- **Mega downloads binary-gated** — Mega's encrypted protocol is handled via a `megatools dl` subprocess when the binary is installed; without it, Mega links are deprioritized to -200 in host scoring and the downloader auto-fallbacks to the next-best link. Arch users need the AUR package (`megatools`/`megatools-git` — not in the official repos).
- **Download feature is BETA** — Verified working without a browser (2026-08-09): Pixeldrain, Mediafire, Workupload, Gofile, Google Drive, Catbox, **Buzzheavier** (token flow, live-verified: resolve + file hop), masked-link unwrap + megatools. **Browser fallback (F95-675j)**: challenge-graded hosts download inside the user's real browser (Chrome-family via rod, or raw-launch Firefox; per-URL engine selection; `MOXIE_BROWSER=auto|chrome|firefox`) when the Go path hits a Cloudflare challenge — live-verified with Firefox 153. Remaining wall: **vikingfile/datanodes free-download buttons** need CDP click automation (the raw-launch engine only auto-downloads URLs that start on navigation). uTLS was verified NOT to help (A/B: stdlib 4/4 pass vs uTLS 4/4 challenged — the h1-only ALPN makes an "impossible browser"). The fallback loop tries all available links and shows detailed per-host errors. When all links fail, use `moxie install <id> <path>` with a manually downloaded archive — the desktop app offers the same path via the "Provide file…" button in the Updates view and detail view.
- **Testability** — 130+ `os.Exit(1)` calls remain in CLI wrappers; `RunPlay`, `RunScan`, `RunSync` extracted as testable logic functions; other commands still need extraction
- **False positives** — tool/editor directories and generic folder names may be misdetected as games
- **No archive scanning** — `.zip`/`.rar`/`.7z` at scan roots are not inspected (but can be extracted after download)
- **No content-based dedup** — same game in multiple paths creates duplicate records
- **Games added via F95Zone browser are virtual** — no local filesystem path until downloaded
- **Non-UTF-8 filenames** — Latin1/Shift-JIS display incorrectly in the TUI
- **Commands package** — 130+ `os.Exit(1)` calls in CLI wrappers make the full I/O layer untestable; `RunPlay`, `RunScan`, `RunSync` extracted with remaining handlers following the same pattern
- **F95Zone browser check blocks old User-Agents** — `/sam/latest_alpha` rejects outdated browsers with a wall; Our scraper and Playwright tests need modern UA strings
- **Desktop install scripts** — Windows Start Menu shortcut implemented via PowerShell; macOS `.app` bundle auto-generated with `.icns` but not yet tested on real hardware
