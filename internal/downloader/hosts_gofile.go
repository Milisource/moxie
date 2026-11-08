package downloader

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// --- Gofile ---
// Current API flow (2025+):
//
//  1. GET https://api.gofile.io/accounts → anonymous account token
//  2. GET https://api.gofile.io/contents/<id>?wt=4fd6sg89d7s6 with
//     Authorization: Bearer <token>
//  3. The response's data.children map contains file entries with a direct
//     "link" field. Folders are traversed one level.
//
// The legacy getContent endpoint is kept as a fallback.
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
	if token != "" {
		if direct, err := r.gofileContents(contentID, token); err == nil && direct != "" {
			return &ResolveResult{URL: direct, Headers: gofileHeaders(token)}, nil
		}
	}

	// Fallback: legacy direct download pattern.
	directURL := fmt.Sprintf("https://%s.gofile.io/%s", contentID, contentID)
	return &ResolveResult{URL: directURL, Headers: gofileHeaders(token)}, nil
}

func gofileHeaders(token string) map[string]string {
	h := map[string]string{
		"User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
		"Referer":    "https://gofile.io/",
	}
	if token != "" {
		h["Cookie"] = "accountToken=" + token
	}
	return h
}

// gofileToken creates an anonymous Gofile account and returns its token.
func (r *HostResolver) gofileToken() string {
	req, err := http.NewRequest("GET", "https://api.gofile.io/accounts", nil)
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
	apiURL := fmt.Sprintf("https://api.gofile.io/contents/%s?wt=4fd6sg89d7s6", contentID)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("gofile request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
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
