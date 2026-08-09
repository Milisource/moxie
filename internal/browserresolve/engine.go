package browserresolve

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/go-rod/rod/lib/proto"
)

// pollInterval is how often the download-dir fallback watcher samples the
// temp download directory.
const pollInterval = 500 * time.Millisecond

// browserBinaryCandidates are the executable names searched on PATH when no
// explicit BinPath is given. rod's own discovery covers install locations
// outside PATH (macOS .app bundles, Playwright caches) when none match.
var browserBinaryCandidates = []string{
	"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome",
}

// rodEngine is the real engine: it launches headless Chrome on a copied
// profile, lets the browser perform the download itself, and returns the
// finished file.
type rodEngine struct {
	opts Options
}

func newRodEngine(opts Options) *rodEngine { return &rodEngine{opts: opts} }

// detectBrowserBinary returns the first Chrome-family executable on PATH.
func detectBrowserBinary() (string, error) {
	for _, name := range browserBinaryCandidates {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}

// applyLaunchFlags maps launchFlags output onto rod's launcher. rod's own
// defaults are kept (random --remote-debugging-port, process-group setup,
// leakless teardown), except --enable-automation, which sets
// navigator.webdriver=true and is removed so the session is not flagged as
// automated.
func applyLaunchFlags(l *launcher.Launcher, args []string) {
	for _, arg := range args {
		name, val, hasVal := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		if hasVal {
			l.Set(flags.Flag(name), val)
		} else {
			l.Set(flags.Flag(name))
		}
	}
	l.Delete(flags.Flag("enable-automation"))
}

// toDownloadState maps CDP download-progress state strings (identical in
// the Page.* and Browser.* families) to downloadState.
func toDownloadState(s string) downloadState {
	switch s {
	case "completed":
		return downloadCompleted
	case "canceled":
		return downloadCanceled
	default:
		return downloadInProgress
	}
}

func (e *rodEngine) run(ctx context.Context, req engineRequest) (engineResult, error) {
	bin := e.opts.BinPath
	if bin == "" {
		if p, err := detectBrowserBinary(); err == nil {
			bin = p
		}
	}

	l := launcher.New()
	if bin != "" {
		l.Bin(bin)
	}
	applyLaunchFlags(l, launchFlags(req.profileDir, e.opts.Headful))

	u, err := l.Launch()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return engineResult{}, fmt.Errorf("%w: %v", ErrNoChrome, err)
		}
		return engineResult{}, fmt.Errorf("browserresolve: launching browser: %w", err)
	}
	// Teardown guarantee: kill the browser process tree, then wait for it
	// to exit and remove its user-data-dir (the profile copy). Cleanup
	// without Kill would block forever on a browser that refuses to exit.
	defer func() {
		l.Kill()
		l.Cleanup()
	}()

	// NoDefaultDevice is essential: rod emulates a laptop UA by default,
	// and cf_clearance is cryptographically bound to the UA that solved the
	// challenge. The browser must send its native fingerprint.
	b := rod.New().ControlURL(u).Context(ctx)
	b.NoDefaultDevice()
	if err := b.Connect(); err != nil {
		return engineResult{}, fmt.Errorf("browserresolve: connecting to browser: %w", err)
	}
	defer b.Close()

	// EventsEnabled:true makes Chrome emit downloadWillBegin/downloadProgress
	// so the GUID (and real progress for GB files) is observable.
	if err := (proto.BrowserSetDownloadBehavior{
		Behavior:         proto.BrowserSetDownloadBehaviorBehaviorAllowAndName,
		BrowserContextID: b.BrowserContextID,
		DownloadPath:     req.downloadDir,
		EventsEnabled:    true,
	}).Call(b); err != nil {
		return engineResult{}, fmt.Errorf("browserresolve: setting download behavior: %w", err)
	}

	tr := newDownloadTracker()
	// Subscribe to both event families: Chrome still emits the deprecated
	// Page.* forms alongside the Browser.* ones; whichever arrives first
	// wins (first-wins is enforced inside the tracker).
	wait := b.EachEvent(
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
	)
	evtDone := make(chan struct{})
	go func() {
		defer close(evtDone)
		wait() // returns on finished download, ctx cancel, or browser close
	}()

	// Fallback file watcher: some flows never emit download events
	// (rod#971: target=_blank downloads), yet the file still lands in the
	// download dir. Watch for a non-empty file with a stable size.
	fileCh := make(chan string, 1)
	pollCtx, pollCancel := context.WithCancel(ctx)
	defer pollCancel()
	go pollDownloadDir(pollCtx, req.downloadDir, pollInterval, fileCh)

	if _, err := b.Page(proto.TargetCreateTarget{URL: req.url}); err != nil {
		return engineResult{}, fmt.Errorf("browserresolve: opening download tab: %w", err)
	}

	timeout := e.opts.DownloadTimeout
	if timeout <= 0 {
		timeout = defaultDownloadTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var polled string
	select {
	case <-ctx.Done():
		return engineResult{}, fmt.Errorf("browserresolve: download: %w", ctx.Err())
	case <-timer.C:
		return engineResult{}, fmt.Errorf("%w: no download finished within %s", ErrDownload, timeout)
	case <-evtDone:
	case polled = <-fileCh:
	}

	if ctx.Err() != nil {
		return engineResult{}, fmt.Errorf("browserresolve: download: %w", ctx.Err())
	}

	st := tr.snapshot()
	if st.err != nil {
		return engineResult{}, fmt.Errorf("%w: %v", ErrDownload, st.err)
	}

	// With allowAndName the file is stored under its GUID; prefer it when
	// the tracker completed.
	path := ""
	if st.completed() && st.guid != "" {
		candidate := filepath.Join(req.downloadDir, st.guid)
		if _, err := os.Stat(candidate); err == nil {
			path = candidate
		}
	}
	if path == "" && polled == "" {
		// Events ended (or never came) but the file is not visible yet —
		// give the watcher a few ticks to confirm stability.
		select {
		case polled = <-fileCh:
		case <-ctx.Done():
			return engineResult{}, fmt.Errorf("browserresolve: download: %w", ctx.Err())
		case <-time.After(pollInterval * 3):
		}
	}
	if path == "" {
		path = polled
	}
	if path == "" {
		return engineResult{}, fmt.Errorf("%w: download ended without a file", ErrDownload)
	}

	return engineResult{path: path, suggested: st.suggested, totalBytes: st.total}, nil
}

// pollDownloadDir watches dir for a finished download and sends its path on
// out. A file counts as finished when it is non-empty and was observed in
// two consecutive ticks at the same size. Partial Chrome downloads carry a
// .crdownload suffix and are ignored. This is the fallback for flows that
// never emit CDP download events (rod#971).
func pollDownloadDir(ctx context.Context, dir string, interval time.Duration, out chan<- string) {
	var last string
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		path, next, ok := stableDownloadFile(dir, last)
		if ok {
			select {
			case out <- path:
				return
			case <-ctx.Done():
				return
			}
		}
		last = next
		timer.Reset(interval)
	}
}

// stableDownloadFile reports the first non-empty, non-partial file in dir
// whose name matches the previously observed name (i.e. its size did not
// change between ticks) and returns the newest candidate name for the next
// tick.
func stableDownloadFile(dir, last string) (path, next string, ok bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", last, false
	}
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if strings.HasSuffix(name, ".crdownload") || strings.HasSuffix(name, ".download") {
			continue
		}
		info, err := ent.Info()
		if err != nil || info.Size() <= 0 {
			continue
		}
		if name == last {
			return filepath.Join(dir, name), name, true
		}
		next = name
	}
	return "", next, false
}
