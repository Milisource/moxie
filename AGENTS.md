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
   bd dolt push
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
<!-- END BEADS INTEGRATION -->
