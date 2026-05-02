# moxie — MVP Specification

**Version:** 2.2 (May 2026)
**Status:** Phase 2 Complete (Steam integration + scraper optimizations)
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
| **TUI** | [tui.md](tui.md) | Bubble Tea model/update/view, keyboard shortcuts, filters |
| **Database** | [database.md](database.md) | SQLite schema, version tracking, migration strategy |
| **Browser** | [browser.md](browser.md) | Cross-browser cookie extraction with kooky |
| **Steam** | [steam-package-design.md](steam-package-design.md) | Steam shortcut management, Proton config, artwork, SteamGridDB |

---

## Implementation Status

### Completed (MVP)

- [x] 22 CLI entry points: `scan`, `list`, `info`, `add`, `scrape`, `scrape --auto`, `remove`, `rename`, `check-updates`, `sync`, `sync <id>`, `play`, `tui`, `steam add`, `steam remove`, `steam list`, `steam proton-list`, `steam proton-set`, `steam fix-artwork`, `config set`, `config get`, `config show`
- [x] Recursive directory scanning with smart `SkipDir` on game roots
- [x] Engine detection for 14 canonical engines + 3 community → Others
- [x] SQLite database with WAL mode, foreign keys, CHECK constraints
- [x] Cookie-based F95Zone scraping (Firefox auto-detect, explicit fallback)
- [x] Auto-association via F95Zone search with title scoring
- [x] Bubble Tea TUI with list/detail views, engine/status filters, engine colors
- [x] Version tracking (`latest_version`, `version_checked_at`)
- [x] Cross-compilation script (linux/mac/windows), install script
- [x] 170+ tests across scanner, engine, scraper, DB
- [x] Rate limiting with exponential backoff and block detection
- [x] Steam library integration: shortcuts.vdf read/write, grid artwork download, Proton configuration
- [x] SteamGridDB API client for premium artwork sourcing
- [x] Deterministic non-Steam AppID generation (CRC32-based)
- [x] Scraper optimizations: stale-skip, search deduplication, concurrent worker pool
- [x] Scraped store link extraction (Steam, Itch.io, dl.site from F95Zone threads)
- [x] Improved title matching with sequel detection (prevents "Corruption of Champions" matching "Corruption of Champions II")
- [x] Configuration management (moxie config set/get/show, JSON-backed)

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
