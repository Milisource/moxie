package browserresolve

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/mili/moxie/internal/browser"
	"github.com/mili/moxie/internal/config"
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
// browser), then Chrome-family, then Firefox.
func selectEngine(o *Options, rawURL string) (engine, error) {
	switch effectiveBrowserMode(o.Browser) {
	case BrowserChrome:
		return newRodEngine(*o), nil
	case BrowserFirefox:
		return newFirefoxEngine(*o), nil
	}
	// Auto: F95Zone masked URLs need click automation — the reCAPTCHA-
	// gated "Continue" interstitial (F95Zone answers every unwrap POST
	// with {"status":"captcha"}) cannot be passed by raw-launch Firefox.
	// Prefer the rod engine: it clicks, and session cookies are injected
	// from every browser store regardless of which browser holds them.
	if strings.Contains(rawURL, "/masked/") {
		if _, err := detectBrowserBinary(); err == nil {
			return newRodEngine(*o), nil
		}
		// No Chrome-family binary: fall through — the cookie-holder or
		// Firefox paths below will fail the masked flow without clicks,
		// but nothing better is available on this machine.
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
		// unwrap indefinitely.
		solveCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		return ResolveMaskedURL(solveCtx, maskedURL)
	})
	log.Info("browserresolve: browser fallback installed (challenge-graded downloads + masked-URL solver)", "mode", mode)
	return true, ""
}
