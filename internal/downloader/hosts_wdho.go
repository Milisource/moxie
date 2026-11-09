package downloader

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

// --- WDHO ---
// WDHO fronts downloads with a custom "click the button matching this colour"
// challenge after a device/JS check. That is an interactive anti-bot check, so
// moxie does not attempt to solve it. This resolver detects the challenge and
// reports it clearly instead of saving the HTML page as if it were the file.
// A direct file response from WDHO (CDN URL or an already-cleared session) is
// still passed through.
func (r *HostResolver) resolveWDHO(rawURL string) (*ResolveResult, error) {
	if BrowserResolverEnabled() {
		return r.resolveWDHOBrowser(rawURL)
	}
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("wdho request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
	req.Header.Set("Accept", "*/*")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wdho: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") && resp.StatusCode == http.StatusOK {
		return &ResolveResult{URL: rawURL, Headers: browserHeaders(rawURL)}, nil
	}
	if strings.Contains(string(body), "btn-color") || strings.Contains(string(body), `name="antibot"`) {
		return nil, fmt.Errorf("wdho requires an interactive colour-button check — use --open for this link")
	}
	return nil, fmt.Errorf("wdho: unexpected response (HTTP %d)", resp.StatusCode)
}
