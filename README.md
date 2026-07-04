# moxie — Game Library Manager

[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)
[![Release](https://img.shields.io/github/v/release/Milisource/moxie)](https://github.com/Milisource/moxie/releases)

**Scan, catalog, enrich, and launch your local game library — from the terminal.**

moxie recursively scans your game directories, detects engines (Unity, Ren'Py, RPG Maker, and 11 more), stores everything in a local SQLite database, and optionally scrapes F95Zone threads for version info, developer names, tags, and cover art. It can push games straight into your Steam library with artwork and Proton configuration — no GUI needed.

---

## Table of Contents

- [Project description](#project-description)
- [Who this project is for](#who-this-project-is-for)
- [Project dependencies](#project-dependencies)
- [Instructions for using moxie](#instructions-for-using-moxie)
  - [Install moxie](#install-moxie)
  - [Configure moxie](#configure-moxie)
  - [Quick start](#quick-start)
  - [Command reference](#command-reference)
  - [Troubleshoot moxie](#troubleshoot-moxie)
- [Additional documentation](#additional-documentation)
- [How to get help](#how-to-get-help)
- [Terms of use](#terms-of-use)

---

## Project description

With moxie you can **scan** local game directories, **enrich** them with metadata from F95Zone, **track** version updates, and **launch** games directly — all from a terminal UI or CLI.

Unlike manually organizing game folders and checking F95Zone threads one by one, moxie automates the entire pipeline:

```
┌──────────┐     ┌──────────────┐     ┌───────────────┐     ┌──────────┐
│  Scan    │────▶│  Scrape      │────▶│  Sync +       │────▶│  TUI /   │
│  ~/Games │     │  F95Zone     │     │  Check Updates│     │  CLI     │
└──────────┘     └──────────────┘     └───────┬───────┘     └──────────┘
                                               │
                                               ▼
                                        ┌──────────────┐     ┌──────────┐
                                        │  Steam Add   │────▶│  Play    │
                                        │  + Artwork   │     │  Launch  │
                                        └──────────────┘     └──────────┘
```

Key capabilities:

- **Engine-aware scanning** — Detects 14 canonical engines (Unity, Ren'Py, RPG Maker, Godot, Unreal, HTML, Flash, WolfRPG, etc.) plus a fallback for Others. Reports byte-exact sizes and finds executables.
- **Incremental by default** — `moxie scan <dir>` skips directories whose modification time hasn't changed since the last scan. Use `--force` for a full re-detection.
- **F95Zone enrichment** — Cookie-based scraping pulls version, developer, tags, overview, cover art, and store links from thread pages. Auto-association scores search results and picks the best match.
- **Steam integration** — Add non-Steam games with deterministic AppIDs, grid artwork (F95Zone cover or SteamGridDB), and Proton version configuration. Safe VDF read/write with automatic backups.
- **Bubble Tea TUI** — Interactive terminal UI with list/detail views, engine/status filters, sortable columns, real-time search, and engine-colored rows.
- **Version tracking** — Check all associated games for newer versions on F95Zone. Supports `--force` to bypass 24h cooldown.
- **Download manager** — Download games directly from supported hosts (Pixeldrain, Buzzheavier, Gofile, Google Drive, DataNodes, MixDrop) with resume support, platform priority, and dead link fallback.
- **Cross-platform** — Single static Go binary (~16 MB), no CGO, no runtime deps. Linux, macOS, and Windows.

---

## Who this project is for

This project is intended for **gamers and collectors** who:

- Have a local directory of game installs and want them organized and searchable
- Follow games on F95Zone and want automatic version update tracking
- Want to add non-Steam games to their Steam library with proper artwork and Proton support (Linux)
- Prefer a terminal workflow over a GUI
- Manage large collections (hundreds of games) across multiple directories

---

## Project dependencies

Before using moxie, ensure you have:

- **A directory of games** — moxie scans local folders for game engine markers. It does not download games from the internet on its own.
- **A Firefox browser (optional)** — for automatic F95Zone cookie detection during scraping. Chrome/Edge cookies are supported via explicit export.
- **Steam (optional)** — for the `steam` commands. Steam must be closed before adding or removing games.
- **Go 1.24+ (optional)** — only needed if building from source. No CGO required.

---

## Instructions for using moxie

### Install moxie

#### macOS / Linux

```bash
curl -fsSL https://raw.githubusercontent.com/milisource/moxie/main/scripts/install.sh | bash
```

The script downloads the latest pre-built binary for your platform to `~/.local/bin/` and adds it to your shell config. Restart your terminal or run `source ~/.bashrc` for PATH changes to take effect.

**To pin a specific version:**
```bash
curl -fsSL https://raw.githubusercontent.com/milisource/moxie/main/scripts/install.sh | bash -s -- --version v0.3.3-alpha
```

**Install from a local build:**
```bash
./scripts/install.sh --binary ./dist/moxie
```

**Available flags:** `--version <ver>`, `--binary <path>`, `--no-modify-path`, `--help`

#### Windows

Open **PowerShell** (not Command Prompt) and run:

```powershell
irm https://raw.githubusercontent.com/milisource/moxie/main/scripts/install.ps1 | iex
```

The script downloads `moxie.exe` to `%LOCALAPPDATA%\moxie\bin\` and adds it to your user PATH.

**Available flags:** `-Version <ver>`, `-Binary <path>`, `-NoModifyPath`, `-Help`

#### Build from source

```bash
# Quick build for current platform
git clone https://github.com/milisource/moxie.git
cd moxie
make build                    # produces dist/moxie
sudo make install             # copies to /usr/local/bin/moxie

# Or build manually
CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=$(git describe --tags --always)" -o moxie .

# Cross-compile all platforms
./scripts/build.sh all
```

#### Verify installation

```bash
moxie --version
```

### Configure moxie

#### F95Zone cookie (for scraping)

moxie automatically detects cookies from Firefox. If you use another browser:

```bash
# Export cookies manually:
moxie sync --cookie "cookie_header_string"
moxie sync --cookie-file /path/to/cookies.txt
```

#### SteamGridDB API key (for higher-quality artwork)

```bash
# Get a free key at https://www.steamgriddb.com/profile/preferences
moxie config set steamgriddb-key YOUR_KEY
moxie steam fix-artwork <id>
```

#### View all configuration

```bash
moxie config show
```

### Quick start

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

#### Typical workflows

<details>
<summary><strong>"I just downloaded a bunch of games and want them organized"</strong></summary>

```bash
moxie scan ~/Downloads           # scan new downloads (incremental)
moxie rename --dry-run           # preview clean directory names
moxie rename                     # apply renames
moxie sync                       # auto-associate + check updates
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

### Command reference

<details>
<summary><strong>Core commands</strong></summary>

| Command | What it does |
|---------|-------------|
| `scan <dir>` | Scan directory for games (incremental by default; `--force` for full rescan). Detects engines, measures sizes, finds executables. |
| `list` | List all games. Supports `--engine`, `--status`, `--json`, `--warnings` (engine/exe mismatch column). |
| `tui` | Launch interactive terminal UI with filtering, sorting, and detail views. |
| `info <id>` | Show detailed game info — path, size, dates, engine, scraped metadata. |
| `play <id>` | Launch a game. Uses native binary on Linux, Wine fallback. |
| `add <path>` | Manually add a game. Engine auto-detected if not specified. |
| `remove <id>` | Remove from library (does not delete files on disk). |
| `rename` | Rename directories to clean, filesystem-safe titles. `--dry-run` to preview. |
| `set-path <id> <path>` | Update the filesystem path for a game in the database. |
| `set-exe <id> <exe>` | Manually set the executable path for a game. |
| `refresh-versions` | Re-extract version strings from directory names (no network calls). |
</details>

<details>
<summary><strong>F95Zone commands</strong></summary>

| Command | What it does |
|---------|-------------|
| `sync [id]` | Full library sync: auto-associate unassociated games, then check all for version updates. `--force` bypasses 24h cooldown. |
| `scrape <id>` | Scrape an F95Zone thread for metadata. Firefox cookies auto-detected. |
| `check-updates` | Check all associated games for newer versions on F95Zone. |
</details>

<details>
<summary><strong>Download commands</strong></summary>

| Command | What it does |
|---------|-------------|
| `download <id>` | Download a game from F95Zone links. Auto-fallbacks through host priority. |
| `install <id> <archive>` | Install a downloaded archive into the game directory (extract + merge). |
| `downloads` | List download history. |
| `check-links` | Validate all stored download links (detect dead/broken URLs). |
</details>

<details>
<summary><strong>Steam commands</strong></summary>

| Command | What it does |
|---------|-------------|
| `steam add <id>` | Add a non-Steam game to Steam with deterministic AppID, artwork, and Proton. |
| `steam remove <id>` | Remove from Steam's shortcuts.vdf. Idempotent. |
| `steam list` | List all non-Steam games added by moxie. |
| `steam proton-list` | Scan for installed Proton versions. |
| `steam proton-set <id>` | Set Proton version for a game. |
| `steam fix-artwork <id>` | Re-download Steam artwork (uses SteamGridDB if key is set). |

**Safety guarantees:** Steam must be closed before writes; timestamped backups are created before every `shortcuts.vdf` modification; atomic temp-file + rename writes prevent corruption.
</details>

<details>
<summary><strong>Administration commands</strong></summary>

| Command | What it does |
|---------|-------------|
| `cleanup` | Detect wrong F95Zone associations (engine/exe mismatch). `--dry-run` to preview. |
| `config set/get/show` | Manage settings (SteamGridDB key, etc.). Persisted to JSON. |
| `update` | Check for and install moxie updates. |
</details>

### Troubleshoot moxie

| Issue | Solution |
|-------|----------|
| **"No cover artwork URL found"** | The game's F95Zone thread lacks a downloadable cover. Run `moxie sync <id>` to refresh metadata, or configure a SteamGridDB API key. |
| **Games don't appear in Steam after `steam add`** | Steam must be fully closed before adding games, and restarted afterward. |
| **"Cookie required" when scraping** | Log into F95Zone in Firefox, or use `--cookie "header"` with a cookie string from browser DevTools. |
| **Scan finds no games** | Ensure the directory contains game engine files (.exe, .sh, .x86_64, etc.) or engine markers (renpy/, www/, _Data folders, .pck files, etc.). |
| **How do I reset my library?** | Delete `~/.config/moxie/games.db`. It will be recreated on the next scan. |
| **How do I set a specific Proton version?** | `moxie steam proton-list` to see available versions, then `moxie steam proton-set <id> --version GE-Proton9-7`. |
| **Download link always fails** | Most file hosts use anti-bot protection. Try `moxie install <id> <path>` with a manually downloaded archive. |

---

## Additional documentation

| Document | What it covers |
|----------|---------------|
| [docs/architecture.md](docs/architecture.md) | System design, package diagram, technology choices, future roadmap |
| [docs/moxie-spec.md](docs/moxie-spec.md) | Full MVP specification, implementation status, known limitations |
| [docs/scanner.md](docs/scanner.md) | Directory walk algorithm, engine detection profiles, exclusion list |
| [docs/scraper.md](docs/scraper.md) | HTTP client, rate limiting, HTML parsing, auto-association |
| [docs/database.md](docs/database.md) | SQLite schema, version tracking, migration strategy |
| [docs/tui.md](docs/tui.md) | Bubble Tea model/update/view, keyboard shortcuts, filter system |
| [docs/browser.md](docs/browser.md) | Cross-browser cookie extraction with kooky |
| [docs/steam-package-design.md](docs/steam-package-design.md) | Steam VDF shortcuts, artwork pipeline, Proton config |

### TUI keyboard shortcuts

| Key | Context | Action |
|-----|---------|--------|
| `↑` / `k` | Library | Move selection up |
| `↓` / `j` | Library | Move selection down |
| `Enter` | Library | Open detail view |
| `Esc` / `←` | Any | Return to library list |
| `/` | Library | Start search/filter |
| `s` | Library | Cycle sort field |
| `d` | Library | Delete game (with confirmation) |
| `e` | Detail | Edit game title |
| `p` | Detail | Launch game |
| `u` | Detail | Set F95Zone URL |
| `?` | Any | Toggle help overlay |

Press `?` in the TUI for CLI quick-start commands (scan, scrape, steam add, etc.).

### Data location

All persistent data lives under `~/.config/moxie/` (Linux), `%APPDATA%/moxie/` (Windows), or `~/Library/Application Support/moxie/` (macOS):

| Path | Contents |
|------|----------|
| `games.db` | SQLite database with WAL mode, foreign keys, and CHECK constraints |
| `config.json` | Configuration store (SteamGridDB key, preferences) |
| `logs/` | Per-day structured log files |

You can safely delete `games.db` to reset your library — it will be recreated on the next scan.

---

## How to get help

- **Bug reports & feature requests** — Open an [issue on GitHub](https://github.com/milisource/moxie/issues)
- **Documentation** — See the [docs/](docs/) directory for detailed component documentation
- **Quick reference** — Run `moxie` without arguments for the full command reference

This is a hobby project. Response times may vary.

---

## Terms of use

Hobby project. All rights reserved by default. See the [LICENSE](LICENSE) file for details. Open an issue on GitHub if you want to contribute.

---

> Structure inspired by [The Good Docs Project](https://www.thegooddocsproject.dev/) README template.
