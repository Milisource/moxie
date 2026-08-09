package downloader

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/mili/moxie/internal/log"
)

// --- MediaFire ---
// Flow (plain HTTP, no anti-bot):
//
//	1. GET /file/<QUICK_KEY>/<name>/file → HTML page; the direct CDN URL
//	   (download<NNN>.mediafire.com) is embedded either as a plain href on
//	   the #downloadButton anchor or base64-encoded in its
//	   data-scrambled-url attribute (Vortex/JDownloader behavior).
//	2. If the page yields no CDN URL, fall back to the sessionless link API
//	   /api/file/get_links.php?quick_key=<QUICK_KEY>.
//	3. Folder links (/folder/<KEY>/...) resolve via folder/get_content.php;
//	   single-file folders resolve to that file, multi-file folders return a
//	   descriptive error so the caller's fallback loop can try other links.

var (
	reMFDownloadButton = regexp.MustCompile(`<a[^>]*\sid=["']downloadButton["'][^>]*>`)
	reMFButtonHref     = regexp.MustCompile(`href=["']([^"']+)["']`)
	reMFScrambledURL   = regexp.MustCompile(`data-scrambled-url=["']([^"']+)["']`)
	reMFCDNHref        = regexp.MustCompile(`https://download\d*\.mediafire\.com/[^"'\s<>]+`)
	reMFCDNHost        = regexp.MustCompile(`^download\d*\.mediafire\.com$`)
)

// resolveMediafire resolves a MediaFire file or folder URL to a direct CDN
// download URL.
func (r *HostResolver) resolveMediafire(rawURL string) (*ResolveResult, error) {
	pageURL, kind, key, err := parseMediafireURL(rawURL)
	if err != nil {
		return nil, err
	}
	if kind == "folder" {
		return r.resolveMediafireFolder(pageURL, key)
	}
	return r.resolveMediafireFile(pageURL, key)
}

// parseMediafireURL normalizes a mediafire.com URL and extracts the resource
// kind ("file", "file_premium", "folder") and its key. The m. mobile
// subdomain redirects to the app shell, so it is rewritten to www, which
// serves the public file page.
func parseMediafireURL(rawURL string) (pageURL, kind, key string, err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", "", fmt.Errorf("mediafire: parse URL %q: %w", rawURL, err)
	}
	host := strings.ToLower(u.Hostname())
	if !strings.HasSuffix(host, "mediafire.com") {
		return "", "", "", fmt.Errorf("mediafire: not a mediafire.com URL: %s", rawURL)
	}
	if host != "www.mediafire.com" {
		u.Host = "www.mediafire.com"
	}

	path := strings.TrimSuffix(u.Path, "/")
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(segments) < 2 || segments[1] == "" {
		return "", "", "", fmt.Errorf("mediafire: could not extract key from %s (expected /file/<KEY> or /folder/<KEY>)", rawURL)
	}
	kind = segments[0]
	switch kind {
	case "file", "file_premium", "folder":
	default:
		return "", "", "", fmt.Errorf("mediafire: unsupported path type %q in %s (expected /file/<KEY> or /folder/<KEY>)", kind, rawURL)
	}

	u.Path = path
	return u.String(), kind, segments[1], nil
}

// resolveMediafireFile fetches the public file page and extracts the direct
// CDN URL, falling back to the sessionless link API when the page carries
// none.
func (r *HostResolver) resolveMediafireFile(pageURL, quickKey string) (*ResolveResult, error) {
	req, err := http.NewRequest("GET", pageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("mediafire create GET: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Referer", "https://www.mediafire.com/")
	r.attachBrowserCookies(req)

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mediafire GET: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mediafire: GET returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("mediafire: read GET body: %w", err)
	}

	if dlURL := extractMediafireCDNURL(string(body)); dlURL != "" {
		log.Debug("mediafire: resolved via page link", "url", dlURL)
		return &ResolveResult{
			URL: dlURL,
			Headers: map[string]string{
				"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
				"Referer":    pageURL,
			},
		}, nil
	}

	// Page parse failed — fall back to the sessionless link API.
	return r.mediafireAPILink(pageURL, quickKey)
}

