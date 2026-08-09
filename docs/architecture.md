# Architecture

## What

moxie is a local game library manager. It scans directories for installed games, detects what engine each one runs on, optionally matches games to F95Zone threads for metadata, and presents everything in a terminal UI. The backend (scanning, scraping, database) and the frontend (TUI) share a single Go binary — no server, no daemon, no runtime dependencies.

## How It Connects

```
┌────────────────────────────────────────────────────────────────────────────┐
│                       main.go (CLI entry point)                             │
│  Parses flags, calls commands.* handlers                                    │
└──────┬──────────┬──────────┬──────────┬──────────┬──────────┬──────────────┘
       │          │          │          │          │          │
       ▼          ▼          ▼          ▼          ▼          ▼
┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐
│ scanner  │ │ engine   │ │ db       │ │ scraper  │ │ tui      │ │ steam    │
│ WalkDir  │ │ detect   │ │ SQLite   │ │ HTTP     │ │ Bubble   │ │ shortcuts│
│ size     │ │ profiles │ │ CRUD     │ │ parse    │ │ Tea      │ │ vendored │
│ find exe │ │ tags     │ │ 8 files  │ │ apply    │ │ 12 files │ │ appid    │
│ ver extr │ │ match    │ │ FTS5     │ │ search   │ │ spinner  │ │ artwork  │
│ progress │ │ custom   │ │ soft del │ │ nongame  │ │ launcher │ │ proton   │
└────┬─────┘ └────┬─────┘ └──────────┘ └────┬─────┘ └──────────┘ └──────────┘
     │            │                          │                    └──────────┘
     └─────┬──────┘                          │
           │                                 │
           ▼                                 ▼
    ┌──────────┐                    ┌──────────────┐
    │ engine   │                    │ browser      │
    │ Result   │                    │ kooky +      │
    └──────────┘                    │ SQLite       │
                                     │ extraction   │
                                     └──────────────┘

package main          internal/util/          internal/commands/
    main.go              config.go              crud.go          (scan, list, info, add, remove)
                         helpers.go             scrape.go        (scrape, resolve cookie)
                sync.go          (Sync dispatcher, RunScrapeAuto)
                sync_check.go    (RunUpdateCheck, CheckUpdates)
                sync_game.go     (SyncGameLogic)
                cleanup.go       (engine mismatch, refresh-versions)
                 play.go          (RunPlay, launch, fuzzy name search)
                 install.go       (install from archive → extract → merge → DB update)
                 steam.go         (Steam dispatcher)
                                                 steam_add.go     (steam add)
                                                 steam_remove.go  (steam remove)
                                                 steam_list.go    (steam list, proton-list)
                                                 steam_proton.go  (proton-set)
                                                 steam_artwork.go (fix-artwork, SGDB helpers)
                                                 download.go      (download)
                                                 download_links.go(link selection, dead-link check)
                                                 download_ui.go   (progress bars, formatting)
                                                 rename.go        (rename, FilesystemSafe)
                                                 config.go        (config get/set/show)
                                                 install.go       (install from archive)
                                                 update.go        (self-update)
                                                 dbutil.go        (OpenDB)
                                                 collections.go   (collections add/list/add-game)
                                                 set_status.go    (set-status --engine/--all)
                                                 export.go        (export --output file.json)
                                                 import.go        (import <file.json>)
                                                 history.go       (history [count])
```

`config.json` is stored in the platform-standard config directory — `~/.config/moxie/` on Linux. `internal/util/` provides config I/O and shared formatters; `internal/commands/` contains all CLI command handlers across 27 domain-grouped files.

Additional shared packages:

- **`internal/log/`** — `slog` wrapper with `Init(dir)` that creates per-day log files (`moxie-YYYY-MM-DD.log`) in the platform log directory (`~/.config/moxie/logs/`). Called once from `main()` before any command runs. All download attempts, fallbacks, resolve failures, and completions are instrumented through this logger.

- **`internal/updater/`** — `Merge()` copies files from a downloaded+extracted archive into the game directory, preserving user saves, mods, and configs based on engine-aware glob patterns (14 engines + default fallback). Supports optional `.old` backup with automatic restore of preserved files.

- **`internal/version/`** — Compares F95Zone game version strings against locally-known ones. `Compare()` returns `Same`/`Newer`/`Older`/`Changed` rather than a boolean, because game versions are not semver: real values include `v0.12.0`, `0.8.1b`, `Ch.4 Free`, `2018-07-18` (the date form used when a game has no version number), and `Final`. `Normalize()` folds case, strips a leading `v`, collapses digit separators (`0_8_1` → `0.8.1`) and trailing `.0` segments. Only `Newer` is an unambiguous update; `Older` signals a parse regression or edited thread, and `Changed` means the two versions cannot be ordered. Used by `check-updates`, `sync`, and the desktop app so all three agree on what "an update is available" means.

