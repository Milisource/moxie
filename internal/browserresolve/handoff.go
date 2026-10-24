package browserresolve

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/mili/moxie/internal/downloader"
	"github.com/mili/moxie/internal/log"
)

// Handoff timings for files a human-assisted browser session downloads
// itself. The visible window is the user's own session: once the check is
// passed they can press the host's download button, so the window must not be
// torn down the moment the destination URL is known.
const (
	// browserHandoffGrace is how long the session waits for the page's own
	// download to begin after the destination page is reached.
	browserHandoffGrace = 15 * time.Second
	// browserHandoffPoll is the sampling interval for download activity.
	browserHandoffPoll = 400 * time.Millisecond
)

// browserHandoffDownloadTimeout bounds the wait for a browser download to
// finish once it has started. Generous: the visible window is the user's own
// download and game archives are multi-GB. Overridable for tests.
var browserHandoffDownloadTimeout = 20 * time.Minute

// errBrowserDownloadStarted is the click flows' early exit when the visible
// page started downloading the file itself (the tab URL may never change for
// a download, so waiting for a destination URL would stall for minutes and
// then kill the window mid-transfer).
var errBrowserDownloadStarted = errors.New("browserresolve: browser download started")

// handoffTracker returns the session tracker for a human-assisted flow and
// nil for the headless (URL-only) flow, so auto sessions keep their previous
// behavior.
func handoffTracker(headful bool, tr *downloadTracker) *downloadTracker {
	if headful {
		return tr
	}
	return nil
}

// startDownloadTracking points the browser's downloads at dir and returns a
// tracker fed by CDP download events. Errors are non-fatal: a session that
// cannot set download behavior still resolves URLs the normal way.
func startDownloadTracking(b *rod.Browser, dir string) *downloadTracker {
	tr := newDownloadTracker()
	if err := (proto.BrowserSetDownloadBehavior{
		Behavior:         proto.BrowserSetDownloadBehaviorBehaviorAllowAndName,
		BrowserContextID: b.BrowserContextID,
		DownloadPath:     dir,
		EventsEnabled:    true,
	}).Call(b); err != nil {
		log.Debug("browserresolve: setting download behavior failed", "error", err)
		return tr
	}
	// Subscribe to both event families: Chrome still emits the deprecated
	// Page.* forms alongside the Browser.* ones; whichever arrives first
	// wins (first-wins is enforced inside the tracker).
	go b.EachEvent(
		func(ev *proto.PageDownloadWillBegin) bool {
			tr.onWillBegin(ev.GUID, ev.SuggestedFilename)
			return tr.finished()
		},
		func(ev *proto.PageDownloadProgress) bool {
			tr.onProgress(ev.GUID, toDownloadState(string(ev.State)), ev.ReceivedBytes, ev.TotalBytes)
			return tr.finished()
		},
		func(ev *proto.BrowserDownloadWillBegin) bool {
			tr.onWillBegin(ev.GUID, ev.SuggestedFilename)
			return tr.finished()
		},
		func(ev *proto.BrowserDownloadProgress) bool {
			tr.onProgress(ev.GUID, toDownloadState(string(ev.State)), ev.ReceivedBytes, ev.TotalBytes)
			return tr.finished()
		},
	)()
	return tr
}

// awaitBrowserDownload blocks until the session's browser download finishes
// (returns the file inside dir) or the grace/timeout budgets expire. grace
// bounds the wait for a download to begin: a page that never starts one keeps
// the session on the normal URL-return path.
func awaitBrowserDownload(ctx context.Context, tr *downloadTracker, dir string, grace, timeout time.Duration) (path, name string, ok bool) {
	// Phase 1: wait for the download to start.
	deadline := time.Now().Add(grace)
	var last string
	for {
		if tr.started() {
			break
		}
		if p, next, found := stableDownloadFile(dir, last); found {
			// Event-less flows (rod#971) still land the finished file.
			return p, tr.snapshot().suggested, true
		} else {
			last = next
		}
		if time.Now().After(deadline) {
			return "", "", false
		}
		if !sleepCtx(ctx, browserHandoffPoll) {
			return "", "", false
		}
	}
	log.Info("browserresolve: the browser window is downloading the file; waiting for it to finish")

	// Phase 2: wait for the file to finish landing.
	deadline = time.Now().Add(timeout)
	for {
		snap := tr.snapshot()
		if snap.state == downloadCanceled {
			log.Warn("browserresolve: browser download was canceled", "error", snap.err)
			return "", "", false
		}
		if snap.completed() && snap.guid != "" {
			candidate := filepath.Join(dir, snap.guid)
			if fi, err := os.Stat(candidate); err == nil && fi.Size() > 0 {
				return candidate, snap.suggested, true
			}
		}
		if p, next, found := stableDownloadFile(dir, last); found {
			return p, snap.suggested, true
		} else {
			last = next
		}
		if time.Now().After(deadline) {
			return "", "", false
		}
		if !sleepCtx(ctx, browserHandoffPoll) {
			return "", "", false
		}
	}
}

// browserFileHandoff drives a human-assisted session to its own download and
// hands the finished file to the downloader. It reports false (and the caller
// falls back to returning the destination URL) when the page never starts a
// download or the file fails verification.
func browserFileHandoff(ctx context.Context, page *rod.Page, tr *downloadTracker, dlDir string, opts Options) (string, bool) {
	// The destination page may itself be a host check (mediafire's
	// "?action=check", a free-download form): drive it so one human pass is
	// enough. Bounded — a page without a trigger costs at most this budget.
	clickCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	clickDownloadTriggers(clickCtx, page, tr)
	cancel()

	path, name, ok := awaitBrowserDownload(ctx, tr, dlDir, browserHandoffGrace, browserHandoffDownloadTimeout)
	if !ok {
		return "", false
	}
	if err := verifyDownloadedFile(path, opts.MinSize, 0, name); err != nil {
		log.Warn("browserresolve: browser download failed verification", "error", err)
		return "", false
	}
	token, err := handOffBrowserFile(path, name)
	if err != nil {
		log.Warn("browserresolve: handing over the browser download failed", "error", err)
		return "", false
	}
	return token, true
}

// handOffBrowserFile moves a finished browser download into a dedicated temp
// dir and registers it with the downloader, returning the token URL the
// solver should return instead of a destination URL. The downloader copies the
// file into the game's download dir and removes the temp dir.
func handOffBrowserFile(path, name string) (string, error) {
	dir, err := browserHandoffDir()
	if err != nil {
		return "", err
	}
	dest := filepath.Join(dir, filepath.Base(path))
	if err := os.Rename(path, dest); err != nil {
		// Cross-device temp dirs (or Windows sharing violations).
		if err := copyFile(path, dest); err != nil {
			_ = os.RemoveAll(dir)
			return "", err
		}
	}
	log.Info("browserresolve: browser finished the download; handing the file to the installer", "file", name, "size_bytes", fileSize(dest))
	return downloader.RegisterBrowserFile(dest, name), nil
}

// browserHandoffDir creates the temp dir that carries a handed-off file to
// the downloader. Overridable so tests can clean up.
var browserHandoffDir = func() (string, error) {
	return os.MkdirTemp("", "moxie-browser-handoff-*")
}

// fileSize returns a file's size, or 0 when it cannot be read (diagnostics).
func fileSize(path string) int64 {
	if fi, err := os.Stat(path); err == nil {
		return fi.Size()
	}
	return 0
}

// sleepCtx pauses for d and reports false when the context ended first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
