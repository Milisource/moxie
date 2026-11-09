package downloader

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	fhttp "github.com/bogdanfinn/fhttp"
	"github.com/mili/moxie/internal/browser"
)

// Live probes for the 2026-08-09 research round:
//  1. TestTLSClientMatrix — buzzheavier /download A/B: stdlib vs
//     tls-client (Chrome_146, full HTTP/2 fingerprint). Continuation of
//     buzz_matrix_test.go, which proved the h1-only uTLS hack is an
//     "impossible browser" — tls-client has a complete HTTP/2 profile, so
//     this tests whether a real Chrome fingerprint changes the adaptive
//     challenge verdict.
//  2. TestTLSClientMaskedAB — F95Zone masked unwrap A/B: stdlib vs
//     tls-client with the same session cookies; counts ok vs captcha
//     (F95Zone's intermittent reCAPTCHA wall).
//  3. TestMaskedSitekeyExtract — GET the masked page and parse the
//     reCAPTCHA sitekey: validates the captcha-solver design step 1.
//
// Run: MOXIE_LIVE=1 go test ./internal/downloader/ -run 'TestTLSClient|TestMaskedSitekey' -v

const maskedProbeURL = "https://f95zone.to/masked/pixeldrain.com/6004/6265512/w0QsrGbXtWLejRyJd2K_9f3Qn94/1TuWkfbn_zSTdTuHu0eM5Q/mifP5JR4lIKvNQsat27MNlzAaXuDtdhleLdtNSqJ_EhFo05L8ZEyzb1ClXjc7Qwm"

const chromeUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36"

func newTLSClient() (tls_client.HttpClient, error) {
	options := []tls_client.HttpClientOption{
		tls_client.WithTimeoutSeconds(30),
		tls_client.WithClientProfile(profiles.Chrome_146),
		tls_client.WithNotFollowRedirects(),
		tls_client.WithCookieJar(tls_client.NewCookieJar()),
	}
	return tls_client.NewHttpClient(tls_client.NewNoopLogger(), options...)
}

