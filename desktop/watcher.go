package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/scanner"
	"github.com/mili/moxie/internal/scraper"
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
	inserted, updated, errs := a.upsertDetected(detected)

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

// upsertDetected inserts new games and updates existing records, preserving
// fields the user may have curated. Returns counts and per-game errors.
func (a *App) upsertDetected(detected []scanner.DetectedGame) (inserted, updated int, errs []string) {
	for _, g := range detected {
		existing, err := a.db.GetGameByPath(g.Path)
		if err != nil {
			errs = append(errs, g.Title+": "+err.Error())
			continue
		}

		now := time.Now().UTC()
		if existing != nil {
			// A soft-deleted game found again on disk comes back to life —
			// otherwise the UNIQUE index on path keeps blocking re-insertion
			// while the row stays invisible in every listing.
			if !existing.DeletedAt.IsZero() {
				if err := a.db.RestoreGame(existing.ID); err != nil {
					errs = append(errs, existing.Title+": "+err.Error())
					continue
				}
			}
			// Narrow, atomic update: only the scanner-owned fields are
			// written, and version/engine/exe_path only when unset (manual
			// corrections win). The "unset" checks live inside the UPDATE,
			// so a user edit landing between our read and write can never be
			// clobbered with the stale record we loaded.
			if err := a.db.UpdateGameScanFields(
				existing.ID, g.Version, string(g.Engine), g.ExePath,
				g.SizeBytes, now, dirModTime(g.Path),
			); err != nil {
				errs = append(errs, existing.Title+": "+err.Error())
				continue
			}
			updated++
			continue
		}

		title := scraper.SanitizeTitle(g.Title)
		if title == "" {
			title = g.Title
		}
		newGame := &db.Game{
			Title:         title,
			Engine:        string(g.Engine),
			Path:          g.Path,
			ExePath:       g.ExePath,
			Version:       g.Version,
			SizeBytes:     g.SizeBytes,
			Status:        "unknown",
			LastScannedAt: now,
			DirMTime:      dirModTime(g.Path),
		}
		if _, err := a.db.InsertGame(newGame); err != nil {
			errs = append(errs, title+": "+err.Error())
			continue
		}
		inserted++
	}
	return inserted, updated, errs
}

// vanishedGame is a game row whose directory is definitively gone from disk,
// collected by removeMissingUnder.
type vanishedGame struct {
	path string
	game *db.Game
}

// removeMissingUnder soft-deletes games under root whose directory no longer
// exists on disk, after first giving vanished directories a chance to be a
// move-within-root: a game folder renamed inside the same scan root keeps its
// row (path and dir mtime updated in place) so user curation — status, title,
// tags — survives. Returns the number of rows soft-deleted.
func (a *App) removeMissingUnder(root string) int {
	entries, err := a.db.AllGamePaths()
	if err != nil {
		return 0
	}

	// Collect the rows whose directories are definitively gone. Only a
	// definitive "not there" justifies action; any other stat failure — an
	// unmounted drive, EACCES on a parent, EMFILE — means we do not know,
	// and guessing "gone" would trash the whole library on the next sweep.
	var gone []vanishedGame
	for _, e := range entries {
		if !isPathUnder(root, e.Path) {
			continue
		}
		if _, serr := os.Stat(e.Path); !errors.Is(serr, fs.ErrNotExist) {
			continue
		}
		game, gerr := a.db.GetGameByPath(e.Path)
		if gerr != nil || game == nil {
			continue
		}
		// Already in the trash — re-deleting would reset deleted_at on every
		// sweep, so the 30-day auto-purge would never come due, and the UI
		// would report a phantom removal each time.
		if !game.DeletedAt.IsZero() {
			continue
		}
		gone = append(gone, vanishedGame{path: e.Path, game: game})
	}
	if len(gone) == 0 {
		return 0
	}

	// Match vanished directories to same-basename directories that now exist
	// under the root — the signature of a move-within-root.
	moved := a.matchMovedWithinRoot(root, gone, entries)

	removed := 0
	for _, v := range gone {
		if newPath, ok := moved[v.path]; ok {
			if err := a.relocateGameRow(v.game, v.path, newPath); err != nil {
				slog.Warn("watch: could not relocate moved game",
					"from", v.path, "to", newPath, "error", err)
				removed++
			}
			continue
		}
		if derr := a.db.DeleteGame(v.game.ID); derr == nil {
			removed++
		}
	}
	return removed
}

// matchMovedWithinRoot finds, for each vanished game directory, a directory
// under the same root with the same basename — the signature of a
// move-within-root. Candidates nested inside another known game's directory
// are rejected: the scanner treats those as part of the parent game, so they
// cannot be the new home of a standalone game row. Returns vanished path ->
// new path.
func (a *App) matchMovedWithinRoot(root string, gone []vanishedGame, allRows []db.GamePathEntry) map[string]string {
	wantBase := make(map[string]string) // basename -> vanished path
	for _, v := range gone {
		wantBase[filepath.Base(v.path)] = v.path
	}
	found := make(map[string]string)

	nestedInRow := func(p string) bool {
		for _, r := range allRows {
			if p != r.Path && isPathUnder(r.Path, p) {
				return true
			}
		}
		return false
	}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() || path == root {
			return nil
		}
		old, ok := wantBase[d.Name()]
		if !ok || path == old {
			return nil
		}
		if nestedInRow(path) {
			return nil
		}
		if _, taken := found[old]; !taken {
			found[old] = path
		}
		return nil
	})
	if err != nil {
		slog.Warn("watch: move-match walk failed", "root", root, "error", err)
	}
	return found
}

// relocateGameRow updates a game row whose directory moved within the scan
// root: path and dir mtime are refreshed in place, and a scanner-owned
// exe_path that pointed into the old directory is cleared so the following
// upsert pass refills it from detection. Everything the user curated (title,
// status, tags, notes, f95 URL) is preserved — the row is never deleted and
// re-inserted.
func (a *App) relocateGameRow(game *db.Game, oldPath, newPath string) error {
	// A row already exists at the new path (a previous scan inserted it
	// before the move was recognised): the old row is a duplicate and the
	// caller falls back to soft-deleting it.
	if existing, err := a.db.GetGameByPath(newPath); err == nil && existing != nil {
		return fmt.Errorf("a row already exists at the moved path")
	}
	game.Path = newPath
	game.DirMTime = dirModTime(newPath)
	game.LastScannedAt = time.Now().UTC()
	if game.ExePath != "" && isPathUnder(oldPath, game.ExePath) {
		// Points into the old directory — stale after the move. Clear it so
		// the upsert pass refills it from the new location.
		game.ExePath = ""
	}
	return a.db.UpdateGame(game)
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
