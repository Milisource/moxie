package downloader

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeTestZip creates a real zip archive at path with a single data file.
// The archive magic bytes (PK\x03\x04) are what archive.IsArchiveFile
// detects, so the watcher's final readiness check passes.
func writeTestZip(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("data.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(strings.Repeat("x", 4096))); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// waitForReport blocks until a path arrives on ch, failing the test after
// timeout.
func waitForReport(t *testing.T, ch <-chan string, timeout time.Duration) string {
	t.Helper()
	select {
	case p := <-ch:
		return p
	case <-time.After(timeout):
		t.Fatal("timed out waiting for watcher callback")
		return ""
	}
}

// assertNoReport fails the test if any path arrives on ch within window.
func assertNoReport(t *testing.T, ch <-chan string, window time.Duration) {
	t.Helper()
	select {
	case p := <-ch:
		t.Fatalf("unexpected watcher callback for %s", p)
	case <-time.After(window):
	}
}

// startTestWatcher creates a watcher on dir with a 150ms debounce and a
// callback that forwards reported paths to the returned channel.
func startTestWatcher(t *testing.T, dir string) (*ArchiveWatcher, <-chan string) {
	t.Helper()
	reported := make(chan string, 16)
	w := NewArchiveWatcher(dir,
		WithDebounce(150*time.Millisecond),
		WithOnArchive(func(path string) {
			select {
			case reported <- path:
			default: // never block the watcher goroutine
			}
		}),
	)
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = w.Stop() })
	return w, reported
}

// ---------------------------------------------------------------------------
// Reporting
// ---------------------------------------------------------------------------

func TestArchiveWatcher_ReportsNewArchive(t *testing.T) {
	dir := t.TempDir()
	_, reported := startTestWatcher(t, dir)

	archivePath := filepath.Join(dir, "Game_Update_v1.2.zip")
	writeTestZip(t, archivePath)

	got := waitForReport(t, reported, 3*time.Second)
	if got != archivePath {
		t.Errorf("reported path = %q, want %q", got, archivePath)
	}

	// The file must be reported exactly once, even though the initial
	// create and any write events are coalesced by the debounce.
	assertNoReport(t, reported, 500*time.Millisecond)
}

func TestArchiveWatcher_ReportsMultipleFilesOnceEach(t *testing.T) {
	dir := t.TempDir()
	_, reported := startTestWatcher(t, dir)

	a := filepath.Join(dir, "first.7z")
	b := filepath.Join(dir, "second.tar.gz")
	writeTestZip(t, a)
	writeTestZip(t, b)

	got := map[string]int{}
	for i := 0; i < 2; i++ {
		got[waitForReport(t, reported, 3*time.Second)]++
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 distinct archives reported, got %d: %v", len(got), got)
	}
	for path, n := range got {
		if n != 1 {
			t.Errorf("archive %s reported %d times, want 1", path, n)
		}
	}
	assertNoReport(t, reported, 500*time.Millisecond)
}

// ---------------------------------------------------------------------------
// Filtering
// ---------------------------------------------------------------------------

func TestArchiveWatcher_IgnoresPartAndHiddenFiles(t *testing.T) {
	dir := t.TempDir()
	_, reported := startTestWatcher(t, dir)

	// Firefox/Chrome partial-download markers.
	writeTestZip(t, filepath.Join(dir, "Game.part"))
	writeTestZip(t, filepath.Join(dir, "Game.zip.crdownload"))
	writeTestZip(t, filepath.Join(dir, "Game.7z.download"))
	// Hidden files (editor/browser sidecars).
	writeTestZip(t, filepath.Join(dir, ".Game.zip"))
	writeTestZip(t, filepath.Join(dir, ".hidden.zip"))

	assertNoReport(t, reported, 700*time.Millisecond)
}

func TestArchiveWatcher_IgnoresNonArchives(t *testing.T) {
	dir := t.TempDir()
	_, reported := startTestWatcher(t, dir)

	// No archive extension at all.
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Archive extension but corrupt content — fails the final check.
	if err := os.WriteFile(filepath.Join(dir, "junk.zip"), []byte("not a zip at all"), 0o644); err != nil {
		t.Fatal(err)
	}

	assertNoReport(t, reported, 700*time.Millisecond)
}

func TestArchiveWatcher_IgnoresPreExistingFiles(t *testing.T) {
	dir := t.TempDir()

	// The archive is already in the directory before the watcher starts —
	// e.g. a leftover from an earlier download attempt. It must not be
	// reported as a new appearance.
	leftover := filepath.Join(dir, "Leftover_Update.zip")
	writeTestZip(t, leftover)

	_, reported := startTestWatcher(t, dir)

	assertNoReport(t, reported, 700*time.Millisecond)

	// A genuinely new file is still picked up.
	fresh := filepath.Join(dir, "Fresh_Update.zip")
	writeTestZip(t, fresh)
	got := waitForReport(t, reported, 3*time.Second)
	if got != fresh {
		t.Errorf("reported path = %q, want %q", got, fresh)
	}
}

