package downloader

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/mili/moxie/internal/log"
)

// F95Zone /dl/ download links (2026-11 form, live-verified 2026-11-01).
//
// Thread posts link every mirror through f95zone.to/dl/<postID>/?u&k&r&t. The
// page served there ("f95dl_check" template) is a plain XenForo form:
//
//	<div class="f95dl-page" data-xf-init="f95dl-page" data-auto="0"
//	     data-code="c<base64 of the code reversed>">
//	<form action="/dl/…" method="post" class="block f95dl-box">
//	  <input name="_xfToken"> <input name="had_check" value="0">
//	  <input name="w" class="js-f95dlCode">            ← filled by JS
//	  <button class="js-f95dlContinue">Continue to <host></button>
//
// The site's script (js/tb72ar/s0rm0/ng6597.js) decodes data-code into the w
// input 2.1 s after the page opens — the server counts whole seconds and
// wants more than one — and submits. The POST answers with the
// "f95dl_go" template whose data-url is the host URL. No captcha is
// involved, so the whole flow runs over plain HTTP as long as the request
// carries a cf_clearance/User-Agent pair Cloudflare accepts.
//
// had_check suggests the site can still demand an extra (captcha) check for
// risky sessions, as it did in 2026-10; a check page that comes back, or
// captcha markers, hand the link to the browser solver.

// f95dlCodeDelay is how long after loading the page the code may be sent.
// The site's own script waits 2100 ms.
var f95dlCodeDelay = 2200 * time.Millisecond

// Errors the native /dl/ solver classifies its failures into. Session-level
// errors (challenge, login) affect every mirror alike; link-level ones only
// this link.
var (
	// ErrF95Challenge: Cloudflare challenged the request — the clearance
	// cookie is stale or does not match the User-Agent. Session-level.
	ErrF95Challenge = errors.New("F95Zone Cloudflare challenge")
	// ErrF95LoginRequired: the session is logged out. Session-level.
	ErrF95LoginRequired = errors.New("F95Zone session expired — log in to f95zone.to in your browser")
	// ErrF95CookiesRequired: XenForo refused the request because the session
	// cookies it issued for the check page were not sent back. Session-level.
	ErrF95CookiesRequired = errors.New("F95Zone rejected the request (cookies are required) — reload f95zone.to in your browser and retry")
	// ErrF95LinkGone: the link expired or no longer exists in the post — a
	// fresh scrape of the thread yields a working one. Link-level.
	ErrF95LinkGone = errors.New("F95Zone download link expired or removed")
	// errF95NeedsBrowser: the page wants a check plain HTTP cannot pass.
	errF95NeedsBrowser = errors.New("F95Zone download link needs the browser check")
)

// f95LinkMaxAge is how long a scraped /dl/ link is trusted. The site says
// links "work for a few hours after the page was opened" (expired-link page,
// 2026-11-01); r= is that page-open time.
const f95LinkMaxAge = 90 * time.Minute

// F95LinkStale reports whether rawURL is an F95Zone /dl/ link scraped too
// long ago to still work (or whose timestamp is unreadable). Callers re-scrape
// the thread for fresh links before downloading. Other URLs are never stale.
func F95LinkStale(rawURL string, now time.Time) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || !isF95DownloadLinkURL(u) {
		return false
	}
	r, err := strconv.ParseInt(u.Query().Get("r"), 10, 64)
	if err != nil || r <= 0 {
		return true
	}
	return now.Sub(time.Unix(r, 0)) > f95LinkMaxAge
}

// IsF95StaleLinkError reports whether err means the F95Zone link expired.
// Every link from the same scrape expires together, so callers re-scrape the
// thread once rather than trying the other mirrors.
func IsF95StaleLinkError(err error) bool {
	return errors.Is(err, ErrF95LinkGone)
}

// IsF95SessionError reports whether err is a session-level F95Zone failure:
// trying the next mirror would hit the same wall, so callers should stop and
// surface it (or refresh the session) instead of walking every link.
func IsF95SessionError(err error) bool {
	return errors.Is(err, ErrF95Challenge) || errors.Is(err, ErrF95LoginRequired) ||
		errors.Is(err, ErrF95CookiesRequired)
}

