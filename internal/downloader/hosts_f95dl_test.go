package downloader

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// Fixtures trimmed from the live pages captured 2026-11-01 (tokens, user ID
// and post replaced). Structure, classes and attribute order are the site's.

const f95dlCheckPage = `<!DOCTYPE html>
<html id="XF" lang="en-US" data-template="f95dl_check" data-logged-in="true">
<head><title>Download: Example Game | F95zone</title></head>
<body data-template="f95dl_check">
<div class="f95dl-shell"><main class="f95dl-shell-main">
<div class="f95dl-page f95dl-page--side" data-xf-init="f95dl-page" data-auto="0" data-code="%s" data-generating="Generating your link…">
<div class="f95dl-ad f95dl-ad--top"><form action="/ad/click" method="post"><input type="hidden" name="ad" value="1" /></form></div>
<form action="/dl/111/?u=1&amp;k=abc&amp;r=%d&amp;t=tok" method="post" class="block f95dl-box">
	<input type="hidden" name="_xfToken" value="1793555327,deadbeef" />
	<input type="hidden" name="had_check" value="0" />
	<input type="hidden" name="w" value="" class="js-f95dlCode" />
	<div class="f95dl-link"><span class="f95dl-link-name">Win/Linux · PIXELDRAIN</span><span class="f95dl-link-host">pixeldrain.com</span></div>
	%s
	<button type="submit" class="button button--primary f95dl-continue js-f95dlContinue"><span class="button-text">Continue to pixeldrain.com</span></button>
</form>
</div></main></div></body></html>`

const f95dlGoPage = `<!DOCTYPE html>
<html id="XF" data-template="f95dl_go"><head><title>Just a quick check | F95zone</title></head><body>
<div class="p-body-pageContent">
	<div class="blockMessage f95dl-going" data-xf-init="f95dl-go" data-url="%s" role="status">
		<div class="f95dl-going-title"><span class="f95dl-spinner" aria-hidden="true"></span>Opening pixeldrain.com…</div>
	</div>
</div></body></html>`

// f95dlExpiredPage is the message page served for an old link. The layout
// carries a JS template blockMessage before the real one.
const f95dlExpiredPage = `<!DOCTYPE html>
<html id="XF" data-template="message_page"><head><title>F95zone</title></head><body>
<script type="text/template"><div class="blockMessage">' + supportUsMessage + '</div></script>
<div class="p-body-pageContent">
	<div class="blockMessage">This download link has expired: they work for a few hours after the page was opened. Please <a href="/posts/111/">open it again from the post</a>.</div>
</div></body></html>`

const cfChallengePage = `<!DOCTYPE html><html lang="en-US"><head><title>Just a moment...</title></head><body>Performing security verification</body></html>`

// encodeF95DLCode is the server side of decodeF95DLCode.
func encodeF95DLCode(code string) string {
	r := []rune(code)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return "c" + base64.StdEncoding.EncodeToString([]byte(string(r)))
}

// fakeF95 serves the /dl/ check flow and enforces the site's rules: the POST
// must carry every served field, the decoded code, the session cookie, and
// arrive at least minDelay after the page was served.
type fakeF95 struct {
	t        *testing.T
	code     string
	dest     string
	minDelay time.Duration
	extra    string // markup injected into the form (e.g. a captcha widget)

	mu       sync.Mutex
	servedAt time.Time
	gets     atomic.Int32
	posts    atomic.Int32
	// challengeUntil makes requests answer with a Cloudflare challenge until
	// the request carries this cookie value.
	challengeUnless string
	// getStatus overrides the GET response.
	getBody string
}

func (f *fakeF95) handler(w http.ResponseWriter, r *http.Request) {
	if f.challengeUnless != "" && !strings.Contains(r.Header.Get("Cookie"), f.challengeUnless) {
		w.Header().Set("cf-mitigated", "challenge")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, cfChallengePage)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/dl/") {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		f.gets.Add(1)
		if f.getBody != "" {
			fmt.Fprint(w, f.getBody)
			return
		}
		f.mu.Lock()
		f.servedAt = time.Now()
		f.mu.Unlock()
		fmt.Fprintf(w, f95dlCheckPage, encodeF95DLCode(f.code), time.Now().Unix(), f.extra)
	case http.MethodPost:
		f.posts.Add(1)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		early := time.Since(f.servedAt) < f.minDelay
		f.mu.Unlock()
		ok := !early &&
			r.PostForm.Get("w") == f.code &&
			r.PostForm.Get("_xfToken") == "1793555327,deadbeef" &&
			r.PostForm.Has("had_check") &&
			!r.PostForm.Has("ad") &&
			strings.Contains(r.Header.Get("Cookie"), "xf_user=") &&
			r.Header.Get("User-Agent") != ""
		if !ok {
			// The site answers a failed check with the check page again.
			fmt.Fprintf(w, f95dlCheckPage, encodeF95DLCode(f.code), time.Now().Unix(), "")
			return
		}
		fmt.Fprintf(w, f95dlGoPage, f.dest)
	}
}

