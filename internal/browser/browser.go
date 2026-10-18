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
	"github.com/browserutils/kooky/browser/firefox"
	_ "github.com/ncruces/go-sqlite3/driver"
)

// cookieCacheTTL bounds how often the browser cookie stores are re-read.
// Reading every browser profile — kooky's set plus every Firefox profile on
// disk — is comparatively expensive, while Cloudflare clearance tokens
// (cf_clearance, __cf_bm) expire in roughly 30 min-2 h; a 60 s cache keeps
// headers fresh without a full read per request, and is shared across all
// hosts and callers.
const cookieCacheTTL = 60 * time.Second

// maxCookieHeaderLen caps the Cookie header built for download hosts. Some
// servers reject headers above ~8 KB; cf_clearance alone is ~1-2 KB and is
// the most valuable cookie, so the cap only trims low-value extras.
const maxCookieHeaderLen = 6000

// cookieCache holds the cookie snapshot; refreshed at most once per
// cookieCacheTTL. The error is kept so callers can distinguish "no cookies
// stored" from "could not read the stores" and degrade accordingly.
var cookieCache = struct {
	mu      sync.Mutex
	cookies []*kooky.Cookie
	readErr error
	at      time.Time
}{}

// cachedBrowserCookies returns the cookie snapshot, refreshing it at most
// once per cookieCacheTTL. On refresh failure the previous snapshot is kept
// (and the error returned) so callers can decide how to degrade.
//
// The snapshot spans every browser kooky knows about plus every Firefox
// profile found on disk — see readExtraFirefoxCookies for why kooky's own
// profile discovery is not sufficient.
func cachedBrowserCookies() ([]*kooky.Cookie, error) {
	cookieCache.mu.Lock()
	defer cookieCache.mu.Unlock()
	if cookieCache.at.IsZero() || time.Since(cookieCache.at) > cookieCacheTTL {
		cookies, err := kooky.ReadCookies(context.Background(), kooky.Valid)
		cookies = append(cookies, readExtraFirefoxCookies(cookies)...)
		cookieCache.cookies, cookieCache.readErr = cookies, err
		cookieCache.at = time.Now()
	}
	return cookieCache.cookies, cookieCache.readErr
}

// firefoxProfileRoots returns the directories that may hold Firefox profile
// subdirectories. It mirrors kooky's own Firefox roots plus the
// distro-specific ~/.config/mozilla/firefox location.
func firefoxProfileRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, "snap", "firefox", "common", ".mozilla", "firefox"),
		filepath.Join(home, ".mozilla", "firefox"),
		filepath.Join(home, ".mozilla", "firefox-esr"),
		filepath.Join(home, ".config", "mozilla", "firefox"),
	}
}

// firefoxProfileCookieFiles returns the cookies.sqlite path of every Firefox
// profile directory found under the known roots.
//
// kooky only walks the profiles named in profiles.ini, and on modern Firefox
// that file is rewritten to point at whichever profile was activated last
// (and may omit the others entirely). Which profile Firefox last activated has
// nothing to do with which one the user is logged into, so the raw directory
// scan is what keeps every profile — including a second window's — reachable.
func firefoxProfileCookieFiles() []string {
	var files []string
	for _, root := range firefoxProfileRoots() {
		matches, err := filepath.Glob(filepath.Join(root, "*", "cookies.sqlite"))
		if err != nil {
			continue
		}
		files = append(files, matches...)
	}
	return files
}

// readExtraFirefoxCookies reads every Firefox profile cookies.sqlite the kooky
// snapshot did not already cover. Files already present in known are skipped by
// path so a profile is never read twice. Individual profile read failures are
// ignored: a profile that cannot be read simply contributes no cookies.
func readExtraFirefoxCookies(known []*kooky.Cookie) []*kooky.Cookie {
	seen := make(map[string]bool, len(known))
	for _, c := range known {
		if c == nil || c.Browser == nil {
			continue
		}
		if path := c.Browser.FilePath(); path != "" {
			seen[filepath.Clean(path)] = true
		}
	}
	var extra []*kooky.Cookie
	for _, file := range firefoxProfileCookieFiles() {
		if seen[filepath.Clean(file)] {
			continue
		}
		cookies, _ := firefox.ReadCookies(context.Background(), file, kooky.Valid)
		extra = append(extra, cookies...)
	}
	return extra
}

// GetF95Cookies extracts f95zone.to cookies from installed browsers.
// Returns a Cookie header string suitable for HTTP requests.
// Reads every browser store kooky finds plus every Firefox profile on disk
// (see firefoxProfileCookieFiles), so it does not depend on Firefox's mutable
// profiles.ini pointer.
func GetF95Cookies() (string, error) {
	// No kooky.Domain filter here: kooky's Domain filter matches with exact
	// equality (cookie.Domain == "f95zone.to"), which drops domain-scoped
	// cookies stored with a leading dot (".f95zone.to") by Firefox/Chrome.
	// All cookies are read and the f95zone filter below runs locally.
	cookies, kookyErr := cachedBrowserCookies()

	f95 := selectF95Cookies(cookies)

	if len(f95) > 0 {
		sort.Slice(f95, func(i, j int) bool { return f95[i].Name < f95[j].Name })
		return buildCookieHeader(f95), nil
	}

	msg := "no f95zone.to cookies found in any browser"
	if kookyErr != nil {
		msg += fmt.Sprintf(" (kooky read: %v)", kookyErr)
	}
	msg += "\nMake sure you're logged into f95zone.to in Firefox or Chrome"
	return "", fmt.Errorf("%s", msg)
}

