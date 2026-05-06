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

// --- VikingFile ---
// Flow (standard file host):
//
//	1. GET /f/<FILE_HASH> → HTML page with file info, optional captcha, hidden form fields
//	2. POST to same URL with form fields (op=download1, id=<HASH>, rand=..., method_free=1)
//	   → 302 redirect to CDN download URL
//	3. Follow redirect to CDN
//
// If the page uses a Cloudflare Turnstile captcha (as vikingfile.com does), the
// POST without a valid captcha token will fail. In that case the resolver falls
// back to extracting direct download links from the page HTML if any are present.
func (r *HostResolver) resolveVikingFile(rawURL string) (*ResolveResult, error) {
	// Step 1: GET the download page to obtain session cookies and hidden form fields
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("vikingfile create GET: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Referer", "https://vikingfile.com/")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vikingfile GET: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vikingfile: GET returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("vikingfile: read GET body: %w", err)
	}

	bodyStr := string(body)

	// Build cookie string from response cookies
	cookies := resp.Cookies()
	var cookieStr string
	for i, c := range cookies {
		if i > 0 {
			cookieStr += "; "
		}
		cookieStr += c.Name + "=" + c.Value
	}

	// Step 2: Try the standard file host POST flow (op=download1, id, rand, method_free)
	//
	// Many file hosts (DataNodes, UploadHaven, HexLoad, etc.) embed hidden form fields
	// in the download page. POSTing them back with session cookies triggers a redirect
	// to the CDN download URL.
	formData := url.Values{}
	hiddenRe := regexp.MustCompile(`<input[^>]*type=["']hidden["'][^>]*name=["']([^"']+)["'][^>]*value=["']([^"']*)["'][^>]*>`)
	for _, m := range hiddenRe.FindAllStringSubmatch(bodyStr, -1) {
		formData.Set(m[1], m[2])
	}

	if formData.Get("op") != "" {
		log.Debug("vikingfile: found hidden form with op, attempting POST", "op", formData.Get("op"))

		// Use http.ErrUseLastResponse to prevent auto-follow of the download redirect
		noFollowClient := &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}

		postURL := rawURL
		// Some hosts POST to a different /download endpoint
		if action := extractFormAction(bodyStr); action != "" {
			postURL = resolveRelativeURL(rawURL, action)
		}

		postReq, err := http.NewRequest("POST", postURL, strings.NewReader(formData.Encode()))
		if err != nil {
			return nil, fmt.Errorf("vikingfile create POST: %w", err)
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
			return nil, fmt.Errorf("vikingfile POST: %w", err)
		}
		defer postResp.Body.Close()

		// POST returns 302 redirect to CDN download URL
		if postResp.StatusCode == http.StatusFound || postResp.StatusCode == http.StatusMovedPermanently {
			dlURL := postResp.Header.Get("Location")
			if dlURL != "" {
				log.Debug("vikingfile: resolved via POST redirect", "url", dlURL)
				return &ResolveResult{
					URL: dlURL,
					Headers: map[string]string{
						"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
						"Referer":    rawURL,
					},
				}, nil
			}
		}

		// POST returns 200 with download link embedded in HTML body
		if postResp.StatusCode == http.StatusOK {
			postBody, err := io.ReadAll(postResp.Body)
			if err != nil {
				return nil, fmt.Errorf("vikingfile: read POST body: %w", err)
			}
			if dlURL := extractDownloadLink(string(postBody)); dlURL != "" {
				log.Debug("vikingfile: resolved via POST body link", "url", dlURL)
				return &ResolveResult{
					URL: dlURL,
					Headers: map[string]string{
						"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
						"Referer":    rawURL,
					},
				}, nil
			}

			// POST returned 200 but no download link found — likely a captcha wall or error page.
			return nil, fmt.Errorf("vikingfile: POST returned 200 without download link (captcha or error)")
		}

		return nil, fmt.Errorf("vikingfile: POST returned HTTP %d", postResp.StatusCode)
	}

	// Step 3: No form found. Look for direct download links in the GET response.
	//
	// Some file hosts embed the download URL directly in the page as:
	//   <a href="https://cdn.example.com/file.zip">Download</a>
	//   <a id="download-link" href="/d/<HASH>/<filename>">Download</a>
	//   <a href="..." download>
	if dlURL := extractDownloadLink(bodyStr); dlURL != "" {
		log.Debug("vikingfile: resolved via direct link in GET response", "url", dlURL)
		return &ResolveResult{
			URL: dlURL,
			Headers: map[string]string{
				"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
				"Referer":    rawURL,
			},
		}, nil
	}

	// Step 4: Nothing worked — return a descriptive error for the caller's fallback loop.
	return nil, fmt.Errorf(
		"vikingfile: could not resolve download URL at %s — "+
			"the page may require a browser captcha or the link format is unrecognized",
		rawURL,
	)
}
