package f95session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/mili/moxie/internal/browser"
	"github.com/mili/moxie/internal/log"
	_ "github.com/ncruces/go-sqlite3/driver"
)

// The minter launches a Firefox-family browser headless on a throwaway
// profile, lets it pass F95Zone's Cloudflare check on its own, and reads the
// resulting cf_clearance out of the profile's cookies.sqlite. No automation
// protocol is involved — the browser is just pointed at the site.
//
// Live-verified 2026-11-01 (Linux): Camoufox 156 minted a clearance in ~5 s
// and stock Firefox 159 in ~4 s, both headless; Go net/http then got 200 with
// that clearance + the browser's UA + the user's own xf_* login cookies, and
// 403 with any other UA. Meanwhile rod-driven headless/headful Chrome stayed
// on "Just a moment..." for 2 minutes. Camoufox is preferred (it is built to
// keep passing as bot detection tightens); the system Firefox is the
// fallback so no extra download is required.

// mintTimeout bounds one mint attempt.
const mintTimeout = 45 * time.Second

// mintTarget is the page loaded to earn the clearance.
const mintTarget = "https://f95zone.to/"

// candidate is a browser binary able to mint a clearance.
type candidate struct {
	name     string // "camoufox" or "firefox"
	bin      string
	version  string // "159.0"
	camoufox bool
}

// Mint obtains a fresh cf_clearance for f95zone.to with the first browser
// that succeeds (Camoufox, then the system Firefox).
func Mint(ctx context.Context) (browser.F95Clearance, error) {
	cands := mintCandidates()
	if len(cands) == 0 {
		return browser.F95Clearance{}, errors.New("no Camoufox or Firefox install found to pass the F95Zone Cloudflare check; install Firefox or Camoufox, or open f95zone.to in your browser")
	}
	var errs []string
	for _, c := range cands {
		start := time.Now()
		cl, err := mintWith(ctx, c)
		if err == nil {
			log.Info("F95Zone clearance minted", "browser", c.name, "version", c.version, "took", time.Since(start).Round(100*time.Millisecond))
			return cl, nil
		}
		log.Warn("F95Zone clearance mint failed", "browser", c.name, "error", err)
		errs = append(errs, c.name+": "+err.Error())
		if ctx.Err() != nil {
			break
		}
	}
	return browser.F95Clearance{}, fmt.Errorf("could not pass the F95Zone Cloudflare check (%s)", strings.Join(errs, "; "))
}

// mintWith runs one browser until cookies.sqlite holds a cf_clearance.
func mintWith(ctx context.Context, c candidate) (browser.F95Clearance, error) {
	ctx, cancel := context.WithTimeout(ctx, mintTimeout)
	defer cancel()

	profile, err := os.MkdirTemp("", "moxie-clearance-*")
	if err != nil {
		return browser.F95Clearance{}, err
	}
	defer os.RemoveAll(profile)
	if err := writeMintPrefs(profile); err != nil {
		return browser.F95Clearance{}, err
	}

	ua := browser.FirefoxUserAgent(c.version)
	cmd := exec.Command(c.bin, "--headless", "--no-remote", "--profile", profile, mintTarget)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	cmd.Env = os.Environ()
	if c.camoufox {
		// Camoufox takes its fingerprint from CAMOU_CONFIG_<n>. Pin the UA
		// (and the navigator fields that must agree with it) to the real
		// OS so the clearance is bound to a UA moxie knows.
		cfg, _ := json.Marshal(map[string]string{
			"navigator.userAgent": ua,
			"navigator.platform":  navigatorPlatform(),
			"navigator.oscpu":     navigatorOSCPU(),
		})
		cmd.Env = append(cmd.Env, "CAMOU_CONFIG_1="+string(cfg))
	}
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return browser.F95Clearance{}, fmt.Errorf("launch %s: %w", c.bin, err)
	}
	defer killTree(cmd)

	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return browser.F95Clearance{}, fmt.Errorf("no clearance after %s", mintTimeout)
		case <-tick.C:
		}
		value, expires, ok := readClearance(profile)
		if !ok {
			continue
		}
		return browser.F95Clearance{Value: value, UserAgent: ua, Created: time.Now(), Expires: expires}, nil
	}
}

// writeMintPrefs keeps the throwaway profile quiet (no first-run pages,
// telemetry, or update checks competing with the target page).
func writeMintPrefs(profile string) error {
	prefs := []string{
		`user_pref("browser.shell.checkDefaultBrowser", false);`,
		`user_pref("browser.startup.homepage_override.mstone", "ignore");`,
		`user_pref("datareporting.policy.dataSubmissionEnabled", false);`,
		`user_pref("toolkit.telemetry.reportingpolicy.firstRun", false);`,
		`user_pref("app.update.enabled", false);`,
		`user_pref("app.update.auto", false);`,
		`user_pref("browser.aboutwelcome.enabled", false);`,
	}
	return os.WriteFile(filepath.Join(profile, "user.js"), []byte(strings.Join(prefs, "\n")+"\n"), 0o600)
}

// readClearance copies the live cookie DB (and its WAL, where fresh writes
// sit) aside and reads cf_clearance for f95zone.to from the copy.
func readClearance(profile string) (value string, expires time.Time, ok bool) {
	src := filepath.Join(profile, "cookies.sqlite")
	if _, err := os.Stat(src); err != nil {
		return "", time.Time{}, false
	}
	tmp, err := os.MkdirTemp("", "moxie-clearance-db-*")
	if err != nil {
		return "", time.Time{}, false
	}
	defer os.RemoveAll(tmp)
	dst := filepath.Join(tmp, "cookies.sqlite")
	if copyFile(src, dst) != nil {
		return "", time.Time{}, false
	}
	_ = copyFile(src+"-wal", dst+"-wal")

	db, err := sql.Open("sqlite3", "file:"+dst)
	if err != nil {
		return "", time.Time{}, false
	}
	defer db.Close()
	var expiry int64
	err = db.QueryRow(`SELECT value, expiry FROM moz_cookies
		WHERE name = 'cf_clearance' AND (host = 'f95zone.to' OR host = '.f95zone.to')
		ORDER BY creationTime DESC LIMIT 1`).Scan(&value, &expiry)
	if err != nil || value == "" {
		return "", time.Time{}, false
	}
	return value, expiryTime(expiry), true
}

