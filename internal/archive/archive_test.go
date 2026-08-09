package archive

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// createTestZip creates a temporary .zip archive with the given files
// (map of path → content) and returns its path.
func createTestZip(t *testing.T, files map[string]string) string {
	t.Helper()
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "test.zip")

	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	for name, content := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return zipPath
}

func TestExtract_OK(t *testing.T) {
	t.Parallel()
	zipPath := createTestZip(t, map[string]string{
		"MyGame/game.exe": "binary",
		"MyGame/data.bin": "asset",
	})

	dest := t.TempDir()
	res, err := Extract(zipPath, dest, Options{})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	expected := filepath.Join(dest, "MyGame")
	if res.Destination != expected {
		t.Errorf("expected destination %s, got %s", expected, res.Destination)
	}
	if res.FilesExtracted != 2 {
		t.Errorf("expected 2 files extracted, got %d", res.FilesExtracted)
	}
	if res.TotalBytes == 0 {
		t.Error("expected nonzero TotalBytes")
	}
}

func TestExtractWithContext_Cancelled(t *testing.T) {
	t.Parallel()
	zipPath := createTestZip(t, map[string]string{"a.txt": "aaa"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	dest := t.TempDir()
	if _, err := ExtractWithContext(ctx, zipPath, dest, Options{}); err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

// TestExtractWithContext_FailurePreservesDestDir mirrors the extractor's
// guarantee through the archive wrapper: a failed extraction must not
// delete the caller's destination directory.
func TestExtractWithContext_FailurePreservesDestDir(t *testing.T) {
	t.Parallel()
	zipPath := createTestZip(t, map[string]string{
		"../../../evil.txt": "bad",
	})

	dest := t.TempDir()
	sentinel := filepath.Join(dest, "archive.zip")
	if err := os.WriteFile(sentinel, []byte("downloaded"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := ExtractWithContext(context.Background(), zipPath, dest, Options{}); err == nil {
		t.Fatal("expected error for zip-slip path, got nil")
	}

	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("dest dir contents were deleted on failed extraction: %v", err)
	}
}