func TestTLSClientMatrix(t *testing.T) {
	if os.Getenv("MOXIE_LIVE") == "" {
		t.Skip("live probe; set MOXIE_LIVE=1")
	}
	raw := strings.TrimRight(os.Getenv("BUZZ_LINK"), "/")
	if raw == "" {
		raw = "https://bzzhr.to/e2yt4zd66jq3"
	}
	cookie, _ := browser.GetCookiesForHost("bzzhr.to")
	t.Logf("cookie present: %v (%d chars)", cookie != "", len(cookie))

	hitStdlib := func() int {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, raw+"/download", nil)
		req.Header.Set("User-Agent", chromeUA)
		req.Header.Set("HX-Request", "true")
		req.Header.Set("priority", "u=1, i")
		req.Header.Set("HX-Current-URL", raw)
		req.Header.Set("Referer", raw)
		req.Header.Set("Accept", "*/*")
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		resp, err := (&http.Client{Timeout: 30 * time.Second, Transport: downloadTransport()}).Do(req)
		if err != nil {
			return 0
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}

	hitTLS := func() int {
		c, err := newTLSClient()
		if err != nil {
			t.Fatalf("tls-client: %v", err)
		}
		req, _ := fhttp.NewRequest(fhttp.MethodGet, raw+"/download", nil)
		req.Header = fhttp.Header{
			"User-Agent":    {chromeUA},
			"HX-Request":    {"true"},
			"priority":      {"u=1, i"},
			"HX-Current-URL": {raw},
			"Referer":       {raw},
			"Accept":        {"*/*"},
		}
		if cookie != "" {
			req.Header["Cookie"] = []string{cookie}
		}
		resp, err := c.Do(req)
		if err != nil {
			return 0
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}

	stats := map[string][2]int{}
	for round := 1; round <= 4; round++ {
		code := hitStdlib()
		if code == 200 || code == 204 {
			stats["stdlib"] = [2]int{stats["stdlib"][0] + 1, stats["stdlib"][1]}
		} else {
			stats["stdlib"] = [2]int{stats["stdlib"][0], stats["stdlib"][1] + 1}
		}
		t.Logf("round %d stdlib: status=%d", round, code)
		time.Sleep(700 * time.Millisecond)

		code = hitTLS()
		if code == 200 || code == 204 {
			stats["tls-client-chrome146"] = [2]int{stats["tls-client-chrome146"][0] + 1, stats["tls-client-chrome146"][1]}
		} else {
			stats["tls-client-chrome146"] = [2]int{stats["tls-client-chrome146"][0], stats["tls-client-chrome146"][1]}
		}
		t.Logf("round %d tls-client: status=%d", round, code)
		time.Sleep(700 * time.Millisecond)
	}
	for label, s := range stats {
		t.Logf("SUMMARY %s: pass=%d challenge=%d", label, s[0], s[1])
	}
}

// maskedUnwrapCaptchaTokenProbe POSTs the unwrap with a fake captcha token
// to confirm the `captcha` POST param name from masked.js: with the right
// name the server must answer something OTHER than the generic
// "Please complete the CAPTCHA" wall (e.g. an invalid-token error).
func TestMaskedCaptchaTokenParamProbe(t *testing.T) {
	if os.Getenv("MOXIE_LIVE") == "" {
		t.Skip("live probe; set MOXIE_LIVE=1")
	}
	r := NewHostResolver()
	body := "xhr=1&download=1&captcha=FAKE_TOKEN_PROBE_123"
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, maskedProbeURL, strings.NewReader(body))
	req.Header.Set("User-Agent", chromeUA)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	r.attachBrowserCookies(req)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	t.Logf("token-POST response (%d bytes, status %d): %s", len(raw), resp.StatusCode, raw)
	if strings.Contains(string(raw), "Please complete the CAPTCHA") {
		t.Log("→ generic wall: param NOT recognized (wrong name) or token demand")
	} else if strings.Contains(strings.ToLower(string(raw)), "invalid") || strings.Contains(strings.ToLower(string(raw)), "captcha error") {
		t.Log("→ server parsed the captcha param and rejected the token — PARAM NAME CONFIRMED")
	}
}

// maskedUnwrapAB performs one unwrap POST with the given persona and
// returns the JSON result_status (ok/captcha/error).
func maskedUnwrapAB(t *testing.T, useTLSClient bool) string {
	cookie, _ := browser.GetCookiesForHost("f95zone.to")
	body := "xhr=1&download=1"
	if useTLSClient {
		c, err := newTLSClient()
		if err != nil {
			t.Fatalf("tls-client: %v", err)
		}
		req, _ := fhttp.NewRequest(fhttp.MethodPost, maskedProbeURL, strings.NewReader(body))
		req.Header = fhttp.Header{
			"User-Agent":      {chromeUA},
			"X-Requested-With": {"XMLHttpRequest"},
			"Content-Type":    {"application/x-www-form-urlencoded"},
			"Accept":          {"application/json, text/javascript, */*; q=0.01"},
			"Referer":         {maskedProbeURL},
		}
		if cookie != "" {
			req.Header["Cookie"] = []string{cookie}
		}
		resp, err := c.Do(req)
		if err != nil {
			return "err:" + err.Error()
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return resultStatus(raw)
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, maskedProbeURL, strings.NewReader(body))
	req.Header.Set("User-Agent", chromeUA)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	req.Header.Set("Referer", maskedProbeURL)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second, Transport: downloadTransport()}).Do(req)
	if err != nil {
		return "err:" + err.Error()
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resultStatus(raw)
}

