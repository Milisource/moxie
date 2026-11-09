package downloader

import (
	"fmt"
	"time"

	"github.com/mili/moxie/internal/log"
)

// --- UploadHaven ---
// The free-download page combines a countdown with a Cloudflare Turnstile
// widget. There is no HTTP-only path: obtaining the Turnstile token requires
// executing the widget, so the headed browser resolver is mandatory.
func (r *HostResolver) resolveUploadHavenWithFallback(rawURL string) (*ResolveResult, error) {
	if store := getResolutionStore(); store != nil {
		if cached, _, ok := store.GetResolvedURL(rawURL, time.Hour); ok && isValidDownloadURL(cached) {
			log.Debug("uploadhaven cache hit", "url", rawURL)
			return &ResolveResult{URL: cached, Headers: browserHeaders(rawURL)}, nil
		}
	}
	if !BrowserResolverEnabled() {
		return nil, fmt.Errorf("uploadhaven requires a browser (Turnstile) — retry with --browser")
	}
	return r.resolveUploadHavenBrowser(rawURL)
}
