// Package archive provides extraction of common game archive formats.
// Supports .zip, .tar.gz, .7z, and .rar files with progress reporting.
package archive

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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

// DetectFormat determines the archive format from filename and file magic bytes.
func DetectFormat(path string) Format {
	// Check extension first
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".zip":
		return FormatZip
	case ".gz", ".tgz":
		if strings.HasSuffix(strings.ToLower(path), ".tar.gz") ||
			strings.HasSuffix(strings.ToLower(path), ".tgz") {
			return FormatTarGz
		}
		return FormatUnknown
	case ".7z":
		return FormatSevenZip
	case ".rar":
		return FormatRar
	}

	// Try to detect from magic bytes
	f, err := os.Open(path)
	if err != nil {
		return FormatUnknown
	}
	defer f.Close()

	magic := make([]byte, 4)
	if _, err := f.Read(magic); err != nil {
		return FormatUnknown
	}

	// ZIP: 50 4B 03 04
	if magic[0] == 0x50 && magic[1] == 0x4B && magic[2] == 0x03 && magic[3] == 0x04 {
		return FormatZip
	}
	// 7z: 37 7A BC AF 27 1C
	// Check first byte
	if magic[0] == 0x37 {
		magic2 := make([]byte, 2)
		if _, err := f.Read(magic2); err == nil {
			if magic2[0] == 0x7A && magic2[1] == 0xBC {
				return FormatSevenZip
			}
		}
	}
	// RAR: 52 61 72 21
	if magic[0] == 0x52 && magic[1] == 0x61 && magic[2] == 0x72 && magic[3] == 0x21 {
		return FormatRar
	}
	// GZIP: 1F 8B
	if magic[0] == 0x1F && magic[1] == 0x8B {
		return FormatTarGz
	}

	return FormatUnknown
}

// ProgressFunc is called during extraction to report progress.
// totalFiles: total number of files in archive
// extractedFiles: number of files extracted so far
// currentFile: name of file currently being extracted
// bytesProcessed: total bytes processed
// bytesTotal: total bytes in archive (may be 0 if unknown)
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

// IsArchiveFile returns true if the file appears to be a known archive format.
func IsArchiveFile(path string) bool {
	return DetectFormat(path) != FormatUnknown
}

// Extract extracts an archive file to the specified destination directory.
// The destination directory will be created if it doesn't exist.
func Extract(archivePath, destDir string, opts Options) (*Result, error) {
	format := DetectFormat(archivePath)
	if format == FormatUnknown {
		return nil, fmt.Errorf("unsupported or unknown archive format: %s", archivePath)
	}

	// Ensure destination exists
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("create destination directory: %w", err)
	}

	// Create extraction subdirectory named after the archive (without extension)
	base := filepath.Base(archivePath)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if strings.HasSuffix(strings.ToLower(base), ".tar") {
		base = strings.TrimSuffix(base, ".tar")
	}
	extractDir := filepath.Join(destDir, base)
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return nil, fmt.Errorf("create extraction directory: %w", err)
	}

	var result *Result
	var err error

	switch format {
	case FormatZip:
		result, err = extractZip(archivePath, extractDir, opts)
	case FormatTarGz:
		result, err = extractTarGz(archivePath, extractDir, opts)
	case FormatSevenZip:
		result, err = extract7z(archivePath, extractDir, opts)
	case FormatRar:
		result, err = extractRar(archivePath, extractDir, opts)
	default:
		return nil, fmt.Errorf("unsupported archive format")
	}

	if err != nil {
		return nil, err
	}

	result.Destination = extractDir
	return result, nil
}

// copyFileWithProgress copies src to dst while tracking bytes processed.
func copyFileWithProgress(dst, src string, onProgress func(int64)) error {
	sf, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sf.Close()

	info, err := sf.Stat()
	if err != nil {
		return err
	}

	df, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer df.Close()

	buf := make([]byte, 32768)
	var written int64
	for {
		n, err := sf.Read(buf)
		if n > 0 {
			wn, werr := df.Write(buf[:n])
			if werr != nil {
				return werr
			}
			written += int64(wn)
			if onProgress != nil {
				onProgress(written)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return df.Sync()
}

// cleanExtractPath ensures the extracted file path stays within the destination directory.
// Prevents zip slip attacks.
func cleanExtractPath(filePath, destDir string) (string, error) {
	// Join and clean the path
	path := filepath.Join(destDir, filePath)
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	absDest, err := filepath.Abs(destDir)
	if err != nil {
		return "", err
	}

	// Ensure the path is within the destination
	if !strings.HasPrefix(absPath, absDest+string(filepath.Separator)) && absPath != absDest {
		return "", fmt.Errorf("invalid file path: %s", filePath)
	}

	return absPath, nil
}
