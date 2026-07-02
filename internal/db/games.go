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
	var seriesID, seriesOrder sql.NullInt64
	var deletedAtStr sql.NullString
	var tagsStr string

	err := s.Scan(
		&g.ID, &g.Title, &g.Engine, &g.Path,
		&exePath, &version, &g.SizeBytes,
		&f95URL, &f95ThreadID, &tagsStr,
		&g.Status, &latestVer, &verCheckedAt, &g.Notes,
		&storeLinksStr, &steamAppID,
		&lastScannedAtStr, &dirMTimeStr,
		&seriesID, &seriesOrder,
		&deletedAtStr,
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
	if seriesID.Valid {
		g.SeriesID = &seriesID.Int64
	}
	if seriesOrder.Valid {
		g.SeriesOrder = int(seriesOrder.Int64)
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
	if deletedAtStr.Valid {
		g.DeletedAt = parseTime(deletedAtStr.String)
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
		                   store_links, steam_app_id, last_scanned_at, dir_mtime,
		                   series_id, series_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
		        ?, ?, ?, ?)`,
		g.Title, g.Engine, g.Path,
		nullableString(g.ExePath), nullableString(g.Version), g.SizeBytes,
		nullableString(g.F95URL), nullableInt64(g.F95ThreadID), tagsStr,
		g.Status, nullableString(g.LatestVersion), nullableTime(g.VersionCheckedAt), g.Notes,
		storeLinksStr, nullableInt64(g.SteamAppID),
		nullableTime(g.LastScannedAt), nullableTime(g.DirMTime),
		nullableInt64Ptr(g.SeriesID), g.SeriesOrder,
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
		       store_links, steam_app_id, last_scanned_at, dir_mtime, series_id, series_order,
		       deleted_at, created_at, updated_at
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
		       store_links, steam_app_id, last_scanned_at, dir_mtime, series_id, series_order,
		       deleted_at, created_at, updated_at
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
		       store_links, steam_app_id, last_scanned_at, dir_mtime, series_id, series_order,
		       deleted_at, created_at, updated_at
		FROM games`

	var conditions []string
	var args []any

	if excludeBackups {
		conditions = append(conditions, "path NOT LIKE '%.old'")
	}
	// Exclude soft-deleted games.
	conditions = append(conditions, "deleted_at IS NULL")
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

// SearchGames performs a full-text search using FTS5 across title, tags,
// developer, and overview. Results are ranked by relevance.
//
// For simple multi-word queries (spaces, no FTS operators), each word is
// automatically wrapped with prefix matching so "witch hunter" matches
// "Witch Hunter" or "Witchcraft". For callers familiar with FTS5 syntax,
// bare operators and phrases are passed through as-is ("dragon & elf" or
// ""visual novel"").
//
// An empty query returns all games ordered by title (backward compatible).
func (db *Database) SearchGames(query string) ([]Game, error) {
	if query == "" {
		// Empty query: return all games ordered by title.
		rows, err := db.conn.Query(`
			SELECT id, title, engine, path, exe_path, version, size_bytes,
			       f95_url, f95_thread_id, tags, status, latest_version, version_checked_at, notes,
			       store_links, steam_app_id, last_scanned_at, dir_mtime, series_id, series_order,
			       deleted_at, created_at, updated_at
			FROM games
			WHERE deleted_at IS NULL
			ORDER BY title COLLATE NOCASE`)
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

	ftsQuery := buildFTSQuery(query)

	rows, err := db.conn.Query(`
		SELECT g.id, g.title, g.engine, g.path, g.exe_path, g.version, g.size_bytes,
		       g.f95_url, g.f95_thread_id, g.tags, g.status, g.latest_version, g.version_checked_at, g.notes,
		       g.store_links, g.steam_app_id, g.last_scanned_at, g.dir_mtime,
	           g.series_id, g.series_order, g.deleted_at, g.created_at, g.updated_at
		FROM games g
		JOIN games_fts fts ON g.id = fts.rowid
		WHERE games_fts MATCH ?
		  AND g.deleted_at IS NULL
		ORDER BY rank`, ftsQuery)
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
	if len(games) > 0 {
		return games, rows.Err()
	}

	// FTS5 returned no results — fall back to LIKE-based substring matching
	// for backward compatibility (e.g., "itch" matching "Witcher").
	rows, err = db.conn.Query(`
		SELECT id, title, engine, path, exe_path, version, size_bytes,
		       f95_url, f95_thread_id, tags, status, latest_version, version_checked_at, notes,
		       store_links, steam_app_id, last_scanned_at, dir_mtime, series_id, series_order,
		       deleted_at, created_at, updated_at
		FROM games
		WHERE deleted_at IS NULL
		  AND title LIKE '%' || ? || '%' COLLATE NOCASE
		ORDER BY title COLLATE NOCASE`, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	games = nil
	for rows.Next() {
		g, err := scanGame(rows)
		if err != nil {
			return nil, err
		}
		games = append(games, *g)
	}
	return games, rows.Err()
}

// hasFTSOperators reports whether the query string contains FTS5 syntax that
// should be passed through without transformation.
func hasFTSOperators(q string) bool {
	// FTS5 special characters: * " ( ) { } ^ ~  and operators NEAR, AND, OR, NOT
	for _, r := range q {
		if r == '*' || r == '"' || r == '(' || r == ')' || r == '{' || r == '}' || r == '^' || r == '~' {
			return true
		}
	}
	upper := strings.ToUpper(q)
	for _, op := range []string{" NEAR ", " AND ", " OR ", " NOT "} {
		if strings.Contains(upper, op) {
			return true
		}
	}
	return false
}

// buildFTSQuery converts a user search query into an FTS5-compatible MATCH
// expression. Simple queries with spaces get each word auto-prefixed for
// matching (e.g., "witch hunter" → "witch* hunter*"). Queries with FTS5
// operators are passed through as-is.
func buildFTSQuery(query string) string {
	if hasFTSOperators(query) {
		return query
	}

	// Split by whitespace and prefix each non-empty word.
	fields := strings.Fields(query)
	if len(fields) == 0 {
		return query
	}

	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		// Quote each term with prefix matching
		parts = append(parts, `"`+f+`"*`)
	}
	return strings.Join(parts, " ")
}

// ---------------------------------------------------------------------------
// Targeted column updates
// ---------------------------------------------------------------------------

// UpdateGameTitle updates only the title column and updated_at.
func (db *Database) UpdateGameTitle(id int64, title string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec("UPDATE games SET title = ?, updated_at = ? WHERE id = ?", title, now, id)
	return err
}

// UpdateGameStatus updates only the status column and updated_at.
func (db *Database) UpdateGameStatus(id int64, status string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec("UPDATE games SET status = ?, updated_at = ? WHERE id = ?", status, now, id)
	return err
}

// UpdateGameF95URL updates only the f95_url column and updated_at.
func (db *Database) UpdateGameF95URL(id int64, url string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec("UPDATE games SET f95_url = ?, updated_at = ? WHERE id = ?", nullableString(url), now, id)
	return err
}

// UpdateGameExePath updates only the exe_path column and updated_at.
func (db *Database) UpdateGameExePath(id int64, path string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec("UPDATE games SET exe_path = ?, updated_at = ? WHERE id = ?", nullableString(path), now, id)
	return err
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
		    last_scanned_at=?, dir_mtime=?,
		    series_id=?, series_order=?, updated_at=?
		WHERE id=?`,
		g.Title, g.Engine, g.Path,
		nullableString(g.ExePath), nullableString(g.Version), g.SizeBytes,
		nullableString(g.F95URL), nullableInt64(g.F95ThreadID), tagsStr,
		g.Status, g.Notes,
		storeLinksStr, nullableInt64(g.SteamAppID),
		nullableString(g.LatestVersion), nullableTime(g.VersionCheckedAt),
		nullableTime(g.LastScannedAt), nullableTime(g.DirMTime),
		nullableInt64Ptr(g.SeriesID), g.SeriesOrder,
		now, g.ID,
	)
	return err
}

// ---------------------------------------------------------------------------
// Lightweight queries
// ---------------------------------------------------------------------------

// ListGameSummaries returns a lightweight summary of all active games
// (excluding .old paths), optionally filtered by engine and/or status.
// An empty string for either filter means "no filter". This only selects 7
// columns and is intended for TUI table rendering where full Game objects
// are unnecessary.
func (db *Database) ListGameSummaries(engine, status string) ([]GameSummary, error) {
	query := `
		SELECT id, title, engine, version, latest_version, status, path
		FROM games
		WHERE path NOT LIKE '%.old'
		  AND deleted_at IS NULL`

	var args []any
	if engine != "" {
		query += " AND engine = ?"
		args = append(args, engine)
	}
	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}

	query += " ORDER BY title COLLATE NOCASE"

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []GameSummary
	for rows.Next() {
		var s GameSummary
		var version, latestVersion sql.NullString
		if err := rows.Scan(&s.ID, &s.Title, &s.Engine, &version, &latestVersion, &s.Status, &s.Path); err != nil {
			return nil, err
		}
		if version.Valid {
			s.Version = version.String
		}
		if latestVersion.Valid {
			s.LatestVersion = latestVersion.String
		}
		summaries = append(summaries, s)
	}
	return summaries, rows.Err()
}

// ---------------------------------------------------------------------------
// Dedicated query methods
// ---------------------------------------------------------------------------

// GamesNeedingUpdate returns games where latest_version != version (i.e., an
// update is available). Excludes backup directories (.old paths).
func (db *Database) GamesNeedingUpdate() ([]Game, error) {
	rows, err := db.conn.Query(`
		SELECT id, title, engine, path, exe_path, version, size_bytes,
		       f95_url, f95_thread_id, tags, status, latest_version, version_checked_at, notes,
		       store_links, steam_app_id, last_scanned_at, dir_mtime, series_id, series_order,
		       deleted_at, created_at, updated_at
		FROM games
		WHERE path NOT LIKE '%.old'
		  AND deleted_at IS NULL
		  AND latest_version IS NOT NULL
		  AND latest_version != version
		ORDER BY title COLLATE NOCASE`)
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

// GamesWithF95URL returns games that have an F95Zone URL (i.e., are associated).
// Excludes backup directories (.old paths).
func (db *Database) GamesWithF95URL() ([]Game, error) {
	rows, err := db.conn.Query(`
		SELECT id, title, engine, path, exe_path, version, size_bytes,
		       f95_url, f95_thread_id, tags, status, latest_version, version_checked_at, notes,
		       store_links, steam_app_id, last_scanned_at, dir_mtime, series_id, series_order,
		       deleted_at, created_at, updated_at
		FROM games
		WHERE path NOT LIKE '%.old'
		  AND deleted_at IS NULL
		  AND f95_url IS NOT NULL
		ORDER BY title COLLATE NOCASE`)
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

// GamesWithoutF95URL returns games that do NOT have an F95Zone URL (i.e.,
// are unassociated). Excludes backup directories (.old paths).
func (db *Database) GamesWithoutF95URL() ([]Game, error) {
	rows, err := db.conn.Query(`
		SELECT id, title, engine, path, exe_path, version, size_bytes,
		       f95_url, f95_thread_id, tags, status, latest_version, version_checked_at, notes,
		       store_links, steam_app_id, last_scanned_at, dir_mtime, series_id, series_order,
		       deleted_at, created_at, updated_at
		FROM games
		WHERE path NOT LIKE '%.old'
		  AND deleted_at IS NULL
		  AND f95_url IS NULL
		ORDER BY title COLLATE NOCASE`)
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

// GamesByStatus returns games matching the given status. Excludes backup
// directories (.old paths).
func (db *Database) GamesByStatus(status string) ([]Game, error) {
	rows, err := db.conn.Query(`
		SELECT id, title, engine, path, exe_path, version, size_bytes,
		       f95_url, f95_thread_id, tags, status, latest_version, version_checked_at, notes,
		       store_links, steam_app_id, last_scanned_at, dir_mtime, series_id, series_order,
		       deleted_at, created_at, updated_at
		FROM games
		WHERE path NOT LIKE '%.old'
		  AND deleted_at IS NULL
		  AND status = ?
		ORDER BY title COLLATE NOCASE`, status)
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

// GamesByEngine returns games matching the given engine type. Excludes backup
// directories (.old paths).
func (db *Database) GamesByEngine(engine string) ([]Game, error) {
	rows, err := db.conn.Query(`
		SELECT id, title, engine, path, exe_path, version, size_bytes,
		       f95_url, f95_thread_id, tags, status, latest_version, version_checked_at, notes,
		       store_links, steam_app_id, last_scanned_at, dir_mtime, series_id, series_order,
		       deleted_at, created_at, updated_at
		FROM games
		WHERE path NOT LIKE '%.old'
		  AND deleted_at IS NULL
		  AND engine = ?
		ORDER BY title COLLATE NOCASE`, engine)
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

// BatchUpdateStatus updates the status of all games matching the given engine.
// Pass engine="" to update all games. Returns the number of rows affected.
func (db *Database) BatchUpdateStatus(engine, status string) (int, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	if engine == "" {
		res, err := db.conn.Exec("UPDATE games SET status = ?, updated_at = ?", status, now)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		return int(n), nil
	}
	res, err := db.conn.Exec("UPDATE games SET status = ?, updated_at = ? WHERE engine = ?",
		status, now, engine)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// DeleteGame soft-deletes a game by setting deleted_at. Use DeleteGamePermanent
// for actual removal or RestoreGame to undo.
func (db *Database) DeleteGame(id int64) error {
	_, err := db.conn.Exec("UPDATE games SET deleted_at = datetime('now'), updated_at = datetime('now') WHERE id = ?", id)
	return err
}

// DeleteGamePermanent permanently removes a game from the database.
func (db *Database) DeleteGamePermanent(id int64) error {
	_, err := db.conn.Exec("DELETE FROM games WHERE id = ?", id)
	return err
}

// RestoreGame clears the deleted_at timestamp on a soft-deleted game.
func (db *Database) RestoreGame(id int64) error {
	_, err := db.conn.Exec("UPDATE games SET deleted_at = NULL, updated_at = datetime('now') WHERE id = ?", id)
	return err
}

// ListDeletedGames returns all soft-deleted games.
func (db *Database) ListDeletedGames() ([]Game, error) {
	rows, err := db.conn.Query(`
		SELECT id, title, engine, path, exe_path, version, size_bytes,
		       f95_url, f95_thread_id, tags, status, latest_version, version_checked_at, notes,
		       store_links, steam_app_id, last_scanned_at, dir_mtime, series_id, series_order,
		       deleted_at, created_at, updated_at
		FROM games
		WHERE deleted_at IS NOT NULL
		ORDER BY deleted_at DESC`)
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

// ListDeletedSummaries returns a lightweight summary of all soft-deleted games.
func (db *Database) ListDeletedSummaries() ([]GameSummary, error) {
	rows, err := db.conn.Query(`
		SELECT id, title, engine, version, latest_version, status, path
		FROM games
		WHERE deleted_at IS NOT NULL
		ORDER BY deleted_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []GameSummary
	for rows.Next() {
		var s GameSummary
		var version, latestVersion sql.NullString
		if err := rows.Scan(&s.ID, &s.Title, &s.Engine, &version, &latestVersion, &s.Status, &s.Path); err != nil {
			return nil, err
		}
		if version.Valid {
			s.Version = version.String
		}
		if latestVersion.Valid {
			s.LatestVersion = latestVersion.String
		}
		summaries = append(summaries, s)
	}
	return summaries, rows.Err()
}

// PurgeDeleted permanently removes all soft-deleted games from the database.
func (db *Database) PurgeDeleted() (int64, error) {
	res, err := db.conn.Exec("DELETE FROM games WHERE deleted_at IS NOT NULL")
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ---------------------------------------------------------------------------
// Play History
// ---------------------------------------------------------------------------

// RecordPlay inserts a play history entry for a game.
func (db *Database) RecordPlay(gameID int64, platform string) error {
	_, err := db.conn.Exec(
		"INSERT INTO play_history (game_id, platform) VALUES (?, ?)",
		gameID, platform)
	return err
}

// RecentPlays returns the most recent play history entries, joined with game title.
func (db *Database) RecentPlays(limit int) ([]PlayHistoryWithGame, error) {
	rows, err := db.conn.Query(`
		SELECT ph.id, ph.game_id, ph.played_at, ph.duration_s, ph.platform, g.title
		FROM play_history ph
		JOIN games g ON g.id = ph.game_id
		ORDER BY ph.played_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []PlayHistoryWithGame
	for rows.Next() {
		var e PlayHistoryWithGame
		var playedAtStr string
		if err := rows.Scan(&e.ID, &e.GameID, &playedAtStr, &e.DurationS, &e.Platform, &e.GameTitle); err != nil {
			return nil, err
		}
		e.PlayedAt, _ = time.Parse(time.RFC3339, playedAtStr)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// CreateSeries creates a new game series and returns it.
func (db *Database) CreateSeries(name string) (*GameSeries, error) {
	res, err := db.conn.Exec("INSERT INTO game_series (name) VALUES (?)", name)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &GameSeries{ID: id, Name: name}, nil
}

// SetGameSeries sets or removes the series association for a game.
func (db *Database) SetGameSeries(gameID int64, seriesID *int64, order int) error {
	_, err := db.conn.Exec(
		"UPDATE games SET series_id = ?, series_order = ?, updated_at = ? WHERE id = ?",
		nullableInt64Ptr(seriesID), order, time.Now().UTC().Format(time.RFC3339), gameID)
		return err
}
