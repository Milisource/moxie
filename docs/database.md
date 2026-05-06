# Database

## What

An embedded SQLite database that stores game metadata, scraped F95Zone enrichment data, and version tracking state. Single file at `~/.config/moxie/games.db`. Created automatically on first run. No server, no daemon, no configuration.

## How

### Package Structure

The database package is split across 6 files in `internal/db/`:
- `db.go` — `Database` struct, connection (`Open`/`Close`), schema migration (`migrate`), stats (`GameCount`, `TotalSize`), nullable helpers
- `models.go` — `Game`, `Download`, `DownloadLink`, `ScrapedMeta` structs
- `games.go` — Game CRUD (`InsertGame`, `GetGame`, `ListGames`, `SearchGames`, `UpdateGame`, `DeleteGame`)
- `downloads.go` — Download job CRUD
- `download_links.go` — Download link CRUD including dead-link marking
- `scraped_meta.go` — Scraped metadata CRUD (`UpsertScrapedMeta`, `GetScrapedMeta`)

### Driver

Uses `ncruces/go-sqlite3` — a pure Go SQLite driver (no CGO). This means the binary cross-compiles trivially: `GOOS=windows go build` produces a working Windows binary without a cross-compiled C toolchain. The driver implements the `database/sql` interface, swapped in via `import _ "github.com/ncruces/go-sqlite3/driver"`.

### Connection Setup

```
PRAGMA journal_mode = WAL     -- concurrent reads allowed
PRAGMA foreign_keys = ON       -- cascading deletes work
```

### Schema

```sql
-- Core game library
games (
    id          INTEGER PRIMARY KEY,
    title       TEXT NOT NULL,
    engine      TEXT NOT NULL CHECK (engine IN (...14 canonical names..., 'Unknown')),
    path        TEXT NOT NULL UNIQUE,
    exe_path    TEXT,
    version     TEXT,
    size_bytes  INTEGER DEFAULT 0,
    f95_url     TEXT,
    f95_thread_id INTEGER,
    tags        TEXT DEFAULT '[]',       -- JSON array string
    status      TEXT DEFAULT 'unknown' CHECK (status IN ('active','completed','abandoned','on_hold','unknown')),
    notes       TEXT DEFAULT '',
    latest_version   TEXT,               -- most recent version seen on F95Zone
    version_checked_at TEXT,             -- last check-updates timestamp
    store_links TEXT DEFAULT '{}',       -- store URLs as JSON map: {"steam":"https://...","itch":"https://..."}
    steam_app_id INTEGER,                -- Steam App ID extracted from store_links["steam"]
    created_at  TEXT DEFAULT (datetime('now')),
    updated_at  TEXT DEFAULT (datetime('now'))
)

-- Scraped F95Zone metadata (1:1 with games)
scraped_meta (
    game_id      INTEGER PRIMARY KEY REFERENCES games(id) ON DELETE CASCADE,
    developer    TEXT,
    overview     TEXT,
    cover_url    TEXT,
    last_scraped TEXT DEFAULT (datetime('now'))
)

-- Download jobs (track active/completed/failed downloads)
downloads (
    id INTEGER PRIMARY KEY,
    game_id INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    url TEXT NOT NULL,
    host TEXT,
    filename TEXT,
    dest_path TEXT,
    status TEXT DEFAULT 'pending',  -- pending, downloading, paused, completed, failed, cancelled, extracting
    bytes_downloaded INTEGER DEFAULT 0,
    total_bytes INTEGER DEFAULT 0,
    speed_bytes_per_sec REAL DEFAULT 0,
    percent_complete REAL DEFAULT 0,
    error TEXT,
    started_at TEXT,
    completed_at TEXT,
    created_at TEXT DEFAULT (datetime('now'))
)

-- Download links scraped from F95Zone threads
download_links (
    id INTEGER PRIMARY KEY,
    game_id INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    url TEXT NOT NULL,
    host TEXT,
    name TEXT,
    platform TEXT DEFAULT 'unknown',  -- linux, windows, macos, all, unknown
    is_dead INTEGER DEFAULT 0,
    dead_reason TEXT,
    last_checked TEXT,
    created_at TEXT DEFAULT (datetime('now'))
)
```

Indexes: `idx_games_engine`, `idx_games_title` (NOCASE), `idx_games_path`, `idx_downloads_game_id`, `idx_downloads_status`, `idx_download_links_game_id`, `idx_download_links_platform`, `idx_download_links_is_dead`.

### Version Tracking

Two columns on the `games` table handle update detection:
- `latest_version` — the version string last observed when scraping the F95Zone thread
- `version_checked_at` — timestamp of the last `check-updates` or `sync` run

When `check-updates` scrapes a thread:
1. If `latest_version` differs from the scraped version, the game is reported as having an update available
2. Both `latest_version` and `version_checked_at` are updated regardless of whether a newer version was found

This means `latest_version` is the "last known F95Zone version," distinct from `version` (which may be the user-entered version or the version detected at scan time).

### Migration Strategy

Migrations use `ALTER TABLE ADD COLUMN` statements and `CREATE TABLE IF NOT EXISTS` that are safe to run repeatedly:

```go
// SQLite ignores errors on ALTER TABLE for columns that already exist
conn.Exec("ALTER TABLE games ADD COLUMN latest_version TEXT")
conn.Exec("ALTER TABLE games ADD COLUMN version_checked_at TEXT")
conn.Exec("ALTER TABLE games ADD COLUMN store_links TEXT DEFAULT '{}'")
conn.Exec("ALTER TABLE games ADD COLUMN steam_app_id INTEGER")

// New tables use IF NOT EXISTS
conn.Exec(`
    CREATE TABLE IF NOT EXISTS downloads (...);
    CREATE TABLE IF NOT EXISTS download_links (...);
`)
```

This pattern allows adding new schema without versioned migration tracking. The `PRAGMA user_version` is set but not actively used for migration gating — the approach is idempotent and simpler for a single-user hobby project.

## Why

**SQLite over PostgreSQL** — single binary, no server, no Docker, no connection pooling. The tool opens the DB, reads/writes, and closes. No daemon lifecycle.

**ncruces/go-sqlite3 over mattn/go-sqlite3** — pure Go means `CGO_ENABLED=0` works. Cross-compilation is `GOOS=windows go build` with no extra toolchain. The mattn driver requires CGO and a C compiler for each target platform.

**JSON in a TEXT column** — tags are stored as `'["rpg","fantasy","3d"]'`. This avoids a join table for a field that's almost always read/written as a unit. The `marshalTags`/`unmarshalTags` helpers serialize to/from `[]string`. This would be wrong for query-heavy tag filtering, but for a local library under 10,000 games, it's fine — filtering is done in-memory by the TUI anyway.

**No FTS5** — `LIKE '%query%'` on the title column is sufficient for libraries under 1,000 games. FTS5 full-text search can be added later (it's a SQLite compile-time option supported by ncruces/go-sqlite3).
