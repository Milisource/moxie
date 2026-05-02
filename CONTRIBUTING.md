# Contributing to Moxie

Thanks for taking an interest! This is a hobby project, so things are lightweight. Here's what you need to know.

## Getting Started

```bash
# Clone the repo
git clone https://github.com/mili/f95-game-manager
cd f95-game-manager

# Build
go build -ldflags="-s -w" -o moxie .

# Run tests
go test ./...

# Build for all platforms
./scripts/build.sh all
```

## Code Structure

All library code lives under `internal/`, organized by domain:

```
moxie/
├── main.go              # Entry point — CLI with 11 commands (flag parsing + output only)
│
├── internal/
│   ├── browser/         # (1 file) — Cross-browser cookie extraction via kooky
│   ├── engine/          # (2 files) — Engine detection: 14 canonical + 3 community profiles
│   ├── scanner/         # (2 files) — WalkDir-based directory scanning and game root detection
│   ├── scraper/         # (8 files) — HTTP client, XenForo HTML parser, search, auto-association
│   ├── db/              # (3 files) — SQLite CRUD, schema migrations, Game/ScrapedMeta models
│   └── tui/             # (9 files) — Bubble Tea TUI (model, update, views, styles, commands)
│
├── scripts/
│   ├── build.sh         # Cross-compilation (linux/mac/windows)
│   └── install.sh       # Build + install to ~/.local/bin/
│
├── docs/
│   ├── architecture.md  # System design and rationale
│   ├── scanner.md       # Directory scanning and engine detection details
│   ├── scraper.md       # HTTP scraper, rate limiting, auto-association
│   ├── tui.md           # Bubble Tea TUI architecture and keyboard reference
│   ├── database.md      # SQLite schema, version tracking, migrations
│   ├── browser.md       # Cross-browser cookie extraction
│   └── f95-game-manager-spec.md  # Index to component docs + status checklist
│
├── README.md            # User-facing: quick start, command reference, workflows
├── CONTRIBUTING.md      # This file
├── go.mod / go.sum
```

**Rules of thumb:**
- `main.go` does flag parsing and output formatting only — no business logic.
- Each `internal/` package is self-contained with its own tests.
- Packages import only from the standard library and declared dependencies — no circular imports.

## Tests

```bash
# Run all tests
go test ./...

# With race detector
go test -race ./...

# Benchmarks (DB)
go test -bench=. ./internal/db/

# Skip slow tests
go test -short ./...
```

Tests live alongside the code they test (`detector_test.go`, `scanner_test.go`, `db_test.go`, `scraper_test.go`). Standard Go conventions: `testing.T` and `testing.B` only, no third-party test frameworks.

On pull requests, CI runs `go test ./...` and `go vet ./...`.

## Adding a New Engine Profile

Engine detection is defined in `internal/engine/detector.go`. Each profile is an entry in the `profiles` slice, checked in priority order (first match wins):

```go
{
    engine:     CustomEngine,
    confidence: 0.90,
    files:      []string{"engine.dll", "engine.ini"},
    subdirs:    []string{"engine_data"},
    extensions: []string{".cmp"},
    name:       "Custom Engine markers found",
},
```

Steps:
1. Add your engine constant to the `Engine` type in `detector.go`
2. Add it to the CHECK constraint in `internal/db/db.go`
3. Add detection profiles (ordered by confidence, most specific first)
4. Add test cases in `detector_test.go` for all new profiles
5. Add the engine to the TUI's `cycleEngineFilter` list in `internal/tui/update.go`
6. Add an engine color in `internal/tui/styles.go`

## Adding a New CLI Command

1. Add a case to the `switch` in `main.go`'s `main()` function
2. Write the command handler as a `cmd*` function in `main.go` (flag parsing + output only)
3. Delegate business logic to `internal/` packages
4. Add the command to `printUsage()` and the README command reference

## Code Style

- `go fmt` before committing
- No external dependencies unless well-justified (current deps: `go-sqlite3`, `goquery`, `kooky`, `bubbletea`, `bubbles`, `lipgloss`)
- Comments on exported functions; internal helpers can be light
- Error strings are lowercase, no trailing punctuation (Go convention)
- Test functions use `t.Parallel()` where safe

## Documentation

When you change architecture or add features:

- Update the relevant component doc in `docs/` (see the index in `docs/f95-game-manager-spec.md`)
- Update the command reference in `README.md` if you add or change a command
- Update the checklist in `docs/f95-game-manager-spec.md` if you're closing a tracked feature

## Pull Requests

1. Open an issue first if it's more than a trivial fix
2. Keep PRs focused on one thing
3. `go test ./...` passes and `go vet ./...` is clean
4. Update docs — add or update the relevant component doc

## Roadmap

See `docs/f95-game-manager-spec.md` for the upcoming features list.

If something isn't on the list and you want to build it, open an issue to discuss first — this is a hobby project with a specific scope.
