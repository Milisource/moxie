package browser

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// futureExpiry is far enough ahead that kooky.Valid keeps the cookie. kooky's
// Firefox reader parses the expiry column as seconds.
const futureExpiry = 4102444800 // 2100-01-01

// testCookie is one row for buildFirefoxCookiesDB. created orders cookies that
// share a name across profiles (the newest wins).
type testCookie struct {
	name, value, host string
	created           time.Time
}

// buildFirefoxCookiesDB writes a Firefox cookies.sqlite at path holding rows.
// It uses the ncruces driver the package already links, so the file is a real
// store; kooky's Firefox reader parses the file's B-tree directly, and
// GetCookiesFromSQLite reads it over SQL.
func buildFirefoxCookiesDB(t *testing.T, path string, rows []testCookie) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE moz_cookies (
		id INTEGER PRIMARY KEY,
		originAttributes TEXT NOT NULL DEFAULT '',
		name TEXT, value TEXT, host TEXT, path TEXT,
		expiry INTEGER, lastAccessed INTEGER, creationTime INTEGER,
		isSecure INTEGER, isHttpOnly INTEGER,
		inBrowserElement INTEGER DEFAULT 0, sameSite INTEGER DEFAULT 0,
		schemeMap INTEGER DEFAULT 0, isPartitionedAttributeSet INTEGER DEFAULT 0,
		updateTime INTEGER
	)`); err != nil {
		t.Fatalf("create moz_cookies: %v", err)
	}
	for _, r := range rows {
		// kooky reads creationTime as microseconds (Firefox's unit).
		if _, err := db.Exec(`INSERT INTO moz_cookies
			(name, value, host, path, expiry, lastAccessed, creationTime, isSecure, isHttpOnly, originAttributes)
			VALUES (?, ?, ?, '/', ?, 0, ?, 0, 0, '')`,
			r.name, r.value, r.host, futureExpiry, r.created.UnixMicro()); err != nil {
			t.Fatalf("insert cookie %s: %v", r.name, err)
		}
	}
}

// writeProfilesINI writes a profiles.ini naming exactly one profile, mirroring
// modern Firefox rewriting the file to point at whichever profile it activated
// last.
func writeProfilesINI(t *testing.T, root, profileDir string) {
	t.Helper()
	ini := "[Profile0]\nName=default\nIsRelative=1\nPath=" + profileDir + "\nDefault=1\n"
	if err := os.WriteFile(filepath.Join(root, "profiles.ini"), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
}

// resetCookieCache clears the shared 60 s snapshot so a test controls what the
// next read picks up (otherwise it holds whatever the real machine held).
func resetCookieCache() {
	cookieCache.mu.Lock()
	defer cookieCache.mu.Unlock()
	cookieCache.cookies, cookieCache.readErr, cookieCache.at = nil, nil, time.Time{}
}

// newFirefoxHome points HOME at a temp dir and returns the Firefox profile
// root under it.
func newFirefoxHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return filepath.Join(home, ".mozilla", "firefox")
}

// Regression: modern Firefox rewrites profiles.ini to name only the profile it
// activated last, so the profile holding the F95Zone login is often absent from
// it. Every Firefox profile on disk must be read, not just the listed one.
func TestGetF95Cookies_ReadsProfileMissingFromProfilesINI(t *testing.T) {
	root := newFirefoxHome(t)

	// The profile Firefox activated last: clearance only, no login.
	buildFirefoxCookiesDB(t, filepath.Join(root, "aaaa.active", "cookies.sqlite"), []testCookie{
		{name: "cf_clearance", value: "active-cf", host: ".f95zone.to", created: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)},
	})
	// A second window's profile holding the login — absent from profiles.ini.
	buildFirefoxCookiesDB(t, filepath.Join(root, "bbbb.logged", "cookies.sqlite"), []testCookie{
		{name: "xf_user", value: "u1", host: "f95zone.to", created: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)},
	})
	writeProfilesINI(t, root, "aaaa.active")

	resetCookieCache()
	got, err := GetF95Cookies()
	if err != nil {
		t.Fatalf("GetF95Cookies: %v", err)
	}
	if !strings.Contains(got, "xf_user=u1") {
		t.Fatalf("GetF95Cookies() = %q, want the login from the unlisted profile", got)
	}
	if !strings.Contains(got, "cf_clearance=active-cf") {
		t.Errorf("GetF95Cookies() = %q, want the clearance from the listed profile filled in", got)
	}
}

// Regression: xf_user and xf_session are only valid together, so the header
// must not splice one profile's xf_user onto another profile's xf_session.
// The store holding the newest login cookie wins, and its pair is kept intact.
func TestGetF95Cookies_KeepsLoginPairInOneStore(t *testing.T) {
	root := newFirefoxHome(t)

	// Listed profile: the newer xf_user but the older session.
	buildFirefoxCookiesDB(t, filepath.Join(root, "aaaa.one", "cookies.sqlite"), []testCookie{
		{name: "xf_user", value: "user-a", host: "f95zone.to", created: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)},
		{name: "xf_session", value: "sess-a", host: "f95zone.to", created: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{name: "cf_clearance", value: "cf-a", host: ".f95zone.to", created: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
	})
	// Unlisted profile: the newest session, so its pair is the one to send —
	// even though the other profile has the newer xf_user.
	buildFirefoxCookiesDB(t, filepath.Join(root, "bbbb.two", "cookies.sqlite"), []testCookie{
		{name: "xf_user", value: "user-b", host: "f95zone.to", created: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{name: "xf_session", value: "sess-b", host: "f95zone.to", created: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)},
	})
	writeProfilesINI(t, root, "aaaa.one")

	resetCookieCache()
	got, err := GetF95Cookies()
	if err != nil {
		t.Fatalf("GetF95Cookies: %v", err)
	}
	for _, want := range []string{"xf_user=user-b", "xf_session=sess-b", "cf_clearance=cf-a"} {
		if !strings.Contains(got, want) {
			t.Errorf("GetF95Cookies() = %q, want %q", got, want)
		}
	}
}

// Without a login cookie anywhere, the cookies are still returned (merged,
// newest per name) rather than dropped.
func TestGetF95Cookies_NoLoginMergesNewest(t *testing.T) {
	root := newFirefoxHome(t)
	buildFirefoxCookiesDB(t, filepath.Join(root, "aaaa.active", "cookies.sqlite"), []testCookie{
		{name: "cf_clearance", value: "old-cf", host: ".f95zone.to", created: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
	})
	buildFirefoxCookiesDB(t, filepath.Join(root, "bbbb.extra", "cookies.sqlite"), []testCookie{
		{name: "cf_clearance", value: "new-cf", host: ".f95zone.to", created: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)},
	})
	writeProfilesINI(t, root, "aaaa.active")

	resetCookieCache()
	got, err := GetF95Cookies()
	if err != nil {
		t.Fatalf("GetF95Cookies: %v", err)
	}
	if got != "cf_clearance=new-cf" {
		t.Errorf("GetF95Cookies() = %q, want %q", got, "cf_clearance=new-cf")
	}
}

// Regression: a download host's clearance can live only in a profile that is
// not the one profiles.ini names; host lookup and engine selection must see it.
func TestGetCookiesForHost_ReadsProfileMissingFromProfilesINI(t *testing.T) {
	root := newFirefoxHome(t)
	buildFirefoxCookiesDB(t, filepath.Join(root, "aaaa.active", "cookies.sqlite"), []testCookie{
		{name: "xf_user", value: "u1", host: "f95zone.to", created: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)},
	})
	buildFirefoxCookiesDB(t, filepath.Join(root, "bbbb.logged", "cookies.sqlite"), []testCookie{
		{name: "cf_clearance", value: "buzz-cf", host: ".buzzheavier.com", created: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)},
	})
	writeProfilesINI(t, root, "aaaa.active")

	resetCookieCache()
	got, err := GetCookiesForHost("buzzheavier.com")
	if err != nil {
		t.Fatalf("GetCookiesForHost: %v", err)
	}
	if got != "cf_clearance=buzz-cf" {
		t.Errorf("GetCookiesForHost() = %q, want %q", got, "cf_clearance=buzz-cf")
	}
	if browsers := GetCookieBrowsersForHost("buzzheavier.com"); len(browsers) != 1 || browsers[0] != "firefox" {
		t.Errorf("GetCookieBrowsersForHost() = %v, want [firefox]", browsers)
	}
}
