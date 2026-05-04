package archive

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func createTestZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("failed to create zip entry %q: %v", name, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("failed to write zip entry %q: %v", name, err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("failed to close zip writer: %v", err)
	}

	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatalf("failed to write zip file: %v", err)
	}
}

// ---------------------------------------------------------------------------
// extractZip - basic extraction
// ---------------------------------------------------------------------------

func TestExtractZip_Basic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "test.zip")

	files := map[string]string{
		"hello.txt":         "Hello, World!",
		"subdir/note.md":    "# Markdown content",
		"README":            "readme content",
	}
	createTestZip(t, zipPath, files)

	result, err := Extract(zipPath, dir, Options{})
	if err != nil {
		t.Fatalf("Extract zip failed: %v", err)
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
			t.Errorf("failed to read extracted %q: %v", name, err)
			continue
		}
		if string(data) != content {
			t.Errorf("extracted %q content = %q, want %q", name, string(data), content)
		}
	}
}

func TestExtractZip_DirectoryStructure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "game.zip")

	files := map[string]string{
		"game.exe":                "binary data",
		"data/file1.dat":          "data1",
		"data/sub/file2.dat":      "data2",
		"data/sub/deep/file3.txt": "deep",
	}
	createTestZip(t, zipPath, files)

	result, err := Extract(zipPath, dir, Options{})
	if err != nil {
		t.Fatalf("Extract zip failed: %v", err)
	}

	for name := range files {
		fullPath := filepath.Join(result.Destination, name)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			t.Errorf("expected extracted file %q to exist at %q", name, fullPath)
		}
	}
}

func TestExtractZip_EmptyArchive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "empty.zip")

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	w.Close()
	if err := os.WriteFile(zipPath, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := Extract(zipPath, dir, Options{})
	if err != nil {
		t.Fatalf("Extract empty zip failed: %v", err)
	}
	if result.FilesExtracted != 0 {
		t.Errorf("expected 0 files from empty zip, got %d", result.FilesExtracted)
	}
}

func TestExtractZip_WithDirectoryEntries(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "withdirs.zip")

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	// Add files with path prefixes (implicit directories)
	f1, _ := w.Create("app/file.txt")
	f1.Write([]byte("content"))
	f2, _ := w.Create("app/data/config.txt")
	f2.Write([]byte("config"))
	w.Close()
	if err := os.WriteFile(zipPath, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := Extract(zipPath, dir, Options{})
	if err != nil {
		t.Fatalf("Extract zip with dirs failed: %v", err)
	}
	// Both files should be extracted
	if result.FilesExtracted != 2 {
		t.Errorf("expected 2 files, got %d", result.FilesExtracted)
	}

	// Verify the files exist in subdirectories
	for _, f := range []string{"app/file.txt", "app/data/config.txt"} {
		if _, err := os.Stat(filepath.Join(result.Destination, f)); os.IsNotExist(err) {
			t.Errorf("expected extracted file %q to exist", f)
		}
	}
}

// ---------------------------------------------------------------------------
// Zip-slip protection
// ---------------------------------------------------------------------------

func TestExtractZip_ZipSlipProtection(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "malicious.zip")

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Create("../../evil.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("malicious content"))
	w.Close()
	if err := os.WriteFile(zipPath, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}

	_, err = Extract(zipPath, dir, Options{})
	if err == nil {
		t.Fatal("expected error for zip-slip attempt, got nil")
	}
	if !strings.Contains(err.Error(), "invalid file path") {
		t.Errorf("expected 'invalid file path' error, got: %v", err)
	}
}

func TestExtractZip_ZipSlipNested(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "malicious2.zip")

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("subdir/../../../../secret.txt")
	f.Write([]byte("secret"))
	w.Close()
	if err := os.WriteFile(zipPath, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Extract(zipPath, dir, Options{})
	if err == nil {
		t.Fatal("expected error for nested zip-slip, got nil")
	}
}

func TestExtractZip_AbsolutePathStaysInDest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "abspath.zip")

	// An absolute path entry in a zip ("/etc/passwd") is joined with destDir
	// via filepath.Join, which on Linux produces "destDir/etc/passwd" — staying
	// safely within destDir. This is not a vulnerability.
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("/etc/passwd")
	f.Write([]byte("not actually /etc/passwd"))
	w.Close()
	if err := os.WriteFile(zipPath, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := Extract(zipPath, dir, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The file should be under destDir
	if !strings.HasPrefix(result.Destination, dir) {
		t.Errorf("expected destination %q to be under %q", result.Destination, dir)
	}

	// Verify file exists at destDir/etc/passwd
	extractedPath := filepath.Join(result.Destination, "etc/passwd")
	if _, err := os.Stat(extractedPath); os.IsNotExist(err) {
		t.Errorf("expected extracted file at %q", extractedPath)
	}
}

// ---------------------------------------------------------------------------
// extractZip with progress callback
// ---------------------------------------------------------------------------

func TestExtractZip_ProgressCallback(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "progress.zip")

	files := map[string]string{
		"a.txt": "aaa",
		"b.txt": "bbb",
		"c.txt": "ccc",
	}
	createTestZip(t, zipPath, files)

	var progressCalls int
	result, err := Extract(zipPath, dir, Options{
		OnProgress: func(totalFiles, extractedFiles int, currentFile string, bytesProcessed, bytesTotal int64) {
			progressCalls++
		},
	})
	if err != nil {
		t.Fatalf("Extract zip with progress failed: %v", err)
	}

	if result.FilesExtracted != 3 {
		t.Errorf("expected 3 files, got %d", result.FilesExtracted)
	}
	if progressCalls != 3 {
		t.Errorf("expected 3 progress callbacks, got %d", progressCalls)
	}
}

// ---------------------------------------------------------------------------
// extractZip - invalid zip file
// ---------------------------------------------------------------------------

func TestExtractZip_InvalidFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.zip")
	if err := os.WriteFile(path, []byte("not a zip file"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Extract(path, dir, Options{})
	if err == nil {
		t.Fatal("expected error for corrupt zip, got nil")
	}
}
