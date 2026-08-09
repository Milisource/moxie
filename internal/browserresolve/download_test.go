package browserresolve

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// launchFlags
// ---------------------------------------------------------------------------

func TestLaunchFlagsHeadless(t *testing.T) {
	t.Parallel()
	got := launchFlags("/tmp/profile-copy", false)
	want := []string{
		"--user-data-dir=/tmp/profile-copy",
		"--no-first-run",
		"--disable-blink-features=AutomationControlled",
		"--headless=new",
	}
	if !slices.Equal(got, want) {
		t.Errorf("launchFlags(headless) = %v, want %v", got, want)
	}
}

func TestLaunchFlagsHeadful(t *testing.T) {
	t.Parallel()
	got := launchFlags("/tmp/profile-copy", true)
	for _, arg := range got {
		if strings.HasPrefix(arg, "--headless") {
			t.Errorf("headful launch must not contain %q (got %v)", arg, got)
		}
	}
	if len(got) != 3 {
		t.Errorf("launchFlags(headful) = %v, want 3 flags", got)
	}
}

func TestLaunchFlagsAlwaysPinUserDataDir(t *testing.T) {
	t.Parallel()
	for _, headful := range []bool{false, true} {
		args := launchFlags("/tmp/pinned", headful)
		want := "--user-data-dir=/tmp/pinned"
		if !slices.Contains(args, want) {
			t.Errorf("launchFlags(headful=%v) missing %q: %v", headful, want, args)
		}
	}
}

// ---------------------------------------------------------------------------
// downloadTracker
// ---------------------------------------------------------------------------

func TestDownloadTracker_WillBeginThenProgress(t *testing.T) {
	t.Parallel()
	tr := newDownloadTracker()
	if tr.finished() {
		t.Fatal("fresh tracker must not be finished")
	}
	tr.onWillBegin("guid-1", "game.zip")
	tr.onProgress("guid-1", downloadInProgress, 500, 1000)
	tr.onProgress("guid-1", downloadCompleted, 1000, 1000)

	snap := tr.snapshot()
	if !snap.completed() {
		t.Errorf("state = %v, want completed", snap.state)
	}
	if snap.guid != "guid-1" || snap.suggested != "game.zip" {
		t.Errorf("snapshot = %+v, want guid=guid-1 suggested=game.zip", snap)
	}
	if snap.received != 1000 || snap.total != 1000 {
		t.Errorf("bytes = %d/%d, want 1000/1000", snap.received, snap.total)
	}
	if snap.err != nil {
		t.Errorf("unexpected error %v", snap.err)
	}
	if !tr.finished() {
		t.Error("tracker must report finished after completion")
	}
}

// rod#971: some download flows never emit downloadWillBegin. Progress alone
// must still drive the tracker to completion and adopt the GUID so the file
// can be located.
func TestDownloadTracker_ProgressWithoutWillBegin(t *testing.T) {
	t.Parallel()
	tr := newDownloadTracker()
	tr.onProgress("orphan-guid", downloadInProgress, 10, 100)
	tr.onProgress("orphan-guid", downloadCompleted, 100, 100)

	snap := tr.snapshot()
	if !snap.completed() {
		t.Errorf("state = %v, want completed", snap.state)
	}
	if snap.guid != "orphan-guid" {
		t.Errorf("guid = %q, want adopted from progress", snap.guid)
	}
	if snap.suggested != "" {
		t.Errorf("suggested = %q, want empty (no willBegin arrived)", snap.suggested)
	}
}

func TestDownloadTracker_Canceled(t *testing.T) {
	t.Parallel()
	tr := newDownloadTracker()
	tr.onWillBegin("guid-1", "a.bin")
	tr.onProgress("guid-1", downloadCanceled, 10, 0)

	snap := tr.snapshot()
	if snap.completed() {
		t.Error("canceled download must not report completed")
	}
	if snap.err == nil {
		t.Error("canceled download must record an error")
	}
	if !tr.finished() {
		t.Error("canceled download is a terminal state")
	}
}

func TestDownloadTracker_FirstWillBeginWins(t *testing.T) {
	t.Parallel()
	tr := newDownloadTracker()
	tr.onWillBegin("guid-1", "a.bin")
	tr.onWillBegin("guid-2", "b.bin") // racing second download
	tr.onProgress("guid-2", downloadCompleted, 5, 5)

	snap := tr.snapshot()
	if snap.guid != "guid-1" {
		t.Errorf("guid = %q, want first-wins guid-1", snap.guid)
	}
	if snap.suggested != "a.bin" {
		t.Errorf("suggested = %q, want a.bin", snap.suggested)
	}
	if snap.completed() {
		t.Error("completion of a later download must not mark the tracked one complete")
	}
}

// ---------------------------------------------------------------------------
// verifyDownloadedFile
// ---------------------------------------------------------------------------

