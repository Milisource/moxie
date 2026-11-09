package browser

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/browserutils/kooky"
)

// storeInfo is a BrowserInfo pointing at a real cookie file path, so the
// version lookups next to it can be exercised.
type storeInfo struct{ name, path string }

func (s storeInfo) Browser() string        { return s.name }
func (s storeInfo) Profile() string        { return "p" }
func (s storeInfo) IsDefaultProfile() bool { return true }
func (s storeInfo) FilePath() string       { return s.path }

func storeCookie(name, value string, info kooky.BrowserInfo, created time.Time) *kooky.Cookie {
	c := domainCookie(name, value, ".f95zone.to")
	c.Browser = info
	c.Creation = created
	return c
}

// firefoxProfile writes a profile dir whose compatibility.ini records version.
func firefoxProfile(t *testing.T, lastVersion string) string {
	t.Helper()
	dir := t.TempDir()
	ini := "[Compatibility]\nLastVersion=" + lastVersion + "\nLastOSABI=Linux_x86_64-gcc3\n"
	if err := os.WriteFile(filepath.Join(dir, "compatibility.ini"), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "cookies.sqlite")
}

func resetMinted(t *testing.T) {
	t.Helper()
	SetF95Clearance(F95Clearance{})
	t.Cleanup(func() { SetF95Clearance(F95Clearance{}) })
}

// The live failure (2026-11-01): the clearance came from a Firefox 159
// profile; the UA must say 159, read from that profile.
func TestF95ClearanceChoice_FirefoxProfileVersion(t *testing.T) {
	resetMinted(t)
	ff := storeInfo{"firefox", firefoxProfile(t, "159.0_20261027211755/20261027211755")}
	cookies := []*kooky.Cookie{
		storeCookie("xf_user", "u", ff, time.Now()),
		storeCookie("cf_clearance", "cf", ff, time.Now()),
	}
	ua, minted := f95ClearanceChoice(cookies, time.Now())
	if minted != nil {
		t.Fatal("no minted clearance installed, but one was chosen")
	}
	if !strings.Contains(ua, "rv:159.0) Gecko/20100101 Firefox/159.0") {
		t.Errorf("UA = %q, want Firefox 159", ua)
	}
}

// The UA follows the store that supplied cf_clearance, not the one holding
// the login (selectF95Cookies fills a missing clearance from another store).
func TestF95ClearanceChoice_FollowsClearanceStore(t *testing.T) {
	resetMinted(t)
	if runtime.GOOS == "windows" {
		t.Skip("path layout")
	}
	login := storeInfo{"firefox", firefoxProfile(t, "143.0.1_2025")}
	userData := t.TempDir()
	if err := os.WriteFile(filepath.Join(userData, "Last Version"), []byte("156.0.8078.79"), 0o600); err != nil {
		t.Fatal(err)
	}
	chrome := storeInfo{"chrome", filepath.Join(userData, "Default", "Network", "Cookies")}
	cookies := []*kooky.Cookie{
		storeCookie("xf_user", "u", login, time.Now()),
		storeCookie("cf_clearance", "cf", chrome, time.Now()),
	}
	ua, _ := f95ClearanceChoice(cookies, time.Now())
	if !strings.Contains(ua, "Chrome/156.0.0.0 Safari/537.36") || strings.Contains(ua, "Edg/") {
		t.Errorf("UA = %q, want the reduced Chrome 156 UA", ua)
	}
}

func TestF95ClearanceChoice_NewestClearanceWins(t *testing.T) {
	resetMinted(t)
	ff := storeInfo{"firefox", firefoxProfile(t, "159.0_1")}
	now := time.Now()
	minted := F95Clearance{Value: "minted", UserAgent: "Camoufox-UA", Created: now.Add(-time.Hour), Expires: now.Add(time.Hour)}
	SetF95Clearance(minted)

	// Browser clearance older than the minted one → minted wins.
	old := []*kooky.Cookie{storeCookie("xf_user", "u", ff, now), storeCookie("cf_clearance", "browser", ff, now.Add(-2*time.Hour))}
	if ua, m := f95ClearanceChoice(old, now); m == nil || ua != "Camoufox-UA" {
		t.Errorf("older browser clearance: ua=%q minted=%v, want the minted pair", ua, m)
	}
	// The user solved the check again in the browser → browser wins.
	fresh := []*kooky.Cookie{storeCookie("xf_user", "u", ff, now), storeCookie("cf_clearance", "browser", ff, now)}
	if ua, m := f95ClearanceChoice(fresh, now); m != nil || !strings.Contains(ua, "Firefox/159.0") {
		t.Errorf("newer browser clearance: ua=%q minted=%v, want the browser pair", ua, m)
	}
	// An expired minted clearance is ignored.
	SetF95Clearance(F95Clearance{Value: "minted", UserAgent: "Camoufox-UA", Created: now, Expires: now.Add(-time.Minute)})
	if _, m := f95ClearanceChoice(old, now); m != nil {
		t.Error("expired minted clearance was chosen")
	}
}

func TestWithClearance_ReplacesValue(t *testing.T) {
	t.Parallel()
	in := []*kooky.Cookie{cookie("xf_user", "u"), cookie("cf_clearance", "old")}
	out := withClearance(in, "new")
	header := buildCookieHeader(out)
	if !strings.Contains(header, "cf_clearance=new") || strings.Contains(header, "old") {
		t.Errorf("header = %q", header)
	}
	if in[1].Value != "old" {
		t.Error("input slice was modified")
	}
}

func TestUserAgentForStore_UnknownVersion(t *testing.T) {
	t.Parallel()
	if ua := userAgentForStore(storeInfo{"chrome", filepath.Join(t.TempDir(), "Default", "Cookies")}); ua != "" {
		t.Errorf("UA without a Last Version file = %q, want \"\"", ua)
	}
	if ua := userAgentForStore(nil); ua != "" {
		t.Errorf("nil store UA = %q", ua)
	}
}
