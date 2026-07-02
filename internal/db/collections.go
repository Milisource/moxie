package db

import (
	"database/sql"
	"errors"
	"time"
)

// ---------------------------------------------------------------------------
// CRUD: Collections
// ---------------------------------------------------------------------------

// CreateCollection creates a new named collection and returns it.
func (db *Database) CreateCollection(name string) (*Collection, error) {
	res, err := db.conn.Exec("INSERT INTO collections (name) VALUES (?)", name)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &Collection{ID: id, Name: name, CreatedAt: time.Now().UTC()}, nil
}

// ListCollections returns all collections ordered by name.
func (db *Database) ListCollections() ([]Collection, error) {
	rows, err := db.conn.Query(`
		SELECT id, name, created_at
		FROM collections
		ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var collections []Collection
	for rows.Next() {
		var c Collection
		var createdAtStr sql.NullString
		if err := rows.Scan(&c.ID, &c.Name, &createdAtStr); err != nil {
			return nil, err
		}
		if createdAtStr.Valid {
			c.CreatedAt = parseTime(createdAtStr.String)
		}
		collections = append(collections, c)
	}
	return collections, rows.Err()
}

// GetCollection retrieves a collection by ID. Returns nil, nil when no
// matching row exists.
func (db *Database) GetCollection(id int64) (*Collection, error) {
	var c Collection
	var createdAtStr sql.NullString
	err := db.conn.QueryRow(`
		SELECT id, name, created_at
		FROM collections WHERE id = ?`, id).Scan(&c.ID, &c.Name, &createdAtStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if createdAtStr.Valid {
		c.CreatedAt = parseTime(createdAtStr.String)
	}
	return &c, nil
}

// DeleteCollection deletes a collection by ID. Also removes all game
// associations via ON DELETE CASCADE.
func (db *Database) DeleteCollection(id int64) error {
	res, err := db.conn.Exec("DELETE FROM collections WHERE id = ?", id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("collection not found")
	}
	return nil
}

// AddGameToCollection adds a game to a collection. Returns an error if the
// association already exists or either ID is invalid.
func (db *Database) AddGameToCollection(gameID, collectionID int64) error {
	_, err := db.conn.Exec(
		"INSERT OR IGNORE INTO game_collections (game_id, collection_id) VALUES (?, ?)",
		gameID, collectionID)
	return err
}

// RemoveGameFromCollection removes a game from a collection.
func (db *Database) RemoveGameFromCollection(gameID, collectionID int64) error {
	_, err := db.conn.Exec(
		"DELETE FROM game_collections WHERE game_id = ? AND collection_id = ?",
		gameID, collectionID)
	return err
}

// GetCollectionsForGame returns all collections that contain the given game.
func (db *Database) GetCollectionsForGame(gameID int64) ([]Collection, error) {
	rows, err := db.conn.Query(`
		SELECT c.id, c.name, c.created_at
		FROM collections c
		JOIN game_collections gc ON gc.collection_id = c.id
		WHERE gc.game_id = ?
		ORDER BY c.name COLLATE NOCASE`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var collections []Collection
	for rows.Next() {
		var c Collection
		var createdAtStr sql.NullString
		if err := rows.Scan(&c.ID, &c.Name, &createdAtStr); err != nil {
			return nil, err
		}
		if createdAtStr.Valid {
			c.CreatedAt = parseTime(createdAtStr.String)
		}
		collections = append(collections, c)
	}
	return collections, rows.Err()
}

// GetGamesInCollection returns all active (non-deleted) games in a collection,
// ordered by title.
func (db *Database) GetGamesInCollection(collectionID int64) ([]*Game, error) {
	rows, err := db.conn.Query(`
		SELECT g.id, g.title, g.engine, g.path, g.exe_path, g.version, g.size_bytes,
		       g.f95_url, g.f95_thread_id, g.tags, g.status, g.latest_version, g.version_checked_at, g.notes,
		       g.store_links, g.steam_app_id, g.last_scanned_at, g.dir_mtime, g.series_id, g.series_order,
		       g.deleted_at, g.created_at, g.updated_at
		FROM games g
		JOIN game_collections gc ON gc.game_id = g.id
		WHERE gc.collection_id = ?
		  AND g.deleted_at IS NULL
		ORDER BY g.title COLLATE NOCASE`, collectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var games []*Game
	for rows.Next() {
		g, err := scanGame(rows)
		if err != nil {
			return nil, err
		}
		games = append(games, g)
	}
	return games, rows.Err()
}

// ListGameSummariesInCollection returns lightweight summaries for games in a
// collection, ordered by title.
func (db *Database) ListGameSummariesInCollection(collectionID int64) ([]GameSummary, error) {
	rows, err := db.conn.Query(`
		SELECT g.id, g.title, g.engine, g.version, g.latest_version, g.status, g.path
		FROM games g
		JOIN game_collections gc ON gc.game_id = g.id
		WHERE gc.collection_id = ?
		  AND g.deleted_at IS NULL
		  AND g.path NOT LIKE '%.old'
		ORDER BY g.title COLLATE NOCASE`, collectionID)
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
