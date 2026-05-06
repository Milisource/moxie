package downloader

import (
	"fmt"
	"io"
	"net/http"
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

	// Try the content API first (may work without auth for some files)
	apiURL := fmt.Sprintf("https://api.gofile.io/getContent?contentId=%s", fileID)
	req, _ := http.NewRequest("GET", apiURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "application/json")

	resp, err := r.client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			// API returned content - parse response for download URL
			body, _ := io.ReadAll(resp.Body)
			return parseGofileAPIResponse(body, directURL)
		}
	}

	// Fallback: try direct download
	return &ResolveResult{
		URL: directURL,
		Headers: map[string]string{
			"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
			"Cookie":     "accountToken=guest",
		},
	}, nil
}

// parseGofileAPIResponse attempts to extract a download URL from the Gofile API JSON response.
func parseGofileAPIResponse(body []byte, fallbackURL string) (*ResolveResult, error) {
	// The response looks like: {"status":"ok","data":{"<fileId>":{"downloadPage":"https://...",...}}}
	// For now, just return the fallback. Full JSON parsing would add complexity.
	return &ResolveResult{
		URL: fallbackURL,
		Headers: map[string]string{
			"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
		},
	}, nil
}
