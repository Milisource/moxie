package extractor

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// extractZip extracts a .zip archive to destDir using Go's standard library.
// It reports per-file progress via the progress callback if provided.
func extractZip(ctx context.Context, archivePath, destDir string, progress ProgressFunc) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	// Count total non-directory files for progress reporting.
	var files []*zip.File
	var totalUncompressed int64
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		files = append(files, f)
		totalUncompressed += int64(f.UncompressedSize64)
	}
	total := len(files)

	// Zip bomb defense: reject archives whose declared total decompressed
	// size is absurd before writing anything to disk.
	if totalUncompressed > maxZipTotalBytes {
		return fmt.Errorf("zip total uncompressed size %d bytes exceeds limit of %d bytes", totalUncompressed, maxZipTotalBytes)
	}

	for i, f := range files {
		if err := ctx.Err(); err != nil {
			return err
		}

		if err := sanitizeZipPath(f.Name); err != nil {
			return fmt.Errorf("%w: %s", ErrPathTraversal, f.Name)
		}

		// Zip bomb defense: a sane entry decompresses at most maxZipRatio×
		// its compressed size; crafted archives hit 1000:1+ and would
		// exhaust disk from a small download.
		if f.CompressedSize64 > 0 && f.UncompressedSize64 > f.CompressedSize64*maxZipRatio {
			return fmt.Errorf("zip entry %s has suspicious compression ratio (%d → %d bytes); possible zip bomb",
				f.Name, f.CompressedSize64, f.UncompressedSize64)
		}

		destPath := filepath.Join(destDir, f.Name)

		// Double-check that destPath is within destDir (extra safety).
		if !strings.HasPrefix(filepath.Clean(destPath), filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("%w: %s", ErrPathTraversal, f.Name)
		}

		// Create parent directories.
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return fmt.Errorf("mkdir for %s: %w", f.Name, err)
		}

		// Open the zip entry for reading.
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("open zip entry %s: %w", f.Name, err)
		}

		// Create the destination file with the entry's permission bits.
		out, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode().Perm())
		if err != nil {
			rc.Close()
			return fmt.Errorf("create %s: %w", f.Name, err)
		}

		_, err = io.Copy(out, rc)
		rcErr := rc.Close()
		outErr := out.Close()
		if err != nil {
			return fmt.Errorf("extract %s: %w", f.Name, err)
		}
		// archive/zip verifies the entry CRC/checksum on Close, so a
		// corrupted entry only surfaces here — treat it as a failure.
		if rcErr != nil {
			return fmt.Errorf("extract %s: %w", f.Name, rcErr)
		}
		if outErr != nil {
			return fmt.Errorf("extract %s: %w", f.Name, outErr)
		}

		if progress != nil {
			progress(Progress{
				FilesExtracted: i + 1,
				TotalFiles:     total,
				CurrentFile:    f.Name,
			})
		}
	}

	return nil
}

// maxZipRatio bounds how much a zip entry may decompress relative to its
// compressed size. Real game assets (already-compressed media, scripts)
// rarely exceed 20:1; 100:1 catches zip bombs while leaving huge headroom.
const maxZipRatio = 100

// maxZipTotalBytes bounds the total uncompressed size of a single archive
// (twice the downloader's 50 GB input cap).
const maxZipTotalBytes int64 = 100 * 1024 * 1024 * 1024

// sanitizeZipPath checks whether a zip entry name is safe to extract.
// It rejects path traversal components — any path segment that is exactly
// ".." — and absolute paths. Names that merely contain ".." as part of a
// longer segment (e.g. "Game v1.0..beta/file.bin") are allowed.
func sanitizeZipPath(name string) error {
	// Reject absolute paths. isWindowsAbsPath is needed because
	// filepath.IsAbs only recognizes Windows drive paths on Windows, but
	// zips are portable and must behave the same on every target.
	if filepath.IsAbs(name) || isWindowsAbsPath(name) {
		return ErrPathTraversal
	}
	// Reject traversal components. Both separators are treated as path
	// separators because zips created on Windows may use backslashes.
	for _, part := range strings.FieldsFunc(name, func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		if part == ".." {
			return ErrPathTraversal
		}
	}
	return nil
}

// isWindowsAbsPath reports whether name looks like a Windows absolute path
// such as `C:\evil.exe` or `C:/evil.exe`.
func isWindowsAbsPath(name string) bool {
	if len(name) < 3 || name[1] != ':' {
		return false
	}
	c := name[0]
	if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
		return false
	}
	return name[2] == '/' || name[2] == '\\'
}
