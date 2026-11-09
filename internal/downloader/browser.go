package downloader

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	cdpbrowser "github.com/chromedp/cdproto/browser"
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

// resolveUploadHavenBrowser automates UploadHaven's free-download page.
// The page shows a countdown and an invisible Turnstile widget; when both are
// ready it enables #submitFree. Submitting returns a "Download Started" page
// whose manual link is the signed downloadNNN.uploadhaven.com URL.
func (r *HostResolver) resolveUploadHavenBrowser(rawURL string) (*ResolveResult, error) {
	if store := getResolutionStore(); store != nil {
		if cached, _, ok := store.GetResolvedURL(rawURL, time.Hour); ok && isValidDownloadURL(cached) {
			log.Debug("uploadhaven browser cache hit", "url", rawURL)
			return &ResolveResult{URL: cached, Headers: browserHeaders(rawURL)}, nil
		}
	}

	href, err := runInBrowser("uploadhaven", "uploadhaven", 180*time.Second, func(ctx context.Context) (string, error) {
		if err := chromedp.Do(ctx, chromedp.Navigate(rawURL)); err != nil {
			return "", fmt.Errorf("browser resolver: navigate: %w", err)
		}
		// The page enables the button only after the countdown and Turnstile
		// both complete. Polling type=submit is the site's own ready signal.
		if err := pollBrowser(ctx, 150*time.Second, `(() => {
			const b = document.getElementById('submitFree');
			return !!b && b.type === 'submit';
		})()`); err != nil {
			return "", fmt.Errorf("browser resolver: UploadHaven countdown/Turnstile: %w", err)
		}
		// Clicking the button is unreliable once overlay ads appear; the
		// server only checks that the countdown finished and Turnstile issued
		// a token, so submit the form directly.
		if _, err := chromedp.Run(ctx, chromedp.Evaluate[chromedp.Void](`(() => {
			const form = document.getElementById('form-download');
			if (form) HTMLFormElement.prototype.submit.call(form);
		})()`)); err != nil {
			return "", fmt.Errorf("browser resolver: UploadHaven submit: %w", err)
		}
		deadline := time.Now().Add(60 * time.Second)
		for time.Now().Before(deadline) {
			href, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(() => {
				const a = [...document.querySelectorAll('a')].find(a => /uploadhaven\.com/.test(a.href) && /download/i.test(a.href));
				return a ? a.href : '';
			})()`))
			if err != nil {
				return "", fmt.Errorf("browser resolver: UploadHaven read link: %w", err)
			}
			if strings.HasPrefix(href, "https://") {
				return href, nil
			}
			select {
			case <-ctx.Done():
				return "", fmt.Errorf("browser resolver: UploadHaven timed out waiting for download link")
			case <-time.After(time.Second):
			}
		}
		return "", fmt.Errorf("browser resolver: UploadHaven download link not produced")
	})
	if err != nil {
		return nil, err
	}
	if !isValidDownloadURL(href) {
		return nil, fmt.Errorf("browser resolver: invalid UploadHaven download URL")
	}
	log.Info("browser resolver obtained direct link", "host", "uploadhaven", "target_host", strings.Split(strings.TrimPrefix(href, "https://"), "/")[0])
	cacheBrowserResolution(rawURL, href, "uploadhaven")
	return &ResolveResult{URL: href, Headers: browserHeaders(rawURL)}, nil
}

// resolveBuzzheavierBrowser opens the file page (Cloudflare blocks plain
// HTTP), reads the HTMX download endpoint from the button, and performs the
// HTMX request inside the page. The response has no body; the direct CDN URL
// is in the hx-redirect header.
func (r *HostResolver) resolveBuzzheavierBrowser(rawURL string) (*ResolveResult, error) {
	if store := getResolutionStore(); store != nil {
		if cached, _, ok := store.GetResolvedURL(rawURL, time.Hour); ok && isValidDownloadURL(cached) {
			log.Debug("buzzheavier browser cache hit", "url", rawURL)
			return &ResolveResult{URL: cached, Headers: browserHeaders(rawURL)}, nil
		}
	}

	direct, err := runInBrowser("buzzheavier", "buzzheavier", 90*time.Second, func(ctx context.Context) (string, error) {
		if err := chromedp.Do(ctx, chromedp.Navigate(rawURL)); err != nil {
			return "", fmt.Errorf("browser resolver: navigate: %w", err)
		}
		if err := pollBrowser(ctx, 45*time.Second, `!!document.querySelector('a.download-btn[hx-get]')`); err != nil {
			return "", fmt.Errorf("browser resolver: Buzzheavier download button: %w", err)
		}
		return chromedp.Run(ctx, chromedp.Evaluate[string](`(async () => {
			const a = document.querySelector('a.download-btn[hx-get]');
			const r = await fetch(a.getAttribute('hx-get'), {
				headers: {'HX-Request': 'true', 'HX-Current-URL': location.href},
				credentials: 'same-origin'
			});
			return r.headers.get('hx-redirect') || '';
		})()`, chromedp.EvalAwaitPromise))
	})
	if err != nil {
		return nil, err
	}
	if !isValidDownloadURL(direct) {
		return nil, fmt.Errorf("browser resolver: Buzzheavier returned no redirect")
	}
	log.Info("browser resolver obtained direct link", "host", "buzzheavier", "target_host", strings.Split(strings.TrimPrefix(direct, "https://"), "/")[0])
	cacheBrowserResolution(rawURL, direct, "buzzheavier")
	return &ResolveResult{URL: direct, Headers: browserHeaders(rawURL)}, nil
}

// resolveMixdropBrowser turns MixDrop's file page into its direct CDN URL.
// The DOWNLOAD button calls grecaptcha.execute (invisible v3), then POSTs
// {csrf, token, a:genticket}; the JSON response carries the signed URL.
func (r *HostResolver) resolveMixdropBrowser(rawURL string) (*ResolveResult, error) {
	if store := getResolutionStore(); store != nil {
		if cached, _, ok := store.GetResolvedURL(rawURL, time.Hour); ok && isValidDownloadURL(cached) {
			log.Debug("mixdrop browser cache hit", "url", rawURL)
			return &ResolveResult{URL: cached, Headers: browserHeaders(rawURL)}, nil
		}
	}

	pageURL := mixdropDownloadPageURL(rawURL)
	direct, err := runInBrowser("mixdrop", "mixdrop", 120*time.Second, func(ctx context.Context) (string, error) {
		if err := chromedp.Do(ctx, chromedp.Navigate(pageURL)); err != nil {
			return "", fmt.Errorf("browser resolver: navigate: %w", err)
		}
		// Let Cloudflare's redirect chain settle before issuing evaluations;
		// an Evaluate during navigation can block until the context expires.
		if err := chromedp.Do(ctx, chromedp.Sleep(8*time.Second)); err != nil {
			return "", fmt.Errorf("browser resolver: MixDrop settle: %w", err)
		}
		if err := pollBrowser(ctx, 60*time.Second, `(() => {
			const b = document.querySelector('.download-btn[data-cf-key]');
			return !!(b && document.querySelector('meta[name="csrf"]') && window.grecaptcha);
		})()`); err != nil {
			return "", fmt.Errorf("browser resolver: MixDrop page not ready: %w", err)
		}
		return chromedp.Run(ctx, chromedp.Evaluate[string](`(async () => {
			const csrf = document.querySelector('meta[name="csrf"]').content;
			// MixDrop's own player executes this reCAPTCHA v3 key. The
			// data-cf-key attribute is a Turnstile key used by a different
			// protection layer, not this exchange.
			const token = await Promise.race([
				grecaptcha.execute('6LetXaoUAAAAAB6axgg4WLG9oZ_6QLTsFXZj-5sd', {action: 'download'}),
				new Promise((_, reject) => setTimeout(() => reject(new Error('reCAPTCHA timed out')), 20000))
			]);
			const r = await fetch(location.href, {
				method: 'POST',
				headers: {'Content-Type': 'application/x-www-form-urlencoded'},
				body: new URLSearchParams({csrf, token, a: 'genticket'}).toString(),
				credentials: 'same-origin'
			});
			return await r.text();
		})()`, chromedp.EvalAwaitPromise))
	})
	if err != nil {
		return nil, err
	}
	var got struct {
		Type string `json:"type"`
		URL  string `json:"url"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal([]byte(direct), &got); err != nil {
		return nil, fmt.Errorf("browser resolver: MixDrop returned non-JSON response")
	}
	if got.URL == "" {
		if got.Msg == "" {
			got.Msg = "no download URL"
		}
		return nil, fmt.Errorf("mixdrop: %s", got.Msg)
	}
	if !isValidDownloadURL(got.URL) {
		return nil, fmt.Errorf("browser resolver: MixDrop returned an invalid URL")
	}
	log.Info("browser resolver obtained direct link", "host", "mixdrop", "target_host", strings.Split(strings.TrimPrefix(got.URL, "https://"), "/")[0])
	cacheBrowserResolution(rawURL, got.URL, "mixdrop")
	return &ResolveResult{URL: got.URL, Headers: browserHeaders(rawURL)}, nil
}

// mixdropDownloadPageURL converts an /e/ embed URL into the /f/ file page and
// requests its download view, which is where the genticket flow lives.
func mixdropDownloadPageURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	if strings.HasPrefix(u.Path, "/e/") {
		u.Path = "/f/" + strings.TrimPrefix(u.Path, "/e/")
	}
	u.RawQuery = "download"
	return u.String()
}

// browserChallengeTimeout bounds how long the masked-link and WDHO resolvers
// wait for a human to clear an interactive challenge in the opened window.
// It is a variable so tests can shorten it.
var browserChallengeTimeout = 240 * time.Second

// resolveMaskedBrowser opens F95Zone's masked page in a headed browser and
// waits for the site's own reCAPTCHA widget to be cleared. It injects the
// caller's F95Zone cookies first, clicks the host link, and captures the
// document navigation to the real file host. The resolver never solves the
// captcha itself; a human (or a prior reputation cookie) must clear it.
func (r *HostResolver) resolveMaskedBrowser(maskedURL string) (string, error) {
	if r.f95Cookie == "" {
		return "", fmt.Errorf("F95Zone masked link requires a session cookie")
	}
	var captured string
	var mu sync.Mutex
	_, err := runInBrowser("f95masked", "f95masked", 300*time.Second, func(ctx context.Context) (string, error) {
		if err := setF95BrowserCookies(ctx, r.f95Cookie); err != nil {
			return "", fmt.Errorf("browser resolver: set F95 cookies: %w", err)
		}
		if _, err := chromedp.Call(ctx, network.Enable, network.EnableParams{}); err != nil {
			return "", fmt.Errorf("browser resolver: enable network: %w", err)
		}
		chromedp.Listen(ctx, func(ev any) {
			e, ok := ev.(*network.EventRequestWillBeSent)
			if !ok || e.Type != network.ResourceTypeDocument {
				return
			}
			u := e.Request.URL
			if strings.Contains(strings.ToLower(u), "f95zone.to") {
				return
			}
			if !isValidDownloadURL(u) || IdentifyHostInURL(u) == "unknown" {
				return
			}
			mu.Lock()
			if captured == "" {
				captured = u
			}
			mu.Unlock()
		})

		if err := chromedp.Do(ctx, chromedp.Navigate(maskedURL)); err != nil {
			return "", fmt.Errorf("browser resolver: navigate: %w", err)
		}
		if err := pollBrowser(ctx, 60*time.Second, `!!document.querySelector('.host_link')`); err != nil {
			return "", fmt.Errorf("browser resolver: F95 masked page: %w", err)
		}
		if _, err := chromedp.Run(ctx, chromedp.Evaluate[chromedp.Void](`(() => {
			const a = document.querySelector('.host_link');
			if (a) a.click();
		})()`)); err != nil {
			return "", fmt.Errorf("browser resolver: click host link: %w", err)
		}

		deadline := time.Now().Add(browserChallengeTimeout)
		for time.Now().Before(deadline) {
			mu.Lock()
			got := captured
			mu.Unlock()
			if got != "" {
				return got, nil
			}
			select {
			case <-ctx.Done():
				return "", fmt.Errorf("browser resolver: F95Zone captcha not cleared")
			case <-time.After(time.Second):
			}
		}
		return "", fmt.Errorf("browser resolver: F95Zone captcha not cleared — complete the reCAPTCHA in the opened browser")
	})
	if err != nil {
		return "", err
	}
	if !isValidDownloadURL(captured) {
		return "", fmt.Errorf("browser resolver: F95Zone returned no host URL")
	}
	log.Info("browser resolver obtained masked target", "target_host", IdentifyHostInURL(captured))
	return captured, nil
}

// setF95BrowserCookies copies a Cookie header into the chromedp browser so the
// masked page is authenticated.
func setF95BrowserCookies(ctx context.Context, header string) error {
	cookies := make([]*network.CookieParam, 0, 8)
	for _, pair := range strings.Split(header, "; ") {
		name, value, ok := strings.Cut(pair, "=")
		if !ok || name == "" || value == "" {
			continue
		}
		cookies = append(cookies, &network.CookieParam{
			Name:   name,
			Value:  value,
			Domain: ".f95zone.to",
			Path:   "/",
			Secure: true,
		})
	}
	if len(cookies) == 0 {
		return fmt.Errorf("empty cookie header")
	}
	if _, err := chromedp.Call(ctx, network.SetCookies, network.SetCookiesParams{Cookies: cookies}); err != nil {
		return err
	}
	return nil
}

// resolveWDHOBrowser drives WDHO's page through its JavaScript/device probe and
// colour-button challenge. The page itself performs the POST to /robots.txt and
// sets the antibot cookie; this resolver waits for a human to clear the colour
// check, then captures the resulting download navigation or link. It never
// picks a colour itself.
func (r *HostResolver) resolveWDHOBrowser(rawURL string) (*ResolveResult, error) {
	if store := getResolutionStore(); store != nil {
		if cached, _, ok := store.GetResolvedURL(rawURL, time.Hour); ok && isValidDownloadURL(cached) {
			log.Debug("wdho browser cache hit", "url", rawURL)
			return &ResolveResult{URL: cached, Headers: browserHeaders(rawURL)}, nil
		}
	}

	downloadDir, err := os.MkdirTemp("", "moxie-wdho-")
	if err != nil {
		return nil, fmt.Errorf("browser resolver: create download dir: %w", err)
	}
	defer os.RemoveAll(downloadDir)

	var captured string
	var sawFormDownload bool
	var mu sync.Mutex
	direct, err := runInBrowser("wdho", "wdho", 360*time.Second, func(ctx context.Context) (string, error) {
		if _, err := chromedp.Call(ctx, network.Enable, network.EnableParams{}); err != nil {
			return "", fmt.Errorf("browser resolver: enable network: %w", err)
		}
		eventsEnabled := true
		if _, err := chromedp.Call(ctx, cdpbrowser.SetDownloadBehavior, cdpbrowser.SetDownloadBehaviorParams{
			Behavior:      cdpbrowser.SetDownloadBehaviorBehaviorAllow,
			DownloadPath:  downloadDir,
			EventsEnabled: &eventsEnabled,
		}); err != nil {
			return "", fmt.Errorf("browser resolver: enable downloads: %w", err)
		}
		chromedp.Listen(ctx, func(ev any) {
			switch e := ev.(type) {
			case *cdpbrowser.EventDownloadWillBegin:
				if !isValidDownloadURL(e.URL) {
					return
				}
				if strings.Contains(e.URL, "/kbhw") {
					mu.Lock()
					sawFormDownload = true
					mu.Unlock()
					return
				}
				mu.Lock()
				if captured == "" {
					captured = e.URL
				}
				mu.Unlock()
			case *network.EventRequestWillBeSent:
				u := e.Request.URL
				if strings.Contains(u, "/robots.txt") {
					log.Info("browser resolver observed WDHO device probe", "host", "wdho")
					return
				}
				if e.Type != network.ResourceTypeDocument {
					return
				}
				// Ignore the challenge, ad and anti-bot frames; keep a
				// navigation to a real download host.
				if IdentifyHostInURL(u) == "unknown" || strings.Contains(u, "wdho.ru") {
					return
				}
				if !isValidDownloadURL(u) {
					return
				}
				mu.Lock()
				if captured == "" {
					captured = u
				}
				mu.Unlock()
			}
		})

		if err := chromedp.Do(ctx, chromedp.Navigate(rawURL)); err != nil {
			return "", fmt.Errorf("browser resolver: navigate: %w", err)
		}
		// The page runs its /robots.txt probe and reloads; wait for the
		// colour buttons (or a direct download if the probe already cleared).
		if err := pollBrowser(ctx, 90*time.Second, `(() => {
			if (document.querySelector('button.btn-color, input[name="color"]')) return true;
			return false;
		})()`); err != nil {
			mu.Lock()
			got := captured
			mu.Unlock()
			if got != "" {
				return got, nil
			}
			return "", fmt.Errorf("browser resolver: WDHO colour challenge did not appear: %w", err)
		}
		log.Info("browser resolver waiting for WDHO colour check", "host", "wdho")

		deadline := time.Now().Add(browserChallengeTimeout)
		for time.Now().Before(deadline) {
			mu.Lock()
			got := captured
			mu.Unlock()
			if got != "" {
				return got, nil
			}
			href, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(() => {
				const a = [...document.querySelectorAll('a')].find(a => {
					if (!a.href) return false;
					if (a.hasAttribute('download')) return true;
					try {
						return /\.(zip|rar|7z|tar|gz|mp4|mkv|apk|exe)$/i.test(new URL(a.href).pathname);
					} catch (e) { return false; }
				});
				return a ? a.href : '';
			})()`))
			if err == nil && isValidDownloadURL(href) {
				return href, nil
			}
			select {
			case <-ctx.Done():
				return "", fmt.Errorf("browser resolver: WDHO challenge not cleared")
			case <-time.After(time.Second):
			}
		}
		mu.Lock()
		formDownload := sawFormDownload
		mu.Unlock()
		if formDownload {
			return "", fmt.Errorf("browser resolver: WDHO returned the challenge form endpoint instead of a replayable file URL")
		}
		return "", fmt.Errorf("browser resolver: WDHO challenge not cleared — complete the colour check in the opened browser")
	})
	if err != nil {
		return nil, err
	}
	if !isValidDownloadURL(direct) {
		return nil, fmt.Errorf("browser resolver: invalid WDHO download URL")
	}
	log.Info("browser resolver obtained direct link", "host", "wdho", "target_host", strings.Split(strings.TrimPrefix(direct, "https://"), "/")[0])
	cacheBrowserResolution(rawURL, direct, "wdho")
	return &ResolveResult{URL: direct, Headers: browserHeaders(rawURL)}, nil
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
