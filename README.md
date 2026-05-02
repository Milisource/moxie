# moxie — Game Library Manager

**Scan, catalog, enrich, and launch your local game library — from the terminal.**

moxie recursively scans your game directories, detects engines (Unity, Ren'Py, RPG Maker, and 11 more), stores everything in a local SQLite database, and optionally scrapes F95Zone threads for version info, developer names, tags, and cover art. It can push games straight into your Steam library with artwork and Proton configuration — no GUI needed.

---

## Features

- **Engine-aware scanning** — Detects 14 canonical engines + community-run games, falling through to Others. Walks directory trees, finds executables, reports byte-exact sizes.
- **F95Zone enrichment** — Cookie-based scraping (Firefox auto-detect, explicit fallback) pulls version, developer, tags, overview, cover art, and store links from thread pages.
- **Auto-association** — Title-matching engine scores unassociated games against F95Zone search results (exact → contains → word overlap) and accepts the best match automatically.
- **Steam integration** — Add non-Steam games to your Steam library with deterministic AppIDs, grid artwork (F95Zone cover or SteamGridDB), Proton version configuration, and safe VDF read/write with automatic backups.
- **Bubble Tea TUI** — Full interactive terminal UI with list/detail views, engine/status filters, sortable columns, real-time search, and engine-colored rows.
- **Version tracking** — `check-updates` and `sync` re-scrape associated threads, compare versions, and report what's new.
- **Config management** — Persistent JSON-backed settings for SteamGridDB API keys and other preferences.
- **Cross-platform** — Single static Go binary (~10 MB), no CGO, no runtime deps. Linux primary target; Windows and macOS supported for scanning and library management.

---

## Quick Start

```bash
# Build from source
go build -ldflags="-s -w" -o moxie .

# Install to PATH (or use ./scripts/install.sh for ~/.local/bin)
sudo mv moxie /usr/local/bin/

# Scan your Games folder
moxie scan ~/Games

# Launch the interactive TUI
moxie tui
```

On first scan you will be prompted before saving. Use `--no-save` for a preview-only scan. Subsequent scans skip duplicates automatically.

> **Note:** If you use `install.sh`, make sure `~/.local/bin` is in your PATH. Add `export PATH="$HOME/.local/bin:$PATH"` to your shell config.

---

## Command Reference

### Library Management

| Command | What it does |
|---|---|
| `scan <dir> [flags]` | Recursively scan a directory for games. Detects engines, measures sizes, finds executables. `--no-save` for preview, `--engine` to filter, `--json` for machine output. |
| `add <path> [flags]` | Manually add a game directory. Engine auto-detection runs if not specified. |
| `remove <id>` | Remove a game from the library. Does **not** delete files on disk. |
| `rename [flags]` | Rename game directories using clean, filesystem-safe names. Use `--dry-run` to preview before applying. |
| `list [flags]` | List all games in the library. Supports `--engine`, `--status`, `--json`. |
| `info <id>` | Show detailed game information — path, size, dates, engine, and scraped metadata. |
| `play <id>` | Launch a game by its library ID. On Linux, prefers native binaries over Wine. |

### F95Zone Integration

| Command | What it does |
|---|---|
| `scrape <id> [flags]` | Scrape an F95Zone thread for metadata. Firefox cookies auto-detected; use `--cookie-file` for explicit import. |
| `scrape --auto` | Batch-associate all unassociated games via F95Zone title search. Scores candidate threads and auto-accepts the best match. |
| `sync [id] [flags]` | Full library sync: associate unassociated games, then check all for updates. Pass a game ID to sync a single title. `--json` for machine output. |
| `check-updates [flags]` | Re-scrape all associated games and report which have newer versions available on F95Zone. |

### Steam Integration

| Command | What it does |
|---|---|
| `steam add <id> [flags]` | Add a non-Steam game to your Steam library. Generates a deterministic AppID, writes to `shortcuts.vdf`, downloads cover artwork, and configures Proton (Linux). |
| `steam remove <id> [flags]` | Remove a game from Steam's `shortcuts.vdf` by library ID. Idempotent — safe to run on already-removed entries. |
| `steam list [flags]` | List all non-Steam games previously added by moxie (tagged with "F95Zone"). |
| `steam proton-list` | Scan Steam's `compatibilitytools.d/` and `steamapps/common/` for installed Proton versions. |
| `steam proton-set <id> --version <proton>` | Set or change the Proton version for a game already in your Steam library. |
| `steam fix-artwork <id> [flags]` | Re-download and refresh artwork for a game in the Steam library. Supports `--steamgriddb-key` for premium GridDB assets. |

**Steam safety guarantees:**
- Checks that Steam is closed before modifying `shortcuts.vdf`
- Creates a timestamped backup (`*.backup-<iso8601>`) before every write
- Uses atomic temp-file + rename writes
- Validates the written file by re-reading and comparing entry counts
- Preserves unknown VDF fields via `RawFields` for round-trip safety

### Configuration

| Command | What it does |
|---|---|
| `config set <key> <value>` | Set a configuration value (e.g., `steamgriddb-key`). Persisted to `~/.config/moxie/config.json`. |
| `config get <key>` | Retrieve a configuration value. |
| `config show` | Display all configuration settings. |

---

## Key Workflows

### "I just downloaded a bunch of games and want them organized"

```bash
moxie scan ~/Downloads           # scan new downloads
moxie rename --dry-run           # preview clean directory names
moxie rename                     # apply renames
moxie sync                       # auto-associate F95Zone threads, check updates
moxie tui                        # browse and manage your library
```

### "I want to add a game to my Steam library"

```bash
# Add game ID 42 to Steam with Proton and F95Zone cover art
moxie steam add 42

# Use a specific Proton version
moxie steam add 42 --proton GE-Proton9-7

# Skip artwork for faster batch operations
moxie steam add 42 --no-artwork

# Add to all Steam accounts on this machine
moxie steam add 42 --all-users
```

### "I want to check for game updates"

```bash
moxie check-updates                # check all associated games for new versions
moxie sync --json > updates.json   # full sync, JSON output for scripting
moxie sync 15                      # check a single game by ID
```

### "I want to configure SteamGridDB artwork"

```bash
# Set your API key (get one free at https://www.steamgriddb.com/profile/preferences)
moxie config set steamgriddb-key abc123def456

# Verify it's stored
moxie config get steamgriddb-key

# Re-download artwork for a game using SGDB
moxie steam fix-artwork 42

# View all config
moxie config show
```

---

## TUI Features

The interactive terminal UI (`moxie tui`) is built with Bubble Tea and provides a full library browsing experience.

### View Layout

| Section | Description |
|---|---|
| **Header** | Library title with game count, filter hint, and help icon |
| **Table** | Sortable, filterable list of games with engine-colored rows |
| **Filter bar** | Real-time text search across game titles |
| **Detail view** | Full game info including scraped metadata, version, tags, and cover URL |
| **Footer** | Keyboard shortcut hints and status messages |

### Keyboard Shortcuts

| Key | Context | Action |
|---|---|---|
| `↑` / `k` | Library | Move selection up |
| `↓` / `j` | Library | Move selection down |
| `Enter` | Library | Open detail view for selected game |
| `Esc` / `←` | Any | Return to library list |
| `/` | Library | Start filter / search input |
| `s` | Library | Cycle sort field |
| `Ctrl+E` | Library | Cycle engine type filter |
| `Ctrl+S` | Library | Cycle game status filter |
| `d` | Library | Delete selected game (with confirmation) |
| `q` | Library | Quit application |
| `?` | Any | Toggle help overlay |
| `Ctrl+C` | Any | Force quit |
| `e` | Detail | Edit game title |
| `s` | Detail | Cycle game status |
| `o` | Detail | Show game folder path |
| `u` | Detail | Set / edit F95Zone thread URL |
| `p` | Detail | Launch game (play hint) |

---

## Data Location

All persistent data lives under `~/.config/moxie/`:

| Path | Contents |
|---|---|
| `~/.config/moxie/games.db` | SQLite database with WAL mode, foreign keys, and CHECK constraints |
| `~/.config/moxie/config.json` | JSON configuration store (SteamGridDB key, preferences) |

The directory and database are created automatically on first run. You can safely delete `games.db` to reset your library — it will be recreated on the next scan. Artwork cached for Steam is stored in Steam's own `grid/` directory, not in `~/.config/moxie/`.

---

## Building from Source

**Prerequisites:** Go 1.22 or later. No CGO dependencies.

```bash
# Single binary (~10 MB, static, cross-compilable)
go build -ldflags="-s -w" -o moxie .

# Build and install to ~/.local/bin/
./scripts/install.sh

# Cross-compile for all platforms (linux/mac/windows)
./scripts/build.sh all

# Run tests (170+ across scanner, engine, scraper, DB)
go test ./...
```

---

## Documentation Index

| Document | What it covers |
|---|---|
| [docs/architecture.md](docs/architecture.md) | System design, package diagram, technology choices, future roadmap |
| [docs/scanner.md](docs/scanner.md) | Directory walk algorithm, engine detection profiles, exclusion list |
| [docs/scraper.md](docs/scraper.md) | HTTP client, rate limiting, HTML parsing, auto-association logic |
| [docs/database.md](docs/database.md) | SQLite schema, version tracking, migration strategy |
| [docs/tui.md](docs/tui.md) | Bubble Tea model/update/view, keyboard shortcuts, filter system |
| [docs/browser.md](docs/browser.md) | Cross-browser cookie extraction with kooky |
| [docs/steam-package-design.md](docs/steam-package-design.md) | Steam VDF shortcuts, artwork pipeline, Proton config, SteamGridDB client |
| [docs/moxie-spec.md](docs/moxie-spec.md) | Full MVP specification, implementation status, roadmap |

---

## License

Hobby project. All rights reserved by default. Open an issue if you want to contribute.
