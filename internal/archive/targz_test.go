package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// createTestTarGz creates a tar.gz file at the given path with the specified files.
// files is a map of filename -> content.
func createTestTarGz(t *testing.T, path string, files map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Size: int64(len(content)),
			Mode: 0644,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("failed to write tar header for %q: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("failed to write tar content for %q: %v", name, err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("failed to close tar writer: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("failed to close gzip writer: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatalf("failed to write tar.gz file: %v", err)
	}
}

// createTestTarGzWithDirs creates a tar.gz with directory entries.
func createTestTarGzWithDirs(t *testing.T, path string, dirs []string, files map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	for _, d := range dirs {
		hdr := &tar.Header{
			Name:     d,
			Typeflag: tar.TypeDir,
			Mode:     0755,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("failed to write dir header for %q: %v", d, err)
		}
	}

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Size: int64(len(content)),
			Mode: 0644,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("failed to write tar header for %q: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("failed to write tar content for %q: %v", name, err)
		}
	}

	tw.Close()
	gzw.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatalf("failed to write tar.gz: %v", err)
	}
}

// ---------------------------------------------------------------------------
// extractTarGz - basic extraction
// ---------------------------------------------------------------------------

func TestExtractTarGz_Basic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tgzPath := filepath.Join(dir, "test.tar.gz")

	files := map[string]string{
		"hello.txt":     "Hello, World!",
		"subdir/note.md": "# Markdown",
		"README":        "readme content",
	}
	createTestTarGz(t, tgzPath, files)

	result, err := Extract(tgzPath, dir, Options{})
	if err != nil {
		t.Fatalf("Extract tar.gz failed: %v", err)
	}

	if result.FilesExtracted != 3 {
		t.Errorf("expected 3 files extracted, got %d", result.FilesExtracted)
	}
	if result.TotalBytes <= 0 {
		t.Errorf("expected positive TotalBytes, got %d", result.TotalBytes)
	}

	for name, content := range files {
		fullPath := filepath.Join(result.Destination, name)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			t.Errorf("failed to read %q: %v", name, err)
			continue
		}
		if string(data) != content {
			t.Errorf("extracted %q = %q, want %q", name, string(data), content)
		}
	}
}

func TestExtractTarGz_WithDirectories(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tgzPath := filepath.Join(dir, "dirs.tar.gz")

	createTestTarGzWithDirs(t, tgzPath,
		[]string{"app/", "app/data/", "app/data/sub/"},
		map[string]string{
			"app/game.exe":             "binary",
			"app/data/file1.dat":       "data1",
			"app/data/sub/file2.dat":   "data2",
		},
	)

	result, err := Extract(tgzPath, dir, Options{})
	if err != nil {
		t.Fatalf("Extract tar.gz with dirs failed: %v", err)
	}

	if result.FilesExtracted != 3 {
		t.Errorf("expected 3 files, got %d", result.FilesExtracted)
	}

	// Verify directory structure was created
	for _, f := range []string{"app/game.exe", "app/data/file1.dat", "app/data/sub/file2.dat"} {
		fullPath := filepath.Join(result.Destination, f)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			t.Errorf("expected file %q to exist", f)
		}
	}
}

func TestExtractTarGz_EmptyArchive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tgzPath := filepath.Join(dir, "empty.tar.gz")

	// Create empty tar.gz
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	tw.Close()
	gzw.Close()
	if err := os.WriteFile(tgzPath, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := Extract(tgzPath, dir, Options{})
	if err != nil {
		t.Fatalf("Extract empty tar.gz failed: %v", err)
	}
	if result.FilesExtracted != 0 {
		t.Errorf("expected 0 files from empty tar.gz, got %d", result.FilesExtracted)
	}
}

// ---------------------------------------------------------------------------
// Path traversal protection
// ---------------------------------------------------------------------------

