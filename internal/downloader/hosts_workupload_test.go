package downloader

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/mili/moxie/internal/browser"
)

// wuTestDigest returns the lowercase hex SHA-256 of s, mirroring the
// page-side solver's digest format for building puzzle fixtures.
func wuTestDigest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------------------
// URL parsing
// ---------------------------------------------------------------------------

func TestWorkuploadFileParts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		url    string
		wantID string
		wantK  string
		wantOK bool
	}{
		{"file", "https://workupload.com/file/ckfAZgjjgej", "ckfAZgjjgej", "file", true},
		{"start", "https://workupload.com/start/ckfAZgjjgej", "ckfAZgjjgej", "file", true},
		{"archive", "https://workupload.com/archive/ckfAZgjjgej", "ckfAZgjjgej", "archive", true},
		{"archive start", "https://workupload.com/archive/ckfAZgjjgej/start", "ckfAZgjjgej", "archive", true},
		{"query string", "https://workupload.com/file/ckfAZgjjgej?dl=1", "ckfAZgjjgej", "file", true},
		{"http scheme", "http://workupload.com/file/a1B2c3D4e5", "a1B2c3D4e5", "file", true},
		{"mixed id", "https://workupload.com/file/aB3xY9zQ1w", "aB3xY9zQ1w", "file", true},
		{"wrong path", "https://workupload.com/other/ckfAZgjjgej", "", "", false},
		{"foreign host", "https://example.com/file/ckfAZgjjgej", "", "", false},
		{"not a url", "workupload", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, kind, ok := workuploadFileParts(tt.url)
			if id != tt.wantID || kind != tt.wantK || ok != tt.wantOK {
				t.Errorf("workuploadFileParts(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tt.url, id, kind, ok, tt.wantID, tt.wantK, tt.wantOK)
			}
		})
	}
}

func TestWorkuploadPageURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		id   string
		kind string
		want string
	}{
		{"file passthrough", "https://workupload.com/file/abc123", "abc123", "file", "https://workupload.com/file/abc123"},
		{"start normalized", "https://workupload.com/start/abc123", "abc123", "file", "https://workupload.com/file/abc123"},
		{"archive start normalized", "https://workupload.com/archive/abc123/start", "abc123", "archive", "https://workupload.com/archive/abc123"},
		{"query stripped", "https://workupload.com/file/abc123?x=1#frag", "abc123", "file", "https://workupload.com/file/abc123"},
		{"http kept", "http://workupload.com/start/abc123", "abc123", "file", "http://workupload.com/file/abc123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := workuploadPageURL(tt.raw, tt.id, tt.kind)
			if got != tt.want {
				t.Errorf("workuploadPageURL(%q, %q, %q) = %q, want %q", tt.raw, tt.id, tt.kind, got, tt.want)
			}
		})
	}
}

