# moxie Go Development

Go CLI/TUI tool for scanning, cataloging, enriching, and launching locally-stored games from F95Zone threads.

## Project Identity

- **Module:** `github.com/mili/moxie`
- **Go version:** See `go.mod` `go` directive (currently 1.26.2)
- **CGO:** Disabled (`CGO_ENABLED=0`) — pure Go static binary
- **Targets:** linux/darwin/windows × amd64/arm64 = 6 targets
- **License:** WTFPL v2
- **Version:** Read from `main.go:15` var, stamped via `-ldflags -X main.version=$(VERSION)`

## Quality Gate

Run before every push:

```bash
go mod tidy && go build ./... && go vet ./... && go test ./...
```

Optional extras: `go test -race ./...`, `go test -short ./...` (skip slow integration tests).

## Build System

`make build` — builds native binary to `dist/moxie`:
- `CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=$(VERSION)" -o dist/moxie .`

`make build-all` — cross-compiles all 6 targets (via `build-linux`, `build-macos`, `build-windows`).

Use `./scripts/build.sh` for shell-based cross-compilation. Use `./scripts/release.sh` for full release (tag + build + GitHub Release via `gh` CLI).

## Code Organization

All library code lives in `internal/` — no external packages. The import graph is a strict DAG with no circular imports:

```
main → commands → {db, scraper, scanner, steam, config, downloader}
                    ↓                        ↓
                   tui ─→ {db, scraper, downloader}
                    ↓
       {engine, util, log, browser, archive, updater}
```

`tui` imports `db`, `scraper`, and `downloader` for async operations. `commands` imports `downloader` for link management. Everything converges on utility packages at the bottom.

**Rules:**
- `internal/commands/` are thin CLI handlers — parse flags, call domain packages, format output. No business logic.
- Business logic lives in domain packages (`internal/engine/`, `internal/scraper/`, `internal/steam/`, etc.).
- `internal/util/` for shared helpers (version normalization, string utilities).
- `internal/log/` for structured logging wrapper around `log/slog`.

## Testing Patterns

- Standard `testing` package only — no testify/ginkgo/gomega.
- File naming: `*_test.go` co-located with source.
- Use `t.Parallel()` for test concurrency.
- Table-driven tests with `tc` structs (`name`, `input`, `expected`).
- Inject test servers via interfaces (e.g., `NewClientWithHTTP` for scraper, `httptest.Server`).
- DB tests use `t.TempDir()` for temp files.
- Run benchmarks: `go test -bench=. ./internal/db/`

## Key Dependencies

| Dependency | Purpose |
|------------|---------|
| `ncruces/go-sqlite3` | Embedded SQLite (pure Go, no CGO) |
| `charmbracelet/bubbletea` | TUI framework (Elm architecture) |
| `charmbracelet/bubbles` | TUI components (table, textinput) |
| `charmbracelet/lipgloss` | Terminal styling |
| `PuerkitoBio/goquery` | HTML scraping (jQuery-like selectors) |
| `browserutils/kooky` | Cross-browser cookie extraction |
| `andygrunwald/vdf` | Valve VDF parsing (Steam shortcuts) |
| `golang.org/x/image` | Image processing (artwork resize) |

## Conventions

- **Errors — two patterns:**
  - **Domain packages** (`internal/engine/`, `internal/scraper/`, etc.):
    `return fmt.Errorf("context: %w", err)`. Use `errors.As` for type assertion.
  - **CLI handlers** (`internal/commands/`):
    `fmt.Fprintf(os.Stderr, "error: %v\n", err); os.Exit(1)` — user-facing, non-recoverable.
- **Logging:** Use `log.Info`, `log.Warn`, `log.Debug`, `log.Error` with key-value pairs. Never `fmt.Println` for diagnostics.
- **Exit codes:** CLI commands exit with `os.Exit(1)` on failure.
- **Config:** `~/.config/moxie/` with JSON config file, atomic writes.
- **Database:** `~/.config/moxie/moxie.db` with 0600 permissions, WAL mode.
- **Version stamping:** `main.version` var set via `-ldflags` at build time.
- **Constants:** Group at package top. Use `const` with `iota` for enums.
- **No init functions:** Prefer explicit initialization in `Open()`, `NewClient()`, etc.
