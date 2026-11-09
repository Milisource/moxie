// Package archive provides extraction of common game archive formats.
// This is a high-level wrapper around the extractor package, providing the
// Options/Result API used by the CLI commands and TUI.
package archive

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
		return "tar"
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
	case "tar", "tar.gz", "tar.bz2", "tar.xz":
		// All tar variants share one Format value; the extractor picks
		// the decompressor from the magic bytes.
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
	// Progress callback (optional)
	OnProgress ProgressFunc
	// MaxTotalBytes caps the total uncompressed size an archive may produce
	// (0 = unlimited). Extraction aborts with an error when the archive's
	// declared total exceeds it, so a high-ratio "decompression bomb" cannot
	// fill the disk. Game installs legitimately unpack to many gigabytes:
	// callers that do not pass a budget are unaffected.
	MaxTotalBytes int64
	// MaxEntries caps the number of non-directory entries an archive may
	// contain (0 = unlimited). Like MaxTotalBytes this is opt-in: zero keeps
	// the previous unlimited behavior.
	MaxEntries int
}

// errExtractBudget is the sentinel wrapped by every budget-exceeded error, so
// callers can detect one with errors.Is.
var errExtractBudget = errors.New("extraction budget exceeded")

// checkListingBudget returns a budget error when a known archive total or entry
// count exceeds the configured cap. A zero cap means unlimited.
//
// This is the whole of the size enforcement in this package: the natives in
// internal/extractor own the write loop, and the sizes an archive declares are
// authoritative for both of them — archive/zip's reader rejects an entry that
// outruns its declared UncompressedSize64 (archive/zip: checksumReader.Read
// errors with ErrFormat) and a tar entry body is exactly its header size — so a
// pre-write check against the declared total bounds the write the same way a
// bounded copy inside the loop would. ExtractWithContext checks it again after
// extraction to catch an under-reported total.
func checkListingBudget(totalBytes int64, entries int, opts Options) error {
	if opts.MaxEntries > 0 && entries > opts.MaxEntries {
		return fmt.Errorf("%w: archive has %d entries, limit is %d", errExtractBudget, entries, opts.MaxEntries)
	}
	if opts.MaxTotalBytes > 0 && totalBytes > opts.MaxTotalBytes {
		return fmt.Errorf("%w: archive expands to %d bytes, limit is %d", errExtractBudget, totalBytes, opts.MaxTotalBytes)
	}
	return nil
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
	return ExtractWithContext(context.Background(), archivePath, destDir, opts)
}

// ExtractWithContext is Extract with a cancellable context. If ctx is
// cancelled during extraction, the operation stops between entries and
// only the extractor's temporary subdirectory is removed — destDir and
// its pre-existing contents are never deleted.
func ExtractWithContext(ctx context.Context, archivePath, destDir string, opts Options) (*Result, error) {
	// Reject an over-budget archive before anything is written. This is
	// best effort for the tool-listed formats (7z, rar); the extraction is
	// verified afterwards so a hostile archive that slipped past this check
	// still cannot end up over budget unnoticed.
	if err := precheckArchiveBudget(archivePath, opts); err != nil {
		return nil, err
	}

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

	extractedRoot, err := extractor.Extract(ctx, archivePath, destDir, extractProgress)
	if err != nil {
		return nil, err
	}

	// The shell-out extractors (7z, unrar) copy archive link entries
	// verbatim and cannot be bounded mid-write, so the extracted tree is
	// verified afterwards: a symlink entry that leaves destDir is rejected.
	if format := DetectFormat(archivePath); format == FormatSevenZip || format == FormatRar {
		if err := verifyExtractedTree(extractedRoot); err != nil {
			return nil, err
		}
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

	// Catch an archive whose headers under-reported its size: the pre-check
	// above only knows what the archive (or its listing) declared.
	if err := checkListingBudget(result.TotalBytes, result.FilesExtracted, opts); err != nil {
		return nil, err
	}

	return result, nil
}
