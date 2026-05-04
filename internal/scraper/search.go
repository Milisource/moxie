package scraper

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// SearchResult represents a single search result from F95Zone.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
}

// SearchF95Zone searches F95Zone for game threads matching the query.
// Returns up to 5 results sorted by relevance.
func (c *Client) SearchF95Zone(query string) ([]SearchResult, error) {
	return c.SearchF95ZoneWithContext(context.Background(), query)
}

// SearchF95ZoneWithContext searches F95Zone for game threads matching the query,
// respecting the given context for cancellation and deadlines.
// Returns up to 5 results sorted by relevance.
func (c *Client) SearchF95ZoneWithContext(ctx context.Context, query string) ([]SearchResult, error) {
	if query == "" {
		return nil, fmt.Errorf("scraper: search query is empty")
	}

	searchURL := fmt.Sprintf("https://f95zone.to/search/search?keywords=%s&c%%5Btitle_only%%5D=1",
		url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("scraper: failed to create search request: %w", err)
	}

	body, err := c.do(req, searchMinDelay)
	if err != nil {
		return nil, fmt.Errorf("scraper: search request failed: %w", err)
	}

	return parseSearchResults(body), nil
}

// parseSearchResults parses the XenForo 2.x search results HTML.
// It iterates over each .contentRow and extracts the title, URL, and snippet.
func parseSearchResults(html string) []SearchResult {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}

	var results []SearchResult
	doc.Find(".contentRow").Each(func(i int, row *goquery.Selection) {
		if i >= 5 {
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

		results = append(results, SearchResult{
			Title:   title,
			URL:     resultURL,
			Snippet: snippet,
		})
	})

	if len(results) > 5 {
		results = results[:5]
	}

	return results
}
