package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ulikunitz/xz"

	"github.com/mili/moxie/internal/extractor"
)

// precheckArchiveBudget rejects an archive that is already over opts' budget
// before any of it is written to disk, so a decompression bomb consumes no
// disk at all. A zero budget means unlimited and skips the check entirely.
//
// The zip and tar formats are measured natively (their headers declare the
// sizes up front), which is exact and needs no external tool. 7z and rar have
// no pure-Go reader here, so they fall back to the listing pre-check of the
// tool that will extract them (best effort — see precheckExtractBudget).
func precheckArchiveBudget(archivePath string, opts Options) error {
	if opts.MaxTotalBytes <= 0 && opts.MaxEntries <= 0 {
		return nil
	}

	switch DetectFormat(archivePath) {
	case FormatZip:
		return scanZipBudget(archivePath, opts)
	case FormatTarGz:
		return scanTarBudget(archivePath, opts)
	case FormatSevenZip, FormatRar:
		return precheckToolBudget(archivePath, opts)
	}
	return nil
}

// scanZipBudget sums the zip central directory's declared uncompressed sizes
// and counts its non-directory entries, aborting as soon as either budget is
// exceeded. A zip whose directory lies about its sizes is caught after
// extraction by ExtractWithContext's verification walk.
func scanZipBudget(archivePath string, opts Options) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		// Not a readable zip: let extraction report the real error.
		return nil
	}
	defer r.Close()

	var totalBytes int64
	entries := 0
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		totalBytes += int64(f.UncompressedSize64)
		entries++
		if err := checkListingBudget(totalBytes, entries, opts); err != nil {
			return err
		}
	}
	return nil
}

// scanTarBudget sums a (optionally compressed) tar's declared entry sizes and
// counts its entries without writing anything. Tar headers are authoritative
// for the entry size, so no per-entry bounding is needed — the running total
// is checked against the budget as the headers are read, which stops a bomb
// after the first oversized header instead of decompressing the whole stream.
func scanTarBudget(archivePath string, opts Options) error {
	typ, err := extractor.DetectArchiveType(archivePath)
	if err != nil {
		return nil // let extraction report the format error
	}

	f, err := os.Open(archivePath)
	if err != nil {
		return nil // let extraction report the open error
	}
	defer f.Close()

	var r io.Reader = f
	switch typ {
	case "tar":
	case "tar.gz":
		gz, err := gzip.NewReader(r)
		if err != nil {
			return nil // let extraction report the decompression error
		}
		defer gz.Close()
		r = gz
	case "tar.bz2":
		r = bzip2.NewReader(r)
	case "tar.xz":
		xr, err := xz.NewReader(r)
		if err != nil {
			return nil // let extraction report the decompression error
		}
		r = xr
	default:
		return nil
	}

	tr := tar.NewReader(r)
	var totalBytes int64
	entries := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Truncated or corrupt: let extraction report the real error.
			return nil
		}
		if hdr.Typeflag == tar.TypeDir ||
			hdr.Typeflag == tar.TypeXGlobalHeader || hdr.Typeflag == tar.TypeXHeader {
			continue
		}
		entries++
		if hdr.Typeflag == tar.TypeReg || hdr.Typeflag == tar.TypeRegA {
			totalBytes += hdr.Size
		}
		if err := checkListingBudget(totalBytes, entries, opts); err != nil {
			return err
		}
	}
	return nil
}

// precheckToolBudget runs the listing pre-check with the tool that
// extractor.Extract will shell out to for this archive, so the sizes it sees
// are the ones the extractor would write. The tool choice mirrors the
// extractor: 7z for .7z, unrar (falling back to 7z) for .rar. When none of
// them is installed the extraction itself reports the missing tool.
func precheckToolBudget(archivePath string, opts Options) error {
	tool := ""
	if DetectFormat(archivePath) == FormatSevenZip {
		if _, err := exec.LookPath("7z"); err == nil {
			tool = "7z"
		}
	} else if _, err := exec.LookPath("unrar"); err == nil {
		tool = "unrar"
	} else if _, err := exec.LookPath("7z"); err == nil {
		tool = "7z"
	}
	if tool == "" {
		return nil
	}
	return precheckExtractBudget(archivePath, tool, opts)
}

// verifyExtractedTree rejects an extracted tree that contains a symlink entry
// or an entry whose path escapes root. The shell-out extractors (7z, unrar)
// preserve archive link entries verbatim and are not covered by the native
// extractors' zip-slip guard, so the destination is verified after
// extraction. filepath.Walk does not follow links, so a symlink is reported as
// an entry rather than traversed.
func verifyExtractedTree(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("archive entry %q is a symlink (symlinks are not allowed in save archives)", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("archive entry %q escapes destination %q", path, root)
		}
		return nil
	})
}
