// Package extractor extracts game archives (.zip, .7z, .rar, .tar[.gz|.bz2|.xz]) to a
// temporary directory, with support for zip-slip protection, context
// cancellation, progress callbacks, and single-folder unwrapping suitable
// for passing directly to updater.Merge().
package extractor

import (
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ulikunitz/xz"
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
// Returns "zip", "7z", "rar", "tar", "tar.gz", "tar.bz2" or "tar.xz", or
// an error wrapping ErrUnknownFormat that describes what the file looks
// like (an HTML page, an empty file, or the leading bytes).
func DetectArchiveType(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	header := make([]byte, 512)
	n, err := io.ReadFull(f, header)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", fmt.Errorf("read header: %w", err)
	}
	header = header[:n]

	switch {
	case bytes.HasPrefix(header, []byte("PK\x03\x04")):
		return "zip", nil
	case bytes.HasPrefix(header, []byte("7z\xBC\xAF\x27\x1C")):
		return "7z", nil
	case bytes.HasPrefix(header, []byte("Rar!\x1A\x07")):
		return "rar", nil
	case isTarHeader(header):
		return "tar", nil
	case bytes.HasPrefix(header, []byte{0x1f, 0x8b}):
		if compressedTar(path, "tar.gz") {
			return "tar.gz", nil
		}
		return "", fmt.Errorf("%w: gzip file that is not a tar archive: %s", ErrUnknownFormat, path)
	case bytes.HasPrefix(header, []byte("BZh")):
		if compressedTar(path, "tar.bz2") {
			return "tar.bz2", nil
		}
		return "", fmt.Errorf("%w: bzip2 file that is not a tar archive: %s", ErrUnknownFormat, path)
	case bytes.HasPrefix(header, []byte{0xFD, '7', 'z', 'X', 'Z', 0x00}):
		if compressedTar(path, "tar.xz") {
			return "tar.xz", nil
		}
		return "", fmt.Errorf("%w: xz file that is not a tar archive: %s", ErrUnknownFormat, path)
	}
	return "", fmt.Errorf("%w (%s): %s", ErrUnknownFormat, describeHeader(header), path)
}

// isTarHeader reports whether b starts with a POSIX/GNU tar header
// ("ustar" magic at offset 257).
func isTarHeader(b []byte) bool {
	return len(b) >= 262 && string(b[257:262]) == "ustar"
}

// compressedTar decompresses just enough of a gzip/bzip2/xz stream to
// check for a tar header.
func compressedTar(path, typ string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var r io.Reader
	switch typ {
	case "tar.gz":
		gz, err := gzip.NewReader(f)
		if err != nil {
			return false
		}
		defer gz.Close()
		r = gz
	case "tar.bz2":
		r = bzip2.NewReader(f)
	case "tar.xz":
		xr, err := xz.NewReader(f)
		if err != nil {
			return false
		}
		r = xr
	}
	buf := make([]byte, 512)
	n, _ := io.ReadFull(r, buf)
	return isTarHeader(buf[:n])
}

// describeHeader names what an unrecognised file looks like, so a failed
// update says "HTML page" instead of just "unknown format".
func describeHeader(b []byte) string {
	if len(b) == 0 {
		return "empty file"
	}
	trimmed := bytes.TrimLeft(b, " \t\r\n\xef\xbb\xbf")
	if len(trimmed) > 0 && trimmed[0] == '<' {
		return "looks like an HTML page, not an archive"
	}
	if bytes.HasPrefix(b, []byte("MZ")) {
		return "Windows executable or self-extracting installer"
	}
	if bytes.HasPrefix(b, []byte("\x7fELF")) {
		return "Linux executable"
	}
	head := b
	if len(head) > 8 {
		head = head[:8]
	}
	return fmt.Sprintf("leading bytes % x", head)
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
	case "tar", "tar.gz", "tar.bz2", "tar.xz":
		err = extractTar(ctx, archivePath, typ, tmpDir, progress)
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
//
// It never unwraps a directory that is itself a game root: some releases put
// the launcher at the top level beside a single asset folder (RPGM MV's www/,
// an HTML game's img/, a Ren'Py build's renpy/). Unwrapping there discards the
// launcher and leaves only the assets, so the installed game has no executable.
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
	if len(subdirs) == 1 && !looksLikeGameRoot(entries) {
		return filepath.Join(dir, subdirs[0])
	}
	return dir
}

// looksLikeGameRoot reports whether entries describe a game root — a directory
// holding a launcher, HTML entry page, or engine marker file — rather than a
// wrapper folder that only contains the real game one level down. It is the
// guard that stops findGameRoot from collapsing a game root into its single
// asset subdirectory and dropping the launcher.
func looksLikeGameRoot(entries []os.DirEntry) bool {
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		switch name {
		case "data.win", "package.json", "unityplayer.dll", "nw.dll",
			"game.ini", "nscript.dat", "game.rgssad", "game.rgss3a",
			"index.html":
			return true
		}
		switch strings.ToLower(filepath.Ext(name)) {
		case ".exe", ".sh", ".x86_64", ".x86", ".appimage",
			".html", ".htm", ".pck", ".rpyc", ".rpa", ".jar", ".love":
			return true
		}
	}
	return false
}
