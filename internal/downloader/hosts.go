package downloader

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mili/moxie/internal/browser"
	"github.com/mili/moxie/internal/log"
)

// HostResolver resolves a file host URL to a direct downloadable URL.
// Many hosts require cookies, headers, or API calls before serving the file.
type HostResolver struct {
	client    *http.Client
	f95Cookie string
	// cookieSource returns a Cookie header value valid for a hostname, or ""
	// when the browser holds none for it. Defaults to browser-backed
	// extraction; a nil value disables cookie attachment (also the
	// zero-value state, used by callers that construct the resolver
	// directly without NewHostResolver).
	cookieSource func(hostname string) string
}

// SetF95Cookie sets the F95Zone session cookie string used to authenticate
// HEAD requests when resolving masked F95Zone redirect URLs.
func (r *HostResolver) SetF95Cookie(cookie string) {
	r.f95Cookie = cookie
}

// NewHostResolver creates a resolver with a shared HTTP client.
func NewHostResolver() *HostResolver {
	return &HostResolver{
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
		cookieSource: browserCookieHeader,
	}
}

// browserCookieHeader extracts the browser's cookies for a hostname as a
// Cookie header value. Best-effort: extraction failures are logged and
// return "" so downloads proceed without clearance instead of failing on a
// cookie read error.
func browserCookieHeader(hostname string) string {
	if hostname == "" {
		return ""
	}
	header, err := browser.GetCookiesForHost(hostname)
	if err != nil {
		log.Debug("browser cookie extraction failed", "host", hostname, "error", err)
		return ""
	}
	return header
}

// attachBrowserCookies merges the browser's cookies for req's URL hostname
// into req's Cookie header. Cookies are host-scoped to the request's own
// domain (RFC 6265 domain match), so they never leak to redirect targets
// or other hosts.
func (r *HostResolver) attachBrowserCookies(req *http.Request) {
	mergeBrowserCookies(req, r.cookieSource)
}

// ResolveResult contains the resolved download URL and any required headers.
type ResolveResult struct {
	URL     string
	Headers map[string]string
}

// Resolve takes a URL and host label, returns a direct download URL + headers.
// If no host-specific handler exists, returns the URL as-is for direct download.
// F95Zone masked URLs (f95zone.to/masked/) are resolved via HEAD redirect first,
// then host-specific resolution is applied on the real download URL.
func (r *HostResolver) Resolve(url string, host string) (*ResolveResult, error) {
	return r.resolveDepth(url, host, 0)
}

// resolveDepth implements Resolve with a recursion bound so a malicious
// masked→masked redirect chain cannot recurse indefinitely.
func (r *HostResolver) resolveDepth(url string, host string, depth int) (*ResolveResult, error) {
	if depth > 5 {
		return nil, fmt.Errorf("masked URL chain too deep")
	}
	// Normalize legacy labels (e.g. "google drive" stored by older scrapes):
	// all canonical labels are single lowercase words.
	host = strings.ToLower(strings.ReplaceAll(host, " ", ""))
	log.Debug("resolving host URL", "host", host, "url", url)

	// F95Zone masked URLs are redirect endpoints — unwrap them first by following
	// a HEAD request to get the real download URL, then resolve from there.
	if strings.Contains(strings.ToLower(url), "/masked/") {
		log.Debug("masked URL detected", "url", url)
		realURL, err := r.unwrapMasked(url)
		realHost := IdentifyHostInURL(realURL)
		log.Debug("masked unwrap result", "real_url", realURL, "real_host", realHost, "error", err)
		if err != nil {
			log.Warn("failed to unwrap masked URL", "url", url, "error", err)
			// Fall through to host-specific resolution with the original URL;
			// it will fail, but the caller's fallback loop will try other links.
		} else if realURL != url {
			log.Debug("masked URL resolved", "original", url, "real", realURL)
			return r.resolveDepth(realURL, realHost, depth+1)
		}
	}

	switch host {
	case "pixeldrain":
		return r.resolvePixeldrain(url)
	case "buzzheavier":
		return r.resolveBuzzheavier(url)
	case "gofile":
		return r.resolveGofile(url)
	case "datanodes":
		return r.resolveDatanodes(url)
	case "vikingfile":
		return r.resolveVikingFile(url)
	case "workupload":
		return r.resolveWorkupload(url)
	case "mixdrop":
		return r.resolveMixdrop(url)
	case "googledrive":
		return r.resolveGoogleDrive(url)
	case "mega":
		return r.resolveMega(url)
	default:
		// Direct download - just pass through
		return &ResolveResult{
			URL:     url,
			Headers: map[string]string{},
		}, nil
	}
}

