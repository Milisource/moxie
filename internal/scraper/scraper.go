package scraper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/mili/moxie/internal/log"
)

const (
	// User-Agent must match the browser that generated the cf_clearance cookie.
	// Since we read cookies from Firefox via kooky, use a Firefox UA.
	// NOTE: platformUserAgent() provides a platform-appropriate Firefox UA;
	// do not use a constant string here.
	defaultTimeout    = 10 * time.Second
	minDelay          = 1500 * time.Millisecond // minimum between thread requests
	maxResponseBytes  = 8 * 1024 * 1024         // cap for block-detection body reads
	maxJitter         = 1500 * time.Millisecond // random extra delay
	searchMinDelay    = 3 * time.Second         // search is more expensive, be gentler
	cooldownInterval  = 35                      // pause longer every N requests
	cooldownDuration  = 10 * time.Second        // length of cooldown pause
	backoffMultiplier = 2                       // multiply delay on rate limit
	maxBackoff        = 2 * time.Minute         // cap exponential backoff

	// Retry policy: transient failures (5xx, network errors, rate limits)
	// are retried up to maxAttempts total with a growing pause between
	// attempts. 403s get a single retry — Cloudflare challenges are
	// sometimes per-request rather than per-IP — but a persistent block
	// trips the circuit breaker instead of hammering the server.
	maxAttempts          = 3
	maxConsecutiveBlocks = 3               // consecutive blocking responses before refusing requests
	retryBackoffBase     = 2 * time.Second // pause for retry attempt N: 2s, 4s, ...
	baseURL              = "https://f95zone.to/"
)

// platformUserAgent returns a Firefox User-Agent string appropriate for the
// current OS, so requests blend in regardless of where the binary runs.
func platformUserAgent() string {
	switch runtime.GOOS {
	case "windows":
		return "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:140.0) Gecko/20100101 Firefox/140.0"
	case "darwin":
		return "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:140.0) Gecko/20100101 Firefox/140.0"
	default:
		return "Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0"
	}
}

// HTTPStatusError reports a non-OK HTTP response status (other than the
// block statuses, which surface as BlockedError). Typed so callers can
// branch on the code instead of matching error strings.
type HTTPStatusError struct {
	StatusCode int
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("scraper: unexpected status %d", e.StatusCode)
}

// BlockedError indicates the request was blocked by anti-bot protection.
type BlockedError struct {
	Reason     string
	StatusCode int
}

func (e *BlockedError) Error() string {
	return fmt.Sprintf("scraper: blocked — %s (HTTP %d)", e.Reason, e.StatusCode)
}

// Client handles authenticated HTTP requests to a XenForo forum with
// built-in rate limiting and bot-detection awareness.
type Client struct {
	http        *http.Client
	mu          sync.Mutex
	lastRequest time.Time
	delay       time.Duration // current delay between requests (adapts)
	reqCount    int           // requests since last cooldown
	csrfToken   string        // _xfToken extracted from xf_csrf cookie

	// Circuit breaker: after maxConsecutiveBlocks blocking responses the
	// client refuses further requests so a dead session doesn't waste a
	// whole sync run failing one game at a time.
	consecutiveBlocks int
	blocked           bool
}

// xfCSRFToken returns the XenForo CSRF token for authenticated POST requests.
// Returns empty string if no xf_csrf cookie was found in the client config.
func (c *Client) xfCSRFToken() string {
	return c.csrfToken
}

// NewClient creates a scraper client with the given cookie string.
func NewClient(cookieStr string) *Client {
	return newClient(cookieStr, false)
}

// NewUnsafeClient creates a scraper client that skips rate limiting.
// ⚠ This may trigger IP bans or Cloudflare blocks. Use only for testing
// or when you're willing to risk being temporarily blocked.
func NewUnsafeClient(cookieStr string) *Client {
	return newClient(cookieStr, true)
}

