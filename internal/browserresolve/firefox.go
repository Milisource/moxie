package browserresolve

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mili/moxie/internal/log"
)

// firefoxEnvBin / firefoxEnvProfile override Firefox discovery, mirroring
// MOXIE_CHROME_PROFILE_DIR and WithBinPath for the Chrome-family engine.
const (
	firefoxEnvBin     = "MOXIE_FIREFOX_BIN"
	firefoxEnvProfile = "MOXIE_FIREFOX_PROFILE_DIR"
)

// firefoxBinaryCandidates are the executables searched on PATH; per-OS
// install locations are probed in detectFirefoxBinary when none match.
var firefoxBinaryCandidates = []string{"firefox", "firefox-esr"}

// firefoxProfileRoots returns the per-OS Firefox profile root (the
// directory containing profiles.ini).
func firefoxProfileRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	switch runtime.GOOS {
	case "darwin":
		if home != "" {
			return []string{filepath.Join(home, "Library", "Application Support", "Firefox")}
		}
	case "windows":
		var roots []string
		if appData := os.Getenv("APPDATA"); appData != "" {
			roots = append(roots, filepath.Join(appData, "Mozilla", "Firefox"))
		}
		return roots
	default:
		if home != "" {
			return []string{filepath.Join(home, ".mozilla", "firefox")}
		}
	}
	return nil
}

// detectFirefoxBinary returns the Firefox executable: the MOXIE_FIREFOX_BIN
// override, a PATH match, or a per-OS standard install location.
func detectFirefoxBinary() (string, error) {
	for _, name := range firefoxBinaryCandidates {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	switch runtime.GOOS {
	case "darwin":
		for _, base := range []string{"/Applications", filepath.Join(home, "Applications")} {
			candidate := filepath.Join(base, "Firefox.app", "Contents", "MacOS", "firefox")
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
		}
	case "windows":
		for _, root := range []string{
			os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LOCALAPPDATA"),
		} {
			if root == "" {
				continue
			}
			candidate := filepath.Join(root, "Mozilla Firefox", "firefox.exe")
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
		}
	}
	return "", exec.ErrNotFound
}

// discoverFirefoxProfileDir locates the user's Firefox profile: an explicit
// override (a profile dir, or a root containing profiles.ini), then
// MOXIE_FIREFOX_PROFILE_DIR, then the per-OS roots. The Default=1 profile
// from profiles.ini wins; the first IsRelative=1 profile is the fallback.
func discoverFirefoxProfileDir(override string) (string, error) {
	if override != "" {
		dir, err := resolveFirefoxProfile(override)
		if err != nil {
			return "", fmt.Errorf("%w: %q: %v", ErrNoProfile, override, err)
		}
		return dir, nil
	}
	if env := os.Getenv(firefoxEnvProfile); env != "" {
		dir, err := resolveFirefoxProfile(env)
		if err != nil {
			return "", fmt.Errorf("%w: %q: %v", ErrNoProfile, env, err)
		}
		return dir, nil
	}
	for _, root := range firefoxProfileRoots() {
		if dir, err := resolveFirefoxProfile(root); err == nil {
			return dir, nil
		}
	}
	return "", fmt.Errorf("%w: no Firefox profile found", ErrNoProfile)
}

// resolveFirefoxProfile accepts either a profile dir (contains
// cookies.sqlite or prefs.js) or a root with profiles.ini, and returns the
// concrete profile dir.
func resolveFirefoxProfile(dir string) (string, error) {
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return "", fmt.Errorf("not a directory")
	}
	// Direct profile dir?
	if _, err := os.Stat(filepath.Join(dir, "cookies.sqlite")); err == nil {
		return dir, nil
	}
	if _, err := os.Stat(filepath.Join(dir, "prefs.js")); err == nil {
		return dir, nil
	}
	ini := filepath.Join(dir, "profiles.ini")
	path, err := parseFirefoxProfilesINI(ini)
	if err != nil {
		return "", err
	}
	profile := path
	if !filepath.IsAbs(profile) {
		profile = filepath.Join(dir, profile)
	}
	if st, err := os.Stat(profile); err != nil || !st.IsDir() {
		return "", fmt.Errorf("profile %q from profiles.ini does not exist", path)
	}
	return profile, nil
}

