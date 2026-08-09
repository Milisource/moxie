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
	"sync"
	"time"

	"github.com/browserutils/kooky"
	_ "github.com/browserutils/kooky/browser/brave"
	_ "github.com/browserutils/kooky/browser/chrome"
	_ "github.com/browserutils/kooky/browser/chromium"
	_ "github.com/browserutils/kooky/browser/edge"
	_ "github.com/browserutils/kooky/browser/firefox"
	_ "github.com/ncruces/go-sqlite3/driver"
)

// cookieCacheTTL bounds how often the browser cookie stores are re-read.
// Reading every browser profile via kooky is comparatively expensive, while
// Cloudflare clearance tokens (cf_clearance, __cf_bm) expire in roughly
// 30 min-2 h — a 60 s cache keeps headers fresh without a kooky read per
// request, and is shared across all hosts and callers.
const cookieCacheTTL = 60 * time.Second

// maxCookieHeaderLen caps the Cookie header built for download hosts. Some
// servers reject headers above ~8 KB; cf_clearance alone is ~1-2 KB and is
// the most valuable cookie, so the cap only trims low-value extras.
const maxCookieHeaderLen = 6000

// cookieCache holds the kooky snapshot; refreshed at most once per
// cookieCacheTTL. The error is kept so callers can distinguish "no cookies
// stored" from "could not read the stores" and degrade accordingly.
var cookieCache = struct {
	mu      sync.Mutex
	cookies []*kooky.Cookie
	readErr error
	at      time.Time
}{}

// cachedBrowserCookies returns the kooky cookie snapshot, refreshing it at
// most once per cookieCacheTTL. On refresh failure the previous snapshot is
// kept (and the error returned) so callers can decide how to degrade.
func cachedBrowserCookies() ([]*kooky.Cookie, error) {
	cookieCache.mu.Lock()
	defer cookieCache.mu.Unlock()
	if cookieCache.at.IsZero() || time.Since(cookieCache.at) > cookieCacheTTL {
		cookieCache.cookies, cookieCache.readErr = kooky.ReadCookies(context.Background(), kooky.Valid)
		cookieCache.at = time.Now()
	}
	return cookieCache.cookies, cookieCache.readErr
}

