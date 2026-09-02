package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/mili/moxie/internal/commands"
	"github.com/mili/moxie/internal/scanner"
)

// watchDebounce is the quiet period after a filesystem event before an
// incremental rescan of the affected scan root is triggered. Coalesces the
// many events a single file operation produces.
const watchDebounce = 750 * time.Millisecond

// watchSweepInterval is how often the debounce map is checked for due scans.
const watchSweepInterval = 500 * time.Millisecond

// watchStopGrace bounds how long Stop waits for in-flight goroutines before
// giving up (a scan stuck on a slow or network-mounted path must not hang
// application shutdown).
const watchStopGrace = 5 * time.Second

// pendingScan records the debounce state for one scan root: when the last
// event arrived, and which event paths were coalesced during the window.
// The paths let the rescan exempt freshly-touched game directories from the
// mtime skip set (an in-place file write does not change the directory
// mtime, so without the exemption the very directory the event was about
// would be skipped).
type pendingScan struct {
	last  time.Time
	paths []string
}

// DirectoryWatcher watches all configured scan paths for filesystem changes
// and triggers incremental rescans so the library stays in sync without
// manual intervention.
type DirectoryWatcher struct {
	app      *App
	watcher  *fsnotify.Watcher
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
	roots    []string // absolute, cleaned scan roots — snapshot taken at Start
	mu       sync.Mutex
	pending  map[string]*pendingScan // scan root -> debounce state
	ctx      context.Context         // cancelled by Stop; aborts in-flight scans
	cancel   context.CancelFunc
}

// NewDirectoryWatcher creates a watcher bound to the given App.
func NewDirectoryWatcher(a *App) *DirectoryWatcher {
	return &DirectoryWatcher{
		app:     a,
		stopCh:  make(chan struct{}),
		pending: make(map[string]*pendingScan),
	}
}

// Start begins watching the given scan paths. It returns an error if the
// underlying fsnotify watcher cannot be created. A scan path that does not
// exist is logged and skipped rather than silently dropped: restartWatcher
// recreates the watcher and re-stats every path, so the watch comes back
// once the path appears (e.g. a drive that was not mounted at startup).
func (w *DirectoryWatcher) Start(paths []string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	w.watcher = watcher
	w.ctx, w.cancel = context.WithCancel(context.Background())

	// Snapshot the roots in absolute, cleaned form. Events arrive as absolute
	// paths, so the roots we match them against must be absolute too —
	// otherwise filepath.Rel errors and every event is silently dropped.
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			slog.Warn("watch: could not resolve scan path", "path", p, "error", err)
			continue
		}
		if _, serr := os.Stat(abs); serr != nil {
			slog.Warn("watch: scan path missing; will retry on restart", "path", abs, "error", serr)
			continue
		}
		w.roots = append(w.roots, abs)
		if err := w.addRecursive(abs); err != nil {
			slog.Warn("watch: could not watch scan path", "path", abs, "error", err)
		}
	}

	w.wg.Add(2)
	go w.eventLoop()
	go w.sweepLoop()
	return nil
}

// Stop stops watching, waits for any in-flight scan to finish, and releases
// the underlying resources. It is safe to call concurrently and more than once.
func (w *DirectoryWatcher) Stop() error {
	var err error
	w.stopOnce.Do(func() {
		close(w.stopCh)
		// Cancel any in-flight scan so a stuck walk (slow/network-mounted
		// path) does not hang shutdown.
		if w.cancel != nil {
			w.cancel()
		}
		// Join the loops before closing the fsnotify watcher so an in-flight
		// RescanDirectory finishes its database writes first — the caller
		// closes the database right after this returns.
		done := make(chan struct{})
		go func() {
			w.wg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(watchStopGrace):
			slog.Warn("watcher goroutines did not stop within grace period; proceeding",
				"grace", watchStopGrace)
		}
		if w.watcher != nil {
			err = w.watcher.Close()
		}
	})
	return err
}

// addRecursive registers an inotify watcher for root and every subdirectory
// under it (inotify does not watch recursively).
func (w *DirectoryWatcher) addRecursive(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		// Log and keep walking rather than aborting the subtree — a single
		// failure (typically fs.inotify.max_user_watches) must not leave the
		// rest of the library silently unwatched.
		if aerr := w.watcher.Add(path); aerr != nil {
			slog.Warn("watch: could not add directory", "path", path, "error", aerr)
		}
		return nil
	})
}

