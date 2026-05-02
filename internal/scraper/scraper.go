package scraper

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// User-Agent must match the browser that generated the cf_clearance cookie.
	// Since we read cookies from Firefox via kooky, use a Firefox UA.
	defaultUserAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:136.0) Gecko/20100101 Firefox/136.0"
	defaultTimeout   = 10 * time.Second
	minDelay         = 3 * time.Second      // minimum between requests
	maxJitter        = 2 * time.Second      // random extra delay
	searchMinDelay   = 5 * time.Second      // search is more expensive, be gentler
	cooldownInterval = 25                   // pause longer every N requests
	cooldownDuration = 15 * time.Second     // length of cooldown pause
	backoffMultiplier = 2                   // multiply delay on rate limit
	maxBackoff       = 2 * time.Minute      // cap exponential backoff
)

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
// baseDelay is the minimum delay between requests (use searchMinDelay for search).
func (c *Client) do(req *http.Request, baseDelay time.Duration) (*http.Response, error) {
	c.mu.Lock()

	// Periodic cooldown — pause longer every N requests to look human.
	if c.reqCount > 0 && c.reqCount%cooldownInterval == 0 {
		c.mu.Unlock()
		select {
		case <-time.After(cooldownDuration):
		case <-req.Context().Done():
			return nil, req.Context().Err()
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
			return nil, req.Context().Err()
		}
		c.mu.Lock()
	}
	start := time.Now()
	c.lastRequest = start
	c.reqCount++
	c.mu.Unlock()

	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("scraper: request failed: %w", err)
	}

	// Check for blocking signals.
	if err := c.checkBlocked(resp); err != nil {
		resp.Body.Close()
		return nil, err
	}

	// Adapt delay on rate-limiting responses.
	if resp.StatusCode == 429 {
		c.mu.Lock()
		c.delay = time.Duration(float64(c.delay) * backoffMultiplier)
		if c.delay > maxBackoff {
			c.delay = maxBackoff
		}
		c.mu.Unlock()
		resp.Body.Close()
		return nil, &BlockedError{
			Reason:     "rate limited (HTTP 429)",
			StatusCode: resp.StatusCode,
		}
	}

	// Gradually reduce delay on success.
	c.mu.Lock()
	c.delay = time.Duration(float64(c.delay) * 0.95)
	if c.delay < minDelay {
		c.delay = minDelay
	}
	c.mu.Unlock()

	return resp, nil
}

// checkBlocked inspects the response for Cloudflare or anti-bot signals.
// It reads the full response body and replaces it with a fresh reader so
// callers can still consume the body afterward.
func (c *Client) checkBlocked(resp *http.Response) error {
	switch resp.StatusCode {
	case 429:
		return &BlockedError{Reason: "rate limited", StatusCode: 429}
	case 403:
		return &BlockedError{
			Reason:     "access denied (HTTP 403) — possible IP block or missing cookies",
			StatusCode: 403,
		}
	case 503:
		return &BlockedError{
			Reason:     "service unavailable (HTTP 503) — possible Cloudflare challenge",
			StatusCode: 503,
		}
	}

	// Read the full body, check for Cloudflare markers, then put it back.
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return fmt.Errorf("scraper: failed to read response body: %w", err)
	}

	bodyStr := string(body)
	if strings.Contains(bodyStr, "cf-browser-verification") ||
		strings.Contains(bodyStr, "cf-challenge-running") ||
		strings.Contains(bodyStr, "_cf_chl_opt") {
		return &BlockedError{
			Reason:     "Cloudflare challenge page detected — refresh your browser session and re-import cookies",
			StatusCode: resp.StatusCode,
		}
	}

	// Replace body with a fresh reader so callers can consume it.
	resp.Body = io.NopCloser(strings.NewReader(bodyStr))
	return nil
}

// ScrapeThread fetches and parses a XenForo thread page.
func (c *Client) ScrapeThread(threadURL string) (*ThreadData, error) {
	if threadURL == "" {
		return nil, fmt.Errorf("scraper: threadURL is empty")
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, threadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("scraper: failed to create request: %w", err)
	}

	resp, err := c.do(req, minDelay)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scraper: unexpected status %d %s", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("scraper: failed to read response body: %w", err)
	}

	td, err := parseThreadHTML(string(body), threadURL)
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
