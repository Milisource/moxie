package extractor

import (
	"archive/tar"
	"bufio"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ulikunitz/xz"
)

// extractTar extracts a (optionally gzip/bzip2/xz-compressed) tar archive
// using only pure-Go readers. typ is one of "tar", "tar.gz", "tar.bz2",
// "tar.xz". Linux builds of F95Zone games commonly ship as .tar.bz2.
//
// Only regular files, directories and symlinks that stay inside destDir
// are materialised; device nodes, FIFOs and hard links are skipped.
func extractTar(ctx context.Context, archivePath, typ, destDir string, progress ProgressFunc) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open tar: %w", err)
	}
	defer f.Close()

	var r io.Reader = bufio.NewReaderSize(f, 1<<20)
	switch typ {
	case "tar":
	case "tar.gz":
		gz, err := gzip.NewReader(r)
		if err != nil {
			return fmt.Errorf("%w: gzip: %v", ErrCorruptArchive, err)
		}
		defer gz.Close()
		r = gz
	case "tar.bz2":
		r = bzip2.NewReader(r)
	case "tar.xz":
		xr, err := xz.NewReader(r)
		if err != nil {
			return fmt.Errorf("%w: xz: %v", ErrCorruptArchive, err)
		}
		r = xr
	default:
		return fmt.Errorf("%w: %s", ErrUnknownFormat, typ)
	}

	cleanDest := filepath.Clean(destDir)
	inside := func(p string) bool {
		return strings.HasPrefix(filepath.Clean(p), cleanDest+string(os.PathSeparator))
	}

	tr := tar.NewReader(r)
	var written int64
	n := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: tar: %v", ErrCorruptArchive, err)
		}
		if err := sanitizeZipPath(hdr.Name); err != nil {
			return fmt.Errorf("%w: %s", ErrPathTraversal, hdr.Name)
		}
		destPath := filepath.Join(destDir, hdr.Name)
		if filepath.Clean(destPath) == cleanDest {
			continue // "./" entry
		}
		if !inside(destPath) {
			return fmt.Errorf("%w: %s", ErrPathTraversal, hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return fmt.Errorf("mkdir %s: %w", hdr.Name, err)
			}
			continue
		case tar.TypeSymlink:
			// Only relative links that resolve inside destDir.
			target := hdr.Linkname
			if filepath.IsAbs(target) || !inside(filepath.Join(filepath.Dir(destPath), target)) {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
				return fmt.Errorf("mkdir for %s: %w", hdr.Name, err)
			}
			_ = os.Remove(destPath)
			if err := os.Symlink(target, destPath); err != nil {
				return fmt.Errorf("symlink %s: %w", hdr.Name, err)
			}
			continue
		case tar.TypeReg, tar.TypeRegA:
		default:
			continue
		}

		written += hdr.Size
		if written > maxZipTotalBytes {
			return fmt.Errorf("tar total size exceeds limit of %d bytes", maxZipTotalBytes)
		}
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return fmt.Errorf("mkdir for %s: %w", hdr.Name, err)
		}
		mode := os.FileMode(hdr.Mode).Perm() | 0600
		out, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
		if err != nil {
			return fmt.Errorf("create %s: %w", hdr.Name, err)
		}
		_, err = io.Copy(out, tr)
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return fmt.Errorf("extract %s: %w", hdr.Name, err)
		}
		n++
		if progress != nil {
			// Tar streams have no index, so the total is unknown (0).
			progress(Progress{FilesExtracted: n, CurrentFile: hdr.Name})
		}
	}
	return nil
}