func (w *DirectoryWatcher) eventLoop() {
	defer w.wg.Done()
	for {
		select {
		case <-w.stopCh:
			return
		case ev, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			// A new directory needs a watcher registered for its subtree.
			// IN_MOVED_TO arrives as Rename (not Create) on Linux, so a
			// Rename whose target is a directory is treated the same as a
			// Create — a directory moved into a scan root would otherwise get
			// no subtree watches and every later change inside it would be
			// missed. The os.Stat check also filters IN_MOVED_FROM (the old
			// name is gone, so Stat fails) and plain file renames.
			if ev.Op&(fsnotify.Create|fsnotify.Rename) != 0 {
				if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
					if err := w.addRecursive(ev.Name); err != nil {
						slog.Warn("watch: could not watch new directory", "path", ev.Name, "error", err)
					}
				}
			}
			w.queueScan(ev.Name)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			slog.Warn("watch error", "error", err)
		}
	}
}

// queueScan records that the scan root containing path changed, deferring the
// actual rescan until the debounce window passes. The event path itself is
// remembered so the rescan can exempt freshly-touched directories from the
// mtime skip set.
func (w *DirectoryWatcher) queueScan(path string) {
	root := w.matchRoot(path)
	if root == "" {
		return
	}
	w.mu.Lock()
	ps, ok := w.pending[root]
	if !ok {
		ps = &pendingScan{}
		w.pending[root] = ps
	}
	ps.last = time.Now()
	if !slices.Contains(ps.paths, path) {
		ps.paths = append(ps.paths, path)
	}
	w.mu.Unlock()
}

// sweepLoop periodically fires due rescans after the debounce window.
func (w *DirectoryWatcher) sweepLoop() {
	defer w.wg.Done()
	ticker := time.NewTicker(watchSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-w.stopCh:
			return
		case now := <-ticker.C:
			var due []string
			triggers := make(map[string][]string)
			w.mu.Lock()
			for root, ps := range w.pending {
				if now.Sub(ps.last) >= watchDebounce {
					due = append(due, root)
					triggers[root] = ps.paths
					delete(w.pending, root)
				}
			}
			w.mu.Unlock()
			for _, root := range due {
				if !w.app.RescanDirectory(w.ctx, root, triggers[root]) {
					// The rescan was skipped because a manual scan holds the
					// lock. Re-queue the root so the change is not dropped
					// forever — the next sweep retries it.
					w.mu.Lock()
					if ps, ok := w.pending[root]; ok {
						ps.last = time.Now()
						ps.paths = append(ps.paths, triggers[root]...)
					} else {
						w.pending[root] = &pendingScan{last: time.Now(), paths: triggers[root]}
					}
					w.mu.Unlock()
				}
			}
		}
	}
}

// emitRuntime emits a Wails runtime event. Guarded: before the Wails runtime
// context exists (startup, tests) events are dropped instead of the runtime
// fatally exiting the process.
func (a *App) emitRuntime(name string, data interface{}) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, name, data)
}

