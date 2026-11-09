package downloader

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/mili/moxie/internal/log"
)

// HostResolver resolves a file host URL to a direct downloadable URL.
// Many hosts require cookies, headers, or API calls before serving the file.
type HostResolver struct {
	client    *http.Client
	f95Cookie string
}

// SetF95Cookie sets the F95Zone session cookie string used to authenticate
// link-unmasking requests on F95Zone.
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
	}
}

// ResolveResult contains the resolved download URL and any required headers.
type ResolveResult struct {
	URL     string
	Headers map[string]string

	// ExternalCommand, when non-empty, means the URL cannot be fetched with
	// plain HTTP and must be downloaded by running this command. The value is
	// the executable name (e.g. "megatools"); DownloadWithHost adds the URL
	// and destination arguments.
	ExternalCommand string
}

// Resolve takes a URL and host label, returns a direct download URL + headers.
// If no host-specific handler exists, returns the URL as-is for direct download.
// F95Zone masked URLs (f95zone.to/masked/) are unmasked via the same JSON
// endpoint the site's own masked.js uses, then host-specific resolution is
// applied to the real download URL.
func (r *HostResolver) Resolve(url string, host string) (*ResolveResult, error) {
	log.Debug("resolving host URL", "host", host, "url", url)

	if strings.Contains(strings.ToLower(url), "/masked/") {
		realURL, err := r.resolveMasked(url)
		if err != nil {
			return nil, err
		}
		realHost := IdentifyHostInURL(realURL)
		log.Debug("masked URL resolved", "original", url, "real", realURL, "real_host", realHost)
		return r.Resolve(realURL, realHost)
	}

	if isF95DLURL(url) {
		return r.resolveF95DL(url)
	}

	switch host {
	case "pixeldrain":
		return r.resolvePixeldrain(url)
	case "buzzheavier":
		return r.resolveBuzzheavierWithFallback(url)
	case "gofile":
		return r.resolveGofile(url)
	case "datanodes":
		return r.resolveDatanodesWithFallback(url)
	case "vikingfile":
		return r.resolveVikingFileWithFallback(url)
	case "mixdrop":
		return r.resolveMixdrop(url)
	case "google drive", "googledrive":
		return r.resolveGoogleDrive(url)
	case "mega":
		return r.resolveMega(url)
	case "mediafire":
		return r.resolveMediafire(url)
	case "bunkrr", "bunkr":
		return r.resolveBunkrr(url)
	case "dropbox":
		return r.resolveDropbox(url)
	case "workupload":
		return r.resolveWorkupload(url)
	case "uploadhaven":
		return r.resolveUploadHavenWithFallback(url)
	case "wdho":
		return r.resolveWDHO(url)
	case "krakenfiles":
		return r.resolveKrakenFilesWithFallback(url)
	default:
		// Direct download - just pass through
		return &ResolveResult{
			URL:     url,
			Headers: map[string]string{},
		}, nil
	}
}

// ---------------------------------------------------------------------------
// F95Zone masked links
// ---------------------------------------------------------------------------

type maskedCacheEntry struct {
	url     string
	expires time.Time
}

var (
	maskedCacheMu sync.Mutex
	maskedCache   = map[string]maskedCacheEntry{}
	// maskedCacheTTL is deliberately long: unmasking is cheap for F95Zone but
	// each request counts toward the site's anti-bot rate limit. Cached results
	// let repeated downloads avoid touching F95Zone at all.
	maskedCacheTTL = 12 * time.Hour
)

