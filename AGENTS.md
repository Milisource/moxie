# Agent Instructions

This project uses **bd** (beads) for issue tracking. Run `bd onboard` to get started.

## Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work atomically
bd close <id>         # Complete work
bd dolt push          # Push beads data to remote
```

## Non-Interactive Shell Commands

**ALWAYS use non-interactive flags** with file operations to avoid hanging on confirmation prompts.

Shell commands like `cp`, `mv`, and `rm` may be aliased to include `-i` (interactive) mode on some systems, causing the agent to hang indefinitely waiting for y/n input.

**Use these forms instead:**
```bash
# Force overwrite without prompting
cp -f source dest           # NOT: cp source dest
mv -f source dest           # NOT: mv source dest
rm -f file                  # NOT: rm file

# For recursive operations
rm -rf directory            # NOT: rm -r directory
cp -rf source dest          # NOT: cp -r source dest
```

**Other commands that may prompt:**
- `scp` - use `-o BatchMode=yes` for non-interactive
- `ssh` - use `-o BatchMode=yes` to fail instead of prompting
- `apt-get` - use `-y` flag
- `brew` - use `HOMEBREW_NO_AUTO_UPDATE=1` env var

## Project: moxie

moxie is a Go CLI/TUI tool (~16 MB static binary) for scanning, cataloging, enriching, and launching locally-stored games from F95Zone threads. It detects game engines (14 types), scrapes F95Zone for metadata, and provides an interactive Bubble Tea terminal UI.

- **Language:** Go 1.26+
- **Dependencies:** Pure Go (no CGO) — cross-compiles to linux/darwin/windows
- **Database:** SQLite via `ncruces/go-sqlite3` (embedded, no server)
- **TUI:** Bubble Tea + Lipgloss + Bubbles

### Documentation

Documentation lives in `docs/`. Keep it in sync with code changes — update the relevant doc when you add, remove, or change behavior. Outdated docs are worse than no docs.

| Doc | Covers |
|-----|--------|
| `docs/moxie-spec.md` | Version, implementation status, roadmap, known limitations |
| `docs/architecture.md` | Package diagram, data flow, design decisions, future path |
| `docs/scanner.md` | Directory walk algorithm, engine detection profiles |
| `docs/scraper.md` | HTTP client, rate limiting, HTML parsing, auto-association |
| `docs/database.md` | SQLite schema, version tracking, migration strategy |
| `docs/tui.md` | Bubble Tea model/update/view, keyboard shortcuts, filters |
| `docs/browser.md` | Cross-browser cookie extraction with kooky |
| `docs/steam-package-design.md` | Steam shortcut management, artwork, Proton, SteamGridDB |

**When to update docs:**
- New feature → add to `moxie-spec.md` Completed list + update component doc
- Bug fix that changes behavior → note in relevant component doc
- New package or major refactor → update `architecture.md`
- Every release → bump version in `moxie-spec.md`, add entry to `CHANGELOG.md`

### Git Workflow

```
dev (working branch)              main (release branch)
──────────────────                ────────────────────
  │                                  │
  ├─ commit often, small chunks      │
  ├─ verify before pushing:          │
  │   go build ./...                 │
  │   go vet ./...                   │
  │   go test ./...                  │
  ├─ push to origin/dev              │
  │                                  │
  └─ when ready for release ─────────┤
     git checkout main               │
     git merge dev                   │
     ./scripts/release.sh            │
