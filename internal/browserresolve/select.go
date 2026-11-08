package browserresolve

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/mili/moxie/internal/browser"
	"github.com/mili/moxie/internal/config"
	"github.com/mili/moxie/internal/downloader"
	"github.com/mili/moxie/internal/log"
)

// Browser engine families. The default is auto-selection.
const (
	BrowserAuto    = "auto"    // per-URL cookie-holder, then Chrome, then Firefox
	BrowserChrome  = "chrome"  // rod + CDP engine (Chrome/Chromium/Edge/Brave)
	BrowserFirefox = "firefox" // raw-launch engine (zero deps)
)

// browserEnvVar opts into / forces the browser family:
// MOXIE_BROWSER=auto|chrome|firefox|off (1/true = auto).
const browserEnvVar = "MOXIE_BROWSER"

// browserConfigKey is the persistent config.json key mirroring
// browserEnvVar (moxie config set browser_fallback auto). The env var wins
// when both are set.
const browserConfigKey = "browser_fallback"

// browserFallbackDisabledHint is appended to challenge errors when the
// browser fallback is not installed, so users discover the opt-in exactly
// when they need it.
const browserFallbackDisabledHint = "enable browser-assisted downloads with `moxie config set browser_fallback auto` or MOXIE_BROWSER=auto"

// chromeBrowserNames are the kooky store names that map to the rod engine.
var chromeBrowserNames = map[string]bool{"chrome": true, "chromium": true, "edge": true, "brave": true}

// cookieBrowsersForHost reports which browsers hold cookies for a host;
// injectable so tests can drive selection without a real kooky store.
var cookieBrowsersForHost = browser.GetCookieBrowsersForHost

// selectEngine picks the engine for a session: an explicit WithBrowser
// override wins; otherwise MOXIE_BROWSER; otherwise the browser whose store
// holds cookies for the target host (a clearance only matches its minting
// browser), then the user's default browser, then Chrome-family, then
// Firefox.
func selectEngine(o *Options, rawURL string) (engine, error) {
	switch effectiveBrowserMode(o.Browser) {
	case BrowserChrome:
		return newRodEngine(*o), nil
	case BrowserFirefox:
		return newFirefoxEngine(*o), nil
	}
	host := urlHostname(rawURL)
	// F95Zone masked URLs and /dl/ check pages need click automation — the
	// reCAPTCHA/hCaptcha interstitials cannot be passed by raw-launch Firefox.
	// The native /dl/ solver handles the no-captcha case over plain HTTP; this
	// engine choice only applies when that solver hands the page to a browser,
	// which is exactly when clicks are required. The rod engine also gets
	// session cookies injected from every browser store.
	if strings.Contains(rawURL, "/masked/") || strings.Contains(rawURL, "/dl/") {
		if eng, ok := clickCapableEngine(o); ok {
			return eng, nil
		}
		// No Chrome-family binary: fall through to anything that exists.
	}
	// Auto: the cookie-holder for this host is the guaranteed fingerprint
	// match; prefer it over any generic availability.
	if eng, ok := cookieHolderEngine(o, host); ok {
		return eng, nil
	}
	if eng, ok := defaultBrowserEngine(o); ok {
		return eng, nil
	}
	if _, err := detectBrowserBinary(); err == nil {
		return newRodEngine(*o), nil
	}
	if _, err := detectFirefoxBinary(); err == nil {
		return newFirefoxEngine(*o), nil
	}
	return nil, fmt.Errorf("%w: no Chrome/Chromium or Firefox binary found", ErrNoBrowser)
}

// clickCapableEngine returns the rod engine when a Chrome-family browser is
// installed.
func clickCapableEngine(o *Options) (engine, bool) {
	if _, err := detectBrowserBinary(); err == nil {
		return newRodEngine(*o), true
	}
	return nil, false
}

// cookieHolderEngine returns the engine for the first browser holding cookies
// for host, when that browser is installed.
func cookieHolderEngine(o *Options, host string) (engine, bool) {
	if host == "" {
		return nil, false
	}
	for _, name := range cookieBrowsersForHost(host) {
		if chromeBrowserNames[name] {
			if _, err := detectBrowserBinary(); err == nil {
				return newRodEngine(*o), true
			}
			continue
		}
		if name == "firefox" {
			if _, err := detectFirefoxBinary(); err == nil {
				return newFirefoxEngine(*o), true
			}
		}
	}
	return nil, false
}