func TestArchiveWatcher_IgnoresDirectories(t *testing.T) {
	dir := t.TempDir()
	_, reported := startTestWatcher(t, dir)

	if err := os.MkdirAll(filepath.Join(dir, "subdir.zip"), 0o755); err != nil {
		t.Fatal(err)
	}
	assertNoReport(t, reported, 500*time.Millisecond)
}

// ---------------------------------------------------------------------------
// Debounce
// ---------------------------------------------------------------------------

func TestArchiveWatcher_DebounceDelaysReporting(t *testing.T) {
	dir := t.TempDir()
	_, reported := startTestWatcher(t, dir)

	archivePath := filepath.Join(dir, "Game.zip")
	writeTestZip(t, archivePath)

	// The debounce window (150ms) plus the sweep interval (200ms) means
	// the callback cannot fire for a while after the write.
	assertNoReport(t, reported, 100*time.Millisecond)

	got := waitForReport(t, reported, 3*time.Second)
	if got != archivePath {
		t.Errorf("reported path = %q, want %q", got, archivePath)
	}
}

func TestArchiveWatcher_RewritesResetDebounce(t *testing.T) {
	dir := t.TempDir()
	reported := make(chan string, 16)
	w := NewArchiveWatcher(dir,
		WithDebounce(800*time.Millisecond),
		WithOnArchive(func(path string) {
			select {
			case reported <- path:
			default:
			}
		}),
	)
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = w.Stop() })

	archivePath := filepath.Join(dir, "Game.zip")
	writeTestZip(t, archivePath)

	// Rewrite the file within the debounce window: each write resets the
	// timer, so the report must be delayed until the quiet period after
	// the LAST write (zip magic stays intact, so the final check passes).
	time.Sleep(300 * time.Millisecond)
	writeTestZip(t, archivePath)
	time.Sleep(300 * time.Millisecond)
	writeTestZip(t, archivePath)
	lastWrite := time.Now()

	got := waitForReport(t, reported, 4*time.Second)
	if got != archivePath {
		t.Errorf("reported path = %q, want %q", got, archivePath)
	}
	if elapsed := time.Since(lastWrite); elapsed < 600*time.Millisecond {
		t.Errorf("callback fired too early after last write: %v (debounce 800ms)", elapsed)
	}
	// Coalesced into exactly one report.
	assertNoReport(t, reported, 500*time.Millisecond)
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

func TestArchiveWatcher_StopStopsReporting(t *testing.T) {
	dir := t.TempDir()
	w, reported := startTestWatcher(t, dir)

	if err := w.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// Stop is idempotent.
	if err := w.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}

	// A file appearing after Stop must never be reported.
	writeTestZip(t, filepath.Join(dir, "Late.zip"))
	assertNoReport(t, reported, 500*time.Millisecond)
}

func TestArchiveWatcher_StartLifecycleErrors(t *testing.T) {
	dir := t.TempDir()
	w := NewArchiveWatcher(dir)
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Double start must fail without disturbing the running watcher.
	if err := w.Start(); err == nil {
		t.Error("expected second Start to fail")
	}

	if err := w.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// A stopped watcher cannot be restarted.
	if err := w.Start(); err == nil {
		t.Error("expected Start after Stop to fail")
	}
}

func TestArchiveWatcher_CreatesMissingDir(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "downloads", "nested")
	_, reported := startTestWatcher(t, dir)

	archivePath := filepath.Join(dir, "Game.zip")
	writeTestZip(t, archivePath)

	got := waitForReport(t, reported, 3*time.Second)
	if got != archivePath {
		t.Errorf("reported path = %q, want %q", got, archivePath)
	}
}

// TestArchiveWatcher_WritesToExistingFile verifies that finishing a
// partially written file (rename into the watched dir) is detected.
func TestArchiveWatcher_WritesToExistingFile(t *testing.T) {
	dir := t.TempDir()
	_, reported := startTestWatcher(t, dir)

	// Browser-style save: write a partial file, then rename it to the
	// final archive name (a Create event for the final name).
	partial := filepath.Join(dir, "Game.zip.part")
	writeTestZip(t, partial)
	final := filepath.Join(dir, "Game.zip")
	if err := os.Rename(partial, final); err != nil {
		t.Fatal(err)
	}

	got := waitForReport(t, reported, 3*time.Second)
	if got != final {
		t.Errorf("reported path = %q, want %q", got, final)
	}
	if !strings.HasSuffix(got, ".zip") {
		t.Errorf("unexpected reported path: %q", got)
	}
}
