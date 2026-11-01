package browser

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/browserutils/kooky"
)

// Cloudflare binds cf_clearance to the User-Agent of the browser that solved
// the challenge: the same cookie is accepted with that browser's exact UA and
// answered with a 403 challenge under any other. moxie therefore never sends a
// fixed UA with F95Zone cookies — it reports the UA of the store whose
// cf_clearance GetF95Cookies returns (live-verified 2026-11-01: Firefox 159's
// clearance → 200 with "rv:159.0", 403 with the old hard-coded "rv:140.0").

// F95Clearance is a cf_clearance minted outside the user's browsers (see
// internal/f95session) together with the UA it is bound to.
type F95Clearance struct {
	Value     string    `json:"value"`
	UserAgent string    `json:"user_agent"`
	Created   time.Time `json:"created"`
	Expires   time.Time `json:"expires"`
}

// usable reports whether the minted clearance can still be sent.
func (c F95Clearance) usable(now time.Time) bool {
	if c.Value == "" || c.UserAgent == "" {
		return false
	}
	return c.Expires.IsZero() || now.Before(c.Expires)
}

var mintedClearance struct {
	mu sync.Mutex
	c  F95Clearance
}

// SetF95Clearance installs a minted cf_clearance. GetF95Cookies and
// F95UserAgent prefer it over a browser-held clearance that is older, so a
// freshly minted clearance wins until the user's browser solves a newer one.
// A zero value clears the override.
func SetF95Clearance(c F95Clearance) {
	mintedClearance.mu.Lock()
	mintedClearance.c = c
	mintedClearance.mu.Unlock()
}

// CurrentF95Clearance returns the installed minted clearance, if usable.
func CurrentF95Clearance() (F95Clearance, bool) {
	mintedClearance.mu.Lock()
	defer mintedClearance.mu.Unlock()
	c := mintedClearance.c
	return c, c.usable(time.Now())
}

// F95UserAgent returns the User-Agent that matches the cf_clearance
// GetF95Cookies currently returns, or "" when it cannot be determined (no
// clearance anywhere, or a browser whose version is unreadable). Callers fall
// back to their own default UA on "".
func F95UserAgent() string {
	cookies, _ := cachedBrowserCookies()
	ua, _ := f95ClearanceChoice(cookies, time.Now())
	return ua
}

// f95ClearanceChoice decides which cf_clearance F95Zone requests carry and the
// UA that goes with it. minted reports whether the minted override won over
// the browser-held clearance (the newest one wins; browsers record creation
// time, the minter records its own).
func f95ClearanceChoice(cookies []*kooky.Cookie, now time.Time) (ua string, minted *F95Clearance) {
	browserClearance := selectedF95Clearance(cookies)
	if m, ok := CurrentF95Clearance(); ok {
		if browserClearance == nil || browserClearance.Creation.IsZero() || !browserClearance.Creation.After(m.Created) {
			return m.UserAgent, &m
		}
	}
	if browserClearance == nil {
		return "", nil
	}
	return userAgentForStore(browserClearance.Browser), nil
}

// selectedF95Clearance returns the cf_clearance cookie selectF95Cookies would
// send, or nil.
func selectedF95Clearance(cookies []*kooky.Cookie) *kooky.Cookie {
	for _, c := range selectF95Cookies(cookies) {
		if c != nil && c.Name == "cf_clearance" {
			return c
		}
	}
	return nil
}

// userAgentForStore builds the UA of the browser behind a cookie store from
// the version that store's profile records on disk. Returns "" when the
// version is unknown.
func userAgentForStore(b kooky.BrowserInfo) string {
	if b == nil {
		return ""
	}
	name := strings.ToLower(b.Browser())
	path := b.FilePath()
	switch {
	case strings.Contains(name, "firefox") || strings.Contains(name, "librewolf"):
		if v := firefoxProfileVersion(filepath.Dir(path)); v != "" {
			return firefoxUserAgent(v)
		}
		if v := installedFirefoxVersion(); v != "" {
			return firefoxUserAgent(v)
		}
	case name == "chrome" || name == "chromium" || name == "brave" || name == "edge":
		if v := chromiumUserDataVersion(path); v != "" {
			return chromiumUserAgent(v, name == "edge")
		}
	}
	return ""
}

