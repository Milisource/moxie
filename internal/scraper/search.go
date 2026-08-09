package scraper

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/mili/moxie/internal/log"
)

// SearchResult represents a single search result from F95Zone.
type SearchResult struct {
	Title        string  `json:"title"`
	URL          string  `json:"url"`
	Snippet      string  `json:"snippet,omitempty"`
	ThumbnailURL string  `json:"thumbnailUrl,omitempty"`
	Score        float64 `json:"score"`
}

// xfTokenTTL bounds how long a fetched _xfToken is reused. Tokens are
// stable for the session, but sessions rotate — re-fetch periodically so
// stale tokens don't pile up 400s on long sync runs.
const xfTokenTTL = 10 * time.Minute

// xfTokenPageURL is the default page fetched to obtain a fresh _xfToken.
// XenForo 2.3+ embeds the current session token on every page (in a
// hidden _xfToken input); the value in the xf_csrf cookie is the legacy
// format and is rejected with HTTP 400 by POST endpoints.
const xfTokenPageURL = "https://f95zone.to/"

// xfSearchURL is the default XenForo search POST endpoint.
const xfSearchURL = "https://f95zone.to/search/search"

// googleSearchURL is the default Google SERP used by the cookie-free
// fallback when the XenForo search is unavailable.
const googleSearchURL = "https://www.google.com/search"

// xfTokenRe matches XenForo's embedded CSRF token in page HTML.
var xfTokenRe = regexp.MustCompile(`name="_xfToken"\s+value="([^"]+)"`)

// threadIDRe matches the trailing numeric thread ID in a XenForo thread
// URL. Handles both the slug form (/threads/slug.12345/) and the
// slug-agnostic form (/threads/12345/).
var threadIDRe = regexp.MustCompile(`/threads/[^/]*?\.?(\d+)/$`)

// ThreadIDFromURL extracts the numeric thread ID from a XenForo thread
// URL, or 0 when the URL isn't a thread link.
func ThreadIDFromURL(u string) int64 {
	m := threadIDRe.FindStringSubmatch(u)
	if len(m) != 2 {
		return 0
	}
	id, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

// SearchF95Zone searches F95Zone for game threads matching the query.
// Returns up to 5 results sorted by relevance.
//
// Uses XenForo's POST /search/search with a fresh _xfToken fetched from a
// page (the legacy xf_csrf cookie value is rejected by current XenForo).
// Falls back to Google site: search if the token is unavailable or the
// POST request fails with a block.
func (c *Client) SearchF95Zone(query string) ([]SearchResult, error) {
	return c.SearchF95ZoneWithContext(context.Background(), query)
}

// SearchF95ZoneWithContext searches F95Zone for game threads matching the query,
// respecting the given context for cancellation and deadlines.
func (c *Client) SearchF95ZoneWithContext(ctx context.Context, query string) ([]SearchResult, error) {
	if query == "" {
		return nil, fmt.Errorf("scraper: search query is empty")
	}

	// Primary method: XenForo POST search with a fresh _xfToken.
	token := c.xfToken(ctx)
	if token != "" {
		results, err := c.xfSearchPOST(ctx, query, token)
		if err == nil {
			return results, nil
		}
		// A 400 from the POST endpoint means the token was rejected
		// (stale/rotated session) — refetch once before falling back.
		if isXFTokenRejected(err) {
			c.invalidateXFToken()
			if fresh := c.xfToken(ctx); fresh != "" && fresh != token {
				if results, err2 := c.xfSearchPOST(ctx, query, fresh); err2 == nil {
					return results, nil
				}
			}
		}
		// If blocked, log and fall through to Google fallback.
		var blockedErr *BlockedError
		if errors.As(err, &blockedErr) {
			log.Warn("xf_search blocked, falling back to Google",
				"query", query,
				"reason", blockedErr.Reason,
			)
		} else {
			log.Debug("xf_search failed, falling back to Google",
				"query", query,
				"error", err,
			)
		}
	} else {
		log.Debug("no _xfToken available, using Google fallback",
			"query", query,
		)
	}

	// Fallback: Google site search — no cookies or CSRF needed.
	return c.googleSiteSearch(ctx, query)
}

// isXFTokenRejected reports whether a search POST error means the CSRF
// token was rejected. XenForo answers bad tokens with HTTP 400 and a
// "Security error occurred" page; other statuses are handled by the
// retry/block machinery in do.
func isXFTokenRejected(err error) bool {
	var statusErr *HTTPStatusError
	return errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusBadRequest
}

// xfToken returns a valid _xfToken for authenticated POSTs: a fresh token
// fetched from page HTML (XenForo 2.3+ format), cached for xfTokenTTL,
// falling back to the legacy xf_csrf cookie value only when the page
// fetch fails — that value is rejected by current XenForo, so the caller
// will fall through to the Google fallback.
func (c *Client) xfToken(ctx context.Context) string {
	c.mu.Lock()
	fresh := c.xfTokenValue != "" && time.Since(c.xfTokenAt) < xfTokenTTL
	c.mu.Unlock()
	if fresh {
		return c.xfTokenValue
	}

	token := c.fetchXFToken(ctx)
	if token != "" {
		c.mu.Lock()
		c.xfTokenValue = token
		c.xfTokenAt = time.Now()
		c.mu.Unlock()
		return token
	}
	return c.csrfToken
}

// fetchXFToken GETs a page and extracts the embedded _xfToken.
func (c *Client) fetchXFToken(ctx context.Context) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.xfTokenPage, nil)
	if err != nil {
		return ""
	}
	baseDelay := searchMinDelay
	if c.unsafe {
		baseDelay = 0
	}
	body, err := c.do(req, baseDelay)
	if err != nil {
		return ""
	}
	m := xfTokenRe.FindStringSubmatch(body)
	if len(m) != 2 || m[1] == "" {
		return ""
	}
	return m[1]
}