// unwrapMasked resolves an F95Zone masked URL to the real download URL.
// F95Zone's masked pages are a JS+reCAPTCHA interstitial ("Continue to
// <host>"), but the page's own AJAX endpoint — POST {xhr:1, download:1} to
// the masked path — returns the destination URL as JSON when a valid
// session cookie is sent:
//
//	{"status":"ok","msg":"https://pixeldrain.com/u/..."}
//
// A "captcha" status would require solving an invisible reCAPTCHA; with a
// fresh session cookie the endpoint typically answers "ok" directly, which
// is what makes masked links resolvable without a browser. Falls back to
// followRedirect (HTTP redirect chain) for older F95Zone deployments.
func (r *HostResolver) unwrapMasked(rawURL string) (string, error) {
	form := url.Values{}
	form.Set("xhr", "1")
	form.Set("download", "1")
	req, err := http.NewRequest("POST", rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("create masked unwrap request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Referer", rawURL)
	// Explicit caller cookie wins; otherwise fall back to browser cookies
	// for f95zone.to (the session may live only in the browser).
	if r.f95Cookie != "" {
		req.Header.Set("Cookie", r.f95Cookie)
	} else {
		r.attachBrowserCookies(req)
	}

	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("masked unwrap POST failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("masked unwrap read body: %w", err)
	}

	var result struct {
		Status string `json:"status"`
		Msg    string `json:"msg"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		// Not JSON — either the legacy redirect flow or an error page.
		// Reuse the redirect chain as a fallback.
		log.Debug("masked unwrap non-JSON response", "status", resp.StatusCode, "len", len(body))
		return r.followRedirect(rawURL)
	}
	log.Debug("masked unwrap response", "status_code", resp.StatusCode, "result_status", result.Status)
	if result.Status == "ok" && result.Msg != "" {
		return result.Msg, nil
	}
	if result.Status == "captcha" {
		return "", fmt.Errorf("masked URL requires a captcha solve (re-import cookies or open the link in a browser)")
	}
	if result.Status == "error" {
		return "", fmt.Errorf("masked URL endpoint error: %s", result.Msg)
	}
	return "", fmt.Errorf("masked URL endpoint returned unexpected status %q", result.Status)
}

// followRedirect performs a GET request to url and follows redirects to find
// the final destination URL. Used to unwrap F95Zone masked redirect endpoints
// before applying host-specific URL resolution.
//
// GET is used instead of HEAD because some F95Zone masked URL handlers do
// not serve the correct redirect on HEAD requests — they return the thread
// page or login page instead. The Referer is set to an f95zone.to domain
// to match the anti-hotlinking expectation.
func (r *HostResolver) followRedirect(url string) (string, error) {
	log.Debug("followRedirect request", "url", url, "has_cookie", r.f95Cookie != "")
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return url, fmt.Errorf("create redirect-follow request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("Referer", "https://f95zone.to/")
	if r.f95Cookie != "" {
		req.Header.Set("Cookie", r.f95Cookie)
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return url, fmt.Errorf("redirect-follow GET failed: %w", err)
	}
	defer resp.Body.Close()

	finalURL := resp.Request.URL.String()
	if finalURL == "" {
		return url, nil
	}

	// If the final URL is the F95Zone login page, the cookie is invalid or
	// the session expired. Signal this clearly so the caller can surface it.
	if isLoginRedirect(finalURL) {
		return url, fmt.Errorf("redirected to login — F95Zone session may have expired; re-import cookies")
	}

	log.Debug("followRedirect result", "original", url, "final", finalURL, "status", resp.StatusCode)
	return finalURL, nil
}

// isLoginRedirect returns true if the URL looks like an F95Zone login page.
func isLoginRedirect(rawURL string) bool {
	lower := strings.ToLower(rawURL)
	return strings.Contains(lower, "/login") || strings.Contains(lower, "_xfRedirect")
}

// IdentifyHostInURL extracts the host label from a URL for host-specific routing.
// Matches the same hosts as parser.go's identifyHost for consistency.
func IdentifyHostInURL(rawURL string) string {
	lower := strings.ToLower(rawURL)

	hosts := []struct {
		label  string
		needle string
	}{
		{"mega", "mega.nz"},
		{"mega", "mega.co"},
		{"pixeldrain", "pixeldrain"},
		{"buzzheavier", "buzzheavier"},
		{"buzzheavier", "bzzhr.to"}, // buzzheavier's short-link domain
		{"gofile", "gofile"},
		{"vikingfile", "vikingfile"},
		{"vern", "vern.cc"},
		{"mediafire", "mediafire"},
		{"workupload", "workupload"},
		{"bunkrr", "bunkrr"},
		{"bunkrr", "bunkr"},
		{"krakenfiles", "krakenfiles"},
		{"uploadhaven", "uploadhaven"},
		{"wetransfer", "wetransfer"},
		{"sendgb", "sendgb"},
		{"1cloudfile", "1cloudfile"},
		{"1cloudfile", "1cloud"},
		{"akirabox", "akirabox"},
		{"anontransfer", "anontransfer"},
		{"anonymfile", "anonymfile"},
		{"apkadmin", "apkadmin"},
		{"bowfile", "bowfile"},
		{"catbox", "catbox"},
		{"cyberfile", "cyberfile"},
		{"datanodes", "datanodes"},
		{"delafil", "delafil"},
		{"downloadgg", "download.gg"},
		{"dropmefiles", "dropmefiles"},
		{"easyupload", "easyupload"},
		{"filemail", "filemail"},
		{"filesdpua", "files.dp.ua"},
		{"filesdpua", "dp.ua"},
		{"filesfm", "files.fm"},
		{"filesfm", "filesfm"},
		{"fromsmash", "fromsmash"},
		{"googledrive", "drive.google"},
		{"hexload", "hexload"},
		{"hexload", "hexupload"},
		{"mixdrop", "mixdrop"},
		{"mixdrop", "m1xdrop"},
		{"mixdrop", "miixdrop"},
		{"protondrive", "proton drive"},
		{"protondrive", "proton.me"},
		{"quax", "qu.ax"},
		{"terminal", "terminal"},
		{"transfersh", "transfer.sh"},
		{"transfert", "transfert"},
		{"uploadnow", "uploadnow"},
		{"wdho", "wdho"},
		{"yourfilestore", "yourfilestore"},
		{"keep2share", "keep2share"},
		{"keep2share", "k2s"},
		{"uploaded", "uploaded"},
		{"uploaded", "ul.to"},
		{"dropbox", "dropbox"},
	}
	for _, h := range hosts {
		if strings.Contains(lower, h.needle) {
			return h.label
		}
	}
	return "unknown"
}
