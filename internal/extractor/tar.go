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

	cleanDest, err := filepath.Abs(destDir)
	if err != nil {
		return err
	}
	inside := func(p string) bool {
		rel, err := filepath.Rel(cleanDest, p)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
	}
	// Deferred links must also be treated as symlinks during the write pass.
	links := make(map[string]string)
	var linkOrder []string
	checkPath := func(p string) error {
		rel, err := filepath.Rel(cleanDest, p)
		if err != nil || !inside(p) {
			return fmt.Errorf("%w: %s", ErrPathTraversal, p)
		}
		current := cleanDest
		for _, component := range strings.Split(rel, string(os.PathSeparator)) {
			current = filepath.Join(current, component)
			if _, pending := links[current]; pending {
				return fmt.Errorf("%w: %s", ErrPathTraversal, current)
			}
			info, err := os.Lstat(current)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("%w: %s", ErrPathTraversal, current)
			}
		}
		return nil
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
		destPath := filepath.Join(cleanDest, hdr.Name)
		if filepath.Clean(destPath) == cleanDest {
			continue // "./" entry
		}
		if !inside(destPath) {
			return fmt.Errorf("%w: %s", ErrPathTraversal, hdr.Name)
		}

		if err := checkPath(destPath); err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return fmt.Errorf("mkdir %s: %w", hdr.Name, err)
			}
			continue
		case tar.TypeSymlink:
			target := hdr.Linkname
			if filepath.IsAbs(target) || !inside(filepath.Join(filepath.Dir(destPath), target)) {
				return fmt.Errorf("%w: %s -> %s", ErrPathTraversal, hdr.Name, target)
			}
			links[destPath] = target
			linkOrder = append(linkOrder, destPath)
			continue
		case tar.TypeLink:
			// Hardlinks are still skipped, but their archive-root-relative targets
			// must pass the same containment and symlink checks as written entries.
			target := filepath.Join(cleanDest, hdr.Linkname)
			if filepath.IsAbs(hdr.Linkname) || !inside(target) {
				return fmt.Errorf("%w: %s -> %s", ErrPathTraversal, hdr.Name, hdr.Linkname)
			}
			if err := checkPath(target); err != nil {
				return err
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
	// Resolve all deferred targets before creating any links, including chains
	// through other archive links or pre-existing symlinks.
	var resolveTarget func(string, int) error
	resolveTarget = func(p string, depth int) error {
		if depth > 255 || !inside(p) {
			return fmt.Errorf("%w: %s", ErrPathTraversal, p)
		}
		prefix := cleanDest + string(os.PathSeparator)
		if p != cleanDest && !strings.HasPrefix(p, prefix) {
			return fmt.Errorf("%w: %s", ErrPathTraversal, p)
		}
		components := strings.Split(strings.TrimPrefix(p, prefix), string(os.PathSeparator))
		if p == cleanDest {
			return nil
		}
		current := cleanDest
		for i, component := range components {
			current = filepath.Join(current, component)
			if !inside(current) {
				return fmt.Errorf("%w: %s", ErrPathTraversal, current)
			}
			target, pending := links[current]
			if !pending {
				info, err := os.Lstat(current)
				if os.IsNotExist(err) {
					continue
				}
				if err != nil {
					return err
				}
				if info.Mode()&os.ModeSymlink == 0 {
					continue
				}
				target, err = os.Readlink(current)
				if err != nil {
					return err
				}
			}
			if !filepath.IsAbs(target) {
				target = filepath.Dir(current) + string(os.PathSeparator) + target
			}
			return resolveTarget(target+string(os.PathSeparator)+strings.Join(components[i+1:], string(os.PathSeparator)), depth+1)
		}
		return nil
	}
	for _, path := range linkOrder {
		if err := resolveTarget(filepath.Dir(path)+string(os.PathSeparator)+links[path], 0); err != nil {
			return err
		}
	}
	for _, path := range linkOrder {
		if err := ctx.Err(); err != nil {
			return err
		}
		// The final component is this deferred link; only check its parents.
		if err := checkPath(filepath.Dir(path)); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return fmt.Errorf("mkdir for %s: %w", path, err)
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.Symlink(links[path], path); err != nil {
			return fmt.Errorf("symlink %s: %w", path, err)
		}
	}
	return nil
}
