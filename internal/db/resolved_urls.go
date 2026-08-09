package db

import (
	"fmt"
	"time"
)

// ResolvedURLTTL is how long a cached masked-URL unwrap result is trusted.
// F95Zone's masked unwrap endpoint (POST xhr=1&download=1) rate-limits after
// repeated hits, so caching unwrap results avoids re-hitting it — but the
// unwrapped destination can go stale (hosts rotate links), so entries are
// pruned after a week and stale entries are treated as misses at read time.
const ResolvedURLTTL = 7 * 24 * time.Hour

// GetResolvedURL returns the cached unwrap result for a masked F95Zone URL.
// The boolean is false when the URL is uncached or the entry is older than
// ResolvedURLTTL — a stale unwrap must re-hit the endpoint.
func (db *Database) GetResolvedURL(maskedURL string) (string, bool) {
	if maskedURL == "" {
		return "", false
	}
	var resolved string
	err := db.conn.QueryRow(`
		SELECT resolved_url FROM resolved_urls
		WHERE masked_url = ? AND created_at >= strftime('%s', 'now') - ?
	`, maskedURL, int64(ResolvedURLTTL.Seconds())).Scan(&resolved)
	if err != nil {
		return "", false
	}
	return resolved, true
}

// PutResolvedURL stores (or refreshes) the unwrap result for a masked URL.
// Upsert semantics: a re-unwrap of the same masked URL replaces the
// destination in place, restarts the TTL (created_at), and bumps hits — the
// table stays one row per masked URL.
func (db *Database) PutResolvedURL(maskedURL, resolvedURL, host string) error {
	if maskedURL == "" || resolvedURL == "" {
		return fmt.Errorf("resolved URL cache requires both a masked and a resolved URL")
	}
	_, err := db.conn.Exec(`
		INSERT INTO resolved_urls (masked_url, resolved_url, resolved_host, created_at, hits)
		VALUES (?, ?, ?, ?, 1)
		ON CONFLICT(masked_url) DO UPDATE SET
			resolved_url  = excluded.resolved_url,
			resolved_host = excluded.resolved_host,
			created_at    = excluded.created_at,
			hits          = hits + 1
	`, maskedURL, resolvedURL, nullableString(host), time.Now().Unix())
	return err
}

// DeleteResolvedURLsOlderThan removes cache entries older than age and
// returns the number of rows deleted.
func (db *Database) DeleteResolvedURLsOlderThan(age time.Duration) (int64, error) {
	res, err := db.conn.Exec(`
		DELETE FROM resolved_urls
		WHERE created_at < strftime('%s', 'now') - ?
	`, int64(age.Seconds()))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// PruneResolvedURLs deletes cache entries older than ResolvedURLTTL.
func (db *Database) PruneResolvedURLs() (int64, error) {
	return db.DeleteResolvedURLsOlderThan(ResolvedURLTTL)
}