// RescanDirectory runs an incremental scan of root, upserts detected games,
// soft-deletes games whose directories vanished, and emits Wails events so
// the frontend can refresh. It is safe to call from any goroutine. When a
// manual scan is already running the sweep is skipped — the manual run
// covers the same upsert path — and RescanDirectory returns false so the
// caller can re-queue the root. triggerPaths are the filesystem event paths
// that caused this scan; game directories they touch are exempted from the
// mtime skip set (an in-place file write does not change the directory
// mtime, so the skip would otherwise hide the very change that triggered the
// scan). ctx cancels the underlying walk; a cancelled scan returns without
// emitting error events (shutdown in progress).
func (a *App) RescanDirectory(ctx context.Context, root string, triggerPaths []string) bool {
	if a.db == nil {
		return false
	}
	if !a.scanRunning.CompareAndSwap(false, true) {
		slog.Debug("auto-scan skipped: manual scan in progress", "root", root)
		return false
	}
	defer a.scanRunning.Store(false)

	abs, err := filepath.Abs(root)
	if err != nil {
		slog.Warn("auto-scan skipped: cannot resolve root", "root", root, "error", err)
		return true
	}

	// This runs on the watcher's sweep goroutine — a panic in the scanner or
	// database layer would otherwise take down the whole desktop app.
	defer func() {
		if r := recover(); r != nil {
			slog.Error("auto-scan panic", "root", abs, "recover", r)
			a.emitRuntime("scan:auto-error", map[string]string{
				"error": fmt.Sprintf("internal error: %v", r),
			})
		}
	}()

	a.emitRuntime("scan:auto", "started")

	// Build the incremental skip set: known games under this root whose
	// directory mtime is unchanged are skipped for speed. Directories touched
	// by the triggering events are exempt — see triggeredInside.
	skipPaths := make(map[string]bool)
	if entries, err := a.db.AllGamePaths(); err == nil {
		for _, e := range entries {
			if !isPathUnder(abs, e.Path) {
				continue
			}
			if mtimeMatches(e.Path, e.DirMTime) && !triggeredInside(e.Path, triggerPaths) {
				skipPaths[e.Path] = true
			}
		}
	}

	progress := func(dirsExamined, gamesFound int, phase string) {
		a.emitRuntime("scan:auto-progress", ScanProgress{
			DirsExamined: dirsExamined,
			GamesFound:   gamesFound,
			Phase:        phase,
		})
	}

	detected, err := scanner.ScanFiltered(ctx, abs, skipPaths, progress)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			a.emitRuntime("scan:auto-error", map[string]string{"error": err.Error()})
		}
		return true
	}

	// removeMissingUnder runs first: a game moved within the root is matched
	// to its new path and relocated in place (preserving user curation)
	// before the upsert pass sees the detection at the new path — otherwise
	// the upsert would insert a fresh, uncurated row and the old one would be
	// soft-deleted.
	removed := a.removeMissingUnder(abs)
	// Watcher auto-upserts stay non-destructive: force=false preserves manual
	// corrections to version/engine/exe_path.
	inserted, updated, errs := a.upsertDetected(detected, false)

	result := map[string]interface{}{
		"gamesFound": len(detected),
		"inserted":   inserted,
		"updated":    updated,
		"removed":    removed,
		"errors":     errs,
	}
	a.emitRuntime("scan:auto-complete", result)
	slog.Info("auto-scan complete", "root", abs, "found", len(detected), "inserted", inserted, "updated", updated, "removed", removed)
	return true
}

// triggeredInside reports whether any of the triggering event paths for a
// scan lies inside dir (or is dir itself). An in-place file overwrite does
// not change the directory's mtime, so the mtime skip set would otherwise
// skip the very directory the event was about.
func triggeredInside(dir string, triggerPaths []string) bool {
	for _, tp := range triggerPaths {
		if isPathUnder(dir, tp) {
			return true
		}
	}
	return false
}

// upsertDetected delegates to the shared scan upsert (internal/commands),
// keeping the desktop's auto-upserts non-destructive: version/engine/exe_path
// are only filled when unset so manual corrections survive. Returns counts and
// per-game errors.
func (a *App) upsertDetected(detected []scanner.DetectedGame, force bool) (inserted, updated int, errs []string) {
	return commands.UpsertDetected(a.db, detected, force)
}

// removeMissingUnder delegates to the shared relocation logic in
// internal/commands: game rows whose directories are definitively gone under
// root are soft-deleted, after giving same-basename moves-within-root a
// chance to keep their row (path updated in place, curation preserved).
// Returns the number of rows soft-deleted.
func (a *App) removeMissingUnder(root string) int {
	return commands.RemoveMissingUnder(a.db, root)
}

// matchRoot returns the watched scan root that path lives under, or an empty
// string if none match. It reads the snapshot taken at Start — matching runs
// once per filesystem event, so it must not touch the config file on disk.
func (w *DirectoryWatcher) matchRoot(path string) string {
	for _, root := range w.roots {
		if isPathUnder(root, path) {
			return root
		}
	}
	return ""
}

// isPathUnder reports whether p is inside root (p == root included).
func isPathUnder(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// dirModTime returns the directory's modification time at second precision,
// matching the CLI's storage convention.
func dirModTime(dir string) time.Time {
	info, err := os.Stat(dir)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime().UTC().Truncate(time.Second)
}

// mtimeMatches reports whether a directory's current mtime matches the stored
// mtime from a previous scan (second precision).
func mtimeMatches(dir string, stored time.Time) bool {
	stored = stored.UTC().Truncate(time.Second)
	current := dirModTime(dir)
	if current.IsZero() {
		// Can't stat: skip re-detecting it. If it is genuinely gone,
		// removeMissingUnder handles the deletion; if it is merely
		// unreachable, skipping is the non-destructive choice.
		return true
	}
	return current.Equal(stored)
}
