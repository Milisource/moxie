package downloader

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/mili/moxie/internal/browser"
	"github.com/mili/moxie/internal/log"
)

// HostResolver resolves a file host URL to a direct downloadable URL.
// Many hosts require cookies, headers, or API calls before serving the file.
type HostResolver struct {
	client    *http.Client
	f95Cookie string
	// cookieSource returns a Cookie header value valid for a hostname, or ""
	// when the browser holds none for it. Defaults to browser-backed
	// extraction; a nil value disables cookie attachment (also the
	// zero-value state, used by callers that construct the resolver
	// directly without NewHostResolver).
	cookieSource func(hostname string) string
	// resolvedCache returns a previously unwrapped destination for a masked
	// F95Zone URL; resolvedCachePut stores one after a successful unwrap.
	// Together they let repeated resolves of the same /masked/ link skip
	// the rate-limited unwrap endpoint. nil = no caching (also the
	// zero-value state). NewHostResolver picks up the pair installed by
	// SetDefaultResolvedCache.
	resolvedCache    func(maskedURL string) (string, bool)
	resolvedCachePut func(maskedURL, resolvedURL, host string)
	// browserFallback, when set, downloads a challenge-graded URL inside a
	// real browser (browserresolve). Invoked at most once per download when
	// the Go path hits a Cloudflare challenge — either at the resolver
	// stage or on the file hop. It returns the path of the finished file
	// inside destDir.
	browserFallback func(ctx context.Context, url, destDir string) (string, error)
	// maskedSolver, when set, resolves a masked URL whose unwrap hit the
	// captcha wall and exhausted the retry budget — the browser drives the
	// interstitial (click Continue, solve the reCAPTCHA checkbox) and
	// returns the real destination URL, which the Go path then downloads
	// with progress/resume. nil = walled links fail with the captcha error
	// (the browser-download fallback may still pick them up).
	maskedSolver func(ctx context.Context, maskedURL string) (string, error)
	// unwrapMu guards the unwrap pacing state. F95Zone's masked endpoint is
	// rate-budgeted (live A/B: ok, ok, wall, wall, wall, wall) — pacing
	// unwraps keeps bursts under the budget, retries ride out transient
	// walls, and the browser solver handles persistent ones.
	unwrapMu         sync.Mutex
	lastUnwrap       time.Time
	lastCaptcha      time.Time
	unwrapBackoff    []time.Duration
	unwrapMinInterval time.Duration
}

// SetF95Cookie sets the F95Zone session cookie string used to authenticate
// HEAD requests when resolving masked F95Zone redirect URLs.
func (r *HostResolver) SetF95Cookie(cookie string) {
	r.f95Cookie = cookie
}

// SetResolvedCache wires an optional masked-URL unwrap cache: get returns a
// previously unwrapped destination for a masked URL, put stores one after a
// successful unwrap. Either may be nil to disable caching for this resolver
// (overriding the package default set by SetDefaultResolvedCache).
func (r *HostResolver) SetResolvedCache(get func(maskedURL string) (string, bool), put func(maskedURL, resolvedURL, host string)) {
	r.resolvedCache = get
	r.resolvedCachePut = put
}

// defaultResolvedCache and defaultResolvedCachePut are the cache pair every
// HostResolver created after SetDefaultResolvedCache picks up — including
// the resolver constructed inside DownloadWithContext, whose caller never
// sees the instance. nil = no caching.
var (
	defaultResolvedCache    func(maskedURL string) (string, bool)
	defaultResolvedCachePut func(maskedURL, resolvedURL, host string)
	// defaultBrowserFallback is the browser-download hook every
	// HostResolver created after SetDefaultBrowserFallback picks up,
	// including the resolver inside DownloadWithContext. nil = Go path only.
	defaultBrowserFallback func(ctx context.Context, url, destDir string) (string, error)
	// defaultMaskedSolver is the browser-masked-solver hook every
	// HostResolver created after SetDefaultMaskedSolver picks up.
	defaultMaskedSolver func(ctx context.Context, maskedURL string) (string, error)
)

