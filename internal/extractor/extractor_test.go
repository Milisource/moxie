package extractor

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
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
		"game.exe":       "binary-content",
		"data/file.txt":  "text-content",
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
		"MyGame/game.exe":         "binary",
		"MyGame/saves/save01.sav": "save-data",
		"MyGame/data/asset.dat":   "asset",
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
		"dir1/a.txt":     "aaa",
		"dir1/sub/b.txt": "bbb",
		"dir2/c.txt":     "ccc",
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

// TestExtract_FailurePreservesDestDir verifies that an extraction failure
// removes only the internal temp subdirectory and never the caller's
// destDir — which typically holds the freshly downloaded archive.
func TestExtract_FailurePreservesDestDir(t *testing.T) {
	t.Parallel()
	// Create a zip with a zip-slip entry to trigger failure.
	zipPath := createTestZip(t, map[string]string{
		"good.txt":          "ok",
		"../../../evil.txt": "bad",
	})

	dest := t.TempDir()
	// Simulate the caller's destDir holding a downloaded archive.
	sentinel := filepath.Join(dest, "MyGame_v1.0.zip")
	if err := os.WriteFile(sentinel, []byte("archive-bytes"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Extract(context.Background(), zipPath, dest, nil)
	if err == nil {
		t.Fatal("expected error due to zip-slip")
	}

	// destDir itself must survive and keep its pre-existing contents.
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("dest dir should still exist after failure: %v", err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("sentinel file in dest dir was deleted on failure: %v", err)
	}
	// The temp extraction subdirectory must be cleaned up.
	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".extract-") {
			t.Errorf("temp extraction dir %s left behind after failure", e.Name())
		}
	}
}

// TestExtract_FailurePreservesDestDir_NoSentinel is the same check without
// a sentinel file: destDir survives, temp subdir is removed.
func TestExtract_FailurePreservesDestDir_NoSentinel(t *testing.T) {
	t.Parallel()
	zipPath := createTestZip(t, map[string]string{
		"good.txt":           "ok",
		"foo/../../evil.txt": "bad",
	})

	dest := t.TempDir()
	_, err := Extract(context.Background(), zipPath, dest, nil)
	if err == nil {
		t.Fatal("expected error due to zip-slip")
	}

	if _, err := os.Stat(dest); err != nil {
		t.Errorf("dest dir should still exist after failure: %v", err)
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected dest dir to be empty after failure, got %d entries", len(entries))
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

// TestExtractZip_RejectsZipBomb verifies a crafted archive with an absurd
// compression ratio (16 MiB of zeros → ~16 KiB on disk) is rejected before
// anything is written.
func TestExtractZip_RejectsZipBomb(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "bomb.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	fw, err := w.CreateHeader(&zip.FileHeader{Name: "bomb.bin", Method: zip.Deflate})
	if err != nil {
		t.Fatal(err)
	}
	zeros := make([]byte, 16*1024*1024)
	if _, err := fw.Write(zeros); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	dest := t.TempDir()
	_, err = Extract(context.Background(), zipPath, dest, nil)
	if err == nil {
		t.Fatal("expected zip bomb rejection, got nil")
	}
	if !strings.Contains(err.Error(), "suspicious compression ratio") {
		t.Errorf("unexpected error: %v", err)
	}
	entries, err := os.ReadDir(dest)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err == nil && len(entries) != 0 {
		t.Errorf("expected nothing extracted, got %d entries", len(entries))
	}
}

// ---------------------------------------------------------------------------
// sanitizeZipPath
// ---------------------------------------------------------------------------

func TestSanitizeZipPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		path string
		want bool // true = allowed
	}{
		{"plain file", "game.exe", true},
		{"nested file", "data/file.txt", true},
		{"deeply nested", "a/b/c/d.bin", true},
		{"dots inside a segment", "Game v1.0..beta/file.bin", true},
		{"ellipsis style", "..data/secret.bin", true},
		{"trailing dots segment", "foo/.../bar.txt", true},
		{"parent traversal", "../evil", false},
		{"nested traversal", "foo/../../evil", false},
		{"traversal at end", "foo/..", false},
		{"windows traversal", `..\evil.exe`, false},
		{"windows nested traversal", `foo\..\..\evil.exe`, false},
		{"absolute unix", "/etc/passwd", false},
		{"absolute windows", `C:\evil.exe`, false},
		{"empty name", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sanitizeZipPath(tt.path)
			if tt.want && err != nil {
				t.Errorf("sanitizeZipPath(%q) = %v, want allowed", tt.path, err)
			}
			if !tt.want && err == nil {
				t.Errorf("sanitizeZipPath(%q) = nil, want ErrPathTraversal", tt.path)
			}
		})
	}
}

// TestExtractZip_DotsInSegmentAllowed verifies that names containing ".."
// inside a longer segment (e.g. a version like "v1.0..beta") extract fine.
func TestExtractZip_DotsInSegmentAllowed(t *testing.T) {
	t.Parallel()
	zipPath := createTestZip(t, map[string]string{
		"Game v1.0..beta/file.bin": "content",
	})

	dest := t.TempDir()
	root, err := Extract(context.Background(), zipPath, dest, nil)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	expected := filepath.Join(dest, "Game v1.0..beta")
	if root != expected {
		t.Errorf("expected root %s, got %s", expected, root)
	}
	data, err := os.ReadFile(filepath.Join(root, "file.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "content" {
		t.Errorf("expected %q, got %q", "content", string(data))
	}
}

// ---------------------------------------------------------------------------
// Checksum/CRC verification
// ---------------------------------------------------------------------------

// TestExtractZip_ChecksumError verifies that a zip entry whose data fails
// its CRC check (detected by archive/zip on Close) is treated as an
// extraction failure instead of being silently ignored.
func TestExtractZip_ChecksumError(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "corrupt-crc.zip")

	// Write a stored (uncompressed) entry so its payload appears verbatim
	// in the archive and can be corrupted in place.
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	fw, err := w.CreateHeader(&zip.FileHeader{Name: "data.bin", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("hello world, this payload must be longer than 12 bytes")
	if _, err := fw.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	// Flip one byte of the stored payload so the CRC no longer matches.
	raw, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	idx := bytes.Index(raw, payload)
	if idx < 0 {
		t.Fatal("payload not found in stored zip entry")
	}
	raw[idx+1] ^= 0xFF
	if err := os.WriteFile(zipPath, raw, 0644); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	if _, err := Extract(context.Background(), zipPath, dest, nil); err == nil {
		t.Fatal("expected checksum error for corrupted zip entry, got nil")
	}
	// The failure must not have wiped destDir.
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("dest dir should still exist after failure: %v", err)
	}
}