// parseFirefoxProfilesINI selects the profile directory from a profiles.ini
// file: in [Install...] sections Default=<path> names the default profile
// directly; in [ProfileN] sections Default=1 marks the section's Path as the
// default. The first section with a Path entry is the fallback when neither
// applies. Returns the (possibly relative) Path value of the chosen section.
func parseFirefoxProfilesINI(iniPath string) (string, error) {
	data, err := os.ReadFile(iniPath)
	if err != nil {
		return "", fmt.Errorf("read profiles.ini: %w", err)
	}
	var fallback, def string
	curPath, curDefault, curSection := "", false, ""
	finish := func() {
		if curPath == "" {
			return
		}
		if curDefault && def == "" {
			def = curPath
		}
		if fallback == "" {
			fallback = curPath
		}
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			finish()
			curSection = strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			curPath, curDefault = "", false
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		switch key {
		case "Path":
			curPath = val
		case "Default":
			if strings.HasPrefix(curSection, "install") {
				// [InstallXXXX] Default=<path> is the default profile path
				// itself, not a flag.
				if val != "" && def == "" {
					def = val
				}
			} else {
				curDefault = strings.EqualFold(val, "1")
			}
		}
	}
	finish()
	if def != "" {
		return def, nil
	}
	if fallback != "" {
		return fallback, nil
	}
	return "", fmt.Errorf("no profile entries in %s", iniPath)
}

// firefoxEngine downloads via raw-launched Firefox on a copied profile:
// zero CDP, zero new deps. Download prefs are injected through user.js, the
// browser is launched with -profile <copy> -no-remote (never -private —
// private mode changes the TLS fingerprint), and the finished file is found
// by polling the download dir (.part suffixes ignored). Headless first; if
// the session produced an HTML challenge page or no file, it retries once
// headful (xvfb-run -a on display-less Linux).
type firefoxEngine struct {
	opts Options
}

func newFirefoxEngine(opts Options) *firefoxEngine { return &firefoxEngine{opts: opts} }

// profileDir locates the Firefox profile (the dir containing cookies.sqlite,
// chosen via profiles.ini Default=1).
func (e *firefoxEngine) profileDir(override string) (string, error) {
	return discoverFirefoxProfileDir(override)
}

func (e *firefoxEngine) run(ctx context.Context, req engineRequest) (engineResult, error) {
	bin := e.opts.BinPath
	if bin == "" {
		bin = os.Getenv(firefoxEnvBin)
	}
	if bin == "" {
		if p, err := detectFirefoxBinary(); err == nil {
			bin = p
		}
	}
	if bin == "" {
		return engineResult{}, fmt.Errorf("%w: install Firefox or set %s", ErrNoBrowser, firefoxEnvBin)
	}

	if err := writeFirefoxUserJS(req.profileDir, req.downloadDir); err != nil {
		return engineResult{}, err
	}

	res, err := e.runOnce(ctx, bin, req, false)
	if err != nil && !e.opts.Headful && ctx.Err() == nil {
		// Escalate: the challenge refused the headless fingerprint, or no
		// file arrived within the timeout. A visible window (or xvfb on a
		// display-less Linux) passes managed challenges headless cannot.
		log.Info("browserresolve: firefox headless session failed, escalating to headful", "error", err)
		if res2, err2 := e.runOnce(ctx, bin, req, true); err2 == nil {
			return res2, nil
		}
	}
	return res, err
}

