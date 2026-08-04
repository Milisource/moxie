package main

import (
	"log/slog"
	"os"
	"path/filepath"
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

// DirectoryWatcher watches all configured scan paths for filesystem changes
// and triggers incremental rescans so the library stays in sync without
// manual intervention.
type DirectoryWatcher struct {
	app     *App
	watcher *fsnotify.Watcher
	stopCh  chan struct{}
	mu      sync.Mutex
	pending map[string]time.Time // scan root -> last event time
}

// NewDirectoryWatcher creates a watcher bound to the given App.
func NewDirectoryWatcher(a *App) *DirectoryWatcher {
	return &DirectoryWatcher{
		app:     a,
		stopCh:  make(chan struct{}),
		pending: make(map[string]time.Time),
	}
}

// Start begins watching the given scan paths. It returns an error if the
// underlying fsnotify watcher cannot be created.
func (w *DirectoryWatcher) Start(paths []string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	w.watcher = watcher

	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if err := w.addRecursive(abs); err != nil {
			slog.Warn("watch: could not watch scan path", "path", abs, "error", err)
		}
	}

	go w.eventLoop()
	go w.sweepLoop()
	return nil
}

// Stop stops watching and releases the underlying resources.
func (w *DirectoryWatcher) Stop() error {
	close(w.stopCh)
	if w.watcher != nil {
		return w.watcher.Close()
	}
	return nil
}

// addRecursive registers an inotify watcher for root and every subdirectory
// under it (inotify does not watch recursively).
func (w *DirectoryWatcher) addRecursive(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		return w.watcher.Add(path)
	})
}

func (w *DirectoryWatcher) eventLoop() {
	for {
		select {
		case <-w.stopCh:
			return
		case ev, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			// A new directory needs a watcher registered for its subtree.
			if ev.Op&fsnotify.Create != 0 {
				if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
					_ = w.addRecursive(ev.Name)
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
// actual rescan until the debounce window passes.
func (w *DirectoryWatcher) queueScan(path string) {
	root := w.app.matchScanRoot(path)
	if root == "" {
		return
	}
	w.mu.Lock()
	w.pending[root] = time.Now()
	w.mu.Unlock()
}

// sweepLoop periodically fires due rescans after the debounce window.
func (w *DirectoryWatcher) sweepLoop() {
	ticker := time.NewTicker(watchSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-w.stopCh:
			return
		case now := <-ticker.C:
			var due []string
			w.mu.Lock()
			for root, t := range w.pending {
				if now.Sub(t) >= watchDebounce {
					due = append(due, root)
					delete(w.pending, root)
				}
			}
			w.mu.Unlock()
			for _, root := range due {
				w.app.RescanDirectory(root)
			}
		}
	}
}

// RescanDirectory runs an incremental scan of root, upserts detected games,
// soft-deletes games whose directories vanished, and emits Wails events so
// the frontend can refresh. It is safe to call from any goroutine.
func (a *App) RescanDirectory(root string) {
	if a.db == nil {
		return
	}

	abs, err := filepath.Abs(root)
	if err != nil {
		return
	}

	runtime.EventsEmit(a.ctx, "scan:auto", "started")

	// Build the incremental skip set: known games under this root whose
	// directory mtime is unchanged are skipped for speed.
	skipPaths := make(map[string]bool)
	if entries, err := a.db.AllGamePaths(); err == nil {
		for _, e := range entries {
			if isPathUnder(abs, e.Path) && mtimeMatches(e.Path, e.DirMTime) {
				skipPaths[e.Path] = true
			}
		}
	}

	progress := func(dirsExamined, gamesFound int, phase string) {
		runtime.EventsEmit(a.ctx, "scan:auto-progress", ScanProgress{
			DirsExamined: dirsExamined,
			GamesFound:   gamesFound,
			Phase:        phase,
		})
	}

	detected, err := scanner.ScanFiltered(abs, skipPaths, progress)
	if err != nil {
		runtime.EventsEmit(a.ctx, "scan:auto-error", map[string]string{"error": err.Error()})
		return
	}

	inserted, updated, errs := a.upsertDetected(detected)
	removed := a.removeMissingUnder(abs)

	result := map[string]interface{}{
		"gamesFound": len(detected),
		"inserted":   inserted,
		"updated":    updated,
		"removed":    removed,
		"errors":     errs,
	}
	runtime.EventsEmit(a.ctx, "scan:auto-complete", result)
	slog.Info("auto-scan complete", "root", abs, "found", len(detected), "inserted", inserted, "updated", updated, "removed", removed)
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
			// Preserve manual corrections — only fill unset/fallback fields.
			if existing.Version == "" {
				existing.Version = g.Version
			}
			if existing.Engine == "" || existing.Engine == "Unknown" {
				existing.Engine = string(g.Engine)
			}
			if existing.ExePath == "" {
				existing.ExePath = g.ExePath
			}
			existing.SizeBytes = g.SizeBytes
			existing.LastScannedAt = now
			existing.DirMTime = dirModTime(g.Path)
			if err := a.db.UpdateGame(existing); err != nil {
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

// removeMissingUnder soft-deletes games under root whose directory no longer
// exists on disk. Returns the number removed.
func (a *App) removeMissingUnder(root string) int {
	entries, err := a.db.AllGamePaths()
	if err != nil {
		return 0
	}
	removed := 0
	for _, e := range entries {
		if !isPathUnder(root, e.Path) {
			continue
		}
		if _, err := os.Stat(e.Path); err == nil {
			continue
		}
		game, gerr := a.db.GetGameByPath(e.Path)
		if gerr != nil || game == nil {
			continue
		}
		if derr := a.db.DeleteGame(game.ID); derr == nil {
			removed++
		}
	}
	return removed
}

// matchScanRoot returns the configured scan root that path lives under, or an
// empty string if none match.
func (a *App) matchScanRoot(path string) string {
	for _, p := range a.GetScanPaths() {
		if isPathUnder(p, path) {
			return p
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
		return true // can't stat, treat as unchanged
	}
	return current.Equal(stored)
}
