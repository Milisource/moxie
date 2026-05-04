package downloader

import (
	"fmt"
	"net/http"
	"time"
)

// CheckLink validates a download URL by making a HEAD request.
// Returns nil if the link is valid, or an error describing why it's dead.
// For host-specific hosts (Buzzheavier, Mega, etc.) this auto-detects
// the host and delegates to CheckLinkWithHost.
func CheckLink(url string) error {
	host := IdentifyHostInURL(url)
	return CheckLinkWithHost(url, host)
}

// CheckLinkWithHost validates a download URL with explicit host awareness.
// For hosts that require special resolution (Buzzheavier HTMX, etc.), it
// uses the HostResolver to get the real download URL before checking.
func CheckLinkWithHost(url, host string) error {
	// For host-specific resolvers that don't serve direct HTTP, use the
	// resolver to validate the link instead of a simple HEAD request.
	switch host {
	case "buzzheavier":
		resolver := NewHostResolver()
		_, err := resolver.Resolve(url, host)
		if err != nil {
			return fmt.Errorf("Buzzheavier: %w", err)
		}
		return nil
	case "mega":
		return fmt.Errorf("Mega uses encrypted protocol — cannot validate via HTTP")
	}

	if !isValidDownloadURL(url) {
		return fmt.Errorf("invalid URL")
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
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
		if resp.StatusCode >= 500 {
			return fmt.Errorf("%d Server Error - host error", resp.StatusCode)
		}
		if resp.StatusCode >= 400 {
			return fmt.Errorf("%d Client Error", resp.StatusCode)
		}
		return nil
	}
}