// NewClientWithHTTP creates a scraper client with the given cookie string
// and a custom http.Client. The inner transport is wrapped with cookie
// injection automatically. Useful for testing with httptest servers.
func NewClientWithHTTP(cookieStr string, httpClient *http.Client) *Client {
	delay := minDelay
	if httpClient.Timeout == 0 {
		httpClient.Timeout = defaultTimeout
	}
	inner := httpClient.Transport
	if inner == nil {
		inner = http.DefaultTransport
	}
	return &Client{
		http: &http.Client{
			Timeout: httpClient.Timeout,
			Transport: &cookieTransport{
				inner:        inner,
				cookieValue:  strings.TrimSpace(cookieStr),
				unrestricted: true, // httptest servers cannot use real f95zone.to hostnames
			},
			CheckRedirect: httpClient.CheckRedirect,
			Jar:           httpClient.Jar,
		},
		delay:     delay,
		csrfToken: extractCSRFToken(cookieStr),
	}
}

func newClient(cookieStr string, unsafe bool) *Client {
	delay := minDelay
	if unsafe {
		delay = 0
	}
	return &Client{
		http: &http.Client{
			Timeout: defaultTimeout,
			Transport: &cookieTransport{
				inner:       http.DefaultTransport,
				cookieValue: strings.TrimSpace(cookieStr),
			},
		},
		delay:     delay,
		csrfToken: extractCSRFToken(cookieStr),
	}
}

// extractCSRFToken parses xf_csrf from a cookie string.
// Supports both Netscape cookie file format (tab-separated) and
// HTTP Cookie header format (semicolon-separated key=value pairs).
func extractCSRFToken(cookieStr string) string {
	s := strings.TrimSpace(cookieStr)
	if s == "" {
		return ""
	}

	// Try Netscape format: tab-separated lines with 7 fields.
	// Field 6 (0-indexed) is the cookie value, field 5 is the name.
	// Example:
	//   f95zone.to	FALSE	/	TRUE	0	xf_csrf	RHqlFZ43aEN40ocI
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) >= 7 && fields[5] == "xf_csrf" {
			return strings.TrimSpace(fields[6])
		}
	}

	// Fall back to Cookie header format: semicolon-separated key=value pairs.
	for _, pair := range strings.Split(s, ";") {
		pair = strings.TrimSpace(pair)
		if strings.HasPrefix(pair, "xf_csrf=") {
			val := strings.TrimPrefix(pair, "xf_csrf=")
			return strings.TrimSpace(val)
		}
	}

	return ""
}

// Delay returns the current inter-request delay.
func (c *Client) Delay() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.delay
}

// do sends an HTTP request with rate limiting, bot detection, retries, and
// a circuit breaker for persistent blocking. It reads the response body once
// and returns it directly, avoiding a second read in the caller.
// baseDelay is the minimum delay between requests (use searchMinDelay for search).
func (c *Client) do(req *http.Request, baseDelay time.Duration) (bodyStr string, err error) {
	if c.isBlocked() {
		return "", c.blockedErr()
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if c.isBlocked() {
			return "", c.blockedErr()
		}
		body, err := c.doOnce(req, baseDelay)
		if err == nil {
			return body, nil
		}
		lastErr = err

		var blockedErr *BlockedError
		if errors.As(err, &blockedErr) {
			c.mu.Lock()
			c.consecutiveBlocks++
			if c.consecutiveBlocks >= maxConsecutiveBlocks {
				c.blocked = true
				log.Warn("circuit breaker tripped; refusing further requests until cookies are refreshed",
					"status", blockedErr.StatusCode,
					"url", req.URL.Redacted(),
				)
			}
			if blockedErr.StatusCode == 429 {
				c.delay = time.Duration(float64(c.delay) * backoffMultiplier)
				if c.delay > maxBackoff {
					c.delay = maxBackoff
				}
			}
			c.mu.Unlock()
		}

		if attempt == maxAttempts || !retryable(err) {
			return "", err
		}
		// Pause between attempts: 2s, 4s, ... with jitter, so retries look
		// like a human re-trying rather than a bot hammering.
		wait := time.Duration(attempt)*retryBackoffBase + time.Duration(rand.Int63n(int64(time.Second)))
		select {
		case <-time.After(wait):
		case <-req.Context().Done():
			return "", req.Context().Err()
		}
	}
	return "", lastErr
}

