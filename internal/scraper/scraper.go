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
	"strconv"
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
// branch on the code instead of matching error strings. Body carries the
// response body when one was read (callers like checker.php discriminate
// on it — a 404 whose body says "Thread not found" means "no tracked
// threads", not "endpoint gone").
type HTTPStatusError struct {
	StatusCode int
	Body       string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("scraper: unexpected status %d", e.StatusCode)
}

// BlockKind classifies a blocked response independently of its message.
type BlockKind int

const (
	BlockUnknown BlockKind = iota
	BlockChallenge
	BlockRateLimit
	BlockUnavailable
	BlockForbidden
)

func (k BlockKind) String() string {
	switch k {
	case BlockChallenge:
		return "challenge"
	case BlockRateLimit:
		return "rate limit"
	case BlockUnavailable:
		return "unavailable"
	case BlockForbidden:
		return "forbidden"
	default:
		return "unknown"
	}
}

// BlockedError indicates the request was blocked by anti-bot protection.
type BlockedError struct {
	Kind       BlockKind
	Reason     string
	StatusCode int
	// RetryAfter is the server-provided wait time from a Retry-After
	// header on 429/503 responses, when present. Zero means the server
	// gave no guidance and the caller should fall back to its own backoff
	// schedule.
	RetryAfter time.Duration
}

func (e *BlockedError) Error() string {
	return fmt.Sprintf("scraper: blocked — %s (HTTP %d)", e.Reason, e.StatusCode)
}

// Client handles authenticated HTTP requests to a XenForo forum with
// built-in rate limiting and bot-detection awareness.
type Client struct {
	http      *http.Client
	mu        sync.Mutex
	csrfToken string // _xfToken extracted from xf_csrf cookie

	// pacer holds pacing, 429 backoff, cooldown and circuit-breaker state.
	// Safe clients share the process-wide f95zone.to pacer (pacer.go);
	// unsafe and test clients get a private one.
	pacer *hostPacer

	// Fresh _xfToken fetched from a page. XenForo 2.3+ embeds the current
	// session token in page HTML and rejects the legacy xf_csrf cookie
	// value (400 Security error), so POSTs must use a fetched token.
	// Cached briefly since the token is stable for the session.
	xfTokenValue string
	xfTokenAt    time.Time

	// cookieKey identifies the session for the process-wide token cache.
	// Empty for anonymous/cookieless clients, which keep a private cache.
	cookieKey string

	// Search endpoints — default to f95zone.to; tests override with
	// httptest servers.
	xfTokenPage     string // page fetched to obtain a fresh _xfToken
	xfSearchURL     string // POST /search/search endpoint
	googleSearchURL string // Google SERP fallback

	// unsafe skips rate limiting entirely (NewUnsafeClient).
	unsafe bool

	// authenticated reports whether the client was built with an xf_user
	// cookie (a logged-in session). Used to decide whether a guest page is
	// an expired session vs. an expected guest scrape.
	authenticated bool

	// sessionExpired is set once a guest-rendered page is seen on an
	// authenticated client, so batches stop issuing further cookie scrapes.
	sessionExpired bool

	// clearanceRefreshed records that this client already asked the
	// clearance refresher (session.go) for a new cf_clearance; it asks once.
	clearanceRefreshed bool
}

// NewClient creates a scraper client with the given cookie string.
func NewClient(cookieStr string) *Client {
	return newClient(cookieStr, false)
}

// NewUnsafeClient creates a scraper client that skips rate limiting.
// WARNING: This may trigger IP bans or Cloudflare blocks. Use only for testing
// or when you're willing to risk being temporarily blocked.
func NewUnsafeClient(cookieStr string) *Client {
	return newClient(cookieStr, true)
}

