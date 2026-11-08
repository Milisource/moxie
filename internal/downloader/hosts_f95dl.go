package downloader

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/mili/moxie/internal/log"
)

// resolveF95DL handles F95Zone's /dl/<postID>/... wrapper links used in current
// thread layouts. Some resolve with a plain 302 to the real host. Others show
// an hCaptcha "Just a quick check" interstitial, which cannot be solved without
// a browser; those return a descriptive error so the caller's fallback loop can
// try another link.
func (r *HostResolver) resolveF95DL(rawURL string) (*ResolveResult, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("f95 dl create GET: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("Referer", "https://f95zone.to/")
	if r.f95Cookie != "" {
		req.Header.Set("Cookie", r.f95Cookie)
	}

	client := r.client
	if client == nil {
		client = &http.Client{}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("f95 dl GET: %w", err)
	}
	defer resp.Body.Close()

	final := resp.Request.URL
	if final != nil && !strings.EqualFold(final.Hostname(), "f95zone.to") {
		realURL := final.String()
		log.Info("F95Zone /dl link resolved", "host", IdentifyHostInURL(realURL))
		return r.Resolve(realURL, IdentifyHostInURL(realURL))
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	page := strings.ToLower(string(body))
	switch {
	case strings.Contains(page, "that link is no longer in the post"):
		return nil, fmt.Errorf("F95Zone /dl link is stale (no longer in the post)")
	case strings.Contains(page, "just a quick check") || strings.Contains(page, "h-captcha") || strings.Contains(page, "data-sitekey"):
		return nil, fmt.Errorf("F95Zone /dl link requires an hCaptcha browser check — use --open for this link")
	case strings.Contains(page, "you are not logged in") || isLoginRedirect(rawURL):
		return nil, fmt.Errorf("F95Zone session expired — re-import cookies")
	case resp.StatusCode >= 400:
		return nil, fmt.Errorf("F95Zone /dl returned HTTP %d", resp.StatusCode)
	default:
		return nil, fmt.Errorf("F95Zone /dl did not redirect to a file host (HTTP %d)", resp.StatusCode)
	}
}

// IsF95DLURL reports whether rawURL is a tokenized F95Zone /dl/ wrapper link.
// These tokens are tied to a rendered page and expire, so callers should
// re-scrape instead of reusing stored /dl/ links.
func IsF95DLURL(rawURL string) bool { return isF95DLURL(rawURL) }

// isF95DLURL reports whether rawURL is an F95Zone /dl/ wrapper link.
func isF95DLURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Hostname(), "f95zone.to") && strings.HasPrefix(u.Path, "/dl/")
}
