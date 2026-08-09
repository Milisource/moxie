// Package extractor extracts game archives (.zip, .7z, .rar) to a
// temporary directory, with support for zip-slip protection, context
// cancellation, progress callbacks, and single-folder unwrapping suitable
// for passing directly to updater.Merge().
package extractor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Common errors returned by this package.
var (
	ErrUnknownFormat   = errors.New("unknown or unsupported archive format")
	ErrCorruptArchive  = errors.New("archive is corrupt or invalid")
	ErrBinNotInstalled = errors.New("required archive tool not installed")
	ErrPathTraversal   = errors.New("path traversal detected in archive")
)

// Progress reports extraction progress.
type Progress struct {
	FilesExtracted int
	TotalFiles     int
	CurrentFile    string
}

// ProgressFunc is called periodically during extraction.
type ProgressFunc func(Progress)

// DetectArchiveType detects the archive type by reading magic bytes.
// Returns "zip", "7z", "rar", or an error if unknown/corrupt.
func DetectArchiveType(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	header := make([]byte, 8)
	if _, err := f.Read(header); err != nil {
		return "", fmt.Errorf("read header: %w", err)
	}

	// .zip: starts with PK\x03\x04
	if len(header) >= 4 && header[0] == 'P' && header[1] == 'K' && header[2] == 0x03 && header[3] == 0x04 {
		return "zip", nil
	}
	// .7z: starts with 7z\xBC\xAF\x27\x1C
	if len(header) >= 6 && header[0] == '7' && header[1] == 'z' && header[2] == 0xBC && header[3] == 0xAF && header[4] == 0x27 && header[5] == 0x1C {
		return "7z", nil
	}
	// .rar: starts with Rar!\x1A\x07
	if len(header) >= 6 && header[0] == 'R' && header[1] == 'a' && header[2] == 'r' && header[3] == '!' && header[4] == 0x1A && header[5] == 0x07 {
		return "rar", nil
	}

	return "", fmt.Errorf("%w: %s", ErrUnknownFormat, path)
}

// Extract extracts an archive to destDir and returns the extracted root
// path suitable for passing to updater.Merge().
//
// destDir should be a temporary directory created by the caller (e.g.
// via os.MkdirTemp). The returned path handles single-folder wrapping:
// if the archive contains exactly one top-level directory, the returned
// path points into that directory rather than destDir itself.
//
// Extraction happens in a fresh temporary subdirectory inside destDir.
// If extraction fails mid-way, only that temporary subdirectory is
// removed — destDir and anything the caller already placed in it (such
// as the freshly downloaded archive) are left untouched. On success the
// extracted files are moved into destDir, so callers see the same result
// as before.
//
// Accepts a context for cancellation and an optional progress callback.
func Extract(ctx context.Context, archivePath, destDir string, progress ProgressFunc) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	typ, err := DetectArchiveType(archivePath)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", fmt.Errorf("create dest dir: %w", err)
	}

	// Extract into a fresh temp subdirectory so a failure mid-way can
	// never wipe the caller's destDir (which may hold the downloaded
	// archive). Only this subdirectory is removed on error.
	tmpDir, err := os.MkdirTemp(destDir, ".extract-*")
	if err != nil {
		return "", fmt.Errorf("create temp extraction dir: %w", err)
	}

	switch typ {
	case "zip":
		err = extractZip(ctx, archivePath, tmpDir, progress)
	case "7z":
		err = extract7z(ctx, archivePath, tmpDir)
	case "rar":
		err = extractRar(ctx, archivePath, tmpDir)
	default:
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("%w: %s", ErrUnknownFormat, typ)
	}
	if err != nil {
		// Clean up partial extraction — only the temp subdirectory, never
		// the caller's destDir.
		os.RemoveAll(tmpDir)
		return "", err
	}

	// Move the extracted files into destDir, then drop the now-empty temp
	// subdirectory. Success semantics are unchanged: files end up in destDir.
	if err := moveContents(tmpDir, destDir); err != nil {
		os.RemoveAll(tmpDir)
		return "", err
	}
	os.RemoveAll(tmpDir)

	return findGameRoot(destDir), nil
}

// moveContents moves every entry of src into dst via rename. Both paths
// live on the same filesystem (src was created inside dst), so each move
// is a metadata-only operation.
func moveContents(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("read extraction dir: %w", err)
	}
	for _, e := range entries {
		if err := os.Rename(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return fmt.Errorf("move %s into destination: %w", e.Name(), err)
		}
	}
	return nil
}

// findGameRoot checks if dir contains exactly one non-hidden subdirectory;
// if so, returns that subdirectory (the actual game root inside the extraction).
// This mirrors updater.findGameRoot to avoid a circular dependency.
func findGameRoot(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return dir
	}
	var subdirs []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			subdirs = append(subdirs, e.Name())
		}
	}
	if len(subdirs) == 1 {
		return filepath.Join(dir, subdirs[0])
	}
	return dir
}