- **`internal/launcher/`** — Shared game launching logic used by both the CLI (`moxie play`) and the TUI (`p` key). Contains `ResolveExecutable()` (scoring-based exe selection with macOS .app/Mach-O detection), `Launch()` (platform-aware process spawning with Wine/CrossOver), and `detect.go` (platform detection helpers). Extracted from duplicated code that previously lived separately in `commands/play.go` and `tui/helpers.go`.

### Data Flow Through the System

1. **Scan** — `scanner.Scan()` walks a directory tree using `filepath.WalkDir`. For each directory that looks like a game root (has executables or engine markers), it calls into `engine.Detect()` which checks built-in + custom profiles (loaded from `~/.config/moxie/engines/*.json`) in priority order. Results: `DetectedGame` structs with title, path, engine, exe, and byte size. Live progress is reported via `ScanProgressFunc` callback.

2. **Store** — `db.InsertGame()` writes a `Game` row to SQLite. The game's path is unique (duplicate paths are skipped on subsequent scans). Title is sanitized via `scraper.SanitizeTitle()` before saving.

3. **Scrape** — `scraper.Client.ScrapeThread()` sends an authenticated HTTP GET to an F95Zone thread URL. The `cookieTransport` injects the `Cookie` header from kooky-extracted browser cookies. `goquery` parses the XenForo HTML into a `ThreadData` struct (title, version, developer, tags, overview, cover URL, download links). The same cookie is also threaded through the download pipeline — `Download() → DownloadWithHost() → HostResolver.SetF95Cookie() → followRedirect()` — to authenticate F95Zone masked URL HEAD requests during download resolution.

4. **Associate** — `scraper.FindMatches()` finds unassociated games, sanitizes their titles, searches F95Zone, scores candidate threads by title similarity (exact=1.0, contains=0.85, word overlap=proportional), engine-aware scoring (+0.15 boost), and auto-accepts the best match. Engine matching logic (`EngineMatchesThread`, `EngineTagVariants`, `ExtractEngineFromTitle`) lives in `internal/engine/engine_tags.go` rather than the commands layer. After association, `scraper.ApplyThreadData()` copies scraped metadata onto the DB game record.

5. **Check updates** — Re-scrapes every associated game's thread, extracts the version from the structured header block, compares with `latest_version` in the DB, and reports differences.

6. **Browse** — The TUI loads all games into a Bubbles table, then filters/sorts/renders entirely in-memory. The only DB call after load is for individual CRUD operations (delete, update meta, etc.).

## Why

### Go over Rust

Development speed trumps marginal performance gains. For a project spending 99% of its time on disk I/O (walking directories, computing sizes) and HTTP (scraping F95Zone), Go's ergonomics win outright. Compile times: ~1 second vs 15-30 seconds. Cross-compilation: `GOOS=windows go build` — no cross-linker toolchain. Single ~10 MB static binary, no runtime.

### TUI over Web UI

The TUI shares the same Go backend as the CLI. The alternative — a web UI — would have required running a local server daemon, adding complexity for a single-user tool. The Bubble Tea TUI is usable with zero setup: `moxie tui`. When a Wails desktop GUI ships later, all `internal/` packages are reused 100% as-is.

### Cookie import over browser automation

Cloudflare evasion via browser automation (Playwright, Puppeteer, Selenium) is legally risky (CF ToS violation) and technically fragile (undetected-chromedriver version churn). Cookie import works with months-valid sessions, no browser process needed, and is the approach used by similar tools. Firefox auto-detection makes it seamless for the default use case.

### Pattern matching over PE binary scanning

File/folder patterns (`UnityPlayer.dll`, `renpy/`, `Game.ini`) are 95%+ reliable for game engine detection and 10x simpler to implement than PE binary parsing. The tradeoff: false positives from tool directories and generic folder names. Handled via an exclusion list and category directory detection.

### No server, no daemon

The DB is SQLite — single file, no server process. The tool opens it, reads/writes, and closes. No background sync, no auto-watcher, no daemon lifecycle. This keeps the binary small, the startup instant, and the mental model simple.

## Desktop GUI (Wails)

A Wails v2 + Svelte 5 desktop GUI ships alongside the CLI, sharing the same `internal/` packages and `games.db`.

```
main.go                        CLI entry point (unchanged)
desktop/
  main.go                      Wails entry: wails.Run() with 1200×800 window
  app.go                       App struct with 45+ methods bound to frontend (Go ~2100 LOC)
  wails.json                   Wails project configuration
  frontend/                    Svelte 5 app (Vite 6, embedded via //go:embed)
    src/
      App.svelte               Root layout with view routing
      lib/
        Sidebar.svelte         Navigation sidebar (Library, Media, Management)
        GameList.svelte        Sortable game table with search/engine/status filters, cover thumbnails, context menu
        GameDetail.svelte      Cover art, metadata, overview, inline editing, sync, download links
        GameUpdatesView.svelte Game version updates list with per-game and batch update
        F95Browser.svelte      F95Zone game browser with search, preview panel, add-to-library
        DownloadsView.svelte   Download management with expandable game cards, open-in-browser
        ScanDialog.svelte      Saved paths, scan with live progress from Wails Events
        SyncDialog.svelte      F95Zone sync with per-game progress, association, update check
        UpdateDialog.svelte    App self-update checker with download and apply
        AddGameDialog.svelte   Manual add game with directory picker, engine detection, fields
        DedupDialog.svelte     Duplicate game detection and resolution
        StatusBar.svelte       Game count + status messages
        viewState.svelte.js    Tab-surviving view state (filters, sort, scroll, browser session)
      app.css                  CSS custom properties with dark/light auto-detection
  build/                       Platform build assets (icon, macOS plists, Windows manifests)
```