// F95 session hooks, wired by internal/f95session at startup and left nil
// in tests.
var f95Hooks struct {
	mu        sync.RWMutex
	userAgent func() string
	refresh   func(ctx context.Context) (cookie string, err error)
}

// SetF95UserAgentProvider installs the source of the User-Agent sent with
// F95Zone cookies. It must match the cf_clearance in use; "" falls back to
// the default.
func SetF95UserAgentProvider(fn func() string) {
	f95Hooks.mu.Lock()
	f95Hooks.userAgent = fn
	f95Hooks.mu.Unlock()
}

// SetF95ClearanceRefresher installs the hook called once per resolver when
// an F95Zone request meets a Cloudflare challenge. It returns a fresh Cookie
// header (and updates the UA provider) so the request can be retried.
func SetF95ClearanceRefresher(fn func(ctx context.Context) (string, error)) {
	f95Hooks.mu.Lock()
	f95Hooks.refresh = fn
	f95Hooks.mu.Unlock()
}

// defaultF95UserAgent is used when no provider is wired or it cannot tell.
const defaultF95UserAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0"

// f95UserAgent returns the UA to send alongside F95Zone cookies.
func f95UserAgent() string {
	f95Hooks.mu.RLock()
	fn := f95Hooks.userAgent
	f95Hooks.mu.RUnlock()
	if fn != nil {
		if ua := fn(); ua != "" {
			return ua
		}
	}
	return defaultF95UserAgent
}

func f95ClearanceRefresher() func(ctx context.Context) (string, error) {
	f95Hooks.mu.RLock()
	defer f95Hooks.mu.RUnlock()
	return f95Hooks.refresh
}

// resolveF95DownloadLink turns a /dl/ link into its destination over plain
// HTTP, refreshing the Cloudflare clearance once if challenged.
func (r *HostResolver) resolveF95DownloadLink(ctx context.Context, dlURL string) (string, error) {
	dest, err := r.solveF95DownloadLink(ctx, dlURL)
	if !errors.Is(err, ErrF95Challenge) {
		return dest, err
	}
	refresh := f95ClearanceRefresher()
	if refresh == nil || r.clearanceRefreshed {
		return "", err
	}
	r.clearanceRefreshed = true
	cookie, rerr := refresh(ctx)
	if rerr != nil || cookie == "" {
		log.Warn("F95Zone download link: clearance refresh failed", "error", rerr)
		return "", err
	}
	r.f95Cookie = cookie
	log.Info("F95Zone download link: clearance refreshed; retrying")
	return r.solveF95DownloadLink(ctx, dlURL)
}

// solveF95DownloadLink performs one GET → wait → POST round.
func (r *HostResolver) solveF95DownloadLink(ctx context.Context, dlURL string) (string, error) {
	base, err := url.Parse(dlURL)
	if err != nil || !isF95DownloadLinkURL(base) {
		return "", fmt.Errorf("not an F95Zone download link: %s", dlURL)
	}
	client := r.f95Client()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dlURL, nil)
	if err != nil {
		return "", err
	}
	r.setF95Headers(req, "")
	loaded := time.Now()
	page, err := r.doF95(client, req)
	if err != nil {
		return "", err
	}
	if page.dest != "" {
		return page.dest, nil
	}
	form, err := parseF95DLCheck(page.doc, base)
	if err != nil {
		return "", err
	}

	if wait := f95dlCodeDelay - time.Since(loaded); wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	post, err := http.NewRequestWithContext(ctx, http.MethodPost, form.action, strings.NewReader(form.values.Encode()))
	if err != nil {
		return "", err
	}
	r.setF95Headers(post, dlURL)
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("Origin", "https://"+base.Host)
	res, err := r.doF95(client, post)
	if err != nil {
		return "", err
	}
	if res.dest != "" {
		return res.dest, nil
	}
	if res.doc != nil && res.doc.Find(".f95dl-page").Length() > 0 {
		// The check page came back: the server wants more than the
		// script check (e.g. a captcha for this session).
		return "", errF95NeedsBrowser
	}
	return "", fmt.Errorf("F95Zone download link: no destination in the response (%s)", res.snippet)
}

