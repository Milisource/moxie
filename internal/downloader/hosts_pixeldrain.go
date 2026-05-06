package downloader

import (
	"fmt"
	"regexp"
)

// --- Pixeldrain ---
// API: GET https://pixeldrain.com/api/file/<FILE_ID>
// Direct download URL, no special headers needed.
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
	return &ResolveResult{
		URL: directURL,
		Headers: map[string]string{
			"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
		},
	}, nil
}
