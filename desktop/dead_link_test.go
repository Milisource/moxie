package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"syscall"
	"testing"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/downloader"
)

// TestDeadLinkReason pins the link-specific vs host-wide/transient split:
// only a terminal "the host says this exact file is gone" signal may mark a
// link dead. Every transient/host-wide case would otherwise poison live
// mirrors across a whole batch.
func TestDeadLinkReason(t *testing.T) {
	cases := []struct {
		name string
		err  error
		dead bool
	}{
		{"nil", nil, false},

		// Link-specific terminal signals → dead.
		{"datanodes 404", errors.New("resolve datanodes URL: datanodes: GET returned HTTP 404"), true},
		{"datanodes 404 wrapped by pipeline",
			fmt.Errorf("download after 1 attempts: %w",
				errors.New("resolve datanodes URL: datanodes: GET returned HTTP 404")), true},
		{"vikingfile 404", errors.New("resolve vikingfile URL: vikingfile: GET returned HTTP 404"), true},
		{"raw download 404", errors.New("HTTP 404"), true},
		{"raw download 410", errors.New("HTTP 410"), true},
		{"mixed-case 404", errors.New("Host Returned Http 404"), true},

		// Host-wide / transient signals → not dead.
		{"gofile host down",
			fmt.Errorf("resolve gofile URL: %w",
				fmt.Errorf("gofile create account: %w", context.DeadlineExceeded)), false},
		{"gofile 5xx", errors.New("gofile create account: HTTP 503: upstream unavailable"), false},
		{"generic 500", errors.New("host returned HTTP 500"), false},
		{"generic 502", errors.New("HTTP 502"), false},
		{"throttled", fmt.Errorf("resolve pixeldrain URL: %w", downloader.ErrHostThrottled), false},
		{"pixeldrain quota cap", fmt.Errorf("host health: %w", downloader.ErrHostThrottled), false},
		{"context canceled", context.Canceled, false},
		{"context deadline", context.DeadlineExceeded, false},
		{"net timeout", fakeNetErr{timeout: true}, false},
		{"wrapped net timeout", fmt.Errorf("download: %w", fakeNetErr{timeout: true}), false},
		{"no f95 cookies", fmt.Errorf("prepare: %w", errNoF95Cookies), false},
		{"cf challenge", fmt.Errorf("download rejected: %w", downloader.ErrCFChallengeStale), false},
		{"mega needs megatools", fmt.Errorf("resolve mega URL: %w", downloader.ErrMegaNeedsMegatools), false},
		{"unexpected eof", fmt.Errorf("read: %w", io.ErrUnexpectedEOF), false},
		{"conn reset", fmt.Errorf("read: %w", syscall.ECONNRESET), false},
		{"browser fallback failed",
			fmt.Errorf("resolve datanodes URL: %w (browser fallback also failed: %v)",
				errors.New("challenge detected"), errors.New("chrome exited")), false},
		{"unclassified error", errors.New("bad archive"), false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reason, dead := deadLinkReason(c.err)
			if dead != c.dead {
				t.Fatalf("deadLinkReason(%v) = (%q, %v), want dead=%v", c.err, reason, dead, c.dead)
			}
			if dead && reason == "" {
				t.Error("dead=true must carry a non-empty reason")
			}
			if !dead && reason != "" {
				t.Errorf("dead=false must carry an empty reason, got %q", reason)
			}
		})
	}
}

func TestTruncateDeadReasonIsRuneSafe(t *testing.T) {
	long := strings.Repeat("é", maxDeadReasonLen+10)
	got := truncateDeadReason(long)
	if []rune(got)[0] != 'é' {
		t.Fatalf("truncateDeadReason mangled the start: %q", got)
	}
	// 1 trailing ellipsis rune beyond the bound.
	if n := len([]rune(got)); n != maxDeadReasonLen+1 {
		t.Errorf("truncated length = %d runes, want %d", n, maxDeadReasonLen+1)
	}
	if truncateDeadReason("short") != "short" {
		t.Errorf("short reason should pass through unchanged")
	}
}