// NewClientWithHTTP creates a scraper client with the given cookie string
// and a custom http.Client. The inner transport is wrapped with cookie
// injection automatically. Useful for testing with httptest servers. The
// client gets its own pacer rather than the shared f95zone.to one, so
// parallel tests against different servers don't slow each other down.
func NewClientWithHTTP(cookieStr string, httpClient *http.Client) *Client {
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
		pacer:           newHostPacer(minDelay),
		csrfToken:       extractCSRFToken(cookieStr),
		cookieKey:       cookieCacheKey(cookieStr),
		xfTokenPage:     xfTokenPageURL,
		xfSearchURL:     xfSearchURL,
		googleSearchURL: googleSearchURL,
		authenticated:   hasAuthenticatedSession(cookieStr),
	}
}

func newClient(cookieStr string, unsafe bool) *Client {
	// Unsafe clients (--unsafe, the F95Checker cache client) keep a private,
	// zero-delay pacer: they deliberately stay outside the F95Zone budget.
	pacer := sharedPacer(f95PacerKey)
	if unsafe {
		pacer = newHostPacer(0)
	}
	log.Debug("scraper session", "authenticated", hasAuthenticatedSession(cookieStr))
	return &Client{
		http: &http.Client{
			Timeout: defaultTimeout,
			Transport: &cookieTransport{
				inner:       http.DefaultTransport,
				cookieValue: strings.TrimSpace(cookieStr),
			},
		},
		pacer:           pacer,
		csrfToken:       extractCSRFToken(cookieStr),
		cookieKey:       cookieCacheKey(cookieStr),
		xfTokenPage:     xfTokenPageURL,
		xfSearchURL:     xfSearchURL,
		googleSearchURL: googleSearchURL,
		unsafe:          unsafe,
		authenticated:   hasAuthenticatedSession(cookieStr),
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

// Authenticated reports whether this client carries a logged-in session.
// Callers use it to skip the XenForo search path, which cannot succeed
// anonymously and costs a token GET plus a POST.
func (c *Client) Authenticated() bool {
	return c.authenticated
}

// hasAuthenticatedSession reports whether the cookie string carries a
// logged-in XenForo session (xf_user) rather than a guest-only session
// (xf_session/xf_csrf with no xf_user). Purely diagnostic — logged once at
// client creation so block-rate comparisons between authenticated and guest
// sessions are visible in the logs without a special build.
func hasAuthenticatedSession(cookieStr string) bool {
	s := strings.TrimSpace(cookieStr)
	if s == "" {
		return false
	}

	// Netscape format: tab-separated lines with 7 fields, field 5 is the name.
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) >= 7 && fields[5] == "xf_user" {
			return true
		}
	}

	// Cookie header format: semicolon-separated key=value pairs.
	for _, pair := range strings.Split(s, ";") {
		if strings.HasPrefix(strings.TrimSpace(pair), "xf_user=") {
			return true
		}
	}

	return false
}

// Delay returns the current inter-request delay (shared by every client of
// the same host).
func (c *Client) Delay() time.Duration {
	return c.pacer.currentDelay()
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
		// A Cloudflare challenge means the clearance no longer matches
		// (expired, or the browser updated and changed its UA). Mint or
		// re-read one and retry instead of burning the attempt budget.
		if c.tryRefreshClearance(req.Context(), err) {
			attempt--
			continue
		}

		var blockedErr *BlockedError
		retryAfter := time.Duration(0)
		if errors.As(err, &blockedErr) {
			retryAfter = blockedErr.RetryAfter
			if retryAfter > maxBackoff {
				retryAfter = maxBackoff
			}
			// Raise the shared delay immediately, but count the block toward
			// the breaker only once this logical request finally fails, so a
			// retried 403 cannot trip the breaker three times over.
			c.pacer.noteThrottled(blockedErr)
		}

		if attempt == maxAttempts || !retryable(err) {
			if blockedErr != nil {
				c.pacer.noteBlocked(blockedErr, req.URL.Redacted())
			}
			return "", err
		}
		// Pause between attempts. Prefer the server's Retry-After value
		// when present — it's an explicit signal, not a guess; otherwise
		// use the fixed schedule (2s, 4s, ... with jitter) so retries look
		// like a human re-trying rather than a bot hammering.
		wait := retryAfter
		if wait == 0 {
			wait = time.Duration(attempt)*retryBackoffBase + time.Duration(rand.Int63n(int64(time.Second)))
		}
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
	return c.pacer.isBlocked()
}