// invalidateXFToken drops the cached token so the next call re-fetches.
func (c *Client) invalidateXFToken() {
	c.mu.Lock()
	c.xfTokenValue = ""
	c.xfTokenAt = time.Time{}
	c.mu.Unlock()
}

// xfSearchPOST performs a XenForo search via POST /search/search with
// a fresh _xfToken. This is the canonical search method and requires a
// valid authenticated session.
func (c *Client) xfSearchPOST(ctx context.Context, query, token string) ([]SearchResult, error) {
	form := url.Values{}
	form.Set("_xfToken", token)
	form.Set("keywords", query)
	form.Set("c[title_only]", "1")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.xfSearchURL,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("scraper: failed to create search request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	baseDelay := searchMinDelay
	if c.unsafe {
		baseDelay = 0
	}
	body, err := c.do(req, baseDelay)
	if err != nil {
		return nil, err
	}

	return parseSearchResults(body, query), nil
}

// googleSiteSearch searches F95Zone via Google's site: operator.
// This is a cookie-free fallback when the XenForo search is unavailable.
// Search results are parsed from the Google SERP HTML.
func (c *Client) googleSiteSearch(ctx context.Context, query string) ([]SearchResult, error) {
	searchURL := fmt.Sprintf("%s?q=site%%3Af95zone.to+%%22%s%%22&hl=en",
		c.googleSearchURL, url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("scraper: failed to create Google search request: %w", err)
	}

	baseDelay := searchMinDelay
	if c.unsafe {
		baseDelay = 0
	}
	body, err := c.do(req, baseDelay)
	if err != nil {
		return nil, fmt.Errorf("scraper: Google search failed: %w", err)
	}

	return parseGoogleResults(body, query), nil
}

// parseSearchResults parses the XenForo 2.x search results HTML.
// It iterates over each .contentRow and extracts the title, URL, and snippet.
// Results are scored against the query, sorted by relevance, and trimmed to 5.
func parseSearchResults(html string, query string) []SearchResult {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}

	var results []SearchResult
	doc.Find(".contentRow").Each(func(i int, row *goquery.Selection) {
		if i >= 20 {
			return
		}

		link := row.Find("h3.contentRow-title a").First()
		href, ok := link.Attr("href")
		if !ok {
			return
		}
		title := strings.TrimSpace(link.Text())
		if title == "" {
			return
		}

		// Resolve relative URLs to absolute.
		resultURL := href
		if strings.HasPrefix(href, "/") {
			resultURL = "https://f95zone.to" + href
		}

		var snippet string
		if s := row.Find(".contentRow-snippet").First(); s.Length() > 0 {
			snippet = strings.TrimSpace(s.Text())
		}

		// No thumbnail here on purpose: the only image on a XenForo
		// search result row is the poster's avatar (.contentRow-figure),
		// which makes a misleading game thumbnail. Callers that need
		// cover art attach it from the F95Checker catalog (see
		// PublicAPI.SearchCovers), keyed by thread ID.

		results = append(results, SearchResult{
			Title:   title,
			URL:     resultURL,
			Snippet: snippet,
		})
	})

	// Score results by relevance to the query and sort descending.
	for i := range results {
		results[i].Score = ComputeMatchScore(query, results[i].Title)
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if len(results) > 5 {
		results = results[:5]
	}

	return results
}

// parseGoogleResults parses Google search results HTML for F95Zone links.
// Selectors target the current Google SERP structure for site: queries.
// This is best-effort and may need updating if Google changes its markup.
// Results are scored against the query, sorted by relevance, and trimmed to 5.
func parseGoogleResults(html string, query string) []SearchResult {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}

	var results []SearchResult

	// Google search result containers use various selectors depending on
	// SERP layout version. Try multiple strategies.
	doc.Find("a[href*='f95zone.to']").Each(func(i int, a *goquery.Selection) {
		if i >= 20 {
			return
		}

		href, ok := a.Attr("href")
		if !ok {
			return
		}

		// Google wraps result URLs: /url?q=<actual-url>&...
		actualURL := href
		if strings.HasPrefix(href, "/url?q=") {
			parts := strings.SplitN(href[7:], "&", 2)
			if len(parts) > 0 {
				actualURL, _ = url.QueryUnescape(parts[0])
			}
		}

		// Only keep F95Zone thread URLs.
		if !strings.Contains(actualURL, "f95zone.to/threads/") {
			return
		}

		// Extract title from the nearest h3.
		title := strings.TrimSpace(a.Find("h3").First().Text())
		if title == "" {
			title = strings.TrimSpace(a.Text())
			// Clean up noise from link text.
			if idx := strings.Index(title, "\n"); idx >= 0 {
				title = strings.TrimSpace(title[:idx])
			}
		}
		if title == "" {
			return
		}

		// Deduplicate by URL.
		for _, r := range results {
			if r.URL == actualURL {
				return
			}
		}

		// Extract thumbnail from Google SERP result.
		var thumbURL string
		container := a.Closest("div.g, div[jscontroller]")
		if container.Length() == 0 {
			container = a.ParentsFiltered("div").First()
		}
		if img := container.Find("img").First(); img.Length() > 0 {
			if src, ok := img.Attr("src"); ok && src != "" {
				if !strings.HasPrefix(src, "data:image") {
					thumbURL = src
				}
			} else if src, ok := img.Attr("data-src"); ok && src != "" {
				thumbURL = src
			}
		}

		results = append(results, SearchResult{
			Title:        title,
			URL:          actualURL,
			ThumbnailURL: thumbURL,
		})
	})

	// Score results by relevance to the query and sort descending.
	for i := range results {
		results[i].Score = ComputeMatchScore(query, results[i].Title)
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if len(results) > 5 {
		results = results[:5]
	}

	return results
}
