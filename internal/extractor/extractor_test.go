package extractor

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

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

// createTestZipWithMode creates a zip entry with a specific file mode.
func createTestZipWithMode(t *testing.T, name string, content []byte, mode os.FileMode) string {
	t.Helper()
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "mode.zip")

	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	fh := &zip.FileHeader{
		Name:   name,
		Method: zip.Deflate,
	}
	fh.SetMode(mode)
	fw, err := w.CreateHeader(fh)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return zipPath
}

// ---------------------------------------------------------------------------
// DetectArchiveType
// ---------------------------------------------------------------------------

func TestDetectArchiveType_Zip(t *testing.T) {
	t.Parallel()
	zipPath := createTestZip(t, map[string]string{"test.txt": "hello"})
	typ, err := DetectArchiveType(zipPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if typ != "zip" {
		t.Errorf("expected zip, got %s", typ)
	}
}

func TestDetectArchiveType_7z(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	// 7z magic: 37 7A BC AF 27 1C ...
	path := filepath.Join(tmp, "archive.7z")
	if err := os.WriteFile(path, []byte{
		0x37, 0x7A, 0xBC, 0xAF, 0x27, 0x1C, 0x00, 0x04,
	}, 0644); err != nil {
		t.Fatal(err)
	}

	typ, err := DetectArchiveType(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if typ != "7z" {
		t.Errorf("expected 7z, got %s", typ)
	}
}

func TestDetectArchiveType_Rar(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	// RAR magic: 52 61 72 21 1A 07 ...
	path := filepath.Join(tmp, "archive.rar")
	if err := os.WriteFile(path, []byte{
		0x52, 0x61, 0x72, 0x21, 0x1A, 0x07, 0x00,
	}, 0644); err != nil {
		t.Fatal(err)
	}

	typ, err := DetectArchiveType(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if typ != "rar" {
		t.Errorf("expected rar, got %s", typ)
	}
}

func TestDetectArchiveType_UnknownHeader(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "garbage.bin")
	if err := os.WriteFile(path, []byte("GARBAGE\x00\x01\x02"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := DetectArchiveType(path)
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
}

func TestDetectArchiveType_NotExist(t *testing.T) {
	t.Parallel()
	_, err := DetectArchiveType("/nonexistent/archive.zip")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestDetectArchiveType_ShortHeader(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "short.bin")
	// Only 3 bytes - can't match any magic.
	if err := os.WriteFile(path, []byte("PK\x03"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := DetectArchiveType(path)
	if err == nil {
		t.Fatal("expected error for truncated header")
	}
}

func TestDetectArchiveType_EmptyFile(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "empty.bin")
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}

	_, err := DetectArchiveType(path)
	if err == nil {
		t.Fatal("expected error for empty file")
	}
}

// ---------------------------------------------------------------------------
// Extract — .zip round-trip
// ---------------------------------------------------------------------------

func TestExtractZip_FilesExist(t *testing.T) {
	t.Parallel()
	zipPath := createTestZip(t, map[string]string{
		"game.exe":      "binary-content",
		"data/file.txt": "text-content",
		"saves/save.sav": "save-data",
	})

	dest := t.TempDir()
	root, err := Extract(context.Background(), zipPath, dest, nil)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	// Multiple top-level files → root is dest itself.
	if root != dest {
		t.Errorf("expected root %s, got %s", dest, root)
	}

	checks := map[string]string{
		"game.exe":       "binary-content",
		"data/file.txt":  "text-content",
		"saves/save.sav": "save-data",
	}
	for relPath, expected := range checks {
		fullPath := filepath.Join(dest, relPath)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			t.Errorf("file %s missing: %v", relPath, err)
			continue
		}
		if string(data) != expected {
			t.Errorf("file %s: expected %q, got %q", relPath, expected, string(data))
		}
	}
}

func TestExtractZip_SingleDirWrapping(t *testing.T) {
	t.Parallel()
	zipPath := createTestZip(t, map[string]string{
		"MyGame/game.exe":           "binary",
		"MyGame/saves/save01.sav":   "save-data",
		"MyGame/data/asset.dat":     "asset",
	})

	dest := t.TempDir()
	root, err := Extract(context.Background(), zipPath, dest, nil)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	// Should unwrap: root = dest/MyGame
	expected := filepath.Join(dest, "MyGame")
	if root != expected {
		t.Errorf("expected root %s, got %s", expected, root)
	}

	// Verify files are reachable from the unwrapped root.
	if _, err := os.Stat(filepath.Join(root, "game.exe")); os.IsNotExist(err) {
		t.Error("game.exe should exist in unwrapped root")
	}
	if _, err := os.Stat(filepath.Join(root, "saves", "save01.sav")); os.IsNotExist(err) {
		t.Error("save01.sav should exist in unwrapped root")
	}
}

func TestExtractZip_NoUnwrapWhenMultipleRootDirs(t *testing.T) {
	t.Parallel()
	zipPath := createTestZip(t, map[string]string{
		"GameA/game.exe": "exe-a",
		"GameB/game.exe": "exe-b",
	})

	dest := t.TempDir()
	root, err := Extract(context.Background(), zipPath, dest, nil)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	// Should NOT unwrap — two top-level dirs.
	if root != dest {
		t.Errorf("expected root %s, got %s", dest, root)
	}
}

// ---------------------------------------------------------------------------
// Zip-slip protection
// ---------------------------------------------------------------------------

func TestExtractZip_SlipSimpleRejected(t *testing.T) {
	t.Parallel()
	zipPath := createTestZip(t, map[string]string{
		"../../../etc/passwd": "evil",
	})

	dest := t.TempDir()
	_, err := Extract(context.Background(), zipPath, dest, nil)
	if err == nil {
		t.Fatal("expected error for zip-slip path, got nil")
	}
}

func TestExtractZip_SlipNestedRejected(t *testing.T) {
	t.Parallel()
	zipPath := createTestZip(t, map[string]string{
		"foo/../../../../etc/passwd": "evil",
	})

	dest := t.TempDir()
	_, err := Extract(context.Background(), zipPath, dest, nil)
	if err == nil {
		t.Fatal("expected error for nested zip-slip path, got nil")
	}
}

func TestExtractZip_AbsolutePathRejected(t *testing.T) {
	t.Parallel()
	// On Windows this could be "C:\evil.exe"; on Unix "/etc/passwd".
	zipPath := createTestZip(t, map[string]string{
		"/etc/passwd": "evil",
	})

	dest := t.TempDir()
	_, err := Extract(context.Background(), zipPath, dest, nil)
	if err == nil {
		t.Fatal("expected error for absolute path, got nil")
	}
}

// ---------------------------------------------------------------------------
// Context cancellation
// ---------------------------------------------------------------------------

func TestExtract_ContextCancelled(t *testing.T) {
	t.Parallel()
	zipPath := createTestZip(t, map[string]string{
		"a.txt": "aaa",
		"b.txt": "bbb",
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	dest := t.TempDir()
	_, err := Extract(ctx, zipPath, dest, nil)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

// ---------------------------------------------------------------------------
// Progress callback
// ---------------------------------------------------------------------------

func TestExtractZip_ProgressCallback(t *testing.T) {
	t.Parallel()
	zipPath := createTestZip(t, map[string]string{
		"a.txt": "aaa",
		"b.txt": "bbb",
		"c.txt": "ccc",
	})

	dest := t.TempDir()
	var progresses []Progress
	_, err := Extract(context.Background(), zipPath, dest, func(p Progress) {
		progresses = append(progresses, p)
	})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	if len(progresses) != 3 {
		t.Errorf("expected 3 progress callbacks, got %d", len(progresses))
	}

	// Last callback should report 3/3.
	last := progresses[len(progresses)-1]
	if last.FilesExtracted != 3 {
		t.Errorf("expected FilesExtracted=3, got %d", last.FilesExtracted)
	}
	if last.TotalFiles != 3 {
		t.Errorf("expected TotalFiles=3, got %d", last.TotalFiles)
	}
}

func TestExtractZip_ProgressWithDirs(t *testing.T) {
	t.Parallel()
	// Directories in the zip should not count toward progress.
	zipPath := createTestZip(t, map[string]string{
		"dir1/a.txt":            "aaa",
		"dir1/sub/b.txt":        "bbb",
		"dir2/c.txt":            "ccc",
	})

	dest := t.TempDir()
	count := 0
	var last Progress
	_, err := Extract(context.Background(), zipPath, dest, func(p Progress) {
		count++
		last = p
	})
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	if count != 3 {
		t.Errorf("expected 3 progress callbacks for 3 files, got %d", count)
	}
	if last.TotalFiles != 3 {
		t.Errorf("expected TotalFiles=3, got %d", last.TotalFiles)
	}
	if last.FilesExtracted != 3 {
		t.Errorf("expected FilesExtracted=3, got %d", last.FilesExtracted)
	}
}

// ---------------------------------------------------------------------------
// Cleanup on failure
// ---------------------------------------------------------------------------

func TestExtractZip_CleanupOnFailure(t *testing.T) {
	t.Parallel()
	// Create a zip with a zip-slip entry to trigger failure.
	zipPath := createTestZip(t, map[string]string{
		"good.txt":          "ok",
		"../../../evil.txt": "bad",
	})

	dest := t.TempDir()
	_, err := Extract(context.Background(), zipPath, dest, nil)
	if err == nil {
		t.Fatal("expected error due to zip-slip")
	}

	// The destination directory should have been cleaned up.
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("expected dest dir %s to be removed after failure, stat: %v", dest, err)
	}
}

// ---------------------------------------------------------------------------
// Corrupt archive
// ---------------------------------------------------------------------------

func TestExtractZip_CorruptArchive(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "corrupt.zip")
	// Write garbage that looks like a zip header but isn't valid.
	os.WriteFile(path, []byte("PK\x03\x04this is not a valid zip"), 0644)

	dest := t.TempDir()
	_, err := Extract(context.Background(), path, dest, nil)
	if err == nil {
		t.Fatal("expected error for corrupt zip, got nil")
	}
}

// ---------------------------------------------------------------------------
// External binary detection
// ---------------------------------------------------------------------------

func TestExtract7z_BinaryCheck(t *testing.T) {
	t.Parallel()
	// Create a tiny file with a valid 7z header (but not a real archive).
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.7z")
	os.WriteFile(path, []byte{
		0x37, 0x7A, 0xBC, 0xAF, 0x27, 0x1C, 0x00, 0x04,
	}, 0644)

	// Extract will pass detection then try to run 7z.
	// If 7z is installed, this will fail during extraction (not detection);
	// if not installed, we get ErrBinNotInstalled.
	dest := t.TempDir()
	_, err := Extract(context.Background(), path, dest, nil)
	if err == nil {
		t.Fatal("expected error (either bin not installed or corrupt archive)")
	}
}

func TestExtractRar_BinaryCheck(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.rar")
	os.WriteFile(path, []byte{
		0x52, 0x61, 0x72, 0x21, 0x1A, 0x07, 0x00,
	}, 0644)

	dest := t.TempDir()
	_, err := Extract(context.Background(), path, dest, nil)
	if err == nil {
		t.Fatal("expected error (either bin not installed or corrupt archive)")
	}
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestFindGameRoot_EmptyDir(t *testing.T) {
	t.Parallel()
	root := findGameRoot(t.TempDir())
	if root == "" {
		t.Error("expected non-empty root")
	}
}

func TestFindGameRoot_DotDirIgnored(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, ".hidden"), 0755)
	os.WriteFile(filepath.Join(tmp, "game.exe"), []byte("exe"), 0644)

	// Single visible subdir? No — .hidden doesn't count, but there are
	// also files at root, so it stays at root.
	root := findGameRoot(tmp)
	if root != tmp {
		t.Errorf("expected root %s when only dot-dir present, got %s", tmp, root)
	}
}

func TestFindGameRoot_OnlyDotDir(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, ".hidden", "sub"), 0755)

	// Only a dot directory at root → should NOT unwrap.
	root := findGameRoot(tmp)
	if root != tmp {
		t.Errorf("expected root %s when only dot-dir, got %s", tmp, root)
	}
}

func TestExtract_ProgressNil(t *testing.T) {
	t.Parallel()
	zipPath := createTestZip(t, map[string]string{
		"file.txt": "content",
	})

	dest := t.TempDir()
	_, err := Extract(context.Background(), zipPath, dest, nil)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
}

func TestDetectArchiveType_RealZipMisnamed(t *testing.T) {
	t.Parallel()
	// A .zip file renamed to .7z should still be detected as zip by magic.
	zipPath := createTestZip(t, map[string]string{"data.bin": "content"})
	// Rename to .7z to prove detection is magic-based, not extension-based.
	renamed := zipPath + ".renamed"
	if err := os.Rename(zipPath, renamed); err != nil {
		t.Fatal(err)
	}

	typ, err := DetectArchiveType(renamed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if typ != "zip" {
		t.Errorf("expected zip (magic-based), got %s", typ)
	}
}
