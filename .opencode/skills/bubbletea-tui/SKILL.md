# Bubble Tea TUI — moxie Terminal UI

The TUI lives in `internal/tui/` (12 files as of v0.3.52) and follows the Bubble Tea **Model-Update-View** (Elm architecture) pattern.

## Architecture

```
model.go     — State struct, Init(), View()
update.go    — Update (message handler, longest file)
library.go   — Library list view rendering
detail.go    — Game detail view rendering
help.go      — Help overlay
keys.go      — Key binding constants and help context
styles.go    — Lipgloss style definitions (engine colors, layout)
commands.go  — tea.Cmd factories (load games, scrape, download)
helpers.go   — Shared rendering helpers
```

## Model State

The central `model` struct (`internal/tui/model.go`) holds:
- `db` — database handle
- `table` — bubbles/table component (ID, Title, Engine, Version, Status columns)
- `filterInput` — textinput for title filtering
- `allGames` / `filtered` — game data slices
- `viewMode` — `LibraryView` or `DetailView`
- `selectedID`, `detailGame`, `scrapedMeta` — detail view state
- `editing`, `editingExe`, `setUrl` — inline edit mode flags
- `editInput`, `exeInput`, `urlInput` — textinput models for inline editing
- `scraperClient` — optional scraper for on-demand scraping
- `activeDownloads` — map of in-progress downloads
- `engineFilter`, `statusFilter` — filter dropdowns
- `confirmDelete`, `deleteID`, `deleteTitle` — delete confirmation modal state

## Key Bindings

Defined in `keys.go` as typed constants per context:

| Key | Library View | Detail View |
|-----|-------------|-------------|
| `q` / `ctrl+c` | Quit (via `tea.Quit`) | Back to library |
| `↑`/`↓` | Navigate rows | — |
| `enter` | Open detail view | — |
| `d` | Delete (with `confirmDelete` modal) | — |
| `e` | Edit title inline | — |
| `E` | Edit exe path inline | — |
| `u` | Set F95 URL inline | — |
| `s` | Scrape metadata | Scrape metadata |
| `r` | Refresh game list | — |
| `g` | Download game | — |
| `/` | Focus filter input | — |
| `<`/`>` | Previous/next sort field | — |
| `?` | Toggle help overlay | Toggle help overlay |

The `confirmDelete` flow: press `d` → model enters `confirmDelete = true` → `handleDeleteConfirm(key)` → `y` confirms (fires `gameDeletedMsg`), anything else cancels.

## Message Passing

Custom message types defined in `model.go` for async operations. To add a new one:

1. Define the struct: `type myMsg struct { result T; err error }`
2. Create a `tea.Cmd` in `commands.go`: `return func() tea.Msg { ...; return myMsg{...} }`
3. Add a `case myMsg:` handler in `update.go`'s type switch
4. Update the view rendering if needed

Existing message types:

```go
gamesLoadedMsg       // games loaded from DB
metaLoadedMsg        // scraped metadata loaded
gameDeletedMsg       // game deletion result (from confirmDelete → y)
metaScrapedMsg       // scrape result
detailGameLoadedMsg  // detail view game loaded
filterTickMsg        // debounced filter timer tick (200ms debounce)
downloadProgressMsg  // periodic download progress update (~500ms)
downloadStartedMsg   // download initiation result
errMsg               // generic error
```

## Update Pattern

`update.go` uses a type switch on `tea.Msg`:

```go
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.WindowSizeMsg:  // resize — reflow table
    case tea.KeyMsg:         // keyboard — delegate to mode handler
    case gamesLoadedMsg:     // DB results — update filtered list
    case filterTickMsg:      // 200ms debounce → reload games from DB
    case downloadProgressMsg: // periodic progress update
    }
}
```

Key handling delegates to mode-specific functions: `handleListKeys`, `handleDetailKeys`, `handleDeleteConfirm`, `handleExeKey`, `handleUrlInput`.

Filter debounce: key presses set `filterDirty = true` and schedule a `filterTickMsg` in 200ms. When the tick arrives, the filter query executes. This prevents DB hammering on fast typing.

## Styling

`styles.go` defines a dark theme with purple/magenta accents:
- `purple` = `"99"`, `purpleBg` = `"55"`, `purpleDim` = `"57"`
- `darkBg` = `"236"`, `subtle` = `"241"`, `white` = `"255"`
- `red` = `"196"`, `green` = `"82"`, `yellow` = `"220"`, `cyan` = `"45"`

Engine colors are cached in `engineStyles` map for performance. Each engine gets a distinct color for the table column.

## Adding a New View

1. Add a `ViewMode` constant in `model.go`
2. Create a render method (e.g., `func (m model) myView() string`)
3. Add the view switch in `View()` (`model.go:233`)
4. Add key handlers in `update.go` for that mode
5. Update `helpView()` in `help.go` with keybindings
6. Update `keys.go` with new key type constants

## Async Commands

Commands factory (`commands.go`) creates `tea.Cmd` values for async work:

```go
func (m model) loadGames() tea.Cmd { ... }      // DB query → gamesLoadedMsg
func (m model) scrapeMeta(id int64) tea.Cmd { ... } // HTTP scrape → metaScrapedMsg
func (m model) startDownload(id int64) tea.Cmd { ... } // download → downloadStartedMsg
```

Pattern: return `func() tea.Msg { ... return resultMsg{...} }` from command factories.

## Known Constraints

- No scrollable detail view — content must fit within terminal
- Filter is debounced (200ms `filterTickMsg` timer) to avoid DB hammering
- TUI init tries loading F95 cookies; nil scraper client is gracefully handled (features degrade)
- Lipgloss can panic on very narrow terminals (<40 cols) — guard with min-width checks
- `confirmDelete` state is boolean, not stackable — only one confirmation at a time
