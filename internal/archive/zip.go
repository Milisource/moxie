package archive

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func extractZip(archivePath, destDir string, opts Options) (*Result, error) {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	result := &Result{}
	totalFiles := len(r.File)

	// Calculate total size
	var totalBytes int64
	for _, f := range r.File {
		if !f.FileInfo().IsDir() {
			totalBytes += int64(f.UncompressedSize64)
		}
	}
	result.TotalBytes = totalBytes

	extractedFiles := 0
	var bytesProcessed int64

	for _, f := range r.File {
		// Clean the file path to prevent zip slip
		path, err := cleanExtractPath(f.Name, destDir)
		if err != nil {
			return nil, err
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(path, f.Mode()); err != nil {
				return nil, fmt.Errorf("create directory: %w", err)
			}
			continue
		}

		// Create parent directories
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return nil, fmt.Errorf("create parent directory: %w", err)
		}

		// Extract file
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("open file in archive: %w", err)
		}

		outFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return nil, fmt.Errorf("create output file: %w", err)
		}

		written, err := io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()

		if err != nil {
			return nil, fmt.Errorf("extract file: %w", err)
		}

		bytesProcessed += written
		extractedFiles++

		if opts.OnProgress != nil {
			opts.OnProgress(totalFiles, extractedFiles, f.Name, bytesProcessed, totalBytes)
		}
	}

	result.FilesExtracted = extractedFiles
	return result, nil
}