// Blocked reports whether the client tripped its circuit breaker after
// repeated blocking responses and is refusing further requests. Callers
// can check this to stop scheduling work before each request.
func (c *Client) Blocked() bool {
	return c.isBlocked()
}

func (c *Client) blockedErr() error {
	return &BlockedError{
		Kind:       BlockForbidden,
		Reason:     "repeated blocking responses — session expired or IP temporarily blocked; refresh F95Zone cookies in your browser and try again later",
		StatusCode: 0,
	}
}

// retryable reports whether a failed attempt is worth retrying. Rate limits
// and plain 5xx outages are transient; a challenge or a 403 would only produce
// the same wall again, so the failure is surfaced after one attempt.
func retryable(err error) bool {
	if err == nil {
		return false
	}
	if err == context.Canceled || err == context.DeadlineExceeded {
		return false
	}
	var blockedErr *BlockedError
	if errors.As(err, &blockedErr) {
		switch blockedErr.Kind {
		case BlockChallenge, BlockForbidden:
			return false
		case BlockRateLimit, BlockUnavailable:
			return true
		default:
			return blockedErr.StatusCode >= 500
		}
	}
	// Plain transport/network errors are transient; HTTP 5xx responses
	// are worth a retry, but 4xx are client errors that will only fail
	// again.
	var statusErr *HTTPStatusError
	if errors.As(err, &statusErr) {
		return statusErr.StatusCode >= 500
	}
	return true
}

// paceRequest enforces the client's request pacing before an outgoing request:
// the periodic cooldown, then the minimum inter-request delay (baseDelay, or
// the adaptive delay when larger) plus random jitter. The slot is reserved in
// the client's pacer, which is shared by every client of the host, so
// concurrent callers are spaced out rather than released together.
//
// It returns ctx.Err() when the context is cancelled during a wait. doOnce and
// DownloadToWriter share it so their pacing stays identical.
func (c *Client) paceRequest(ctx context.Context, baseDelay time.Duration, urlStr string) error {
	return c.pacer.wait(ctx, baseDelay, urlStr)
}

// resetAfterSuccess clears the consecutive-block counter and gradually reduces
// the adaptive delay after a successful request.
func (c *Client) resetAfterSuccess() {
	c.pacer.noteSuccess()
}

// doOnce performs a single paced, rate-limited request attempt.
// It reads the response body once and returns it directly.
func (c *Client) doOnce(req *http.Request, baseDelay time.Duration) (bodyStr string, err error) {
	if err := c.paceRequest(req.Context(), baseDelay, req.URL.Redacted()); err != nil {
		return "", err
	}

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
		return "", &HTTPStatusError{StatusCode: resp.StatusCode, Body: bodyStr}
	}

	// Request succeeded — reset the block counter and gradually reduce delay.
	c.resetAfterSuccess()

	return bodyStr, nil
}

