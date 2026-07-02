package db

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"

	"github.com/mili/moxie/internal/log"
)

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// Database wraps a SQLite connection for game library management.
type Database struct {
	conn *sql.DB
}

// Open creates or opens the SQLite database at the given path, runs
// migrations, and returns a Database handle.
func Open(path string) (*Database, error) {
	conn, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	// Restrict database file permissions (sensitive data: game paths,
	// F95Zone metadata).
	os.Chmod(path, 0600)

	// Enable WAL mode and foreign keys.
	if _, err := conn.Exec("PRAGMA journal_mode = WAL"); err != nil {
		conn.Close()
		return nil, err
	}
	if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
		conn.Close()
		return nil, err
	}

	// Set a busy timeout so lock contention waits instead of immediately
	// failing with SQLITE_BUSY. 5000 ms is generous for single-user CLI use.
	if _, err := conn.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		conn.Close()
		return nil, err
	}

	// Run schema migration.
	if err := migrate(conn); err != nil {
		conn.Close()
		return nil, err
	}

	return &Database{conn: conn}, nil
}

// Close closes the underlying database connection.
func (db *Database) Close() error {
	return db.conn.Close()
}

// ---------------------------------------------------------------------------
// Schema migration
// ---------------------------------------------------------------------------

// currentSchemaVersion is the latest schema version known to this build.
// Increment when adding new migrations — never decrement or change past
// migration steps.
//
// Version history:
//  1. Core tables (games, scraped_meta, downloads, download_links) with all
//     current columns including latest_version, version_checked_at,
//     store_links, steam_app_id, last_scanned_at, dir_mtime.
//  2. FTS5 virtual table (games_fts) for full-text search across title, tags,
//     developer, and overview, with content sync triggers and initial population.
//  3. Play history (play_history table) and game series (game_series table)
//     with series_id and series_order on games.
//  4. Soft delete support: deleted_at TEXT column on games (NULL = active).
//  5. Game collections: collections table + game_collections join table.
const currentSchemaVersion = 6

// fts5Setup creates the FTS5 virtual table, content sync triggers, and populates
// existing games. Safe to run multiple times via IF NOT EXISTS and idempotent
// INSERT (FTS5 uses REPLACE semantics for same rowid).
//
// developer and overview live in scraped_meta, not games, so we use a
// standalone FTS5 table (no content='games') and keep it in sync via triggers
// on both games and scraped_meta.
//
// NOTE: The standard FTS5 "INSERT INTO t(t, rowid, ...) VALUES('delete', ...)"
// hidden-column syntax is not supported by this SQLite driver. We use
// DELETE FROM games_fts WHERE rowid = ? instead.
const fts5Setup = `
CREATE VIRTUAL TABLE IF NOT EXISTS games_fts USING fts5(
    title, tags, developer, overview
);

CREATE TRIGGER IF NOT EXISTS games_ai AFTER INSERT ON games BEGIN
    INSERT INTO games_fts(rowid, title, tags, developer, overview)
    VALUES (new.id, new.title, new.tags, COALESCE((SELECT sm.developer FROM scraped_meta sm WHERE sm.game_id = new.id), ''), COALESCE((SELECT sm.overview FROM scraped_meta sm WHERE sm.game_id = new.id), ''));
END;

CREATE TRIGGER IF NOT EXISTS games_ad AFTER DELETE ON games BEGIN
    DELETE FROM games_fts WHERE rowid = old.id;
END;

CREATE TRIGGER IF NOT EXISTS games_au AFTER UPDATE ON games BEGIN
    DELETE FROM games_fts WHERE rowid = old.id;
    INSERT INTO games_fts(rowid, title, tags, developer, overview)
    VALUES (new.id, new.title, new.tags, COALESCE((SELECT sm.developer FROM scraped_meta sm WHERE sm.game_id = new.id), ''), COALESCE((SELECT sm.overview FROM scraped_meta sm WHERE sm.game_id = new.id), ''));
END;

CREATE TRIGGER IF NOT EXISTS scraped_meta_ai AFTER INSERT ON scraped_meta BEGIN
    INSERT INTO games_fts(rowid, title, tags, developer, overview)
    VALUES (new.game_id, COALESCE((SELECT title FROM games WHERE id = new.game_id), ''), COALESCE((SELECT tags FROM games WHERE id = new.game_id), '[]'), new.developer, new.overview);
END;

CREATE TRIGGER IF NOT EXISTS scraped_meta_ad AFTER DELETE ON scraped_meta BEGIN
    DELETE FROM games_fts WHERE rowid = old.game_id;
END;

CREATE TRIGGER IF NOT EXISTS scraped_meta_au AFTER UPDATE ON scraped_meta BEGIN
    DELETE FROM games_fts WHERE rowid = old.game_id;
    INSERT INTO games_fts(rowid, title, tags, developer, overview)
    VALUES (new.game_id, COALESCE((SELECT title FROM games WHERE id = new.game_id), ''), COALESCE((SELECT tags FROM games WHERE id = new.game_id), '[]'), new.developer, new.overview);
END;
`

