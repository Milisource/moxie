// Package browser provides cross-browser cookie extraction for authenticating
// with web services. Uses github.com/browserutils/kooky which reads SQLite
// files at the binary level (B-tree parser), avoiding WAL/locking issues
// that plague SQL drivers when reading live browser databases.
package browser

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/browserutils/kooky"
	_ "github.com/browserutils/kooky/browser/firefox"
	_ "github.com/ncruces/go-sqlite3/driver"
)

// GetF95Cookies extracts f95zone.to cookies from installed browsers.
// Returns a Cookie header string suitable for HTTP requests.
// Checks kooky's standard paths first, then falls back to non-standard
// locations like ~/.config/mozilla/firefox (used by some distros).
func GetF95Cookies() (string, error) {
	cookies, _ := kooky.ReadCookies(
		context.Background(),
		kooky.Valid,
		kooky.Domain("f95zone.to"),
	)

	var f95 []*kooky.Cookie
	for _, c := range cookies {
		if c.Domain == "f95zone.to" || strings.HasSuffix(c.Domain, ".f95zone.to") {
			f95 = append(f95, c)
		}
	}

	if len(f95) > 0 {
		sort.Slice(f95, func(i, j int) bool { return f95[i].Name < f95[j].Name })
		return buildCookieHeader(f95), nil
	}

	// kooky didn't find cookies — try non-standard Firefox profile locations
	// (e.g. ~/.config/mozilla/firefox used on some distros and custom setups).
	if cookie, err := tryNonStandardFirefoxPaths(); err == nil && cookie != "" {
		return cookie, nil
	}

	return "", fmt.Errorf("no f95zone.to cookies found in any browser\n" +
		"Make sure you're logged into f95zone.to in Firefox or Chrome")
}

// GetF95CookiesFromSQLite reads f95zone.to cookies directly from a Firefox
// cookies.sqlite file at the given path.
func GetF95CookiesFromSQLite(path string) (string, error) {
	db, err := sql.Open("sqlite3", "file:"+path+"?mode=ro&immutable=1")
	if err != nil {
		return "", fmt.Errorf("opening cookie database: %w", err)
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT name, value FROM moz_cookies
		WHERE (host = 'f95zone.to' OR host = '.f95zone.to')
		AND (expiry = 0 OR expiry > strftime('%s','now'))
	`)
	if err != nil {
		return "", fmt.Errorf("querying cookies: %w", err)
	}
	defer rows.Close()

	var pairs []string
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			continue
		}
		value = sanitizeHeaderValue(value)
		if value == "" {
			continue
		}
		pairs = append(pairs, name+"="+value)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("reading cookies: %w", err)
	}
	if len(pairs) == 0 {
		return "", fmt.Errorf("no f95zone.to cookies found in %s — make sure you are logged in", path)
	}
	sort.Strings(pairs)
	return strings.Join(pairs, "; "), nil
}

// tryNonStandardFirefoxPaths checks Firefox profile locations that kooky
// doesn't cover (e.g. ~/.config/mozilla/firefox).
func tryNonStandardFirefoxPaths() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	roots := []string{
		filepath.Join(home, ".config", "mozilla", "firefox"),
	}
	for _, root := range roots {
		pattern := filepath.Join(root, "*", "cookies.sqlite")
		matches, err := filepath.Glob(pattern)
		if err != nil || len(matches) == 0 {
			continue
		}
		for _, match := range matches {
			cookie, err := GetF95CookiesFromSQLite(match)
			if err == nil && cookie != "" {
				return cookie, nil
			}
		}
	}
	return "", fmt.Errorf("no cookies found in non-standard paths")
}

// buildCookieHeader constructs a Cookie header string from cookie pairs.
// Cookie values from live browser databases can contain control characters
// (\r, \n, \x00) that Go's net/http rejects as invalid header values.
func buildCookieHeader(cookies []*kooky.Cookie) string {
	pairs := make([]string, 0, len(cookies))
	for _, c := range cookies {
		value := sanitizeHeaderValue(c.Value)
		if value == "" {
			continue
		}
		pairs = append(pairs, c.Name+"="+value)
	}
	return strings.Join(pairs, "; ")
}

// sanitizeHeaderValue strips bytes that are invalid in HTTP header values.
func sanitizeHeaderValue(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == '\x00' {
			return -1
		}
		return r
	}, s)
}