// applyBrowserHeaders sets headers matching a real Firefox navigation so
// Cloudflare doesn't flag the request as a bot. Sec-Fetch-* headers are
// checked by modern bot detection and were previously missing.
func applyBrowserHeaders(req *http.Request) {
	req.Header.Set("User-Agent", UserAgent())
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

// parseRetryAfter parses a Retry-After header value per RFC 9110: either an
// integer number of delta-seconds, or an HTTP-date. Returns 0 (meaning "no
// guidance") when the header is absent, empty, unparseable, or would produce
// a non-positive or absurd wait — a hostile/misconfigured header must never
// stall the client; callers cap the result against maxBackoff regardless.
func parseRetryAfter(header string) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}
	if secs, err := strconv.Atoi(header); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(header); err == nil {
		if wait := time.Until(t); wait > 0 {
			return wait
		}
	}
	return 0
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
	cf := resp.Header.Get("cf-mitigated") == "challenge" ||
		strings.Contains(bodyStr, "cf-browser-verification") ||
		strings.Contains(bodyStr, "cf-challenge-running") ||
		strings.Contains(bodyStr, "_cf_chl_opt")

	retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))

	switch resp.StatusCode {
	case 429:
		return "", &BlockedError{Kind: BlockRateLimit, Reason: "rate limited", StatusCode: 429, RetryAfter: retryAfter}
	case 403:
		if cf {
			return "", &BlockedError{
				Kind:       BlockChallenge,
				Reason:     "Cloudflare challenge (HTTP 403) — refresh your browser session and re-import cookies",
				StatusCode: 403,
			}
		}
		return "", &BlockedError{
			Kind:       BlockForbidden,
			Reason:     "access denied (HTTP 403) — possible IP block or missing cookies",
			StatusCode: 403,
		}
	case 503:
		if cf {
			return "", &BlockedError{
				Kind:       BlockChallenge,
				Reason:     "Cloudflare challenge (HTTP 503) — refresh your browser session and re-import cookies",
				StatusCode: 503,
				RetryAfter: retryAfter,
			}
		}
		return "", &BlockedError{
			Kind:       BlockUnavailable,
			Reason:     "service unavailable (HTTP 503)",
			StatusCode: 503,
			RetryAfter: retryAfter,
		}
	}

	if cf {
		return "", &BlockedError{
			Kind:       BlockChallenge,
			Reason:     "Cloudflare challenge page detected — refresh your browser session and re-import cookies",
			StatusCode: resp.StatusCode,
		}
	}

	return bodyStr, nil
}

// DownloadToWriter streams the response body for rawURL into w and returns the
// number of bytes copied.
//
// Unlike do(), it never buffers the body: it is meant for F95Zone attachment
// downloads (which 302 to attachments.f95zone.to with signed query parameters)
// and other large archives. The transfer reuses the client's cookie session
// (host-scoped at the transport level), so rawURL is typically an
// f95zone.to/attachments/... URL.
//
// Pacing is identical to do() — minimum delay, jitter, periodic cooldown, and
// 429 backoff — and block detection is status-code-only: 429/403/503 become a
// *BlockedError, any other non-200 a plain error. An archive is not HTML, so
// the body is never buffered or scanned for Cloudflare markers.
//
// There is no fixed client timeout; callers must apply a deadline via ctx
// (e.g. 15 minutes for save archives). Range/resume is not supported: a failed
// transfer restarts from the beginning (future work).
func (c *Client) DownloadToWriter(ctx context.Context, rawURL string, w io.Writer) (int64, error) {
	if rawURL == "" {
		return 0, fmt.Errorf("scraper: download URL is empty")
	}
	if c.isBlocked() {
		return 0, c.blockedErr()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, fmt.Errorf("scraper: failed to create download request: %w", err)
	}
	urlStr := req.URL.Redacted()

	if err := c.paceRequest(ctx, minDelay, urlStr); err != nil {
		return 0, err
	}

	applyBrowserHeaders(req)

	// Use a dedicated client: c.http has a 10s total timeout, far too short
	// for a save archive. Cancellation comes from ctx instead.
	resp, err := c.downloadClient().Do(req)
	if err != nil {
		return 0, fmt.Errorf("scraper: download request failed: %w", err)
	}
	defer resp.Body.Close()

	// Status-code-only block detection: never read the body for markers.
	if blockedErr := blockErrorForStatus(resp); blockedErr != nil {
		return 0, c.noteBlocked(blockedErr, urlStr)
	}
	if resp.StatusCode != http.StatusOK {
		return 0, &HTTPStatusError{StatusCode: resp.StatusCode}
	}

	n, err := io.Copy(w, resp.Body)
	if err != nil {
		// Surface the partial byte count so callers can report/resume later.
		return n, fmt.Errorf("scraper: download stream failed after %d bytes: %w", n, err)
	}

	c.resetAfterSuccess()
	return n, nil
}

