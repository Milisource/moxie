package downloader

// --- MixDrop ---
// Direct download via standard HTTP.
func (r *HostResolver) resolveMixdrop(url string) (*ResolveResult, error) {
	return &ResolveResult{
		URL: url,
		Headers: map[string]string{
			"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
		},
	}, nil
}
