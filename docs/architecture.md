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
      app.css                  CSS custom properties with dark/light auto-detection
  build/                       Platform build assets (icon, macOS plists, Windows manifests)
```

**Build:**
```bash
make desktop                          # production build
cd desktop && wails dev -tags webkit2_41  # hot-reload development
```

**Key design:** Every bound Go method delegates to an existing `internal/` package. The frontend is purely a view layer — all business logic stays in the shared Go backend. The Svelte frontend communicates with Go exclusively through auto-generated `wailsjs/` bindings (no REST, no IPC).

**Browser design reference:** See [`docs/f95zone-browser-design.md`](f95zone-browser-design.md) for a detailed analysis of F95Zone's `latest_alpha` page structure and UX patterns, used as inspiration for the `F95Browser.svelte` component.
