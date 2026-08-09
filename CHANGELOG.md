## [Unreleased]

### Added

- **Desktop app logs now reach the per-day file** — `log.Init`/`InitWithConsole` call `slog.SetDefault`, so the desktop app's stdlib `slog.*` calls (startup, sync, cover server, cover downloads) land in `~/.config/moxie/logs/moxie-YYYY-MM-DD.log` instead of stderr only. `SetLevel` also preserves file output now
- **`MOXIE_LOG_LEVEL` env var** — `debug|info|warn|error` sets the startup log level for both CLI and desktop (default `info`); `-v` on the CLI still enables debug
- **Cover-fetch logging** — `fetchCoversRun` logs run start/complete with counts and elapsed, one `cover resolved` line per game (source + per-game elapsed), per-path resolve failures with errors, `cover cached` with bytes/elapsed, and thumbnail backfill summaries. Stuck runs are now attributable to a specific game
- **Scraper pacing debug log** — rate-limit waits between requests are logged at debug level with duration and URL
- **AVIF cover support** — F95Zone's CDN serves AVIF-encoded images under `.png`/`.jpg` cover URLs; `knownImageFormat`/`imageMimeFromPrefix` now sniff the ISOBMFF `ftyp` box + `avif`/`avis` brand, so those covers are cached and served with `image/avif` content type instead of being rejected as "not an image" (thumbnails skipped for AVIF — no pure-Go decoder; the cover server falls back to the full image)
- **Downloads view timeout** — `loadData` fails observably after 20s instead of showing "Loading download links…" forever when a binding call never settles
- **Download link names carry section + platform context** — threads like "Henteria Chronicles" list the same host once per part/platform row (`<b>Part 2</b> … <b>Win</b>: GOFILE - MEGA - …`), so bare anchor text can't tell links apart. The parser now recovers the nearest preceding section heading and the row's platform label and names links accordingly ("DOWNLOAD Part 2 · Win", "Part 1 Win"); real filenames in anchor text are kept. Names refresh on re-sync (diff-and-apply now updates names for unchanged URLs), and labeled rows get correct platform classification via `DetectPlatformFromLink`
- **Desktop "Full sync" option** — `SyncAllGames(force)` mirrors the CLI's `sync --force`: a checkbox in the Sync dialog bypasses the 24h cooldown for both phases and cookie-scrapes every associated game's thread, which also refreshes download links for the Downloads tab (the cheap bulk/cache-API paths never see links). The scrape path (`syncPhase2ScrapeOne`) now diff-and-applies scraped links for every game it checks, not just per-game syncs
- **Download-link binding logging** — `GetGamesWithDownloadLinks`/`GetAllDownloadLinks` log query failures and (at debug level) row counts
- **Downloads tab null-slice crash fixed** — `GetGamesWithDownloadLinks` returned a nil slice with an empty links table, which Wails marshals to JSON `null`; the view then crashed on `.length` (frozen "Loading download links…" spinner). Both slice bindings now return `make(..., 0, ...)` non-nil slices, and the view guards with `?? []` (F95-bug from empty-library testing)