// SetDefaultResolvedCache installs the resolved-URL cache pair used by every
// HostResolver created afterwards. App entry points that hold a
// *db.Database (TUI, CLI download) attach the DB-backed implementation so
// repeated downloads of the same /masked/ link skip F95Zone's rate-limited
// unwrap endpoint. Pass nil/nil to clear.
func SetDefaultResolvedCache(get func(maskedURL string) (string, bool), put func(maskedURL, resolvedURL, host string)) {
	defaultResolvedCache = get
	defaultResolvedCachePut = put
}

// SetDefaultBrowserFallback installs the browser-download hook used by every
// HostResolver created afterwards (including the one inside
// DownloadWithContext). It runs at most once per download when the Go path
// hits a Cloudflare challenge — the browser performs the download itself
// into destDir and returns the finished file's path. Pass nil to clear.
func SetDefaultBrowserFallback(fn func(ctx context.Context, url, destDir string) (string, error)) {
	defaultBrowserFallback = fn
}

// SetDefaultMaskedSolver installs the masked-URL browser-solver hook used
// by every HostResolver created afterwards (including the one inside
// DownloadWithContext). It runs when an unwrap hits F95Zone's captcha wall
// and the retry budget is exhausted: the browser drives the masked
// interstitial and returns the real destination URL, which the Go path
// then downloads. Pass nil to clear.
func SetDefaultMaskedSolver(fn func(ctx context.Context, maskedURL string) (string, error)) {
	defaultMaskedSolver = fn
}

// NewHostResolver creates a resolver with a shared HTTP client.
func NewHostResolver() *HostResolver {
	return &HostResolver{
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
		cookieSource:     browserCookieHeader,
		resolvedCache:    defaultResolvedCache,
		resolvedCachePut: defaultResolvedCachePut,
		browserFallback:  defaultBrowserFallback,
		maskedSolver:     defaultMaskedSolver,
		// F95Zone's masked unwrap is rate-budgeted; live A/B showed the
		// wall after ~2-3 back-to-back unwraps, clearing minutes later.
		// Pace unwraps and retry with backoff before surfacing the wall.
		unwrapBackoff:     []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second, 45 * time.Second},
		unwrapMinInterval: 3 * time.Second,
	}
}

// SetBrowserFallback overrides the browser-download hook for this resolver
// (see SetDefaultBrowserFallback). Pass nil to disable.
func (r *HostResolver) SetBrowserFallback(fn func(ctx context.Context, url, destDir string) (string, error)) {
	r.browserFallback = fn
}

// SetMaskedSolver overrides the masked-URL browser-solver hook for this
// resolver (see SetDefaultMaskedSolver). Pass nil to disable.
func (r *HostResolver) SetMaskedSolver(fn func(ctx context.Context, maskedURL string) (string, error)) {
	r.maskedSolver = fn
}

// browserCookieHeader extracts the browser's cookies for a hostname as a
// Cookie header value. Best-effort: extraction failures are logged and
// return "" so downloads proceed without clearance instead of failing on a
// cookie read error.
func browserCookieHeader(hostname string) string {
	if hostname == "" {
		return ""
	}
	header, err := browser.GetCookiesForHost(hostname)
	if err != nil {
		log.Debug("browser cookie extraction failed", "host", hostname, "error", err)
		return ""
	}
	return header
}

// attachBrowserCookies merges the browser's cookies for req's URL hostname
// into req's Cookie header. Cookies are host-scoped to the request's own
// domain (RFC 6265 domain match), so they never leak to redirect targets
// or other hosts.
func (r *HostResolver) attachBrowserCookies(req *http.Request) {
	mergeBrowserCookies(req, r.cookieSource)
}

// ResolveResult contains the resolved download URL and any required headers.
type ResolveResult struct {
	URL     string
	Headers map[string]string
}

// Resolve takes a URL and host label, returns a direct download URL + headers.
// If no host-specific handler exists, returns the URL as-is for direct download.
// F95Zone masked URLs (f95zone.to/masked/) are resolved via HEAD redirect first,
// then host-specific resolution is applied on the real download URL.
func (r *HostResolver) Resolve(url string, host string) (*ResolveResult, error) {
	return r.resolveDepth(url, host, 0)
}

