package scraper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
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
	defaultTimeout   = 10 * time.Second
	minDelay         = 3 * time.Second      // minimum between requests
	maxJitter        = 2 * time.Second      // random extra delay
	searchMinDelay   = 5 * time.Second      // search is more expensive, be gentler
	cooldownInterval = 25                   // pause longer every N requests
	cooldownDuration = 15 * time.Second     // length of cooldown pause
	backoffMultiplier = 2                   // multiply delay on rate limit
	maxBackoff       = 2 * time.Minute      // cap exponential backoff
)

// platformUserAgent returns a Firefox User-Agent string appropriate for the
// current OS, so requests blend in regardless of where the binary runs.
func platformUserAgent() string {
	switch runtime.GOOS {
	case "windows":
		return "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:136.0) Gecko/20100101 Firefox/136.0"
	case "darwin":
		return "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:136.0) Gecko/20100101 Firefox/136.0"
	default:
		return "Mozilla/5.0 (X11; Linux x86_64; rv:136.0) Gecko/20100101 Firefox/136.0"
	}
}

// BlockedError indicates the request was blocked by anti-bot protection.
type BlockedError struct {
	Reason string
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
				inner:       inner,
				cookieValue: strings.TrimSpace(cookieStr),
			},
			CheckRedirect: httpClient.CheckRedirect,
			Jar:           httpClient.Jar,
		},
		delay: delay,
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
		delay: delay,
	}
}

// Delay returns the current inter-request delay.
func (c *Client) Delay() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.delay
}

// do sends an HTTP request with rate limiting, bot detection, and
// exponential backoff on rate-limit responses.
// It reads the response body once and returns it directly, avoiding
// a second read in the caller.
// baseDelay is the minimum delay between requests (use searchMinDelay for search).
func (c *Client) do(req *http.Request, baseDelay time.Duration) (bodyStr string, err error) {
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

	req.Header.Set("User-Agent", platformUserAgent())
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("scraper: request failed: %w", err)
	}

	// Check for blocking signals and read the body.
	bodyStr, err = c.checkBlocked(resp)
	resp.Body.Close()
	if err != nil {
		// Log blocked events and apply 429 backoff.
		var blockedErr *BlockedError
		if errors.As(err, &blockedErr) {
			log.Warn("request blocked",
				"status", blockedErr.StatusCode,
				"reason", blockedErr.Reason,
				"url", req.URL.Redacted(),
			)
			if blockedErr.StatusCode == 429 {
				c.mu.Lock()
				c.delay = time.Duration(float64(c.delay) * backoffMultiplier)
				if c.delay > maxBackoff {
					c.delay = maxBackoff
				}
				log.Warn("rate limited, backing off",
					"new_delay", c.delay,
					"url", req.URL.Redacted(),
				)
				c.mu.Unlock()
			}
		}
		return "", err
	}

	// Check for non-OK status codes (429/403/503 already caught above).
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("scraper: unexpected status %d %s", resp.StatusCode, resp.Status)
	}

	// Gradually reduce delay on success.
	c.mu.Lock()
	c.delay = time.Duration(float64(c.delay) * 0.95)
	if c.delay < minDelay {
		c.delay = minDelay
	}
	c.mu.Unlock()

	return bodyStr, nil
}

// checkBlocked inspects the response for Cloudflare or anti-bot signals.
// It reads the full response body once and returns it so callers can use
// the body directly without a second read.
func (c *Client) checkBlocked(resp *http.Response) (bodyStr string, err error) {
	switch resp.StatusCode {
	case 429:
		return "", &BlockedError{Reason: "rate limited", StatusCode: 429}
	case 403:
		return "", &BlockedError{
			Reason:     "access denied (HTTP 403) — possible IP block or missing cookies",
			StatusCode: 403,
		}
	case 503:
		return "", &BlockedError{
			Reason:     "service unavailable (HTTP 503) — possible Cloudflare challenge",
			StatusCode: 503,
		}
	}

	// Read the full body and check for Cloudflare markers.
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return "", fmt.Errorf("scraper: failed to read response body: %w", err)
	}

	bodyStr = string(body)
	if strings.Contains(bodyStr, "cf-browser-verification") ||
		strings.Contains(bodyStr, "cf-challenge-running") ||
		strings.Contains(bodyStr, "_cf_chl_opt") {
		return "", &BlockedError{
			Reason:     "Cloudflare challenge page detected — refresh your browser session and re-import cookies",
			StatusCode: resp.StatusCode,
		}
	}

	return bodyStr, nil
}

// ScrapeThread fetches and parses a XenForo thread page.
func (c *Client) ScrapeThread(threadURL string) (*ThreadData, error) {
	return c.ScrapeThreadWithContext(context.Background(), threadURL)
}

// ScrapeThreadWithContext fetches and parses a XenForo thread page,
// respecting the given context for cancellation and deadlines.
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
// cookieTransport injects a raw Cookie header on every outgoing request.
// ---------------------------------------------------------------------------

type cookieTransport struct {
	inner       http.RoundTripper
	cookieValue string
}

func (ct *cookieTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if ct.cookieValue != "" {
		if existing := req.Header.Get("Cookie"); existing != "" {
			req.Header.Set("Cookie", existing+"; "+ct.cookieValue)
		} else {
			req.Header.Set("Cookie", ct.cookieValue)
		}
	}
	return ct.inner.RoundTrip(req)
}
