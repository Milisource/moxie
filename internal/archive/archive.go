// Package archive provides extraction of common game archive formats.
// This is a high-level wrapper around the extractor package, providing the
// Options/Result API used by the CLI commands and TUI.
package archive

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mili/moxie/internal/extractor"
)

// Format represents the type of archive file.
type Format int

const (
	FormatUnknown Format = iota
	FormatZip
	FormatTarGz
	FormatSevenZip
	FormatRar
)

func (f Format) String() string {
	switch f {
	case FormatZip:
		return "zip"
	case FormatTarGz:
		return "tar.gz"
	case FormatSevenZip:
		return "7z"
	case FormatRar:
		return "rar"
	default:
		return "unknown"
	}
}

// DetectFormat determines the archive format from file magic bytes.
func DetectFormat(path string) Format {
	typ, err := extractor.DetectArchiveType(path)
	if err != nil {
		return FormatUnknown
	}
	switch typ {
	case "zip":
		return FormatZip
	case "7z":
		return FormatSevenZip
	case "rar":
		return FormatRar
	}

	// Check for tar.gz via extension (not detected by magic bytes alone)
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		return FormatTarGz
	}

	return FormatUnknown
}

// IsArchiveFile returns true if the file appears to be a known archive format.
func IsArchiveFile(path string) bool {
	return DetectFormat(path) != FormatUnknown
}

// ProgressFunc is called during extraction to report progress.
type ProgressFunc func(totalFiles, extractedFiles int, currentFile string, bytesProcessed, bytesTotal int64)

// Options controls extraction behavior.
type Options struct {
	// Password for encrypted archives (optional)
	Password string
	// Progress callback (optional)
	OnProgress ProgressFunc
}

// Result contains information about the extraction.
type Result struct {
	FilesExtracted int
	TotalBytes     int64
	Destination    string
}

// Extract extracts an archive file to the specified destination directory.
// The destination directory will be created if it doesn't exist.
// Returns a Result with the extracted root path (handles single-folder wrapping).
func Extract(archivePath, destDir string, opts Options) (*Result, error) {
	// Ensure destination exists
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("create destination directory: %w", err)
	}

	// Map the archive-style progress callback to the extractor's callback
	extractProgress := func(p extractor.Progress) {
		if opts.OnProgress != nil {
			opts.OnProgress(p.TotalFiles, p.FilesExtracted, p.CurrentFile, 0, 0)
		}
	}

	extractedRoot, err := extractor.Extract(context.Background(), archivePath, destDir, extractProgress)
	if err != nil {
		return nil, err
	}

	// Count extracted files and total bytes
	result := &Result{Destination: extractedRoot}
	filepath.Walk(extractedRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible
		}
		if !info.IsDir() {
			result.FilesExtracted++
			result.TotalBytes += info.Size()
		}
		return nil
	})

	return result, nil
}
