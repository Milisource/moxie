package scraper

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

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

// SearchF95Zone searches F95Zone for game threads matching the query.
// Returns up to 5 results sorted by relevance.
//
// Uses XenForo's POST /search/search with _xfToken from the session's
// xf_csrf cookie. Falls back to Google site: search if CSRF token is
// unavailable or the POST request fails with a block.
func (c *Client) SearchF95Zone(query string) ([]SearchResult, error) {
	return c.SearchF95ZoneWithContext(context.Background(), query)
}

// SearchF95ZoneWithContext searches F95Zone for game threads matching the query,
// respecting the given context for cancellation and deadlines.
func (c *Client) SearchF95ZoneWithContext(ctx context.Context, query string) ([]SearchResult, error) {
	if query == "" {
		return nil, fmt.Errorf("scraper: search query is empty")
	}

	// Primary method: XenForo POST search with _xfToken.
	if token := c.xfCSRFToken(); token != "" {
		results, err := c.xfSearchPOST(ctx, query, token)
		if err == nil {
			return results, nil
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
		log.Debug("no xf_csrf token available, using Google fallback",
			"query", query,
		)
	}

	// Fallback: Google site search — no cookies or CSRF needed.
	return c.googleSiteSearch(ctx, query)
}

// xfSearchPOST performs a XenForo search via POST /search/search with
// the _xfToken from the user's session cookie. This is the canonical
// search method and requires a valid authenticated session.
func (c *Client) xfSearchPOST(ctx context.Context, query, token string) ([]SearchResult, error) {
	form := url.Values{}
	form.Set("_xfToken", token)
	form.Set("keywords", query)
	form.Set("c[title_only]", "1")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://f95zone.to/search/search",
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("scraper: failed to create search request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	body, err := c.do(req, searchMinDelay)
	if err != nil {
		return nil, err
	}

	return parseSearchResults(body, query), nil
}

// googleSiteSearch searches F95Zone via Google's site: operator.
// This is a cookie-free fallback when the XenForo search is unavailable.
// Search results are parsed from the Google SERP HTML.
func (c *Client) googleSiteSearch(ctx context.Context, query string) ([]SearchResult, error) {
	searchURL := fmt.Sprintf("https://www.google.com/search?q=site%%3Af95zone.to+%%22%s%%22&hl=en",
		url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("scraper: failed to create Google search request: %w", err)
	}

	body, err := c.do(req, searchMinDelay)
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

		// Extract thumbnail from the result's avatar/image.
		var thumbURL string
		if img := row.Find(".contentRow-figure img").First(); img.Length() > 0 {
			if src, ok := img.Attr("src"); ok {
				if strings.HasPrefix(src, "/") {
					thumbURL = "https://f95zone.to" + src
				} else {
					thumbURL = src
				}
			}
		}

		results = append(results, SearchResult{
			Title:        title,
			URL:          resultURL,
			Snippet:      snippet,
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
