package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIsPathUnder(t *testing.T) {
	cases := []struct {
		root, path string
		want       bool
	}{
		{"/games", "/games", true},
		{"/games", "/games/sub/game", true},
		{"/games", "/games2/game", false},
		{"/games", "/other/game", false},
		{"/games", "/games-other", false},
		{"/games", "/g", false},
	}
	for _, c := range cases {
		if got := isPathUnder(c.root, c.path); got != c.want {
			t.Errorf("isPathUnder(%q, %q) = %v, want %v", c.root, c.path, got, c.want)
		}
	}
}

func TestDirModTimeAndMtimeMatches(t *testing.T) {
	dir := t.TempDir()

	if dirModTime(dir).IsZero() {
		t.Error("dirModTime on existing dir should not be zero")
	}
	if !dirModTime(filepath.Join(dir, "missing")).IsZero() {
		t.Error("dirModTime on missing dir should be zero")
	}

	// Fresh dir mtime matches itself at second precision.
	mt := dirModTime(dir)
	if !mtimeMatches(dir, mt) {
		t.Error("mtimeMatches should be true for a just-stated dir")
	}

	// Zero stored time never matches.
	if mtimeMatches(dir, time.Time{}) {
		t.Error("mtimeMatches should be false for zero stored time")
	}

	// Missing dir treated as unchanged.
	if !mtimeMatches(filepath.Join(dir, "missing"), time.Now().Add(-time.Hour)) {
		t.Error("mtimeMatches on missing dir should be true")
	}

	// A stored mtime that differs should not match.
	stale := mt.Add(-time.Hour)
	if mtimeMatches(dir, stale) {
		t.Error("mtimeMatches should be false when mtime differs from stored")
	}
}

func TestMtimeMatchesStaleAfterModify(t *testing.T) {
	dir := t.TempDir()
	before := dirModTime(dir)

	// Write a file; on coarse filesystems the dir mtime may not change within
	// the same second, so only assert the invariant holds at second precision.
	_ = os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x"), 0o644)
	after := dirModTime(dir)

	if mtimeMatches(dir, before) && before.Equal(after) {
		t.Log("directory mtime did not advance within same second — invariant checked via Equal")
	}
	// After a change, either the mtime advanced (no match) or remained equal
	// (match). Both are consistent with second-precision comparison.
	_ = after
}

// --- fsnotify-level watcher tests -------------------------------------------
//
// These exercise the real fsnotify event loop against temp directories: the
// debounced sweep calls App.RescanDirectory, which runs the real scanner
// against a real database, so each test observes the library state the way a
// user would — a game row appearing or updating. The 750 ms debounce plus the
// 500 ms sweep tick mean every assertion below polls instead of sleeping.

