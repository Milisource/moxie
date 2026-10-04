package downloader

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// resolveVikingFile — Cloudflare Turnstile branch (F95-ob9m)
// ---------------------------------------------------------------------------

// turnstileFixturePage is the shape of vikingfile's current page: a Turnstile
// widget whose callback XHR-POSTs cf-turnstile-response and sets
// #download-link.href from the JSON {"link": ...} response. There is no
// hidden `op` form and no direct link in the static HTML.
const turnstileFixturePage = `<html><head>
	<script src="https://challenges.cloudflare.com/turnstile/v0/api.js?onload=showCaptcha" defer></script>
</head><body>
	<div id="captcha"></div>
	<a id="download-link" class="button hidden">Generating download link </a>
	<script>
	function cloudflareCallback(token) {
		var xhr = new XMLHttpRequest();
		xhr.open("POST", window.location.href, true);
		xhr.send("cf-turnstile-response=" + encodeURIComponent(token));
	}
	</script>
</body></html>`

// withVikingFileServer serves body at any /f/* path by rewriting requests to
// the vikingfile hostname, mirroring the existing vikingfile tests.
func withVikingFileServer(t *testing.T, body string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, body)
	}))
	t.Cleanup(server.Close)

	origTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Hostname(), "vikingfile") {
			testURL := server.URL + req.URL.Path
			if req.URL.RawQuery != "" {
				testURL += "?" + req.URL.RawQuery
			}
			newReq, err := http.NewRequest(req.Method, testURL, req.Body)
			if err != nil {
				return nil, err
			}
			newReq.Header = req.Header.Clone()
			return origTransport.RoundTrip(newReq)
		}
		return origTransport.RoundTrip(req)
	})
	t.Cleanup(func() { http.DefaultTransport = origTransport })
}

func TestResolveVikingFile_TurnstileBrowserSolver(t *testing.T) {
	// NOT parallel — swaps http.DefaultTransport and the default solver.

	const rawURL = "https://vikingfile.com/f/abc123"
	revealed := "https://vikingfile.com/d/Q4GEdYpgph/CyanBrain-1.1.4.zip"

	var solvedFor string
	SetDefaultTurnstileSolver(func(ctx context.Context, pageURL string) (string, error) {
		solvedFor = pageURL
		return revealed, nil
	})
	t.Cleanup(func() { SetDefaultTurnstileSolver(nil) })

	withVikingFileServer(t, turnstileFixturePage)

	r := NewHostResolver()
	result, err := r.Resolve(rawURL, "vikingfile")
	if err != nil {
		t.Fatalf("Resolve vikingfile Turnstile page failed: %v", err)
	}
	if result.URL != revealed {
		t.Errorf("resolved URL = %q, want %q", result.URL, revealed)
	}
	if solvedFor != rawURL {
		t.Errorf("solver called with %q, want %q", solvedFor, rawURL)
	}
	if got := result.Headers["Referer"]; got != rawURL {
		t.Errorf("Referer = %q, want %q", got, rawURL)
	}
}

func TestResolveVikingFile_TurnstileSolverDisabled(t *testing.T) {
	// NOT parallel — swaps http.DefaultTransport and the default solver.

	SetDefaultTurnstileSolver(nil)
	t.Cleanup(func() { SetDefaultTurnstileSolver(nil) })

	withVikingFileServer(t, turnstileFixturePage)

	r := NewHostResolver()
	_, err := r.Resolve("https://vikingfile.com/f/abc123", "vikingfile")
	if err == nil {
		t.Fatal("expected error when no Turnstile solver is installed")
	}
	// The error must stay challenge-classified so the caller can still fall
	// back to the browser download path.
	if !strings.Contains(err.Error(), "Turnstile") {
		t.Errorf("error %q must mention Turnstile", err)
	}
	if !isChallengeFailure(err) {
		t.Errorf("error %q must be classified as a challenge failure", err)
	}
}

func TestResolveVikingFile_TurnstileSolverError(t *testing.T) {
	// NOT parallel — swaps http.DefaultTransport and the default solver.

	SetDefaultTurnstileSolver(func(context.Context, string) (string, error) {
		return "", fmt.Errorf("turnstile: widget never solved")
	})
	t.Cleanup(func() { SetDefaultTurnstileSolver(nil) })

	withVikingFileServer(t, turnstileFixturePage)

	r := NewHostResolver()
	_, err := r.Resolve("https://vikingfile.com/f/abc123", "vikingfile")
	if err == nil {
		t.Fatal("expected error when the Turnstile solver fails")
	}
	if !strings.Contains(err.Error(), "widget never solved") {
		t.Errorf("error %q must wrap the solver failure", err)
	}
	if !isChallengeFailure(err) {
		t.Errorf("error %q must be classified as a challenge failure", err)
	}
}

// A legacy page with a direct link must resolve without launching a browser:
// the Turnstile solver is reserved for Turnstile-gated pages.
func TestResolveVikingFile_DirectLinkSkipsTurnstileSolver(t *testing.T) {
	// NOT parallel — swaps http.DefaultTransport and the default solver.

	solverCalled := false
	SetDefaultTurnstileSolver(func(context.Context, string) (string, error) {
		solverCalled = true
		return "", fmt.Errorf("solver must not run for a direct-link page")
	})
	t.Cleanup(func() { SetDefaultTurnstileSolver(nil) })

	dlURL := "https://vikingfile.com/d/Q4GEdYpgph/CyanBrain-1.1.4.zip"
	withVikingFileServer(t, `<html><body>
		<h2>CyanBrain-1.1.4.zip</h2>
		<a id="download-link" href="`+dlURL+`">Download</a>
	</body></html>`)

	r := NewHostResolver()
	result, err := r.Resolve("https://vikingfile.com/f/k3rSl0cvF9", "vikingfile")
	if err != nil {
		t.Fatalf("Resolve vikingfile direct link failed: %v", err)
	}
	if result.URL != dlURL {
		t.Errorf("resolved URL = %q, want %q", result.URL, dlURL)
	}
	if solverCalled {
		t.Error("Turnstile solver must not run for a page with a direct download link")
	}
}

func TestIsVikingFileTurnstilePage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"widget bootstrap", `<script src="https://challenges.cloudflare.com/turnstile/v0/api.js"></script>`, true},
		{"xhr body field", `<script>xhr.send("cf-turnstile-response=" + token)</script>`, true},
		{"callback name", `<script>function cloudflareCallback(t){}</script>`, true},
		{"legacy op form", `<input type="hidden" name="op" value="download1">`, false},
		{"captcha word only", `<p>This file requires a captcha to download.</p>`, false},
		{"empty", ``, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isVikingFileTurnstilePage(tt.body); got != tt.want {
				t.Errorf("isVikingFileTurnstilePage(%q) = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}