// isBlocked reports whether the circuit breaker has tripped.
func (c *Client) isBlocked() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.blocked
}

// Blocked reports whether the client tripped its circuit breaker after
// repeated blocking responses and is refusing further requests. Callers
// can check this to stop scheduling work before each request.
func (c *Client) Blocked() bool {
	return c.isBlocked()
}

func (c *Client) blockedErr() error {
	return &BlockedError{
		Reason:     "repeated blocking responses — session expired or IP temporarily blocked; refresh F95Zone cookies in your browser and try again later",
		StatusCode: 0,
	}
}

// retryable reports whether a failed attempt is worth retrying. Network
// errors and 5xx/429 responses are transient; Cloudflare challenge pages
// would only produce the same challenge again.
func retryable(err error) bool {
	if err == nil {
		return false
	}
	if err == context.Canceled || err == context.DeadlineExceeded {
		return false
	}
	var blockedErr *BlockedError
	if errors.As(err, &blockedErr) {
		switch blockedErr.StatusCode {
		case 403, 429, 503:
			return !strings.Contains(blockedErr.Reason, "Cloudflare")
		}
		return false
	}
	// Plain transport/network errors are transient.
	return true
}

// doOnce performs a single paced, rate-limited request attempt.
// It reads the response body once and returns it directly.
func (c *Client) doOnce(req *http.Request, baseDelay time.Duration) (bodyStr string, err error) {
	c.mu.Lock()

	// Periodic cooldown — pause longer every N requests to look human.
	if c.reqCount > 0 && c.reqCount%cooldownInterval == 0 {
		log.Debug("cooldown pause",
			"duration", cooldownDuration,
			"req_count", c.reqCount,
			"url", req.URL.Redacted(),
		)
		c.mu.Unlock()
		select {
		case <-time.After(cooldownDuration):
		case <-req.Context().Done():
			return "", req.Context().Err()
		}
		c.mu.Lock()
	}

	// Enforce minimum delay since last request.
	waitFor := baseDelay
	if c.delay > waitFor {
		waitFor = c.delay
	}
	elapsed := time.Since(c.lastRequest)
	if elapsed < waitFor {
		wait := waitFor - elapsed + time.Duration(rand.Int63n(int64(maxJitter)))
		log.Debug("pacing wait",
			"duration", wait,
			"url", req.URL.Redacted(),
		)
		c.mu.Unlock()
		select {
		case <-time.After(wait):
		case <-req.Context().Done():
			return "", req.Context().Err()
		}
		c.mu.Lock()
	}
	start := time.Now()
	c.lastRequest = start
	c.reqCount++
	c.mu.Unlock()

	applyBrowserHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("scraper: request failed: %w", err)
	}

	// Check for blocking signals and read the body.
	bodyStr, err = c.checkBlocked(resp)
	if err != nil {
		var blockedErr *BlockedError
		if errors.As(err, &blockedErr) {
			log.Warn("request blocked",
				"status", blockedErr.StatusCode,
				"reason", blockedErr.Reason,
				"url", req.URL.Redacted(),
			)
		}
		return "", err
	}

	// Check for non-OK status codes (429/403/503 already caught above).
	if resp.StatusCode != http.StatusOK {
		return "", &HTTPStatusError{StatusCode: resp.StatusCode}
	}

	// Request succeeded — reset the block counter and gradually reduce delay.
	// Decay only when above the floor: a zero delay (unsafe/paced clients)
	// must stay zero, not snap up to minDelay after the first success.
	c.mu.Lock()
	c.consecutiveBlocks = 0
	if c.delay > minDelay {
		c.delay = time.Duration(float64(c.delay) * 0.95)
		if c.delay < minDelay {
			c.delay = minDelay
		}
	}
	c.mu.Unlock()

	return bodyStr, nil
}

