package downloader

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

// --- Buzzheavier ---
// Uses HTMX-based download flow: GET <url>/download with HX-Request header
// Response includes hx-redirect header with the actual download URL.
// Also supports direct DD (buzzheavier.com/dd/<ID>) and torrents.
func (r *HostResolver) resolveBuzzheavier(url string) (*ResolveResult, error) {
	// Clean the URL
	url = strings.TrimRight(url, "/")

	// Try direct domain first
	ddURL := strings.Replace(url, "buzzheavier.com", "dd.buzzheavier.com", 1)
	resp, err := r.client.Get(ddURL)
	if err == nil && resp.StatusCode == http.StatusOK {
		resp.Body.Close()
		// If it's a file (has Content-Disposition), we can download directly
		if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.Contains(ct, "text/html") {
			return &ResolveResult{
				URL: ddURL,
				Headers: map[string]string{
					"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
				},
			}, nil
		}
	} else if resp != nil {
		resp.Body.Close()
	}

	// Use HTMX-based resolution: GET <url>/download with HX-Request header
	dlURL := url + "/download"
	req, err := http.NewRequest("GET", dlURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create buzzheavier request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Current-URL", url)
	req.Header.Set("Referer", url)
	req.Header.Set("Accept", "*/*")

	resp, err = r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("buzzheavier resolve: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusFound {
		return nil, fmt.Errorf("buzzheavier: HTTP %d", resp.StatusCode)
	}

	// The actual download URL is in the hx-redirect header
	redirectURL := resp.Header.Get("hx-redirect")
	if redirectURL == "" {
		// Some versions return it in body
		body, _ := io.ReadAll(resp.Body)
		redirectURL = strings.TrimSpace(string(body))
	}
	if redirectURL == "" {
		// Fallback: try the URL directly
		redirectURL = url
	}

	// Ensure absolute URL
	if strings.HasPrefix(redirectURL, "/") {
		redirectURL = "https://dd.buzzheavier.com" + redirectURL
	}

	return &ResolveResult{
		URL: redirectURL,
		Headers: map[string]string{
			"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
			"Referer":    url,
		},
	}, nil
}