// resolveDepth implements Resolve with a recursion bound so a malicious
// masked→masked redirect chain cannot recurse indefinitely.
func (r *HostResolver) resolveDepth(url string, host string, depth int) (*ResolveResult, error) {
	if depth > 5 {
		return nil, fmt.Errorf("masked URL chain too deep")
	}
	// Normalize legacy labels (e.g. "google drive" stored by older scrapes):
	// all canonical labels are single lowercase words.
	host = strings.ToLower(strings.ReplaceAll(host, " ", ""))
	log.Debug("resolving host URL", "host", host, "url", url)

	// F95Zone masked URLs are redirect endpoints — unwrap them first by following
	// a HEAD request to get the real download URL, then resolve from there.
	if strings.Contains(strings.ToLower(url), "/masked/") {
		log.Debug("masked URL detected", "url", url)

		// Cache consult: skip the rate-limited unwrap endpoint when this
		// masked URL was already unwrapped. The cached destination still
		// goes through host-specific resolution below.
		if r.resolvedCache != nil {
			if cached, ok := r.resolvedCache(url); ok && cached != "" && cached != url {
				cachedHost := IdentifyHostInURL(cached)
				log.Debug("masked URL cache hit", "masked", url, "resolved", cached, "host", cachedHost)
				return r.resolveDepth(cached, cachedHost, depth+1)
			}
		}

		realURL, err := r.unwrapMasked(url)
		realHost := IdentifyHostInURL(realURL)
		log.Debug("masked unwrap result", "real_url", realURL, "real_host", realHost, "error", err)
		if err != nil {
			// Surface the real unwrap failure (e.g. F95Zone's captcha wall
			// or an expired session) instead of falling through to host
			// resolution with the masked URL — the host resolvers cannot
			// extract anything from a masked path (they only produce the
			// misleading "could not extract file ID" error), and the
			// challenge-marked error is what triggers the browser fallback.
			log.Warn("failed to unwrap masked URL", "url", url, "error", err)
			return nil, fmt.Errorf("unwrap masked URL: %w", err)
		}
		if realURL != url {
			// Cache store: a successful unwrap, so the next resolve of this
			// masked URL skips the endpoint entirely.
			if r.resolvedCachePut != nil {
				r.resolvedCachePut(url, realURL, realHost)
			}
			log.Debug("masked URL resolved", "original", url, "real", realURL)
			return r.resolveDepth(realURL, realHost, depth+1)
		}
	}

	switch host {
	case "pixeldrain":
		return r.resolvePixeldrain(url)
	case "buzzheavier":
		return r.resolveBuzzheavier(url)
	case "gofile":
		return r.resolveGofile(url)
	case "mediafire":
		return r.resolveMediafire(url)
	case "datanodes":
		return r.resolveDatanodes(url)
	case "vikingfile":
		return r.resolveVikingFile(url)
	case "workupload":
		return r.resolveWorkupload(url)
	case "mixdrop":
		return r.resolveMixdrop(url)
	case "googledrive":
		return r.resolveGoogleDrive(url)
	case "mega":
		return r.resolveMega(url)
	default:
		// Direct download - just pass through
		return &ResolveResult{
			URL:     url,
			Headers: map[string]string{},
		}, nil
	}
}