// firefoxVersionRe pulls "159.0" out of compatibility.ini's
// "LastVersion=159.0_20261027211755/20261027211755" and application.ini's
// "Version=159.0".
var firefoxVersionRe = regexp.MustCompile(`^(\d+)\.(\d+)`)

// firefoxProfileVersion returns the Firefox version that last ran a profile,
// read from <profile>/compatibility.ini, or "".
func firefoxProfileVersion(profileDir string) string {
	return iniValueVersion(filepath.Join(profileDir, "compatibility.ini"), "LastVersion")
}

// installedFirefoxVersion reads the system Firefox's application.ini. Used
// only when the profile does not record a version.
func installedFirefoxVersion() string {
	for _, p := range firefoxApplicationINIs() {
		if v := iniValueVersion(p, "Version"); v != "" {
			return v
		}
	}
	return ""
}

func firefoxApplicationINIs() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"/Applications/Firefox.app/Contents/Resources/application.ini"}
	case "windows":
		var out []string
		for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
			if root != "" {
				out = append(out, filepath.Join(root, "Mozilla Firefox", "application.ini"))
			}
		}
		return out
	default:
		return []string{"/usr/lib/firefox/application.ini", "/usr/lib64/firefox/application.ini", "/opt/firefox/application.ini"}
	}
}

// iniValueVersion returns "<major>.<minor>" from key=value in an ini file.
func iniValueVersion(path, key string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(k), key) {
			continue
		}
		if m := firefoxVersionRe.FindStringSubmatch(strings.TrimSpace(v)); m != nil {
			return m[1] + "." + m[2]
		}
	}
	return ""
}

// chromiumMajorRe matches the major version in a "Last Version" file
// ("156.0.8078.79").
var chromiumMajorRe = regexp.MustCompile(`^(\d+)\.`)

// chromiumUserDataVersion walks up from a Chromium cookie DB
// (<User Data>/<Profile>/[Network/]Cookies) to the "Last Version" file in the
// User Data directory and returns the major version, or "".
func chromiumUserDataVersion(cookiePath string) string {
	dir := filepath.Dir(cookiePath)
	for i := 0; i < 4 && dir != "" && dir != filepath.Dir(dir); i++ {
		if b, err := os.ReadFile(filepath.Join(dir, "Last Version")); err == nil {
			if m := chromiumMajorRe.FindStringSubmatch(strings.TrimSpace(string(b))); m != nil {
				return m[1]
			}
		}
		dir = filepath.Dir(dir)
	}
	return ""
}

// firefoxUserAgent builds Firefox's UA for version "159.0" on this OS.
func firefoxUserAgent(version string) string {
	return "Mozilla/5.0 (" + firefoxPlatform() + "; rv:" + version + ") Gecko/20100101 Firefox/" + version
}

func firefoxPlatform() string {
	switch runtime.GOOS {
	case "windows":
		return "Windows NT 10.0; Win64; x64"
	case "darwin":
		return "Macintosh; Intel Mac OS X 10.15"
	default:
		return "X11; Linux x86_64"
	}
}

// chromiumUserAgent builds the reduced Chromium UA (minor versions frozen at
// 0.0.0) for a major version; Edge appends its own token. Brave sends the
// plain Chrome UA.
func chromiumUserAgent(major string, edge bool) string {
	var platform string
	switch runtime.GOOS {
	case "windows":
		platform = "Windows NT 10.0; Win64; x64"
	case "darwin":
		platform = "Macintosh; Intel Mac OS X 10_15_7"
	default:
		platform = "X11; Linux x86_64"
	}
	ua := "Mozilla/5.0 (" + platform + ") AppleWebKit/537.36 (KHTML, like Gecko) Chrome/" + major + ".0.0.0 Safari/537.36"
	if edge {
		ua += " Edg/" + major + ".0.0.0"
	}
	return ua
}

// FirefoxUserAgent is the UA a Firefox-family browser of the given version
// ("159.0") sends on this OS. Exposed for clearance minters that launch a
// Firefox build and must report the UA its clearance is bound to.
func FirefoxUserAgent(version string) string { return firefoxUserAgent(version) }