// newF95Resolver returns a resolver whose f95zone.to traffic goes to fake.
func newF95Resolver(t *testing.T, fake *fakeF95) *HostResolver {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(srv.Close)
	r := NewHostResolver()
	r.client.Transport = rewriteToServer("f95zone.to", srv.URL)
	r.SetF95Cookie("xf_user=1%2Cabc; xf_session=s; cf_clearance=old")
	r.SetDLSolver(nil)
	return r
}

// shortCodeDelay shrinks the site's 2.1 s wait for the duration of a test.
func shortCodeDelay(t *testing.T, d time.Duration) {
	t.Helper()
	orig := f95dlCodeDelay
	f95dlCodeDelay = d
	t.Cleanup(func() { f95dlCodeDelay = orig })
}

const testDL = "https://f95zone.to/dl/111/?u=1&k=abc&r=1793555292&t=tok"

// Not parallel: these tests adjust package-level timing/hook state.

func TestF95DL_NativeSolveReachesHost(t *testing.T) {
	shortCodeDelay(t, 120*time.Millisecond)
	fake := &fakeF95{t: t, code: "1793555483.f7FlYXEaJENrmTAd", dest: "https://pixeldrain.com/u/3vkeyGis", minDelay: 100 * time.Millisecond}
	r := newF95Resolver(t, fake)

	got, err := r.resolveF95DownloadLink(context.Background(), testDL)
	if err != nil {
		t.Fatalf("resolveF95DownloadLink: %v", err)
	}
	if got != "https://pixeldrain.com/u/3vkeyGis" {
		t.Errorf("destination = %q, want the pixeldrain URL", got)
	}
	if fake.gets.Load() != 1 || fake.posts.Load() != 1 {
		t.Errorf("requests: %d GET / %d POST, want 1 / 1", fake.gets.Load(), fake.posts.Load())
	}
}

// The full Resolve path continues into the host resolver with the
// destination (pixeldrain's /u/ page becomes its API file URL).
func TestF95DL_ResolveContinuesWithHostResolver(t *testing.T) {
	shortCodeDelay(t, 20*time.Millisecond)
	fake := &fakeF95{t: t, code: "x.y", dest: "https://pixeldrain.com/u/3vkeyGis"}
	r := newF95Resolver(t, fake)

	res, err := r.Resolve(testDL, "pixeldrain")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !strings.Contains(res.URL, "pixeldrain.com/api/file/3vkeyGis") {
		t.Errorf("Resolve URL = %q, want the pixeldrain API file URL", res.URL)
	}
}

// The POST must wait out the script-check delay measured from the page load;
// a server enforcing it rejects an early POST by re-serving the check page.
func TestF95DL_WaitsForCodeDelay(t *testing.T) {
	shortCodeDelay(t, 300*time.Millisecond)
	fake := &fakeF95{t: t, code: "c0de", dest: "https://gofile.io/d/abc", minDelay: 250 * time.Millisecond}
	r := newF95Resolver(t, fake)

	start := time.Now()
	if _, err := r.resolveF95DownloadLink(context.Background(), testDL); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if el := time.Since(start); el < 250*time.Millisecond {
		t.Errorf("resolved after %v, before the code delay", el)
	}
}

func TestF95DL_CheckPageAgainNeedsBrowser(t *testing.T) {
	shortCodeDelay(t, 10*time.Millisecond)
	fake := &fakeF95{t: t, code: "c0de", dest: "https://gofile.io/d/abc", minDelay: time.Hour}
	r := newF95Resolver(t, fake)

	_, err := r.resolveF95DownloadLink(context.Background(), testDL)
	if !errors.Is(err, errF95NeedsBrowser) {
		t.Fatalf("err = %v, want errF95NeedsBrowser", err)
	}
}

func TestF95DL_CaptchaHandsOffToBrowserSolver(t *testing.T) {
	shortCodeDelay(t, 10*time.Millisecond)
	fake := &fakeF95{t: t, code: "c0de", dest: "https://gofile.io/d/abc",
		extra: `<div class="h-captcha" data-sitekey="x"></div>`}
	r := newF95Resolver(t, fake)
	var solved string
	r.SetDLSolver(func(ctx context.Context, dlURL string) (string, error) {
		solved = dlURL
		return "http://127.0.0.1:1/file.zip", nil
	})

	res, err := r.Resolve(testDL, "pixeldrain")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if solved != testDL || res.URL != "http://127.0.0.1:1/file.zip" {
		t.Errorf("browser solver got %q, result %q", solved, res.URL)
	}
	if fake.posts.Load() != 0 {
		t.Errorf("POSTed %d times to a captcha page, want 0", fake.posts.Load())
	}
}

// Without the browser hook, a captcha page names the setting that enables
// browser-assisted downloads.
func TestF95DL_CaptchaWithoutSolverHints(t *testing.T) {
	shortCodeDelay(t, 10*time.Millisecond)
	fake := &fakeF95{t: t, code: "c0de", extra: `<div class="h-captcha"></div>`}
	r := newF95Resolver(t, fake)

	_, err := r.Resolve(testDL, "pixeldrain")
	if err == nil || !strings.Contains(err.Error(), "browser_fallback") {
		t.Fatalf("err = %v, want the browser-assisted-downloads hint", err)
	}
}

