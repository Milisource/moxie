package browserresolve

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/stealth"
	"github.com/mili/moxie/internal/log"
)

// pollInterval is how often the download-dir fallback watcher samples the
// temp download directory.
const pollInterval = 500 * time.Millisecond

// browserBinaryCandidates are the executable names searched on PATH when no
// explicit BinPath is given. rod's own discovery covers install locations
// outside PATH (macOS .app bundles, Playwright caches) when none match.
var browserBinaryCandidates = []string{
	"google-chrome", "google-chrome-stable", "chromium", "chromium-browser",
	"microsoft-edge", "microsoft-edge-stable", "brave-browser", "brave", "chrome",
}

// rodEngine is the real engine: it launches headless Chrome on a copied
// profile, lets the browser perform the download itself, and returns the
// finished file.
type rodEngine struct {
	opts Options
}

func newRodEngine(opts Options) *rodEngine { return &rodEngine{opts: opts} }

// profileDir locates the Chrome-family profile root ("User Data" dir).
func (e *rodEngine) profileDir(override string) (string, error) {
	return discoverProfileDir(override)
}

// detectBrowserBinary returns the first Chrome-family executable on PATH,
// in a per-OS standard install location, or in the Playwright cache
// (rod's own launcher discovery covers the same ground at launch time;
// this is the cheap selection-time check).
func detectBrowserBinary() (string, error) {
	for _, name := range browserBinaryCandidates {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	switch runtime.GOOS {
	case "darwin":
		for _, app := range []string{"Google Chrome.app", "Microsoft Edge.app", "Brave Browser.app"} {
			for _, base := range []string{"/Applications", filepath.Join(home, "Applications")} {
				candidate := filepath.Join(base, app, "Contents", "MacOS", app[:len(app)-4])
				if _, err := os.Stat(candidate); err == nil {
					return candidate, nil
				}
			}
		}
	case "windows":
		for _, root := range []string{
			os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LOCALAPPDATA"),
		} {
			if root == "" {
				continue
			}
			for _, app := range []string{
				filepath.Join("Google", "Chrome", "Application", "chrome.exe"),
				filepath.Join("Microsoft", "Edge", "Application", "msedge.exe"),
				filepath.Join("BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
			} {
				candidate := filepath.Join(root, app)
				if _, err := os.Stat(candidate); err == nil {
					return candidate, nil
				}
			}
		}
	}
	// Playwright-cached Chromium builds (dev machines, CI, playwright users).
	if home != "" {
		for _, pattern := range []string{
			filepath.Join(home, ".cache", "ms-playwright", "chromium-*", "chrome-linux64", "chrome"),
			filepath.Join(home, ".cache", "ms-playwright", "chromium-*", "chrome-linux", "chrome"),
			filepath.Join(home, "Library", "Caches", "ms-playwright", "chromium-*", "chrome-mac", "Chromium.app", "Contents", "MacOS", "Chromium"),
		} {
			if matches, _ := filepath.Glob(pattern); len(matches) > 0 {
				return matches[0], nil
			}
		}
	}
	return "", exec.ErrNotFound
}

// applyLaunchFlags maps launchFlags output onto rod's launcher. rod's own
// defaults are kept (random --remote-debugging-port, process-group setup,
// leakless teardown), except --enable-automation, which sets
// navigator.webdriver=true and is removed so the session is not flagged as
// automated.
//
// NOTE: navigator.webdriver is NOT neutralized via
// --disable-blink-features=AutomationControlled here — live-verified
// 2026-08-09 that the flag crashes Playwright-cached Chromium builds when
// the copied profile carries conflicting blink-feature preferences (e.g.
// Brave profiles core-dump at startup). The webdriver signal is patched
// instead via script injection in open() (EvalOnNewDocument), which has
// the same effect without the launch flag.
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

// open launches the browser session for a request: binary discovery,
// profile copy (launchFlags), rod connection, cookie injection, and the
// target tab. The returned cleanup kills the browser process tree and
// removes the profile copy.
func (e *rodEngine) open(ctx context.Context, req engineRequest) (b *rod.Browser, page *rod.Page, cleanup func(), err error) {
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
			return nil, nil, nil, fmt.Errorf("%w: %v", ErrNoChrome, err)
		}
		return nil, nil, nil, fmt.Errorf("browserresolve: launching browser: %w", err)
	}
	// Teardown guarantee: kill the browser process tree, then wait for it
	// to exit and remove its user-data-dir (the profile copy). Cleanup
	// without Kill would block forever on a browser that refuses to exit.
	cleanup = func() { l.Kill(); l.Cleanup() }

	// NoDefaultDevice is essential: rod emulates a laptop UA by default,
	// and cf_clearance is cryptographically bound to the UA that solved the
	// challenge. The browser must send its native fingerprint.
	b = rod.New().ControlURL(u).Context(ctx)
	b.NoDefaultDevice()
	if err := b.Connect(); err != nil {
		cleanup()
		return nil, nil, nil, fmt.Errorf("browserresolve: connecting to browser: %w", err)
	}
	baseCleanup := cleanup
	cleanup = func() { b.Close(); baseCleanup() }

	// Seed the session with the user's cookies for the target host (from
	// every browser store) before any navigation.
	injectCookiesForURL(b, req.url)

	// Blank page first, then patch the automation fingerprints via the
	// go-rod/stealth evasion bundle and navigate. (A launch flag would be
	// cleaner, but --disable-blink-features=AutomationControlled crashes
	// Playwright Chromium builds on profiles with conflicting blink prefs
	// — Brave core-dumps at startup. The JS bundle is crash-proof and
	// live-verified 2026-08-09 on bot.sannysoft.com: 31/31 checks pass
	// vs 29/31 baseline. Its en-US/en languages hardcode suits F95Zone
	// (English site) — the rod#1208 locale breakage affects non-English
	// locales.)
	page, err = b.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		cleanup()
		return nil, nil, nil, fmt.Errorf("browserresolve: creating tab: %w", err)
	}
	remove, err := page.EvalOnNewDocument(stealth.JS)
	if err != nil {
		log.Debug("browserresolve: stealth injection failed", "error", err)
	} else {
		defer remove()
	}
	if err := page.Navigate(req.url); err != nil {
		cleanup()
		return nil, nil, nil, fmt.Errorf("browserresolve: opening tab: %w", err)
	}
	return b, page, cleanup, nil
}

