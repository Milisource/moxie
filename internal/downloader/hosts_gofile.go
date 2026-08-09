package downloader

import (
	"fmt"
	"regexp"
	"strings"
)

// --- Gofile ---
// API: GET https://api.gofile.io/getContent?contentId=<FILE_ID>&wt=4fd6sg89d7s6
// Then redirects to download server.
func (r *HostResolver) resolveGofile(url string) (*ResolveResult, error) {
	// Extract file ID from URL:
	// https://gofile.io/d/<FILE_ID>
	re := regexp.MustCompile(`gofile\.io/(?:d|download)/([a-zA-Z0-9]+)`)
	matches := re.FindStringSubmatch(url)
	if len(matches) < 2 {
		// Try direct download subdomain
		if strings.Contains(url, ".gofile.io") {
			return &ResolveResult{
				URL: url,
				Headers: map[string]string{
					"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
				},
			}, nil
		}
		return nil, fmt.Errorf("could not extract Gofile file ID from: %s", url)
	}
	fileID := matches[1]

	// Gofile requires an account token for full-speed downloads in 2025+.
	// For free downloads, use the direct download link pattern.
	directURL := fmt.Sprintf("https://%s.gofile.io/%s", fileID, fileID)

	// Note: gofile's getContent API response is parsed but only ever yields
	// this same direct URL for free downloads, so the extra request was
	// dropped — try the direct link directly.
	return &ResolveResult{
		URL: directURL,
		Headers: map[string]string{
			"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
			"Cookie":     "accountToken=guest",
		},
	}, nil
}