// applyBrowserHeaders sets headers matching a real Firefox navigation so
// Cloudflare doesn't flag the request as a bot. Sec-Fetch-* headers are
// checked by modern bot detection and were previously missing.
func applyBrowserHeaders(req *http.Request) {
	req.Header.Set("User-Agent", platformUserAgent())
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	if req.Method == http.MethodPost {
		req.Header.Set("Sec-Fetch-Dest", "empty")
		req.Header.Set("Sec-Fetch-Mode", "cors")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
	} else {
		req.Header.Set("Sec-Fetch-Dest", "document")
		req.Header.Set("Sec-Fetch-Mode", "navigate")
		req.Header.Set("Sec-Fetch-Site", "none")
		req.Header.Set("Sec-Fetch-User", "?1")
	}
}

// checkBlocked inspects the response for Cloudflare or anti-bot signals.
// It reads the response body once and returns it so callers can use
// the body directly without a second read. The read is capped — a hostile
// or oversized response must not exhaust memory; block pages are tiny.
func (c *Client) checkBlocked(resp *http.Response) (bodyStr string, err error) {
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	resp.Body.Close()
	if readErr != nil {
		return "", fmt.Errorf("scraper: failed to read response body: %w", readErr)
	}
	if len(body) > maxResponseBytes {
		return "", fmt.Errorf("scraper: response body exceeds %d bytes; refusing to read", maxResponseBytes)
	}

	bodyStr = string(body)
	cf := strings.Contains(bodyStr, "cf-browser-verification") ||
		strings.Contains(bodyStr, "cf-challenge-running") ||
		strings.Contains(bodyStr, "_cf_chl_opt")

	switch resp.StatusCode {
	case 429:
		return "", &BlockedError{Reason: "rate limited", StatusCode: 429}
	case 403:
		if cf {
			return "", &BlockedError{
				Reason:     "Cloudflare challenge (HTTP 403) — refresh your browser session and re-import cookies",
				StatusCode: 403,
			}
		}
		return "", &BlockedError{
			Reason:     "access denied (HTTP 403) — possible IP block or missing cookies",
			StatusCode: 403,
		}
	case 503:
		if cf {
			return "", &BlockedError{
				Reason:     "Cloudflare challenge (HTTP 503) — refresh your browser session and re-import cookies",
				StatusCode: 503,
			}
		}
		return "", &BlockedError{
			Reason:     "service unavailable (HTTP 503) — possible Cloudflare challenge",
			StatusCode: 503,
		}
	}

	if cf {
		return "", &BlockedError{
			Reason:     "Cloudflare challenge page detected — refresh your browser session and re-import cookies",
			StatusCode: resp.StatusCode,
		}
	}

	return bodyStr, nil
}

// Preflight verifies the F95Zone session is still valid before a long sync
// run by fetching the forum index. It fails fast with a clear error when
// cookies are expired or the IP is blocked, instead of letting every game
// in the run fail one by one.
func (c *Client) Preflight() error {
	return c.preflightWithContext(context.Background(), baseURL)
}

// preflightWithContext is Preflight with a custom context and URL, used by
// tests to point at an httptest server.
func (c *Client) preflightWithContext(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("scraper: preflight: %w", err)
	}
	if _, err := c.do(req, minDelay); err != nil {
		return fmt.Errorf("scraper: preflight failed — F95Zone session invalid or IP blocked: %w", err)
	}
	return nil
}