- **Scanner version extraction from files** — `ExtractVersionFromDir` reads `Game.ini` `Title=` field (RPG Maker), `package.json` `"version"` field (HTML/NW.js), and `game/options.rpy` `config.version` (Ren'Py) when directory name has no version (F95-kq8)
- **Parent directory name fallback** — scanner now checks parent directory name for version when game dir has none, catching nested game structures like `Game v1.0/Game Windows/Game.exe` (F95-kq8)
- **Executable filename version extraction** — scanner checks exe names for embedded versions like `[Full]EmberDoors_v0.1.7_Linux.x86_64` → `0.1.7` (F95-kq8)
- **Compact YYYYMMDD date pattern** — `Data20260403` detected as valid date version with month/day validation (F95-kq8)
- **Single/double-digit version pattern** — `v5`, `v01`, `v0` now detected from directory names (F95-kq8)
- **Trailing build letter support** — `v0.7.7i` captured as `"0.7.7i"` instead of missed (F95-kq8)
- **Expanded bracketed-title version extraction** — per F95Zone title format rules: supports `[YYYY-MM-DD]`, bare `[X.Y]`, `[Final]`, `[Ch. 2 v3.0]`, and `[v1.0 Alpha]` patterns (F95-kq8)
- **Application-wide per-day logging** — `internal/log/` enhanced with `Init(dir)` that creates `moxie-YYYY-MM-DD.log` files in `~/.config/moxie/logs/`. Wired via `config.LogDir()` at startup. Instruments download attempts, fallbacks, resolve failures, extraction, and merge throughout the downloader, CLI, and TUI packages (F95-nle)
- **Download fallback loop** — CLI and TUI now iterate through all available download links in platform-priority order instead of failing on the first error. Failed links are logged and skipped; the loop only exits when all links are exhausted. Shows per-link error summary with manual download fallback hint (F95-nle)
- **Download validation** — `IsValidGameFile()` rejects interstitial HTML pages (minimum 4096 bytes, must be an archive format or executable). Prevents ~80 KB HTML ad pages from being silently "downloaded" as game files (F95-nle)
- **`moxie install <id> <path>` command** — Manually downloaded archives can now be fed through the full pipeline: validate format → extract → merge into game directory (preserving saves/mods/configs) → update DB version. Reuses `archive.Extract()` and `updater.Merge()` (F95-nle)
- **`moxie play <id|name>` fuzzy name search** — Now accepts game name in addition to numeric ID. Uses `db.SearchGames()` for LIKE-based title matching. Multiple matches prompt the user to pick from a numbered list (F95-nle)
- **`internal/updater/` package** — `Merge()` merges extracted game files into existing game directories, preserving user saves, mods, and configs based on 14 engine-aware glob patterns (e.g., `game/saves/*` for Ren'Py, `save/*` + `Game.ini` + `package.json` for RPGM). Creates `.old` backup directory (F95-nle)
- **Google Drive resolver** — Two-step confirm token extraction for files >100 MB. Handles virus-scan interstitial page by parsing `uc-download-link` and extracting the confirm token. Falls through to direct UC URL for small files (F95-nle)
- **DataNodes resolver** — Cookie + POST flow: scrapes download page for hidden form fields, submits with session cookies, follows redirect to CDN URL. Handles both `/download/<CODE>` and `/<CODE>/<filename>` URL formats (F95-nle)
- **MixDrop resolver** — Pass-through download for archive files with User-Agent header (beta) (F95-nle)
- **VikingFile resolver** — Form POST scraper that extracts hidden fields (`op`, `id`, `rand`, `method_free`) from the download page, submits them, and follows redirects to the CDN URL. Marked beta — blocked by Cloudflare anti-bot on most requests (F95-nle)
- **Masked F95Zone URL resolution** — `HostResolver.Resolve()` detects `f95zone.to/masked/` URLs, performs a HEAD request to follow the redirect through to the real host URL, re-identifies the host, and recursively applies host-specific resolution (F95-nle)
- **F95Zone cookie wiring through download pipeline** — Browser cookies from `browser.GetF95Cookies()` are threaded through the entire call chain: `main.go` → `tui.Run()` → model → `startDownloadCmd()` → `DownloadWithHost()` → `HostResolver.SetF95Cookie()` → `followRedirect()`. Authenticates HEAD requests to masked F95Zone URLs (F95-nle)
- **Host scoring reorganization** — New priority tiers based on verified downloadability: **+25** (verified working: pixeldrain, buzzheavier, gofile, catbox), **+10** (may work: datanodes, google drive, mixdrop), **0** (default: everything else), **-200** (known not to work via HTTP: mega, vikingfile, workupload, krakenfiles, bunkrr). Deprioritized hosts are tried last so failed downloads are fast and actionable (F95-nle)
- **TUI step-by-step download status display** — `activeDownload.stepMsg` provides live status updates in the detail view: "Finding suitable host..." → "Trying: Pixeldrain..." → "Downloading..." (with progress bar) → "Extracting archive..." → "Merging into game directory..." → "✓ Download completed!" or "✗ All links failed" with per-link error summary (F95-nle)
- **Direct host verification** — Research confirmed only 6 of 18 previously assumed "direct" hosts are truly direct HTTP downloads (Catbox, Transfer.sh, Quax, Vern, YourFileStore, Files.dp.ua). The rest have Cloudflare, captchas, countdown timers, or interstitial pages. Downloader docs updated with full 44-host feasibility matrix (F95-nle)

### Fixed

- **Full-app security & robustness review (2026-08-09)** — 26 findings across all packages closed (7 High, 14 Medium, 5 grouped sweeps):
  - **Security:** the F95Zone session cookie is now host-scoped — `cookieTransport` only attaches it to `f95zone.to` / `*.f95zone.to` / `api.f95checker.dev`. It previously went to every request, including the Google SERP fallback and all redirect hops (Go's cross-domain header stripping doesn't cover transport-level injection). The desktop's cover fetcher matches cover hosts by hostname, not substring. The CLI self-update now stages in a private 0700 config dir (was a predictable `/tmp` path, symlink-TOCTOU) and verifies GitHub's SHA-256 digest; backup-restore failures surface instead of being swallowed. ZIP extraction rejects zip bombs (100:1 per-entry compression ratio cap + 100 GB total cap). Download SSRF blocklist extended (`fd00:ec2::254`, `0.0.0.0`, `::`).
  - **Data integrity:** `PRAGMA foreign_keys=ON` and `busy_timeout` now apply per-connection via DSN `_pragma` — concurrent connections previously ran with FK off, silently orphaning cascade deletes. The Godot engine migration pins one connection for its FK OFF/ON + rebuild. `BatchUpdateStatus` no longer touches soft-deleted games; `AllDownloadLinks` excludes them. `RecentPlays`/`PlaysForGame` parse SQLite timestamps (were zero times). Played timestamps round-trip.
  - **Functional breakage:** the Google Drive host label mismatch (`googledrive` vs `google drive`) broke every Drive download — labels unified, legacy labels normalized, regression test added. `Proton Experimental`/`Hotfix` tool IDs are lowercased so Steam actually applies them. `updater.Merge` preserves executable bits (Ren'Py `.sh` launchers stopped launching after updates) and refuses to run when the `.old` rollback backup cannot be created — the live directory is never overwritten with nothing to restore. `set-status <id> <status>` validated the game ID as the status and could never succeed — fixed and re-structured. Batch download counts real failures instead of always printing "N/N". TUI downloads report extract/merge failures as failed instead of "✓ completed". Downloads over 50 GB are rejected instead of silently truncated as success.
  - **Hangs & blocking:** shutdown no longer hangs on in-flight scans (`scanner.Scan*` take a context; the watcher's `Stop` is grace-bounded). Downloads are cancellable via `DownloadWithContext` — the desktop's update pipeline was already ctx-aware but the HTTP transfer ignored it. The TUI's "g" key no longer scrapes F95Zone synchronously inside `Update` (rate-limited scrape froze the whole UI); it resolves links in a background `tea.Cmd`. `DownloadUpdate` runs on the background pipeline with throttled progress events (was blocking + thousands of IPC events). Interrupted `sync` cancels its workers and joins them before returning.
  - **Steam:** a byte-identical copy of a real Steam-generated `shortcuts.vdf` (12 entries) is committed as `testdata/shortcuts_real.vdf`; the fixture test exposed serializer drift (missing canonical field order, nondeterministic map-order output) — serialization is now deterministic with Steam's canonical field order. Backups are pruned to 5; grid image downloads are size-capped.
  - **Misc:** zip-bomb/50GB guards, `Resolve` recursion bound, filename percent-decoding, shared download transport, host identification matches parsed hostnames (short needles like `k2s`/`dp.ua`/`terminal`/`bunkr` no longer false-positive on unrelated URLs), title matching requires word-boundary containment (`Aurelia` vs `Aurelian Nostrum` no longer scores 0.85), Chrome/Chromium/Brave/Edge cookies are now readable, dead `tui/keys.go` removed, spinner animation fixed, `Truncate` no longer panics on n≤0, config/cache writes fsync before rename, `MOXIE_LOG_LEVEL` unaffected by `--unsafe` delay clamp (zero-delay clients stay unpaced), version compare handles hotfix letters (`0.8.1b` > `0.8.1`) and chapter-styled versions (`Ch.4 Free` no longer beats `0.12.0`).
- **Re-syncing a game no longer churns download links** — `SyncSingleGame` deleted and re-inserted every download link on each sync, churning link IDs the frontend holds and silently resurrecting links the user marked dead. Links are now diff-and-applied: unchanged URLs keep their IDs and dead state, new links are inserted, and links that vanished from the thread are removed (F95-tbxe)
- **Dead Wails surface reduced** — the unused `GetGameCountByStatus` binding (and its `StatusCount` type) was removed; bindings regenerated. `Greet`/`GetCoverPath` were already gone. Startup errors were already surfaced via `GetStartupError` in the app shell (F95-taix)
- **Dead `download-complete` event removed** — the update/install pipelines emitted a `:download-complete` event no frontend listener consumed (completion is tracked via `download-progress` and the phase events); the emission is gone. `scan:auto` / `scan:auto-progress` were verified subscribed in the app shell's status bar (F95-wq1n)
- **Play history is queried per game** — `GetGameDetail` already uses the per-game `PlaysForGame` query instead of loading all play history and filtering in Go (verified fixed) (F95-zkxv)
- **gofmt drift cleaned** — `gofmt -l desktop/` reports no files (F95-p1bi)
- **Game update/sync/scan state survives navigation** — update state (`gameStates`, `batchState`, the retry queue) and scan state moved to the app shell like sync state, so navigating away mid-batch keeps events flowing, remounts show real phases, and the UI blocks duplicate concurrent runs (single-flight guards on the backend too) (F95-xaxx)
- **Retry-failed runs sequentially with a live summary** — retries fired one goroutine per game (the backend's pipeline is single-flight by design) and nulled the batch state before finishing. Retries now pump one game at a time, waiting for the backend's idle event between each, with results accumulating in a live batch summary (F95-j0et)
- **Search/preview responses are race-safe** — a slower older response could overwrite a newer one, and cleared fields could resurrect results. GameList and F95Browser now use monotonic request ids: stale responses bail out, clearing the field immediately invalidates in-flight searches, and previews/overview can't leak across selections (F95-zlq7)
- **Batch updates refresh the library exactly once** — every `game-update:complete` during a batch reloaded the whole library (N games → N full reloads). Single updates still refresh per game; a batch refreshes once on `batch-complete` (F95-ehjt)
- **Engine dropdown covers all canonical engines** — the hardcoded list omitted java/adrift/qsp/rags/tads/webgl/others, so those values displayed as the first option (and changing the select would write the wrong engine). Options are derived from engineColors keys; the select binds to the actual engine and renders for engine-less games, which can now be assigned one (F95-lw93)
- **Dedup dialog refreshes the library and survives partial failures** — `onDedupDone` was never called, so the library stayed stale; a mid-loop failure also left already-deleted games rendered. Successful mutations now trigger the refresh; failures reload the groups to resync (F95-z0le)
- **AddGameDialog browse errors are surfaced** — `handleBrowse` awaited `PickDirectory()` unguarded; a failed pick is now caught and shown like other handler errors (F95-o625)
- **Scanner updates are atomic — concurrent manual edits survive** — `upsertDetected` read the whole game row, mutated scanner fields, and wrote the record back, so an edit landing in between (rename/status/notes) was clobbered with stale data. Scans now use a narrow single-statement UPDATE that touches only scanner-owned fields, with the "fill only when unset" checks inside the SQL (F95-wtsq)
- **Self-update works on Windows (staged) and clears stale backups** — `ApplyUpdate` renamed the running exe, which is locked on Windows, so updates always failed there; a stale `.bak` also blocked the copy fallback (O_EXCL). On Windows the update is now staged: a marker file is written and the staged binary spawns as a swap agent that waits for the app to exit, swaps binaries, and relaunches. Stale `.bak` files are cleared before backup on all platforms. Digest verification against GitHub's asset checksum was already in place (F95-y0t0)
- **Manual scans and watcher rescans are single-flight** — a manual `ScanDirectory` while a background rescan was sweeping could race into the same upsert path; both now share a guard, and the watcher skips its sweep when a manual scan holds it (F95-xaxx)
- **Sync no longer misclassifies games from latest_data.php prefixes** — the search API's `prefixes` array was interpreted with the F95Checker Type enum numbering (wrong for that field; even F95Checker only hashes it), so a fresh sync rejected 89/98 real games as `[non-game]` even at 100% self-match. Non-game filtering is title-based, engine signals come from the cache API `type` field only, and the desktop mirror (`pickBestLatestResult` + the engine-consistency check) follows suit (F95-sj8o)
- **Desktop sync no longer exhausts the anonymous request quota** — the public endpoints (search, checker, cache API) ran cookie-free even when real browser cookies were available, so a full library sync died mid-run at F95Zone's anonymous hourly cap. The PublicAPI now carries the detected browser cookies; a cookie-free client remains the fallback when no browser session exists (F95-8vlc)
- **Desktop scan resurrects soft-deleted games** — rescanning a directory whose game was trashed earlier updated the hidden row but never cleared `deleted_at`, so the game stayed invisible while the UNIQUE index blocked re-insertion. `upsertDetected` now restores the row when the directory is found again on disk (F95-colq)
- **Cover thumbnails backfilled for pre-thumbnail caches** — covers downloaded before the thumbnailing change sat on disk without a `.thumb`, so the list view served full-size images until each game was re-fetched. A local backfill pass (startup + end of every cover fetch) generates the missing thumbnails; no network involved (F95-ydh8)
- **Desktop scrape targets validated before rendering** — store/download/release links rendered `href={url}` with no scheme allowlist, so a `javascript:` URL in scraped data could execute script inside the Wails webview (which exposes `window.go`/`window.runtime`). All untrusted href sites across F95Browser, GameDetail, UpdateDialog, and GameUpdatesView now go through a shared `safeExternalUrl` http(s) allowlist; unsafe links are not rendered (F95-8qks)
- **GameDetail edits and remove surface errors** — every save/remove handler logged failures to the console and closed the editor anyway, making failures indistinguishable from success. Errors now show a visible notice and the editor stays open with the user's value for retry (F95-h6i2)
- **GameDetail update/download surface works** — the "Update Available" badge had no action and download-link rows were inert; the detail view now has a Download Update button, clickable download links (opened in the system browser via link ID), and subscribes to `game-update:*` events so the badge refreshes after completion (F95-hjbw)
- **Scanner skips `downloads/` directories** — the downloader's own dirs (where extracted archives accumulate) were scanned as games, polluting fresh libraries with duplicates; `downloads` is now excluded by name (F95-zk5h)
- **Scanner skips tool directories inside game trees** — bundled tools (RPG Maker XP decrypter/uncpacker with SetupMenu.exe) nested in a game's directory tree registered as standalone games; tool-named dirs are now suppressed when a real game shares the same ancestor tree. Standalone tool-named dirs are still scanned (F95-qe0e)
- **Engine detection covers bundled layouts** — JRE-bundled Java games (Lilith's Throne: `jre*/lib/*.jar`), Ruffle-launched Flash games (`ruffle.exe`), and source-repo HTML games (shallow `index.html`) no longer fall back to Others (F95-uh95)
- **Desktop sync state lost on tab switch** — sync progress, per-game results, and the completion summary were owned by the Sync view, so navigating away destroyed them: an in-flight sync vanished from the UI, its result was lost, and returning to the tab could start a second concurrent run. Sync state now lives in the app shell (survives tab switches), the status bar reports live sync progress from any view, and a backend guard rejects concurrent runs.
- **Desktop sync no longer overwrites curated titles from weak matches** — partial/substring associations ('Aurelia' → 'Aurelian Nostrum') used to rewrite the local title and engine with the wrong thread's data. Thread data is now only applied to the title when the match score is strong (≥0.7) with token containment, and engine assignment prefers the reliable cache-API type field (F95-6duz, F95-ytru)
- **Sync engines now come from the cache API `type` field** — `CacheFullThread` ignored the F95Checker type enum (correct numbering), leaving engine detection to the wrong-numbered latest_data.php prefixes. The type field is now parsed and drives both association and the engine-consistency check (F95-ytru)
- **Desktop EditGame can clear fields** — the doc promised empty strings clear a field, but the code silently ignored them, so a wrong executable path or stale note could never be removed. Fields are now nullable (`null` = unchanged, `''` = clear) across the binding and the detail view, with tests for all three semantics (F95-o3lr)
- **Desktop scrape entry points validate URLs** — `GetThreadPreview`/`AddGameFromF95Zone` passed caller-supplied URLs straight into the cookie-carrying scraper, so a forged URL could exfiltrate the F95Zone session cookie. Both now require an `https` URL on `f95zone.to` before any request is made (F95-vavb)
- **Desktop update dialog shows real update-check failures** — `CheckForUpdate` reports GitHub API failures in its `error` field, but the dialog only handled thrown exceptions and rendered "Moxie is up to date" on failure. Errors now surface; the up-to-date block only renders on a successful check (F95-hbpq)
- **Desktop update pipeline is single-flight** — `DownloadGameUpdate`, `DownloadAllUpdates`, and `InstallGame` share an in-progress guard, so two concurrent pipelines can never merge into the same game directory; a test exercises the guard (F95-p9xl)
- **Desktop context menu "Set Status" submenu opens** — clicks on the submenu bubbled to the overlay's click-to-close handler and killed the menu before it rendered. The menu container now stops propagation; clicks outside still close it (F95-qxui)
- **Desktop update batch failure no longer deadlocks the UI** — when the batch failed before starting (e.g. listing updatable games), the error landed on a phantom `gameID: 0` row and the spinner/buttons stayed stuck forever. Batch-level failures now clear the running state, show the error, and re-enable the controls (F95-r1sx)
- **Desktop sync too slow** — auto-association ran every unassociated game through one sequentially-paced search client with no cooldown, so a library with many unassociated or no-match games took minutes per sync and re-searched the same titles every run. Phase 1 now runs on 3 parallel workers (each with its own paced client), skips no-match games within a 24h cooldown, dedupes identical queries, and reuses the persistent association cache shared with the CLI. The bulk version check (the "~100 games in ~2s" path) is unchanged.
- **Desktop sync now aborts on a Cloudflare block** — previously a blocked run ground through the whole queue producing one futile error per game; it now stops, clears the syncing state, and surfaces a single actionable error, matching the CLI's circuit-breaker behavior.
- **Desktop sync enforces engine consistency** — associations whose thread metadata positively contradicts the detected engine are skipped (cache-API type check on the cookie-free path, tags check on the scrape path), matching the CLI.
- **Desktop phase-2 sync covers untracked and thread-ID-less games** — games checker.php doesn't track are version-checked via the cache API and counted as updates; legacy games without a thread ID are cookie-scraped. Version checks now also stamp the 24h cooldown like the CLI, so repeat syncs skip unchanged libraries.
- **CLI direct-scrape version check now strips qualifiers** — `moxie check-updates` fallback path compared unstripped stored versions ("v1.03 + DLC") against clean thread versions, surfacing phantom "ordering unclear" updates; it now strips like the bulk path and the desktop.
- **Desktop single-game sync no longer clobbers curated titles** — `SyncSingleGame` preserved the thread title over a user-curated one; the CLI's single-game check never touches the title, so neither does the desktop now.
- **Auto-scan progress visible** — the watcher's `scan:auto` / `scan:auto-progress` events were emitted but never displayed; the status bar now shows background scanning from any tab.
- **TUI 🔄 update indicator on empty versions** — required `Version != ""` guard prevents false update markers on every game with scraped metadata but no local version (F95-kq8)
- **Scan now updates existing games** — `moxie scan` updates version/engine/size/exe on re-scan instead of skipping, so improved detection takes effect immediately (F95-kq8)
- **`RefreshVersions` file-content fallback** — now calls `ExtractVersionFromDir` matching full scanner logic, not just directory name (F95-kq8)
- **Stale `? no version detected` output** — suppressed in `RunUpdateCheck()` and `SyncGame()` since no user action is needed (F95-kq8)
- **TUI download result never displayed** — Completed/failed downloads were deleted from the `activeDownloads` map immediately on the first poll after finishing, so the detail view never rendered the result. Removed auto-cleanup; results now stay visible until the user navigates away (F95-nle)
- **ZIP archive progress always ~92% at completion** — `totalFiles` counted all ZIP entries including directories. Changed to count only non-directory files so progress reaches 100%. tar.gz extraction already handled this correctly (F95-nle)
- **Clobbered archive progress filenames** — `\r` carriage return overwrite left text bleeding from longer previous filenames. Filenames now truncated to 60 chars with `%-60s` width padding to clear the line (F95-nle)
- **DataNodes regex mismatch** — Expected `/download/<CODE>` URL format but real F95Zone links use `/<CODE>/<filename>`. Resolver now handles both formats (F95-nle)
- **Pixeldrain masked URL never resolved** — `IdentifyHostInURL` matched "pixeldrain" inside the masked F95Zone URL string and routed to `resolvePixeldrain`, which extracted the file ID from the wrong URL. Fixed by detecting `/masked/` in the URL first and following the redirect before applying host resolution (F95-nle)

### Architecture

- **File separation** — 7 monolith files (700-884 lines each) split into 22+ entity-grouped files across all packages. `db/db.go` split into core + games + downloads + download_links + scraped_meta; `downloader/hosts.go` split into one-file-per-host (9 resolvers + helpers); `commands/sync.go` split into check + game + auto; `commands/steam.go` split into add/remove/list/proton/artwork; `scraper/parser.go` split into version/metadata/links; `commands/download.go` split into exec + links + UI (F95-57h, F95-wn7, F95-0s4, F95-37v, F95-rjk, F95-tup, F95-6p9)
- **Domain logic extraction** — engine matching functions (`EngineTagVariants`, `EngineCompat`, `EngineMatchesThread`, `FindF95Engine`, `ExtractEngineFromTitle`, `FormatTagsBrief`) moved from `commands/cleanup.go` to `engine/engine_tags.go`; `StripThreadPrefix` moved to `scraper/title.go`; `ExtractSteamAppID` moved to `steam/appid.go`; `DetectPlatformFromLink` moved to `downloader/detect.go`; `ApplyThreadData` moved to `scraper/apply.go`. All domain logic now lives in proper packages; `commands/` is thin orchestrators (F95-mwa, F95-56u, F95-ee2, F95-3cw, F95-dny)
- **Cross-package deduplication** — `IsOnlineOnly`, `ScoreLinkHost`, `ScoreDownloadLink`, and `FindMostRecentFile` extracted to `downloader/links.go`, eliminating 3 duplicate implementations across `commands/download.go` and `tui/commands.go` (F95-9w0)

### Changed

- **`\b` replaced with `[^a-zA-Z0-9]` boundaries** — Go's regex `\b` treats `_` as word character, breaking underscore-delimited versions like `FullEmberDoors_v0.1.7_Linux` (F95-kq8)
- **Display-layer version fallback** — TUI and CLI show `LatestVersion` when `Version` is empty (no DB backfill), preserving `LatestVersion != Version` update detection (F95-kq8)
- **`shouldSkip` optimized** — exact-match map for `config`/`saved`/`logs`/`crashes`, substring slice for prefix patterns (F95-kq8)
- **Walk path optimized** — single `os.ReadDir` reused across game marker and category checks instead of double-read (F95-kq8)
- **Regex compilation hoisted** — `verIniRE`, `pkgVerRE`, `rpyVerRE` compiled once at package init instead of per-call (F95-kq8)

### Performance

- **Engine profiles loaded once** — `getProfiles` re-read the profiles directory and every JSON file per game directory in every scan pass (a comment promised caching that never existed); profiles are now merged once and cached, invalidated on the profiles-directory mtime change, eliminating hundreds of redundant reads per scan
- **Download feature marked as Beta** — Most file hosts use anti-bot protection (Cloudflare, CAPTCHAs, JS challenges) that HTTP clients cannot bypass. Only Pixeldrain, Buzzheavier, and verified direct hosts have reliable support. All other hosts may fail intermittently. When all links fail, `moxie install <id> <path>` accepts a manually downloaded archive (F95-nle)
- **Updated host resolver table** — `docs/downloader.md` now includes a complete 44-host feasibility matrix with ✅ Direct, ⚡ API, ⚠️ Difficult, and ❌ Impossible ratings based on verified research (F95-nle)
- **Updated architecture and component docs** — `docs/architecture.md`, `docs/tui.md`, `docs/downloader.md`, and `docs/moxie-spec.md` updated for logging infrastructure, updater package, TUI download flow, and download validation (F95-nle)


# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.4.0-alpha] - 2026-07-01

### Added

- **Fuzzy name search for all commands** — Shared `ResolveGame`/`ResolveFirstArg` helpers in `internal/commands/game_lookup.go` (143 lines, 12 tests) power 17 command entry points. Commands like `moxie info`, `moxie download`, `moxie remove`, `moxie steam add` now accept game names in addition to numeric IDs. Backward compatible — numeric IDs still work (F95-yy8)
- **Interactive multi-match picker** — When a fuzzy search returns multiple results (e.g. `moxie info Demon` matches 6 games), a numbered list is shown with ID, title, and engine. User picks by number or enters 0 to cancel (F95-yy8)
- **TTY detection** — `isInteractive()` checks whether stdin is a terminal. When piped (non-interactive), `promptSelectGame` prints matches to stderr and exits with code 1 instead of blocking (F95-ci3)
- **`ConfirmDestructive` helper** — Guards destructive operations (remove, set-exe, set-path, config set-thread) with a confirmation prompt: `[Name Match] Removing 'Demonherd' [ID 88] — confirm? (y/N)`. Skips when `--assume-yes` is set or stdin is not a TTY (F95-5cr)
- **`ResolveFirstArg` variant** — For multi-arg commands like `moxie install <id|name> <archive-path>` — only the first positional token is searched, preventing subsequent args from being consumed as part of the game name (F95-3bu)
- **Multi-word unquoted name support** — All positional args are joined before resolution, so `moxie play Cyan Brain` works the same as `moxie play "Cyan Brain"` (F95-b21)
- **`SelectBestExe` scoring overhaul** — Skips known runtime engines (`nwjc`, `nw`, `node`) and installers (`unitycrashhandler`, `unins`, `setup`). Awards +1 GB score bonus for `Game.exe` — fixes Demonherd launching nwjc.exe instead of Game.exe (F95-b21)
- **`LaunchCommand` working directory fix** — `cmd.Dir` set to game root so Windows games under Wine resolve relative asset paths correctly (F95-b21)
- **Cross-device update fallback** — `replaceBinary` now uses copy+delete when `os.Rename` fails across filesystem boundaries (e.g. `/tmp` on tmpfs → `/home` on ext4) (F95-b21)
- **Updated usage strings** — 12 commands now show `<id|name>` syntax in help text (F95-b21)

### Fixed

- **`cleanExtractPath` test on Windows CI** — Hardcoded Unix `/tmp/dest` paths replaced with `filepath.Join` + `t.TempDir()`, fixing cross-platform test failures (F95-b21)

### Changed

- **CI/CD Go version** — `.github/workflows/ci.yml` and `release.yml` updated from Go 1.24 to 1.26 to match `go.mod` (F95-b21)
- **Documentation updated** — `docs/moxie-spec.md` version bumped to 0.4.0-alpha with 7 new feature entries

## [0.3.52-alpha] - 2026-05-21

### Fixed

- **`moxie update` now uses correct GitHub repo URL** — changed from `mili/moxie` to `Milisource/moxie` to match the actual remote repository. The command was returning HTTP 404 when checking for updates (F95-pjg)

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

[Unreleased]: https://github.com/Milisource/moxie/compare/v0.4.0-alpha...HEAD
[0.4.0-alpha]: https://github.com/Milisource/moxie/compare/v0.3.52-alpha...v0.4.0-alpha
[0.3.52-alpha]: https://github.com/Milisource/moxie/compare/v0.3.5-alpha...v0.3.52-alpha
[0.3.5-alpha]: https://github.com/Milisource/moxie/compare/v0.3.4-alpha...v0.3.5-alpha
[0.3.4-alpha]: https://github.com/Milisource/moxie/compare/v0.3.3-alpha...v0.3.4-alpha
[0.3.3-alpha]: https://github.com/Milisource/moxie/compare/v0.3.1-alpha...v0.3.3-alpha
[0.3.2-alpha]: https://github.com/Milisource/moxie/releases/tag/v0.3.2-alpha
