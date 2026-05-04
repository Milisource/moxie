package downloader

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// HostResolver resolves a file host URL to a direct downloadable URL.
// Many hosts require cookies, headers, or API calls before serving the file.
type HostResolver struct {
	client *http.Client
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
}

// Resolve takes a URL and host label, returns a direct download URL + headers.
// If no host-specific handler exists, returns the URL as-is for direct download.
func (r *HostResolver) Resolve(url string, host string) (*ResolveResult, error) {
	switch host {
	case "pixeldrain":
		return r.resolvePixeldrain(url)
	case "buzzheavier":
		return r.resolveBuzzheavier(url)
	case "gofile":
		return r.resolveGofile(url)
	case "vikingfile":
		return r.resolveVikingFile(url)
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

// --- Pixeldrain ---
// API: GET https://pixeldrain.com/api/file/<FILE_ID>
// Direct download URL, no special headers needed.
func (r *HostResolver) resolvePixeldrain(url string) (*ResolveResult, error) {
	// Extract file ID from various URL formats:
	// https://pixeldrain.com/u/<FILE_ID>
	// https://pixeldrain.com/api/file/<FILE_ID>
	re := regexp.MustCompile(`pixeldrain\.com/(?:u|api/file)/([a-zA-Z0-9_-]+)`)
	matches := re.FindStringSubmatch(url)
	if len(matches) < 2 {
		return nil, fmt.Errorf("could not extract Pixeldrain file ID from: %s", url)
	}
	fileID := matches[1]
	directURL := fmt.Sprintf("https://pixeldrain.com/api/file/%s", fileID)
	return &ResolveResult{
		URL: directURL,
		Headers: map[string]string{
			"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
		},
	}, nil
}

// --- Buzzheavier ---
// Uses HTMX-based download flow: GET <url>/download with HX-Request header
// Response includes hx-redirect header with the actual download URL.
// Also supports direct DD (buzzheavier.com/dd/<ID>) and torrents.
func (r *HostResolver) resolveBuzzheavier(url string) (*ResolveResult, error) {
	// Clean the URL
	url = strings.TrimRight(url, "/")

	// Try direct domain first
	ddURL := strings.Replace(url, "buzzheavier.com", "dd.buzzheavier.com", 1)
	resp, err := r.client.Get(ddURL)
	if err == nil && resp.StatusCode == http.StatusOK {
		resp.Body.Close()
		// If it's a file (has Content-Disposition), we can download directly
		if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.Contains(ct, "text/html") {
			return &ResolveResult{
				URL: ddURL,
				Headers: map[string]string{
					"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
				},
			}, nil
		}
	} else if resp != nil {
		resp.Body.Close()
	}

	// Use HTMX-based resolution: GET <url>/download with HX-Request header
	dlURL := url + "/download"
	req, err := http.NewRequest("GET", dlURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create buzzheavier request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Current-URL", url)
	req.Header.Set("Referer", url)
	req.Header.Set("Accept", "*/*")

	resp, err = r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("buzzheavier resolve: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusFound {
		return nil, fmt.Errorf("buzzheavier: HTTP %d", resp.StatusCode)
	}

	// The actual download URL is in the hx-redirect header
	redirectURL := resp.Header.Get("hx-redirect")
	if redirectURL == "" {
		// Some versions return it in body
		body, _ := io.ReadAll(resp.Body)
		redirectURL = strings.TrimSpace(string(body))
	}
	if redirectURL == "" {
		// Fallback: try the URL directly
		redirectURL = url
	}

	// Ensure absolute URL
	if strings.HasPrefix(redirectURL, "/") {
		redirectURL = "https://dd.buzzheavier.com" + redirectURL
	}

	return &ResolveResult{
		URL: redirectURL,
		Headers: map[string]string{
			"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
			"Referer":    url,
		},
	}, nil
}

// --- Gofile ---
// API: GET https://api.gofile.io/getContent?contentId=<FILE_ID>&wt=4fd6sg89d7s6
// Then redirects to download server.
func (r *HostResolver) resolveGofile(url string) (*ResolveResult, error) {
	// Extract file ID from URL:
	// https://gofile.io/d/<FILE_ID>
	re := regexp.MustCompile(`gofile\.io/(?:d|download)/([a-zA-Z0-9]+)`)
	matches := re.FindStringSubmatch(url)
	if len(matches) < 2 {
		// Try direct download subdomain
		if strings.Contains(url, ".gofile.io") {
			return &ResolveResult{
				URL: url,
				Headers: map[string]string{
					"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
				},
			}, nil
		}
		return nil, fmt.Errorf("could not extract Gofile file ID from: %s", url)
	}
	fileID := matches[1]

	// Gofile requires an account token for full-speed downloads in 2025+.
	// For free downloads, use the direct download link pattern.
	directURL := fmt.Sprintf("https://%s.gofile.io/%s", fileID, fileID)

	// Try the content API first (may work without auth for some files)
	apiURL := fmt.Sprintf("https://api.gofile.io/getContent?contentId=%s", fileID)
	req, _ := http.NewRequest("GET", apiURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "application/json")

	resp, err := r.client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			// API returned content - parse response for download URL
			body, _ := io.ReadAll(resp.Body)
			return parseGofileAPIResponse(body, directURL)
		}
	}

	// Fallback: try direct download
	return &ResolveResult{
		URL: directURL,
		Headers: map[string]string{
			"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
			"Cookie":     "accountToken=guest",
		},
	}, nil
}

// parseGofileAPIResponse attempts to extract a download URL from the Gofile API JSON response.
func parseGofileAPIResponse(body []byte, fallbackURL string) (*ResolveResult, error) {
	// The response looks like: {"status":"ok","data":{"<fileId>":{"downloadPage":"https://...",...}}}
	// For now, just return the fallback. Full JSON parsing would add complexity.
	return &ResolveResult{
		URL: fallbackURL,
		Headers: map[string]string{
			"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
		},
	}, nil
}

// --- VikingFile ---
// Direct download via standard HTTP.
func (r *HostResolver) resolveVikingFile(url string) (*ResolveResult, error) {
	return &ResolveResult{
		URL: url,
		Headers: map[string]string{
			"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
		},
	}, nil
}

// --- Mega ---
// Mega uses proprietary encrypted protocol. Cannot be handled with simple HTTP.
// Inform user to use megatools CLI or download manually in browser.
func (r *HostResolver) resolveMega(url string) (*ResolveResult, error) {
	return nil, fmt.Errorf(
		"Mega uses encrypted protocol - use megatools CLI:\n"+
			"  megatools dl --path <dest> '%s'\n"+
			"  Install: brew install megatools / apt install megatools",
		url,
	)
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
