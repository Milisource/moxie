package downloader

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// --- Gofile ---
// Free flow (2026): gofile's API requires a guest account token plus a
// dynamic X-Website-Token header derived from the site's own JS
// (wt.obf.js): wt = sha256(UA "::" lang "::" token "::" 4h-slot "::" secret).
// Without a correct wt the contents API answers error-notPremium — a
// missing/invalid wt, not actual premium gating. Flow:
//
//	1. POST https://api.gofile.io/accounts            -> guest token
//	2. GET  https://api.gofile.io/contents/<contentId> (with Authorization,
//	   X-Website-Token, X-BL)                          -> child file's "link"
//	3. That link is the direct download URL.
// gofileAPIBase is the gofile API origin. A var so tests can point it at an
// httptest server.
var gofileAPIBase = "https://api.gofile.io"

// gofileWTSecret is the static suffix extracted from the site's obfuscated
// wt.obf.js (verified against generateWT's runtime output, 2026-08-09).
const gofileWTSecret = "9844d94d963d30"

// gofileSlotSeconds is the time window (4h) used in the wt derivation.
const gofileSlotSeconds = 4 * 60 * 60

// gofileLanguage mirrors navigator.language for the wt derivation and X-BL.
const gofileLanguage = "en-US"

// gofileUserAgent must match the literal UA the site's JS embeds in the wt
// derivation; the browser cookie path may send a different UA, so always use
// this exact string for the wt computation itself.
const gofileUserAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0"

// gofileToken computes the X-Website-Token header value for a guest token
// using the current 4-hour slot.
func gofileToken(accountToken string) string {
	return gofileTokenWithSlot(accountToken, time.Now().Unix()/gofileSlotSeconds)
}

// gofileTokenWithSlot is the gofileToken derivation with an injectable slot
// (exposed for golden-vector tests; production uses gofileToken).
func gofileTokenWithSlot(accountToken string, slot int64) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s::%s::%s::%d::%s", gofileUserAgent, gofileLanguage, accountToken, slot, gofileWTSecret)
	return hex.EncodeToString(h.Sum(nil))
}

// gofileAPIClient is a short-timeout client for the gofile API calls.
var gofileAPIClient = &http.Client{Timeout: 20 * time.Second}

// gofileGuestToken creates a guest account and returns its token.
func gofileGuestToken() (string, error) {
	req, err := http.NewRequest(http.MethodPost, gofileAPIBase+"/accounts", nil)
	if err != nil {
		return "", fmt.Errorf("gofile create account request: %w", err)
	}
	req.Header.Set("User-Agent", gofileUserAgent)
	req.Header.Set("Origin", "https://gofile.io")
	req.Header.Set("Referer", "https://gofile.io/")
	resp, err := gofileAPIClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("gofile create account: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("gofile create account read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gofile create account: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var result struct {
		Status string `json:"status"`
		Data   struct {
			Token string `json:"token"`
			Tier  string `json:"tier"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("gofile create account: bad JSON: %w", err)
	}
	if result.Status != "ok" || result.Data.Token == "" {
		return "", fmt.Errorf("gofile create account: status %q, no token", result.Status)
	}
	return result.Data.Token, nil
}

// gofileContents fetches the contents JSON for a content ID using the guest
// token and returns the first direct download link found in its children
// (the free single-file flow; the bulk directlinks endpoint is premium).
func gofileContents(contentID, token string) (string, error) {
	u := gofileAPIBase + "/contents/" + url.PathEscape(contentID) +
		"?contentFilter=&page=1&pageSize=100&sortField=name&sortDirection=asc"
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("gofile contents request: %w", err)
	}
	req.Header.Set("User-Agent", gofileUserAgent)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Website-Token", gofileToken(token))
	req.Header.Set("X-BL", gofileLanguage)
	req.Header.Set("Origin", "https://gofile.io")
	req.Header.Set("Referer", "https://gofile.io/")
	resp, err := gofileAPIClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("gofile contents: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("gofile contents read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gofile contents: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var result struct {
		Status string `json:"status"`
		Data   struct {
			Link     string `json:"link"`
			Children map[string]struct {
				Link string `json:"link"`
				Type string `json:"type"`
			} `json:"children"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("gofile contents: bad JSON: %w", err)
	}
	if result.Status != "ok" {
		return "", fmt.Errorf("gofile contents: status %q", result.Status)
	}
	// A file share exposes the file in children with its download link;
	// prefer the first file-type child, then a top-level link.
	for _, child := range result.Data.Children {
		if child.Link != "" {
			return child.Link, nil
		}
	}
	if result.Data.Link != "" {
		return result.Data.Link, nil
	}
	return "", fmt.Errorf("gofile contents: no downloadable link found in response")
}

// resolveGofile resolves a gofile URL to a direct download URL via the
// guest token + dynamic website-token flow.
func (r *HostResolver) resolveGofile(url string) (*ResolveResult, error) {
	re := regexp.MustCompile(`gofile\.io/(?:d|download)/([a-zA-Z0-9]+)`)
	matches := re.FindStringSubmatch(url)
	if len(matches) < 2 {
		// A direct download subdomain URL (e.g. from a previously resolved
		// link) needs no further resolution.
		if strings.Contains(url, ".gofile.io") {
			return &ResolveResult{
				URL:     url,
				Headers: map[string]string{"User-Agent": gofileUserAgent},
			}, nil
		}
		return nil, fmt.Errorf("could not extract Gofile content ID from: %s", url)
	}
	contentID := matches[1]

	token, err := gofileGuestToken()
	if err != nil {
		return nil, err
	}
	directURL, err := gofileContents(contentID, token)
	if err != nil {
		return nil, err
	}
	return &ResolveResult{
		URL:     directURL,
		Headers: map[string]string{"User-Agent": gofileUserAgent},
	}, nil
}
