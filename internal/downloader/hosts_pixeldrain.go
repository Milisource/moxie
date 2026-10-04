package downloader

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// --- Pixeldrain ---
// API: GET https://pixeldrain.com/api/file/<FILE_ID>
// Track D research (2026-08-09): pixeldrain serves reliably only when the
// request carries Referer matching the original /u/<FILE_ID> page URL, so
// derive it from the input URL before rewriting. /api/file/<FILE_ID> inputs
// have no page URL available; fall back to the bare origin (https://pixeldrain.com/),
// which keeps the Referer on the same origin without inventing a page that
// does not exist — safer than sending no Referer at all, and more truthful
// than fabricating a specific /u/ page.

const pixeldrainUserAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0"

var (
	pixeldrainOrigin = "https://pixeldrain.com"
	// pixeldrainInfoBase is the origin the /info throttle probe uses. A var so
	// tests can point it at an httptest server; the download URL stays on the
	// real origin.
	pixeldrainInfoBase = pixeldrainOrigin
	pixeldrainIDRe     = regexp.MustCompile(`pixeldrain\.com/(?:u|api/file)/([a-zA-Z0-9_-]+)`)
)

func (r *HostResolver) resolvePixeldrain(url string) (*ResolveResult, error) {
	// Extract file ID from various URL formats:
	// https://pixeldrain.com/u/<FILE_ID>
	// https://pixeldrain.com/api/file/<FILE_ID>
	matches := pixeldrainIDRe.FindStringSubmatch(url)
	if len(matches) < 2 {
		return nil, fmt.Errorf("could not extract Pixeldrain file ID from: %s", url)
	}
	fileID := matches[1]
	directURL := fmt.Sprintf("%s/api/file/%s", pixeldrainOrigin, fileID)

	// matches[0] is the full regex match; since the regex is case-sensitive,
	// "/u/" can only appear when the input was a page URL, never an API URL
	// (the regex stops at the file ID, before any query string).
	referer := pixeldrainOrigin + "/"
	if strings.Contains(matches[0], "/u/") {
		referer = fmt.Sprintf("%s/u/%s", pixeldrainOrigin, fileID)
	}

	return &ResolveResult{
		URL: directURL,
		Headers: map[string]string{
			"User-Agent": pixeldrainUserAgent,
			"Referer":    referer,
		},
	}, nil
}

// pixeldrainLimitErrors are StandardError "value" strings that mean the
// download is capped or blocked: wait, solve a captcha, or use another host.
// https://pixeldrain.com/api (file response table).
var pixeldrainLimitErrors = map[string]bool{
	"transfer_limit_exceeded":              true,
	"download_limit_exceeded":              true,
	"ip_download_limited_captcha_required": true,
	"max_concurrent_downloads":             true,
	"file_rate_limited_captcha_required":   true,
}

// pixeldrainFileInfo is the subset of GET /api/file/{id}/info moxie uses.
type pixeldrainFileInfo struct {
	Success             bool   `json:"success"`
	CanDownload         bool   `json:"can_download"`
	Availability        string `json:"availability"`
	AvailabilityMessage string `json:"availability_message"`
	// DownloadSpeedLimit is the server-enforced cap in bytes per second;
	// 0 means no limit. Non-zero once the free 24h transfer quota is exceeded.
	DownloadSpeedLimit int64 `json:"download_speed_limit"`
	// Value carries StandardError bodies, e.g. {"value":"transfer_limit_exceeded"}.
	Value string `json:"value"`
}

// probePixeldrainLimit queries the file-info endpoint and reports the
// server-side download speed cap. limited is true when a positive cap is
// reported or the API returns a known limit error. err is non-nil only when
// the state could not be determined (network/parse); callers treat that as
// "unknown — proceed".
func (r *HostResolver) probePixeldrainLimit(ctx context.Context, resolvedURL string) (limit int64, limited bool, err error) {
	fileID := pixeldrainFileID(resolvedURL)
	if fileID == "" {
		return 0, false, fmt.Errorf("could not extract pixeldrain file ID from %q", resolvedURL)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/api/file/%s/info", pixeldrainInfoBase, fileID), nil)
	if err != nil {
		return 0, false, err
	}
	req.Header.Set("User-Agent", pixeldrainUserAgent)
	req.Header.Set("Referer", fmt.Sprintf("%s/u/%s", pixeldrainOrigin, fileID))
	req.Header.Set("Accept", "application/json")

	client := r.client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, false, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return 0, false, err
	}
	var info pixeldrainFileInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return 0, false, fmt.Errorf("decode pixeldrain info: %w", err)
	}
	if pixeldrainLimitErrors[strings.ToLower(strings.TrimSpace(info.Value))] {
		return 0, true, nil
	}
	if resp.StatusCode != http.StatusOK {
		return 0, false, fmt.Errorf("pixeldrain info: HTTP %d", resp.StatusCode)
	}
	if info.DownloadSpeedLimit > 0 {
		return info.DownloadSpeedLimit, true, nil
	}
	return 0, false, nil
}

// pixeldrainFileID extracts the file ID from a pixeldrain /u/ or /api/file/ URL.
func pixeldrainFileID(rawURL string) string {
	if m := pixeldrainIDRe.FindStringSubmatch(rawURL); len(m) >= 2 {
		return m[1]
	}
	return ""
}
