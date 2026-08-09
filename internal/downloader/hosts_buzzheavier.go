package downloader

import (
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/mili/moxie/internal/log"
)

// --- Buzzheavier ---
// Token-based resolution (live-verified 2026-08-09, F95-hs4y). The wall is
// NOT a TLS fingerprint and NOT a Turnstile proof: the share page embeds a
// server-signed t= token in its hx-get attributes, and the HTMX /download
// endpoint accepts it from a plain stdlib client. Flow:
//
//  1. GET the share page (browser cookies required; retry through
//     intermittent adaptive 403s — backoff ~1s, cap 5 tries).
//  2. Extract t= from hx-get="/<id>/download?t=<token>" (the &amp;-encoded
//     &alt=true variant carries the same token).
//  3. GET /download?t=<token>&alt=true with HX-Request + priority: u=1, i
//     + Referer → hx-redirect. alt=true yields fafda.to/d/<id>?v=<token>
//     (challenge-free app-layer token gate); the non-alt ts.bzzhr.to origin
//     is dead, so its redirects are rewritten to fafda.to.
//  4. Probe the file URL (Range: bytes=0-0): 503 {"error":"service
//     unavailable"} is a transient origin outage (retry/backoff); 403/404
//     JSON errors mean an expired/forged token → re-fetch the page for a
//     fresh one (bounded).
//
// The old dd.buzzheavier.com direct pattern is dead (521/522).

const (
	buzzUserAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0"
)

// Per-stage retry caps and backoff (adaptive 403s on the page, origin 503s
// on the probe). Vars so tests can shrink them.
var (
	buzzMaxTries   = 5
	buzzRetryDelay = time.Second
	// buzzProbeBackoff is the escalating backoff between file-probe retries:
	// the fafda.to origin's 503 outages are intermittent (live-observed
	// recovering within a minute), so a flat 1s×5 window misses them. Empty
	// falls back to buzzRetryDelay; probe tries = len(backoff) + 1.
	buzzProbeBackoff = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second}
	// buzzMaxPageRefetch bounds how many times a rejected token re-fetches
	// the share page for a fresh one, so a broken share link cannot loop.
	buzzMaxPageRefetch = 2
)

// reBuzzHxGet matches the hx-get attributes that carry the download
// endpoint; the server-signed t= token lives in their query string.
// HTML-entity encoded &amp; is unescaped before parsing.
var reBuzzHxGet = regexp.MustCompile(`hx-get=["']([^"']*/download\?[^"']*)["']`)

// errBuzzTokenRejected signals a 403/404 JSON error from the file origin:
// the t= token is expired or forged — re-fetch the share page for a new one.
var errBuzzTokenRejected = errors.New("buzzheavier: token rejected by file origin")

func (r *HostResolver) resolveBuzzheavier(rawURL string) (*ResolveResult, error) {
	pageURL := strings.TrimRight(rawURL, "/")

	for attempt := 1; attempt <= buzzMaxPageRefetch; attempt++ {
		body, err := r.buzzFetchSharePage(pageURL)
		if err != nil {
			return nil, err
		}

		token, endpoint, err := extractBuzzDownloadHref(body)
		if err != nil {
			return nil, err
		}

		fileURL, err := r.buzzRequestDownload(pageURL, endpoint, token)
		if err != nil {
			return nil, err
		}

		fileURL, err = r.buzzProbeFile(fileURL)
		if errors.Is(err, errBuzzTokenRejected) {
			log.Debug("buzzheavier token rejected, re-fetching share page", "url", redactedURL(pageURL))
			continue
		}
		if err != nil {
			return nil, err
		}

		return &ResolveResult{
			URL: fileURL,
			Headers: map[string]string{
				"User-Agent": buzzUserAgent,
				"Referer":    pageURL,
			},
		}, nil
	}
	return nil, fmt.Errorf(
		"buzzheavier: token rejected after %d share-page refetches — the share link is expired or invalid",
		buzzMaxPageRefetch,
	)
}

// buzzFetchSharePage GETs the share page with browser cookies and retries
// through the intermittent adaptive 403s Cloudflare serves between 200s.
// Only 403 (and transport errors) are retried — 404/5xx are permanent share
// problems.
func (r *HostResolver) buzzFetchSharePage(pageURL string) ([]byte, error) {
	var lastErr error
	for attempt := 1; attempt <= buzzMaxTries; attempt++ {
		req, err := http.NewRequest(http.MethodGet, pageURL, nil)
		if err != nil {
			return nil, fmt.Errorf("buzzheavier create page request: %w", err)
		}
		req.Header.Set("User-Agent", buzzUserAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		r.attachBrowserCookies(req)

		resp, err := r.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("buzzheavier page GET: %w", err)
			time.Sleep(buzzRetryDelay)
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			return body, nil
		}
		lastErr = fmt.Errorf("buzzheavier page: HTTP %d", resp.StatusCode)
		if readErr != nil {
			lastErr = fmt.Errorf("buzzheavier read page body: %w", readErr)
		}
		if resp.StatusCode != http.StatusForbidden {
			return nil, lastErr
		}
		time.Sleep(buzzRetryDelay)
	}
	return nil, fmt.Errorf("buzzheavier: share page still challenged after %d attempts: %w", buzzMaxTries, lastErr)
}

