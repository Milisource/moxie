# Terminal UI

## What

The TUI is an interactive terminal application for browsing, filtering, and managing your game library. Built with Bubble Tea (Go port of the Elm architecture), it presents a sortable table of games with a detail view, engine/status/collection filters, inline editing, download progress, and keyboard-only navigation.

## How

### Architecture

The TUI follows the Elm Architecture — a unidirectional data flow pattern:

```
Model ←─── Update ←─── View
  │                     │
  └───── messages ──────┘
```

- **Model** — holds all state: the game list, table component, filter inputs, cursor position, view mode, scraped metadata cache, active download tracking, and collection filter state
- **Update** — receives `tea.Msg` (key presses, async results, window resize events, spinner ticks) and returns a new model plus optional commands. Update stays pure and fast: every network operation (link resolution, scraping, downloads, extraction, merges) runs in background `tea.Cmd` goroutines that report back via messages — a rate-limited F95Zone scrape must never stall the UI
- **View** — renders the model as a styled string for terminal output

### File Structure (11 files)

| File | Responsibility |
|---|---|
| `tui.go` | Entry point — opens DB, creates `tea.NewProgram` with alt screen |
| `model.go` | Model struct, Bubble Tea columns, sort fields, Init method, active download tracking, collection filter state, startup tip state, spinner |
| `update.go` | Message handler, key bindings, filter/sort/collection cycling, CRUD delegation, spinner ticks |
| `library.go` | Library list view rendering (title bar, status line with spinner, dynamic footer with hints) |
| `detail.go` | Game detail view rendering (sectioned layout, status selector, download progress, edit/url/exe overlays) |
| `help.go` | Help overlay with grouped keyboard shortcut reference |
| `styles.go` | Lip Gloss style palette, engine/status color lookups (engine colors from the shared `internal/engine/engine-colors.json`), section header styles, tag styles |
| `helpers.go` | Filter/sort logic, table row building, size formatting, delegates to `internal/launcher/` for executable listing |
| `commands.go` | Tea commands for async DB operations (loadGames, loadMeta, startDownload, pollDownloads, loadCollections) |

### Views

The TUI has four visual modes, switched via the `viewMode` field in the model:

1. **Library view** — the default. Shows a sortable table of all games with a startup tip (auto-dismisses after 6s), a status bar (`12/82 games | Unity | Active | ID ↑ | Filter: — | Collections: RPG`), a filter input overlay (`/` to activate), and a dynamic footer with key hints.

2. **Detail view** — shows full game info when you press Enter on a row. Organized in sections:
   - **Core Info**: Title, Version (with `🆕` update indicator if newer version exists), Engine (color-coded), Status (with interactive selector showing all 5 statuses as `[a] active [c] completed ...`)
   - **F95Zone**: URL (with copy hint), Developer, Cover URL (with copy hint), Tags (rendered as styled chips), Overview (expanded text), Last Scraped date
   - **Metadata**: Path, Exe, Size, Created/Updated dates
   Action keys are displayed in the header.

3. **Help overlay** — a centered box listing all keyboard shortcuts grouped by category (Library Navigation, Global, Detail Actions), dismissed with any key.

4. **Startup tip** — a brief one-line tip that auto-dismisses after 6 seconds on first launch.

### Filtering and Sorting