// runOnce launches one Firefox session (headless or headful) against the
// copied profile and waits for a finished file in the download dir.
func (e *firefoxEngine) runOnce(ctx context.Context, bin string, req engineRequest, headful bool) (engineResult, error) {
	args := []string{"-profile", req.profileDir, "-no-remote"}
	if !headful {
		args = append(args, "-headless")
	}
	args = append(args, req.url)

	cmd := exec.CommandContext(ctx, bin, args...)
	// Headful escalation on a display-less Linux needs a virtual display.
	if headful && runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" {
		if _, err := exec.LookPath("xvfb-run"); err != nil {
			return engineResult{}, fmt.Errorf("browserresolve: headful escalation needs xvfb-run on display-less Linux: %w", err)
		}
		cmd = exec.CommandContext(ctx, "xvfb-run", append([]string{"-a", bin}, args...)...)
	}
	setProcessGroup(cmd)
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return engineResult{}, fmt.Errorf("%w: %v", ErrNoBrowser, err)
		}
		return engineResult{}, fmt.Errorf("browserresolve: launching Firefox: %w", err)
	}
	defer killTree(cmd)

	// Wait in the background so the poll loop can also watch for an early
	// browser exit (crash at startup) and fail fast instead of idling out
	// the full download timeout.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	timeout := e.opts.DownloadTimeout
	if timeout <= 0 {
		timeout = defaultDownloadTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	fileCh := make(chan string, 1)
	pollCtx, pollCancel := context.WithCancel(ctx)
	defer pollCancel()
	go pollDownloadDir(pollCtx, req.downloadDir, pollInterval, fileCh)

	select {
	case <-ctx.Done():
		return engineResult{}, fmt.Errorf("browserresolve: firefox download: %w", ctx.Err())
	case path := <-fileCh:
		return engineResult{path: path, suggested: filepath.Base(path)}, nil
	case <-timer.C:
		return engineResult{}, fmt.Errorf("%w: no file within %s (firefox %s)", ErrDownload, timeout, headfulMode(headful))
	case err := <-done:
		// The browser exited on its own — the file may still have landed
		// just before (headless Firefox exits after finishing a download
		// in some configurations).
		select {
		case path := <-fileCh:
			return engineResult{path: path, suggested: filepath.Base(path)}, nil
		default:
			if err != nil {
				return engineResult{}, fmt.Errorf("browserresolve: firefox exited without a download: %w", err)
			}
			return engineResult{}, fmt.Errorf("browserresolve: firefox exited without a download")
		}
	}
}

func headfulMode(headful bool) string {
	if headful {
		return "headful"
	}
	return "headless"
}

// writeFirefoxUserJS injects the download prefs into the profile copy via
// user.js (applied at startup, wins over prefs.js). Windows paths must use
// forward slashes — backslashes are escape characters in prefs files.
func writeFirefoxUserJS(profileDir, downloadDir string) error {
	dir := filepath.ToSlash(downloadDir)
	prefs := fmt.Sprintf(`user_pref("browser.download.folderList", 2);
user_pref("browser.download.dir", "%s");
user_pref("browser.download.useDownloadDir", true);
user_pref("browser.download.manager.showWhenStarting", false);
user_pref("browser.helperApps.neverAsk.saveToDisk", "application/zip,application/x-zip-compressed,application/octet-stream,application/x-7z-compressed,application/x-rar-compressed,application/gzip,application/x-tar");
user_pref("browser.download.always_ask_before_handling_new_types", false);
user_pref("browser.startup.page", 0);
user_pref("browser.sessionstore.resume_from_crash", false);
`, escapePrefString(dir))
	if err := os.WriteFile(filepath.Join(profileDir, "user.js"), []byte(prefs), 0o600); err != nil {
		return fmt.Errorf("browserresolve: writing firefox user.js: %w", err)
	}
	return nil
}

// escapePrefString escapes a value for embedding in a prefs file (backslash
// and double quote are the only escapes prefs files honor).
func escapePrefString(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}