// resolveMasked drives the F95Zone masked download interstitial in the
// browser and returns the real destination URL once the page navigates
// away from f95zone.to. Nothing is downloaded — the Go path fetches the
// destination with progress/resume.
func (e *rodEngine) resolveMasked(ctx context.Context, req engineRequest) (string, error) {
	b, page, cleanup, err := e.open(ctx, req)
	if err != nil {
		return "", err
	}
	defer cleanup()

	// Insurance against destinations that auto-start downloads: they land
	// in a temp dir instead of the user's default download folder.
	if err := (proto.BrowserSetDownloadBehavior{
		Behavior:         proto.BrowserSetDownloadBehaviorBehaviorAllowAndName,
		BrowserContextID: b.BrowserContextID,
		DownloadPath:     os.TempDir(),
	}).Call(b); err != nil {
		log.Debug("browserresolve: masked flow download behavior failed", "error", err)
	}

	dest, err := clickMaskedFlow(ctx, page, req.url)
	if err != nil {
		return "", fmt.Errorf("browserresolve: masked unwrap in browser: %w", err)
	}
	log.Info("browserresolve: masked unwrap in browser succeeded", "dest", dest)
	return dest, nil
}

func (e *rodEngine) run(ctx context.Context, req engineRequest) (engineResult, error) {
	b, page, cleanup, err := e.open(ctx, req)
	if err != nil {
		return engineResult{}, err
	}
	defer cleanup()

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

	// Click-required flows (F95Zone masked "Continue", free-download
	// buttons): drive the chain until a download starts. Auto-download
	// pages bail out via the tracker within one selector timeout.
	clickDownloadTriggers(ctx, page, tr)

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
		if strings.HasSuffix(name, ".crdownload") || strings.HasSuffix(name, ".download") || strings.HasSuffix(name, ".part") {
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
