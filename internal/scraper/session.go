package scraper

import (
	"context"
	"errors"
	"sync"

	"github.com/mili/moxie/internal/log"
)

// Session hooks wired by internal/f95session at startup. They stay nil in
// tests, so the scraper never reads real browser stores or launches a
// browser from a unit test.
var sessionHooks struct {
	mu        sync.RWMutex
	userAgent func() string
	refresh   func(ctx context.Context) (cookie string, err error)
}

// SetUserAgentProvider installs the source of the User-Agent sent with F95Zone
// requests. Cloudflare binds cf_clearance to the UA that solved its challenge,
// so the provider must report the UA matching the cookies in use; "" falls
// back to the built-in default.
func SetUserAgentProvider(fn func() string) {
	sessionHooks.mu.Lock()
	sessionHooks.userAgent = fn
	sessionHooks.mu.Unlock()
}

// SetClearanceRefresher installs the hook a client calls once when F95Zone
// answers with a Cloudflare challenge. It returns a fresh Cookie header whose
// cf_clearance matches the (possibly changed) User-Agent provider; the client
// swaps it in and retries instead of tripping its circuit breaker.
func SetClearanceRefresher(fn func(ctx context.Context) (string, error)) {
	sessionHooks.mu.Lock()
	sessionHooks.refresh = fn
	sessionHooks.mu.Unlock()
}

// UserAgent returns the User-Agent F95Zone requests are sent with.
func UserAgent() string {
	sessionHooks.mu.RLock()
	fn := sessionHooks.userAgent
	sessionHooks.mu.RUnlock()
	if fn != nil {
		if ua := fn(); ua != "" {
			return ua
		}
	}
	return platformUserAgent()
}

func clearanceRefresher() func(ctx context.Context) (string, error) {
	sessionHooks.mu.RLock()
	defer sessionHooks.mu.RUnlock()
	return sessionHooks.refresh
}

// IsCloudflareChallenge reports whether err is a Cloudflare challenge block —
// a session-level failure (stale or UA-mismatched cf_clearance) that no other
// thread or mirror would get past.
func IsCloudflareChallenge(err error) bool {
	var b *BlockedError
	return errors.As(err, &b) && b.Kind == BlockChallenge
}

// tryRefreshClearance runs the clearance refresher once per client after a
// Cloudflare challenge. It reports whether the request is worth retrying.
func (c *Client) tryRefreshClearance(ctx context.Context, err error) bool {
	if !IsCloudflareChallenge(err) {
		return false
	}
	refresh := clearanceRefresher()
	if refresh == nil {
		return false
	}
	c.mu.Lock()
	if c.clearanceRefreshed {
		c.mu.Unlock()
		return false
	}
	c.clearanceRefreshed = true
	c.mu.Unlock()

	cookie, rerr := refresh(ctx)
	if rerr != nil || cookie == "" {
		log.Warn("Cloudflare challenge: clearance refresh failed", "error", rerr)
		return false
	}
	if ct, ok := c.http.Transport.(*cookieTransport); ok {
		ct.setCookie(cookie)
	}
	c.pacer.resetBreaker()
	c.mu.Lock()
	c.csrfToken = extractCSRFToken(cookie)
	c.mu.Unlock()
	log.Info("Cloudflare challenge: clearance refreshed; retrying")
	return true
}
