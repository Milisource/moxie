package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"reflect"
	"syscall"
	"testing"

	"github.com/mili/moxie/internal/downloader"
	"github.com/mili/moxie/internal/scraper"
)

func TestUpdateErrorKind(t *testing.T) {
	tests := []struct {
		name string
		err  error
		kind string
	}{
		{"nil", nil, ""},
		{"session", &scraper.SessionError{HadCookie: true}, "auth"},
		{"session sentinel", scraper.ErrSessionExpired, "auth"},
		{"login", downloader.ErrF95LoginRequired, "auth"},
		{"missing cookies", errNoF95Cookies, "auth"},
		{"cookies rejected", downloader.ErrF95CookiesRequired, "auth"},
		{"Cloudflare", &scraper.BlockedError{Kind: scraper.BlockChallenge, StatusCode: http.StatusForbidden}, "challenge"},
		{"stale challenge", downloader.ErrCFChallengeStale, "challenge"},
		{"F95 challenge", downloader.ErrF95Challenge, "challenge"},
		{"browser check", downloader.ErrF95NeedsBrowser, "challenge"},
		{"browser check with fallback disabled", downloader.ErrBrowserCheckRequired, "challenge"},
		{"rate limit", &scraper.BlockedError{Kind: scraper.BlockRateLimit}, "rate_limit"},
		{"blocked 429", &scraper.BlockedError{StatusCode: http.StatusTooManyRequests}, "rate_limit"},
		{"scraper 429", &scraper.HTTPStatusError{StatusCode: http.StatusTooManyRequests}, "rate_limit"},
		{"download 429", &downloader.HTTPStatusError{StatusCode: http.StatusTooManyRequests}, "rate_limit"},
		{"download 404", &downloader.HTTPStatusError{StatusCode: http.StatusNotFound}, "dead_link"},
		{"download 410", &downloader.HTTPStatusError{StatusCode: http.StatusGone}, "dead_link"},
		{"expired F95 link", downloader.ErrF95LinkGone, "dead_link"},
		{"timeout", &net.DNSError{Err: "timeout", IsTimeout: true}, "network"},
		{"dns failure", &net.DNSError{Err: "no such host"}, "network"},
		{"unexpected EOF", io.ErrUnexpectedEOF, "network"},
		{"truncated transfer", downloader.ErrDownloadTruncated, "network"},
		{"refused", syscall.ECONNREFUSED, "network"},
		{"dial failure", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.EADDRNOTAVAIL}, "network"},
		{"reset", syscall.ECONNRESET, "network"},
		{"outage", &scraper.BlockedError{Kind: scraper.BlockUnavailable, StatusCode: http.StatusServiceUnavailable}, "network"},
		{"download 503", &downloader.HTTPStatusError{StatusCode: http.StatusServiceUnavailable}, "network"},
		{"disk full", syscall.ENOSPC, "disk"},
		{"cancelled", context.Canceled, "unknown"},
		{"unknown", errors.New("failure"), "unknown"},
		{"text alone does not classify", errors.New("HTTP 404 needs a browser check"), "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := updateErrorKind(tc.err, "download"); got != tc.kind {
				t.Errorf("kind = %q, want %q", got, tc.kind)
			}
			if tc.err != nil {
				if got := updateErrorKind(fmt.Errorf("outer context: %w", tc.err), "download"); got != tc.kind {
					t.Errorf("wrapped kind = %q, want %q", got, tc.kind)
				}
			}
		})
	}
}

func TestSelectBatchUpdatesEmitsSkipped(t *testing.T) {
	games := []DesktopGameSummary{
		{ID: 1, Title: "Not installed", Path: "/virtual/1", UpdateState: updateAvailable, Version: "1.0", LatestVersion: "2.0"},
		{ID: 2, Title: "No update", Path: "/games/2", UpdateState: updateCurrent, Version: "1.0", LatestVersion: "1.0"},
		{ID: 3, Title: "Unorderable", Path: "/games/3", UpdateState: updateAvailable, Version: "1.0", LatestVersion: "2026-11-13"},
		{ID: 4, Title: "Already updating", Path: "/games/4", UpdateState: updateAvailable, Version: "1.0", LatestVersion: "2.0"},
		{ID: 5, Title: "Ready", Path: "/games/5", UpdateState: updateAvailable, Version: "1.0", LatestVersion: "2.0"},
	}
	var claimed []int64
	var events []map[string]interface{}
	selected := selectBatchUpdates(games, func(g DesktopGameSummary) bool { claimed = append(claimed, g.ID); return g.ID != 4 }, func(name string, data ...interface{}) {
		if name != "game-update:skipped" {
			t.Fatalf("event = %q", name)
		}
		if len(data) != 1 {
			t.Fatalf("payload length = %d", len(data))
		}
		events = append(events, data[0].(map[string]interface{}))
	})
	if !reflect.DeepEqual(claimed, []int64{4, 5}) {
		t.Errorf("claimed = %v", claimed)
	}
	if len(selected) != 1 || selected[0].ID != 5 {
		t.Fatalf("selected = %+v", selected)
	}
	reasons := []string{"not-installed", "no-update", "unorderable-version", "already-updating"}
	if len(events) != len(reasons) {
		t.Fatalf("events = %+v", events)
	}
	for i, reason := range reasons {
		want := map[string]interface{}{"gameID": games[i].ID, "title": games[i].Title, "reason": reason}
		if !reflect.DeepEqual(events[i], want) {
			t.Errorf("event %d = %+v, want %+v", i, events[i], want)
		}
	}
}

// A short read during extraction or merge is a corrupt payload, not a
// connection problem: the hint must not tell the user to check the network.
func TestUpdateErrorKindStepAwareness(t *testing.T) {
	if got := updateErrorKind(io.ErrUnexpectedEOF, "extract"); got != "unknown" {
		t.Errorf("extract unexpected EOF = %q, want unknown", got)
	}
	if got := updateErrorKind(io.ErrUnexpectedEOF, "merge"); got != "unknown" {
		t.Errorf("merge unexpected EOF = %q, want unknown", got)
	}
	if got := updateErrorKind(io.ErrUnexpectedEOF, "download"); got != "network" {
		t.Errorf("download unexpected EOF = %q, want network", got)
	}
}

func TestSelectBatchUpdatesUnknownVersionReason(t *testing.T) {
	games := []DesktopGameSummary{
		{ID: 1, Title: "Unknown version", Path: "/games/1", UpdateState: updateUnknown, Version: "", LatestVersion: "1.0"},
	}
	var events []map[string]interface{}
	selectBatchUpdates(games, func(DesktopGameSummary) bool { return true }, func(name string, data ...interface{}) {
		events = append(events, data[0].(map[string]interface{}))
	})
	if len(events) != 1 || events[0]["reason"] != "unknown-version" {
		t.Fatalf("events = %+v", events)
	}
}
