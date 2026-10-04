package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"syscall"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/downloader"
)

// This file implements the desktop update pipeline's "dead on failure"
// policy: after a link has exhausted its retries with a *link-specific*
// terminal error, the link is persisted as dead so it drops out of link
// ranking instead of being retried on every future batch.
//
// The distinction that matters is link-specific vs host-wide. A 404 for one
// file means that mirror no longer serves it; a timeout talking to
// api.gofile.io means the *host* is unreachable and every gofile link would
// be wrongly killed if it were marked dead. deadLinkReason encodes that
// split so it can be unit-tested without a live download.

// deadLinkMarkers are case-insensitive error substrings that identify a
// link-specific terminal failure: the host answered for this exact file with
// "gone" / "no longer exists". Both the resolver stage (e.g.
// "datanodes: GET returned HTTP 404") and the raw download stage
// ("HTTP 404") surface this text.
var deadLinkMarkers = []string{
	"http 404",
	"http 410",
}

// deadLinkChallengeMarkers mirror downloader.challengeMarkers: a
// challenge/browser-fallback failure is a host-wide bot wall, not proof the
// file is gone, so it must never mark a link dead.
var deadLinkChallengeMarkers = []string{
	"captcha",
	"challenge",
	"cloudflare",
	"turnstile",
	"browser fallback also failed",
}

// deadLinkReason classifies a terminal download error. It returns dead=true
// with a short human-readable reason only when the failure is specific to
// the link (the mirror no longer serves that file), and dead=false for
// host-wide or transient failures so a temporary outage never discards a
// live mirror.
//
// Callers must only pass the *final* error after a link's retries are
// exhausted; a first-attempt transient error is not terminal.
func deadLinkReason(err error) (string, bool) {
	if err == nil {
		return "", false
	}

	// Host-wide / transient sentinels: never link-specific. errors.Is is
	// preferred wherever a wrapped sentinel exists.
	if errors.Is(err, errNoF95Cookies) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, downloader.ErrHostThrottled) ||
		errors.Is(err, downloader.ErrCFChallengeStale) ||
		errors.Is(err, downloader.ErrMegaNeedsMegatools) {
		return "", false
	}
	// Timeouts and resets are retryable by nature.
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "", false
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET) {
		return "", false
	}

	msg := strings.TrimSpace(err.Error())
	lower := strings.ToLower(msg)

	// Challenge / browser-fallback failures are bot walls: the file may be
	// perfectly alive behind them.
	for _, m := range deadLinkChallengeMarkers {
		if strings.Contains(lower, m) {
			return "", false
		}
	}
	// Generic 5xx is a server-side outage; retry on a later batch.
	if strings.Contains(lower, "http 5") {
		return "", false
	}

	for _, m := range deadLinkMarkers {
		if strings.Contains(lower, m) {
			return fmt.Sprintf("terminal download failure (%s): %s", m, truncateDeadReason(msg)), true
		}
	}
	return "", false
}

// maxDeadReasonLen bounds the persisted dead_reason so one long error cannot
// bloat the row.
const maxDeadReasonLen = 300

// truncateDeadReason clips s to maxDeadReasonLen runes (rune-safe so it never
// splits a multi-byte character).
func truncateDeadReason(s string) string {
	r := []rune(s)
	if len(r) <= maxDeadReasonLen {
		return s
	}
	return string(r[:maxDeadReasonLen]) + "…"
}

// markDownloadLinkDeadIfTerminal persists link as dead when lastErr is a
// link-specific terminal failure (see deadLinkReason). It is a no-op for
// transient/host-wide errors or when the app has no database. It never
// changes the caller's error handling — the update pipeline still returns
// lastErr to its caller either way.
func (a *App) markDownloadLinkDeadIfTerminal(link db.DownloadLink, lastErr error) {
	if a.db == nil || lastErr == nil {
		return
	}
	reason, dead := deadLinkReason(lastErr)
	if !dead {
		return
	}
	if err := a.db.MarkDownloadLinkDead(link.ID, reason); err != nil {
		slog.Warn("failed to mark download link dead",
			"linkID", link.ID, "host", link.Host, "error", err)
		return
	}
	slog.Info("marked download link dead",
		"linkID", link.ID, "host", link.Host, "reason", reason)
}
