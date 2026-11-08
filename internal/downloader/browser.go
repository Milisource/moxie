package downloader

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/mili/moxie/internal/log"
)

// browserResolverEnabled is opt-in: launching a visible browser is disruptive
// and only useful for hosts that gate downloads behind Turnstile/reCAPTCHA.
var browserResolverEnabled atomic.Bool

// SetBrowserResolver enables or disables the optional browser-assisted
// resolver. It is enabled by `moxie download --browser` or MOXIE_BROWSER=1.
func SetBrowserResolver(enabled bool) {
	browserResolverEnabled.Store(enabled)
}

// BrowserResolverEnabled reports whether the browser resolver is enabled.
func BrowserResolverEnabled() bool {
	return browserResolverEnabled.Load()
}

// resolveVikingFileBrowser drives a real (non-headless) Chromium through CDP.
// VikingFile's Turnstile challenge only issues a token to a headed browser; once
// solved, the page fills #download-link with a /d/<id>/<filename> URL that
// redirects to a presigned S3 object. The caller then downloads that URL with
// ordinary HTTP (including Range/resume).
func (r *HostResolver) resolveVikingFileBrowser(rawURL string) (*ResolveResult, error) {
	execPath, err := findBrowserExecutable()
	if err != nil {
		return nil, err
	}
	profileDir, err := os.UserCacheDir()
	if err != nil {
		profileDir = os.TempDir()
	}
	profileDir = filepath.Join(profileDir, "moxie", "browser-profile")
	if err := os.MkdirAll(profileDir, 0700); err != nil {
		return nil, fmt.Errorf("browser resolver: create profile dir: %w", err)
	}

	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts,
		chromedp.ExecPath(execPath),
		chromedp.Flag("headless", false),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("no-first-run", true),
		chromedp.UserDataDir(profileDir),
		chromedp.WindowSize(1366, 900),
	)

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancelAlloc()
	ctx, cancelCtx := chromedp.NewContext(allocCtx)
	defer cancelCtx()
	ctx, cancelTimeout := context.WithTimeout(ctx, 120*time.Second)
	defer cancelTimeout()

	log.Info("browser resolver starting", "host", "vikingfile", "browser", filepath.Base(execPath))
	if err := chromedp.Do(ctx, chromedp.Navigate(rawURL)); err != nil {
		return nil, fmt.Errorf("browser resolver: navigate: %w", err)
	}

	// Wait up to 100s for Turnstile to solve and the site's JS to set the href.
	deadline := time.Now().Add(100 * time.Second)
	var href string
	for time.Now().Before(deadline) {
		got, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(() => {
			const el = document.getElementById('download-link');
			if (!el) return '';
			const href = el.href || '';
			return href && href !== location.href ? href : '';
		})()`))
		if err != nil {
			return nil, fmt.Errorf("browser resolver: read download link: %w", err)
		}
		if got != "" {
			href = got
			break
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("browser resolver: timed out waiting for Turnstile")
		case <-time.After(time.Second):
		}
	}
	if href == "" {
		return nil, fmt.Errorf("browser resolver: Turnstile did not produce a download link within 100s")
	}
	if !strings.HasPrefix(href, "https://") {
		return nil, fmt.Errorf("browser resolver: unexpected download link %q", href)
	}

	log.Info("browser resolver obtained direct link", "host", "vikingfile", "target_host", strings.Split(strings.TrimPrefix(href, "https://"), "/")[0])
	return &ResolveResult{
		URL: href,
		Headers: map[string]string{
			"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
			"Referer":    rawURL,
		},
	}, nil
}

// findBrowserExecutable locates a Chromium-family browser suitable for a
// headed Turnstile session. MOXIE_BROWSER overrides everything.
func findBrowserExecutable() (string, error) {
	if p := strings.TrimSpace(os.Getenv("MOXIE_BROWSER")); p != "" &&
		p != "1" && !strings.EqualFold(p, "true") && !strings.EqualFold(p, "yes") {
		if _, err := os.Stat(p); err == nil || !strings.ContainsRune(p, filepath.Separator) {
			return p, nil
		}
		return "", fmt.Errorf("browser resolver: MOXIE_BROWSER=%q not found", p)
	}
	for _, name := range []string{
		"google-chrome", "google-chrome-stable", "chromium", "chromium-browser",
		"brave-browser", "microsoft-edge", "microsoft-edge-stable",
	} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	home, err := os.UserHomeDir()
	if err == nil {
		matches, _ := filepath.Glob(filepath.Join(home, ".cache", "ms-playwright", "chromium-*", "chrome-linux*", "chrome"))
		if len(matches) > 0 {
			sort.Strings(matches)
			return matches[len(matches)-1], nil
		}
	}
	return "", fmt.Errorf("browser resolver: no Chromium-family browser found (set MOXIE_BROWSER=/path/to/chrome)")
}
