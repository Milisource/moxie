package downloader

import (
	"context"
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
//
// Current flow (verified live 2026-10): a Cloudflare Turnstile widget + a
// client-side callback. The initial HTML has no hidden `op` form and no
// direct link:
//
//	1. GET /f/<FILE_HASH> → HTML with a Turnstile widget (#captcha) whose
//	   callback is the page's cloudflareCallback(token).
//	2. On success cloudflareCallback XHR-POSTs `cf-turnstile-response=<token>`
//	   to the same URL and receives JSON `{"link": "..."}`.
//	3. The page sets #download-link.href = response.link (the anchor starts
//	   hidden and href-less).
//
// A plain HTTP client cannot produce a Turnstile token, so when the page is
// Turnstile-gated the resolver hands the page to the browser solver
// (browserresolve.ResolveTurnstileURL via defaultTurnstileSolver), which
// clears the widget and returns the revealed URL for the Go path to fetch.
//
// Legacy flow (still supported as a fallback for old-style pages):
//
//	GET → hidden form fields → POST (op=download1, id, rand, method_free=1)
//	   → 302 redirect to the CDN download URL.
//
// Direct links embedded in the initial HTML are used first, without a browser.
func (r *HostResolver) resolveVikingFile(rawURL string) (*ResolveResult, error) {
	// Step 1: GET the download page to obtain session cookies and hidden form fields
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("vikingfile create GET: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Referer", "https://vikingfile.com/")
	r.attachBrowserCookies(req)

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
		r.attachBrowserCookies(postReq)

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

	// Step 3.5: Cloudflare Turnstile page (vikingfile's current flow).
	//
	// The download URL only exists after the page's cloudflareCallback runs:
	// cloudflareCallback XHR-POSTs cf-turnstile-response=<token> to the same
	// URL and sets #download-link.href from the JSON {"link": ...} response.
	// A plain HTTP client cannot produce the token, so the browser solves the
	// widget (auto-pass or managed-challenge checkbox) and hands back the
	// revealed URL; the Go path then fetches it with cookies + Referer.
	if isVikingFileTurnstilePage(bodyStr) {
		dlURL, err := r.solveVikingFileTurnstile(rawURL)
		if err != nil {
			return nil, fmt.Errorf("vikingfile: Turnstile browser solve failed: %w", err)
		}
		if dlURL == "" {
			return nil, fmt.Errorf("vikingfile: Turnstile browser solve returned no download link")
		}
		log.Debug("vikingfile: resolved via browser Turnstile", "url", dlURL)
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

// isVikingFileTurnstilePage reports whether the downloaded HTML is the
// Cloudflare Turnstile + client-side-callback variant of the vikingfile
// page. It keys on the widget bootstrap, the XHR body field, and the
// callback name the page's inline script defines — any one is enough.
func isVikingFileTurnstilePage(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "challenges.cloudflare.com/turnstile") ||
		strings.Contains(lower, "cf-turnstile-response") ||
		strings.Contains(lower, "cloudflarecallback")
}

// solveVikingFileTurnstile runs the browser Turnstile solver (installed by
// browserresolve) for a vikingfile page and returns the revealed download
// URL. browserMu serialises the browser session with the download fallback
// and the masked solver: they all copy the same profile and two at once
// contend for its lock and look like a bot to the host.
func (r *HostResolver) solveVikingFileTurnstile(rawURL string) (string, error) {
	solver := defaultTurnstileSolver
	if solver == nil {
		return "", fmt.Errorf(
			"browser-assisted resolution is not enabled "+
				"(enable it with `moxie config set browser_fallback auto`)")
	}
	browserMu.Lock()
	defer browserMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), vikingFileTurnstileTimeout)
	defer cancel()
	return solver(ctx, rawURL)
}
