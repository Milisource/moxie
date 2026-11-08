package downloader

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// --- MediaFire ---
// MediaFire serves a file page that embeds a time-limited direct download URL
// on downloadNNN.mediafire.com. The URL is present in the initial HTML for
// files that do not require a captcha, so no browser is needed.
//
// Flow:
//  1. GET the /file/<key>/<name>/file page
//  2. Extract https://downloadNNN.mediafire.com/... from the page
//  3. Fall back to the #downloadButton href when present
func (r *HostResolver) resolveMediafire(rawURL string) (*ResolveResult, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("mediafire create GET: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mediafire GET: %w", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if ct != "" && !strings.Contains(ct, "text/html") {
		// Already a direct file response.
		return &ResolveResult{URL: rawURL, Headers: map[string]string{
			"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
			"Referer":    "https://www.mediafire.com/",
		}}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mediafire: page returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("mediafire: read page: %w", err)
	}
	page := string(body)

	// The direct URL is the most reliable signal.
	directRe := regexp.MustCompile(`https://download[0-9]*\.mediafire\.com/[^"'\s<>\\]+`)
	if m := directRe.FindString(page); m != "" {
		return &ResolveResult{URL: html.UnescapeString(m), Headers: map[string]string{
			"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
			"Referer":    "https://www.mediafire.com/",
		}}, nil
	}

	// Fallback: the download button anchor.
	btnRe := regexp.MustCompile(`<a[^>]*id=["']downloadButton["'][^>]*href=["']([^"']+)["']`)
	if m := btnRe.FindStringSubmatch(page); len(m) >= 2 && m[1] != "#" {
		return &ResolveResult{URL: html.UnescapeString(resolveRelativeURL(rawURL, m[1])), Headers: map[string]string{
			"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
			"Referer":    "https://www.mediafire.com/",
		}}, nil
	}

	if strings.Contains(strings.ToLower(page), "file removed") ||
		strings.Contains(strings.ToLower(page), "file has been removed") {
		return nil, fmt.Errorf("mediafire: file was removed")
	}
	return nil, fmt.Errorf("mediafire: no direct download link found (captcha or changed page layout)")
}

// --- Bunkr / Bunkrr ---
// Bunkr file pages expose the CDN host and numeric file id. The CDN page has a
// small API + URL-signing flow:
//
//  1. GET https://bunkr.<tld>/f/<slug> → find https://dl.bunkr.<tld>/file/<id>
//  2. POST https://dl.bunkr.<tld>/api/_001_v2 {"id":"<id>"}
//     → {"mediafiles":"https://psu.scdn.st","original":"name.rar","path":"/storage/media/..."}
//  3. GET https://glb-apisign.cdn.cr/sign?path=<path> → {"token":"...","ex":...}
//  4. mediafiles+path?n=<original>&token=<token>&ex=<ex> is a direct download
func (r *HostResolver) resolveBunkrr(rawURL string) (*ResolveResult, error) {
	pageHost := ""
	if u, err := url.Parse(rawURL); err == nil {
		pageHost = u.Scheme + "://" + u.Host
	}
	if pageHost == "" {
		return nil, fmt.Errorf("bunkr: invalid URL")
	}

	pageID := ""
	dlURL := ""
	if strings.Contains(strings.ToLower(rawURL), "dl.") && strings.Contains(rawURL, "/file/") {
		dlURL = rawURL
	} else {
		req, err := http.NewRequest("GET", rawURL, nil)
		if err != nil {
			return nil, fmt.Errorf("bunkr create GET: %w", err)
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		req.Header.Set("Referer", pageHost+"/")

		resp, err := r.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("bunkr GET: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("bunkr: page returned HTTP %d", resp.StatusCode)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
		if err != nil {
			return nil, fmt.Errorf("bunkr: read page: %w", err)
		}
		re := regexp.MustCompile(`https://dl\.bunkr\.[a-z]+/file/([0-9]+)`)
		if m := re.FindStringSubmatch(string(body)); len(m) >= 2 {
			pageID = m[1]
			dlURL = m[0]
		}
		if pageID == "" {
			return nil, fmt.Errorf("bunkr: no CDN link found on file page (album/removed?)")
		}
	}

	// Derive the API host and id from the CDN URL.
	u, err := url.Parse(dlURL)
	if err != nil {
		return nil, fmt.Errorf("bunkr: parse CDN URL: %w", err)
	}
	idRe := regexp.MustCompile(`/file/([0-9]+)`)
	m := idRe.FindStringSubmatch(u.Path)
	if len(m) < 2 {
		return nil, fmt.Errorf("bunkr: could not extract file id")
	}
	fileID := m[1]
	apiURL := u.Scheme + "://" + u.Host + "/api/_001_v2"

	apiReq, err := http.NewRequest("POST", apiURL, strings.NewReader(fmt.Sprintf(`{"id":%q}`, fileID)))
	if err != nil {
		return nil, fmt.Errorf("bunkr create API request: %w", err)
	}
	apiReq.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
	apiReq.Header.Set("Content-Type", "application/json")
	apiReq.Header.Set("Accept", "application/json")
	apiReq.Header.Set("Origin", pageHost)
	apiReq.Header.Set("Referer", pageHost+"/")

	apiResp, err := r.client.Do(apiReq)
	if err != nil {
		return nil, fmt.Errorf("bunkr API: %w", err)
	}
	defer apiResp.Body.Close()
	if apiResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bunkr: API returned HTTP %d", apiResp.StatusCode)
	}
	var meta struct {
		MediaFiles string `json:"mediafiles"`
		Original   string `json:"original"`
		Path       string `json:"path"`
	}
	if err := decodeJSON(apiResp.Body, &meta); err != nil {
		return nil, fmt.Errorf("bunkr: decode API response: %w", err)
	}
	if meta.MediaFiles == "" || meta.Path == "" {
		return nil, fmt.Errorf("bunkr: API response missing media path")
	}

	raw := meta.MediaFiles + meta.Path
	signURL := "https://glb-apisign.cdn.cr/sign?path=" + url.QueryEscape(meta.Path)
	signResp, err := r.client.Get(signURL)
	if err != nil {
		return nil, fmt.Errorf("bunkr sign request: %w", err)
	}
	defer signResp.Body.Close()
	if signResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bunkr: sign service returned HTTP %d", signResp.StatusCode)
	}
	var signed struct {
		Token string `json:"token"`
		Ex    int64  `json:"ex"`
	}
	if err := decodeJSON(signResp.Body, &signed); err != nil {
		return nil, fmt.Errorf("bunkr: decode sign response: %w", err)
	}
	if signed.Token == "" {
		return nil, fmt.Errorf("bunkr: sign service returned no token")
	}

	q := url.Values{}
	if meta.Original != "" {
		q.Set("n", meta.Original)
	}
	q.Set("token", signed.Token)
	if signed.Ex > 0 {
		q.Set("ex", fmt.Sprintf("%d", signed.Ex))
	}
	sep := "?"
	if strings.Contains(raw, "?") {
		sep = "&"
	}

	return &ResolveResult{
		URL: raw + sep + q.Encode(),
		Headers: map[string]string{
			"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
			"Referer":    pageHost + "/",
		},
	}, nil
}

// --- Dropbox ---
// Shared Dropbox links serve an HTML preview page unless dl=1 is set.
func (r *HostResolver) resolveDropbox(rawURL string) (*ResolveResult, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("dropbox: invalid URL")
	}
	q := u.Query()
	q.Set("dl", "1")
	u.RawQuery = q.Encode()

	return &ResolveResult{
		URL: u.String(),
		Headers: map[string]string{
			"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
		},
	}, nil
}