func newTestDownloadLink(t *testing.T, a *App, gameID int64, host, url string) int64 {
	t.Helper()
	id, err := a.db.CreateDownloadLink(&db.DownloadLink{
		GameID:   gameID,
		URL:      url,
		Host:     host,
		Name:     "DOWNLOAD",
		Platform: db.PlatformAll,
	})
	if err != nil {
		t.Fatalf("CreateDownloadLink(%s): %v", host, err)
	}
	return id
}

func TestMarkDownloadLinkDeadIfTerminalMarks404(t *testing.T) {
	a := newTestApp(t)
	gameID := addGame(t, a, "Game", t.TempDir())
	id := newTestDownloadLink(t, a, gameID, "datanodes", "https://datanodes.to/abc")

	err := errors.New("download after 1 attempts: resolve datanodes URL: datanodes: GET returned HTTP 404")
	a.markDownloadLinkDeadIfTerminal(db.DownloadLink{ID: id, Host: "datanodes"}, err)

	got, err := a.db.GetDownloadLink(id)
	if err != nil {
		t.Fatalf("GetDownloadLink: %v", err)
	}
	if got == nil || !got.IsDead {
		t.Fatalf("link not marked dead: %+v", got)
	}
	if got.DeadReason == "" {
		t.Error("dead link must carry a dead_reason")
	}
}

func TestMarkDownloadLinkDeadIfTerminalSkipsTransient(t *testing.T) {
	a := newTestApp(t)
	gameID := addGame(t, a, "Game", t.TempDir())
	id := newTestDownloadLink(t, a, gameID, "gofile", "https://gofile.io/d/abc")

	err := fmt.Errorf("download after 3 attempts: %w",
		fmt.Errorf("resolve gofile URL: %w",
			fmt.Errorf("gofile create account: %w", context.DeadlineExceeded)))
	a.markDownloadLinkDeadIfTerminal(db.DownloadLink{ID: id, Host: "gofile"}, err)

	got, err := a.db.GetDownloadLink(id)
	if err != nil {
		t.Fatalf("GetDownloadLink: %v", err)
	}
	if got == nil || got.IsDead {
		t.Fatalf("transient host-wide failure must not mark the link dead: %+v", got)
	}
}

// TestDeadLinkExcludedFromRanking is the end-to-end contract: once a datanodes
// link is marked dead by the terminal-404 policy, the next ranking pass must
// drop it and keep the live mirror.
func TestDeadLinkExcludedFromRanking(t *testing.T) {
	a := newTestApp(t)
	gameID := addGame(t, a, "Game", t.TempDir())

	deadID := newTestDownloadLink(t, a, gameID, "datanodes", "https://datanodes.to/abc")
	liveID := newTestDownloadLink(t, a, gameID, "pixeldrain", "https://pixeldrain.com/u/abc")

	before, err := a.rankedDownloadLinks(gameID, rankOpts{})
	if err != nil {
		t.Fatalf("rankedDownloadLinks (before): %v", err)
	}
	if !containsLinkID(before, deadID) || !containsLinkID(before, liveID) {
		t.Fatalf("expected both links eligible before marking dead; got %v", linkIDs(before))
	}

	a.markDownloadLinkDeadIfTerminal(db.DownloadLink{ID: deadID, Host: "datanodes"},
		errors.New("resolve datanodes URL: datanodes: GET returned HTTP 404"))

	after, err := a.rankedDownloadLinks(gameID, rankOpts{})
	if err != nil {
		t.Fatalf("rankedDownloadLinks (after): %v", err)
	}
	if containsLinkID(after, deadID) {
		t.Errorf("dead link %d still present in ranked set %v", deadID, linkIDs(after))
	}
	if !containsLinkID(after, liveID) {
		t.Errorf("live link %d disappeared from ranked set %v", liveID, linkIDs(after))
	}

	// The exclusion must come from persisted state, not an in-memory flag.
	row, err := a.db.GetDownloadLink(deadID)
	if err != nil || row == nil || !row.IsDead {
		t.Fatalf("dead link not persisted: %+v (err %v)", row, err)
	}
}

func containsLinkID(links []db.DownloadLink, id int64) bool {
	for _, l := range links {
		if l.ID == id {
			return true
		}
	}
	return false
}

func linkIDs(links []db.DownloadLink) []int64 {
	ids := make([]int64, len(links))
	for i, l := range links {
		ids[i] = l.ID
	}
	return ids
}
