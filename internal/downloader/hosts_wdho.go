package downloader

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mili/moxie/internal/log"
)

// --- WDHO ---
// WDHO is a two-step HTTP flow once the request looks like a normal referral:
//
//  1. GET https://wdho.ru/<id> with a Referer header. Without a Referer the
//     AntiBot.Cloud colour-button interstitial is served; with one, the real
//     file page (with its countdown) is returned.
//  2. The page embeds the signed download URL as base64 in
//     `e='aHR0cHM6...'`. The token is single-use and only becomes valid after
//     the page's 4–8 second countdown, so the resolver waits, then hands the
//     URL (plus the page's cookies and Referer) to the normal downloader.
//
// The colour-button challenge only appears for direct/no-referer visits; when
// it does, the browser resolver can wait for a human to clear it.
var (
	wdhoTokenPattern = regexp.MustCompile(`e='([A-Za-z0-9+/=]{20,})'`)
	// wdhoTokenDelay is a variable so tests can shorten the countdown wait.
	wdhoTokenDelay = 8 * time.Second
)

const wdhoUserAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0"

func (r *HostResolver) resolveWDHO(rawURL string) (*ResolveResult, error) {
	res, err := r.resolveWDHOHTTP(rawURL)
	if err == nil {
		return res, nil
	}
	log.Debug("wdho HTTP flow failed", "error", err)
	if BrowserResolverEnabled() {
		return r.resolveWDHOBrowser(rawURL)
	}
	if strings.Contains(err.Error(), "colour") {
		return nil, fmt.Errorf("wdho requires an interactive colour-button check — retry with --browser and clear it in the opened window")
	}
	return nil, err
}

func (r *HostResolver) resolveWDHOHTTP(rawURL string) (*ResolveResult, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("wdho: create cookie jar: %w", err)
	}
	client := &http.Client{
		Timeout:       60 * time.Second,
		Jar:           jar,
		CheckRedirect: r.client.CheckRedirect,
	}
	if tr := r.client.Transport; tr != nil {
		client.Transport = tr
	}

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("wdho request: %w", err)
	}
	req.Header.Set("User-Agent", wdhoUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Referer", "https://wdho.ru/")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wdho: %w", err)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wdho: HTTP %d", resp.StatusCode)
	}
	if strings.Contains(string(body), "btn-color") || strings.Contains(string(body), `name="antibot"`) {
		return nil, fmt.Errorf("wdho requires an interactive colour-button check")
	}
	m := wdhoTokenPattern.FindStringSubmatch(string(body))
	if len(m) < 2 {
		return nil, fmt.Errorf("wdho: signed download token not found on page")
	}
	token, err := base64.StdEncoding.DecodeString(m[1])
	if err != nil {
		return nil, fmt.Errorf("wdho: decode download token: %w", err)
	}
	direct := string(token)
	u, err := url.Parse(direct)
	if err != nil || !strings.Contains(u.Hostname(), "wdho.ru") || !isValidDownloadURL(direct) {
		return nil, fmt.Errorf("wdho: invalid signed download URL")
	}

	// The server rejects the token until the on-page countdown has elapsed.
	// The page randomises 0–4s and adds 4s on load, so wait a little longer.
	time.Sleep(wdhoTokenDelay)

	cookieValues := map[string]string{}
	for _, c := range jar.Cookies(resp.Request.URL) {
		cookieValues[c.Name] = c.Value
	}
	for _, sc := range resp.Header.Values("Set-Cookie") {
		kv, _, _ := strings.Cut(sc, ";")
		name, value, ok := strings.Cut(kv, "=")
		if ok && name != "" && value != "" {
			cookieValues[name] = value
		}
	}
	names := make([]string, 0, len(cookieValues))
	for name := range cookieValues {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"="+cookieValues[name])
	}
	cookieHeader := strings.Join(parts, "; ")
	headers := map[string]string{
		"User-Agent": wdhoUserAgent,
		"Referer":    rawURL,
	}
	if cookieHeader != "" {
		headers["Cookie"] = cookieHeader
	}
	// The signed URL is single-use and tied to this cookie session, so it is
	// deliberately not cached.
	return &ResolveResult{URL: direct, Headers: headers}, nil
}