**Build & Install:**
```bash
make desktop                          # production build → dist/moxie-desktop
make install-desktop                  # register in system app launcher
cd desktop && wails dev -tags webkit2_41  # hot-reload development
```
`make desktop` stamps the git descriptor into the binary (`-X main.appVersion=$(VERSION)`), so the sidebar shows exactly which build is running. On KDE, `install-desktop.sh` forces a ksycoca rebuild so the launcher menu reflects new builds immediately instead of waiting on the background watcher.

**Per-platform install behavior** (`scripts/install-desktop.sh`):
- **Linux** → copies to `~/.local/bin`, writes a `.desktop` entry + icon, refreshes the desktop database.
- **macOS** → builds `/Applications/Moxie.app` with a generated `.icns` icon and `Info.plist`. Verify on real hardware with:
  ```bash
  ./scripts/install-desktop.sh
  open /Applications/Moxie.app
  # If Gatekeeper blocks it:  xattr -dr com.apple.quarantine /Applications/Moxie.app
  plutil -lint /Applications/Moxie.app/Contents/Info.plist
  ls /Applications/Moxie.app/Contents/Resources/AppIcon.icns
  ```
- **Windows** → copies to `%APPDATA%\moxie\bin` and creates a Start Menu shortcut via PowerShell (`[Environment]::GetFolderPath('Programs')`, icon points at the installed exe).

**Key design:** Every bound Go method delegates to an existing `internal/` package. The frontend is purely a view layer — all business logic stays in the shared Go backend. The Svelte frontend communicates with Go exclusively through auto-generated `wailsjs/` bindings (no REST, no IPC).

**Cover art pipeline:** Covers are cached to `~/.config/moxie/covers/<gameID>` and served to the webview over loopback HTTP (`/cover/<id>` full image, `/cover/<id>/thumb` list thumbnail; missing `.thumb` falls back to the full image). F95Zone's CDN serves many covers as **AVIF** regardless of the URL extension — the webview (Chromium) renders AVIF natively, but Go has no pure-Go AVIF decoder (libavif is CGO, banned), so AVIF covers are cached and served in full and simply get no thumbnail: `decodeCoverImage` returns a typed `errCoverFormatNotThumbnailable`, `writeCoverThumb` skips them silently, and `backfillCoverThumbs` reports one `skippedAVIF=N` summary line instead of a per-cover WARN storm (F95-x2mg).

**F95Zone browser search flow:** `F95Browser.svelte` calls `App.SearchF95Zone(query)` which runs a **two-phase lookup**: (1) the XenForo POST search (`scraper.Client.SearchF95Zone` — requires the fresh `_xfToken` fetched from page HTML, see `docs/scraper.md`), and (2) a catalog cover lookup (`scraper.PublicAPI.SearchCovers`) whose `thread_id → cover` map is matched to results by `scraper.ThreadIDFromURL`. Results the catalog doesn't know (mods, requests) get an empty thumbnail → UI placeholder. Searches are **explicit-only**: typing updates the query, only the Search button / Enter calls the backend, and remounting the tab never re-runs a persisted query — in-flight responses are still guarded by the shared `searchSeq` counter in `viewState.svelte.js`.

**State across tabs:** Views are destroyed on every tab switch, so state lives at two levels. Long-running operations (sync, scan, cover backfill, update pipeline) keep their state in `App.svelte` itself — the dialog views only render it, so an in-flight run survives navigation and the backend's single-flight guards stay enforced. Interactive view state (library search/filters/sort/scroll, browser search+preview, downloads expansion, expanded collection) lives in `lib/viewState.svelte.js` as module-scope `$state` objects (`library`, `browser`, `downloads`, `collectionsView`). Exported as containers rather than individual runes: the Svelte compiler rewrites runes one file at a time, so a directly-exported binding can't be reassigned from an importing component. The browser's request sequence counters also live in shared state, so a stale in-flight response can't land in a freshly remounted view.

**View transitions:** The active view is wrapped in `{#key activeView}` with a `fly` transition (140ms, 8px rise + fade), so tab switches crossfade instead of hard-swapping. `prefers-reduced-motion` users get a 0ms hard switch.

**Browser design reference:** See [`docs/f95zone-browser-design.md`](f95zone-browser-design.md) for a detailed analysis of F95Zone's `latest_alpha` page structure and UX patterns, used as inspiration for the `F95Browser.svelte` component.
