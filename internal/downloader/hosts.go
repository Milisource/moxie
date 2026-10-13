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

	"golang.org/x/time/rate"
)

// f95Hostnames are the F95Zone hosts that serve masked redirect endpoints.
// Only requests to these hosts may carry the session cookie, and only a URL on
// one of them may be treated as a masked unwrap target: a foreign URL must
// never receive the cookie nor trigger unwrapping.
var f95Hostnames = map[string]bool{
	"f95zone.to":             true,
	"attachments.f95zone.to": true,
}

// isF95Hostname reports whether host (a bare hostname) is an F95Zone host.
func isF95Hostname(host string) bool {
	return f95Hostnames[strings.ToLower(host)]
}

// isMaskedF95URL reports whether u is an F95Zone masked redirect URL
// (f95zone.to/masked/...), whose trailing path segments are opaque tokens
// rather than filenames. The host must be a genuine F95Zone host and the path
// must start with "/masked/": a foreign URL that merely contains the substring
// is not an F95Zone endpoint and must not be followed with the session cookie.
func isMaskedF95URL(u *url.URL) bool {
	if u == nil || !isF95Hostname(u.Hostname()) {
		return false
	}
	return strings.HasPrefix(u.EscapedPath(), "/masked/")
}

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
	// unwrapLimiter paces unwrap pacing state. F95Zone's masked endpoint is
	// rate-budgeted (live A/B: ok, ok, wall, wall, wall, wall) — pacing
	// unwraps keeps bursts under the budget, retries ride out transient
	// walls, and the browser solver handles persistent ones.
	lastCaptcha   time.Time
	unwrapBackoff []time.Duration
	unwrapLimiter *rate.Limiter
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

// sharedUnwrapLimiter paces F95Zone masked-URL unwraps across every
// resolver in the process. Each download builds its own HostResolver, so a
// per-resolver limiter let parallel game updates fire unwraps back to back
// and trip the captcha wall (~2-3 quick unwraps). One process-wide budget
// keeps parallel downloads as polite as sequential ones (F95-cbv5).
var sharedUnwrapLimiter = rate.NewLimiter(rate.Every(3*time.Second), 1)

// browserMu serialises the browser fallback and masked solver: each run
// copies the user's browser profile and launches a browser, and two at once
// contend for the same profile lock and look like a bot to the hosts.
var browserMu sync.Mutex

// serialBrowserFallback wraps fn so only one browser download runs at a time.
func serialBrowserFallback(fn func(ctx context.Context, url, destDir string) (string, error)) func(ctx context.Context, url, destDir string) (string, error) {
	if fn == nil {
		return nil
	}
	return func(ctx context.Context, url, destDir string) (string, error) {
		browserMu.Lock()
		defer browserMu.Unlock()
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return fn(ctx, url, destDir)
	}
}