func migrate(conn *sql.DB) error {
	// Determine the current schema version.
	var userVersion int
	if err := conn.QueryRow("PRAGMA user_version").Scan(&userVersion); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	// Core tables definition — includes all current columns.
	coreTables := `
		CREATE TABLE IF NOT EXISTS games (
			id          INTEGER PRIMARY KEY,
			title       TEXT NOT NULL,
			engine      TEXT NOT NULL CHECK (engine IN ('ADRIFT','Flash','HTML','Java','Others','QSP','RAGS','RPGM','RenPy','Tads','Unity','UnrealEngine','WebGL','WolfRPG','Unknown')),
			path        TEXT NOT NULL UNIQUE,
			exe_path    TEXT,
			version     TEXT,
			size_bytes  INTEGER DEFAULT 0,
			f95_url     TEXT,
			f95_thread_id INTEGER,
			tags        TEXT DEFAULT '[]',
			status      TEXT DEFAULT 'unknown' CHECK (status IN ('active','completed','abandoned','on_hold','unknown')),
			notes       TEXT DEFAULT '',
			latest_version   TEXT,
			version_checked_at TEXT,
			store_links TEXT DEFAULT '{}',
			steam_app_id INTEGER,
			last_scanned_at TEXT,
			dir_mtime TEXT,
			series_id    INTEGER REFERENCES game_series(id),
			series_order INTEGER DEFAULT 0,
			deleted_at  TEXT,
			created_at  TEXT DEFAULT (datetime('now')),
			updated_at  TEXT DEFAULT (datetime('now'))
		);
		CREATE INDEX IF NOT EXISTS idx_games_engine ON games(engine);
		CREATE INDEX IF NOT EXISTS idx_games_title ON games(title COLLATE NOCASE);
		CREATE INDEX IF NOT EXISTS idx_games_path ON games(path);

		CREATE TABLE IF NOT EXISTS scraped_meta (
			game_id      INTEGER PRIMARY KEY REFERENCES games(id) ON DELETE CASCADE,
			developer    TEXT,
			overview     TEXT,
			cover_url    TEXT,
			last_scraped TEXT DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS downloads (
			id INTEGER PRIMARY KEY,
			game_id INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
			url TEXT NOT NULL,
			host TEXT,
			filename TEXT,
			dest_path TEXT,
			status TEXT DEFAULT 'pending',
			bytes_downloaded INTEGER DEFAULT 0,
			total_bytes INTEGER DEFAULT 0,
			speed_bytes_per_sec REAL DEFAULT 0,
			percent_complete REAL DEFAULT 0,
			error TEXT,
			started_at TEXT,
			completed_at TEXT,
			created_at TEXT DEFAULT (datetime('now'))
		);
		CREATE INDEX IF NOT EXISTS idx_downloads_game_id ON downloads(game_id);
		CREATE INDEX IF NOT EXISTS idx_downloads_status ON downloads(status);

		CREATE TABLE IF NOT EXISTS download_links (
			id INTEGER PRIMARY KEY,
			game_id INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
			url TEXT NOT NULL,
			host TEXT,
			name TEXT,
			platform TEXT DEFAULT 'unknown',
			is_dead INTEGER DEFAULT 0,
			dead_reason TEXT,
			last_checked TEXT,
			created_at TEXT DEFAULT (datetime('now'))
		);
		CREATE INDEX IF NOT EXISTS idx_download_links_game_id ON download_links(game_id);
		CREATE INDEX IF NOT EXISTS idx_download_links_platform ON download_links(platform);
		CREATE INDEX IF NOT EXISTS idx_download_links_is_dead ON download_links(is_dead);

		CREATE TABLE IF NOT EXISTS play_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			game_id INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
			played_at TEXT NOT NULL DEFAULT (datetime('now')),
			duration_s INTEGER DEFAULT 0,
			platform TEXT DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS idx_play_history_game ON play_history(game_id);
		CREATE INDEX IF NOT EXISTS idx_play_history_played ON play_history(played_at);

		CREATE TABLE IF NOT EXISTS game_series (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE
		);

		CREATE TABLE IF NOT EXISTS collections (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			created_at TEXT DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS game_collections (
			game_id INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
			collection_id INTEGER NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
			PRIMARY KEY (game_id, collection_id)
		);
	`

	// ── First-run databases (userVersion == 0) ─────────────────────
	// Create everything and jump to the latest version in a single
	// transaction. ALTER TABLE statements are not needed because the
	// CREATE TABLE above already has every column.
	if userVersion == 0 {
		var tx *sql.Tx
		tx, err := conn.Begin()
		if err != nil {
			return fmt.Errorf("begin initial migration: %w", err)
		}
		if _, err := tx.Exec(coreTables); err != nil {
			tx.Rollback()
			return fmt.Errorf("create core tables: %w", err)
		}
		if _, err := tx.Exec(fts5Setup); err != nil {
			tx.Rollback()
			return fmt.Errorf("create FTS5: %w", err)
		}
		// Populate FTS index from existing games + scraped_meta (there will be
		// 0 rows on a fresh DB, but this is still idempotent).
		if _, err := tx.Exec(`
			INSERT INTO games_fts(rowid, title, tags, developer, overview)
			SELECT g.id, g.title, g.tags, COALESCE(sm.developer, ''), COALESCE(sm.overview, '')
			FROM games g
			LEFT JOIN scraped_meta sm ON sm.game_id = g.id
		`); err != nil {
			tx.Rollback()
			return fmt.Errorf("populate FTS5: %w", err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", currentSchemaVersion)); err != nil {
			tx.Rollback()
			return fmt.Errorf("set user_version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit initial migration: %w", err)
		}
		return nil
	}

	// ── Existing databases ─────────────────────────────────────────
	if userVersion >= currentSchemaVersion {
		return nil
	}

	// Apply any old ALTER TABLE steps (for databases from before the
	// migration system). These are idempotent via columnExists checks.
	if userVersion < 1 {
		if err := upgradeOldDB(conn); err != nil {
			return err
		}
		// After upgradeOldDB, user_version is now at least 4 (the last
		// old ALTER TABLE version). Re-read so the version-gated loop
		// below picks the right starting point.
		if err := conn.QueryRow("PRAGMA user_version").Scan(&userVersion); err != nil {
			return fmt.Errorf("re-read schema version: %w", err)
		}
	}

	// New version-gated migrations: run all steps from userVersion+1
	// up to currentSchemaVersion.
	for v := userVersion + 1; v <= currentSchemaVersion; v++ {
		if err := migrateVersionStep(conn, v); err != nil {
			return fmt.Errorf("migration v%d: %w", v, err)
		}
	}

	// Auto-purge games soft-deleted more than 30 days ago.
	if _, err := conn.Exec(
		"DELETE FROM games WHERE deleted_at IS NOT NULL AND datetime(deleted_at) < datetime('now', '-30 days')",
	); err != nil {
		// Non-fatal — log and continue.
		log.Warn("auto-purge failed", "error", err)
	}

	return nil
}

// migrateVersionStep applies a single version-gated migration in its own
// transaction. Each step must be idempotent (safe to re-run).
func migrateVersionStep(conn *sql.DB, version int) error {
	tx, err := conn.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	switch version {
	case 2:
		if _, err := tx.Exec(fts5Setup); err != nil {
			return fmt.Errorf("create FTS5: %w", err)
		}
		// Populate FTS index from games + scraped_meta. Idempotent for a
		// standalone FTS5 table — same rowid replaces existing entries.
		if _, err := tx.Exec(`
			INSERT INTO games_fts(rowid, title, tags, developer, overview)
			SELECT g.id, g.title, g.tags, COALESCE(sm.developer, ''), COALESCE(sm.overview, '')
			FROM games g
			LEFT JOIN scraped_meta sm ON sm.game_id = g.id
		`); err != nil {
			return fmt.Errorf("populate FTS5: %w", err)
		}
	case 3:
		// Play history and game series support for existing databases.
		// Tables are created by coreTables for fresh DBs; existing DBs
		// need ALTER TABLE for the new columns on the games table.
		if !columnExists(tx, "games", "series_id") {
			if _, err := tx.Exec("ALTER TABLE games ADD COLUMN series_id INTEGER REFERENCES game_series(id)"); err != nil {
				return fmt.Errorf("add series_id: %w", err)
			}
		}
		if !columnExists(tx, "games", "series_order") {
			if _, err := tx.Exec("ALTER TABLE games ADD COLUMN series_order INTEGER DEFAULT 0"); err != nil {
				return fmt.Errorf("add series_order: %w", err)
			}
		}
	case 4:
		// Soft delete support: deleted_at TEXT (NULL = active).
		if !columnExists(tx, "games", "deleted_at") {
			if _, err := tx.Exec("ALTER TABLE games ADD COLUMN deleted_at TEXT"); err != nil {
				return fmt.Errorf("add deleted_at: %w", err)
			}
		}
	case 5:
		// Game collections.
		if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS collections (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT NOT NULL UNIQUE,
				created_at TEXT DEFAULT (datetime('now'))
			)
		`); err != nil {
			return fmt.Errorf("create collections: %w", err)
		}
		if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS game_collections (
				game_id INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
				collection_id INTEGER NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
				PRIMARY KEY (game_id, collection_id)
			)
		`); err != nil {
			return fmt.Errorf("create game_collections: %w", err)
		}
	case 6:
		// Repair step: ensure all schema columns exist on the games table
		// for databases that were created or migrated before these columns
		// were added to the schema definition (e.g., user_version was bumped
		// past the migration step that adds them).
		if !columnExists(tx, "games", "series_id") {
			if _, err := tx.Exec("ALTER TABLE games ADD COLUMN series_id INTEGER REFERENCES game_series(id)"); err != nil {
				return fmt.Errorf("add series_id: %w", err)
			}
		}
		if !columnExists(tx, "games", "series_order") {
			if _, err := tx.Exec("ALTER TABLE games ADD COLUMN series_order INTEGER DEFAULT 0"); err != nil {
				return fmt.Errorf("add series_order: %w", err)
			}
		}
		if !columnExists(tx, "games", "deleted_at") {
			if _, err := tx.Exec("ALTER TABLE games ADD COLUMN deleted_at TEXT"); err != nil {
				return fmt.Errorf("add deleted_at: %w", err)
			}
		}
	default:
		return fmt.Errorf("unknown migration version %d", version)
	}

	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
		return fmt.Errorf("set user_version: %w", err)
	}
	return tx.Commit()
}

