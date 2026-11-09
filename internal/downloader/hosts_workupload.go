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
	"sort"
	"strconv"
	"strings"

	"github.com/mili/moxie/internal/log"
)

// --- Workupload ---
// workupload.com guards every download page with a home-grown anti-bot
// puzzle (not reCAPTCHA, not a slider): a SHA-256 proof-of-work whose
// answer is computable in pure Go in a few milliseconds. The observed
// flow (page embeds a small JS solver that runs it in the browser):
//
//  1. GET the download page → "Are you a human?" wall page
//  2. GET /puzzle → {"success":true,"data":{"puzzle":"<ts>.<salt>",
//     "range":N,"find":["<64 hex digest>",...]}} and sets a `captcha`
//     cookie carrying the encrypted challenge payload
//  3. Solve: for i in [0,N): the SHA-256 hex digest of (puzzle + i); the
//     indices whose digest appears in `find` are the answer
//  4. POST /captcha with form captcha="<i1> <i2> ... " (space-separated,
//     trailing space — exactly the JS `val() + s + ' '` concatenation)
//     → HTTP 200, and the `captcha` cookie is refreshed to a validated
//     payload
//  5. GET the download page again → the real file page; the response sets
//     the `token` session cookie
//  6. GET /api/file/getDownloadServer/<ID> (or /api/archive/...) with the
//     captcha+token cookies → {"success":true,"data":{"url":"https://
//     sNN.workupload.com/download/<ID>"}}
//  7. The direct URL only serves the file with the captcha+token cookies;
//     without them it returns the security-wall HTML again.
//
// The /start/<ID> step older workupload clients used is not required: the
// API answers with the captcha+token pair alone.
const (
	// workuploadWallMarker is the page title marker of the anti-bot wall
	// ("workupload - Are you a human?").
	workuploadWallMarker = "Are you a human"

	// workuploadUserAgent matches the other host resolvers' Firefox UA.
	workuploadUserAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0"

	// workuploadCaptchaRetries bounds the puzzle round: the server can
	// transiently reject a submission, in which case a fresh puzzle is
	// fetched and solved again.
	workuploadCaptchaRetries = 2
)

var (
	reWorkuploadFile    = regexp.MustCompile(`workupload\.com/(?:file|start)/([a-zA-Z0-9]+)`)
	reWorkuploadArchive = regexp.MustCompile(`workupload\.com/archive/([a-zA-Z0-9]+)`)
)

// workuploadFileParts extracts the file ID and kind ("file" or "archive")
// from a workupload URL. Returns ok=false for anything that does not look
// like a workupload file link.
func workuploadFileParts(rawURL string) (id, kind string, ok bool) {
	if m := reWorkuploadArchive.FindStringSubmatch(rawURL); len(m) >= 2 {
		return m[1], "archive", true
	}
	if m := reWorkuploadFile.FindStringSubmatch(rawURL); len(m) >= 2 {
		return m[1], "file", true
	}
	return "", "", false
}

