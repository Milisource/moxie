package downloader

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// stubPixeldrainInfo points pixeldrainInfoBase at an httptest server that
// answers every request with status/body, and restores the base on cleanup.
// These tests are deliberately NOT parallel: they mutate package-level probe
// state, and the parallel links tests assert exact unthrottled scores.
func stubPixeldrainInfo(t *testing.T, status int, body string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/info") {
			t.Errorf("probe path = %q, want .../info", r.URL.Path)
		}
		if got := r.Header.Get("Referer"); !strings.Contains(got, "/u/") {
			t.Errorf("probe Referer = %q, want a /u/<id> page", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	old := pixeldrainInfoBase
	pixeldrainInfoBase = srv.URL
	t.Cleanup(func() { pixeldrainInfoBase = old })
}

const pixeldrainTestURL = "https://pixeldrain.com/api/file/abc123"

func TestProbePixeldrainLimit_Reported(t *testing.T) {
	stubPixeldrainInfo(t, http.StatusOK, `{"success":true,"download_speed_limit":1048576}`)

	limit, limited, err := NewHostResolver().probePixeldrainLimit(context.Background(), pixeldrainTestURL)
	if err != nil {
		t.Fatalf("probePixeldrainLimit: %v", err)
	}
	if !limited || limit != 1048576 {
		t.Fatalf("got (limit=%d, limited=%v), want (1048576, true)", limit, limited)
	}
}

func TestProbePixeldrainLimit_Unlimited(t *testing.T) {
	stubPixeldrainInfo(t, http.StatusOK, `{"success":true,"download_speed_limit":0}`)

	limit, limited, err := NewHostResolver().probePixeldrainLimit(context.Background(), pixeldrainTestURL)
	if err != nil {
		t.Fatalf("probePixeldrainLimit: %v", err)
	}
	if limited || limit != 0 {
		t.Fatalf("got (limit=%d, limited=%v), want (0, false)", limit, limited)
	}
}

func TestProbePixeldrainLimit_LimitError(t *testing.T) {
	stubPixeldrainInfo(t, http.StatusForbidden, `{"value":"transfer_limit_exceeded"}`)

	_, limited, err := NewHostResolver().probePixeldrainLimit(context.Background(), pixeldrainTestURL)
	if err != nil {
		t.Fatalf("probePixeldrainLimit: %v", err)
	}
	if !limited {
		t.Fatal("expected limited=true for transfer_limit_exceeded")
	}
}

func TestProbePixeldrainLimit_UnknownBodyIsError(t *testing.T) {
	stubPixeldrainInfo(t, http.StatusInternalServerError, `boom`)

	if _, _, err := NewHostResolver().probePixeldrainLimit(context.Background(), pixeldrainTestURL); err == nil {
		t.Fatal("expected an error for a non-JSON body")
	}
}

func TestProbePixeldrainLimit_NoFileID(t *testing.T) {
	if _, _, err := NewHostResolver().probePixeldrainLimit(context.Background(), "https://example.com/nope"); err == nil {
		t.Fatal("expected an error when no file ID is present")
	}
}

func TestCheckHostThrottle_RejectsAndMarks(t *testing.T) {
	stubPixeldrainInfo(t, http.StatusOK, `{"download_speed_limit":1048576}`)
	ClearHostThrottle("pixeldrain")
	t.Cleanup(func() { ClearHostThrottle("pixeldrain") })

	err := NewHostResolver().checkHostThrottle(context.Background(), "pixeldrain", pixeldrainTestURL)
	if !errors.Is(err, ErrHostThrottled) {
		t.Fatalf("checkHostThrottle err = %v, want ErrHostThrottled", err)
	}
	var te *ThrottledError
	if !errors.As(err, &te) || te.Host != "pixeldrain" || te.Limit != 1048576 {
		t.Fatalf("unexpected ThrottledError: %#v", te)
	}
	if !HostThrottled("pixeldrain") {
		t.Fatal("expected pixeldrain to be marked throttled")
	}
	if got := ScoreLinkHost("pixeldrain"); got != 25-throttlePenalty {
		t.Fatalf("throttled ScoreLinkHost(pixeldrain) = %d, want %d", got, 25-throttlePenalty)
	}
}

func TestCheckHostThrottle_AllowedByEnv(t *testing.T) {
	stubPixeldrainInfo(t, http.StatusOK, `{"download_speed_limit":1048576}`)
	t.Setenv("MOXIE_ALLOW_THROTTLED", "1")
	ClearHostThrottle("pixeldrain")
	t.Cleanup(func() { ClearHostThrottle("pixeldrain") })

	if err := NewHostResolver().checkHostThrottle(context.Background(), "pixeldrain", pixeldrainTestURL); err != nil {
		t.Fatalf("with MOXIE_ALLOW_THROTTLED=1 err = %v, want nil", err)
	}
	// The host is still marked, so later ranking avoids it.
	if !HostThrottled("pixeldrain") {
		t.Fatal("expected pixeldrain to be marked even when the download proceeds")
	}
}

func TestCheckHostThrottle_ProbeFailureProceeds(t *testing.T) {
	// A probe that cannot determine the state (non-200 + non-JSON) must not
	// block a download nor mark the host.
	stubPixeldrainInfo(t, http.StatusInternalServerError, `boom`)
	ClearHostThrottle("pixeldrain")
	t.Cleanup(func() { ClearHostThrottle("pixeldrain") })

	if err := NewHostResolver().checkHostThrottle(context.Background(), "pixeldrain", pixeldrainTestURL); err != nil {
		t.Fatalf("checkHostThrottle err = %v, want nil on probe failure", err)
	}
	if HostThrottled("pixeldrain") {
		t.Fatal("probe failure must not mark the host throttled")
	}
}

func TestCheckHostThrottle_UnlimitedClearsMark(t *testing.T) {
	stubPixeldrainInfo(t, http.StatusOK, `{"download_speed_limit":0}`)
	MarkHostThrottled("pixeldrain", time.Hour)
	t.Cleanup(func() { ClearHostThrottle("pixeldrain") })

	if err := NewHostResolver().checkHostThrottle(context.Background(), "pixeldrain", pixeldrainTestURL); err != nil {
		t.Fatalf("checkHostThrottle err = %v", err)
	}
	if HostThrottled("pixeldrain") {
		t.Fatal("a recovered host must clear its throttle mark")
	}
}

func TestCheckHostThrottle_NonPixeldrainPassthrough(t *testing.T) {
	// No network stub: any probe for a non-pixeldrain host would fail/hang.
	if err := NewHostResolver().checkHostThrottle(context.Background(), "catbox", "https://catbox.moe/x.zip"); err != nil {
		t.Fatalf("checkHostThrottle(catbox) = %v, want nil", err)
	}
}

func TestCheckHostThrottle_DetectsHostFromURL(t *testing.T) {
	stubPixeldrainInfo(t, http.StatusOK, `{"download_speed_limit":1048576}`)
	ClearHostThrottle("pixeldrain")
	t.Cleanup(func() { ClearHostThrottle("pixeldrain") })

	// An empty host label must fall back to the resolved URL's host.
	err := NewHostResolver().checkHostThrottle(context.Background(), "", pixeldrainTestURL)
	if !errors.Is(err, ErrHostThrottled) {
		t.Fatalf("err = %v, want ErrHostThrottled", err)
	}
}

func TestScoreLinkHost_ThrottlePenalty(t *testing.T) {
	// A host label outside the tier table (default score 0) isolates the
	// throttle adjustment from the static tier assertions in links_test.go.
	const host = "throttle-test-host"
	ClearHostThrottle(host)
	t.Cleanup(func() { ClearHostThrottle(host) })

	if got := ScoreLinkHost(host); got != 0 {
		t.Fatalf("unthrottled ScoreLinkHost(%q) = %d, want 0", host, got)
	}
	MarkHostThrottled(host, time.Hour)
	if got := ScoreLinkHost(host); got != -throttlePenalty {
		t.Fatalf("throttled ScoreLinkHost(%q) = %d, want %d", host, got, -throttlePenalty)
	}
}

func TestHostThrottle_Expiry(t *testing.T) {
	const host = "expiry-test-host"
	ClearHostThrottle(host)
	MarkHostThrottled(host, 15*time.Millisecond)
	if !HostThrottled(host) {
		t.Fatal("host should be throttled immediately after marking")
	}
	time.Sleep(30 * time.Millisecond)
	if HostThrottled(host) {
		t.Fatal("host throttle must expire")
	}
}
