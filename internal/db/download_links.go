package db

import (
	"database/sql"
	"errors"
	"time"
)

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