func TestF95DL_ExpiredLinkIsStaleError(t *testing.T) {
	fake := &fakeF95{t: t, getBody: f95dlExpiredPage}
	r := newF95Resolver(t, fake)

	_, err := r.Resolve(testDL, "pixeldrain")
	if !IsF95StaleLinkError(err) {
		t.Fatalf("err = %v, want a stale-link error", err)
	}
	if IsF95SessionError(err) {
		t.Error("an expired link must not count as a session error")
	}
	if !strings.Contains(err.Error(), "work for a few hours") || strings.Contains(err.Error(), "supportUsMessage") {
		t.Errorf("err = %v, want the page's own message", err)
	}
}

// A Cloudflare challenge triggers the clearance refresher exactly once; the
// retry carries the refreshed cookie.
func TestF95DL_ChallengeRefreshesClearanceOnce(t *testing.T) {
	shortCodeDelay(t, 10*time.Millisecond)
	fake := &fakeF95{t: t, code: "c0de", dest: "https://gofile.io/d/abc", challengeUnless: "cf_clearance=fresh"}
	r := newF95Resolver(t, fake)

	var calls atomic.Int32
	SetF95ClearanceRefresher(func(ctx context.Context) (string, error) {
		calls.Add(1)
		return "xf_user=1%2Cabc; xf_session=s; cf_clearance=fresh", nil
	})
	t.Cleanup(func() { SetF95ClearanceRefresher(nil) })

	got, err := r.resolveF95DownloadLink(context.Background(), testDL)
	if err != nil {
		t.Fatalf("resolve after refresh: %v", err)
	}
	if got != "https://gofile.io/d/abc" || calls.Load() != 1 {
		t.Errorf("dest %q after %d refreshes, want gofile after 1", got, calls.Load())
	}
}

// A challenge the refresh cannot clear is a session error, so mirror loops
// stop instead of walking every link.
func TestF95DL_PersistentChallengeIsSessionError(t *testing.T) {
	fake := &fakeF95{t: t, challengeUnless: "never-sent"}
	r := newF95Resolver(t, fake)
	SetF95ClearanceRefresher(func(ctx context.Context) (string, error) {
		return "xf_user=1; cf_clearance=still-bad", nil
	})
	t.Cleanup(func() { SetF95ClearanceRefresher(nil) })

	_, err := r.Resolve(testDL, "pixeldrain")
	if !IsF95SessionError(err) {
		t.Fatalf("err = %v, want a session error", err)
	}
}

func TestF95DL_UserAgentProvider(t *testing.T) {
	const ua = "Mozilla/5.0 (X11; Linux x86_64; rv:159.0) Gecko/20100101 Firefox/159.0"
	SetF95UserAgentProvider(func() string { return ua })
	t.Cleanup(func() { SetF95UserAgentProvider(nil) })
	if got := f95UserAgent(); got != ua {
		t.Errorf("f95UserAgent = %q, want the provider's", got)
	}
	SetF95UserAgentProvider(func() string { return "" })
	if got := f95UserAgent(); got != defaultF95UserAgent {
		t.Errorf("empty provider: f95UserAgent = %q, want the default", got)
	}
}

func TestDecodeF95DLCode_LiveSample(t *testing.T) {
	t.Parallel()
	// data-code from a live page (2026-11-01) and what the site's own
	// decode() produced for it.
	got, err := decodeF95DLCode("cZEFUbXJORUphRVhZbEY3Zi4zODQ1NTUzOTcx")
	if err != nil {
		t.Fatal(err)
	}
	if got != "1793555483.f7FlYXEaJENrmTAd" {
		t.Errorf("decode = %q", got)
	}
}

func TestF95GoDestination_RejectsF95Pages(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		url  string
		want bool
	}{
		{"https://pixeldrain.com/u/x", true},
		{"https://f95zone.to/masked/pixeldrain.com/1/2", true},
		{"https://f95zone.to/threads/1/", false},
		{"javascript:alert(1)", false},
	} {
		doc := mustDoc(t, fmt.Sprintf(f95dlGoPage, tc.url))
		if _, ok := f95GoDestination(doc); ok != tc.want {
			t.Errorf("f95GoDestination(%q) ok = %v, want %v", tc.url, ok, tc.want)
		}
	}
}

func TestF95LinkStale(t *testing.T) {
	t.Parallel()
	now := time.Unix(1793556000, 0)
	for _, tc := range []struct {
		url  string
		want bool
	}{
		{"https://f95zone.to/dl/1/?u=1&k=a&r=1793555292&t=x", false}, // 12 min old
		{"https://f95zone.to/dl/1/?u=1&k=a&r=1792873546&t=x", true},  // 8 days old
		{"https://f95zone.to/dl/1/?u=1&k=a&t=x", true},               // no timestamp
		{"https://f95zone.to/masked/pixeldrain.com/1/2", false},
		{"https://pixeldrain.com/u/x", false},
	} {
		if got := F95LinkStale(tc.url, now); got != tc.want {
			t.Errorf("F95LinkStale(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

func mustDoc(t *testing.T, html string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}
