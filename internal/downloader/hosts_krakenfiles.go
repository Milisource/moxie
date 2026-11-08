package downloader

import "fmt"

// resolveKrakenFilesWithFallback uses the headed-browser resolver when enabled.
// KrakenFiles has no usable HTTP-only flow: the download POST requires a
// Turnstile token and FingerprintJS/behavior data.
func (r *HostResolver) resolveKrakenFilesWithFallback(rawURL string) (*ResolveResult, error) {
	if BrowserResolverEnabled() {
		return r.resolveKrakenFilesBrowser(rawURL)
	}
	return nil, fmt.Errorf("krakenfiles: requires the browser resolver — retry with --browser")
}
