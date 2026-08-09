package downloader

import (
	"fmt"
	"regexp"
	"strings"
)

// --- Pixeldrain ---
// API: GET https://pixeldrain.com/api/file/<FILE_ID>
// Track D research (2026-08-09): pixeldrain serves reliably only when the
// request carries Referer matching the original /u/<FILE_ID> page URL, so
// derive it from the input URL before rewriting. /api/file/<FILE_ID> inputs
// have no page URL available; fall back to the bare origin (https://pixeldrain.com/),
// which keeps the Referer on the same origin without inventing a page that
// does not exist — safer than sending no Referer at all, and more truthful
// than fabricating a specific /u/ page.
func (r *HostResolver) resolvePixeldrain(url string) (*ResolveResult, error) {
	// Extract file ID from various URL formats:
	// https://pixeldrain.com/u/<FILE_ID>
	// https://pixeldrain.com/api/file/<FILE_ID>
	re := regexp.MustCompile(`pixeldrain\.com/(?:u|api/file)/([a-zA-Z0-9_-]+)`)
	matches := re.FindStringSubmatch(url)
	if len(matches) < 2 {
		return nil, fmt.Errorf("could not extract Pixeldrain file ID from: %s", url)
	}
	fileID := matches[1]
	directURL := fmt.Sprintf("https://pixeldrain.com/api/file/%s", fileID)

	// matches[0] is the full regex match; since the regex is case-sensitive,
	// "/u/" can only appear when the input was a page URL, never an API URL
	// (the regex stops at the file ID, before any query string).
	referer := "https://pixeldrain.com/"
	if strings.Contains(matches[0], "/u/") {
		referer = fmt.Sprintf("https://pixeldrain.com/u/%s", fileID)
	}

	return &ResolveResult{
		URL: directURL,
		Headers: map[string]string{
			"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
			"Referer":    referer,
		},
	}, nil
}
