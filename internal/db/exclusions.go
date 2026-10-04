package db

import "fmt"

// ExcludePath records that path must be skipped by scans and never
// re-imported. reason is informational (e.g. "duplicate"). Idempotent.
func (db *Database) ExcludePath(path, reason string) error {
	if path == "" {
		return fmt.Errorf("exclude path: empty path")
	}
	_, err := db.conn.Exec(
		`INSERT OR IGNORE INTO excluded_paths (path, reason) VALUES (?, ?)`,
		path, reason)
	return err
}

// UnexcludePath removes path from the scan exclusion list. No-op if absent.
func (db *Database) UnexcludePath(path string) error {
	_, err := db.conn.Exec(`DELETE FROM excluded_paths WHERE path = ?`, path)
	return err
}

// ExcludedPaths returns the set of paths currently excluded from scans.
func (db *Database) ExcludedPaths() (map[string]bool, error) {
	rows, err := db.conn.Query(`SELECT path FROM excluded_paths`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	paths := make(map[string]bool)
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		paths[p] = true
	}
	return paths, rows.Err()
}

// SoftDeleteAndExclude soft-deletes a game and records its path as excluded
// from future scans in a single transaction. Resolving a duplicate this way
// stops the directory that is still on disk from resurrecting the row (or
// inserting a fresh one) on the next scan.
func (db *Database) SoftDeleteAndExclude(id int64, reason string) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`UPDATE games SET deleted_at = datetime('now'), updated_at = datetime('now') WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("game %d not found", id)
	}
	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO excluded_paths (path, reason)
		 SELECT path, ? FROM games WHERE id = ?`,
		reason, id); err != nil {
		return err
	}
	return tx.Commit()
}