// upgradeOldDB applies ALTER TABLE migrations for databases created by
// versions of moxie that predate version-gated migrations. Each step
// checks whether a column already exists before running ALTER TABLE.
func upgradeOldDB(conn *sql.DB) error {
	alterSteps := []struct {
		version int
		sql     string
	}{
		{2, "ALTER TABLE games ADD COLUMN latest_version TEXT"},
		{2, "ALTER TABLE games ADD COLUMN version_checked_at TEXT"},
		{3, "ALTER TABLE games ADD COLUMN store_links TEXT DEFAULT '{}'"},
		{3, "ALTER TABLE games ADD COLUMN steam_app_id INTEGER"},
		{4, "ALTER TABLE games ADD COLUMN last_scanned_at TEXT"},
		{4, "ALTER TABLE games ADD COLUMN dir_mtime TEXT"},
		{5, "ALTER TABLE games ADD COLUMN series_id INTEGER REFERENCES game_series(id)"},
		{5, "ALTER TABLE games ADD COLUMN series_order INTEGER DEFAULT 0"},
		{6, "ALTER TABLE games ADD COLUMN deleted_at TEXT"},
	}

	// Group by version so each version gets its own transaction.
	type versionStep struct {
		version int
		stmts   []string
	}
	byVersion := make(map[int]*versionStep)
	var versions []int
	for _, s := range alterSteps {
		if _, ok := byVersion[s.version]; !ok {
			byVersion[s.version] = &versionStep{version: s.version}
			versions = append(versions, s.version)
		}
		byVersion[s.version].stmts = append(byVersion[s.version].stmts, s.sql)
	}

	for _, v := range versions {
		step := byVersion[v]

		tx, err := conn.Begin()
		if err != nil {
			return fmt.Errorf("begin upgrade v%d: %w", v, err)
		}

		for _, stmt := range step.stmts {
			// Extract the column name from "ALTER TABLE x ADD COLUMN name type"
			parts := strings.Fields(stmt)
			if len(parts) >= 5 {
				colName := parts[4]
				if !columnExists(tx, "games", colName) {
					if _, err := tx.Exec(stmt); err != nil {
						tx.Rollback()
						return fmt.Errorf("upgrade v%d add %q: %w", v, colName, err)
					}
				}
			}
		}

		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", step.version)); err != nil {
			tx.Rollback()
			return fmt.Errorf("set user_version v%d: %w", v, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit upgrade v%d: %w", v, err)
		}
	}

	return nil
}

// columnExists checks whether a column exists in a table by scanning PRAGMA
// table_info output.
func columnExists(tx *sql.Tx, table, column string) bool {
	rows, err := tx.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dfltValue *string
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			return false
		}
		if name == column {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Null-value helpers
// ---------------------------------------------------------------------------

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableInt64(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

func nullableInt64Ptr(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

// ---------------------------------------------------------------------------
// Aggregates
// ---------------------------------------------------------------------------

// GameCount returns the total number of active (non-deleted) games in the library.
func (db *Database) GameCount() (int, error) {
	var n int
	err := db.conn.QueryRow("SELECT COUNT(*) FROM games WHERE deleted_at IS NULL").Scan(&n)
	return n, err
}

// TotalSize returns the sum of size_bytes across all active (non-deleted) games.
func (db *Database) TotalSize() (int64, error) {
	var total int64
	err := db.conn.QueryRow("SELECT COALESCE(SUM(size_bytes), 0) FROM games WHERE deleted_at IS NULL").Scan(&total)
	return total, err
}
