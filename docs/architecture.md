# Architecture

## What

moxie is a local game library manager. It scans directories for installed games, detects what engine each one runs on, optionally matches games to F95Zone threads for metadata, and presents everything in a terminal UI. The backend (scanning, scraping, database) and the frontend (TUI) share a single Go binary — no server, no daemon, no runtime dependencies.

## How It Connects

```
┌────────────────────────────────────────────────────────────────────────────┐
│                          main.go (CLI)                                     │
│  Parses flags, opens DB, calls one of 22 command handlers                 │
└──────┬──────────┬──────────┬──────────┬──────────┬──────────┬──────────────┘
       │          │          │          │          │          │
       ▼          ▼          ▼          ▼          ▼          ▼
┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐
│ scanner  │ │ engine   │ │ db       │ │ scraper  │ │ tui      │ │ steam    │
│ WalkDir  │ │ detect   │ │ SQLite   │ │ HTTP     │ │ Bubble   │ │ shortcuts│
│ size     │ │ profiles │ │ CRUD     │ │ parse    │ │ Tea      │ │ VDF      │
│ find exe │ │          │ │ migrate  │ │ search   │ │          │ │ SGDB API │
└────┬─────┘ └────┬─────┘ └──────────┘ └────┬─────┘ └──────────┘ │ artwork  │
     │            │                          │                    └──────────┘
     └─────┬──────┘                          │
           │                                 │
           ▼                                 ▼
    ┌──────────┐                    ┌──────────────┐
    │ engine   │                    │ browser      │
    │ Result   │                    │ kooky cookie │
    └──────────┘                    │ extraction   │
                                     └──────────────┘
```

`config.json` is stored in the platform-standard config directory (Go's `os.UserConfigDir`) — `~/.config/moxie/` on Linux, `%APPDATA%/moxie/` on Windows, `~/Library/Application Support/moxie/` on macOS. The `internal/config/` package provides typed get/set access to settings like the SteamGridDB API key.

### Data Flow Through the System

1. **Scan** — `scanner.Scan()` walks a directory tree using `filepath.WalkDir`. For each directory that looks like a game root (has executables or engine markers), it calls into `engine.Detect()` which checks 20+ detection profiles in priority order. Results: `DetectedGame` structs with title, path, engine, exe, and byte size.

2. **Store** — `db.InsertGame()` writes a `Game` row to SQLite. The game's path is unique (duplicate paths are skipped on subsequent scans). Title is sanitized via `scraper.SanitizeTitle()` before saving.

3. **Scrape** — `scraper.Client.ScrapeThread()` sends an authenticated HTTP GET to an F95Zone thread URL. The `cookieTransport` injects the `Cookie` header from kooky-extracted browser cookies. `goquery` parses the XenForo HTML into a `ThreadData` struct (title, version, developer, tags, overview, cover URL, download links).

4. **Associate** — `scraper.FindMatches()` finds unassociated games, sanitizes their titles, searches F95Zone, scores candidate threads by title similarity (exact=1.0, contains=0.85, word overlap=proportional), and auto-accepts the best match.

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

## Future

The planned Wails desktop path keeps all `internal/` packages unchanged. A new `internal/desktop/` entry point and a Svelte/React `frontend/` directory would add cover image display, native dialogs, system tray, and NSFW blur — all on top of the same scan/scrape/store pipeline.