// resolveMasked unwraps https://f95zone.to/masked/... using the JSON endpoint
// that the site's masked.js calls (POST xhr=1&download=1 on the masked URL):
//
//	{"status":"ok","msg":"https://pixeldrain.com/u/..."}
//	{"status":"captcha","msg":"Please complete the CAPTCHA to continue"}
//	{"status":"error","msg":"..."}
//
// The endpoint requires an authenticated F95Zone session (xf_user cookie).
// After several automated requests F95Zone raises an interactive CAPTCHA;
// callers get a clear error so they can fall back to another link.
func (r *HostResolver) resolveMasked(maskedURL string) (string, error) {
	maskedCacheMu.Lock()
	if e, ok := maskedCache[maskedURL]; ok && time.Now().Before(e.expires) {
		maskedCacheMu.Unlock()
		log.Debug("masked URL cache hit", "url", maskedURL)
		return e.url, nil
	}
	maskedCacheMu.Unlock()

	if store := getResolutionStore(); store != nil {
		if resolved, host, ok := store.GetResolvedURL(maskedURL, maskedCacheTTL); ok && isValidDownloadURL(resolved) {
			maskedCacheMu.Lock()
			maskedCache[maskedURL] = maskedCacheEntry{url: resolved, expires: time.Now().Add(maskedCacheTTL)}
			maskedCacheMu.Unlock()
			log.Debug("masked URL DB cache hit", "url", maskedURL, "host", host)
			return resolved, nil
		}
	}

	if !isValidDownloadURL(maskedURL) {
		return "", fmt.Errorf("invalid masked URL: %s", maskedURL)
	}
	if r.f95Cookie == "" {
		return "", fmt.Errorf("F95Zone masked link requires a session cookie — import browser cookies or pass --cookie")
	}

	form := url.Values{"xhr": {"1"}, "download": {"1"}}
	req, err := http.NewRequest("POST", maskedURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("create masked request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Referer", maskedURL)
	req.Header.Set("Cookie", r.f95Cookie)

	client := r.client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("masked request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", fmt.Errorf("read masked response: %w", err)
	}

	var result struct {
		Status string `json:"status"`
		Msg    string `json:"msg"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		// Cloudflare or a login page was returned instead of JSON.
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusServiceUnavailable {
			return "", fmt.Errorf("F95Zone blocked the unmask request (HTTP %d) — cookies may be stale", resp.StatusCode)
		}
		return "", fmt.Errorf("unexpected masked response (HTTP %d)", resp.StatusCode)
	}

	switch result.Status {
	case "ok":
		if !isValidDownloadURL(result.Msg) {
			return "", fmt.Errorf("F95Zone returned an invalid download URL")
		}
		maskedCacheMu.Lock()
		maskedCache[maskedURL] = maskedCacheEntry{url: result.Msg, expires: time.Now().Add(maskedCacheTTL)}
		maskedCacheMu.Unlock()
		if store := getResolutionStore(); store != nil {
			if err := store.SaveResolvedURL(maskedURL, result.Msg, IdentifyHostInURL(result.Msg)); err != nil {
				log.Warn("failed to cache masked URL resolution", "error", err)
			}
		}
		log.Info("F95Zone masked link resolved", "host", IdentifyHostInURL(result.Msg))
		return result.Msg, nil
	case "captcha":
		return "", fmt.Errorf("F95Zone requires a CAPTCHA to unmask this link — retry later or use --open (link: %s)", maskedURL)
	case "error":
		if result.Msg == "" {
			result.Msg = "unknown error"
		}
		if isLoginRedirect(result.Msg) || strings.Contains(strings.ToLower(result.Msg), "logged in") {
			return "", fmt.Errorf("F95Zone session expired — re-import cookies from your browser")
		}
		return "", fmt.Errorf("F95Zone unmask failed: %s", result.Msg)
	default:
		return "", fmt.Errorf("F95Zone unmask returned status %q", result.Status)
	}
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
		{"buzzheavier", "bzzhr"},
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
		{"google drive", "drive.google"},
		{"hexload", "hexload"},
		{"hexload", "hexupload"},
		{"mixdrop", "mixdrop"},
		{"mixdrop", "m1xdrop"},
		{"mixdrop", "mxdrop"},
		{"protondrive", "proton drive"},
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
