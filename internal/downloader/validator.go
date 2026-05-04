package downloader

import (
	"fmt"
	"net/http"
	"time"
)

// CheckLink validates a download URL by making a HEAD request.
// Returns nil if the link is valid, or an error describing why it's dead.
func CheckLink(url string) error {
	if !isValidDownloadURL(url) {
		return fmt.Errorf("invalid URL")
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Follow redirects, but limit to 10
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