// unwrapMasked resolves an F95Zone masked URL to the real download URL.
// F95Zone's masked pages are a JS+reCAPTCHA interstitial ("Continue to
// <host>"), but the page's own AJAX endpoint — POST {xhr:1, download:1} to
// the masked path — returns the destination URL as JSON when a valid
// session cookie is sent:
//
//	{"status":"ok","msg":"https://pixeldrain.com/u/..."}
//
// The endpoint is rate-budgeted: live A/B (2026-08-09) showed "ok" for the
// first ~2 requests, then the captcha wall, clearing minutes later. Three
// defenses, cheapest first:
//
//  1. pacing — unwrap POSTs are spaced (unwrapMinInterval) so bursts stay
//     under the budget;
//  2. retries — a walled unwrap is retried with backoff before giving up;
//  3. the maskedSolver hook — when the wall persists, the browser drives
//     the interstitial (click Continue, solve the reCAPTCHA checkbox) and
//     hands back the real URL for the Go path to download.
//
// Falls back to followRedirect (HTTP redirect chain) for older F95Zone
// deployments that answer non-JSON.
func (r *HostResolver) unwrapMasked(rawURL string) (string, error) {
	r.paceUnwrap()

	status, msg, err := r.unwrapOnce(rawURL)
	if err != nil {
		return "", err
	}

	backoff := r.unwrapBackoff
	for status == "captcha" && len(backoff) > 0 {
		delay := backoff[0]
		backoff = backoff[1:]
		log.Debug("masked unwrap captcha — retrying", "url", rawURL, "delay", delay)
		time.Sleep(delay)
		status, msg, err = r.unwrapOnce(rawURL)
		if err != nil {
			return "", err
		}
	}

	switch status {
	case "ok":
		if msg == "" {
			return "", fmt.Errorf("masked URL endpoint returned ok without a destination")
		}
		return msg, nil
	case "captcha":
		if r.maskedSolver != nil {
			log.Info("masked unwrap: captcha wall persists — solving in the browser", "url", rawURL)
			realURL, err := r.maskedSolver(context.Background(), rawURL)
			if err == nil && realURL != "" && realURL != rawURL {
				log.Info("masked unwrap: browser solver returned destination", "real_url", realURL)
				return realURL, nil
			}
			log.Warn("masked unwrap: browser solver failed", "url", rawURL, "error", err)
		}
		return "", fmt.Errorf("masked URL requires a captcha solve (re-import cookies or open the link in a browser)")
	case "error":
		return "", fmt.Errorf("masked URL endpoint error: %s", msg)
	default:
		return "", fmt.Errorf("masked URL endpoint returned unexpected status %q", status)
	}
}

// paceUnwrap enforces a minimum interval between masked unwrap POSTs:
// F95Zone rate-budgets the endpoint and bursts trip the captcha wall.
func (r *HostResolver) paceUnwrap() {
	r.unwrapMu.Lock()
	defer r.unwrapMu.Unlock()
	interval := r.unwrapMinInterval
	if interval <= 0 {
		return
	}
	if !r.lastUnwrap.IsZero() {
		if wait := interval - time.Since(r.lastUnwrap); wait > 0 {
			log.Debug("masked unwrap pacing", "wait", wait)
			time.Sleep(wait)
		}
	}
	r.lastUnwrap = time.Now()
}

