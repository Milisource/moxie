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
	"github.com/browserutils/kooky/browser/chrome"
	_ "github.com/browserutils/kooky/browser/firefox"
	_ "github.com/ncruces/go-sqlite3/driver"
)

// GetF95Cookies extracts f95zone.to cookies from installed browsers.
// Returns a Cookie header string suitable for HTTP requests.
// Checks kooky's standard paths first, then falls back to non-standard
// locations like ~/.config/mozilla/firefox (used by some distros).
func GetF95Cookies() (string, error) {
	byName := map[string]string{}
	addHeader := func(header string) {
		for _, pair := range strings.Split(header, "; ") {
			name, value, ok := strings.Cut(pair, "=")
			if !ok || name == "" {
				continue
			}
			value = sanitizeHeaderValue(value)
			if value == "" {
				continue
			}
			byName[name] = value
		}
	}

	// kooky's registered finders (Firefox out of the box).
	cookies, kookyErr := kooky.ReadCookies(
		context.Background(),
		kooky.Valid,
		kooky.DomainHasSuffix("f95zone.to"),
	)
	for _, c := range cookies {
		if c.Domain == "f95zone.to" || strings.HasSuffix(c.Domain, ".f95zone.to") {
			if v := sanitizeHeaderValue(c.Value); v != "" {
				byName[c.Name] = v
			}
		}
	}

	// Non-standard Firefox profiles (e.g. ~/.config/mozilla/firefox).
	if cookie, err := tryNonStandardFirefoxPaths(); err == nil && cookie != "" {
		addHeader(cookie)
	}

	// Chromium-family profiles that kooky's built-in roots do not cover
	// (Helium, Playwright's bundled Chrome, portable Chromium builds, ...).
	if cookie, err := tryChromiumCookieStores(); err == nil && cookie != "" {
		addHeader(cookie)
	}

	if len(byName) > 0 {
		pairs := make([]string, 0, len(byName))
		for name, value := range byName {
			pairs = append(pairs, name+"="+value)
		}
		sort.Strings(pairs)
		return strings.Join(pairs, "; "), nil
	}

	msg := "no f95zone.to cookies found in any browser"
	if kookyErr != nil {
		msg += fmt.Sprintf(" (kooky read: %v)", kookyErr)
	}
	msg += "\nMake sure you're logged into f95zone.to in Firefox or a Chromium-family browser"
	return "", fmt.Errorf("%s", msg)
}

// tryChromiumCookieStores reads F95Zone cookies from Chromium-family profile
// directories that kooky does not discover automatically. Chromium encrypts
// cookie values, so the stores go through kooky's Chrome reader rather than
// the plain SQLite path used for Firefox.
func tryChromiumCookieStores() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	patterns := []string{
		filepath.Join(home, ".config", "*", "*", "Cookies"),
		filepath.Join(home, ".config", "*", "*", "*", "Cookies"),
		filepath.Join(home, ".config", "*", "*", "Network", "Cookies"),
		filepath.Join(home, ".config", "*", "*", "*", "Network", "Cookies"),
		filepath.Join(home, ".cache", "ms-playwright*", "*", "Default", "Cookies"),
	}

	seen := map[string]bool{}
	byName := map[string]string{}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, path := range matches {
			if seen[path] {
				continue
			}
			seen[path] = true
			store, err := chrome.CookieStore(path, kooky.Valid)
			if err != nil {
				continue
			}
			for c := range store.TraverseCookies(kooky.Valid, kooky.DomainHasSuffix("f95zone.to")) {
				if c == nil {
					continue
				}
				if v := sanitizeHeaderValue(c.Value); v != "" {
					byName[c.Name] = v
				}
			}
			_ = store.Close()
		}
	}
	if len(byName) == 0 {
		return "", fmt.Errorf("no f95zone.to cookies found in Chromium-family profiles")
	}
	pairs := make([]string, 0, len(byName))
	for name, value := range byName {
		pairs = append(pairs, name+"="+value)
	}
	sort.Strings(pairs)
	return strings.Join(pairs, "; "), nil
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