// defaultBrowserEngine returns the engine for the user's default browser when
// Moxie supports it and its binary exists.
func defaultBrowserEngine(o *Options) (engine, bool) {
	switch detectDefaultBrowser() {
	case BrowserFirefox:
		if _, err := detectFirefoxBinary(); err == nil {
			return newFirefoxEngine(*o), true
		}
	case BrowserChrome:
		if _, err := detectBrowserBinary(); err == nil {
			return newRodEngine(*o), true
		}
	}
	return nil, false
}

// detectDefaultBrowser reports the user's default browser family
// (BrowserFirefox / BrowserChrome) or "" when it cannot be determined. A var
// so tests can drive selection without a desktop session.
var detectDefaultBrowser = defaultBrowserFamily

// defaultBrowserFamily queries the desktop environment for the default
// browser. $BROWSER wins when set, matching the convention most tools use.
func defaultBrowserFamily() string {
	if v := strings.TrimSpace(os.Getenv("BROWSER")); v != "" {
		for _, part := range strings.Split(v, string(os.PathListSeparator)) {
			if fam := browserFamilyFromName(part); fam != "" {
				return fam
			}
		}
	}
	switch runtime.GOOS {
	case "linux":
		out, err := exec.Command("xdg-settings", "get", "default-web-browser").Output()
		if err != nil {
			return ""
		}
		return browserFamilyFromName(string(out))
	case "darwin":
		out, err := exec.Command("defaults", "read",
			"com.apple.LaunchServices/com.apple.launchservices.secure", "LSHandlers").Output()
		if err != nil {
			return ""
		}
		return parseMacDefaultBrowser(string(out))
	case "windows":
		out, err := exec.Command("reg", "query",
			`HKCU\Software\Microsoft\Windows\Shell\Associations\UrlAssociations\http\UserChoice`, "/v", "ProgId").Output()
		if err != nil {
			return ""
		}
		return browserFamilyFromName(string(out))
	}
	return ""
}

// browserFamilyFromName maps a desktop-file id, binary path, or ProgId to an
// engine family.
func browserFamilyFromName(s string) string {
	l := strings.ToLower(s)
	switch {
	case strings.Contains(l, "firefox"):
		return BrowserFirefox
	case strings.Contains(l, "chrome"), strings.Contains(l, "chromium"),
		strings.Contains(l, "brave"), strings.Contains(l, "edge"),
		strings.Contains(l, "vivaldi"), strings.Contains(l, "opera"):
		return BrowserChrome
	}
	return ""
}

// parseMacDefaultBrowser extracts the http handler from the
// com.apple.launchservices.secure LSHandlers plist text.
func parseMacDefaultBrowser(plist string) string {
	lines := strings.Split(plist, "\n")
	inHTTP := false
	for _, line := range lines {
		if strings.Contains(line, "LSHandlerURLScheme") && strings.Contains(line, "http") {
			inHTTP = true
			continue
		}
		if inHTTP && strings.Contains(line, "LSHandlerRoleAll") {
			return browserFamilyFromName(line)
		}
	}
	return ""
}

// effectiveBrowserMode resolves the mode option against the MOXIE_BROWSER
// environment variable, then the browser_fallback config key. "" (unset)
// means auto-detect; "0"/"false"/"off" explicitly disables; unknown values
// are treated as disabled.
func effectiveBrowserMode(mode string) string {
	if mode == "" || mode == BrowserAuto {
		mode = strings.ToLower(strings.TrimSpace(os.Getenv(browserEnvVar)))
	}
	if mode == "" {
		if cfg, err := config.ReadConfig(); err == nil {
			mode = strings.ToLower(strings.TrimSpace(cfg.Get(browserConfigKey)))
		}
	}
	switch mode {
	case "", "1", "true", BrowserAuto:
		return BrowserAuto
	case "0", "false", "off":
		return ""
	case BrowserChrome:
		return BrowserChrome
	case BrowserFirefox:
		return BrowserFirefox
	default:
		return ""
	}
}

// urlHostname returns the hostname of a URL, or "" when unparsable.
func urlHostname(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Hostname()
}

