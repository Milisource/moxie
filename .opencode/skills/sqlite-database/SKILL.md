# SQLite Database — Embedded Game Library

Lives in `internal/db/` (files: db.go, models.go, games.go, scraped_meta.go, downloads.go, download_links.go + tests). Uses `ncruces/go-sqlite3` (pure Go, no CGO) for embedded SQLite storage.

## Schema

See authoritative schema at `internal/db/db.go:69-155` (the `migrate()` function). Key tables:

| Table | Purpose | Key Columns |
|-------|---------|-------------|
| `games` | Game library entries | id, title, engine (with CHECK constraint), path, version, f95_url, f95_thread_id, status |
| `scraped_meta` | Cached F95Zone metadata | game_id (FK→games), developer, overview, cover_url |
| `downloads` | Download history | game_id (FK→games), url, host, status, bytes_downloaded, progress |
| `download_links` | Scraped download links | game_id (FK→games), url, host, platform, is_dead |

Engine CHECK constraint (see `db.go:73` for current list):
```sql
CHECK (engine IN ('ADRIFT','Flash','HTML','Java','Others','QSP','RAGS','RPGM','RenPy','Tads',
       'Unity','UnrealEngine','WebGL','WolfRPG','Unknown'))
```

## Database Setup

`db.Open(path)` initializes and returns a `*Database`:
- Opens SQLite connection
- Sets file permissions to 0600
- Enables WAL mode (`PRAGMA journal_mode = WAL`)
- Enables foreign keys (`PRAGMA foreign_keys = ON`)
- Sets busy timeout to 5000ms (`PRAGMA busy_timeout = 5000`)
- Runs schema migration

## Migration Pattern

Idempotent migrations using `CREATE TABLE IF NOT EXISTS` and `ALTER TABLE ... ADD COLUMN`:

```go
func migrate(conn *sql.DB) error {
    // Core schema in CREATE TABLE IF NOT EXISTS (returns error if table exists — ignored)
    conn.Exec("CREATE TABLE IF NOT EXISTS games (...)")
    // Additive columns — Exec returns (Result, error), both discarded intentionally
    conn.Exec("ALTER TABLE games ADD COLUMN latest_version TEXT")
}
```

Additive columns use `ALTER TABLE` with ignored errors (SQLite has no `IF NOT EXISTS` for ALTER). Do NOT drop or rename columns — that requires `CREATE TABLE ... AS SELECT` migration with transaction.

## CRUD Patterns

Games CRUD in `games.go`, scraped metadata in `scraped_meta.go`, download history in `downloads.go`, download links in `download_links.go`. Models defined in `models.go`.

Use the `scanner` interface for type-safe row scanning:
```go
type scanner interface { Scan(dest ...any) error }
```

Null-value helpers handle Go type → SQL NULL:
```go
nullableString("")  → nil
nullableInt64(0)    → nil
nullableTime(zero)  → nil
```

## Adding a Column

1. Add `ALTER TABLE` in `migrate()` — errors for existing columns are ignored
2. Add field to the relevant model struct in `models.go`
3. Update INSERT/UPDATE/SELECT queries in the CRUD file
4. Update `docs/database.md` schema

## Concurrency

Single-user CLI tool: no concurrent write contention expected. The 5000ms busy_timeout handles any lock conflicts during `t.Parallel()` test execution.

Use `sql.DB` connection pooling (Go's default) — no custom pooling needed.
