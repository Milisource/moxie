package db

import (
	"database/sql"
	"errors"
	"os"
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

	return nil
}

// ---------------------------------------------------------------------------
// Helper: scan a single row into a Game
// ---------------------------------------------------------------------------

func scanGame(s scanner) (*Game, error) {
	var g Game
	var exePath, version, f95URL, createdAtStr, updatedAtStr, latestVer, verCheckedAt, storeLinksStr sql.NullString
	var f95ThreadID, steamAppID sql.NullInt64
	var tagsStr string

	err := s.Scan(
		&g.ID, &g.Title, &g.Engine, &g.Path,
		&exePath, &version, &g.SizeBytes,
		&f95URL, &f95ThreadID, &tagsStr,
		&g.Status, &latestVer, &verCheckedAt, &g.Notes,
		&storeLinksStr, &steamAppID,
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
	if storeLinksStr.Valid {
		g.StoreLinks, _ = unmarshalStoreLinks(storeLinksStr.String)
	}
	if steamAppID.Valid {
		g.SteamAppID = steamAppID.Int64
	}

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

	storeLinksStr, err := marshalStoreLinks(g.StoreLinks)
	if err != nil {
		return 0, err
	}

	res, err := db.conn.Exec(`
		INSERT INTO games (title, engine, path, exe_path, version, size_bytes,
		                   f95_url, f95_thread_id, tags, status, latest_version, version_checked_at, notes,
		                   store_links, steam_app_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		g.Title, g.Engine, g.Path,
		nullableString(g.ExePath), nullableString(g.Version), g.SizeBytes,
		nullableString(g.F95URL), nullableInt64(g.F95ThreadID), tagsStr,
		g.Status, nullableString(g.LatestVersion), nullableTime(g.VersionCheckedAt), g.Notes,
		storeLinksStr, nullableInt64(g.SteamAppID),
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
		       store_links, steam_app_id, created_at, updated_at
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
		       store_links, steam_app_id, created_at, updated_at
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
		       store_links, steam_app_id, created_at, updated_at
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
		       store_links, steam_app_id, created_at, updated_at
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

	storeLinksStr, err := marshalStoreLinks(g.StoreLinks)
	if err != nil {
		return err
	}

	_, err = db.conn.Exec(`
		UPDATE games
		SET title=?, engine=?, path=?, exe_path=?, version=?, size_bytes=?,
		    f95_url=?, f95_thread_id=?, tags=?, status=?, notes=?,
		    store_links=?, steam_app_id=?,
		    latest_version=?, version_checked_at=?, updated_at=?
		WHERE id=?`,
		g.Title, g.Engine, g.Path,
		nullableString(g.ExePath), nullableString(g.Version), g.SizeBytes,
		nullableString(g.F95URL), nullableInt64(g.F95ThreadID), tagsStr,
		g.Status, g.Notes,
		storeLinksStr, nullableInt64(g.SteamAppID),
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

// ---------------------------------------------------------------------------
// CRUD: Downloads
// ---------------------------------------------------------------------------

// scanDownload scans a download row from the database.
func scanDownload(s scanner) (*Download, error) {
	var d Download
	var host, filename, destPath, status, errStr, startedAtStr, completedAtStr, createdAtStr sql.NullString

	err := s.Scan(
		&d.ID, &d.GameID, &d.URL, &host, &filename, &destPath,
		&status, &d.BytesDownloaded, &d.TotalBytes, &d.SpeedBytesPerSec, &d.PercentComplete,
		&errStr, &startedAtStr, &completedAtStr, &createdAtStr,
	)
	if err != nil {
		return nil, err
	}

	if host.Valid {
		d.Host = host.String
	}
	if filename.Valid {
		d.Filename = filename.String
	}
	if destPath.Valid {
		d.DestPath = destPath.String
	}
	if status.Valid {
		d.Status = DownloadStatus(status.String)
	}
	if errStr.Valid {
		d.Error = errStr.String
	}
	if startedAtStr.Valid {
		d.StartedAt = parseTime(startedAtStr.String)
	}
	if completedAtStr.Valid {
		d.CompletedAt = parseTime(completedAtStr.String)
	}
	if createdAtStr.Valid {
		d.CreatedAt = parseTime(createdAtStr.String)
	}

	return &d, nil
}

// CreateDownload inserts a new download record.
func (db *Database) CreateDownload(d *Download) (int64, error) {
	if d == nil {
		return 0, errors.New("db: cannot insert nil download")
	}

	now := time.Now().UTC().Format(time.RFC3339)
	d.CreatedAt, _ = time.Parse(time.RFC3339, now)

	res, err := db.conn.Exec(`
		INSERT INTO downloads (game_id, url, host, filename, dest_path, status,
			bytes_downloaded, total_bytes, speed_bytes_per_sec, percent_complete,
			error, started_at, completed_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.GameID, d.URL, nullableString(d.Host), nullableString(d.Filename), nullableString(d.DestPath),
		string(d.Status), d.BytesDownloaded, d.TotalBytes, d.SpeedBytesPerSec, d.PercentComplete,
		nullableString(d.Error), nullableTime(d.StartedAt), nullableTime(d.CompletedAt), now,
	)
	if err != nil {
		return 0, err
	}

	d.ID, err = res.LastInsertId()
	return d.ID, err
}

// GetDownload retrieves a download by its ID.
func (db *Database) GetDownload(id int64) (*Download, error) {
	row := db.conn.QueryRow(`
		SELECT id, game_id, url, host, filename, dest_path, status,
			bytes_downloaded, total_bytes, speed_bytes_per_sec, percent_complete,
			error, started_at, completed_at, created_at
		FROM downloads WHERE id = ?`, id)

	d, err := scanDownload(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return d, nil
}

// GetDownloadByGameID retrieves the most recent download for a game.
func (db *Database) GetDownloadByGameID(gameID int64) (*Download, error) {
	row := db.conn.QueryRow(`
		SELECT id, game_id, url, host, filename, dest_path, status,
			bytes_downloaded, total_bytes, speed_bytes_per_sec, percent_complete,
			error, started_at, completed_at, created_at
		FROM downloads WHERE game_id = ? ORDER BY created_at DESC LIMIT 1`, gameID)

	d, err := scanDownload(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return d, nil
}

// ListDownloads returns all downloads optionally filtered by status.
func (db *Database) ListDownloads(status string) ([]Download, error) {
	query := `
		SELECT id, game_id, url, host, filename, dest_path, status,
			bytes_downloaded, total_bytes, speed_bytes_per_sec, percent_complete,
			error, started_at, completed_at, created_at
		FROM downloads`

	var args []any
	if status != "" {
		query += " WHERE status = ?"
		args = append(args, status)
	}

	query += " ORDER BY created_at DESC"

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var downloads []Download
	for rows.Next() {
		d, err := scanDownload(rows)
		if err != nil {
			return nil, err
		}
		downloads = append(downloads, *d)
	}
	return downloads, rows.Err()
}

// UpdateDownload updates a download record.
func (db *Database) UpdateDownload(d *Download) error {
	if d == nil {
		return errors.New("db: cannot update nil download")
	}

	_, err := db.conn.Exec(`
		UPDATE downloads SET
			game_id = ?, url = ?, host = ?, filename = ?, dest_path = ?, status = ?,
			bytes_downloaded = ?, total_bytes = ?, speed_bytes_per_sec = ?, percent_complete = ?,
			error = ?, started_at = ?, completed_at = ?
		WHERE id = ?`,
		d.GameID, d.URL, nullableString(d.Host), nullableString(d.Filename), nullableString(d.DestPath),
		string(d.Status), d.BytesDownloaded, d.TotalBytes, d.SpeedBytesPerSec, d.PercentComplete,
		nullableString(d.Error), nullableTime(d.StartedAt), nullableTime(d.CompletedAt),
		d.ID,
	)
	return err
}

// DeleteDownload removes a download by its ID.
func (db *Database) DeleteDownload(id int64) error {
	_, err := db.conn.Exec("DELETE FROM downloads WHERE id = ?", id)
	return err
}

// DeleteDownloadsByGameID removes all downloads for a game.
func (db *Database) DeleteDownloadsByGameID(gameID int64) error {
	_, err := db.conn.Exec("DELETE FROM downloads WHERE game_id = ?", gameID)
	return err
}

// ---------------------------------------------------------------------------
// CRUD: DownloadLinks
// ---------------------------------------------------------------------------

// scanDownloadLink scans a download_link row from the database.
func scanDownloadLink(s scanner) (*DownloadLink, error) {
	var d DownloadLink
	var host, name, platform, deadReason, lastCheckedStr, createdAtStr sql.NullString

	err := s.Scan(
		&d.ID, &d.GameID, &d.URL, &host, &name, &platform,
		&d.IsDead, &deadReason, &lastCheckedStr, &createdAtStr,
	)
	if err != nil {
		return nil, err
	}

	if host.Valid {
		d.Host = host.String
	}
	if name.Valid {
		d.Name = name.String
	}
	if platform.Valid {
		d.Platform = Platform(platform.String)
	}
	if deadReason.Valid {
		d.DeadReason = deadReason.String
	}
	if lastCheckedStr.Valid {
		d.LastChecked = parseTime(lastCheckedStr.String)
	}
	if createdAtStr.Valid {
		d.CreatedAt = parseTime(createdAtStr.String)
	}

	return &d, nil
}

// CreateDownloadLink inserts a new download link record.
func (db *Database) CreateDownloadLink(d *DownloadLink) (int64, error) {
	if d == nil {
		return 0, errors.New("db: cannot insert nil download link")
	}

	now := time.Now().UTC().Format(time.RFC3339)
	d.CreatedAt, _ = time.Parse(time.RFC3339, now)

	res, err := db.conn.Exec(`
		INSERT INTO download_links (game_id, url, host, name, platform, is_dead, dead_reason, last_checked, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.GameID, d.URL, nullableString(d.Host), nullableString(d.Name), string(d.Platform),
		d.IsDead, nullableString(d.DeadReason), nullableTime(d.LastChecked), now,
	)
	if err != nil {
		return 0, err
	}

	d.ID, err = res.LastInsertId()
	return d.ID, err
}

// GetDownloadLink retrieves a download link by its ID.
func (db *Database) GetDownloadLink(id int64) (*DownloadLink, error) {
	row := db.conn.QueryRow(`
		SELECT id, game_id, url, host, name, platform, is_dead, dead_reason, last_checked, created_at
		FROM download_links WHERE id = ?`, id)

	d, err := scanDownloadLink(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return d, nil
}

// GetDownloadLinkByURL retrieves a download link by game ID and URL.
func (db *Database) GetDownloadLinkByURL(gameID int64, url string) (*DownloadLink, error) {
	row := db.conn.QueryRow(`
		SELECT id, game_id, url, host, name, platform, is_dead, dead_reason, last_checked, created_at
		FROM download_links WHERE game_id = ? AND url = ?`, gameID, url)

	d, err := scanDownloadLink(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return d, nil
}

// ListDownloadLinks returns all download links for a game, optionally filtering by platform.
func (db *Database) ListDownloadLinks(gameID int64, platform string, includeDead bool) ([]DownloadLink, error) {
	query := `
		SELECT id, game_id, url, host, name, platform, is_dead, dead_reason, last_checked, created_at
		FROM download_links WHERE game_id = ?`
	args := []any{gameID}

	if !includeDead {
		query += " AND is_dead = 0"
	}
	if platform != "" {
		query += " AND platform = ?"
		args = append(args, platform)
	}

	query += " ORDER BY created_at DESC"

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var links []DownloadLink
	for rows.Next() {
		d, err := scanDownloadLink(rows)
		if err != nil {
			return nil, err
		}
		links = append(links, *d)
	}
	return links, rows.Err()
}

// UpdateDownloadLink updates a download link record.
func (db *Database) UpdateDownloadLink(d *DownloadLink) error {
	if d == nil {
		return errors.New("db: cannot update nil download link")
	}

	_, err := db.conn.Exec(`
		UPDATE download_links SET
			game_id = ?, url = ?, host = ?, name = ?, platform = ?,
			is_dead = ?, dead_reason = ?, last_checked = ?
		WHERE id = ?`,
		d.GameID, d.URL, nullableString(d.Host), nullableString(d.Name), string(d.Platform),
		d.IsDead, nullableString(d.DeadReason), nullableTime(d.LastChecked),
		d.ID,
	)
	return err
}

// MarkDownloadLinkDead marks a link as dead with a reason.
func (db *Database) MarkDownloadLinkDead(id int64, reason string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(`
		UPDATE download_links SET is_dead = 1, dead_reason = ?, last_checked = ?
		WHERE id = ?`,
		reason, now, id)
	return err
}

// DeleteDownloadLink removes a download link by its ID.
func (db *Database) DeleteDownloadLink(id int64) error {
	_, err := db.conn.Exec("DELETE FROM download_links WHERE id = ?", id)
	return err
}

// DeleteDownloadLinksByGameID removes all download links for a game.
func (db *Database) DeleteDownloadLinksByGameID(gameID int64) error {
	_, err := db.conn.Exec("DELETE FROM download_links WHERE game_id = ?", gameID)
	return err
}
