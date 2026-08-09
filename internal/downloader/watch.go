package downloader

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/mili/moxie/internal/archive"
	"github.com/mili/moxie/internal/log"
)

// DefaultDebounce is the quiet period after the last filesystem event for a
// file before it is reported as a completed download. Browsers write
// archives incrementally; this coalesces the many Create/Write/Rename events
// a single save produces so a half-written file is never handed to the
// install pipeline.
const DefaultDebounce = 2 * time.Second

// sweepInterval is how often the debounce map is scanned for due files.
const sweepInterval = 200 * time.Millisecond

// partialSuffixes are browser/extension partial-download markers. A file
// with one of these suffixes is still being written and is never reported;
// the browser renames it to its final name when the download completes.
var partialSuffixes = []string{
	".part",        // Firefox, aria2
	".crdownload",  // Chrome/Chromium
	".download",    // Safari, wget
	".partial",     // common partial marker
	".opdownload",  // Opera
	".tmp",         // generic temp
	".temp",        // generic temp
	".aria2",       // aria2 control file
	".crdownload1", // Chrome multi-part
}

// archiveExtension reports whether name looks like a game archive download.
// Extension-only at event time: magic-byte detection is unreliable while a
// file is still being written (zip central directories live at the tail).
// The final readiness check in reportIfReady re-verifies with
// archive.IsArchiveFile once the file has settled.
func archiveExtension(name string) bool {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".zip"),
		strings.HasSuffix(lower, ".7z"),
		strings.HasSuffix(lower, ".rar"),
		strings.HasSuffix(lower, ".tar.gz"),
		strings.HasSuffix(lower, ".tgz"),
		strings.HasSuffix(lower, ".tar"),
		strings.HasSuffix(lower, ".gz"),
		strings.HasSuffix(lower, ".bz2"),
		strings.HasSuffix(lower, ".xz"):
		return true
	}
	return false
}

// ArchiveWatcher watches a single directory for newly appeared game archive
// files (e.g. downloads saved by the user's browser after moxie opened a
// download link in it). Files are debounced, filtered (non-archive, hidden,
// and partial-download files are ignored), and only files that appear after
// Start are reported — pre-existing content of the directory is never
// handed to the callback.
//
// The callback runs on the watcher's own goroutine and must not block.
type ArchiveWatcher struct {
	dir      string
	debounce time.Duration
	callback func(path string)

	watcher  *fsnotify.Watcher
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup

	mu       sync.Mutex
	started  bool
	stopped  bool
	pending  map[string]time.Time // path -> last event time (debounce window)
	reported map[string]bool      // paths already handed to the callback
	existing map[string]bool      // names present at Start — never reported
}

// ArchiveWatcherOption configures an ArchiveWatcher.
type ArchiveWatcherOption func(*ArchiveWatcher)

// WithDebounce sets the quiet period after the last filesystem event for a
// file before it is reported. Defaults to DefaultDebounce.
func WithDebounce(d time.Duration) ArchiveWatcherOption {
	return func(w *ArchiveWatcher) { w.debounce = d }
}

// WithOnArchive sets the callback invoked with the full path of each
// debounced, completed archive.
func WithOnArchive(fn func(path string)) ArchiveWatcherOption {
	return func(w *ArchiveWatcher) { w.callback = fn }
}

// NewArchiveWatcher creates a watcher for dir. The directory is created if
// it does not exist (at Start). Start must be called before events are
// delivered; Stop releases the underlying fsnotify resources.
func NewArchiveWatcher(dir string, opts ...ArchiveWatcherOption) *ArchiveWatcher {
	w := &ArchiveWatcher{
		dir:      dir,
		debounce: DefaultDebounce,
		pending:  make(map[string]time.Time),
		reported: make(map[string]bool),
		existing: make(map[string]bool),
	}
	for _, o := range opts {
		o(w)
	}
	return w
}

// Dir returns the watched directory.
func (w *ArchiveWatcher) Dir() string {
	return w.dir
}

