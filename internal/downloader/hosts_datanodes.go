package downloader

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/mili/moxie/internal/log"
)

// --- DataNodes ---
// Flow:
//
//	1. GET /download/<FILE_CODE> → obtain session cookies + hidden form fields
//	2. POST with form fields + cookies → receive download redirect or link in body
//	3. Follow redirect to CDN
//
// Known limitations: DataNodes uses Cloudflare protection which may cause
// intermittent failures. The caller's fallback loop handles retries.
func (r *HostResolver) resolveDatanodes(rawURL string) (*ResolveResult, error) {
	// Extract file code from URL
	// Pattern 1: https://datanodes.to/download/<FILE_CODE>
	// Pattern 2: https://datanodes.to/<FILE_CODE>/<filename>
	var fileCode string

	re := regexp.MustCompile(`datanodes\.to/download/([a-zA-Z0-9]+)`)
	matches := re.FindStringSubmatch(rawURL)
	if len(matches) >= 2 {
		fileCode = matches[1]
	} else {
		// Try datanodes.to/<code>/<filename> format (no /download/ prefix)
		re = regexp.MustCompile(`datanodes\.to/([a-zA-Z0-9]+)/`)
		matches = re.FindStringSubmatch(rawURL)
		if len(matches) >= 2 {
			fileCode = matches[1]
		}
	}

	if fileCode == "" {
		return nil, fmt.Errorf("could not extract DataNodes file code from: %s (expected /download/<CODE> or /<CODE>/<filename>)", rawURL)
	}

	// Step 1: GET the download page to obtain session cookies and hidden form fields
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("datanodes create GET: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Referer", "https://datanodes.to/")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("datanodes GET: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("datanodes: GET returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("datanodes: read GET body: %w", err)
	}

	// Build cookie string from response cookies
	cookies := resp.Cookies()
	var cookieStr string
	for i, c := range cookies {
		if i > 0 {
			cookieStr += "; "
		}
		cookieStr += c.Name + "=" + c.Value
	}

	// Parse hidden form fields from the HTML response
	// DataNodes typically includes: op, id, rand, method_free, file_code, etc.
	formData := url.Values{}
	hiddenRe := regexp.MustCompile(`<input[^>]*type=["']hidden["'][^>]*name=["']([^"']+)["'][^>]*value=["']([^"']*)["'][^>]*>`)
	for _, m := range hiddenRe.FindAllStringSubmatch(string(body), -1) {
		formData.Set(m[1], m[2])
	}
	// Ensure file_code is present in the form data
	if formData.Get("file_code") == "" {
		formData.Set("file_code", fileCode)
	}

	// Step 2: POST the form with cookies to generate the download link
	// Use http.ErrUseLastResponse to prevent auto-follow of the download redirect
	noFollowClient := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	postReq, err := http.NewRequest("POST", rawURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, fmt.Errorf("datanodes create POST: %w", err)
	}
	postReq.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	postReq.Header.Set("Referer", rawURL)
	if cookieStr != "" {
		postReq.Header.Set("Cookie", cookieStr)
	}

	postResp, err := noFollowClient.Do(postReq)
	if err != nil {
		return nil, fmt.Errorf("datanodes POST: %w", err)
	}
	defer postResp.Body.Close()

	// Typical flow: POST returns 302 redirect to CDN download URL
	if postResp.StatusCode == http.StatusFound || postResp.StatusCode == http.StatusMovedPermanently {
		dlURL := postResp.Header.Get("Location")
		if dlURL != "" {
			log.Debug("datanodes: resolved via redirect", "url", dlURL)
			return &ResolveResult{
				URL: dlURL,
				Headers: map[string]string{
					"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
					"Referer":    rawURL,
				},
			}, nil
		}
	}

	// Fallback: POST returns 200 with a direct download link embedded in the page
	if postResp.StatusCode == http.StatusOK {
		postBody, err := io.ReadAll(postResp.Body)
		if err != nil {
			return nil, fmt.Errorf("datanodes: read POST body: %w", err)
		}

		// Look for URLs with common file extensions
		dlRe := regexp.MustCompile(`https?://[^\s"'<>]+\.(?:zip|rar|7z|tar\.gz|tar|xz|bz2|mp4|mkv|avi|mp3|flac|gz)(?:\?[^\s"'<>]*)?`)
		if dlMatch := dlRe.FindString(string(postBody)); dlMatch != "" {
			log.Debug("datanodes: resolved via body link", "url", dlMatch)
			return &ResolveResult{
				URL: dlMatch,
				Headers: map[string]string{
					"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
					"Referer":    rawURL,
				},
			}, nil
		}
	}

	return nil, fmt.Errorf("datanodes: could not resolve download URL (HTTP %d)", postResp.StatusCode)
}