// extractBuzzDownloadHref pulls the /download endpoint and its server-signed
// t= token from the share page's hx-get attributes. The endpoint is rebuilt
// deterministically with alt=true (the fafda.to backend), regardless of which
// variant the page embeds — all variants carry the same token.
func extractBuzzDownloadHref(body []byte) (token string, endpoint string, err error) {
	for _, m := range reBuzzHxGet.FindAllSubmatch(body, -1) {
		u, parseErr := url.Parse(html.UnescapeString(string(m[1])))
		if parseErr != nil {
			continue
		}
		tok := u.Query().Get("t")
		if tok == "" {
			continue
		}
		q := url.Values{}
		q.Set("t", tok)
		q.Set("alt", "true")
		return tok, u.Path + "?" + q.Encode(), nil
	}
	return "", "", fmt.Errorf("buzzheavier: no t= token in share page (hx-get download attribute missing)")
}

// buzzRequestDownload performs the HTMX GET /download?t=<token>&alt=true and
// returns the hx-redirect file URL, preferring the fafda.to origin. The
// non-alt backend ts.bzzhr.to is dead (000 / wrong DNS), so its redirects —
// which carry the same v= token — are rewritten to fafda.to.
func (r *HostResolver) buzzRequestDownload(pageURL, endpoint, token string) (string, error) {
	dlURL := resolveRelativeURL(pageURL, endpoint)
	req, err := http.NewRequest(http.MethodGet, dlURL, nil)
	if err != nil {
		return "", fmt.Errorf("buzzheavier create download request: %w", err)
	}
	req.Header.Set("User-Agent", buzzUserAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Current-URL", pageURL)
	req.Header.Set("priority", "u=1, i")
	req.Header.Set("Referer", pageURL)
	r.attachBrowserCookies(req)

	resp, err := r.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("buzzheavier download request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("buzzheavier /download: HTTP %d", resp.StatusCode)
	}

	redirect := resp.Header.Get("hx-redirect")
	if redirect == "" {
		// Some deployments return the URL in the body instead.
		if body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10)); len(body) > 0 {
			redirect = strings.TrimSpace(string(body))
		}
	}
	if redirect == "" {
		return "", fmt.Errorf("buzzheavier /download: empty hx-redirect (no token was issued)")
	}

	u, err := url.Parse(redirect)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("buzzheavier /download: malformed hx-redirect %q", redirect)
	}
	if strings.HasPrefix(u.Hostname(), "ts.") || strings.HasPrefix(u.Hostname(), "dd.") {
		u.Host = "fafda.to"
	}
	if u.Scheme == "" {
		u.Scheme = "https"
	}
	return u.String(), nil
}

// buzzProbeFile validates the resolved file URL with a 1-byte ranged GET:
// 503 {"error":"service unavailable"} is a transient origin outage — retried
// with an escalating backoff. 403/404 JSON errors mean the v= token is
// expired or forged — returned as errBuzzTokenRejected so the caller
// re-fetches the share page.
func (r *HostResolver) buzzProbeFile(fileURL string) (string, error) {
	probeTries := buzzMaxTries
	if len(buzzProbeBackoff) > 0 {
		probeTries = len(buzzProbeBackoff) + 1
	}
	var lastErr error
	for attempt := 1; attempt <= probeTries; attempt++ {
		req, err := http.NewRequest(http.MethodGet, fileURL, nil)
		if err != nil {
			return "", fmt.Errorf("buzzheavier create file probe: %w", err)
		}
		req.Header.Set("User-Agent", buzzUserAgent)
		req.Header.Set("Accept", "*/*")
		req.Header.Set("Range", "bytes=0-0")

		resp, err := r.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("buzzheavier file probe: %w", err)
			time.Sleep(buzzBackoff(attempt))
			continue
		}
		// Drain only a little — a server that ignores Range would otherwise
		// stream the whole file into memory.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		resp.Body.Close()

		switch resp.StatusCode {
		case http.StatusPartialContent, http.StatusOK:
			return fileURL, nil
		case http.StatusServiceUnavailable:
			lastErr = fmt.Errorf("buzzheavier file origin unavailable (HTTP 503): %s", strings.TrimSpace(string(body)))
			time.Sleep(buzzBackoff(attempt))
		case http.StatusForbidden, http.StatusNotFound:
			return "", fmt.Errorf("buzzheavier file probe: HTTP %d — %s: %w",
				resp.StatusCode, strings.TrimSpace(string(body)), errBuzzTokenRejected)
		default:
			return "", fmt.Errorf("buzzheavier file probe: HTTP %d", resp.StatusCode)
		}
	}
	return "", lastErr
}

// buzzBackoff returns the sleep before retry attempt n (1-based): the probe's
// escalating schedule when set, else the flat default.
func buzzBackoff(attempt int) time.Duration {
	if attempt <= len(buzzProbeBackoff) {
		return buzzProbeBackoff[attempt-1]
	}
	return buzzRetryDelay
}
