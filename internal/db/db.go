package db

import (
	"database/sql"
	"os"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
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

func migrate(conn *sql.DB) error {
	_, err := conn.Exec(`
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
	`)
	if err != nil {
		return err
	}

	// Migration: create downloads table
	conn.Exec(`
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
	`)

	// Migration: create download_links table for scraped links
	conn.Exec(`
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
	`)

	// Migration: add version tracking columns for existing databases.
	// SQLite has no IF NOT EXISTS for ALTER TABLE, so ignore errors.
	conn.Exec("ALTER TABLE games ADD COLUMN latest_version TEXT")
	conn.Exec("ALTER TABLE games ADD COLUMN version_checked_at TEXT")
	conn.Exec("ALTER TABLE games ADD COLUMN store_links TEXT DEFAULT '{}'")
	conn.Exec("ALTER TABLE games ADD COLUMN steam_app_id INTEGER")
	conn.Exec("ALTER TABLE games ADD COLUMN last_scanned_at TEXT")
	conn.Exec("ALTER TABLE games ADD COLUMN dir_mtime TEXT")

	return nil
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

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

// ---------------------------------------------------------------------------
// Aggregates
// ---------------------------------------------------------------------------

// GameCount returns the total number of games in the library.
func (db *Database) GameCount() (int, error) {
	var n int
	err := db.conn.QueryRow("SELECT COUNT(*) FROM games").Scan(&n)
	return n, err
}

// TotalSize returns the sum of size_bytes across all games.
func (db *Database) TotalSize() (int64, error) {
	var total int64
	err := db.conn.QueryRow("SELECT COALESCE(SUM(size_bytes), 0) FROM games").Scan(&total)
	return total, err
}