// selectF95Cookies returns the F95Zone cookies to send for the login session.
//
// A login is a pair — xf_user and xf_session must come from the same store to
// be accepted — while the snapshot spans every browser and every Firefox
// profile on disk. Merging purely by name (newest value wins) could therefore
// pair one profile's xf_user with another profile's xf_session. So the store
// holding the newest login cookie wins outright, and only names that store
// lacks (e.g. a cf_clearance held by a profile that is not logged in) are
// filled in from the remaining stores. With no login cookie anywhere, the
// cookies are merged by name as before.
func selectF95Cookies(cookies []*kooky.Cookie) []*kooky.Cookie {
	f95 := filterF95Cookies(cookies)
	if len(f95) == 0 {
		return nil
	}
	session := newestF95SessionStore(f95)
	if session == "" {
		return dedupCookies(f95)
	}

	var store []*kooky.Cookie
	for _, c := range f95 {
		if c != nil && cookieStoreKey(c) == session {
			store = append(store, c)
		}
	}
	chosen := dedupCookies(store)
	seen := make(map[string]bool, len(chosen))
	for _, c := range chosen {
		seen[c.Name] = true
	}
	for _, c := range dedupCookies(f95) {
		if c == nil || seen[c.Name] {
			continue
		}
		seen[c.Name] = true
		chosen = append(chosen, c)
	}
	return chosen
}

// f95SessionCookie reports whether name is one of the F95Zone login cookies.
func f95SessionCookie(name string) bool {
	return name == "xf_user" || name == "xf_session"
}

// newestF95SessionStore returns the store key of the newest F95Zone login
// cookie, or "" when the cookies contain no login cookie.
func newestF95SessionStore(f95 []*kooky.Cookie) string {
	var newest *kooky.Cookie
	for _, c := range f95 {
		if c == nil || !f95SessionCookie(c.Name) {
			continue
		}
		if newest == nil || cookieNewer(c, newest) {
			newest = c
		}
	}
	if newest == nil {
		return ""
	}
	return cookieStoreKey(newest)
}

// cookieStoreKey identifies the browser profile a cookie came from, so cookies
// from different stores are never mixed within one login session.
func cookieStoreKey(c *kooky.Cookie) string {
	if c == nil || c.Browser == nil {
		return ""
	}
	return c.Browser.Browser() + "\x00" + c.Browser.Profile() + "\x00" + c.Browser.FilePath()
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

	cookies, _ := cachedBrowserCookies()
	return buildHostCookieHeader(cookies, hostname), nil
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

// cookieBrowserOrder is the stable order GetCookieBrowsersForHost reports
// browsers in; earlier entries win when several stores hold cookies for the
// same host.
var cookieBrowserOrder = []string{"chrome", "chromium", "edge", "brave", "firefox"}

// GetCookieBrowsersForHost returns the names of the browsers whose stores
// hold at least one cookie valid for hostname (RFC 6265 domain match), in a
// stable order (chrome, chromium, edge, brave, firefox). A cf_clearance is
// cryptographically bound to the browser that minted it, so the fallback
// download engine should prefer a browser listed here — it is the only one
// guaranteed to pass the host's challenge. Returns nil when the browser
// holds no cookies for the host.
func GetCookieBrowsersForHost(hostname string) []string {
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	hostname = strings.TrimSuffix(hostname, ".")
	if hostname == "" {
		return nil
	}
	cookies, _ := cachedBrowserCookies()
	return cookieBrowsersForHostname(cookies, hostname)
}

// cookieBrowsersForHostname filters a cookie snapshot to the browsers
// holding cookies valid for hostname, in the cookieBrowserOrder preference.
func cookieBrowsersForHostname(cookies []*kooky.Cookie, hostname string) []string {
	seen := map[string]bool{}
	var browsers []string
	for _, c := range cookies {
		if c == nil || c.Browser == nil {
			continue
		}
		name := c.Browser.Browser()
		if name == "" {
			continue
		}
		if !domainMatchesHostname(hostname, c.Domain) {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		browsers = append(browsers, name)
	}
	sort.SliceStable(browsers, func(i, j int) bool {
		return browserOrderIndex(browsers[i]) < browserOrderIndex(browsers[j])
	})
	return browsers
}

func browserOrderIndex(name string) int {
	for i, b := range cookieBrowserOrder {
		if name == b {
			return i
		}
	}
	return len(cookieBrowserOrder)
}

// CookieParam is a cookie ready for injection into a browser session (CDP),
// so a fallback engine can carry the user's session cookies even when they
// live in a different browser's store (e.g. Firefox cookies powering a
// Chromium session).
type CookieParam struct {
	Name     string
	Value    string
	Domain   string
	Path     string
	Secure   bool
	HTTPOnly bool
	// Expires is the unix-seconds expiry; 0 = session cookie.
	Expires int64
}

// GetCookieParamsForHost returns the cookies valid for hostname (RFC 6265
// domain match) as injection-ready params, drawn from every browser store.
// Returns nil when no browser holds cookies for the host.
func GetCookieParamsForHost(hostname string) []CookieParam {
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	hostname = strings.TrimSuffix(hostname, ".")
	if hostname == "" {
		return nil
	}
	cookies, _ := cachedBrowserCookies()
	var params []CookieParam
	for _, c := range cookies {
		if c == nil {
			continue
		}
		if !domainMatchesHostname(hostname, c.Domain) {
			continue
		}
		value := sanitizeHeaderValue(c.Value)
		if value == "" {
			continue
		}
		var expires int64
		if !c.Expires.IsZero() {
			expires = c.Expires.Unix()
		}
		params = append(params, CookieParam{
			Name:     c.Name,
			Value:    value,
			Domain:   c.Domain,
			Path:     c.Path,
			Secure:   c.Secure,
			HTTPOnly: c.HttpOnly,
			Expires:  expires,
		})
	}
	return params
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
