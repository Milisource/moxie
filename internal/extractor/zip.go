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
	for _, f := range r.File {
		if !f.FileInfo().IsDir() {
			files = append(files, f)
		}
	}
	total := len(files)

	for i, f := range files {
		if err := ctx.Err(); err != nil {
			return err
		}

		if err := sanitizeZipPath(f.Name); err != nil {
			return fmt.Errorf("%w: %s", ErrPathTraversal, f.Name)
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
		rc.Close()
		out.Close()
		if err != nil {
			return fmt.Errorf("extract %s: %w", f.Name, err)
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

// sanitizeZipPath checks whether a zip entry name is safe to extract.
// It rejects paths containing ".." (zip-slip) and absolute paths.
func sanitizeZipPath(name string) error {
	// Reject path traversal components.
	if strings.Contains(name, "..") {
		return ErrPathTraversal
	}
	// Reject absolute paths.
	if filepath.IsAbs(name) {
		return ErrPathTraversal
	}
	return nil
}