func resultStatus(raw []byte) string {
	var m struct {
		Status string `json:"status"`
		Msg    string `json:"msg"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Sprintf("nonjson(%d bytes): %.120s", len(raw), raw)
	}
	return m.Status + ":" + m.Msg
}

func TestTLSClientMaskedAB(t *testing.T) {
	if os.Getenv("MOXIE_LIVE") == "" {
		t.Skip("live probe; set MOXIE_LIVE=1")
	}
	stats := map[string][2]int{} // label → [ok, not-ok]
	for round := 1; round <= 6; round++ {
		res := maskedUnwrapAB(t, false)
		if strings.HasPrefix(res, "ok:") {
			stats["stdlib"] = [2]int{stats["stdlib"][0] + 1, stats["stdlib"][1]}
		} else {
			stats["stdlib"] = [2]int{stats["stdlib"][0], stats["stdlib"][1] + 1}
		}
		t.Logf("round %d stdlib: %s", round, res)
		time.Sleep(900 * time.Millisecond)

		res = maskedUnwrapAB(t, true)
		if strings.HasPrefix(res, "ok:") {
			stats["tls-client"] = [2]int{stats["tls-client"][0] + 1, stats["tls-client"][1]}
		} else {
			stats["tls-client"] = [2]int{stats["tls-client"][0], stats["tls-client"][1] + 1}
		}
		t.Logf("round %d tls-client: %s", round, res)
		time.Sleep(900 * time.Millisecond)
	}
	for label, s := range stats {
		t.Logf("SUMMARY %s: ok=%d blocked=%d", label, s[0], s[1])
	}
}

func TestMaskedSitekeyExtract(t *testing.T) {
	if os.Getenv("MOXIE_LIVE") == "" {
		t.Skip("live probe; set MOXIE_LIVE=1")
	}
	r := NewHostResolver()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, maskedProbeURL, nil)
	req.Header.Set("User-Agent", chromeUA)
	r.attachBrowserCookies(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("masked page GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	page := string(body)
	t.Logf("masked page: %d bytes, status %d", len(page), resp.StatusCode)

	sitekey := extractSitekey(page)
	if sitekey == "" {
		t.Log("sitekey NOT FOUND in HTML — checking /assets/js/masked.js (the widget is rendered at runtime)")
		jsReq, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://f95zone.to/assets/js/masked.js", nil)
		jsReq.Header.Set("User-Agent", chromeUA)
		jsReq.Header.Set("Referer", maskedProbeURL)
		r.attachBrowserCookies(jsReq)
		jsResp, err := http.DefaultClient.Do(jsReq)
		if err != nil {
			t.Logf("masked.js fetch failed: %v", err)
			return
		}
		defer jsResp.Body.Close()
		jsBody, _ := io.ReadAll(io.LimitReader(jsResp.Body, 64<<10))
		js := string(jsBody)
		t.Logf("masked.js: %d bytes, status %d", len(js), jsResp.StatusCode)
		sitekey = extractSitekey(js)
		t.Logf("SITEKEY from masked.js: %q", sitekey)
		os.WriteFile("/tmp/opencode/masked.js", jsBody, 0o644)
		t.Log("masked.js dumped to /tmp/opencode/masked.js")
		return
	}
	t.Logf("SITEKEY: %s", sitekey)
}

// extractSitekey finds the reCAPTCHA sitekey in a masked page: either the
// data-sitekey attribute or the first argument of grecaptcha.render(...).
func extractSitekey(page string) string {
	const attr = `data-sitekey="`
	if i := strings.Index(page, attr); i >= 0 {
		rest := page[i+len(attr):]
		if j := strings.Index(rest, `"`); j > 0 {
			return rest[:j]
		}
	}
	marker := "grecaptcha.render("
	if i := strings.Index(page, marker); i >= 0 {
		rest := page[i+len(marker):]
		for _, q := range []string{`"`, "'"} {
			if j := strings.Index(rest, q); j >= 0 {
				rest = rest[j+1:]
				if k := strings.Index(rest, q); k > 0 {
					return rest[:k]
				}
			}
		}
	}
	return ""
}