- **Title filter** — `/` activates a real-time text input. Games are filtered by substring match on each keystroke (no Enter needed). `Esc` clears and exits.
- **Engine filter** — `Ctrl+E` cycles through a 16-element array: `""` (all) → Unity → RenPy → RPGM → ... → WolfRPG.
- **Status filter** — `Ctrl+S` cycles through: `""` (all) → active → completed → abandoned → on_hold → unknown.
- **Collection filter** — `c` cycles through the user's collections (loaded from DB). Status bar shows active collection name.
- **Sort cycling** — `s` cycles ID → Title → Engine → Version (desc). The active sort field and direction appear in the status bar as `Title ↑` or `Version ↓`.
- **Reverse sort** — `r` reverses the current sort direction.
- **Engine colors** — each engine type has a distinct color mirroring F95Zone's "Latest Updates" page palette (Unity = orange, Ren'Py = purple, Java = teal, etc.). The palette is the shared canonical map in `internal/engine/engine-colors.json` (also imported by the desktop frontend), applied per-row via `engineColor(e)`.
- **Update indicators** — a `🔄` marker appears next to game titles where both `latest_version` and `version` are known and differ. If the local version is empty (no version found in the directory name), no indicator is shown — an unknown local version cannot confirm an update.
- **Unknown versions** — games without a detected version show the scraped `LatestVersion` from F95Zone when available, falling back to `"unknown"` only when neither source has a version.

### Detail View Actions

| Key | Action |
|-----|--------|
| `Esc` / `←` / `⌫` | Back to library |
| `e` | Edit title (shows text input pre-filled with current title) |
| `x` | Edit executable path |
| `s` | Cycle status (unknown → active → completed → abandoned → on_hold) |
| `a` / `c` / `b` / `h` / `u` | Direct status selection (active / completed / abandoned / on_hold / unknown) |
| `d` | Delete game (with confirmation) |
| `o` | Show game path |
| `g` | Start download flow |
| `Ctrl+u` | Edit F95Zone URL |
| `p` | Launch game |

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

The TUI polls every 500ms (`pollDownloads()`) to check active download progress and trigger re-renders. A spinner animates in the status bar while downloads are active.

#### Post-Download Pipeline

After a successful download, the TUI automatically:
1. Extracts the archive if it's a recognized format (zip, 7z, rar, tar.gz)
2. Removes the archive file
3. Merges extracted files into the game directory via `updater.Merge()` (preserving saves/configs)
4. Creates a `.old` backup of the original game directory before merging

All steps are reported via `stepMsg` updates visible in the detail view.

### Overlays

The detail view supports overlay modes for editing. Each has a dedicated handler:

- **Delete confirmation** — `d` shows `Delete 'Summer's Gone'? (y/N)` with red border styling
- **Title editing** — `e` shows a text input pre-filled with the current title
- **Executable editing** — `x` shows a text input for the exe path
- **URL assignment** — `Ctrl+u` shows a text input for the F95Zone thread URL
- **Play hint** — `p` shows the game path if no executable is found, or launches the game

Overlays stack on top of the current view. Each has a dedicated handler (`handleDeleteConfirm`, `handleEditKey`, `handleExeKey`, `handleUrlInput`).

### Executable Launching

The TUI's `p` key calls into `internal/launcher/` — the same shared package used by the `moxie play` CLI command. This guarantees identical launch behavior from both entry points:
- macOS: `.app` bundle detection + Mach-O native binary detection
- Linux: prefers AppImage → native binaries (`.x86_64`, `.sh`) → `.exe` via Wine
- CrossOver path detection on macOS
- Scoring-based executable selection (skips runtime engines, launchers, crash handlers)

### Spinner

A Bubble Tea spinner (Dot style, purple) appears in the library view status bar when downloads are in progress. Controlled by the `spinnerActive` model field.

## Why

**Bubble Tea over raw tcell** — the Elm architecture makes TUI state manageable without explicit state machine boilerplate. Bubbles provides pre-built components (tables, text inputs, spinners). Lip Gloss handles styling without raw terminal escape codes. All three are maintained by Charmbracelet (29k+ stars) and have stable APIs.

**TUI over web UI** — a web UI requires a running server, a browser, and adds complexity for a single-user tool. The TUI shares the same Go binary, starts instantly, and works over SSH. When a Wails desktop GUI ships later, all internal packages are reused unchanged.

**In-memory filtering** — the entire game list is loaded once and filtered/sorted in-memory via `filterAndSort()`. This avoids repeated SQL queries during navigation. For libraries under ~10,000 games, Go slice operations are effectively instant.
