package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Installing a local image must write the cover, lock it against syncs, and
// build the grid thumbnail.
func TestSetGameCoverFromFileInstallsAndLocks(t *testing.T) {
	coverDir := testCoverDir(t)
	a := newTestApp(t)
	id := addGame(t, a, "Game", "/games/game")

	src := filepath.Join(t.TempDir(), "cover.png")
	if err := os.WriteFile(src, makePNG(t, 600, 900), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := a.SetGameCoverFromFile(id, src); err != nil {
		t.Fatalf("SetGameCoverFromFile: %v", err)
	}

	coverPath := filepath.Join(coverDir, "1")
	if _, err := os.Stat(coverPath); err != nil {
		t.Fatalf("cover not written: %v", err)
	}
	if _, err := os.Stat(coverPath + ".thumb"); err != nil {
		t.Errorf("thumbnail not written: %v", err)
	}
	m, ok := readCoverMeta(coverPath)
	if !ok {
		t.Fatal("cover meta not written")
	}
	if m.Source != "manual" {
		t.Errorf("Source = %q, want manual", m.Source)
	}
	if !m.Locked {
		t.Error("manually installed cover must be locked")
	}
	if m.W != 600 || m.H != 900 {
		t.Errorf("size = %dx%d, want 600x900", m.W, m.H)
	}
}

func TestSetGameCoverFromFileRejectsNonImage(t *testing.T) {
	testCoverDir(t)
	a := newTestApp(t)
	id := addGame(t, a, "Game", "/games/game")

	src := filepath.Join(t.TempDir(), "not-an-image.txt")
	if err := os.WriteFile(src, []byte("hello, not an image"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := a.SetGameCoverFromFile(id, src); err == nil {
		t.Fatal("expected error for a non-image file, got nil")
	}
	if _, err := os.Stat(coverPathFor(id)); err == nil {
		t.Error("a rejected file must not be installed as a cover")
	}
}

func TestSetGameCoverFromFileRejectsMissingFile(t *testing.T) {
	testCoverDir(t)
	a := newTestApp(t)
	id := addGame(t, a, "Game", "/games/game")

	if err := a.SetGameCoverFromFile(id, filepath.Join(t.TempDir(), "nope.png")); err == nil {
		t.Fatal("expected error for a missing file, got nil")
	}
}

func TestPreviewCoverFileReturnsDataURL(t *testing.T) {
	testCoverDir(t)
	a := newTestApp(t)

	src := filepath.Join(t.TempDir(), "cover.png")
	if err := os.WriteFile(src, makePNG(t, 4, 4), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := a.PreviewCoverFile(src)
	if err != nil {
		t.Fatalf("PreviewCoverFile: %v", err)
	}
	if !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Errorf("preview = %q, want a png data URL", got)
	}
}

func TestPreviewCoverFileRejectsNonImage(t *testing.T) {
	testCoverDir(t)
	a := newTestApp(t)

	src := filepath.Join(t.TempDir(), "garbage.bin")
	if err := os.WriteFile(src, []byte("not an image at all"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := a.PreviewCoverFile(src); err == nil {
		t.Fatal("expected error for a non-image file, got nil")
	}
}

// The manual URL path accepts http and https but nothing else. Non-http(s)
// schemes are rejected before any network call.
func TestSetGameCoverRejectsNonHTTPScheme(t *testing.T) {
	testCoverDir(t)
	a := newTestApp(t)
	id := addGame(t, a, "Game", "/games/game")

	for _, bad := range []string{"", "   ", "ftp://example.com/c.png", "file:///tmp/c.png", "example.com/c.png"} {
		if err := a.SetGameCover(id, bad, "manual"); err == nil {
			t.Errorf("SetGameCover(%q): expected error, got nil", bad)
		}
	}
}
