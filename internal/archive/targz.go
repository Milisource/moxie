package archive

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func extractTarGz(archivePath, destDir string, opts Options) (*Result, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	// Get file size for progress
	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat archive: %w", err)
	}
	totalBytes := fi.Size()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("create gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	result := &Result{TotalBytes: totalBytes}
	extractedFiles := 0
	var bytesProcessed int64

	// First pass: count total files
	totalFiles := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar header: %w", err)
		}
		if !h.FileInfo().IsDir() {
			totalFiles++
		}
	}

	// Reset to beginning
	f.Seek(0, 0)
	gzr, _ = gzip.NewReader(f)
	tr = tar.NewReader(gzr)

	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar header: %w", err)
		}

		// Clean the file path
		path, err := cleanExtractPath(h.Name, destDir)
		if err != nil {
			return nil, err
		}

		if h.FileInfo().IsDir() {
			if err := os.MkdirAll(path, h.FileInfo().Mode()); err != nil {
				return nil, fmt.Errorf("create directory: %w", err)
			}
			continue
		}

		// Create parent directories
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return nil, fmt.Errorf("create parent directory: %w", err)
		}

		// Extract file
		outFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, h.FileInfo().Mode())
		if err != nil {
			return nil, fmt.Errorf("create output file: %w", err)
		}

		written, err := io.Copy(outFile, tr)
		outFile.Close()

		if err != nil {
			return nil, fmt.Errorf("extract file: %w", err)
		}

		bytesProcessed += written
		extractedFiles++

		if opts.OnProgress != nil {
			opts.OnProgress(totalFiles, extractedFiles, h.Name, bytesProcessed, totalBytes)
		}
	}

	result.FilesExtracted = extractedFiles
	return result, nil
}
