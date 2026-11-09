package downloader

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// --- Gofile ---
// Current API flow (2026):
//
//  1. POST https://api.gofile.io/accounts → anonymous account token
//  2. GET https://api.gofile.io/contents/<id> with Authorization: Bearer
//     <token>, X-Website-Token and X-BL headers. The website token is a
//     SHA-256 of userAgent::language::token::timeBucket::salt where the
//     time bucket is floor(unix/14400); it is what the site's own
//     /js/wt.obf.js generates.
//  3. The response's data.children map contains file entries with a direct
//     "link" field. Folders are traversed one level.
//
// The legacy <id>.gofile.io host is gone; it is kept only as a last resort
// because DNS for it no longer resolves.
const (
	gofileUserAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0"
	gofileLanguage  = "en-US"
	gofileWTSalt    = "12af056dacea0b"
	gofileWTBucket  = 14400
)

func (r *HostResolver) resolveGofile(url string) (*ResolveResult, error) {
	re := regexp.MustCompile(`gofile\.io/(?:d|download)/([a-zA-Z0-9-]+)`)
	matches := re.FindStringSubmatch(url)
	if len(matches) < 2 {
		if strings.Contains(url, ".gofile.io") {
			return &ResolveResult{URL: url, Headers: gofileHeaders("")}, nil
		}
		return nil, fmt.Errorf("could not extract Gofile file ID from: %s", url)
	}
	contentID := matches[1]

	token := r.gofileToken()
	if token == "" {
		return nil, fmt.Errorf("gofile: could not create a guest account (api.gofile.io unreachable)")
	}
	direct, err := r.gofileContents(contentID, token)
	if err != nil {
		return nil, fmt.Errorf("gofile: %w", err)
	}
	if direct == "" {
		return nil, fmt.Errorf("gofile: no downloadable file found in %s", contentID)
	}
	return &ResolveResult{URL: direct, Headers: gofileHeaders(token)}, nil
}

func gofileHeaders(token string) map[string]string {
	h := map[string]string{
		"User-Agent":      gofileUserAgent,
		"Accept-Language": gofileLanguage,
		"Referer":         "https://gofile.io/",
	}
	if token != "" {
		h["Cookie"] = "accountToken=" + token
	}
	return h
}

// gofileWebsiteToken mirrors /js/wt.obf.js generateWT(). The server rotates
// the salt occasionally; when it does, content requests start returning 401
// and this constant has to be refreshed from the live script.
func gofileWebsiteToken(accountToken string) string {
	return gofileWebsiteTokenAt(accountToken, time.Now())
}

func gofileWebsiteTokenAt(accountToken string, now time.Time) string {
	bucket := strconv.FormatInt(now.Unix()/gofileWTBucket, 10)
	sum := sha256.Sum256([]byte(gofileUserAgent + "::" + gofileLanguage + "::" + accountToken + "::" + bucket + "::" + gofileWTSalt))
	return hex.EncodeToString(sum[:])
}

// gofileToken creates an anonymous Gofile account and returns its token.
// The API switched from GET to POST /accounts in 2026; the GET form now
// returns 404, which used to make every resolution fall back to the dead
// <id>.gofile.io host.
func (r *HostResolver) gofileToken() string {
	req, err := http.NewRequest("POST", "https://api.gofile.io/accounts", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
	req.Header.Set("Accept", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var out struct {
		Status string `json:"status"`
		Data   struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := decodeJSON(resp.Body, &out); err != nil {
		return ""
	}
	return out.Data.Token
}

// gofileContents returns the first direct file link under a content ID,
// descending up to two folder levels.
func (r *HostResolver) gofileContents(contentID, token string) (string, error) {
	return r.gofileContentAt(contentID, token, 0)
}

func (r *HostResolver) gofileContentAt(contentID, token string, depth int) (string, error) {
	apiURL := fmt.Sprintf("https://api.gofile.io/contents/%s?page=1&pageSize=100", contentID)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("gofile request: %w", err)
	}
	req.Header.Set("User-Agent", gofileUserAgent)
	req.Header.Set("Accept-Language", gofileLanguage)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Website-Token", gofileWebsiteToken(token))
	req.Header.Set("X-BL", gofileLanguage)
	resp, err := r.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gofile contents: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gofile contents: HTTP %d", resp.StatusCode)
	}

	var out struct {
		Status string `json:"status"`
		Data   struct {
			Type     string `json:"type"`
			Children map[string]struct {
				Type string `json:"type"`
				Link string `json:"link"`
			} `json:"children"`
		} `json:"data"`
	}
	if err := decodeJSON(resp.Body, &out); err != nil {
		return "", fmt.Errorf("gofile decode: %w", err)
	}
	if out.Status != "ok" {
		return "", fmt.Errorf("gofile status: %s", out.Status)
	}
	for id, child := range out.Data.Children {
		if child.Link != "" {
			return child.Link, nil
		}
		if child.Type == "folder" && depth < 2 {
			if link, err := r.gofileContentAt(id, token, depth+1); err == nil && link != "" {
				return link, nil
			}
		}
	}
	return "", fmt.Errorf("gofile: no direct link in contents")
}
