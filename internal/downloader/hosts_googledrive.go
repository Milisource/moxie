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
//	   - Large files: server returns HTML virus-scan warning page
//	3. If HTML:
//	   a. current flow — the page carries a drive.usercontent.google.com
//	      download URL (or a form posting to it) with id/export/confirm/uuid
//	   b. legacy flow — uc-download-link href with a confirm token
//	4. Return the resolved download URL.

// googleDriveUCBase and googleDriveUserContent are vars so tests can point
// the resolver at a local server.
var (
	googleDriveUCBase      = "https://drive.google.com"
	googleDriveUserContent = "https://drive.usercontent.google.com/download"
)

const googleDriveUA = "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0"

func (r *HostResolver) resolveGoogleDrive(url string) (*ResolveResult, error) {
	fileID := extractGoogleDriveFileID(url)
	if fileID == "" {
		return nil, fmt.Errorf("could not extract Google Drive file ID from: %s", url)
	}

	ucURL := fmt.Sprintf("%s/uc?export=download&id=%s", googleDriveUCBase, fileID)

	req, err := http.NewRequest("GET", ucURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create google drive request: %w", err)
	}
	req.Header.Set("User-Agent", googleDriveUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google drive GET: %w", err)
	}
	defer resp.Body.Close()

	// If response is HTML, it's a virus-scan warning page → resolve the
	// download URL the page carries. Otherwise (binary Content-Type, e.g.
	// application/zip), it's a direct download.
	if strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("google drive: read body: %w", err)
		}

		// Current flow: drive.usercontent.google.com/download with the
		// warning page's id/export/confirm/uuid parameters.
		if dlURL := extractGoogleDriveUserContentURL(body); dlURL != "" {
			log.Debug("google drive: resolved via usercontent URL")
			return &ResolveResult{
				URL:     dlURL,
				Headers: map[string]string{"User-Agent": googleDriveUA},
			}, nil
		}

		// Legacy flow: uc-download-link href with a confirm token.
		if confirm := extractGoogleDriveConfirm(body); confirm != "" {
			dlURL := fmt.Sprintf(
				"%s/uc?export=download&confirm=%s&id=%s",
				googleDriveUCBase, confirm, fileID,
			)
			log.Debug("google drive: resolved with confirm token", "confirm", confirm)
			return &ResolveResult{
				URL:     dlURL,
				Headers: map[string]string{"User-Agent": googleDriveUA},
			}, nil
		}
	}

	// Small file or no confirm needed — UC URL redirects to CDN automatically
	return &ResolveResult{
		URL:     ucURL,
		Headers: map[string]string{"User-Agent": googleDriveUA},
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

// extractGoogleDriveUserContentURL finds the current large-file download URL.
// The warning page carries either a direct drive.usercontent.google.com link
// or a form whose hidden inputs (id, export, confirm, uuid) build one.
func extractGoogleDriveUserContentURL(body []byte) string {
	s := strings.ReplaceAll(string(body), "&amp;", "&")

	// Direct link on the page.
	reLink := regexp.MustCompile(`https://drive\.usercontent\.google\.com/download\?[^\s"'<>]+`)
	if m := reLink.FindString(s); m != "" {
		return m
	}

	// Form with hidden inputs.
	formRe := regexp.MustCompile(`(?is)<form[^>]*action="([^"]*drive\.usercontent\.google\.com/download[^"]*)"[^>]*>(.*?)</form>`)
	fm := formRe.FindStringSubmatch(s)
	if len(fm) < 3 {
		return ""
	}
	action := strings.ReplaceAll(fm[1], "&amp;", "&")
	inputRe := regexp.MustCompile(`(?is)<input[^>]*name="([^"]+)"[^>]*value="([^"]*)"`)
	values := url.Values{}
	for _, m := range inputRe.FindAllStringSubmatch(fm[2], -1) {
		values.Set(m[1], m[2])
	}
	if len(values) == 0 {
		return ""
	}
	base, err := url.Parse(action)
	if err != nil {
		return ""
	}
	base.RawQuery = values.Encode()
	return base.String()
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