// workuploadPageURL returns the canonical download-page URL for a file,
// mapping /start/<ID> and /archive/<ID>/start link forms back to the page
// the captcha flow expects, and stripping query/fragment noise.
func workuploadPageURL(rawURL, id, kind string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.Path = "/file/" + id
	if kind == "archive" {
		u.Path = "/archive/" + id
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// workuploadAPIURL builds the getDownloadServer API URL on the same
// scheme+host as the input page so mirrors keep working.
func workuploadAPIURL(pageURL, kind, id string) string {
	u, err := url.Parse(pageURL)
	if err != nil {
		return "https://workupload.com/api/" + kind + "/getDownloadServer/" + id
	}
	return u.Scheme + "://" + u.Host + "/api/" + kind + "/getDownloadServer/" + id
}

// workuploadPuzzleJSON is the wire shape of GET /puzzle.
type workuploadPuzzleJSON struct {
	Success bool `json:"success"`
	Data    struct {
		Puzzle string   `json:"puzzle"`
		Range  int      `json:"range"`
		Find   []string `json:"find"`
	} `json:"data"`
}

// parseWorkuploadPuzzle validates and extracts the challenge fields from a
// /puzzle response body.
func parseWorkuploadPuzzle(body []byte) (puzzle string, rng int, find []string, err error) {
	var p workuploadPuzzleJSON
	if err := json.Unmarshal(body, &p); err != nil {
		return "", 0, nil, fmt.Errorf("puzzle response is not JSON: %w", err)
	}
	if !p.Success {
		return "", 0, nil, fmt.Errorf("puzzle response success=false")
	}
	if p.Data.Puzzle == "" || p.Data.Range <= 0 || len(p.Data.Find) == 0 {
		return "", 0, nil, fmt.Errorf("puzzle response missing puzzle/range/find data")
	}
	return p.Data.Puzzle, p.Data.Range, p.Data.Find, nil
}

// solveWorkuploadPuzzle computes the answer to a workupload proof-of-work
// challenge: the indices i in [0, range) whose SHA-256 digest of
// (puzzle + i) appears in find, returned space-separated with a trailing
// space — exactly the string the page's JS solver submits to /captcha.
func solveWorkuploadPuzzle(puzzle string, rng int, find []string) (string, error) {
	if puzzle == "" || rng <= 0 || len(find) == 0 {
		return "", fmt.Errorf("invalid puzzle input (puzzle=%q range=%d find=%d)", puzzle, rng, len(find))
	}
	targets := make(map[string]struct{}, len(find))
	for _, f := range find {
		targets[strings.ToLower(f)] = struct{}{}
	}
	answers := make([]string, 0, len(find))
	for i := 0; i < rng && len(answers) < len(find); i++ {
		sum := sha256.Sum256([]byte(puzzle + strconv.Itoa(i)))
		if _, ok := targets[hex.EncodeToString(sum[:])]; ok {
			answers = append(answers, strconv.Itoa(i))
		}
	}
	if len(answers) != len(find) {
		return "", fmt.Errorf("found %d/%d target digests", len(answers), len(find))
	}
	return strings.Join(answers, " ") + " ", nil
}

// workuploadCookieHeader returns the Cookie header for workupload requests:
// the browser's stored workupload cookies (if any) merged with the fresh
// session jar. Jar values win on name conflicts so a just-solved captcha
// can never be shadowed by a stale browser copy.
func (r *HostResolver) workuploadCookieHeader(jar map[string]string) string {
	var browser string
	if r.cookieSource != nil {
		browser = r.cookieSource("workupload.com")
	}
	return mergeCookieHeader(browser, jar)
}

// mergeCookieHeader merges a browser Cookie header with a session cookie
// jar into one deterministic header; jar values win on name conflicts.
func mergeCookieHeader(browser string, jar map[string]string) string {
	merged := make(map[string]string, len(jar)+4)
	for _, pair := range strings.Split(browser, ";") {
		if k, v, ok := strings.Cut(strings.TrimSpace(pair), "="); ok && k != "" {
			merged[k] = v
		}
	}
	for k, v := range jar {
		merged[k] = v
	}
	if len(merged) == 0 {
		return ""
	}
	names := make([]string, 0, len(merged))
	for k := range merged {
		names = append(names, k)
	}
	sort.Strings(names)
	pairs := make([]string, 0, len(names))
	for _, k := range names {
		pairs = append(pairs, k+"="+merged[k])
	}
	return strings.Join(pairs, "; ")
}

// workuploadSolveCaptcha drives the proof-of-work round end to end: fetch
// the challenge, compute the answer, submit it. On success the caller's
// cookie jar holds a validated `captcha` cookie.
func workuploadSolveCaptcha(fetch func(method, target string, form url.Values) (*http.Response, []byte, error)) error {
	for attempt := 0; attempt < workuploadCaptchaRetries; attempt++ {
		_, pbody, err := fetch(http.MethodGet, "https://workupload.com/puzzle", nil)
		if err != nil {
			return fmt.Errorf("workupload GET puzzle: %w", err)
		}
		puzzle, rng, find, err := parseWorkuploadPuzzle(pbody)
		if err != nil {
			return fmt.Errorf("workupload parse puzzle: %w", err)
		}
		answer, err := solveWorkuploadPuzzle(puzzle, rng, find)
		if err != nil {
			return fmt.Errorf("workupload solve puzzle: %w", err)
		}
		resp, cbody, err := fetch(http.MethodPost, "https://workupload.com/captcha", url.Values{"captcha": {answer}})
		if err != nil {
			return fmt.Errorf("workupload POST captcha: %w", err)
		}
		if resp.StatusCode == http.StatusOK {
			return nil
		}
		log.Warn("workupload captcha submission rejected, retrying",
			"attempt", attempt, "status", resp.StatusCode, "body", truncate(string(cbody), 120))
	}
	return fmt.Errorf("workupload: captcha submission failed after %d attempts", workuploadCaptchaRetries)
}

// truncate shortens s for error and log messages.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// resolveWorkupload resolves a workupload.com file or archive link to a
// direct download URL by solving the site's SHA-256 proof-of-work puzzle
// (when the browser cookies do not already carry a validated captcha) and
// asking the getDownloadServer API for the direct CDN URL.
func (r *HostResolver) resolveWorkupload(rawURL string) (*ResolveResult, error) {
	id, kind, ok := workuploadFileParts(rawURL)
	if !ok || id == "" {
		return nil, fmt.Errorf("workupload: could not extract file ID from %s (expected /file/<ID> or /archive/<ID>)", rawURL)
	}
	pageURL := workuploadPageURL(rawURL, id, kind)

	// jar accumulates the fresh session cookies (captcha, token) from the
	// workupload responses; it always wins over browser cookies.
	jar := map[string]string{}

	fetch := func(method, target string, form url.Values) (*http.Response, []byte, error) {
		var body io.Reader
		if form != nil {
			body = strings.NewReader(form.Encode())
		}
		req, err := http.NewRequest(method, target, body)
		if err != nil {
			return nil, nil, fmt.Errorf("workupload create %s request: %w", method, err)
		}
		req.Header.Set("User-Agent", workuploadUserAgent)
		req.Header.Set("Referer", pageURL)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		if form != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		if cookie := r.workuploadCookieHeader(jar); cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return nil, nil, err
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, nil, err
		}
		for _, c := range resp.Cookies() {
			jar[c.Name] = c.Value
		}
		return resp, data, nil
	}

	// Step 1 — the download page. Browser cookies may already carry a
	// solved captcha (the user cleared the wall in a browser), in which
	// case the real file page is served and the puzzle is skipped.
	pageResp, pageBody, err := fetch(http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("workupload GET page: %w", err)
	}
	if pageResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("workupload: GET page returned HTTP %d", pageResp.StatusCode)
	}
	if strings.Contains(string(pageBody), workuploadWallMarker) {
		if err := workuploadSolveCaptcha(fetch); err != nil {
			return nil, err
		}
		// Re-fetch the page: a validated captcha serves the real file
		// page and sets the session `token` cookie.
		pageResp, pageBody, err = fetch(http.MethodGet, pageURL, nil)
		if err != nil {
			return nil, fmt.Errorf("workupload GET page after captcha: %w", err)
		}
		if strings.Contains(string(pageBody), workuploadWallMarker) {
			return nil, fmt.Errorf("workupload: captcha solve did not clear the security wall")
		}
	}

	// Step 2 — ask the API for the direct download server URL.
	apiURL := workuploadAPIURL(pageURL, kind, id)
	apiResp, apiBody, err := fetch(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("workupload API: %w", err)
	}
	var api struct {
		Success bool `json:"success"`
		Data    struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(apiBody, &api); err != nil {
		return nil, fmt.Errorf("workupload: API returned non-JSON (HTTP %d): %s", apiResp.StatusCode, truncate(string(apiBody), 160))
	}
	if !api.Success || api.Data.URL == "" {
		return nil, fmt.Errorf("workupload: API rejected the session (HTTP %d): %s", apiResp.StatusCode, truncate(string(apiBody), 160))
	}

	log.Debug("workupload: resolved via getDownloadServer API", "url", api.Data.URL)
	return &ResolveResult{
		URL: api.Data.URL,
		Headers: map[string]string{
			"User-Agent": workuploadUserAgent,
			"Referer":    pageURL,
			"Cookie":     r.workuploadCookieHeader(jar),
		},
	}, nil
}
