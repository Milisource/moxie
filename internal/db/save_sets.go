package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// saveSetColumns is the canonical column list for save_sets SELECTs. Keep it
// in the same order as scanSaveSet expects.
const saveSetColumns = `id, game_id, name, label, source, source_url, game_version, ` +
	`apply_root, size_bytes, file_count, active, created_at`

// scanSaveSet reads one save_sets row (in saveSetColumns order).
func scanSaveSet(s scanner) (*SaveSet, error) {
	var set SaveSet
	var label, source, applyRoot, createdAt string
	var sourceURL, gameVersion sql.NullString
	var active int

	if err := s.Scan(
		&set.ID, &set.GameID, &set.Name, &label, &source,
		&sourceURL, &gameVersion, &applyRoot,
		&set.SizeBytes, &set.FileCount, &active, &createdAt,
	); err != nil {
		return nil, err
	}

	set.Label = label
	set.Source = source
	set.ApplyRoot = applyRoot
	if sourceURL.Valid {
		set.SourceURL = sourceURL.String
	}
	if gameVersion.Valid {
		set.GameVersion = gameVersion.String
	}
	set.Active = active != 0
	if createdAt != "" {
		set.CreatedAt = parseTime(createdAt)
	}
	return &set, nil
}

// CreateSaveSet inserts a new save set and returns its ID. It also fills in
// set.ID and (when unset) set.CreatedAt. Source defaults to "import".
func (db *Database) CreateSaveSet(set *SaveSet) (int64, error) {
	if set == nil {
		return 0, errors.New("db: cannot insert nil save set")
	}
	if set.GameID == 0 {
		return 0, errors.New("db: save set requires a game_id")
	}
	if set.Name == "" {
		return 0, errors.New("db: save set requires a name")
	}
	if set.Source == "" {
		set.Source = "import"
	}

	createdAt := time.Now().UTC().Format(time.RFC3339)
	if !set.CreatedAt.IsZero() {
		createdAt = set.CreatedAt.UTC().Format(time.RFC3339)
	} else {
		set.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	}

	res, err := db.conn.Exec(`
		INSERT INTO save_sets (game_id, name, label, source, source_url,
		                       game_version, apply_root, size_bytes, file_count,
		                       active, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		set.GameID, set.Name, set.Label, set.Source,
		nullableString(set.SourceURL), nullableString(set.GameVersion),
		set.ApplyRoot, set.SizeBytes, set.FileCount, boolToInt(set.Active),
		createdAt)
	if err != nil {
		return 0, err
	}

	set.ID, err = res.LastInsertId()
	return set.ID, err
}

// GetSaveSet retrieves a set by (game_id, name). It returns nil, nil when no
// matching row exists.
func (db *Database) GetSaveSet(gameID int64, name string) (*SaveSet, error) {
	row := db.conn.QueryRow(
		"SELECT "+saveSetColumns+" FROM save_sets WHERE game_id = ? AND name = ?",
		gameID, name)
	set, err := scanSaveSet(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return set, nil
}

// GetSaveSetByID retrieves a set by primary key. It returns nil, nil when no
// matching row exists.
func (db *Database) GetSaveSetByID(id int64) (*SaveSet, error) {
	row := db.conn.QueryRow("SELECT "+saveSetColumns+" FROM save_sets WHERE id = ?", id)
	set, err := scanSaveSet(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return set, nil
}

// ListSaveSets returns all save sets for a game, newest first.
func (db *Database) ListSaveSets(gameID int64) ([]SaveSet, error) {
	rows, err := db.conn.Query(
		"SELECT "+saveSetColumns+" FROM save_sets WHERE game_id = ? ORDER BY created_at DESC, id DESC",
		gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sets []SaveSet
	for rows.Next() {
		set, err := scanSaveSet(rows)
		if err != nil {
			return nil, err
		}
		sets = append(sets, *set)
	}
	return sets, rows.Err()
}

// ListAllSaveSets returns every save set across all games, ordered by game_id
// ascending then newest first within each game.
func (db *Database) ListAllSaveSets() ([]SaveSet, error) {
	rows, err := db.conn.Query(
		"SELECT " + saveSetColumns +
			" FROM save_sets ORDER BY game_id ASC, created_at DESC, id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sets []SaveSet
	for rows.Next() {
		set, err := scanSaveSet(rows)
		if err != nil {
			return nil, err
		}
		sets = append(sets, *set)
	}
	return sets, rows.Err()
}

// DeleteSaveSet removes a save set row by ID. Deleting a non-existent ID is a
// no-op.
func (db *Database) DeleteSaveSet(id int64) error {
	_, err := db.conn.Exec("DELETE FROM save_sets WHERE id = ?", id)
	return err
}

// SetActiveSaveSet marks setID as the active set for gameID, deactivating any
// other active set for that game. Both writes happen in a single transaction
// so the partial unique index (one active set per game) never sees two active
// rows. It errors if (setID, gameID) does not exist.
func (db *Database) SetActiveSaveSet(gameID, setID int64) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var count int
	if err := tx.QueryRow(
		"SELECT COUNT(*) FROM save_sets WHERE id = ? AND game_id = ?", setID, gameID,
	).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("db: save set %d not found for game %d", setID, gameID)
	}

	// Deactivate first so activating the target cannot trip the partial index.
	if _, err := tx.Exec(
		"UPDATE save_sets SET active = 0 WHERE game_id = ? AND active = 1", gameID,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(
		"UPDATE save_sets SET active = 1 WHERE id = ? AND game_id = ?", setID, gameID,
	); err != nil {
		return err
	}

	return tx.Commit()
}

// GetSaveConfig returns the save config for a game. It returns nil, nil when
// no config exists.
func (db *Database) GetSaveConfig(gameID int64) (*SaveConfig, error) {
	row := db.conn.QueryRow(
		"SELECT game_id, manual_path, resolved_path, updated_at FROM save_configs WHERE game_id = ?",
		gameID)

	var cfg SaveConfig
	var manualPath, resolvedPath, updatedAt sql.NullString
	if err := row.Scan(&cfg.GameID, &manualPath, &resolvedPath, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if manualPath.Valid {
		cfg.ManualPath = manualPath.String
	}
	if resolvedPath.Valid {
		cfg.ResolvedPath = resolvedPath.String
	}
	if updatedAt.Valid {
		cfg.UpdatedAt = parseTime(updatedAt.String)
	}
	return &cfg, nil
}

// UpsertSaveConfig inserts or replaces the save config for a game.
func (db *Database) UpsertSaveConfig(cfg *SaveConfig) error {
	if cfg == nil {
		return errors.New("db: cannot upsert nil save config")
	}
	if cfg.GameID == 0 {
		return errors.New("db: save config requires a game_id")
	}

	updatedAt := time.Now().UTC().Format(time.RFC3339)
	if !cfg.UpdatedAt.IsZero() {
		updatedAt = cfg.UpdatedAt.UTC().Format(time.RFC3339)
	}

	_, err := db.conn.Exec(`
		INSERT INTO save_configs (game_id, manual_path, resolved_path, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(game_id) DO UPDATE SET
			manual_path = excluded.manual_path,
			resolved_path = excluded.resolved_path,
			updated_at = excluded.updated_at`,
		cfg.GameID, nullableString(cfg.ManualPath), nullableString(cfg.ResolvedPath), updatedAt)
	return err
}

// ClearSaveConfig removes the save config for a game. Clearing a game with no
// config is a no-op.
func (db *Database) ClearSaveConfig(gameID int64) error {
	_, err := db.conn.Exec("DELETE FROM save_configs WHERE game_id = ?", gameID)
	return err
}
