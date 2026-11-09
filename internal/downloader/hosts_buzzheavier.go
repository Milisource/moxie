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

// --- Buzzheavier ---
// The file page exposes an HTMX download endpoint:
//
//	<a class="download-btn" hx-get="/<id>/download?t=<signed-token>">
//
// and the endpoint answers with an empty 204 whose hx-redirect header is the
// signed CDN URL (ts.bzzhr.to/d/<id>?v=...). Cloudflare often rejects plain
// HTTP clients with 403, in which case the headed browser resolver performs
// the same request from inside the already-validated page.
var buzzheavierHXGet = regexp.MustCompile(`hx-get="(/[^"]+/download\?t=[^"]+)"`)

func (r *HostResolver) resolveBuzzheavierWithFallback(rawURL string) (*ResolveResult, error) {
	if store := getResolutionStore(); store != nil {
		if cached, _, ok := store.GetResolvedURL(rawURL, time.Hour); ok && isValidDownloadURL(cached) {
			log.Debug("buzzheavier cache hit", "url", rawURL)
			return &ResolveResult{URL: cached, Headers: browserHeaders(rawURL)}, nil
		}
	}

	// First try plain HTTP. It works when the caller's IP already has a
	// Cloudflare clearance cookie or when the host is not challenging.
	if direct, err := r.resolveBuzzheavierHTTP(rawURL); err == nil && direct != "" {
		log.Info("buzzheavier resolved over HTTP", "target_host", strings.Split(strings.TrimPrefix(direct, "https://"), "/")[0])
		cacheBrowserResolution(rawURL, direct, "buzzheavier")
		return &ResolveResult{URL: direct, Headers: browserHeaders(rawURL)}, nil
	}

	if !BrowserResolverEnabled() {
		return nil, fmt.Errorf("buzzheavier is behind a Cloudflare challenge — retry with --browser")
	}
	return r.resolveBuzzheavierBrowser(rawURL)
}

func (r *HostResolver) resolveBuzzheavierHTTP(rawURL string) (string, error) {
	base := strings.TrimRight(rawURL, "/")
	req, err := http.NewRequest("GET", base, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("buzzheavier page: HTTP %d", resp.StatusCode)
	}
	m := buzzheavierHXGet.FindStringSubmatch(string(body))
	if len(m) < 2 {
		return "", fmt.Errorf("buzzheavier: no HTMX download endpoint on page")
	}

	hxURL := m[1]
	if strings.HasPrefix(hxURL, "/") {
		if u, err := url.Parse(base); err == nil {
			hxURL = u.Scheme + "://" + u.Host + hxURL
		}
	}
	hreq, err := http.NewRequest("GET", hxURL, nil)
	if err != nil {
		return "", err
	}
	hreq.Header.Set("User-Agent", req.Header.Get("User-Agent"))
	hreq.Header.Set("HX-Request", "true")
	hreq.Header.Set("HX-Current-URL", base)
	hreq.Header.Set("Referer", base)
	hreq.Header.Set("Accept", "*/*")
	hresp, err := r.client.Do(hreq)
	if err != nil {
		return "", err
	}
	hresp.Body.Close()
	if hresp.StatusCode != http.StatusOK && hresp.StatusCode != http.StatusNoContent {
		return "", fmt.Errorf("buzzheavier download endpoint: HTTP %d", hresp.StatusCode)
	}
	direct := hresp.Header.Get("HX-Redirect")
	if direct == "" {
		direct = hresp.Header.Get("hx-redirect")
	}
	if !isValidDownloadURL(direct) {
		return "", fmt.Errorf("buzzheavier: no hx-redirect in response")
	}
	return direct, nil
}
