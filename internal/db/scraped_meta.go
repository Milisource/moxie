package db

import (
	"database/sql"
	"errors"
	"time"
)

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
