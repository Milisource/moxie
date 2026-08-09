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
	_ "github.com/browserutils/kooky/browser/brave"
	_ "github.com/browserutils/kooky/browser/chrome"
	_ "github.com/browserutils/kooky/browser/chromium"
	_ "github.com/browserutils/kooky/browser/edge"
	_ "github.com/browserutils/kooky/browser/firefox"
	_ "github.com/ncruces/go-sqlite3/driver"
)

// GetF95Cookies extracts f95zone.to cookies from installed browsers.
// Returns a Cookie header string suitable for HTTP requests.
// Checks kooky's standard paths first, then falls back to non-standard
// locations like ~/.config/mozilla/firefox (used by some distros).
func GetF95Cookies() (string, error) {
	// No kooky.Domain filter here: kooky's Domain filter matches with exact
	// equality (cookie.Domain == "f95zone.to"), which drops domain-scoped
	// cookies stored with a leading dot (".f95zone.to") by Firefox/Chrome.
	// All cookies are read and the f95zone filter below runs locally.
	cookies, kookyErr := kooky.ReadCookies(
		context.Background(),
		kooky.Valid,
	)

	f95 := filterF95Cookies(cookies)
	// Same cookie name from multiple browsers/profiles (xf_session,
	// xf_user) would otherwise produce duplicate name=value pairs with
	// nondeterministic order — keep the newest value per name.
	f95 = dedupCookies(f95)

	if len(f95) > 0 {
		sort.Slice(f95, func(i, j int) bool { return f95[i].Name < f95[j].Name })
		return buildCookieHeader(f95), nil
	}

	// kooky didn't find cookies — try non-standard Firefox profile locations
	// (e.g. ~/.config/mozilla/firefox used on some distros and custom setups).
	if cookie, err := tryNonStandardFirefoxPaths(); err == nil && cookie != "" {
		return cookie, nil
	}

	msg := "no f95zone.to cookies found in any browser"
	if kookyErr != nil {
		msg += fmt.Sprintf(" (kooky read: %v)", kookyErr)
	}
	msg += "\nMake sure you're logged into f95zone.to in Firefox or Chrome"
	return "", fmt.Errorf("%s", msg)
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

// f95ZoneDomain reports whether a cookie domain is f95zone.to or one of its
// subdomains. Browser stores record domain-scoped cookies with a leading
// dot (".f95zone.to") — the leading dot also keeps lookalike hosts
// ("notf95zone.to") out.
func f95ZoneDomain(domain string) bool {
	return domain == "f95zone.to" || strings.HasSuffix(domain, ".f95zone.to")
}

// filterF95Cookies keeps only cookies whose domain belongs to f95zone.to.
func filterF95Cookies(cookies []*kooky.Cookie) []*kooky.Cookie {
	var f95 []*kooky.Cookie
	for _, c := range cookies {
		if c == nil {
			continue
		}
		if f95ZoneDomain(c.Domain) {
			f95 = append(f95, c)
		}
	}
	return f95
}

// dedupCookies removes duplicate cookie names, keeping the newest value per
// name (first occurrence's position is kept, so the result order is
// deterministic for a given input order). Multiple browsers/profiles can
// hold the same cookie (xf_session, xf_user) with different values; a
// Cookie header with duplicate name=value pairs is invalid, and which
// browser's value won used to be nondeterministic.
func dedupCookies(cookies []*kooky.Cookie) []*kooky.Cookie {
	seen := make(map[string]int, len(cookies)) // name → index in out
	out := make([]*kooky.Cookie, 0, len(cookies))
	for _, c := range cookies {
		if c == nil {
			continue
		}
		if idx, ok := seen[c.Name]; ok {
			if cookieNewer(c, out[idx]) {
				out[idx] = c
			}
			continue
		}
		seen[c.Name] = len(out)
		out = append(out, c)
	}
	return out
}

// cookieNewer reports whether a is newer than b: the later Creation wins;
// when both are unset, the later Expires is used as a proxy (some stores
// don't record creation). Zero times compare as older.
func cookieNewer(a, b *kooky.Cookie) bool {
	if a.Creation.Equal(b.Creation) {
		return a.Expires.After(b.Expires)
	}
	return a.Creation.After(b.Creation)
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
