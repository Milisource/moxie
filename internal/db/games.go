package db

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Helper: scan a single row into a Game
// ---------------------------------------------------------------------------

func scanGame(s scanner) (*Game, error) {
	var g Game
	var exePath, version, f95URL, createdAtStr, updatedAtStr, latestVer, verCheckedAt, storeLinksStr sql.NullString
	var f95ThreadID, steamAppID sql.NullInt64
	var lastScannedAtStr, dirMTimeStr sql.NullString
	var tagsStr string

	err := s.Scan(
		&g.ID, &g.Title, &g.Engine, &g.Path,
		&exePath, &version, &g.SizeBytes,
		&f95URL, &f95ThreadID, &tagsStr,
		&g.Status, &latestVer, &verCheckedAt, &g.Notes,
		&storeLinksStr, &steamAppID,
		&lastScannedAtStr, &dirMTimeStr,
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
	if lastScannedAtStr.Valid {
		g.LastScannedAt = parseTime(lastScannedAtStr.String)
	}
	if dirMTimeStr.Valid {
		g.DirMTime = parseTime(dirMTimeStr.String)
	}

	return &g, nil
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
		                   store_links, steam_app_id, last_scanned_at, dir_mtime, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		g.Title, g.Engine, g.Path,
		nullableString(g.ExePath), nullableString(g.Version), g.SizeBytes,
		nullableString(g.F95URL), nullableInt64(g.F95ThreadID), tagsStr,
		g.Status, nullableString(g.LatestVersion), nullableTime(g.VersionCheckedAt), g.Notes,
		storeLinksStr, nullableInt64(g.SteamAppID),
		nullableTime(g.LastScannedAt), nullableTime(g.DirMTime),
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
		       store_links, steam_app_id, last_scanned_at, dir_mtime, created_at, updated_at
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
		       store_links, steam_app_id, last_scanned_at, dir_mtime, created_at, updated_at
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
	return db.listGamesFiltered(engine, status, false)
}

// ListActiveGames returns all non-backup games (excluding .old directories)
// optionally filtered by engine and/or status. An empty string for either
// parameter means "no filter". Backup directories created by the updater's
// Merge() function are hidden from normal listing, sync, and TUI views.
func (db *Database) ListActiveGames(engine, status string) ([]Game, error) {
	return db.listGamesFiltered(engine, status, true)
}

// listGamesFiltered is the shared implementation for ListGames and
// ListActiveGames. When excludeBackups is true, paths ending in .old
// are excluded from results.
func (db *Database) listGamesFiltered(engine, status string, excludeBackups bool) ([]Game, error) {
	query := `
		SELECT id, title, engine, path, exe_path, version, size_bytes,
		       f95_url, f95_thread_id, tags, status, latest_version, version_checked_at, notes,
		       store_links, steam_app_id, last_scanned_at, dir_mtime, created_at, updated_at
		FROM games`

	var conditions []string
	var args []any

	if excludeBackups {
		conditions = append(conditions, "path NOT LIKE '%.old'")
	}
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
		       store_links, steam_app_id, last_scanned_at, dir_mtime, created_at, updated_at
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
		    latest_version=?, version_checked_at=?,
		    last_scanned_at=?, dir_mtime=?, updated_at=?
		WHERE id=?`,
		g.Title, g.Engine, g.Path,
		nullableString(g.ExePath), nullableString(g.Version), g.SizeBytes,
		nullableString(g.F95URL), nullableInt64(g.F95ThreadID), tagsStr,
		g.Status, g.Notes,
		storeLinksStr, nullableInt64(g.SteamAppID),
		nullableString(g.LatestVersion), nullableTime(g.VersionCheckedAt),
		nullableTime(g.LastScannedAt), nullableTime(g.DirMTime),
		now, g.ID,
	)
	return err
}

// AllGamePaths returns all game paths and their directory mtimes.
// Used by --new-only scan to skip already-known directories.
type GamePathEntry struct {
	Path    string
	DirMTime time.Time
}

func (db *Database) AllGamePaths() ([]GamePathEntry, error) {
	rows, err := db.conn.Query(`SELECT path, dir_mtime FROM games ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []GamePathEntry
	for rows.Next() {
		var e GamePathEntry
		var dirMTimeStr sql.NullString
		if err := rows.Scan(&e.Path, &dirMTimeStr); err != nil {
			return nil, err
		}
		if dirMTimeStr.Valid {
			e.DirMTime = parseTime(dirMTimeStr.String)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// DeleteGame removes a game by its primary key.
func (db *Database) DeleteGame(id int64) error {
	_, err := db.conn.Exec("DELETE FROM games WHERE id = ?", id)
	return err
}
