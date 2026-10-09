# moxie — Game Library Manager

[![Go 1.26+](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Platforms](https://img.shields.io/badge/platforms-Linux%20%C2%B7%20macOS%20%C2%B7%20Windows-6b5ce7)](#install)
[![License: WTFPL](https://img.shields.io/badge/license-WTFPL-blue)](LICENSE)
[![Release](https://img.shields.io/github/v/release/Milisource/moxie)](https://github.com/Milisource/moxie/releases)

**Scan, catalog, enrich, and launch your local game library — from a desktop app or your terminal.**

moxie is an engine-aware game library manager. It recursively scans local folders, detects 15 [F95Zone-canonical](https://f95zone.to) engine types (Unity, Ren'Py, RPG Maker, Godot, and more), stores metadata in an embedded SQLite database, and optionally enriches every entry with version info, tags, and cover art from community threads. Games can be launched directly or added to Steam with artwork and Proton configuration — no manual folder wrangling.

One Go module ships **two front-ends**: a cross-platform desktop app (Wails + Svelte) and a CLI with a built-in terminal UI.

**[Features](#features) · [Desktop app](#desktop-app) · [CLI & TUI](#cli--tui) · [Install](#install) · [Quick start](#quick-start) · [Command reference](#command-reference) · [Configuration](#configuration) · [Documentation](#documentation)**

> **Status:** alpha (`0.4.0-alpha`). The library, scanner, F95Zone sync, cover art and Steam integration are stable; the download manager and desktop app are actively evolving. Bug reports and ideas are welcome — see [How to get help](#how-to-get-help).

---

## How it works

With moxie you can **scan** local game directories, **enrich** them with metadata from community threads, **track** version updates, and **launch** games — all from the desktop app or a terminal UI / CLI.

Unlike manually organizing game folders and checking announcement threads one by one, moxie automates the entire pipeline:

```
┌──────────┐     ┌──────────────┐     ┌───────────────┐     ┌──────────┐
│  Scan    │────►│  Scrape      │────►│  Sync +       │────►│  Desktop │
│  ~/Games │     │  Web Thread  │     │  Check Updates│     │  TUI/CLI │
└──────────┘     └──────────────┘     └───────┬───────┘     └──────────┘
                                               │
                                               ▼
                                        ┌──────────────┐     ┌──────────┐
                                        │  Steam Add   │────►│  Play    │
                                        │  + Artwork   │     │  Launch  │
                                        └──────────────┘     └──────────┘
```

---

## Features

| | |
|---|---|
| **Engine-aware scanning** | Detects 15 F95Zone-canonical engine types — Unity, Ren'Py, RPG Maker (RPGM), Godot, Unreal, HTML, Java/JRE, WebGL, Flash, QSP, RAGS, Tads, ADRIFT, WolfRPG — plus an `Others` fallback. Content-sniffing recovers HTML/Twine roots, and non-F95 community engines map to `Others` with a descriptive match reason. |
| **Incremental by default** | `moxie scan <dir>` skips directories whose modification time hasn't changed. Use `--force` for a full re-detection. Reports byte-exact sizes and finds executables (including nested ones and HTML entry pages). |
| **Metadata enrichment** | Scraping pulls version, developer, tags, overview, cover art, and store links from community threads. Auto-association scores search results by title and engine, and picks the best match. |
| **Cover art** | Keeps a portrait cover *and* a landscape banner per game, and can upgrade landscape/missing/low-res covers to real box art from Steam (keyless), SteamGridDB (API key) and VNDB (opt-in) on an exact title match. |
| **Download manager** | Download from supported hosts with resume, platform priority, multi-part handling, and dead-link fallback. Cloudflare/Turnstile-gated hosts fall back to a real browser. |
| **Steam integration** | Add non-Steam games with deterministic AppIDs, grid artwork, and Proton configuration. Safe VDF read/write with automatic backups. |
| **Two front-ends** | A native desktop app (Wails + Svelte 5) and a keyboard-driven Bubble Tea TUI — both over one SQLite library. |
| **Cross-platform** | Single static, CGO-free Go binary for the CLI/TUI. Linux, macOS, and Windows (amd64 + arm64). |

---

## Desktop app

A native Wails v2 + Svelte 5 app with a violet "archive / catalog" visual system, bundled IBM Plex type, and keyboard-first navigation. Its sidebar covers the whole workflow:

- **Library** — Grid, Wide and List layouts with engine/status filters, search and sort.
- **Browse** — a live F95Zone Discover feed and search, with a thread preview and **Add to Library**.
- **Scan** — manage scan paths and trigger incremental or full rescans.
- **Add Game** — add a game by path, or from the F95Zone browser.
- **Updates** — games with a newer version, with Update All and parallel updates.
- **Sync** — auto-associate and refresh metadata for the whole library.
- **Downloads** — download history and install/update progress.
- **Covers** — fetch missing covers and upgrade to portrait art.
- **Collections**, **Duplicates**, **Trash** and **Settings** round out library management.

The desktop app also supports in-app self-updates and watches your game folders for changes.

> **Availability:** on Windows, the combined installer (below) ships the desktop app and the CLI together. On Linux and macOS, build the desktop app from source with `make desktop` (see [Install](#install)).

---

## CLI & TUI

The same engine is available from the terminal. Run `moxie` with no arguments for the full command reference, or launch the interactive TUI:

```bash
moxie tui
```

### TUI keyboard shortcuts

| Key | Context | Action |
|-----|---------|--------|
| `↑` / `k` | Library | Move selection up |
| `↓` / `j` | Library | Move selection down |
| `Enter` | Library | Open detail view |
| `Esc` / `←` | Any | Return to the library list |
| `/` | Library | Start search/filter |
| `s` | Library | Cycle sort field |
| `d` | Library | Delete game (with confirmation) |
| `e` | Detail | Edit game title |
| `p` | Detail | Launch game |
| `Ctrl+u` | Detail | Set the game's thread URL |
| `Ctrl+E` | Library | Cycle engine filter |
| `Ctrl+S` | Library | Cycle status filter |
| `c` | Library | Cycle collection filter |
| `?` | Any | Toggle the help overlay |

Press `?` in the TUI for quick-start CLI commands (scan, scrape, steam add, …).

---

## Install

### Desktop app + CLI (Windows)

Download **`Moxie-Setup.exe`** from the [latest release](https://github.com/Milisource/moxie/releases/latest) and run it. It installs the desktop app and the `moxie` CLI to `%LOCALAPPDATA%\Programs\Moxie` (per-user, no admin), adds the CLI to your PATH, and embeds the WebView2 bootstrapper (already present on a normal Windows 10/11 install). One download works on both x64 and ARM64.

### CLI only

**macOS / Linux:**

```bash
curl -fsSL https://raw.githubusercontent.com/Milisource/moxie/main/scripts/install.sh | bash
```

The script downloads the latest pre-built binary for your platform to `~/.local/bin/` and adds it to your shell config. Restart your terminal or run `source ~/.bashrc`.

**Windows (PowerShell, not Command Prompt):**

```powershell
irm https://raw.githubusercontent.com/Milisource/moxie/main/scripts/install.ps1 | iex
```

CLI-only mode downloads `moxie.exe` to `%LOCALAPPDATA%\moxie\bin\` and adds it to your user PATH; pass `-Desktop` to run the bundled installer instead.

**Pin a version / install a local build:**

```bash
# Pin a specific release
curl -fsSL https://raw.githubusercontent.com/Milisource/moxie/main/scripts/install.sh | bash -s -- --version v0.4.0-alpha

# From a local build
./scripts/install.sh --binary ./dist/moxie
```

**Available flags:** `--version <ver>`, `--binary <path>`, `--no-modify-path`, `--help`.

### Dev channel (`moxie-dev`)

There are two release channels. They install side by side and are kept entirely separate, so a dev build can never damage a stable install:

| Channel | Binary | Data directory | Source |
|---------|--------|----------------|--------|
| Stable (main) | `moxie` | `~/.config/moxie` / `%APPDATA%\moxie` | GitHub Releases |
| Dev | `moxie-dev` | `~/.config/moxie-dev` / `%APPDATA%\moxie-dev` | `dev` branch |

The dev channel builds from source, so it needs git and a Go 1.26+ toolchain:

```bash
git clone https://github.com/Milisource/moxie.git
cd moxie
./scripts/install-dev.sh          # build this checkout → ~/.local/bin/moxie-dev
./scripts/install-dev.sh --clone  # or shallow-clone the dev branch first
```

Windows (PowerShell):

```powershell
.\scripts\install-dev.ps1
.\scripts\install-dev.ps1 -Clone
```

**Windows dev desktop app:** download **`Moxie-Dev-Setup.exe`** from the rolling [`dev` release](https://github.com/Milisource/moxie/releases/tag/dev). It installs **Moxie Dev** (desktop + `moxie-dev` CLI) side by side with a stable install, using `%APPDATA%\moxie-dev` so it never touches stable data.

Because the dev build has its own database, a dev schema migration can never strand the stable install. `moxie-dev` also refuses to self-update from stable releases.

### Build from source

```bash
git clone https://github.com/Milisource/moxie.git
cd moxie

# One command: build and install the CLI + desktop from this checkout
./scripts/install-local.sh

# Or step by step
make build                    # produces dist/moxie
sudo make install             # copies the CLI to /usr/local/bin/moxie
make desktop                  # Wails desktop build → dist/moxie-desktop
./scripts/install-local.sh --skip-desktop   # CLI only

# Or build the CLI manually
CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=$(git describe --tags --always)" -o moxie .

# Cross-compile all platforms
./scripts/build.sh all
```

`scripts/install-local.sh` (also `make install-local`) installs the CLI into a bin dir on your `PATH` (default `~/.local/bin`, override with `--bin-dir`), registers the desktop launcher, and verifies both. The desktop build needs the [Wails CLI](https://wails.io) and, on Linux, `webkit2gtk 4.1`; pass `--skip-desktop` to skip it. Run `./scripts/install-local.sh --help` for all flags.

**Windows combined installer:** `scripts/package-windows.ps1` builds the CLI (amd64 + arm64) and the desktop app, then produces a single `Moxie-Setup.exe` NSIS installer (desktop + CLI, CLI on PATH). It needs Go, Node, the Wails CLI and NSIS:

```powershell
.\scripts\package-windows.ps1 -Version 0.4.0            # stable
.\scripts\package-windows.ps1 -Version dev-abc1234 -Channel dev
```

### Verify installation

```bash
moxie --version
```

---

## Quick start

```bash
# 1. Scan your games folder (incremental — skips already-known games)
moxie scan ~/Downloads

# 2. Auto-associate F95Zone threads for metadata
moxie sync

# 3. Browse your library
moxie tui

# 4. Add a game to Steam
moxie steam add 42
```

### Typical workflows

<details>
<summary><strong>"I just downloaded a bunch of games and want them organized"</strong></summary>

```bash
moxie scan ~/Downloads           # scan new downloads (incremental)
moxie rename --dry-run           # preview clean directory names
moxie rename                     # apply renames
moxie sync                       # enrich with metadata + check updates
moxie tui                        # browse and manage your library
```
</details>

<details>
<summary><strong>"I want to add a game to my Steam library"</strong></summary>

```bash
moxie steam add 42                     # add with Proton + artwork
moxie steam add 42 --proton GE-Proton9-7  # specific Proton version
moxie steam add 42 --no-artwork        # skip artwork (faster)
moxie steam add 42 --all-users         # add for all Steam accounts
```
</details>

<details>
<summary><strong>"I want to check for game updates"</strong></summary>

```bash
moxie check-updates                  # check all associated games
moxie check-updates --force          # bypass 24h cooldown
moxie sync --json > updates.json     # full sync, JSON output
moxie sync 15                        # single game by ID
```
</details>

<details>
<summary><strong>"I want to verify F95Zone associations are correct"</strong></summary>

```bash
moxie cleanup --dry-run              # preview flagged mismatches
moxie cleanup                        # interactive review
moxie cleanup --assume-yes           # auto-disassociate all flagged
moxie list --warnings                # quick scan for engine/exe issues
```
</details>

---

## Command reference

Run `moxie <command> --help` for any command's flags. Global flags: `--help` / `-h`, and `--verbose` / `-v` for debug logging.

<details>
<summary><strong>Core commands</strong></summary>

| Command | What it does |
|---------|-------------|
| `scan <dir>` | Scan a directory for games (incremental by default; `--force` for a full rescan). Detects engines, measures sizes, finds executables. |
| `detect <path\|id>` | Explain how a path or game was classified (`--json` for machine output). |
| `list` | List all games. Supports `--engine`, `--status`, `--deleted`, `--warnings` (engine/exe mismatch), `--json`. |
| `tui` | Launch the interactive terminal UI with filtering, sorting, and detail views. |
| `info <id\|name>` | Show detailed game info — path, size, dates, engine, scraped metadata. |
| `play <id\|name>` | Launch a game. Uses the native binary on Linux, with a Wine fallback. |
| `history [count]` | Show recently played games. |
| `add <path>` | Manually add a game. Engine auto-detected if not specified. |
| `remove <id\|name>` | Remove from the library (soft delete — does not delete files on disk). |
| `restore <id\|name>` | Restore a soft-deleted game. |
| `purge` | Permanently delete all soft-deleted games. |
| `rename` | Rename directories to clean, filesystem-safe titles. `--dry-run` to preview. |
| `set-path <id> <path>` | Update the filesystem path for a game. |
| `set-exe <id> <exe>` | Manually set the executable path for a game. |
| `set-wine-prefix <id> <path>` | Set a default Wine prefix for a game. |
| `set-status <status>` | Update game status (`active`/`completed`/`abandoned`/`on_hold`/`unknown`); supports `--engine` and `--all`. |
| `refresh-versions` | Re-detect installed versions from folder names and game files (Ren'Py options/`.rpa`, RPG Maker `System.json`, `Game.ini`, `package.json`); no network calls. |
| `covers upgrade` | Replace landscape, missing or low-res covers with portrait art. `--dry-run`, `--vndb`, `--no-steam`, `--limit N`, `--game ID`. |
</details>

<details>
<summary><strong>F95Zone &amp; metadata commands</strong></summary>

| Command | What it does |
|---------|-------------|
| `sync [id]` | Full library sync: auto-associate unassociated games, then check all for version updates. `--force` bypasses the 24h cooldown. |
| `scrape <id>` | Scrape one thread for metadata. Firefox cookies are auto-detected. |
| `scrape-batch` | Scrape several games in one run. |
| `check-updates` (alias `updates`) | Check all associated games for newer versions. |
</details>

<details>
<summary><strong>Download commands</strong></summary>

| Command | What it does |
|---------|-------------|
| `download <id>` | Download a game from community thread links. Auto-fallbacks through host priority. |
| `install <id> <archive>` | Install a downloaded archive into the game directory (extract + merge). |
| `downloads` | List download history. |
| `check-links` | Validate all stored download links (detect dead/broken URLs). |
</details>

<details>
<summary><strong>Steam commands</strong></summary>

| Command | What it does |
|---------|-------------|
| `steam add <id>` | Add a non-Steam game to Steam with a deterministic AppID, artwork, and Proton. |
| `steam remove <id>` | Remove from Steam's `shortcuts.vdf`. Idempotent. |
| `steam list` | List all non-Steam games added by moxie. |
| `steam proton-list` | Scan for installed Proton versions. |
| `steam proton-set <id>` | Set the Proton version for a game. |
| `steam fix-artwork <id>` | Re-download Steam artwork (uses SteamGridDB if a key is set). |

**Safety guarantees:** Steam must be closed before writes; timestamped backups are created before every `shortcuts.vdf` modification; atomic temp-file + rename writes prevent corruption.
</details>

<details>
<summary><strong>Library management commands</strong></summary>

| Command | What it does |
|---------|-------------|
| `collections <create\|list\|show\|add\|remove\|delete>` | Manage game collections. |
| `export [--output file]` | Export the library as JSON. |
| `import <file>` | Import games from a JSON export. |
| `cleanup` | Detect wrong thread associations (engine/exe mismatch). `--dry-run` to preview, `--assume-yes` / `-y` to auto-disassociate. |
</details>

<details>
<summary><strong>Administration commands</strong></summary>

| Command | What it does |
|---------|-------------|
| `config <set\|get\|show>` | Manage settings (SteamGridDB key, cover sources, …). Persisted to JSON. |
| `update` | Check for and install moxie updates. |
</details>

---

## Configuration

### Source-site cookie (for scraping)

moxie automatically detects site cookies from Firefox. If you use another browser:

```bash
moxie sync --cookie "cookie_header_string"
moxie sync --cookie-file /path/to/cookies.txt
```

### SteamGridDB API key (for higher-quality artwork)

```bash
# Get a free key at https://www.steamgriddb.com/profile/preferences
moxie config set steamgriddb-key YOUR_KEY
moxie steam fix-artwork <id>
```

### Portrait library covers

F95Zone covers are mostly landscape banners. `covers upgrade` replaces them with portrait box art from Steam (no key needed), SteamGridDB (when a key is set) and VNDB (`--vndb`). It only replaces a cover on an exact title match, and only with art that is portrait and at least as sharp. The desktop app has the same action under **Covers → Upgrade to Portrait Art**. See [`docs/cover-art.md`](docs/cover-art.md).

```bash
moxie covers upgrade --dry-run   # list proposed replacements
moxie covers upgrade             # apply (the previous cover is kept for undo)
```

### View all configuration

```bash
moxie config show
```

---

## Troubleshoot moxie

| Issue | Solution |
|-------|----------|
| **"No cover artwork URL found"** | The game's thread lacks a downloadable cover. Run `moxie sync <id>` to refresh metadata, or configure a SteamGridDB API key. |
| **Games don't appear in Steam after `steam add`** | Steam must be fully closed before adding games, and restarted afterward. |
| **"Cookie required" when scraping** | Log into the source site in Firefox, or use `--cookie "header"` with a cookie string from browser DevTools. |
| **Scan finds no games** | Ensure the directory contains game engine files (`.exe`, `.sh`, `.x86_64`, …) or engine markers (`renpy/`, `www/`, `_Data` folders, `.pck` files, …). |
| **How do I reset my library?** | Delete `~/.config/moxie/games.db`. It will be recreated on the next scan. |
| **How do I set a specific Proton version?** | `moxie steam proton-list` to see versions, then `moxie steam proton-set <id> --version GE-Proton9-7`. |
| **Download link always fails** | Most file hosts use anti-bot protection. Try `moxie install <id> <path>` with a manually downloaded archive. |
| **`moxie sync` fails with a block error** | Your session cookie expired. Log in again in Firefox, or pass a fresh cookie file with `--cookie-file`. See [`docs/FAQ.md`](docs/FAQ.md). |

More edge cases live in [`docs/FAQ.md`](docs/FAQ.md).

---

## Documentation

| Document | What it covers |
|----------|---------------|
| [`docs/architecture.md`](docs/architecture.md) | System design, package diagram, technology choices, future roadmap |
| [`docs/moxie-spec.md`](docs/moxie-spec.md) | Full MVP specification, implementation status, known limitations |
| [`docs/scanner.md`](docs/scanner.md) | Directory walk algorithm, engine detection profiles, exclusion list |
| [`docs/scraper.md`](docs/scraper.md) | HTTP client, rate limiting, HTML parsing, auto-association |
| [`docs/downloader.md`](docs/downloader.md) | Host resolvers, resume, platform priority, dead-link validation |
| [`docs/database.md`](docs/database.md) | SQLite schema, version tracking, migration strategy |
| [`docs/tui.md`](docs/tui.md) | Bubble Tea model/update/view, keyboard shortcuts, filter system |
| [`docs/browser.md`](docs/browser.md) | Cross-browser cookie extraction with kooky |
| [`docs/cover-art.md`](docs/cover-art.md) | Cover cache, thumbnails, portrait sources (Steam, SteamGridDB, VNDB) |
| [`docs/steam-package-design.md`](docs/steam-package-design.md) | Steam VDF shortcuts, artwork pipeline, Proton config |
| [`docs/FAQ.md`](docs/FAQ.md) | Edge cases and troubleshooting |

### Data location

All persistent data lives under `~/.config/moxie/` (Linux), `%APPDATA%\moxie\` (Windows), or `~/Library/Application Support/moxie/` (macOS):

| Path | Contents |
|------|----------|
| `games.db` | SQLite database with WAL mode, foreign keys, and CHECK constraints |
| `config.json` | Configuration store (SteamGridDB key, cover sources, preferences) |
| `covers/` | Cached cover art and metadata sidecars |
| `work/` | Download and extraction scratch space |
| `logs/` | Per-day structured log files |

You can safely delete `games.db` to reset your library — it will be recreated on the next scan.

---

## How to get help

- **Bug reports & feature requests** — open an [issue on GitHub](https://github.com/Milisource/moxie/issues)
- **Documentation** — see the [`docs/`](docs/) directory for detailed component documentation
- **Quick reference** — run `moxie` without arguments for the full command reference

This is a hobby project, so response times may vary. Contributions are welcome — see [`CONTRIBUTING.md`](CONTRIBUTING.md).

---

## License

Released under the [WTFPL v2](LICENSE) — do what the fuck you want to.

---

> README structure inspired by [PhotoCraft](https://github.com/storytold/photocraft) and [OpenWork](https://github.com/different-ai/openwork), and The Good Docs Project's [README template](https://www.thegooddocsproject.dev/).
