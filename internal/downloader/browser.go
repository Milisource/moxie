package downloader

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
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

func browserHeaders(referer string) map[string]string {
	return map[string]string{
		"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
		"Referer":    referer,
	}
}

// runInBrowser launches a headed Chromium-family browser and runs fn against
// its CDP context. The browser is closed when fn returns.
func runInBrowser(host, profileKey string, timeout time.Duration, fn func(ctx context.Context) (string, error)) (string, error) {
	execPath, err := findBrowserExecutable()
	if err != nil {
		return "", err
	}
	profileDir, err := os.UserCacheDir()
	if err != nil {
		profileDir = os.TempDir()
	}
	profileDir = filepath.Join(profileDir, "moxie", "browser-profile-"+profileKey)
	if err := os.MkdirAll(profileDir, 0700); err != nil {
		return "", fmt.Errorf("browser resolver: create profile dir: %w", err)
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
	ctx, cancelTimeout := context.WithTimeout(ctx, timeout)
	defer cancelTimeout()

	log.Info("browser resolver starting", "host", host, "browser", filepath.Base(execPath))
	return fn(ctx)
}

// resolveVikingFileBrowser drives a real (non-headless) Chromium through CDP.
// VikingFile's Turnstile challenge only issues a token to a headed browser; once
// solved, the page fills #download-link with a /d/<id>/<filename> URL that
// redirects to a presigned S3 object. The caller then downloads that URL with
// ordinary HTTP (including Range/resume).
func (r *HostResolver) resolveVikingFileBrowser(rawURL string) (*ResolveResult, error) {
	// Reuse a previously browser-resolved link when fresh. VikingFile's /d/
	// URL redirects to a new presigned S3 URL, so caching it avoids relaunching
	// a browser on every retry/download.
	if store := getResolutionStore(); store != nil {
		if cached, _, ok := store.GetResolvedURL(rawURL, 12*time.Hour); ok && isValidDownloadURL(cached) {
			log.Debug("vikingfile browser cache hit", "url", rawURL)
			return &ResolveResult{URL: cached, Headers: browserHeaders(rawURL)}, nil
		}
	}

	href, err := runInBrowser("vikingfile", "vikingfile", 120*time.Second, func(ctx context.Context) (string, error) {
		if err := chromedp.Do(ctx, chromedp.Navigate(rawURL)); err != nil {
			return "", fmt.Errorf("browser resolver: navigate: %w", err)
		}
		// Wait up to 100s for Turnstile to solve and the site's JS to set the href.
		deadline := time.Now().Add(100 * time.Second)
		for time.Now().Before(deadline) {
			got, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(() => {
				const el = document.getElementById('download-link');
				if (!el) return '';
				const href = el.href || '';
				return href && href !== location.href ? href : '';
			})()`))
			if err != nil {
				return "", fmt.Errorf("browser resolver: read download link: %w", err)
			}
			if got != "" {
				return got, nil
			}
			select {
			case <-ctx.Done():
				return "", fmt.Errorf("browser resolver: timed out waiting for Turnstile")
			case <-time.After(time.Second):
			}
		}
		return "", fmt.Errorf("browser resolver: Turnstile did not produce a download link within 100s")
	})
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(href, "https://") {
		return nil, fmt.Errorf("browser resolver: unexpected download link %q", href)
	}

	log.Info("browser resolver obtained direct link", "host", "vikingfile", "target_host", strings.Split(strings.TrimPrefix(href, "https://"), "/")[0])
	cacheBrowserResolution(rawURL, href, "vikingfile")
	return &ResolveResult{URL: href, Headers: browserHeaders(rawURL)}, nil
}

// resolveDatanodesBrowser automates DataNodes' two-step free-download flow:
//  1. GET file page → click #method_free (op=download1)
//  2. step 2 renders an invisible Turnstile widget; once it issues a token,
//     click "Free Download".
//  3. the page requests nodeNN.datanodes.to:8443/d/<token>/<filename>, which is
//     a direct HTTP download URL with Range support.
func (r *HostResolver) resolveDatanodesBrowser(rawURL string) (*ResolveResult, error) {
	if store := getResolutionStore(); store != nil {
		if cached, _, ok := store.GetResolvedURL(rawURL, time.Hour); ok && isValidDownloadURL(cached) {
			log.Debug("datanodes browser cache hit", "url", rawURL)
			return &ResolveResult{URL: cached, Headers: browserHeaders(rawURL)}, nil
		}
	}

	cdnURL, err := runInBrowser("datanodes", "datanodes", 150*time.Second, func(ctx context.Context) (string, error) {
		var mu sync.Mutex
		var cdn string
		if _, err := chromedp.Call(ctx, network.ClearBrowserCookies, cdp.Empty{}); err != nil {
			return "", fmt.Errorf("browser resolver: clear cookies: %w", err)
		}
		if _, err := chromedp.Call(ctx, network.Enable, network.EnableParams{}); err != nil {
			return "", fmt.Errorf("browser resolver: enable network: %w", err)
		}
		chromedp.Listen(ctx, func(ev any) {
			if e, ok := ev.(*network.EventRequestWillBeSent); ok {
				u := e.Request.URL
				if strings.Contains(u, ".datanodes.to:") && strings.Contains(u, "/d/") {
					mu.Lock()
					cdn = u
					mu.Unlock()
				}
			}
		})

		if err := chromedp.Do(ctx, chromedp.Navigate(rawURL)); err != nil {
			return "", fmt.Errorf("browser resolver: navigate: %w", err)
		}

		// Step 1: wait for the scan component to arm the button (~6s), then POST.
		if err := pollBrowser(ctx, 25*time.Second, `(() => {
			const b = document.getElementById('method_free');
			return !!b && !b.disabled;
		})()`); err != nil {
			return "", fmt.Errorf("browser resolver: DataNodes step 1 button: %w", err)
		}
		if err := chromedp.Do(ctx, chromedp.Click(`#method_free`)); err != nil {
			return "", fmt.Errorf("browser resolver: DataNodes step 1 submit: %w", err)
		}

		// Step 2: Turnstile auto-solves (invisible) and fills the hidden response.
		if err := pollBrowser(ctx, 40*time.Second, `(() => {
			const t = document.querySelector('input[name="cf-turnstile-response"]');
			if (t && t.value && t.value.length > 20) return true;
			return [...document.querySelectorAll('button')].some(b => (b.innerText || '').trim().startsWith('Free Download'));
		})()`); err != nil {
			return "", fmt.Errorf("browser resolver: DataNodes Turnstile: %w", err)
		}

		if _, err := chromedp.Run(ctx, chromedp.Evaluate[chromedp.Void](`(() => {
			const b = [...document.querySelectorAll('button')].find(b => (b.innerText || '').trim().startsWith('Free Download'));
			if (b) b.click();
		})()`)); err != nil {
			return "", fmt.Errorf("browser resolver: DataNodes step 2 click: %w", err)
		}

		deadline := time.Now().Add(60 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			got := cdn
			mu.Unlock()
			if got != "" {
				return got, nil
			}
			select {
			case <-ctx.Done():
				return "", fmt.Errorf("browser resolver: timed out waiting for DataNodes CDN URL")
			case <-time.After(500 * time.Millisecond):
			}
		}
		return "", fmt.Errorf("browser resolver: DataNodes CDN URL not observed")
	})
	if err != nil {
		return nil, err
	}
	if !isValidDownloadURL(cdnURL) {
		return nil, fmt.Errorf("browser resolver: invalid DataNodes CDN URL")
	}

	log.Info("browser resolver obtained direct link", "host", "datanodes", "target_host", strings.Split(strings.TrimPrefix(cdnURL, "https://"), "/")[0])
	cacheBrowserResolution(rawURL, cdnURL, "datanodes")
	return &ResolveResult{URL: cdnURL, Headers: browserHeaders(rawURL)}, nil
}

// resolveKrakenFilesBrowser drives the single-step KrakenFiles form:
// wait for Turnstile + FingerprintJS, then POST /download/<hash> with the
// form fields plus behavior data and read the JSON response.
func (r *HostResolver) resolveKrakenFilesBrowser(rawURL string) (*ResolveResult, error) {
	if store := getResolutionStore(); store != nil {
		if cached, _, ok := store.GetResolvedURL(rawURL, time.Hour); ok && isValidDownloadURL(cached) {
			log.Debug("krakenfiles browser cache hit", "url", rawURL)
			return &ResolveResult{URL: cached, Headers: browserHeaders(rawURL)}, nil
		}
	}

	body, err := runInBrowser("krakenfiles", "krakenfiles", 120*time.Second, func(ctx context.Context) (string, error) {
		if err := chromedp.Do(ctx, chromedp.Navigate(rawURL)); err != nil {
			return "", fmt.Errorf("browser resolver: navigate: %w", err)
		}
		if err := pollBrowser(ctx, 45*time.Second, `(() => {
			const t = document.querySelector('input[name="cf-turnstile-response"]');
			const f = document.querySelector('input[name="fingerprint"]');
			return !!(t && t.value.length > 20 && f && f.value.length > 10);
		})()`); err != nil {
			return "", fmt.Errorf("browser resolver: KrakenFiles Turnstile/fingerprint: %w", err)
		}
		// Replicate custom.min.js: populate behavior data, POST the form, and
		// return the raw JSON body.
		return chromedp.Run(ctx, chromedp.Evaluate[string](`(async () => {
			const form = document.getElementById('dl-form');
			if (!form) return '{"status":"error","msg":"download form missing"}';
			if (window.behaviorTracker) {
				const c = behaviorTracker.getBehavior();
				c.ft = behaviorTracker.getPageFocusTime();
				c.fg = behaviorTracker.getFocusGaps();
				c.bg = behaviorTracker.getBlurDurations();
				c.iw = window.innerWidth;
				c.documentReferrer = document.referrer;
				form.querySelector('input[name="userdata"]').value = btoa(JSON.stringify(c));
				behaviorTracker.removeAllListeners();
			}
			const resp = await fetch(form.action, {
				method: 'POST',
				headers: {'Content-Type': 'application/x-www-form-urlencoded'},
				body: new URLSearchParams(new FormData(form)).toString(),
				credentials: 'same-origin'
			});
			return await resp.text();
		})()`, chromedp.EvalAwaitPromise))
	})
	if err != nil {
		return nil, err
	}
	return parseKrakenFilesResponse(rawURL, body)
}

// parseKrakenFilesResponse interprets the JSON returned by KrakenFiles'
// /download/<hash> endpoint. Kept separate from the browser flow so the success
// and error shapes are unit-testable without launching Chromium.
func parseKrakenFilesResponse(rawURL, body string) (*ResolveResult, error) {
	var result struct {
		Status string `json:"status"`
		URL    string `json:"url"`
		Msg    string `json:"msg"`
	}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		return nil, fmt.Errorf("browser resolver: KrakenFiles returned non-JSON response")
	}
	if result.Status != "ok" || result.URL == "" {
		if result.Msg == "" {
			result.Msg = "unknown server error"
		}
		return nil, fmt.Errorf("krakenfiles: %s", result.Msg)
	}
	if !isValidDownloadURL(result.URL) || !strings.HasPrefix(result.URL, "https://") {
		return nil, fmt.Errorf("browser resolver: KrakenFiles returned an invalid URL")
	}
	cacheBrowserResolution(rawURL, result.URL, "krakenfiles")
	return &ResolveResult{URL: result.URL, Headers: browserHeaders(rawURL)}, nil
}

func cacheBrowserResolution(key, resolved, host string) {
	if store := getResolutionStore(); store != nil {
		if err := store.SaveResolvedURL(key, resolved, host); err != nil {
			log.Warn("failed to cache browser resolution", "error", err)
		}
	}
}

// pollBrowser evaluates a boolean JS predicate until it is true or times out.
func pollBrowser(ctx context.Context, timeout time.Duration, js string) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](js))
		if err == nil && ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("condition not met within %s", timeout)
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