func TestExtractTarGz_TraversalProtection(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tgzPath := filepath.Join(dir, "traversal.tar.gz")

	// Create tar.gz with path traversal
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	hdr := &tar.Header{
		Name: "../../evil.txt",
		Size: int64(len("malicious")),
		Mode: 0644,
	}
	tw.WriteHeader(hdr)
	tw.Write([]byte("malicious"))
	tw.Close()
	gzw.Close()
	os.WriteFile(tgzPath, buf.Bytes(), 0644)

	_, err := Extract(tgzPath, dir, Options{})
	if err == nil {
		t.Fatal("expected error for path traversal in tar.gz, got nil")
	}
	if !strings.Contains(err.Error(), "invalid file path") {
		t.Errorf("expected 'invalid file path' error, got: %v", err)
	}
}

func TestExtractTarGz_AbsolutePathStaysInDest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tgzPath := filepath.Join(dir, "abs.tar.gz")

	// On Linux, filepath.Join joins absolute paths as relative components.
	// "/etc/passwd" → "destDir/etc/passwd" (stays within destDir).
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	hdr := &tar.Header{
		Name: "/etc/passwd",
		Size: int64(len("not actually /etc/passwd")),
		Mode: 0644,
	}
	tw.WriteHeader(hdr)
	tw.Write([]byte("not actually /etc/passwd"))
	tw.Close()
	gzw.Close()
	os.WriteFile(tgzPath, buf.Bytes(), 0644)

	// This should succeed — the path stays within destDir
	result, err := Extract(tgzPath, dir, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(result.Destination, dir) {
		t.Errorf("expected destination under %q, got %q", dir, result.Destination)
	}
	// Verify file exists at destDir/etc/passwd
	extractedPath := filepath.Join(result.Destination, "etc/passwd")
	if _, err := os.Stat(extractedPath); os.IsNotExist(err) {
		t.Errorf("expected extracted file at %q", extractedPath)
	}
}

// ---------------------------------------------------------------------------
// Progress callback
// ---------------------------------------------------------------------------

func TestExtractTarGz_ProgressCallback(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tgzPath := filepath.Join(dir, "prog.tar.gz")

	files := map[string]string{
		"a.txt": "aaa",
		"b.txt": "bbb",
		"c.txt": "ccc",
	}
	createTestTarGz(t, tgzPath, files)

	var progressCount int
	result, err := Extract(tgzPath, dir, Options{
		OnProgress: func(totalFiles, extractedFiles int, currentFile string, bytesProcessed, bytesTotal int64) {
			progressCount++
		},
	})
	if err != nil {
		t.Fatalf("Extract tar.gz with progress failed: %v", err)
	}

	if result.FilesExtracted != 3 {
		t.Errorf("expected 3 files, got %d", result.FilesExtracted)
	}
	if progressCount != 3 {
		t.Errorf("expected 3 progress callbacks, got %d", progressCount)
	}
}

// ---------------------------------------------------------------------------
// Invalid file
// ---------------------------------------------------------------------------

func TestExtractTarGz_InvalidFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.tar.gz")
	if err := os.WriteFile(path, []byte("not a valid tar.gz"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Extract(path, dir, Options{})
	if err == nil {
		t.Fatal("expected error for corrupt tar.gz, got nil")
	}
}

func TestExtractTarGz_SingleFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tgzPath := filepath.Join(dir, "single.tar.gz")

	// Single file at root
	files := map[string]string{
		"game.exe": "binary content here",
	}
	createTestTarGz(t, tgzPath, files)

	result, err := Extract(tgzPath, dir, Options{})
	if err != nil {
		t.Fatalf("Extract single file tar.gz failed: %v", err)
	}

	if result.FilesExtracted != 1 {
		t.Errorf("expected 1 file, got %d", result.FilesExtracted)
	}

	fullPath := filepath.Join(result.Destination, "game.exe")
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		t.Errorf("expected game.exe to exist at %q", fullPath)
	}
}
