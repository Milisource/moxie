package db

import (
	"fmt"
	"time"
)

// ResolvedURL is a cached unmasking result for an F95Zone masked link.
type ResolvedURL struct {
	MaskedURL    string
	ResolvedURL  string
	ResolvedHost string
	CreatedAt    int64
	Hits         int
}

// GetResolvedURL returns a cached unmask result newer than maxAge. It also
// increments the hit counter so stale entries can be pruned intelligently.
func (db *Database) GetResolvedURL(maskedURL string, maxAge time.Duration) (string, string, bool) {
	cutoff := time.Now().Add(-maxAge).Unix()
	var resolved, host string
	err := db.conn.QueryRow(
		`SELECT resolved_url, COALESCE(resolved_host, '')
		 FROM resolved_urls
		 WHERE masked_url = ? AND created_at >= ?`,
		maskedURL, cutoff,
	).Scan(&resolved, &host)
	if err != nil {
		return "", "", false
	}
	_, _ = db.conn.Exec(`UPDATE resolved_urls SET hits = hits + 1 WHERE masked_url = ?`, maskedURL)
	return resolved, host, true
}

// SaveResolvedURL caches a masked-link resolution.
func (db *Database) SaveResolvedURL(maskedURL, resolvedURL, resolvedHost string) error {
	if maskedURL == "" || resolvedURL == "" {
		return fmt.Errorf("resolved URL cache: empty key or value")
	}
	_, err := db.conn.Exec(
		`INSERT INTO resolved_urls (masked_url, resolved_url, resolved_host, created_at, hits)
		 VALUES (?, ?, ?, ?, 0)
		 ON CONFLICT(masked_url) DO UPDATE SET
		   resolved_url = excluded.resolved_url,
		   resolved_host = excluded.resolved_host,
		   created_at = excluded.created_at`,
		maskedURL, resolvedURL, resolvedHost, time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("resolved URL cache: %w", err)
	}
	return nil
}

// PruneResolvedURLs deletes cache entries older than maxAge.
func (db *Database) PruneResolvedURLs(maxAge time.Duration) error {
	cutoff := time.Now().Add(-maxAge).Unix()
	if _, err := db.conn.Exec(`DELETE FROM resolved_urls WHERE created_at < ?`, cutoff); err != nil {
		return fmt.Errorf("prune resolved URL cache: %w", err)
	}
	return nil
}

// CountResolvedURLs returns the number of cached unmask results.
func (db *Database) CountResolvedURLs() (int, error) {
	var n int
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM resolved_urls`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}
