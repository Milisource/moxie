# Database

## What

An embedded SQLite database that stores game metadata, scraped F95Zone enrichment data, and version tracking state. Single file at `~/.config/moxie/games.db`. Created automatically on first run. No server, no daemon, no configuration.

## How

### Package Structure

The database package is split across 8 files in `internal/db/`:
- `db.go` — `Database` struct, connection (`Open`/`Close`), schema migration (`migrate`), stats (`GameCount`, `TotalSize`), nullable helpers
- `models.go` — `Game`, `GameSummary`, `PlayHistory`, `PlayHistoryWithGame`, `GameSeries`, `Collection`, `Download`, `DownloadLink`, `ScrapedMeta` structs
- `games.go` — Game CRUD (`InsertGame`, `GetGame`, `ListGames`, `SearchGames`, `UpdateGame`, `DeleteGame`, `DeleteGamePermanent`, `RestoreGame`), dedicated query methods (`GamesNeedingUpdate`, `GamesWithF95URL`, `GamesWithoutF95URL`, `GamesByStatus`, `GamesByEngine`), targeted column updates (`UpdateGameTitle`, `UpdateGameStatus`, `UpdateGameF95URL`, `UpdateGameExePath`), play history CRUD (`RecordPlay`, `RecentPlays`), series CRUD (`CreateSeries`, `SetGameSeries`), lightweight queries (`ListGameSummaries`, `ListDeletedSummaries`)
- `downloads.go` — Download job CRUD
- `download_links.go` — Download link CRUD including `AllDownloadLinks` (JOIN query), dead-link marking
- `scraped_meta.go` — Scraped metadata CRUD (`UpsertScrapedMeta`, `GetScrapedMeta`)
- `collections.go` — Collection CRUD (`CreateCollection`, `ListCollections`, `GetCollection`, `DeleteCollection`, `AddGameToCollection`, `RemoveGameFromCollection`, `GetGamesInCollection`, `ListGameSummariesInCollection`)

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
    engine      TEXT NOT NULL CHECK (engine IN ('ADRIFT','Flash','HTML','Java','Others','QSP','RAGS','RPGM','RenPy','Tads','Unity','UnrealEngine','WebGL','WolfRPG','Unknown')),
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
    wine_prefix  TEXT,                    -- custom Wine prefix path (WINEPREFIX)
    last_scanned_at TEXT,                -- last incremental scan time
    dir_mtime TEXT,                      -- directory modification time for incremental scan
    series_id    INTEGER REFERENCES game_series(id),  -- FK to game series
    series_order INTEGER DEFAULT 0,      -- order within series
    deleted_at TEXT,                     -- soft delete timestamp (NULL = active)
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

-- Play history — records each time a game is launched
play_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    game_id INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    played_at TEXT NOT NULL DEFAULT (datetime('now')),
    duration_s INTEGER DEFAULT 0,
    platform TEXT DEFAULT ''
)

-- Game series — groups related games (e.g. episodic)
game_series (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE
)

-- User-created collections for custom grouping
collections (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    created_at TEXT DEFAULT (datetime('now'))
)

-- Game-to-collection mapping (many-to-many)
game_collections (
    game_id INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    collection_id INTEGER NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    PRIMARY KEY (game_id, collection_id)
)
```

Indexes:
- `idx_games_engine`, `idx_games_title` (NOCASE), `idx_games_path`,
- `idx_downloads_game_id`, `idx_downloads_status`,
- `idx_download_links_game_id`, `idx_download_links_platform`, `idx_download_links_is_dead`,
- `idx_play_history_game`, `idx_play_history_played`

### FTS5 Full-Text Search

A virtual FTS5 table provides full-text search across title, tags, developer, and overview:

```sql
CREATE VIRTUAL TABLE games_fts USING fts5(
    title, tags, developer, overview,
    content='games',
    content_rowid='id'
);
```

Content sync triggers on `games` (INSERT/UPDATE/DELETE) and `scraped_meta` (INSERT/UPDATE/DELETE) keep the FTS index in sync automatically. For simple multi-word queries, the `SearchGames()` method auto-wraps each word as a prefix query (`"term"*`). Advanced FTS5 syntax (operators, phrases) is passed through. A `LIKE` substring fallback handles queries that produce no FTS5 results.

### Version Tracking

Two columns on the `games` table handle update detection:
- `latest_version` — the version string last observed when scraping the F95Zone thread
- `version_checked_at` — timestamp of the last `check-updates` or `sync` run

When `check-updates` scrapes a thread:
1. If `latest_version` differs from the scraped version, the game is reported as having an update available
2. Both `latest_version` and `version_checked_at` are updated regardless of whether a newer version was found

This means `latest_version` is the "last known F95Zone version," distinct from `version` (which may be the user-entered version or the version detected at scan time).

### Migration Strategy

Migrations use version-gated steps via `PRAGMA user_version`:

```go
const currentSchemaVersion = 7

// Query current version
var userVersion int
conn.QueryRow("PRAGMA user_version").Scan(&userVersion)

// For pre-migration databases, apply old ALTER TABLE steps
// with column-existence checks (idempotent)
if userVersion < 1 {
    upgradeOldDB(conn)
}

// Apply version-gated migrations from userVersion+1 up to currentSchemaVersion
for v := userVersion + 1; v <= currentSchemaVersion; v++ {
    migrateVersionStep(conn, v)  // each step wrapped in a transaction
}
```

Each `migrateVersionStep` handles a specific version:
- **v1**: Core tables (CREATE TABLE IF NOT EXISTS with all current columns)
- **v2**: FTS5 virtual table + triggers + initial population
- **v3**: play_history, game_series tables; series_id/series_order ALTER TABLE
- **v4**: Soft delete (`deleted_at` column)
- **v5**: Game collections (collections + game_collections tables)
- **v6**: Repair step — ensures all schema columns exist (handles old DBs that bumped past migration steps)
- **v7**: Per-game Wine prefix (`wine_prefix TEXT` column on games)

This replaces the earlier approach of running bare `ALTER TABLE` statements that ignored errors. All migration steps are idempotent (use `columnExists` checks for ALTER TABLE, `CREATE TABLE IF NOT EXISTS` for new tables).

An auto-purge step at the end of migration deletes games soft-deleted more than 30 days.

## Why

**SQLite over PostgreSQL** — single binary, no server, no Docker, no connection pooling. The tool opens the DB, reads/writes, and closes. No daemon lifecycle.

**ncruces/go-sqlite3 over mattn/go-sqlite3** — pure Go means `CGO_ENABLED=0` works. Cross-compilation is `GOOS=windows go build` with no extra toolchain. The mattn driver requires CGO and a C compiler for each target platform.

**FTS5 over LIKE** — for libraries of 1,000+ games, `LIKE '%query%'` on title-only is too slow and misses tags/developer/overview. FTS5 provides ranked results with phrase and prefix search, indexed via triggers for zero-maintenance sync.

**JSON in a TEXT column** — tags are stored as `'["rpg","fantasy","3d"]'`. This avoids a join table for a field that's almost always read/written as a unit. The `marshalTags`/`unmarshalTags` helpers serialize to/from `[]string`. This would be wrong for query-heavy tag filtering, but for a local library under 10,000 games, it's fine — filtering is done in-memory by the TUI anyway.

**Version-gated migrations over bare ALTER TABLE** — bare `ALTER TABLE` with error-swallowing makes it impossible to distinguish "column already exists" (benign) from "disk full" (fatal). Version-gated steps with `PRAGMA user_version`, transactions, and `columnExists()` checks provide atomic upgrades, clear diagnostics, and safe re-runs.