// unwrapOnce performs a single masked unwrap POST and returns the JSON
// status/msg. Legacy non-JSON deployments (redirect flow) fold into
// status "ok" with the redirect-chain destination.
func (r *HostResolver) unwrapOnce(rawURL string) (status, msg string, err error) {
	form := url.Values{}
	form.Set("xhr", "1")
	form.Set("download", "1")
	req, err := http.NewRequest("POST", rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", fmt.Errorf("create masked unwrap request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Referer", rawURL)
	// Explicit caller cookie wins; otherwise fall back to browser cookies
	// for f95zone.to (the session may live only in the browser).
	if r.f95Cookie != "" {
		req.Header.Set("Cookie", r.f95Cookie)
	} else {
		r.attachBrowserCookies(req)
	}

	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("masked unwrap POST failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("masked unwrap read body: %w", err)
	}

	var result struct {
		Status string `json:"status"`
		Msg    string `json:"msg"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		// Not JSON — either the legacy redirect flow or an error page.
		// Reuse the redirect chain as a fallback.
		log.Debug("masked unwrap non-JSON response", "status", resp.StatusCode, "len", len(body))
		realURL, err := r.followRedirect(rawURL)
		if err != nil {
			return "", "", fmt.Errorf("masked unwrap non-JSON and redirect chain failed: %w", err)
		}
		return "ok", realURL, nil
	}
	log.Debug("masked unwrap response", "status_code", resp.StatusCode, "result_status", result.Status)
	return result.Status, result.Msg, nil
}

// followRedirect performs a GET request to url and follows redirects to find
// the final destination URL. Used to unwrap F95Zone masked redirect endpoints
// before applying host-specific URL resolution.
//
// GET is used instead of HEAD because some F95Zone masked URL handlers do
// not serve the correct redirect on HEAD requests — they return the thread
// page or login page instead. The Referer is set to an f95zone.to domain
// to match the anti-hotlinking expectation.
func (r *HostResolver) followRedirect(url string) (string, error) {
	log.Debug("followRedirect request", "url", url, "has_cookie", r.f95Cookie != "")
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return url, fmt.Errorf("create redirect-follow request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("Referer", "https://f95zone.to/")
	if r.f95Cookie != "" {
		req.Header.Set("Cookie", r.f95Cookie)
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return url, fmt.Errorf("redirect-follow GET failed: %w", err)
	}
	defer resp.Body.Close()

	finalURL := resp.Request.URL.String()
	if finalURL == "" {
		return url, nil
	}

	// If the final URL is the F95Zone login page, the cookie is invalid or
	// the session expired. Signal this clearly so the caller can surface it.
	if isLoginRedirect(finalURL) {
		return url, fmt.Errorf("redirected to login — F95Zone session may have expired; re-import cookies")
	}

	log.Debug("followRedirect result", "original", url, "final", finalURL, "status", resp.StatusCode)
	return finalURL, nil
}

// isLoginRedirect returns true if the URL looks like an F95Zone login page.
func isLoginRedirect(rawURL string) bool {
	lower := strings.ToLower(rawURL)
	return strings.Contains(lower, "/login") || strings.Contains(lower, "_xfRedirect")
}

// IdentifyHostInURL extracts the host label from a URL for host-specific routing.
// Matches the same hosts as parser.go's identifyHost for consistency.
func IdentifyHostInURL(rawURL string) string {
	lower := strings.ToLower(rawURL)

	hosts := []struct {
		label  string
		needle string
	}{
		{"mega", "mega.nz"},
		{"mega", "mega.co"},
		{"pixeldrain", "pixeldrain"},
		{"buzzheavier", "buzzheavier"},
		{"buzzheavier", "bzzhr.to"}, // buzzheavier's short-link domain
		{"gofile", "gofile"},
		{"vikingfile", "vikingfile"},
		{"vern", "vern.cc"},
		{"mediafire", "mediafire"},
		{"workupload", "workupload"},
		{"bunkrr", "bunkrr"},
		{"bunkrr", "bunkr"},
		{"krakenfiles", "krakenfiles"},
		{"uploadhaven", "uploadhaven"},
		{"wetransfer", "wetransfer"},
		{"sendgb", "sendgb"},
		{"1cloudfile", "1cloudfile"},
		{"1cloudfile", "1cloud"},
		{"akirabox", "akirabox"},
		{"anontransfer", "anontransfer"},
		{"anonymfile", "anonymfile"},
		{"apkadmin", "apkadmin"},
		{"bowfile", "bowfile"},
		{"catbox", "catbox"},
		{"cyberfile", "cyberfile"},
		{"datanodes", "datanodes"},
		{"delafil", "delafil"},
		{"downloadgg", "download.gg"},
		{"dropmefiles", "dropmefiles"},
		{"easyupload", "easyupload"},
		{"filemail", "filemail"},
		{"filesdpua", "files.dp.ua"},
		{"filesdpua", "dp.ua"},
		{"filesfm", "files.fm"},
		{"filesfm", "filesfm"},
		{"fromsmash", "fromsmash"},
		{"googledrive", "drive.google"},
		{"hexload", "hexload"},
		{"hexload", "hexupload"},
		{"mixdrop", "mixdrop"},
		{"mixdrop", "m1xdrop"},
		{"mixdrop", "miixdrop"},
		{"protondrive", "proton drive"},
		{"protondrive", "proton.me"},
		{"quax", "qu.ax"},
		{"terminal", "terminal"},
		{"transfersh", "transfer.sh"},
		{"transfert", "transfert"},
		{"uploadnow", "uploadnow"},
		{"wdho", "wdho"},
		{"yourfilestore", "yourfilestore"},
		{"keep2share", "keep2share"},
		{"keep2share", "k2s"},
		{"uploaded", "uploaded"},
		{"uploaded", "ul.to"},
		{"dropbox", "dropbox"},
	}
	for _, h := range hosts {
		if strings.Contains(lower, h.needle) {
			return h.label
		}
	}
	return "unknown"
}
