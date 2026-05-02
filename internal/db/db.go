package db

import (
	"database/sql"
	"errors"
	"strings"
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

	// Enable WAL mode and foreign keys.
	if _, err := conn.Exec("PRAGMA journal_mode = WAL"); err != nil {
		conn.Close()
		return nil, err
	}
	if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
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

	// Migration: add version tracking columns for existing databases.
	// SQLite has no IF NOT EXISTS for ALTER TABLE, so ignore errors.
	conn.Exec("ALTER TABLE games ADD COLUMN latest_version TEXT")
	conn.Exec("ALTER TABLE games ADD COLUMN version_checked_at TEXT")

	return nil
}

// ---------------------------------------------------------------------------
// Helper: scan a single row into a Game
// ---------------------------------------------------------------------------

func scanGame(s scanner) (*Game, error) {
	var g Game
	var exePath, version, f95URL, createdAtStr, updatedAtStr, latestVer, verCheckedAt sql.NullString
	var f95ThreadID sql.NullInt64
	var tagsStr string

	err := s.Scan(
		&g.ID, &g.Title, &g.Engine, &g.Path,
		&exePath, &version, &g.SizeBytes,
		&f95URL, &f95ThreadID, &tagsStr,
		&g.Status, &latestVer, &verCheckedAt, &g.Notes,
		&createdAtStr, &updatedAtStr,
	)
	if err != nil {
		return nil, err
	}

	if exePath.Valid {
		g.ExePath = exePath.String
	}
	if version.Valid {
		g.Version = version.String
	}
	if f95URL.Valid {
		g.F95URL = f95URL.String
	}
	if f95ThreadID.Valid {
		g.F95ThreadID = f95ThreadID.Int64
	}

	g.Tags, _ = unmarshalTags(tagsStr)

	if createdAtStr.Valid {
		g.CreatedAt = parseTime(createdAtStr.String)
	}
	if updatedAtStr.Valid {
		g.UpdatedAt = parseTime(updatedAtStr.String)
	}
	if latestVer.Valid {
		g.LatestVersion = latestVer.String
	}
	if verCheckedAt.Valid {
		g.VersionCheckedAt = parseTime(verCheckedAt.String)
	}

	return &g, nil
}

