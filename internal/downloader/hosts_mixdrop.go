package downloader

import (
	"fmt"
	"time"

	"github.com/mili/moxie/internal/log"
)

// --- MixDrop ---
// MixDrop now serves downloads through a reCAPTCHA v3 (invisible) genticket
// exchange. That requires a real browser to execute grecaptcha, so the HTTP
// path only returns a clear error unless a cached browser resolution exists.
func (r *HostResolver) resolveMixdrop(url string) (*ResolveResult, error) {
	if store := getResolutionStore(); store != nil {
		if cached, _, ok := store.GetResolvedURL(url, time.Hour); ok && isValidDownloadURL(cached) {
			log.Debug("mixdrop cache hit", "url", url)
			return &ResolveResult{URL: cached, Headers: browserHeaders(url)}, nil
		}
	}
	if !BrowserResolverEnabled() {
		return nil, fmt.Errorf("mixdrop requires a browser (reCAPTCHA v3) — retry with --browser")
	}
	return r.resolveMixdropBrowser(url)
}