// downloadClient returns an http.Client that mirrors c.http's transport,
// redirect policy, and cookie jar but has no overall timeout, so large
// transfers are not aborted mid-stream. Callers cancel via the request context.
func (c *Client) downloadClient() *http.Client {
	return &http.Client{
		Transport:     c.http.Transport,
		CheckRedirect: c.http.CheckRedirect,
		Jar:           c.http.Jar,
	}
}

// blockErrorForStatus maps an HTTP status code to a *BlockedError using the
// same status semantics as checkBlocked, but without reading or scanning the
// response body — safe for streaming non-HTML downloads. Retry-After is
// honoured for 429/503 exactly as checkBlocked honours it.
func blockErrorForStatus(resp *http.Response) *BlockedError {
	retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		return &BlockedError{Kind: BlockRateLimit, Reason: "rate limited", StatusCode: http.StatusTooManyRequests, RetryAfter: retryAfter}
	case http.StatusForbidden:
		return &BlockedError{
			Kind:       BlockForbidden,
			Reason:     "access denied (HTTP 403) — possible IP block or missing cookies",
			StatusCode: http.StatusForbidden,
		}
	case http.StatusServiceUnavailable:
		return &BlockedError{
			Kind:       BlockUnavailable,
			Reason:     "service unavailable (HTTP 503)",
			StatusCode: http.StatusServiceUnavailable,
			RetryAfter: retryAfter,
		}
	}
	return nil
}

// noteBlocked logs a blocked download and applies the same bookkeeping do()
// performs for blocked responses (shared circuit breaker, 429 backoff). It
// returns blockedErr so callers can chain it.
func (c *Client) noteBlocked(blockedErr *BlockedError, urlStr string) error {
	log.Warn("request blocked",
		"status", blockedErr.StatusCode,
		"reason", blockedErr.Reason,
		"url", urlStr,
	)
	c.pacer.noteThrottled(blockedErr)
	c.pacer.noteBlocked(blockedErr, urlStr)
	return blockedErr
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
	if c.SessionExpired() {
		return nil, &SessionError{HadCookie: c.authenticated}
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

	// Detect an expired F95 session: an authenticated client receiving a
	// guest-rendered page means the imported cookies no longer log in.
	if c.authenticated && pageSession(body) == SessionGuest {
		td.LinksHidden = true
		c.mu.Lock()
		c.sessionExpired = true
		c.mu.Unlock()
		return td, &SessionError{HadCookie: true}
	}

	return td, nil
}

// SessionExpired reports whether this client has seen a guest-rendered page
// while authenticated (i.e. the imported F95 session has expired).
func (c *Client) SessionExpired() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionExpired
}

// IsSessionExpired is an alias for SessionExpired.
func (c *Client) IsSessionExpired() bool {
	return c.SessionExpired()
}

// MarkSessionExpired records that the session has expired.
func (c *Client) MarkSessionExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionExpired = true
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
	mu          sync.RWMutex
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

// setCookie replaces the session cookie (after a clearance refresh).
func (ct *cookieTransport) setCookie(v string) {
	ct.mu.Lock()
	ct.cookieValue = strings.TrimSpace(v)
	ct.mu.Unlock()
}

func (ct *cookieTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ct.mu.RLock()
	cookieValue := ct.cookieValue
	ct.mu.RUnlock()
	if cookieValue != "" && (ct.unrestricted || f95ZoneHost(req)) {
		req = req.Clone(req.Context())
		if existing := req.Header.Get("Cookie"); existing != "" {
			req.Header.Set("Cookie", existing+"; "+cookieValue)
		} else {
			req.Header.Set("Cookie", cookieValue)
		}
	}
	return ct.inner.RoundTrip(req)
}
