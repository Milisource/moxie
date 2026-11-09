package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// createTestZip creates a .zip archive at zipPath with the given files
// (map of path → content).
func createTestZip(t *testing.T, zipPath string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(zipPath), 0755); err != nil {
		t.Fatal(err)
	}

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
}

// createTestTarGz creates a .tar.gz archive at tgzPath with the given files
// (map of path → content).
func createTestTarGz(t *testing.T, tgzPath string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(tgzPath), 0755); err != nil {
		t.Fatal(err)
	}

	f, err := os.Create(tgzPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		hdr := &tar.Header{Name: name, Mode: 0644, Size: int64(len(content))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func TestExtract_OK(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "test.zip")
	createTestZip(t, zipPath, map[string]string{
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
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "cancel.zip")
	createTestZip(t, zipPath, map[string]string{"a.txt": "aaa"})

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
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "evil.zip")
	createTestZip(t, zipPath, map[string]string{
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

// ---------------------------------------------------------------------------
// verifyExtractedTree
// ---------------------------------------------------------------------------

func TestVerifyExtractedTree_AcceptsRegularFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "persistent"), []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "save.dat"), []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyExtractedTree(dir); err != nil {
		t.Errorf("verifyExtractedTree = %v, want nil", err)
	}
}

func TestVerifyExtractedTree_RejectsSymlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "persistent"), []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(dir, "evil.save")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	err := verifyExtractedTree(dir)
	if err == nil {
		t.Fatal("verifyExtractedTree should reject a symlink entry")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("error = %v, want it to mention symlink", err)
	}
	if !strings.Contains(err.Error(), "evil.save") {
		t.Errorf("error = %v, want it to name the offending entry", err)
	}
}
