# Terminal UI

## What

The TUI is an interactive terminal application for browsing, filtering, and managing your game library. Built with Bubble Tea (Go port of the Elm architecture), it presents a sortable table of games with a detail view, engine/status filters, inline editing, and keyboard-only navigation.

## How

### Architecture

The TUI follows the Elm Architecture — a unidirectional data flow pattern:

```
Model ←─── Update ←─── View
  │                     │
  └───── messages ──────┘
```

- **Model** — holds all state: the game list, table component, filter inputs, cursor position, view mode, and scraped metadata cache
- **Update** — receives `tea.Msg` (key presses, async results, window resize events) and returns a new model plus optional commands
- **View** — renders the model as a styled string for terminal output

### File Structure (9 files)

| File | Responsibility |
|---|---|
| `tui.go` | Entry point — opens DB, creates `tea.NewProgram` with alt screen |
| `model.go` | Model struct, Bubble Tea columns, sort fields, Init method, active download tracking |
| `update.go` | Message handler, key bindings, filter/sort cycling, CRUD delegation |
| `library.go` | Library list view rendering (title bar, status line, table, footer) |
| `detail.go` | Game detail view rendering (info box, actions, edit/url overlays, download progress) |
| `help.go` | Help overlay with keyboard shortcut reference |
| `styles.go` | Lip Gloss style palette, engine color map, status color map |
| `helpers.go` | Filter/sort logic, executable finder, `launchExe`, size formatting |
| `commands.go` | Tea commands for async DB operations (loadGames, loadMeta, startDownload, pollDownloads) |

### Views

The TUI has three visual modes, switched via the `viewMode` field in the model:

1. **Library view** — the default. Shows a sortable table of all games with a status bar (`12/82 games | Engine: All | Status: Active | Sort: Title ↑ | Filter: —`), a filter input overlay (`/` to activate), and a footer with key hints.

2. **Detail view** — shows full game info when you press Enter on a row: title, engine, version (with update indicator if a newer version exists), status, path, size, dates, F95Zone URL, tags, developer, overview excerpt, and last-scraped date. Action keys are displayed in the header.

3. **Help overlay** — a centered box listing all keyboard shortcuts, dismissed with any key.

### Filtering and Sorting

- **Title filter** — `/` activates a real-time text input. Games are filtered by substring match on each keystroke (no Enter needed). `Esc` clears and exits.
- **Engine filter** — `Ctrl+E` cycles through a 15-element array: `""` (all) → Unity → RenPy → RPGM → ... → WolfRPG.
- **Status filter** — `Ctrl+S` cycles through: `""` (all) → active → completed → abandoned → on_hold → unknown.
- **Sort cycling** — `s` cycles ID → Title → Engine → Version (desc). The active sort field and direction appear in the status bar as `Title ↑` or `Version ↓`.
- **Engine colors** — each engine type has a distinct Lip Gloss color (Unity = cyan, RenPy = magenta, RPGM = green, etc.). Applied per-row via `engineColor(e)`.
- **Update indicators** — a `🔄` marker appears next to game titles where both `latest_version` and `version` are known and differ. If the local version is empty (no version found in the directory name), no indicator is shown — an unknown local version cannot confirm an update.
- **Unknown versions** — games without a detected version show the scraped `LatestVersion` from F95Zone when available, falling back to `"unknown"` only when neither source has a version. This applies in the library table, detail view, and `moxie list` output.

### Downloads

The TUI provides a download workflow accessible from the detail view (`g` key). Downloads run asynchronously in a background goroutine while the TUI remains responsive.

#### Link Resolution

`resolveDownloadLinks()` retrieves download links from the database first. If none are found, it scrapes the game's F95Zone thread (requires a scraper client with valid cookies). Links are sorted by platform priority + host reliability score (descending) using the same `ScoreDownloadLink` logic as the CLI.

#### Fallback Loop

`startDownloadCmd()` launches a background goroutine that tries each link in priority order:

1. Calls `downloader.DownloadWithHost()` with the best link
2. On success, validates with `downloader.IsValidGameFile()` — rejects interstitial HTML pages
3. On failure or validation reject, logs the error and tries the next link
4. If all links fail, shows a summary with per-link failure reasons and suggests manual install

#### Step-by-Step Status Display

The `activeDownload` struct carries a `stepMsg` string that provides real-time status text. The detail view renders this in `downloadSection()`:

| State | Display |
|-------|---------|
| **Finding host** | `"Trying: buzzheavier..."` (accent color) |
| **Downloading** | Progress bar with speed, bytes, percentage |
| **Host failed** | `"✗ Failed: vikingfile"` (red) |
| **Fallback retry** | `"Trying next: datanodes..."` (accent) |
| **Complete** | `"✓ Download succeeded!"` (green) |
| **Extracting** | `"Extracting archive..."` (accent) |
| **Merging** | `"Merging into game directory..."` (accent) |
| **All failed** | `"✗ All links failed"` (red) with per-host error summary and `moxie install` hint |

The TUI polls every 500ms (`pollDownloads()`) to check active download progress and trigger re-renders. This is implemented via `tea.Tick` and the `downloadProgressMsg` message type.

#### Post-Download Pipeline

After a successful download, the TUI automatically:
1. Extracts the archive if it's a recognized format (zip, 7z, rar, tar.gz)
2. Removes the archive file
3. Merges extracted files into the game directory via `updater.Merge()` (preserving saves/configs)
4. Creates a `.old` backup of the original game directory before merging

All steps are reported via `stepMsg` updates visible in the detail view.

### Overlays

The detail view supports four overlay modes:

- **Delete confirmation** — `d` shows `Delete 'Summer's Gone'? (y/N)` with red border styling
- **Title editing** — `e` shows a text input pre-filled with the current title
- **URL assignment** — `u` shows a text input for the F95Zone thread URL
- **Play hint** — `p` shows the game path if no executable is found, or launches the game

Overlays stack on top of the current view. Each has a dedicated handler (`handleDeleteConfirm`, `handleEditKey`, `handleUrlInput`).

### Executable Launching

The TUI's `p` key calls `findPlayableExe()` (same logic as the `play` CLI command but in-package). On Linux: prefers AppImage → native binaries (`.x86_64`, `.sh`) → `.exe` via Wine. The largest executable wins (game engines produce bigger binaries than launcher utilities).

## Why

**Bubble Tea over raw tcell** — the Elm architecture makes TUI state manageable without explicit state machine boilerplate. Bubbles provides pre-built components (tables, text inputs, spinners). Lip Gloss handles styling without raw terminal escape codes. All three are maintained by Charmbracelet (29k+ stars) and have stable APIs.

**TUI over web UI** — a web UI requires a running server, a browser, and adds complexity for a single-user tool. The TUI shares the same Go binary, starts instantly, and works over SSH. When a Wails desktop GUI ships later, all internal packages are reused unchanged.

**In-memory filtering** — the entire game list is loaded once and filtered/sorted in-memory via `filterAndSort()`. This avoids repeated SQL queries during navigation. For libraries under ~10,000 games, Go slice operations are effectively instant.
