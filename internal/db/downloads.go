package db

import (
	"database/sql"
	"errors"
	"time"
)

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