```

### Commit Rules

- **Verify before pushing.** Never push code that doesn't build or has failing tests. Run the quality gate:
  ```bash
  go build ./... && go vet ./... && go test ./...
  ```
- **Commit frequently.** Small, atomic commits are easier to review, revert, and bisect than monolithic ones.
- **Use conventional prefixes:**
  - `feat:` — new feature
  - `fix:` — bug fix
  - `docs:` — documentation only
  - `refactor:` — code restructuring (no behavior change)
  - `chore:` — build, deps, tooling, gitignore
  - `test:` — adding or updating tests
- **Push to `dev`**, not `main`. Main is for tagged releases only.

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:ca08a54f -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

## Session Completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER push on your own, the user will push.
<!-- END BEADS INTEGRATION -->

<!-- BEGIN OPENGUIDE STRATEGY -->
## OpenCode Configuration & Strategy

### Agent Configuration

Two Go-specific subagents are registered in `~/.config/opencode/opencode.json`:

| Agent | Model | Temp | Purpose |
|-------|-------|------|---------|
| `go-moxie` | flash | 0.1 | Go dev: build, test, refactor, debug |
| `go-tester` | flash | 0.1 | Test writing with project conventions |

Both are registered in swarm-orchestrator's `permission.task` allowlist. Agent definition files with full context live in `~/.config/opencode/agents/go-moxie.md` and `go-tester.md`.

### Skill Loading Strategy

| When working on... | Load this skill |
|---|---|
| Any `.go` file | `go-moxie` |
| `internal/tui/*` | `bubbletea-tui` |
| `internal/scraper/*` | `f95zone-scraper` |
| `internal/downloader/*` | `downloader-hosts` |
| `internal/steam/*` | `steam-packaging` |
| `internal/db/*` | `sqlite-database` |
| `internal/engine/*` | `engine-detection` |

Skills live in `.opencode/skills/<name>/SKILL.md` at the project root. They autoload when files matching the description are opened.

### MCP Tool Strategy

| MCP Server | Status | Use for |
|------------|--------|---------|
| `context7` | ✅ Enabled | Go library API docs (ncruces/go-sqlite3, charmbracelet/bubbletea, goquery, etc.) |
| `searxng` | ✅ Enabled | F95Zone site changes, Steam API docs, general web research |
| `chrome-devtools` | ✅ Enabled | Debugging web-related features, inspecting HTML for scraper parser updates |
| `codegraph` | ✅ Enabled | Codebase-wide refactoring analysis, dependency graph visualization — DB lives at `/mnt/milk/F95/.codegraph/` via symlink |

### Plugin Status

| Plugin | Purpose | Status |
|--------|---------|--------|
| `opencode-beads` | Issue tracking & workflow | ✅ Installed |
| `opencode-snippets` | Reusable code templates | ✅ Installed |
| `oc-crofai` | Unknown — needs investigation | ⚠ Installed, undocumented |

### Subagent Delegation Strategy

| Task | Subagent | Why |
|------|----------|-----|
| Bug investigation | `debugger` | Systematic root cause analysis |
| Code review | `code-review` | Framework-agnostic, catches convention drift |
| Architecture design | `architect` + `oracle` | Plan + red-team review before implementation |
| Refactoring | `refactorer` | Plans changes before editing, reduces risk |
| Security audit | `security-engineer` | SSRF, path traversal, cookie handling |
| Schema changes | `database-architect` | Migration planning, index optimization |
| External API research | `researcher` | Fetches docs while primary agent continues |
| Test writing | `test-engineer` | Project-pattern-aware test generation |
| Complex features | `swarm-orchestrator` | Breaks into parallel subtasks |
| Codebase exploration | `explore` | Fast file/code search across 116+ files |

### Workflow Patterns

**Adding a new engine type:**
1. Load `engine-detection` skill
2. Add `Engine` const + profile in `detector.go`
3. Add color in `tui/styles.go`
4. Add to CHECK constraint in `db/db.go`
5. Add to engine tags in `engine_tags.go`
6. Load `sqlite-database` skill for migration step
7. Run quality gate

**Adding a new download host:**
1. Load `downloader-hosts` skill
2. Create `hosts_<name>.go` with `HostResolver` interface
3. Register in `hosts.go` factory
4. Update link scoring in `links.go`
5. Load `go-moxie` skill to write tests
6. Run `go test ./internal/downloader/...`

**Adding a new CLI command:**
1. Create handler in `internal/commands/<name>.go`
2. Add dispatch in `main.go` switch
3. Update `printUsage()` in `main.go`
4. Update `docs/moxie-spec.md` Completed list

### Getting Best Model Performance

1. **Load the relevant skill first** — skills provide full project context without cluttering the prompt
2. **Use `architect` + `oracle` for big changes** — plan first with `oracle` as red team, then implement
3. **Use `swarm-orchestrator` for cross-package features** — parallelizes independent subtasks
4. **Use `codegraph` MCP** for dependency analysis before refactoring
5. **Always run `go build ./... && go vet ./... && go test ./...`** — never skip the quality gate
6. **Keep commits atomic** — `feat:` for features, `fix:` for bugs, `refactor:` for restructuring
7. **Document behavior changes** — update relevant `docs/*.md` alongside code changes
<!-- END OPENGUIDE STRATEGY -->
