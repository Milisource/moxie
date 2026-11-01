// Package fsutil provides filesystem safety helpers shared by the archive
// and extractor packages.
package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LinkPolicy controls how symlinks are treated during tree verification.
type LinkPolicy int

const (
	// LinksForbidden rejects every symlink (used for save archives).
	LinksForbidden LinkPolicy = iota
	// LinksWithinRoot allows symlinks whose target stays inside root
	// (used for game installs, which may legitimately contain .so links).
	LinksWithinRoot
)

// ErrUnsafeArchive is returned when an archive contains an unsafe entry.
type ErrUnsafeArchive struct {
	Entry  string
	Reason string
}

func (e *ErrUnsafeArchive) Error() string {
	return fmt.Sprintf("unsafe archive entry %q: %s", e.Entry, e.Reason)
}

// VerifyTree walks root (without following links) and verifies it is safe.
func VerifyTree(root string, policy LinkPolicy) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	return filepath.WalkDir(rootAbs, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == rootAbs {
			return nil
		}
		rel, _ := filepath.Rel(rootAbs, path)
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode()
		// Reject special files.
		if !mode.IsRegular() && !mode.IsDir() && mode&os.ModeSymlink == 0 {
			return &ErrUnsafeArchive{Entry: rel, Reason: "non-regular special file"}
		}
		if mode&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			// Reject absolute targets.
			if filepath.IsAbs(target) {
				if policy == LinksForbidden {
					return &ErrUnsafeArchive{Entry: rel, Reason: "symlink (forbidden)"}
				}
				return &ErrUnsafeArchive{Entry: rel, Reason: "absolute symlink target"}
			}
			if policy == LinksForbidden {
				return &ErrUnsafeArchive{Entry: rel, Reason: "symlink (forbidden)"}
			}
			// LinksWithinRoot: resolve relative to link's dir and require inside root.
			linkDir := filepath.Dir(path)
			resolved := filepath.Clean(filepath.Join(linkDir, target))
			relToRoot, err := filepath.Rel(rootAbs, resolved)
			if err != nil || strings.HasPrefix(relToRoot, "..") || relToRoot == ".." {
				return &ErrUnsafeArchive{Entry: rel, Reason: "symlink resolves outside root"}
			}
			// Also check evaluated symlinks for chains.
			if ev, err := filepath.EvalSymlinks(path); err == nil {
				evRel, rerr := filepath.Rel(rootAbs, ev)
				if rerr != nil || strings.HasPrefix(evRel, "..") {
					return &ErrUnsafeArchive{Entry: rel, Reason: "symlink resolves outside root"}
				}
			}
		}
		// case-insensitive duplicate check optional; skip.
		return nil
	})
}