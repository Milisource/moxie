package db

import (
	"database/sql"
	"errors"
	"strings"
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
		m.CoverURL = CleanCoverURL(coverURL.String)
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
		nullableString(CleanCoverURL(m.CoverURL)),
		now,
	)
	return err
}

// SetScrapedMetaFields sets only the developer and overview columns for a
// game. Unlike UpsertScrapedMeta it leaves cover_url and last_scraped alone:
// a manual edit is not a scrape, so it must not clobber the cover resolved
// from F95Zone nor claim the metadata was just refreshed (LastScraped is
// shown in the CLI/TUI detail view).
//
// A row is created when none exists yet, with a NULL cover and the table's
// default last_scraped.
func (db *Database) SetScrapedMetaFields(gameID int64, developer, overview string) error {
	// Check for an existing row first rather than relying on UPDATE's
	// RowsAffected: a no-op UPDATE may report 0, which would send us down the
	// insert path and clobber the cover.
	existing, err := db.GetScrapedMeta(gameID)
	if err != nil {
		return err
	}
	if existing == nil {
		// No row yet (e.g. a scanned game that was never scraped): create one
		// through the normal upsert path, with no cover.
		return db.UpsertScrapedMeta(&ScrapedMeta{
			GameID:    gameID,
			Developer: developer,
			Overview:  overview,
		})
	}

	// Update in place — leaves cover_url and last_scraped untouched.
	_, err = db.conn.Exec(`
		UPDATE scraped_meta SET developer = ?, overview = ? WHERE game_id = ?`,
		nullableString(developer), nullableString(overview), gameID)
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

// CleanCoverURL returns u when it is an http(s) URL and "" otherwise —
// F95Zone's API answers "missing" for threads without a cover image.
func CleanCoverURL(u string) string {
	u = strings.TrimSpace(u)
	if strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") {
		return u
	}
	return ""
}