// f95Client is an HTTP client for F95Zone pages that stops at the first
// redirect leaving F95Zone (the destination) instead of following it with
// the session cookie.
//
// The client carries a cookie jar because the check page answers with
// `Set-Cookie: xf_csrf=…` and its POST is rejected (HTTP 400, "Cookies are
// required to use this site") unless that cookie comes back. A static
// Cookie header alone is not enough: XenForo binds the form token to the
// cookie it just issued.
func (r *HostResolver) f95Client() *http.Client {
	var transport http.RoundTripper
	if r.client != nil {
		transport = r.client.Transport
	}
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Transport: transport,
		Jar:       jar,
		Timeout:   30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			if !isF95Hostname(req.URL.Hostname()) {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

func (r *HostResolver) setF95Headers(req *http.Request, referer string) {
	req.Header.Set("User-Agent", f95UserAgent())
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	if referer != "" {
		req.Header.Set("Referer", referer)
	} else {
		req.Header.Set("Referer", "https://f95zone.to/")
	}
	if isF95Hostname(req.URL.Hostname()) {
		if r.f95Cookie != "" {
			req.Header.Set("Cookie", r.f95Cookie)
		} else {
			r.attachBrowserCookies(req)
		}
	}
}

// f95Page is a classified F95Zone response: either a destination (a redirect
// off-site or an f95dl_go page) or a parsed document to inspect further.
type f95Page struct {
	dest    string
	doc     *goquery.Document
	snippet string
}

// doF95 sends req and classifies the response, mapping challenge, login and
// dead-link pages to their typed errors.
func (r *HostResolver) doF95(client *http.Client, req *http.Request) (*f95Page, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("F95Zone download link: %w", err)
	}
	defer resp.Body.Close()

	if loc := resp.Header.Get("Location"); resp.StatusCode >= 300 && resp.StatusCode < 400 && loc != "" {
		target, err := resp.Request.URL.Parse(loc)
		if err != nil {
			return nil, fmt.Errorf("F95Zone download link: bad redirect %q", loc)
		}
		if isLoginRedirect(target.String()) {
			return nil, ErrF95LoginRequired
		}
		return &f95Page{dest: target.String()}, nil
	}
	if isLoginRedirect(resp.Request.URL.String()) {
		return nil, ErrF95LoginRequired
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("F95Zone download link: read: %w", err)
	}
	text := string(body)
	if resp.Header.Get("cf-mitigated") == "challenge" || isCloudflareChallengeBody(text) {
		return nil, fmt.Errorf("%w (HTTP %d) on %s", ErrF95Challenge, resp.StatusCode, resp.Request.URL.Redacted())
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(text))
	if err != nil {
		return nil, fmt.Errorf("F95Zone download link: parse: %w", err)
	}
	page := &f95Page{doc: doc, snippet: pageSnippet(doc)}

	if dest, ok := f95GoDestination(doc); ok {
		page.dest = dest
		return page, nil
	}
	if resp.StatusCode == http.StatusOK && doc.Find(".f95dl-page").Length() > 0 {
		return page, nil
	}
	if doc.Find(`form[action*="login"] input[name="login"]`).Length() > 0 || strings.Contains(strings.ToLower(page.snippet), "you must be logged-in") {
		return nil, ErrF95LoginRequired
	}
	if strings.Contains(strings.ToLower(page.snippet), "cookies are required") {
		return nil, fmt.Errorf("%w (HTTP %d)", ErrF95CookiesRequired, resp.StatusCode)
	}
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone || isDeadLinkMessage(page.snippet) {
		return nil, fmt.Errorf("%w: %s", ErrF95LinkGone, page.snippet)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("F95Zone download link: HTTP %d: %s", resp.StatusCode, page.snippet)
	}
	return page, nil
}

// f95GoDestination reads the host URL from the "taking you to the host" page
// (data-xf-init="f95dl-go" data-url="https://host/…").
func f95GoDestination(doc *goquery.Document) (string, bool) {
	sel := doc.Find(`[data-xf-init~="f95dl-go"]`).First()
	if sel.Length() == 0 {
		return "", false
	}
	raw, _ := sel.Attr("data-url")
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", false
	}
	if isF95Hostname(u.Hostname()) && !isMaskedF95URL(u) {
		// Never treat another F95Zone page as the file destination.
		return "", false
	}
	return u.String(), true
}

// f95dlForm is the check form ready to submit.
type f95dlForm struct {
	action string
	values url.Values
}

