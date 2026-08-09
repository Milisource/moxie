package browserresolve

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/mili/moxie/internal/browser"
	"github.com/mili/moxie/internal/downloader"
	"github.com/mili/moxie/internal/log"
)

// Browser engine families. The default is auto-selection.
const (
	BrowserAuto    = "auto"   // per-URL cookie-holder, then Chrome, then Firefox
	BrowserChrome  = "chrome" // rod + CDP engine (Chrome/Chromium/Edge/Brave)
	BrowserFirefox = "firefox" // raw-launch engine (zero deps)
)

// browserEnvVar opts into / forces the browser family:
// MOXIE_BROWSER=auto|chrome|firefox (1/true = auto).
const browserEnvVar = "MOXIE_BROWSER"

// chromeBrowserNames are the kooky store names that map to the rod engine.
var chromeBrowserNames = map[string]bool{"chrome": true, "chromium": true, "edge": true, "brave": true}

// cookieBrowsersForHost reports which browsers hold cookies for a host;
// injectable so tests can drive selection without a real kooky store.
var cookieBrowsersForHost = browser.GetCookieBrowsersForHost

// selectEngine picks the engine for a session: an explicit WithBrowser
// override wins; otherwise MOXIE_BROWSER; otherwise the browser whose store
// holds cookies for the target host (a clearance only matches its minting
// browser), then Chrome-family, then Firefox.
func selectEngine(o *Options, rawURL string) (engine, error) {
	switch effectiveBrowserMode(o.Browser) {
	case BrowserChrome:
		return newRodEngine(*o), nil
	case BrowserFirefox:
		return newFirefoxEngine(*o), nil
	}
	// Auto: the cookie-holder for this host is the guaranteed fingerprint
	// match; prefer it over any generic availability.
	if host := urlHostname(rawURL); host != "" {
		for _, name := range cookieBrowsersForHost(host) {
			if chromeBrowserNames[name] {
				return newRodEngine(*o), nil
			}
			if name == "firefox" {
				return newFirefoxEngine(*o), nil
			}
		}
	}
	if _, err := detectBrowserBinary(); err == nil {
		return newRodEngine(*o), nil
	}
	if _, err := detectFirefoxBinary(); err == nil {
		return newFirefoxEngine(*o), nil
	}
	return nil, fmt.Errorf("%w: no Chrome/Chromium or Firefox binary found", ErrNoBrowser)
}

// effectiveBrowserMode resolves the mode option against the MOXIE_BROWSER
// environment variable. "" and "1"/"true" both mean auto.
func effectiveBrowserMode(mode string) string {
	if mode == "" || mode == BrowserAuto {
		mode = strings.ToLower(strings.TrimSpace(os.Getenv(browserEnvVar)))
	}
	switch mode {
	case "", "1", "true", BrowserAuto:
		return BrowserAuto
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
// path stays primary. MOXIE_BROWSER=auto|chrome|firefox forces an engine
// family (1/true = auto); unset auto-detects. The installed hook picks the
// engine per URL (the cookie-holding browser first), so a Firefox user on a
// Chrome box gets the right engine for each host.
//
// Returns installed=false with a reason when no browser is available or the
// MOXIE_BROWSER value is invalid.
func InstallDownloaderFallback() (installed bool, reason string) {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv(browserEnvVar)))
	switch mode {
	case "", "0", "false", "off":
		// Auto-detect: install when any browser exists.
	case "1", "true", BrowserAuto, BrowserChrome, BrowserFirefox:
	default:
		return false, fmt.Sprintf("invalid %s value %q (want auto|chrome|firefox)", browserEnvVar, mode)
	}
	if ok, why := browserAvailable(mode); !ok {
		log.Debug("browserresolve: fallback not installed", "reason", why)
		return false, why
	}
	downloader.SetDefaultBrowserFallback(func(ctx context.Context, rawURL, destDir string) (string, error) {
		return ResolveDownload(ctx, rawURL, destDir)
	})
	log.Info("browserresolve: browser fallback installed (challenge-graded downloads)", "mode", effectiveBrowserMode(mode))
	return true, ""
}
