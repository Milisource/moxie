# moxie — MVP Specification

**Version:** 0.3.0-alpha (May 2026)
**Status:** Alpha — 0.3.0 (packages refactored: util + commands, engine cleanup, version extraction)
**Target:** CLI/TUI → Multi-platform Wails desktop app

---

## Overview

A local game library manager for adult games. Scans directories, detects engines (14 canonical + 3 community → Others), matches games to F95Zone threads for metadata, and presents results in a terminal UI. Built as a single static Go binary with embedded SQLite.

---

## Component Documentation

| Component | Document | Covers |
|---|---|---|
| **Architecture** | [architecture.md](architecture.md) | Package diagram, data flow, design rationale, future path |
| **Scanner** | [scanner.md](scanner.md) | Directory walk, engine detection profiles, exclusion list, limitations |
| **Scraper** | [scraper.md](scraper.md) | HTTP client, rate limiting, HTML parsing, auto-association |
| **Commands** | (this doc) | 28 CLI handlers in 8 domain files: crud, scrape, sync, cleanup, play, steam, rename, config |
| **Utilities** | (this doc) | Shared config I/O, formatters, version normalization in `internal/util/` |
| **TUI** | [tui.md](tui.md) | Bubble Tea model/update/view, keyboard shortcuts, filters |
| **Database** | [database.md](database.md) | SQLite schema, version tracking, migration strategy |
| **Browser** | [browser.md](browser.md) | Cross-browser cookie extraction with kooky + SQLite fallback |
| **Steam** | [steam-package-design.md](steam-package-design.md) | Steam shortcut management, Proton config, artwork, SteamGridDB |

---

## Implementation Status

### Completed (MVP)

- [x] 28 CLI entry points: all previous + `cleanup`, `refresh-versions`, `scrape-batch`, `set-path`, `set-exe`
- [x] Recursive directory scanning with smart `SkipDir` on game roots
- [x] Engine detection for 14 canonical engines + NW.js (RPG Maker MV/MZ) detection
- [x] SQLite database with WAL mode, foreign keys, CHECK constraints, LatestVersion tracking
- [x] Cookie-based F95Zone scraping (Firefox auto-detect, explicit, SQLite fallback)
- [x] Auto-association via F95Zone search with title scoring + engine mismatch prevention
- [x] Bubble Tea TUI with list/detail views, engine/status filters, engine colors
- [x] Version tracking with `LatestVersion` fallback for unknown local versions
- [x] Version normalization (trailing .0 stripping, v-prefix handling)
- [x] `--force` flag for bypassing 24h update-check cooldown
- [x] Cleanup command: engine mismatch detection, exe mismatch detection, interactive disassociation
- [x] Engine compatibility map (RPGM↔HTML via NW.js, WolfRPG↔HTML)
- [x] `--warnings` flag on `list` command
- [x] 325+ tests across scanner, engine, scraper, DB, helpers, commands

### Upcoming

- [ ] DB migration: store_links column for persistent Steam/Itch.io links
- [ ] SGDB artwork activation (currently pending DB StoreLinks migration)
- [ ] FTS5 full-text search
- [ ] Cover image download and local caching
- [ ] Download manager / file organizer
- [ ] Directory watcher (auto-scan on file changes)
- [ ] GitHub Actions release pipeline
- [ ] Export/import library (JSON backup)
- [ ] Wails desktop GUI

### Known Limitations

- **No FTS5** — uses `LIKE '%query%'` on title only
- **False positives** — tool/editor directories and generic folder names may be misdetected as games
- **No archive scanning** — `.zip`/`.rar`/`.7z` at scan roots are not inspected
- **No content-based dedup** — same game in multiple paths creates duplicate records
- **No cover caching** — cover URLs are stored but images are not downloaded
- **Non-UTF-8 filenames** — Latin1/Shift-JIS display incorrectly in the TUI
- **No Wine detection** — `play` command assumes `wine` is on PATH for `.exe` files on Linux