// parseF95DLCheck extracts the Continue form from the check page and fills
// the script-check code. Captcha widgets mean plain HTTP cannot pass.
func parseF95DLCheck(doc *goquery.Document, base *url.URL) (*f95dlForm, error) {
	pageSel := doc.Find(`[data-xf-init~="f95dl-page"]`).First()
	if pageSel.Length() == 0 {
		pageSel = doc.Find(".f95dl-page").First()
	}
	if pageSel.Length() == 0 {
		return nil, fmt.Errorf("F95Zone download link: unrecognised page (%s)", pageSnippet(doc))
	}
	if doc.Find(`.h-captcha, .g-recaptcha, .cf-turnstile, iframe[src*="hcaptcha"], iframe[src*="recaptcha"], script[src*="hcaptcha.com"]`).Length() > 0 {
		return nil, errF95NeedsBrowser
	}
	// The Continue button's own form: an ad above it may carry a form of
	// its own (the site's script makes the same distinction).
	form := pageSel.Find(".js-f95dlContinue").First().Closest("form")
	if form.Length() == 0 {
		form = pageSel.Find("form.f95dl-box").First()
	}
	if form.Length() == 0 {
		return nil, fmt.Errorf("F95Zone download link: no Continue form on the page")
	}
	actionRaw, _ := form.Attr("action")
	action, err := base.Parse(strings.TrimSpace(actionRaw))
	if err != nil || !isF95Hostname(action.Hostname()) {
		return nil, fmt.Errorf("F95Zone download link: unexpected form action %q", actionRaw)
	}
	method, _ := form.Attr("method")
	if method != "" && !strings.EqualFold(method, "post") {
		return nil, fmt.Errorf("F95Zone download link: unexpected form method %q", method)
	}

	values := url.Values{}
	form.Find("input[name]").Each(func(_ int, in *goquery.Selection) {
		name, _ := in.Attr("name")
		typ, _ := in.Attr("type")
		if typ == "checkbox" || typ == "radio" {
			if _, checked := in.Attr("checked"); !checked {
				return
			}
		}
		v, _ := in.Attr("value")
		values.Set(name, v)
	})
	if code, ok := pageSel.Attr("data-code"); ok && strings.TrimSpace(code) != "" {
		decoded, err := decodeF95DLCode(code)
		if err != nil {
			return nil, err
		}
		name := "w"
		if n, ok := form.Find(".js-f95dlCode").First().Attr("name"); ok && n != "" {
			name = n
		}
		values.Set(name, decoded)
	}
	return &f95dlForm{action: action.String(), values: values}, nil
}

// decodeF95DLCode mirrors the site's decode(): strip the leading "c", base64
// decode, reverse.
func decodeF95DLCode(hidden string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(strings.TrimSpace(hidden), "c"))
	if err != nil {
		return "", fmt.Errorf("F95Zone download link: undecodable script-check code: %w", err)
	}
	r := []rune(string(raw))
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r), nil
}

// isCloudflareChallengeBody detects Cloudflare's interstitial.
func isCloudflareChallengeBody(body string) bool {
	return strings.Contains(body, "<title>Just a moment...</title>") ||
		strings.Contains(body, "cf-challenge-running") ||
		strings.Contains(body, "challenges.cloudflare.com/cdn-cgi/challenge-platform") && strings.Contains(body, "Performing security verification")
}

// isDeadLinkMessage recognises F95Zone's messages for a link that no longer
// works ("That link is no longer in the post", expired links).
func isDeadLinkMessage(s string) bool {
	l := strings.ToLower(s)
	for _, needle := range []string{"no longer in the post", "has expired", "link expired", "no longer valid", "open it again from the post", "requested page could not be found"} {
		if strings.Contains(l, needle) {
			return true
		}
	}
	return false
}

// pageSnippet is the page's main message text, trimmed, for errors. The
// page body's own message is preferred: XenForo also carries template and
// "JavaScript is disabled" blockMessages elsewhere in the layout.
func pageSnippet(doc *goquery.Document) string {
	sel := doc.Find(".p-body-pageContent .blockMessage").First()
	if sel.Length() == 0 {
		sel = doc.Find(".p-body-pageContent").First()
	}
	if sel.Length() == 0 {
		sel = doc.Find("title").First()
	}
	s := strings.Join(strings.Fields(sel.Text()), " ")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