func TestVerifyDownloadedFile(t *testing.T) {
	t.Parallel()
	html := []byte("<!DOCTYPE html>\n<html><body>challenge</body></html>")
	spaceyHTML := []byte("  \n <HTML>just checking</HTML>")
	bomHTML := []byte("\xef\xbb\xbf<html><title>captcha</title></html>")
	binary := []byte{0x50, 0x4b, 0x03, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x1a, 0x00}

	tests := []struct {
		name         string
		content      []byte
		minSize      int64
		expectedSize int64
		wantErr      error
	}{
		{"binary ok", binary, 0, 0, nil},
		{"size match ok", binary, 0, int64(len(binary)), nil},
		{"empty file", []byte{}, 0, 0, ErrDownload},
		{"below min size", binary, int64(len(binary)) + 1, 0, ErrDownload},
		{"size mismatch", binary, 0, int64(len(binary)) + 1, ErrDownload},
		{"html page", html, 0, 0, ErrChallengePage},
		{"spacey html", spaceyHTML, 0, 0, ErrChallengePage},
		{"bom html", bomHTML, 0, 0, ErrChallengePage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "file")
			if err := os.WriteFile(path, tt.content, 0o600); err != nil {
				t.Fatal(err)
			}
			err := verifyDownloadedFile(path, tt.minSize, tt.expectedSize)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("verifyDownloadedFile = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("verifyDownloadedFile = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestVerifyDownloadedFile_MissingFile(t *testing.T) {
	t.Parallel()
	err := verifyDownloadedFile(filepath.Join(t.TempDir(), "nope"), 0, 0)
	if !errors.Is(err, ErrDownload) {
		t.Fatalf("expected ErrDownload, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// sanitizeFilename
// ---------------------------------------------------------------------------

func TestSanitizeFilename(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want string
	}{
		{"game.zip", "game.zip"},
		{"Game v1.2.3.zip", "Game v1.2.3.zip"},
		{"../../etc/passwd", "passwd"},
		{"..", ""},
		{".", ""},
		{"", ""},
		{"   ", ""},
		{".hidden", "hidden"},
		{"C:\\Users\\evil\\file.txt", "file.txt"},
		{"dir/name.tar.gz", "name.tar.gz"},
		{"a\x00b\x1f.zip", "ab.zip"},
		{"日本語.zip", "日本語.zip"},
		{"name:with*chars?.zip", "namewithchars.zip"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.in), func(t *testing.T) {
			if got := sanitizeFilename(tt.in); got != tt.want {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// moveFile
// ---------------------------------------------------------------------------

func TestMoveFileBasic(t *testing.T) {
	t.Parallel()
	src := filepath.Join(t.TempDir(), "src.bin")
	writeFile(t, src, "payload")
	destDir := filepath.Join(t.TempDir(), "games")

	got, err := moveFile(src, destDir, "game.bin")
	if err != nil {
		t.Fatalf("moveFile: %v", err)
	}
	want := filepath.Join(destDir, "game.bin")
	if got != want {
		t.Errorf("moveFile = %q, want %q", got, want)
	}
	assertFileContent(t, want, "payload")
	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("source %q must be gone after move, stat err = %v", src, err)
	}
}

func TestMoveFileCollision(t *testing.T) {
	t.Parallel()
	src := filepath.Join(t.TempDir(), "src.zip")
	writeFile(t, src, "new")
	destDir := t.TempDir()
	writeFile(t, filepath.Join(destDir, "game.zip"), "old")
	writeFile(t, filepath.Join(destDir, "game (1).zip"), "old-1")

	got, err := moveFile(src, destDir, "game.zip")
	if err != nil {
		t.Fatalf("moveFile: %v", err)
	}
	if want := filepath.Join(destDir, "game (2).zip"); got != want {
		t.Errorf("moveFile = %q, want %q", got, want)
	}
	assertFileContent(t, got, "new")
	// Existing files untouched.
	assertFileContent(t, filepath.Join(destDir, "game.zip"), "old")
	assertFileContent(t, filepath.Join(destDir, "game (1).zip"), "old-1")
}

func TestMoveFileCreatesDestDir(t *testing.T) {
	t.Parallel()
	src := filepath.Join(t.TempDir(), "src")
	writeFile(t, src, "x")
	destDir := filepath.Join(t.TempDir(), "a", "b", "c") // does not exist
	got, err := moveFile(src, destDir, "x")
	if err != nil {
		t.Fatalf("moveFile: %v", err)
	}
	assertFileContent(t, got, "x")
}

// ---------------------------------------------------------------------------
// stableDownloadFile
// ---------------------------------------------------------------------------

func TestStableDownloadFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Nothing yet.
	if _, next, ok := stableDownloadFile(dir, ""); ok || next != "" {
		t.Fatalf("empty dir: ok=%v next=%q, want ok=false next=", ok, next)
	}

	// Partial downloads are ignored.
	writeFile(t, filepath.Join(dir, "abc.crdownload"), "partial")
	writeFile(t, filepath.Join(dir, "abc.download"), "partial")
	if _, next, ok := stableDownloadFile(dir, ""); ok || next != "" {
		t.Fatalf("partial files must be ignored: ok=%v next=%q", ok, next)
	}

	// First observation records the name but does not confirm stability.
	writeFile(t, filepath.Join(dir, "guid-1"), "finished")
	_, next, ok := stableDownloadFile(dir, "")
	if ok || next != "guid-1" {
		t.Fatalf("first tick: ok=%v next=%q, want ok=false next=guid-1", ok, next)
	}

	// Same name on the next tick = stable.
	path, _, ok := stableDownloadFile(dir, next)
	if !ok || filepath.Base(path) != "guid-1" {
		t.Fatalf("second tick: path=%q ok=%v, want guid-1 stable", path, ok)
	}

	// A name that disappeared (was moved/removed) can never go stable.
	if _, _, ok := stableDownloadFile(dir, "vanished"); ok {
		t.Fatal("a vanished name must not be reported stable")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Errorf("%s content = %q, want %q", path, got, want)
	}
}