// Helper: scan a single row into a ScrapedMeta.
func scanScrapedMeta(s scanner) (*ScrapedMeta, error) {
	var m ScrapedMeta
	var developer, overview, coverURL, lastScrapedStr sql.NullString

	err := s.Scan(&m.GameID, &developer, &overview, &coverURL, &lastScrapedStr)
	if err != nil {
		return nil, err
	}

	if developer.Valid {
		m.Developer = developer.String
	}
	if overview.Valid {
		m.Overview = overview.String
	}
	if coverURL.Valid {
		m.CoverURL = coverURL.String
	}
	if lastScrapedStr.Valid {
		m.LastScraped = parseTime(lastScrapedStr.String)
	}

	return &m, nil
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
// CRUD: Games
// ---------------------------------------------------------------------------

// applyDefaults sets sensible defaults for fields that have CHECK constraints.
func applyDefaults(g *Game) {
	if g.Status == "" {
		g.Status = "unknown"
	}
}

// InsertGame inserts a new game into the database. It sets CreatedAt and
// UpdatedAt to the current UTC time and returns the new row's ID.
func (db *Database) InsertGame(g *Game) (int64, error) {
	if g == nil {
		return 0, errors.New("db: cannot insert nil game")
	}

	applyDefaults(g)

	now := time.Now().UTC().Format(time.RFC3339)
	g.CreatedAt, _ = time.Parse(time.RFC3339, now)
	g.UpdatedAt = g.CreatedAt

	tagsStr, err := marshalTags(g.Tags)
	if err != nil {
		return 0, err
	}

	res, err := db.conn.Exec(`
		INSERT INTO games (title, engine, path, exe_path, version, size_bytes,
		                   f95_url, f95_thread_id, tags, status, latest_version, version_checked_at, notes,
		                   created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		g.Title, g.Engine, g.Path,
		nullableString(g.ExePath), nullableString(g.Version), g.SizeBytes,
		nullableString(g.F95URL), nullableInt64(g.F95ThreadID), tagsStr,
		g.Status, nil, nil, g.Notes,
		now, now,
	)
	if err != nil {
		return 0, err
	}

	g.ID, err = res.LastInsertId()
	return g.ID, err
}

// GetGame retrieves a game by its primary key. It returns nil, nil when no
// matching row exists.
func (db *Database) GetGame(id int64) (*Game, error) {
	row := db.conn.QueryRow(`
		SELECT id, title, engine, path, exe_path, version, size_bytes,
		       f95_url, f95_thread_id, tags, status, latest_version, version_checked_at, notes,
		       created_at, updated_at
		FROM games WHERE id = ?`, id)

	g, err := scanGame(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return g, nil
}

// GetGameByPath retrieves a game by its unique path. It returns nil, nil when
// no matching row exists.
func (db *Database) GetGameByPath(path string) (*Game, error) {
	row := db.conn.QueryRow(`
		SELECT id, title, engine, path, exe_path, version, size_bytes,
		       f95_url, f95_thread_id, tags, status, latest_version, version_checked_at, notes,
		       created_at, updated_at
		FROM games WHERE path = ?`, path)

	g, err := scanGame(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return g, nil
}

// ListGames returns all games optionally filtered by engine and/or status.
// An empty string for either parameter means "no filter".
func (db *Database) ListGames(engine, status string) ([]Game, error) {
	query := `
		SELECT id, title, engine, path, exe_path, version, size_bytes,
		       f95_url, f95_thread_id, tags, status, latest_version, version_checked_at, notes,
		       created_at, updated_at
		FROM games`

	var conditions []string
	var args []any

	if engine != "" {
		conditions = append(conditions, "engine = ?")
		args = append(args, engine)
	}
	if status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, status)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	query += " ORDER BY title COLLATE NOCASE"

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var games []Game
	for rows.Next() {
		g, err := scanGame(rows)
		if err != nil {
			return nil, err
		}
		games = append(games, *g)
	}
	return games, rows.Err()
}

// SearchGames performs a case-insensitive LIKE search on the title column.
func (db *Database) SearchGames(query string) ([]Game, error) {
	rows, err := db.conn.Query(`
		SELECT id, title, engine, path, exe_path, version, size_bytes,
		       f95_url, f95_thread_id, tags, status, latest_version, version_checked_at, notes,
		       created_at, updated_at
		FROM games
		WHERE title LIKE '%' || ? || '%' COLLATE NOCASE
		ORDER BY title COLLATE NOCASE`, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var games []Game
	for rows.Next() {
		g, err := scanGame(rows)
		if err != nil {
			return nil, err
		}
		games = append(games, *g)
	}
	return games, rows.Err()
}

// UpdateGame updates all columns of the given game. It sets UpdatedAt to the
// current UTC time before writing.
func (db *Database) UpdateGame(g *Game) error {
	if g == nil {
		return errors.New("db: cannot update nil game")
	}

	applyDefaults(g)

	now := time.Now().UTC().Format(time.RFC3339)
	g.UpdatedAt, _ = time.Parse(time.RFC3339, now)

	tagsStr, err := marshalTags(g.Tags)
	if err != nil {
		return err
	}

	_, err = db.conn.Exec(`
		UPDATE games
		SET title=?, engine=?, path=?, exe_path=?, version=?, size_bytes=?,
		    f95_url=?, f95_thread_id=?, tags=?, status=?, notes=?,
		    latest_version=?, version_checked_at=?, updated_at=?
		WHERE id=?`,
		g.Title, g.Engine, g.Path,
		nullableString(g.ExePath), nullableString(g.Version), g.SizeBytes,
		nullableString(g.F95URL), nullableInt64(g.F95ThreadID), tagsStr,
		g.Status, g.Notes,
		nullableString(g.LatestVersion), nullableTime(g.VersionCheckedAt),
		now, g.ID,
	)
	return err
}

// DeleteGame removes a game by its primary key.
func (db *Database) DeleteGame(id int64) error {
	_, err := db.conn.Exec("DELETE FROM games WHERE id = ?", id)
	return err
}

// ---------------------------------------------------------------------------
// CRUD: ScrapedMeta
// ---------------------------------------------------------------------------

// UpsertScrapedMeta inserts or replaces a scraped_meta row. It sets
// LastScraped to the current UTC time.
func (db *Database) UpsertScrapedMeta(m *ScrapedMeta) error {
	if m == nil {
		return errors.New("db: cannot upsert nil scraped meta")
	}

	now := time.Now().UTC().Format(time.RFC3339)
	m.LastScraped, _ = time.Parse(time.RFC3339, now)

	_, err := db.conn.Exec(`
		INSERT OR REPLACE INTO scraped_meta (game_id, developer, overview, cover_url, last_scraped)
		VALUES (?, ?, ?, ?, ?)`,
		m.GameID,
		nullableString(m.Developer),
		nullableString(m.Overview),
		nullableString(m.CoverURL),
		now,
	)
	return err
}

// GetScrapedMeta retrieves scraped metadata for a game. It returns nil, nil
// when no matching row exists.
func (db *Database) GetScrapedMeta(gameID int64) (*ScrapedMeta, error) {
	row := db.conn.QueryRow(`
		SELECT game_id, developer, overview, cover_url, last_scraped
		FROM scraped_meta WHERE game_id = ?`, gameID)

	m, err := scanScrapedMeta(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return m, nil
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
