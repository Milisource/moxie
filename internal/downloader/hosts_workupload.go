package downloader

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// --- WorkUpload ---
// WorkUpload gates file pages behind a client-side proof-of-work ("Are you a
// human?"). The page calls GET /puzzle, brute-forces SHA-256(puzzle+i) until it
// matches every hash in the response's "find" list, then POSTs the winning
// indices to /captcha and reloads. That entire check is reproducible without a
// browser, so WorkUpload can be fully automated.
func (r *HostResolver) resolveWorkupload(rawURL string) (*ResolveResult, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("workupload cookie jar: %w", err)
	}
	transport := http.DefaultTransport
	if r.client != nil && r.client.Transport != nil {
		transport = r.client.Transport
	}
	client := &http.Client{
		Timeout:   30 * time.Second,
		Jar:       jar,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	ua := "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0"
	page, finalURL, err := fetchWorkuploadPage(client, rawURL, ua)
	if err != nil {
		return nil, err
	}

	if isWorkuploadChallenge(page) {
		if err := solveWorkuploadPuzzle(client, ua); err != nil {
			return nil, err
		}
		page, finalURL, err = fetchWorkuploadPage(client, rawURL, ua)
		if err != nil {
			return nil, err
		}
	}
	if isWorkuploadChallenge(page) {
		return nil, fmt.Errorf("workupload: proof-of-work was rejected")
	}
	if strings.Contains(strings.ToLower(page), "file not found") || strings.Contains(strings.ToLower(page), "file has been deleted") {
		return nil, fmt.Errorf("workupload: file not found or deleted")
	}

	// Direct link candidates, in reliability order.
	patterns := []string{
		`https://workupload\.com/file/[A-Za-z0-9]+/download[^"'\s<>]*`,
		`https://[a-zA-Z0-9.-]+\.workupload\.com/[^"'\s<>]+\.(?:zip|rar|7z|tar|gz|xz|bz2|mp4|mkv|avi|apk|exe|html)`,
	}
	for _, p := range patterns {
		if m := regexp.MustCompile(p).FindString(page); m != "" {
			return &ResolveResult{URL: m, Headers: map[string]string{"User-Agent": ua, "Referer": finalURL}}, nil
		}
	}
	// Direct-download anchor or JS variable.
	if m := regexp.MustCompile(`(?:id=["']directDownload["'][^>]*href=["']([^"']+)["']|href=["']([^"']+)["'][^>]*id=["']directDownload["'])`).FindStringSubmatch(page); len(m) > 0 {
		for _, g := range m[1:] {
			if g != "" {
				return &ResolveResult{URL: resolveRelativeURL(finalURL, g), Headers: map[string]string{"User-Agent": ua, "Referer": finalURL}}, nil
			}
		}
	}

	return nil, fmt.Errorf("workupload: no direct download link found (page layout may have changed)")
}

func fetchWorkuploadPage(client *http.Client, rawURL, ua string) (string, string, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("workupload create GET: %w", err)
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("workupload GET: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if err != nil {
		return "", "", fmt.Errorf("workupload read page: %w", err)
	}
	return string(body), resp.Request.URL.String(), nil
}

func isWorkuploadChallenge(page string) bool {
	lower := strings.ToLower(page)
	return strings.Contains(lower, "are you a human") || strings.Contains(lower, "security check") ||
		strings.Contains(lower, `url: "/puzzle"`)
}

func solveWorkuploadPuzzle(client *http.Client, ua string) error {
	req, err := http.NewRequest("GET", "https://workupload.com/puzzle", nil)
	if err != nil {
		return fmt.Errorf("workupload puzzle request: %w", err)
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("workupload puzzle: %w", err)
	}
	defer resp.Body.Close()

	var p struct {
		Success bool `json:"success"`
		Data    struct {
			Puzzle string   `json:"puzzle"`
			Range  int      `json:"range"`
			Find   []string `json:"find"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&p); err != nil {
		return fmt.Errorf("workupload puzzle decode: %w", err)
	}
	if p.Data.Puzzle == "" || p.Data.Range <= 0 || len(p.Data.Find) == 0 {
		return fmt.Errorf("workupload puzzle: malformed response")
	}

	targets := make(map[string]bool, len(p.Data.Find))
	for _, h := range p.Data.Find {
		targets[strings.ToLower(h)] = true
	}
	var found []int
	for i := 0; i < p.Data.Range && len(found) < len(p.Data.Find); i++ {
		sum := sha256.Sum256([]byte(p.Data.Puzzle + strconv.Itoa(i)))
		h := hex.EncodeToString(sum[:])
		if targets[h] {
			found = append(found, i)
			delete(targets, h)
		}
	}
	if len(found) != len(p.Data.Find) {
		return fmt.Errorf("workupload puzzle: no solution in range")
	}

	parts := make([]string, len(found))
	for i, v := range found {
		parts[i] = strconv.Itoa(v)
	}
	form := url.Values{"captcha": {strings.Join(parts, " ") + " "}}
	postReq, err := http.NewRequest("POST", "https://workupload.com/captcha", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("workupload captcha request: %w", err)
	}
	postReq.Header.Set("User-Agent", ua)
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	postReq.Header.Set("X-Requested-With", "XMLHttpRequest")
	postReq.Header.Set("Referer", "https://workupload.com/")
	postResp, err := client.Do(postReq)
	if err != nil {
		return fmt.Errorf("workupload captcha POST: %w", err)
	}
	defer postResp.Body.Close()
	if postResp.StatusCode != http.StatusOK {
		return fmt.Errorf("workupload captcha POST returned HTTP %d", postResp.StatusCode)
	}
	return nil
}