// serialMaskedSolver wraps fn so only one browser solve runs at a time.
func serialMaskedSolver(fn func(ctx context.Context, maskedURL string) (string, error)) func(ctx context.Context, maskedURL string) (string, error) {
	if fn == nil {
		return nil
	}
	return func(ctx context.Context, maskedURL string) (string, error) {
		browserMu.Lock()
		defer browserMu.Unlock()
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return fn(ctx, maskedURL)
	}
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
		browserFallback:  serialBrowserFallback(defaultBrowserFallback),
		maskedSolver:     serialMaskedSolver(defaultMaskedSolver),
		// F95Zone's masked unwrap is rate-budgeted; live A/B showed the
		// wall after ~2-3 back-to-back unwraps, clearing minutes later.
		// Pace unwraps and retry with backoff before surfacing the wall.
		unwrapBackoff: []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second, 45 * time.Second},
		unwrapLimiter: sharedUnwrapLimiter,
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
func (r *HostResolver) resolveDepth(rawURL string, host string, depth int) (*ResolveResult, error) {
	if depth > 5 {
		return nil, fmt.Errorf("masked URL chain too deep")
	}
	// Normalize legacy labels (e.g. "google drive" stored by older scrapes):
	// all canonical labels are single lowercase words.
	host = strings.ToLower(strings.ReplaceAll(host, " ", ""))
	log.Debug("resolving host URL", "host", host, "url", rawURL)

	// F95Zone masked URLs are redirect endpoints — unwrap them first by following
	// a HEAD request to get the real download URL, then resolve from there.
	// Masked handling is host- and path-gated: a foreign URL that merely
	// contains "/masked/" is never treated as an F95Zone endpoint (and never
	// receives the session cookie).
	if u, err := url.Parse(rawURL); err == nil && isMaskedF95URL(u) {
		log.Debug("masked URL detected", "url", rawURL)

		// Cache consult: skip the rate-limited unwrap endpoint when this
		// masked URL was already unwrapped. The cached destination still
		// goes through host-specific resolution below.
		if r.resolvedCache != nil {
			if cached, ok := r.resolvedCache(rawURL); ok && cached != "" && cached != rawURL {
				cachedHost := IdentifyHostInURL(cached)
				log.Debug("masked URL cache hit", "masked", rawURL, "resolved", cached, "host", cachedHost)
				return r.resolveDepth(cached, cachedHost, depth+1)
			}
		}

		realURL, err := r.unwrapMasked(rawURL)
		realHost := IdentifyHostInURL(realURL)
		log.Debug("masked unwrap result", "real_url", realURL, "real_host", realHost, "error", err)
		if err != nil {
			// Surface the real unwrap failure (e.g. F95Zone's captcha wall
			// or an expired session) instead of falling through to host
			// resolution with the masked URL — the host resolvers cannot
			// extract anything from a masked path (they only produce the
			// misleading "could not extract file ID" error), and the
			// challenge-marked error is what triggers the browser fallback.
			log.Warn("failed to unwrap masked URL", "url", rawURL, "error", err)
			return nil, fmt.Errorf("unwrap masked URL: %w", err)
		}
		if realURL != rawURL {
			// Cache store: a successful unwrap, so the next resolve of this
			// masked URL skips the endpoint entirely.
			if r.resolvedCachePut != nil {
				r.resolvedCachePut(rawURL, realURL, realHost)
			}
			log.Debug("masked URL resolved", "original", rawURL, "real", realURL)
			return r.resolveDepth(realURL, realHost, depth+1)
		}
	}

	switch host {
	case "pixeldrain":
		return r.resolvePixeldrain(rawURL)
	case "buzzheavier":
		return r.resolveBuzzheavier(rawURL)
	case "gofile":
		return r.resolveGofile(rawURL)
	case "mediafire":
		return r.resolveMediafire(rawURL)
	case "datanodes":
		return r.resolveDatanodes(rawURL)
	case "vikingfile":
		return r.resolveVikingFile(rawURL)
	case "workupload":
		return r.resolveWorkupload(rawURL)
	case "mixdrop":
		return r.resolveMixdrop(rawURL)
	case "googledrive":
		return r.resolveGoogleDrive(rawURL)
	case "mega":
		return r.resolveMega(rawURL)
	default:
		// Direct download - just pass through
		return &ResolveResult{
			URL:     rawURL,
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
	r.paceUnwrap(context.Background())

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
func (r *HostResolver) paceUnwrap(ctx context.Context) {
	if r.unwrapLimiter == nil {
		return
	}
	if err := r.unwrapLimiter.Wait(ctx); err != nil {
		log.Debug("masked unwrap pacing wait aborted", "error", err)
	}
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
	// for f95zone.to (the session may live only in the browser). The session
	// cookie is host-scoped: a non-f95 target never receives it.
	if r.f95Cookie != "" && isF95Hostname(req.URL.Hostname()) {
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
	// The session cookie is host-scoped: only a genuine F95Zone destination
	// may receive it, never a foreign host reached via a caller-supplied URL.
	if r.f95Cookie != "" && isF95Hostname(req.URL.Hostname()) {
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