// mediafireAPILink queries the sessionless link API for a direct download
// URL. The API only returns direct_download for sessions with permission
// (anonymous requests get an error code), so this is a fallback behind the
// page parse, which works for public files without a session.
func (r *HostResolver) mediafireAPILink(pageURL, quickKey string) (*ResolveResult, error) {
	apiURL := "https://www.mediafire.com/api/file/get_links.php?quick_key=" +
		url.QueryEscape(quickKey) + "&response_format=json"
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("mediafire create API request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	req.Header.Set("Referer", "https://www.mediafire.com/")
	r.attachBrowserCookies(req)

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mediafire API GET: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("mediafire: read API body: %w", err)
	}

	var result struct {
		Response struct {
			Links []struct {
				DirectDownload string `json:"direct_download"`
			} `json:"links"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("mediafire: parse API response: %w", err)
	}
	for _, link := range result.Response.Links {
		if link.DirectDownload != "" {
			log.Debug("mediafire: resolved via link API", "url", link.DirectDownload)
			return &ResolveResult{
				URL: link.DirectDownload,
				Headers: map[string]string{
					"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
					"Referer":    pageURL,
				},
			}, nil
		}
	}
	return nil, fmt.Errorf("mediafire: no direct download link found for %s (page and API both failed)", pageURL)
}

// resolveMediafireFolder resolves a folder link through the folder content
// API. A single-file folder resolves to that file; folders with several
// files return a descriptive error because a resolver yields exactly one URL.
func (r *HostResolver) resolveMediafireFolder(folderURL, folderKey string) (*ResolveResult, error) {
	apiURL := "https://www.mediafire.com/api/folder/get_content.php?folder_key=" +
		url.QueryEscape(folderKey) + "&content_type=files&chunk_size=1000&response_format=json"
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("mediafire create folder API request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	req.Header.Set("Referer", "https://www.mediafire.com/")
	r.attachBrowserCookies(req)

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mediafire folder API GET: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("mediafire: read folder API body: %w", err)
	}

	var result struct {
		Response struct {
			FolderContent struct {
				Files []struct {
					Quickkey string `json:"quickkey"`
					Links    struct {
						NormalDownload string `json:"normal_download"`
					} `json:"links"`
				} `json:"files"`
			} `json:"folder_content"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("mediafire: parse folder API response: %w", err)
	}

	files := result.Response.FolderContent.Files
	switch len(files) {
	case 0:
		return nil, fmt.Errorf("mediafire: folder %s contains no files", folderURL)
	case 1:
		file := files[0]
		filePage := file.Links.NormalDownload
		if filePage == "" {
			filePage = "https://www.mediafire.com/file/" + file.Quickkey
		}
		log.Debug("mediafire: folder resolved to single file", "page", filePage)
		return r.resolveMediafireFile(filePage, file.Quickkey)
	default:
		return nil, fmt.Errorf("mediafire: folder %s contains %d files; resolve individual file links instead", folderURL, len(files))
	}
}

// extractMediafireCDNURL scans a MediaFire file page for the direct CDN URL.
// Checks in priority order:
//  1. The #downloadButton anchor: its data-scrambled-url attribute (base64 of
//     the CDN URL), then its plain href
//  2. Any download<NNN>.mediafire.com href in the page
//  3. Any data-scrambled-url attribute in the page
//
// Scrambled payloads are decoded and validated against the
// download*.mediafire.com host so attacker-controlled scramble data cannot
// redirect downloads elsewhere.
func extractMediafireCDNURL(html string) string {
	if anchor := reMFDownloadButton.FindString(html); anchor != "" {
		if dl := decodeScrambledURL(anchor); dl != "" {
			return dl
		}
		if m := reMFButtonHref.FindStringSubmatch(anchor); len(m) >= 2 && isMediafireCDNURL(m[1]) {
			return m[1]
		}
	}
	if m := reMFCDNHref.FindString(html); m != "" {
		return m
	}
	return decodeScrambledURL(html)
}

// decodeScrambledURL base64-decodes a data-scrambled-url attribute value and
// returns it when it decodes to a download*.mediafire.com URL.
func decodeScrambledURL(s string) string {
	m := reMFScrambledURL.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	decoded, err := decodeMediafireBase64(m[1])
	if err != nil {
		return ""
	}
	raw := string(decoded)
	if isMediafireCDNURL(raw) {
		return raw
	}
	return ""
}

// decodeMediafireBase64 decodes a base64 string, tolerating both padded and
// unpadded encodings.
func decodeMediafireBase64(encoded string) ([]byte, error) {
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding} {
		if b, err := enc.DecodeString(encoded); err == nil {
			return b, nil
		}
	}
	return nil, fmt.Errorf("invalid base64 data")
}

// isMediafireCDNURL reports whether raw is an http(s) URL on a
// download*.mediafire.com host — the MediaFire CDN.
func isMediafireCDNURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return reMFCDNHost.MatchString(strings.ToLower(u.Hostname()))
}