// GetF95Cookies extracts f95zone.to cookies from installed browsers.
// Returns a Cookie header string suitable for HTTP requests.
// Checks kooky's standard paths first, then falls back to non-standard
// locations like ~/.config/mozilla/firefox (used by some distros).
func GetF95Cookies() (string, error) {
	// No kooky.Domain filter here: kooky's Domain filter matches with exact
	// equality (cookie.Domain == "f95zone.to"), which drops domain-scoped
	// cookies stored with a leading dot (".f95zone.to") by Firefox/Chrome.
	// All cookies are read and the f95zone filter below runs locally.
	cookies, kookyErr := cachedBrowserCookies()

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

// GetCookiesForHost extracts cookies valid for hostname from installed
// browsers and returns them as a Cookie header string. Hostnames are
// matched per the RFC 6265 domain-match rule, so a cf_clearance stored for
// ".buzzheavier.com" is returned for "dd.buzzheavier.com" but never for an
// unrelated host. Returns "" without error when the browser holds no
// cookies for the host (e.g. download hosts the user has never visited).
func GetCookiesForHost(hostname string) (string, error) {
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	hostname = strings.TrimSuffix(hostname, ".")
	if hostname == "" {
		return "", nil
	}

	cookies, readErr := cachedBrowserCookies()
	if header := buildHostCookieHeader(cookies, hostname); header != "" {
		return header, nil
	}

	// kooky read failed outright (e.g. distro-specific Firefox profile
	// paths) — retry through the SQLite fallback. A successful read with no
	// matching cookies means the browser genuinely has none for this host.
	if readErr != nil {
		if header, err := tryNonStandardFirefoxPathsForHost(hostname); err == nil && header != "" {
			return header, nil
		}
	}
	return "", nil
}

// buildHostCookieHeader filters cookies to those valid for hostname, dedups
// to the newest value per name, sorts by name, and caps the total header
// length at maxCookieHeaderLen so oversized stores cannot produce a header
// that servers reject.
func buildHostCookieHeader(cookies []*kooky.Cookie, hostname string) string {
	var matched []*kooky.Cookie
	for _, c := range cookies {
		if c == nil {
			continue
		}
		if domainMatchesHostname(hostname, c.Domain) {
			matched = append(matched, c)
		}
	}
	matched = dedupCookies(matched)
	sort.Slice(matched, func(i, j int) bool { return matched[i].Name < matched[j].Name })

	var pairs []string
	length := 0
	for _, c := range matched {
		value := sanitizeHeaderValue(c.Value)
		if value == "" {
			continue
		}
		pair := c.Name + "=" + value
		if length > 0 && length+len(pair)+2 > maxCookieHeaderLen {
			continue
		}
		pairs = append(pairs, pair)
		length += len(pair) + 2
	}
	return strings.Join(pairs, "; ")
}

// domainMatchesHostname implements the RFC 6265 domain-match rule browsers
// use to decide whether a stored cookie applies to a request host: the
// cookie's Domain attribute (leading dot ignored) must equal the hostname
// or be a parent domain of it (".buzzheavier.com" → "dd.buzzheavier.com").
// Browsers enforce the public suffix list when storing cookies, so a
// Domain=".com" cookie cannot exist in a real store; the single-label guard
// and the leading-dot boundary check keep lookalikes ("notbuzzheavier.com")
// out regardless.
func domainMatchesHostname(hostname, cookieDomain string) bool {
	h := strings.ToLower(strings.TrimSuffix(hostname, "."))
	d := strings.ToLower(strings.TrimPrefix(cookieDomain, "."))
	d = strings.TrimSuffix(d, ".")
	if h == "" || d == "" || !strings.Contains(d, ".") {
		return false
	}
	return h == d || strings.HasSuffix(h, "."+d)
}

// GetF95CookiesFromSQLite reads f95zone.to cookies directly from a Firefox
// cookies.sqlite file at the given path.
func GetF95CookiesFromSQLite(path string) (string, error) {
	return GetCookiesFromSQLite(path, "f95zone.to")
}

// GetCookiesFromSQLite reads cookies matching hostname directly from a
// Firefox cookies.sqlite file at the given path. Matches the exact host,
// the dot-prefixed domain form Firefox uses for domain cookies, and any
// subdomain (the LIKE pattern anchors on a literal dot, so lookalikes like
// "notf95zone.to" cannot match).
func GetCookiesFromSQLite(path, hostname string) (string, error) {
	db, err := sql.Open("sqlite3", "file:"+path+"?mode=ro&immutable=1")
	if err != nil {
		return "", fmt.Errorf("opening cookie database: %w", err)
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT name, value FROM moz_cookies
		WHERE (host = ? OR host = '.' || ? OR host LIKE '%.' || ?)
		AND (expiry = 0 OR expiry > strftime('%s','now'))
	`, hostname, hostname, hostname)
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
		return "", fmt.Errorf("no %s cookies found in %s", hostname, path)
	}
	sort.Strings(pairs)
	return strings.Join(pairs, "; "), nil
}

// tryNonStandardFirefoxPathsForHost checks Firefox profile locations that
// kooky doesn't cover (e.g. ~/.config/mozilla/firefox) for cookies matching
// hostname.
func tryNonStandardFirefoxPathsForHost(hostname string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	matches, err := filepath.Glob(filepath.Join(home, ".config", "mozilla", "firefox", "*", "cookies.sqlite"))
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("no cookie stores found in non-standard paths")
	}
	for _, match := range matches {
		cookie, err := GetCookiesFromSQLite(match, hostname)
		if err == nil && cookie != "" {
			return cookie, nil
		}
	}
	return "", fmt.Errorf("no %s cookies found in non-standard paths", hostname)
}

// tryNonStandardFirefoxPaths checks Firefox profile locations that kooky
// doesn't cover (e.g. ~/.config/mozilla/firefox).
func tryNonStandardFirefoxPaths() (string, error) {
	return tryNonStandardFirefoxPathsForHost("f95zone.to")
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