// Start begins watching w.dir. It returns an error if the directory cannot
// be created or the underlying fsnotify watcher cannot be initialized.
// Start must not be called twice, and a stopped watcher cannot be
// restarted (create a new one instead).
func (w *ArchiveWatcher) Start() error {
	w.mu.Lock()
	if w.started {
		w.mu.Unlock()
		return fmt.Errorf("archive watcher already started for %s", w.dir)
	}
	if w.stopped {
		w.mu.Unlock()
		return fmt.Errorf("archive watcher for %s was stopped and cannot be restarted", w.dir)
	}
	w.mu.Unlock()

	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		return fmt.Errorf("create watch dir %s: %w", w.dir, err)
	}

	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("fsnotify: %w", err)
	}
	if err := fw.Add(w.dir); err != nil {
		fw.Close()
		return fmt.Errorf("watch %s: %w", w.dir, err)
	}

	w.watcher = fw
	w.stopCh = make(chan struct{})

	// Snapshot the directory so only files that appear after Start are
	// reported, not leftovers from earlier downloads.
	w.snapshotExisting()

	w.mu.Lock()
	w.started = true
	w.mu.Unlock()

	w.wg.Add(2)
	go w.eventLoop()
	go w.sweepLoop()
	return nil
}

// Stop stops the watcher, waits for the event and sweep loops to exit, and
// releases the fsnotify watcher. Safe to call concurrently and more than
// once; calling Stop without Start is a no-op.
//
// The OnArchive callback runs synchronously on the watcher's sweep
// goroutine — it must not block and must not call Stop.
func (w *ArchiveWatcher) Stop() error {
	var err error
	w.stopOnce.Do(func() {
		if w.stopCh != nil {
			close(w.stopCh)
		}
		w.wg.Wait()
		if w.watcher != nil {
			err = w.watcher.Close()
		}
		w.mu.Lock()
		w.started = false
		w.stopped = true
		w.mu.Unlock()
	})
	return err
}

// snapshotExisting records the names currently in the watched directory.
func (w *ArchiveWatcher) snapshotExisting() {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, e := range entries {
		if !e.IsDir() {
			w.existing[e.Name()] = true
		}
	}
}

func (w *ArchiveWatcher) eventLoop() {
	defer w.wg.Done()
	for {
		select {
		case <-w.stopCh:
			return
		case ev, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleEvent(ev)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			log.Warn("archive watcher error", "dir", w.dir, "error", err)
		}
	}
}

func (w *ArchiveWatcher) sweepLoop() {
	defer w.wg.Done()
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.sweep()
		}
	}
}

// handleEvent records a debounce timestamp for any event touching a
// candidate archive file directly inside the watched directory.
func (w *ArchiveWatcher) handleEvent(ev fsnotify.Event) {
	if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) == 0 {
		return
	}

	// Only direct children of the watched dir (fsnotify is non-recursive).
	if filepath.Dir(ev.Name) != filepath.Clean(w.dir) {
		return
	}

	name := filepath.Base(ev.Name)
	if w.isCandidate(name) {
		w.mu.Lock()
		w.pending[ev.Name] = time.Now()
		w.mu.Unlock()
	}
}

// isCandidate reports whether name is worth tracking: not hidden, not a
// partial download, and a known archive extension.
func (w *ArchiveWatcher) isCandidate(name string) bool {
	if strings.HasPrefix(name, ".") {
		return false
	}
	lower := strings.ToLower(name)
	for _, s := range partialSuffixes {
		if strings.HasSuffix(lower, s) {
			return false
		}
	}
	return archiveExtension(name)
}

// sweep collects pending paths whose debounce window has elapsed.
func (w *ArchiveWatcher) sweep() {
	now := time.Now()
	var due []string
	w.mu.Lock()
	for path, t := range w.pending {
		if now.Sub(t) >= w.debounce {
			due = append(due, path)
		}
	}
	w.mu.Unlock()

	for _, path := range due {
		w.reportIfReady(path)
	}
}

// reportIfReady performs the final readiness checks for a settled file and
// invokes the callback exactly once per path.
func (w *ArchiveWatcher) reportIfReady(path string) {
	w.mu.Lock()
	delete(w.pending, path)
	if w.reported[path] || w.existing[filepath.Base(path)] {
		w.mu.Unlock()
		return
	}
	w.mu.Unlock()

	// Final readiness: a settled regular file that is a real archive.
	// Files that fail the check (junk, corrupt, still being written) are
	// marked reported so they are not retried on every sweep.
	ready := false
	if fi, err := os.Stat(path); err == nil && !fi.IsDir() && archive.IsArchiveFile(path) {
		ready = true
	}

	w.mu.Lock()
	w.reported[path] = true
	cb := w.callback
	w.mu.Unlock()

	if ready && cb != nil {
		log.Info("archive watcher: new archive detected", "dir", w.dir, "file", filepath.Base(path))
		cb(path)
	}
}