// browserAvailable reports whether at least one engine has a usable binary.
func browserAvailable(mode string) (bool, string) {
	switch effectiveBrowserMode(mode) {
	case BrowserChrome:
		if _, err := detectBrowserBinary(); err != nil {
			return false, "no Chrome/Chromium binary found"
		}
		return true, ""
	case BrowserFirefox:
		if _, err := detectFirefoxBinary(); err != nil {
			return false, "no Firefox binary found"
		}
		return true, ""
	default:
		if _, err := detectBrowserBinary(); err == nil {
			return true, ""
		}
		if _, err := detectFirefoxBinary(); err == nil {
			return true, ""
		}
		return false, "no Chrome/Chromium or Firefox binary found"
	}
}

// InstallDownloaderFallback wires the downloader's browser fallback hook
// (challenge-graded hosts) to this package, if a usable browser exists.
//
// The hook fires only when the Go path hits a Cloudflare challenge — the Go
// path stays primary. Default is auto-detect: unset (or MOXIE_BROWSER=auto)
// installs when any supported browser is found; the env var or the
// browser_fallback config key can force an engine family or disable the
// feature entirely (off). The installed hook picks the engine per URL (the
// cookie-holding browser first), so a Firefox user on a Chrome box gets the
// right engine for each host. No browser is ever shipped or downloaded —
// both engines launch the user's own installed browser.
//
// Returns installed=false with a reason when the feature is disabled, no
// browser is available, or the value is invalid.
func InstallDownloaderFallback() (installed bool, reason string) {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(browserEnvVar)))
	if raw == "" {
		if cfg, err := config.ReadConfig(); err == nil {
			raw = strings.ToLower(strings.TrimSpace(cfg.Get(browserConfigKey)))
		}
	}
	switch raw {
	case "", "1", "true", BrowserAuto:
		// Auto-detect: install when any browser exists.
	case "0", "false", "off":
		return false, "disabled via " + browserEnvVar + " / " + browserConfigKey
	case BrowserChrome, BrowserFirefox:
	default:
		return false, fmt.Sprintf("invalid %s value %q (want auto|chrome|firefox|off)", browserEnvVar, raw)
	}
	mode := raw
	if mode == "" || mode == "1" || mode == "true" {
		mode = BrowserAuto
	}
	if ok, why := browserAvailable(mode); !ok {
		log.Debug("browserresolve: fallback not installed", "reason", why)
		return false, why
	}
	downloader.SetDefaultBrowserFallback(func(ctx context.Context, rawURL, destDir string) (string, error) {
		return ResolveDownload(ctx, rawURL, destDir)
	})
	downloader.SetDefaultMaskedSolver(func(ctx context.Context, maskedURL string) (string, error) {
		// The browser drives the masked interstitial (Continue click +
		// reCAPTCHA checkbox) and returns the destination; the Go path then
		// downloads it with progress/resume. Bound the session so a
		// stubborn wall (image challenge, login wall) cannot stall the
		// unwrap indefinitely — long enough for the visible-window
		// verification retry, the user passing the check, and a browser
		// download the visible session started itself.
		solveCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		defer cancel()
		return ResolveMaskedURL(solveCtx, maskedURL)
	})
	// F95Zone /dl/ download-link pages (2026-10) put an hCaptcha + script
	// check in front of every link; only a browser can pass it. The browser
	// returns the destination (host URL or /masked/ interstitial), which
	// the Go path then resolves/downloads. The 30 min bound covers the
	// headless attempt, the visible-window retry when Cloudflare wants a
	// human check, the user passing that check, and a browser download the
	// visible session started itself (multi-GB archives); the internal
	// gates still bound inactivity.
	downloader.SetDefaultDLSolver(func(ctx context.Context, dlURL string) (string, error) {
		solveCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		defer cancel()
		return ResolveDownloadLinkURL(solveCtx, dlURL)
	})
	// Turnstile-gated file pages (vikingfile.com) need the browser to clear
	// the widget and read the client-side link; the Go path then downloads the
	// revealed URL. Installed only when a click-capable Chrome-family binary
	// exists and Firefox was not explicitly forced.
	if mode != BrowserFirefox {
		if _, err := detectBrowserBinary(); err == nil {
			downloader.SetDefaultTurnstileSolver(func(ctx context.Context, pageURL string) (string, error) {
				solveCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
				defer cancel()
				return ResolveTurnstileURL(solveCtx, pageURL, WithBrowser(BrowserChrome))
			})
		}
	}
	log.Info("browserresolve: browser fallback installed (challenge-graded downloads + masked-URL solver)", "mode", mode)
	return true, ""
}