// expiryTime converts moz_cookies.expiry, which is seconds in older Firefox
// and milliseconds in newer builds.
func expiryTime(v int64) time.Time {
	switch {
	case v <= 0:
		return time.Time{}
	case v > 1e11:
		return time.UnixMilli(v)
	default:
		return time.Unix(v, 0)
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// mintCandidates lists usable browsers, Camoufox first. MOXIE_CAMOUFOX_BIN
// points at a Camoufox binary outside the standard cache location.
func mintCandidates() []candidate {
	var out []candidate
	camou := os.Getenv("MOXIE_CAMOUFOX_BIN")
	if camou == "" {
		camou = findCamoufox()
	}
	if camou != "" {
		if v := binaryVersion(camou); v != "" {
			out = append(out, candidate{name: "camoufox", bin: camou, version: v, camoufox: true})
		}
	}
	for _, bin := range firefoxBinaries() {
		if v := binaryVersion(bin); v != "" {
			out = append(out, candidate{name: "firefox", bin: bin, version: v})
			break
		}
	}
	return out
}

// camoufoxCacheDir is where the camoufox installer ("camoufox fetch") puts
// its browsers (platformdirs user_cache_dir("camoufox")).
func camoufoxCacheDir() string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Caches", "camoufox")
	case "windows":
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "camoufox", "camoufox", "Cache")
		}
		return filepath.Join(home, "AppData", "Local", "camoufox", "camoufox", "Cache")
	default:
		if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
			return filepath.Join(d, "camoufox")
		}
		return filepath.Join(home, ".cache", "camoufox")
	}
}

func camoufoxExe(dir string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(dir, "Camoufox.app", "Contents", "MacOS", "camoufox")
	case "windows":
		return filepath.Join(dir, "camoufox.exe")
	default:
		return filepath.Join(dir, "camoufox")
	}
}

// findCamoufox locates the installed Camoufox: the active version named in
// the cache's config.json, any installed version, or the older flat layout.
func findCamoufox() string {
	root := camoufoxCacheDir()
	var dirs []string
	if b, err := os.ReadFile(filepath.Join(root, "config.json")); err == nil {
		var cfg struct {
			ActiveVersion string `json:"active_version"`
		}
		if json.Unmarshal(b, &cfg) == nil && cfg.ActiveVersion != "" {
			dirs = append(dirs, filepath.Join(root, filepath.FromSlash(cfg.ActiveVersion)))
		}
	}
	if matches, _ := filepath.Glob(filepath.Join(root, "browsers", "*", "*")); len(matches) > 0 {
		for i := len(matches) - 1; i >= 0; i-- { // lexically newest first
			dirs = append(dirs, matches[i])
		}
	}
	dirs = append(dirs, root)
	for _, d := range dirs {
		exe := camoufoxExe(d)
		if fi, err := os.Stat(exe); err == nil && !fi.IsDir() {
			return exe
		}
	}
	return ""
}

func firefoxBinaries() []string {
	var out []string
	for _, name := range []string{"firefox", "firefox-esr"} {
		if p, err := exec.LookPath(name); err == nil {
			out = append(out, p)
		}
	}
	switch runtime.GOOS {
	case "darwin":
		out = append(out, "/Applications/Firefox.app/Contents/MacOS/firefox")
	case "windows":
		for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
			if root != "" {
				out = append(out, filepath.Join(root, "Mozilla Firefox", "firefox.exe"))
			}
		}
	}
	var existing []string
	for _, p := range out {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			existing = append(existing, p)
		}
	}
	return existing
}

var (
	iniVersionRe = regexp.MustCompile(`(?m)^(?:Milestone|Version)=(\d+)\.(\d+)`)
	cliVersionRe = regexp.MustCompile(`(\d+)\.(\d+)`)
)

// binaryVersion returns "<major>.<minor>" for a Firefox-family binary, read
// from platform.ini/application.ini beside it, else from --version.
func binaryVersion(bin string) string {
	real, err := filepath.EvalSymlinks(bin)
	if err != nil {
		real = bin
	}
	dir := filepath.Dir(real)
	for _, ini := range []string{
		filepath.Join(dir, "application.ini"),
		filepath.Join(dir, "platform.ini"),
		filepath.Join(dir, "..", "Resources", "application.ini"), // macOS bundle
	} {
		if b, err := os.ReadFile(ini); err == nil {
			if m := iniVersionRe.FindSubmatch(b); m != nil {
				return string(m[1]) + "." + string(m[2])
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return ""
	}
	if m := cliVersionRe.FindStringSubmatch(string(out)); m != nil {
		return m[1] + "." + m[2]
	}
	return ""
}

func navigatorPlatform() string {
	switch runtime.GOOS {
	case "windows":
		return "Win32"
	case "darwin":
		return "MacIntel"
	default:
		return "Linux x86_64"
	}
}

func navigatorOSCPU() string {
	switch runtime.GOOS {
	case "windows":
		return "Windows NT 10.0; Win64; x64"
	case "darwin":
		return "Intel Mac OS X 10.15"
	default:
		return "Linux x86_64"
	}
}