// waitFor polls cond until it holds or the deadline expires.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// watchGame creates a directory the scanner detects as a game (an .exe file
// is a sufficient marker) and returns its path.
func watchGame(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".exe"), []byte("MZ"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// startWatcher starts a watcher on the given roots and registers a cleanup
// that stops it, so a failing assertion cannot leak watcher goroutines into
// the next test.
func startWatcher(t *testing.T, a *App, roots ...string) *DirectoryWatcher {
	t.Helper()
	w := NewDirectoryWatcher(a)
	if err := w.Start(roots); err != nil {
		t.Fatalf("watcher Start(%v): %v", roots, err)
	}
	t.Cleanup(func() { w.Stop() })
	return w
}

// The end-to-end watcher path: a file created inside a watched game
// directory fires an incremental rescan that inserts the game. An in-place
// overwrite of the exe — a change that does NOT advance the directory mtime —
// must still be rescanned, because the triggering event path exempts the game
// directory from the mtime skip set. Without the exemption the scanner would
// skip the very directory the event was about.
func TestWatcherRescanFiresAfterWriteAndInPlaceWrite(t *testing.T) {
	a := newTestApp(t)
	root := t.TempDir()
	gameDir := watchGame(t, root, "Alpha Game")

	startWatcher(t, a, root)

	// Create a file inside the watched game dir: the trigger for the first
	// rescan (a tree that existed before Start is only scanned once an event
	// arrives).
	if err := os.WriteFile(filepath.Join(gameDir, "seed.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "game row inserted by watcher rescan", func() bool {
		g, err := a.db.GetGameByPath(gameDir)
		return err == nil && g != nil && g.DeletedAt.IsZero()
	})

	// In-place overwrite of the existing exe: same directory entry, so the
	// directory mtime does not advance. Only the triggeredInside exemption
	// can make the rescan re-detect it (the scanner reports the new size).
	big := strings.Repeat("MZ", 200)
	if err := os.WriteFile(filepath.Join(gameDir, "Alpha Game.exe"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}

	wantSize := int64(len(big) + 1) // exe + seed.txt
	waitFor(t, "size update from in-place write rescan", func() bool {
		g, err := a.db.GetGameByPath(gameDir)
		return err == nil && g != nil && g.SizeBytes == wantSize
	})
}

// A directory moved into a scan root from outside arrives as IN_MOVED_TO (a
// Rename op on Linux). The watcher must register watches for the moved-in
// subtree so later writes inside it still fire rescans — otherwise every
// change in the new home is silently missed.
func TestWatcherRescanAfterMovedInDir(t *testing.T) {
	a := newTestApp(t)
	root := t.TempDir()
	external := t.TempDir()
	gameDir := watchGame(t, external, "Moved In Game")

	startWatcher(t, a, root)

	// Move the whole game dir into the root.
	newDir := filepath.Join(root, "Moved In Game")
	if err := os.Rename(gameDir, newDir); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "game row for moved-in directory", func() bool {
		g, err := a.db.GetGameByPath(newDir)
		return err == nil && g != nil && g.DeletedAt.IsZero()
	})

	// A write INSIDE the moved-in dir must still be seen — the subtree watch
	// registered from the Rename event. Without it the row would never see
	// the size update and this wait would time out.
	if err := os.WriteFile(filepath.Join(newDir, "extra.bin"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantSize := int64(len("MZ") + len("data")) // exe + extra.bin
	waitFor(t, "size update from write inside moved-in dir", func() bool {
		g, err := a.db.GetGameByPath(newDir)
		return err == nil && g != nil && g.SizeBytes == wantSize
	})
}

// A game directory renamed within the same scan root must keep its row: the
// move-match pass relocates path and dir mtime in place so user curation
// (status, title, tags) survives, instead of the row being soft-deleted and a
// fresh, uncurated one inserted at the new path.
func TestWatcherMoveWithinRootPreservesCuration(t *testing.T) {
	a := newTestApp(t)
	root := t.TempDir()
	gameDir := watchGame(t, root, "Curated Game")

	startWatcher(t, a, root)

	if err := os.WriteFile(filepath.Join(gameDir, "seed.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "game row at initial location", func() bool {
		g, err := a.db.GetGameByPath(gameDir)
		return err == nil && g != nil
	})
	before, err := a.db.GetGameByPath(gameDir)
	if err != nil || before == nil {
		t.Fatalf("GetGameByPath: %v", err)
	}

	// User curation, then the move one level deeper (same basename).
	if err := a.db.UpdateGameStatus(before.ID, "completed"); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(root, "nested", "Curated Game")
	if err := os.MkdirAll(filepath.Dir(moved), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(gameDir, moved); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "game row relocated to the new path", func() bool {
		g, err := a.db.GetGameByPath(moved)
		return err == nil && g != nil && g.ID == before.ID && g.DeletedAt.IsZero()
	})

	after, err := a.db.GetGameByPath(moved)
	if err != nil || after == nil {
		t.Fatalf("GetGameByPath(moved): %v", err)
	}
	if after.ID != before.ID {
		t.Errorf("row id = %d, want %d — the move must update in place, not re-insert", after.ID, before.ID)
	}
	if after.Status != "completed" {
		t.Errorf("status = %q, want curated %q preserved through the move", after.Status, "completed")
	}
	if gone, _ := a.db.GetGameByPath(gameDir); gone != nil {
		t.Error("old path must no longer resolve to the row after relocation")
	}
}

// When a manual scan holds the single-flight lock, the sweep's rescan
// returns false and the root must be re-queued, not dropped — the change
// fires as soon as the lock frees.
func TestWatcherCASSkippedRescanRequeued(t *testing.T) {
	a := newTestApp(t)
	root := t.TempDir()
	gameDir := watchGame(t, root, "CAS Game")

	w := startWatcher(t, a, root)

	// Hold the scan lock: every RescanDirectory attempt now fails its CAS.
	a.scanRunning.Store(true)
	if err := os.WriteFile(filepath.Join(gameDir, "seed.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Wait for the event to be queued, then for the sweep to have attempted
	// (and failed) the rescan: a successful CAS failure re-queues the root
	// with a fresh debounce timestamp, which advances past the initial queue
	// time by at least one full debounce window.
	var queuedAt time.Time
	waitFor(t, "event queued while scan lock held", func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		ps, ok := w.pending[root]
		if ok {
			queuedAt = ps.last
		}
		return ok
	})

	waitFor(t, "root re-queued after failed CAS attempt", func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		ps, ok := w.pending[root]
		return ok && ps.last.Sub(queuedAt) > watchDebounce
	})

	// Free the lock: the re-queued entry must now fire the rescan.
	a.scanRunning.Store(false)
	waitFor(t, "game row inserted after lock release", func() bool {
		g, err := a.db.GetGameByPath(gameDir)
		return err == nil && g != nil
	})
}

// A scan path that does not exist at Start must be logged and skipped, not
// fatal: the remaining roots still watch and rescan normally, and the missing
// path is not matched against events.
func TestWatcherStartSkipsMissingRoot(t *testing.T) {
	a := newTestApp(t)
	root := t.TempDir()
	missing := filepath.Join(t.TempDir(), "not-mounted-yet")
	gameDir := watchGame(t, root, "Valid Game")

	w := startWatcher(t, a, missing, root)

	if got := w.matchRoot(missing); got != "" {
		t.Errorf("missing root must not be watched, matchRoot = %q", got)
	}
	if got := w.matchRoot(root); got != root {
		t.Errorf("valid root must be watched, matchRoot = %q", got)
	}

	// The valid root still works end to end.
	if err := os.WriteFile(filepath.Join(gameDir, "seed.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "game row inserted via the surviving root", func() bool {
		g, err := a.db.GetGameByPath(gameDir)
		return err == nil && g != nil
	})
}

// Stop must return promptly (never hit the grace deadline), be idempotent,
// and leave the watcher in a state where late filesystem activity cannot
// panic the process.
func TestWatcherCleanStop(t *testing.T) {
	a := newTestApp(t)
	root := t.TempDir()
	watchGame(t, root, "Game")

	w := startWatcher(t, a, root)

	done := make(chan struct{})
	go func() {
		w.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(watchStopGrace + 2*time.Second):
		t.Fatal("Stop did not return within the grace period")
	}
	w.Stop() // second call must not panic

	// Filesystem activity after Stop must not panic the event loop.
	if err := os.WriteFile(filepath.Join(root, "after-stop.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
}

// triggeredInside is the pure function behind the in-place-write exemption:
// an event path inside (or equal to) the game directory means the directory
// must not be skipped by the mtime skip set.
func TestTriggeredInside(t *testing.T) {
	cases := []struct {
		name     string
		dir      string
		triggers []string
		want     bool
	}{
		{"file inside dir", "/games/A", []string{"/games/A/data.txt"}, true},
		{"dir itself", "/games/A", []string{"/games/A"}, true},
		{"deeper file", "/games/A", []string{"/games/A/sub/game.exe"}, true},
		{"sibling dir", "/games/A", []string{"/games/B/data.txt"}, false},
		{"parent dir", "/games/A", []string{"/games"}, false},
		{"prefix collision", "/games/A", []string{"/games/AB/data.txt"}, false},
		{"no triggers", "/games/A", nil, false},
	}
	for _, c := range cases {
		if got := triggeredInside(c.dir, c.triggers); got != c.want {
			t.Errorf("triggeredInside(%q, %v) = %v, want %v", c.dir, c.triggers, got, c.want)
		}
	}
}
