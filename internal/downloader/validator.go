package downloader

import (
	"fmt"
	"net/http"
)

// CheckLink validates a download URL by making a HEAD request.
// Returns nil if the link is valid, or an error describing why it's dead.
// Hosts with a HostResolver (Buzzheavier, DataNodes, VikingFile, Pixeldrain,
// etc.) and F95Zone masked URLs are validated through the resolver first,
// because a raw HEAD on their stored URL returns a 403 challenge page or the
// mask interstitial even for live files.
func CheckLink(url string) error {
	host := IdentifyHostInURL(url)
	return CheckLinkWithHost(url, host)
}

// CheckLinkWithHost validates a download URL with explicit host awareness.
//
// Every host is routed through HostResolver.Resolve first: resolver-backed
// hosts (and masked URLs, which Resolve unwraps before host-specific
// resolution) come back with the real download URL plus any headers the
// download would need; pass-through/direct hosts come back with their URL
// unchanged and no headers, so the HEAD request that follows is equivalent
// to the plain request the old code made. The link is validated the same way
// the downloader would actually fetch it, instead of HEADing the stored URL
// and tripping on challenge pages and mask interstitials.
func CheckLinkWithHost(url, host string) error {
	if host == "mega" {
		return fmt.Errorf("Mega uses encrypted protocol — cannot validate via HTTP")
	}

	if !isValidDownloadURL(url) {
		return fmt.Errorf("invalid URL")
	}

	resolver := NewHostResolver()
	resolved, err := resolver.Resolve(url, host)
	if err != nil {
		// The host's own resolver failed — the link cannot be validated (or
		// downloaded) as-is. Resolver errors already name the host and cause.
		return err
	}

	return resolver.headURL(resolved.URL, resolved.Headers)
}

// headURL issues a HEAD request against target with headers applied, and
// returns nil when the response status indicates a live link. It mirrors the
// download path: the same URL-validity guard (SSRF protection), the same
// host-specific headers from the resolver, and the same browser cookies for
// the target host.
func (r *HostResolver) headURL(target string, headers map[string]string) error {
	if !isValidDownloadURL(target) {
		return fmt.Errorf("invalid URL")
	}

	client := r.client
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequest(http.MethodHead, target, nil)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	r.attachBrowserCookies(req)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	return statusError(resp.StatusCode)
}

// statusError maps an HTTP status code to a descriptive error, or nil when
// the status indicates a live link. 4xx/5xx statuses are dead links; 1xx-3xx
// (including redirects the client did not follow) are treated as live.
func statusError(code int) error {
	switch code {
	case http.StatusOK, http.StatusPartialContent:
		return nil
	case http.StatusNotFound:
		return fmt.Errorf("404 Not Found - file removed")
	case http.StatusForbidden:
		return fmt.Errorf("403 Forbidden - access denied or DMCA'd")
	case http.StatusGone:
		return fmt.Errorf("410 Gone - permanently removed")
	case http.StatusServiceUnavailable:
		return fmt.Errorf("503 Service Unavailable - host down")
	case http.StatusTooManyRequests:
		return fmt.Errorf("429 Too Many Requests - rate limited")
	default:
		if code >= 500 {
			return fmt.Errorf("%d Server Error - host error", code)
		}
		if code >= 400 {
			return fmt.Errorf("%d Client Error", code)
		}
		return nil
	}
}
