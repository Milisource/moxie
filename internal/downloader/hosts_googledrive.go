package downloader

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/mili/moxie/internal/log"
)

// --- Google Drive ---
// Flow:
//
//	1. Extract file ID from URL (/file/d/<ID>/view or ?id=<ID>)
//	2. GET https://drive.google.com/uc?export=download&id=<FILE_ID>
//	   - Small files (<100MB): server redirects to CDN (direct download)
//	   - Large files: server returns HTML virus-scan warning page with a confirm token
//	3. If HTML response, parse for confirm= token from uc-download-link href
//	4. Return URL with confirm token: /uc?export=download&confirm=<TOKEN>&id=<FILE_ID>
func (r *HostResolver) resolveGoogleDrive(url string) (*ResolveResult, error) {
	fileID := extractGoogleDriveFileID(url)
	if fileID == "" {
		return nil, fmt.Errorf("could not extract Google Drive file ID from: %s", url)
	}

	ucURL := fmt.Sprintf("https://drive.google.com/uc?export=download&id=%s", fileID)

	req, err := http.NewRequest("GET", ucURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create google drive request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google drive GET: %w", err)
	}
	defer resp.Body.Close()

	// If response is HTML, it's a virus-scan warning page → extract confirm token.
	// Otherwise (binary Content-Type, e.g. application/zip), it's a direct download.
	if strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("google drive: read body: %w", err)
		}

		confirm := extractGoogleDriveConfirm(body)
		if confirm != "" {
			dlURL := fmt.Sprintf(
				"https://drive.google.com/uc?export=download&confirm=%s&id=%s",
				confirm, fileID,
			)
			log.Debug("google drive: resolved with confirm token", "confirm", confirm)
			return &ResolveResult{
				URL: dlURL,
				Headers: map[string]string{
					"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
				},
			}, nil
		}
	}

	// Small file or no confirm needed — UC URL redirects to CDN automatically
	return &ResolveResult{
		URL: ucURL,
		Headers: map[string]string{
			"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
		},
	}, nil
}

// extractGoogleDriveFileID extracts the file ID from various Google Drive URL formats:
//
//	https://drive.google.com/file/d/<FILE_ID>/view
//	https://drive.google.com/uc?export=download&id=<FILE_ID>
//	https://drive.google.com/open?id=<FILE_ID>
func extractGoogleDriveFileID(rawURL string) string {
	// Pattern 1: /file/d/<FILE_ID> (most common share link)
	re := regexp.MustCompile(`/file/d/([a-zA-Z0-9_-]+)`)
	if matches := re.FindStringSubmatch(rawURL); len(matches) >= 2 {
		return matches[1]
	}

	// Pattern 2: ?id=<FILE_ID> query parameter
	u, err := url.Parse(rawURL)
	if err == nil {
		if id := u.Query().Get("id"); id != "" {
			return id
		}
	}

	// Pattern 3: /open?id=<FILE_ID> (handled by Pattern 2 via url.Parse)

	return ""
}

// extractGoogleDriveConfirm parses the HTML virus-scan warning page for the
// confirm token. The token appears in the uc-download-link anchor href or as
// a general confirm= parameter in URLs embedded in the page.
func extractGoogleDriveConfirm(body []byte) string {
	s := string(body)

	// Pattern 1: uc-download-link anchor contains confirm= in its href
	// <a ... class="uc-download-link" ... href="/uc?export=download&confirm=TOKEN...">
	re := regexp.MustCompile(`uc-download-link[^>]*href="[^"]*confirm=([^"&]+)`)
	if matches := re.FindStringSubmatch(s); len(matches) >= 2 {
		return matches[1]
	}

	// Pattern 2: generic confirm= parameter in any URL or form action
	re2 := regexp.MustCompile(`confirm=([a-zA-Z0-9_-]+)`)
	if matches := re2.FindStringSubmatch(s); len(matches) >= 2 {
		return matches[1]
	}

	return ""
}