func TestWorkuploadAPIURL(t *testing.T) {
	t.Parallel()
	got := workuploadAPIURL("https://workupload.com/file/abc123", "file", "abc123")
	want := "https://workupload.com/api/file/getDownloadServer/abc123"
	if got != want {
		t.Errorf("workuploadAPIURL(file) = %q, want %q", got, want)
	}
	got = workuploadAPIURL("https://workupload.com/archive/abc123", "archive", "abc123")
	want = "https://workupload.com/api/archive/getDownloadServer/abc123"
	if got != want {
		t.Errorf("workuploadAPIURL(archive) = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Puzzle parsing and solving (pure functions)
// ---------------------------------------------------------------------------

func TestParseWorkuploadPuzzle(t *testing.T) {
	t.Parallel()
	valid := `{"success":true,"data":{"puzzle":"1786262653.43496a78347d6a2d0","range":10000,"find":["2a1830ef9da8647b01e942f188f0773111575b141dfa842a86bd6899e5d78a16"]}}`
	puzzle, rng, find, err := parseWorkuploadPuzzle([]byte(valid))
	if err != nil {
		t.Fatalf("parseWorkuploadPuzzle(valid) error: %v", err)
	}
	if puzzle != "1786262653.43496a78347d6a2d0" || rng != 10000 || len(find) != 1 {
		t.Errorf("parseWorkuploadPuzzle(valid) = (%q, %d, %v)", puzzle, rng, find)
	}

	for name, body := range map[string]string{
		"not json":   `hello`,
		"empty":      ``,
		"success no": `{"success":false,"data":null}`,
		"no data":    `{"success":true,"data":{"puzzle":"","range":0,"find":[]}}`,
		"zero range": `{"success":true,"data":{"puzzle":"p","range":0,"find":["aa"]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, _, err := parseWorkuploadPuzzle([]byte(body)); err == nil {
				t.Errorf("parseWorkuploadPuzzle(%q) expected error, got nil", body)
			}
		})
	}
}

func TestSolveWorkuploadPuzzle(t *testing.T) {
	t.Parallel()
	puzzle := "fixture.puzzle.1"
	// Targets at indices 7 and 9876.
	find := []string{
		wuTestDigest(puzzle + "7"),
		wuTestDigest(puzzle + "9876"),
	}
	answer, err := solveWorkuploadPuzzle(puzzle, 20000, find)
	if err != nil {
		t.Fatalf("solveWorkuploadPuzzle error: %v", err)
	}
	if answer != "7 9876 " {
		t.Errorf("solveWorkuploadPuzzle = %q, want %q", answer, "7 9876 ")
	}

	// Uppercase hex targets must still match (the parser lowercases).
	upper := []string{strings.ToUpper(wuTestDigest(puzzle + "7"))}
	answer, err = solveWorkuploadPuzzle(puzzle, 20000, upper)
	if err != nil {
		t.Fatalf("solveWorkuploadPuzzle (uppercase find) error: %v", err)
	}
	if answer != "7 " {
		t.Errorf("solveWorkuploadPuzzle (uppercase find) = %q, want %q", answer, "7 ")
	}

	// No matching digest in range → error.
	if _, err := solveWorkuploadPuzzle("no-match.puzzle", 100, []string{wuTestDigest("nope")}); err == nil {
		t.Error("solveWorkuploadPuzzle with no matches expected error, got nil")
	}

	// Early exit: a target at the very start must not need the full range.
	early := []string{wuTestDigest(puzzle + "0")}
	answer, err = solveWorkuploadPuzzle(puzzle, 100000, early)
	if err != nil {
		t.Fatalf("solveWorkuploadPuzzle (early) error: %v", err)
	}
	if answer != "0 " {
		t.Errorf("solveWorkuploadPuzzle (early) = %q, want %q", answer, "0 ")
	}

	// Invalid inputs.
	for name, args := range map[string]struct {
		puzzle string
		rng    int
		find   []string
	}{
		"empty puzzle": {"", 100, []string{"aa"}},
		"zero range":   {"p", 0, []string{"aa"}},
		"empty find":   {"p", 100, nil},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := solveWorkuploadPuzzle(args.puzzle, args.rng, args.find); err == nil {
				t.Errorf("solveWorkuploadPuzzle(%q, %d, %v) expected error, got nil", args.puzzle, args.rng, args.find)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Cookie merging
// ---------------------------------------------------------------------------

func TestMergeCookieHeader(t *testing.T) {
	t.Parallel()
	jar := map[string]string{"captcha": "fresh", "token": "sess"}
	got := mergeCookieHeader("captcha=stale; other=keep", jar)
	for _, want := range []string{"captcha=fresh", "token=sess", "other=keep"} {
		if !strings.Contains(got, want) {
			t.Errorf("mergeCookieHeader missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "captcha=stale") {
		t.Errorf("mergeCookieHeader kept stale browser captcha: %q", got)
	}
	// Deterministic ordering (sorted by name).
	if !strings.HasPrefix(got, "captcha=fresh; other=keep; token=sess") {
		t.Errorf("mergeCookieHeader not sorted deterministically: %q", got)
	}

	if got := mergeCookieHeader("", nil); got != "" {
		t.Errorf("mergeCookieHeader(empty) = %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// End-to-end resolver flow (mocked server)
// ---------------------------------------------------------------------------

// workuploadPuzzleFixture builds a /puzzle response whose targets are known
// indices, so the mock server and the test agree on the answer.
func workuploadPuzzleFixture(puzzle string, rng int, indices ...int) string {
	find := make([]string, 0, len(indices))
	for _, i := range indices {
		find = append(find, wuTestDigest(puzzle+fmt.Sprint(i)))
	}
	findJSON, _ := json.Marshal(find)
	return fmt.Sprintf(`{"success":true,"data":{"puzzle":%q,"range":%d,"find":%s}}`, puzzle, rng, findJSON)
}

func TestResolveWorkupload_FullFlow(t *testing.T) {
	// NOT parallel — replaces http.DefaultTransport which is global state.

	const (
		wallPage   = `<html><head><title>workupload - Are you a human?</title></head><body>Security Check</body></html>`
		realPage   = `<html><head><title>Downloads.zip</title></head><body><a href="/start/code123" class="btn btn-prio nu-reward">Download</a></body></html>`
		puzzle     = "mock.puzzle.42"
		wantAnswer = "7 1234 "
	)

	var (
		filePageHits int
		puzzleHits   int
		postBody     string
		apiCookie    string
		apiReferer   string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/file/code123":
			filePageHits++
			switch filePageHits {
			case 1: // captcha wall, no cookies issued
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, wallPage)
			default: // validated session → real page + token cookie
				http.SetCookie(w, &http.Cookie{Name: "token", Value: "sess123", Path: "/"})
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, realPage)
			}
		case "/puzzle":
			puzzleHits++
			http.SetCookie(w, &http.Cookie{Name: "captcha", Value: "challenge-payload", Path: "/"})
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, workuploadPuzzleFixture(puzzle, 20000, 7, 1234))
		case "/captcha":
			body, _ := io.ReadAll(r.Body)
			postBody = string(body)
			http.SetCookie(w, &http.Cookie{Name: "captcha", Value: "validated-payload", Path: "/"})
			w.WriteHeader(http.StatusOK)
		case "/api/file/getDownloadServer/code123":
			apiCookie = r.Header.Get("Cookie")
			apiReferer = r.Header.Get("Referer")
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"success":true,"data":{"url":"https://s1.workupload.com/download/code123"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = rewriteToServer("workupload.com", srv.URL)
	defer func() { http.DefaultTransport = origTransport }()

	r := NewHostResolver()
	r.cookieSource = func(hostname string) string {
		if strings.Contains(hostname, "workupload") {
			return "captcha=stale-browser; other=keep"
		}
		return ""
	}

	result, err := r.resolveWorkupload("https://workupload.com/start/code123")
	if err != nil {
		t.Fatalf("resolveWorkupload failed: %v", err)
	}

	if result.URL != "https://s1.workupload.com/download/code123" {
		t.Errorf("result.URL = %q, want direct CDN URL", result.URL)
	}
	// The submitted answer is the space-separated trailing-space form.
	vals, _ := url.ParseQuery(postBody)
	if got := vals.Get("captcha"); got != wantAnswer {
		t.Errorf("POST /captcha body captcha=%q, want %q", got, wantAnswer)
	}
	if puzzleHits != 1 {
		t.Errorf("puzzle fetched %d times, want 1", puzzleHits)
	}
	// API request must carry the validated captcha (jar wins over the
	// stale browser copy) and the token from the real page.
	for _, want := range []string{"captcha=validated-payload", "token=sess123", "other=keep"} {
		if !strings.Contains(apiCookie, want) {
			t.Errorf("API cookie missing %q, got %q", want, apiCookie)
		}
	}
	if strings.Contains(apiCookie, "captcha=stale-browser") {
		t.Errorf("API cookie used stale browser captcha, got %q", apiCookie)
	}
	if !strings.Contains(apiReferer, "/file/code123") {
		t.Errorf("API referer = %q, want the file page URL", apiReferer)
	}
	// Returned headers must let the downloader serve the direct URL.
	gotCookie := result.Headers["Cookie"]
	for _, want := range []string{"captcha=validated-payload", "token=sess123"} {
		if !strings.Contains(gotCookie, want) {
			t.Errorf("result Cookie header missing %q, got %q", want, gotCookie)
		}
	}
	if result.Headers["Referer"] != "https://workupload.com/file/code123" {
		t.Errorf("result Referer = %q, want file page URL", result.Headers["Referer"])
	}
}

func TestResolveWorkupload_ArchiveFlow(t *testing.T) {
	// NOT parallel — replaces http.DefaultTransport which is global state.

	var apiHit string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/archive/code123":
			http.SetCookie(w, &http.Cookie{Name: "token", Value: "sess", Path: "/"})
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><head><title>game.7z</title></head><body><a href="/archive/code123/start">Download</a></body></html>`)
		case "/api/archive/getDownloadServer/code123":
			apiHit = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"success":true,"data":{"url":"https://s2.workupload.com/download/code123"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = rewriteToServer("workupload.com", srv.URL)
	defer func() { http.DefaultTransport = origTransport }()

	r := NewHostResolver()
	result, err := r.resolveWorkupload("https://workupload.com/archive/code123/start")
	if err != nil {
		t.Fatalf("resolveWorkupload archive failed: %v", err)
	}
	if result.URL != "https://s2.workupload.com/download/code123" {
		t.Errorf("result.URL = %q, want archive CDN URL", result.URL)
	}
	if apiHit != "/api/archive/getDownloadServer/code123" {
		t.Errorf("API path = %q, want archive endpoint", apiHit)
	}
}

func TestResolveWorkupload_NoWallWithBrowserCookie(t *testing.T) {
	// NOT parallel — replaces http.DefaultTransport which is global state.

	var puzzleHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/file/code123":
			// Browser cookie already cleared the wall: real page first try.
			http.SetCookie(w, &http.Cookie{Name: "token", Value: "browsertok", Path: "/"})
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><head><title>Downloads.zip</title></head><body>Download</body></html>`)
		case "/puzzle":
			puzzleHits++
			http.NotFound(w, r)
		case "/api/file/getDownloadServer/code123":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"success":true,"data":{"url":"https://s3.workupload.com/download/code123"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = rewriteToServer("workupload.com", srv.URL)
	defer func() { http.DefaultTransport = origTransport }()

	r := NewHostResolver()
	r.cookieSource = func(hostname string) string {
		if strings.Contains(hostname, "workupload") {
			return "captcha=solved-in-browser"
		}
		return ""
	}

	result, err := r.resolveWorkupload("https://workupload.com/file/code123")
	if err != nil {
		t.Fatalf("resolveWorkupload failed: %v", err)
	}
	if puzzleHits != 0 {
		t.Errorf("puzzle endpoint hit %d times, want 0 (browser cookie path)", puzzleHits)
	}
	if result.URL != "https://s3.workupload.com/download/code123" {
		t.Errorf("result.URL = %q", result.URL)
	}
	// Browser captcha + response token both reach the downloader.
	gotCookie := result.Headers["Cookie"]
	for _, want := range []string{"captcha=solved-in-browser", "token=browsertok"} {
		if !strings.Contains(gotCookie, want) {
			t.Errorf("result Cookie header missing %q, got %q", want, gotCookie)
		}
	}
}

func TestResolveWorkupload_WallPersists(t *testing.T) {
	// NOT parallel — replaces http.DefaultTransport which is global state.

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/file/code123":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><head><title>workupload - Are you a human?</title></head><body>Security Check</body></html>`)
		case "/puzzle":
			http.SetCookie(w, &http.Cookie{Name: "captcha", Value: "challenge", Path: "/"})
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, workuploadPuzzleFixture("mock.puzzle.wall", 1000, 3))
		case "/captcha":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = rewriteToServer("workupload.com", srv.URL)
	defer func() { http.DefaultTransport = origTransport }()

	r := NewHostResolver()
	_, err := r.resolveWorkupload("https://workupload.com/file/code123")
	if err == nil {
		t.Fatal("resolveWorkupload expected error when wall persists, got nil")
	}
	if !strings.Contains(err.Error(), "did not clear") {
		t.Errorf("error = %q, want wall-persists message", err)
	}
}

func TestResolveWorkupload_BadURL(t *testing.T) {
	t.Parallel()
	r := NewHostResolver()
	_, err := r.resolveWorkupload("https://example.com/file/code123")
	if err == nil || !strings.Contains(err.Error(), "could not extract file ID") {
		t.Errorf("resolveWorkupload(bad URL) error = %v, want file ID extraction error", err)
	}
}

// ---------------------------------------------------------------------------
// Live probe (opt-in; skipped by default so the suite stays offline-safe)
// ---------------------------------------------------------------------------

// TestWorkuploadLiveProbe exercises the resolver against the real
// workupload.com: the public verification file plus (when F95Zone cookies
// are present in a browser) the masked sample link from issue F95-ggyw.
// Run with MOXIE_LIVE=1. The direct URL is only fetched to confirm the
// headers are accepted; the body is discarded.
func TestWorkuploadLiveProbe(t *testing.T) {
	if os.Getenv("MOXIE_LIVE") == "" {
		t.Skip("live network probe; set MOXIE_LIVE=1 to run")
	}

	const (
		publicFile = "https://workupload.com/file/ckfAZgjjgej"
		maskedURL  = "https://f95zone.to/masked/workupload.com/94386/6265512/J7FKpRUw76BlZYOqOvcFhmfdSgE/MHIm_R9k4JtHPQtsd"
	)

	r := NewHostResolver()
	resolveAndCheck := func(t *testing.T, raw string) *ResolveResult {
		t.Helper()
		result, err := r.resolveWorkupload(raw)
		if err != nil {
			t.Fatalf("resolveWorkupload(%s) failed: %v", raw, err)
		}
		if result.URL == "" {
			t.Fatalf("resolveWorkupload(%s) returned empty URL", raw)
		}
		t.Logf("resolved %s → %s", raw, result.URL)
		return result
	}

	// 1. Public verification file.
	result := resolveAndCheck(t, publicFile)

	// 2. Confirm the direct URL serves a file with the returned headers
	// (status 200 + non-HTML content type), body discarded.
	req, err := http.NewRequest(http.MethodGet, result.URL, nil)
	if err != nil {
		t.Fatalf("create direct URL request: %v", err)
	}
	for k, v := range result.Headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("direct URL GET failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("direct URL status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); strings.Contains(ct, "text/html") {
		t.Errorf("direct URL served HTML (%q) — cookies not accepted", ct)
	}

	// 3. Masked F95Zone sample (requires browser session cookies).
	cookie, err := browser.GetF95Cookies()
	if err != nil {
		t.Logf("skipping masked URL (no F95Zone cookies): %v", err)
		return
	}
	r.SetF95Cookie(cookie)
	real, err := r.unwrapMasked(maskedURL)
	if err != nil {
		t.Logf("skipping masked URL (unwrap failed): %v", err)
		return
	}
	if real == maskedURL {
		t.Logf("skipping masked URL (unwrap returned URL unchanged)")
		return
	}
	t.Logf("masked unwrap → %s", real)
	resolveAndCheck(t, real)
}
