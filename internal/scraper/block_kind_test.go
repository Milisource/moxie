package scraper

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestBlockKindClassification(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name            string
		status          int
		body, mitigated string
		kind            BlockKind
		retry           bool
	}{
		{"plain forbidden", 403, "", "", BlockForbidden, false},
		{"challenge forbidden", 403, "_cf_chl_opt", "", BlockChallenge, false},
		{"plain unavailable", 503, "", "", BlockUnavailable, true},
		{"challenge unavailable", 503, "", "challenge", BlockChallenge, false},
		{"rate limit", 429, "", "", BlockRateLimit, true},
		{"challenge success status", 200, "cf-browser-verification", "", BlockChallenge, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body))}
			resp.Header.Set("cf-mitigated", tc.mitigated)
			resp.Header.Set("Retry-After", "2")
			_, err := (&Client{}).checkBlocked(resp)
			var blocked *BlockedError
			if !errors.As(err, &blocked) || blocked.Kind != tc.kind {
				t.Fatalf("error = %#v, want kind %v", err, tc.kind)
			}
			if got := retryable(err); got != tc.retry {
				t.Errorf("retryable = %v, want %v", got, tc.retry)
			}
			if got := IsCloudflareChallenge(err); got != (tc.kind == BlockChallenge) {
				t.Errorf("challenge = %v", got)
			}
			if (tc.kind == BlockUnavailable || tc.kind == BlockRateLimit) && blocked.RetryAfter.Seconds() != 2 {
				t.Errorf("RetryAfter = %v", blocked.RetryAfter)
			}
		})
	}
}

func TestRetryableBlockKindOverridesMessageAndStatus(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind   BlockKind
		status int
		want   bool
	}{
		{BlockChallenge, 503, false}, {BlockForbidden, 503, false}, {BlockRateLimit, 429, true},
		{BlockUnavailable, 403, true}, {BlockUnknown, 503, true}, {BlockUnknown, 403, false}, {BlockUnknown, 429, false},
	} {
		err := &BlockedError{Kind: tc.kind, StatusCode: tc.status, Reason: "Cloudflare"}
		if got := retryable(err); got != tc.want {
			t.Errorf("kind %v status %d: got %v want %v", tc.kind, tc.status, got, tc.want)
		}
		if got := IsCloudflareChallenge(err); got != (tc.kind == BlockChallenge) {
			t.Errorf("kind %v challenge = %v", tc.kind, got)
		}
	}
}

func TestBlockKindString(t *testing.T) {
	t.Parallel()
	for kind, want := range map[BlockKind]string{BlockUnknown: "unknown", BlockChallenge: "challenge", BlockRateLimit: "rate limit", BlockUnavailable: "unavailable", BlockForbidden: "forbidden", BlockKind(99): "unknown"} {
		if got := kind.String(); got != want {
			t.Errorf("kind %d = %q, want %q", int(kind), got, want)
		}
	}
}

func TestCookieTransportPreservesRequest(t *testing.T) {
	t.Parallel()
	for _, cookie := range []string{"", "custom=present"} {
		rt := &recordingTransport{}
		ct := &cookieTransport{inner: rt, cookieValue: "cf_clearance=fresh"}
		req, _ := http.NewRequest(http.MethodGet, "https://f95zone.to/threads/1/", nil)
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		resp, err := ct.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if got := req.Header.Get("Cookie"); got != cookie {
			t.Errorf("original cookie = %q, want %q", got, cookie)
		}
		want := "cf_clearance=fresh"
		if cookie != "" {
			want = cookie + "; " + want
		}
		if rt.cookies[0] != want {
			t.Errorf("sent cookie = %q, want %q", rt.cookies[0], want)
		}
	}
}