// ValidateThreadURL rejects URLs that are not https F95Zone thread pages.
// The desktop binds caller-supplied URLs directly to the cookie-carrying
// client, so this keeps the F95Zone session cookie from leaking to
// attacker-controlled hosts. Only the scheme and host are checked; the
// path may be any F95Zone page (threads, members, etc.).
func ValidateThreadURL(rawURL string) error {
	if strings.TrimSpace(rawURL) == "" {
		return fmt.Errorf("scraper: URL is empty")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("scraper: invalid URL %q: %w", rawURL, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("scraper: URL scheme must be https, got %q", u.Scheme)
	}
	host := strings.ToLower(u.Hostname())
	if host != "f95zone.to" && host != "www.f95zone.to" {
		return fmt.Errorf("scraper: URL host must be f95zone.to, got %q", u.Hostname())
	}
	return nil
}

// ThreadURL constructs a slug-agnostic F95Zone thread URL from a thread ID.
// XenForo resolves by thread ID regardless of slug content, so this URL
// is stable across version updates (when the version in the slug changes).
// When threadID is 0, returns empty string (caller should fall back to the
// stored full URL in that case).
func ThreadURL(threadID int64) string {
	if threadID <= 0 {
		return ""
	}
	return fmt.Sprintf("https://f95zone.to/threads/%d/", threadID)
}

// ResolveScrapeURL returns the best URL to use for scraping a game's thread.
// Prefers a slug-agnostic URL from F95ThreadID when available, falling back
// to the stored F95URL for backward compatibility with older DB entries.
func ResolveScrapeURL(f95URL string, f95ThreadID int64) string {
	if u := ThreadURL(f95ThreadID); u != "" {
		return u
	}
	return f95URL
}

// ScrapeThread fetches and parses a XenForo thread page.
func (c *Client) ScrapeThread(threadURL string) (*ThreadData, error) {
	return c.ScrapeThreadWithContext(context.Background(), threadURL)
}

// ScrapeThreadWithContext fetches and parses a XenForo thread page,
// respecting the given context for cancellation and deadlines. Cookie
// injection is host-scoped at the transport level, so the session cookie
// cannot leak to non-F95Zone hosts even when threadURL is caller-supplied.
func (c *Client) ScrapeThreadWithContext(ctx context.Context, threadURL string) (*ThreadData, error) {
	if threadURL == "" {
		return nil, fmt.Errorf("scraper: threadURL is empty")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, threadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("scraper: failed to create request: %w", err)
	}

	body, err := c.do(req, minDelay)
	if err != nil {
		return nil, err
	}

	td, err := parseThreadHTML(body, threadURL)
	if err != nil {
		return nil, fmt.Errorf("scraper: failed to parse thread HTML: %w", err)
	}

	return td, nil
}

// ---------------------------------------------------------------------------
// cookieTransport injects a raw Cookie header on outgoing requests.
// Injection is host-scoped: the session cookie is only attached to
// F95Zone and F95Checker hosts, so it cannot leak to third-party hosts
// via the Google search fallback or redirect chains (Go's cross-domain
// header stripping only covers headers set on the initial request —
// transport-level injection would otherwise re-add the cookie on every
// hop).
type cookieTransport struct {
	inner       http.RoundTripper
	cookieValue string
	// unrestricted disables host scoping. Only set by NewClientWithHTTP,
	// which exists for httptest-based tests that cannot use real
	// f95zone.to hostnames.
	unrestricted bool
}

// f95ZoneHost reports whether the request targets a host the session
// cookie may legitimately be sent to.
func f95ZoneHost(req *http.Request) bool {
	host := strings.ToLower(req.URL.Hostname())
	return host == "f95zone.to" || strings.HasSuffix(host, ".f95zone.to") ||
		host == "api.f95checker.dev"
}

func (ct *cookieTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if ct.cookieValue != "" && (ct.unrestricted || f95ZoneHost(req)) {
		if existing := req.Header.Get("Cookie"); existing != "" {
			req.Header.Set("Cookie", existing+"; "+ct.cookieValue)
		} else {
			req.Header.Set("Cookie", ct.cookieValue)
		}
	}
	return ct.inner.RoundTrip(req)
}
