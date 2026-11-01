// Package updater merges downloaded game updates into existing game directories,
// preserving user saves, mods, and configuration files based on engine detection.
package updater

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mili/moxie/internal/log"
)

// MergeResult summarizes what a merge operation did.
type MergeResult struct {
	FilesCopied    int
	FilesPreserved int
	BackupPath     string // empty if backup was not requested or failed
}

// Merge copies new game files from extractedDir into gameDir, preserving
// user data (saves, mods, configs) based on the detected game engine.
// If backup is true, the existing gameDir is renamed to gameDir.old first
// and preserved files are restored from the backup after copying new files.
// A failed merge is rolled back from the backup, so the live game dir is
// never left half-updated and a previous backup is never destroyed before
// the new merge has completed. The context can abort the copy/restore
// walks mid-merge; a cancelled merge is rolled back like any other failure.
func Merge(ctx context.Context, gameDir, engine, extractedDir string, backup bool) (*MergeResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := os.Stat(gameDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("game directory does not exist: %s", gameDir)
	}

	srcDir := findGameRoot(extractedDir)
	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("extracted directory does not exist: %s", srcDir)
	}

	userData := patterns(engine)
	keepIfAbsent := keepIfAbsentPatterns(engine)
	result := &MergeResult{}
	committed := false

	// Optional backup. The backup is the rollback safety net — if it
	// cannot be created, proceeding would overwrite the live game dir
	// with nothing to restore.
	if backup {
		backupPath := gameDir + ".old"

		// A previous merge left a backup behind. Set it aside instead of
		// deleting it: until this merge completes it is the only intact
		// pre-merge copy on disk, and it is discarded only after the new
		// backup has been created successfully (see the deferred commit).
		// The staging name keeps the ".old" suffix so the scanner skips
		// it if a scan happens to run mid-merge.
		stalePath := ""
		if _, err := os.Stat(backupPath); err == nil {
			stalePath = backupPath + ".old"
			if err := os.Rename(backupPath, stalePath); err != nil {
				return nil, fmt.Errorf("merge backup failed (game dir not modified): %w", err)
			}
		}

		if err := os.Rename(gameDir, backupPath); err != nil {
			// Restore the set-aside backup so a failed rename never
			// destroys the previous merge's snapshot.
			if stalePath != "" {
				if rerr := os.Rename(stalePath, backupPath); rerr != nil {
					log.Warn("merge backup failed: restoring previous backup", "dir", backupPath, "error", rerr)
				}
			}
			return nil, fmt.Errorf("merge backup failed (game dir not modified): %w", err)
		}
		result.BackupPath = backupPath

		if err := os.MkdirAll(gameDir, 0755); err != nil {
			// gameDir may be partially created; remove it and restore the
			// live dir plus any stale backup before returning.
			os.RemoveAll(gameDir)
			os.Rename(backupPath, gameDir)
			if stalePath != "" {
				os.Rename(stalePath, backupPath)
			}
			return nil, fmt.Errorf("recreate game dir: %w", err)
		}

		// From here on the live game dir is a fresh, half-populated copy.
		// If the copy below fails (or panics), roll the live dir back from
		// the backup and put the stale backup back in place.
		defer func() {
			if committed {
				// Success: backupPath holds the pre-merge game version;
				// the stale backup is now obsolete.
				if stalePath != "" {
					os.RemoveAll(stalePath)
				}
				return
			}
			// Failure: undo the rename dance.
			if err := os.RemoveAll(gameDir); err != nil {
				log.Warn("merge rollback: remove partial game dir", "dir", gameDir, "error", err)
			}
			if err := os.Rename(backupPath, gameDir); err != nil {
				log.Warn("merge rollback: restore game dir from backup", "dir", gameDir, "error", err)
			}
			if stalePath != "" {
				if err := os.Rename(stalePath, backupPath); err != nil {
					log.Warn("merge rollback: restore previous backup", "dir", backupPath, "error", err)
				}
			}
			result.BackupPath = ""
		}()
	}

	// Copy new files
	if err := copyNew(ctx, srcDir, gameDir, userData, result); err != nil {
		return result, err
	}

	// If we have a backup, restore preserved files from it. A failure here
	// (including ctx cancellation) must not commit: the live dir is only
	// partially restored, so returning with committed=false triggers the
	// deferred rollback rather than recording a half-restored merge as success.
	if result.BackupPath != "" {
		if err := restorePreserved(ctx, result.BackupPath, gameDir, userData); err != nil {
			return result, fmt.Errorf("restore preserved files: %w", err)
		}
		// keepIfAbsent: copy from backup only when new version didn't ship it.
		if err := restoreKeepIfAbsent(ctx, result.BackupPath, gameDir, keepIfAbsent); err != nil {
			return result, fmt.Errorf("restore preserved files: %w", err)
		}
	}

	committed = true

	log.Info("merge complete", "game_dir", gameDir, "engine", engine,
		"copied", result.FilesCopied, "preserved", result.FilesPreserved)
	return result, nil
}

// copyNew walks srcDir and copies files to destDir, skipping any that match
// preserve patterns AND already exist in the destination. A non-nil ctx.Err()
// aborts the walk early.
func copyNew(ctx context.Context, srcDir, destDir string, preserve []string, result *MergeResult) error {
	return filepath.Walk(srcDir, func(srcPath string, info os.FileInfo, err error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err != nil {
			return err
		}
		relPath, _ := filepath.Rel(srcDir, srcPath)
		if relPath == "." {
			return nil
		}
		destPath := filepath.Join(destDir, relPath)

		if info.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}

		// Dest-side check: refuse to traverse a parent symlink that leaves
		// destDir.
		if !destPathSafe(destPath, destDir) {
			log.Warn("skipping entry: destination path traverses an escaping symlink", "path", relPath)
			return nil
		}

		// Handle symlinks explicitly: never copy a target's contents.
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(srcPath)
			if err != nil {
				return err
			}
			if !linkWithinRoot(target, filepath.Dir(destPath), destDir) {
				log.Warn("skipping symlink that escapes destination", "path", relPath, "target", target)
				return nil
			}
			os.Remove(destPath)
			if err := os.Symlink(target, destPath); err != nil {
				return fmt.Errorf("symlink %s: %w", relPath, err)
			}
			result.FilesCopied++
			return nil
		}

		// Skip non-regular files.
		if !info.Mode().IsRegular() {
			return nil
		}

		// Preserve existing user files that match engine-specific patterns
		if shouldPreserve(relPath, destPath, preserve) {
			result.FilesPreserved++
			log.Debug("preserved user file", "path", relPath)
			return nil
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return err
		}
		if err := copyFile(srcPath, destPath); err != nil {
			return fmt.Errorf("copy %s: %w", relPath, err)
		}
		result.FilesCopied++
		return nil
	})
}

// restorePreserved copies files from backupDir to gameDir that match preserve
// patterns, overwriting any defaults from the new version with the user's files.
// A non-nil ctx.Err() stops the walk early and is returned so the caller can
// roll the merge back instead of committing a partially-restored game dir.
func restorePreserved(ctx context.Context, backupDir, gameDir string, preserve []string) error {
	return filepath.Walk(backupDir, func(srcPath string, info os.FileInfo, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil || info.IsDir() {
			return nil
		}
		relPath, _ := filepath.Rel(backupDir, srcPath)
		if !shouldPreserve(relPath, "", preserve) {
			return nil
		}
		destPath := filepath.Join(gameDir, relPath)
		if !destPathSafe(destPath, gameDir) {
			log.Warn("skipping restore: destination path traverses an escaping symlink", "path", relPath)
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(srcPath)
			if err != nil {
				return err
			}
			if !linkWithinRoot(target, filepath.Dir(destPath), gameDir) {
				log.Warn("skipping symlink that escapes destination", "path", relPath, "target", target)
				return nil
			}
			os.Remove(destPath)
			if err := os.Symlink(target, destPath); err != nil {
				return err
			}
			log.Debug("restored user symlink from backup", "path", relPath)
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return err
		}
		if err := copyFile(srcPath, destPath); err != nil {
			return err
		}
		log.Debug("restored user file from backup", "path", relPath)
		return nil
	})
}

// restoreKeepIfAbsent copies files from backupDir to gameDir only when the
// destination does not already exist (i.e. the new version did not ship
// that file). Used for user-created config.
func restoreKeepIfAbsent(ctx context.Context, backupDir, gameDir string, patterns []string) error {
	return filepath.Walk(backupDir, func(srcPath string, info os.FileInfo, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil || info.IsDir() {
			return nil
		}
		relPath, _ := filepath.Rel(backupDir, srcPath)
		for _, pat := range patterns {
			if !matchPath(pat, relPath) {
				continue
			}
			destPath := filepath.Join(gameDir, relPath)
			if _, err := os.Stat(destPath); err == nil {
				return nil // new version shipped it; keep new
			}
			if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
				return err
			}
			if err := copyFile(srcPath, destPath); err != nil {
				return err
			}
			log.Debug("restored keep-if-absent file from backup", "path", relPath)
			return nil
		}
		return nil
	})
}

// shouldPreserve returns true if the file at relPath should NOT be overwritten
// because it already exists and matches a preserve pattern.
func shouldPreserve(relPath, destPath string, patterns []string) bool {
	if destPath != "" {
		if _, err := os.Stat(destPath); os.IsNotExist(err) {
			return false
		}
	}
	for _, pat := range patterns {
		if matchPath(pat, relPath) {
			return true
		}
	}
	return false
}

// matchPath checks whether a glob-like pattern matches a relative file path.
// Patterns can be: "saves/*" (dir prefix), "*.sav" (extension), "Game.ini" (exact basename).
func matchPath(pattern, relPath string) bool {
	// relPath comes from filepath.Walk/Rel, so on Windows it uses
	// backslashes; patterns are written with forward slashes. Normalize to
	// slash form and match with path.Match, which treats "/" as the only
	// separator on every platform, so behaviour is identical everywhere.
	relPath = filepath.ToSlash(relPath)
	// Exact path match
	if matched, _ := path.Match(pattern, relPath); matched {
		return true
	}
	// Basename match
	if matched, _ := path.Match(pattern, path.Base(relPath)); matched {
		return true
	}
	// Directory prefix match: "saves/*" matches "saves/anything"
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		return strings.HasPrefix(relPath, prefix+"/") || relPath == prefix
	}
	return false
}

// destPathSafe reports whether no component of destPath under root is a
// symlink that escapes root.
func destPathSafe(destPath, root string) bool {
	cur := root
	rel, err := filepath.Rel(root, destPath)
	if err != nil {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if part == "" || part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if err != nil {
			continue
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			tgt, err := os.Readlink(cur)
			if err != nil {
				return false
			}
			resolved := tgt
			if !filepath.IsAbs(tgt) {
				resolved = filepath.Join(filepath.Dir(cur), tgt)
			}
			rr, err := filepath.Rel(root, filepath.Clean(resolved))
			if err != nil || strings.HasPrefix(rr, "..") {
				return false
			}
		}
	}
	return true
}

// linkWithinRoot reports whether a symlink target stays within root.
func linkWithinRoot(target, linkDir, root string) bool {
	resolved := target
	if !filepath.IsAbs(target) {
		resolved = filepath.Join(linkDir, target)
	}
	resolved = filepath.Clean(resolved)
	rr, err := filepath.Rel(root, resolved)
	if err != nil || strings.HasPrefix(rr, "..") || rr == ".." {
		return false
	}
	// EvalSymlinks chain check.
	if ev, err := filepath.EvalSymlinks(filepath.Join(linkDir, target)); err == nil {
		rr2, err2 := filepath.Rel(root, ev)
		if err2 != nil || strings.HasPrefix(rr2, "..") {
			return false
		}
	}
	return true
}

func copyFile(src, dst string) error {
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()

	fi, err := s.Stat()
	if err != nil {
		return err
	}

	d, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer d.Close()

	if _, err := io.Copy(d, s); err != nil {
		return err
	}
	// Preserve the source's permission bits: os.Create applies 0666 &
	// umask, which drops the executable bit from game launchers
	// (.sh, .x86_64, .AppImage).
	if err := os.Chmod(dst, fi.Mode().Perm()); err != nil {
		return err
	}
	return d.Sync()
}

// findGameRoot checks if extractedDir contains exactly one subdirectory;
// if so, returns that subdirectory (the actual game root inside the extraction).
//
// It never unwraps a directory that is itself a game root: some releases put
// the launcher at the top level beside a single asset folder (RPGM MV's www/,
// an HTML game's img/, a Ren'Py build's renpy/). Unwrapping there discards the
// launcher and leaves only the assets, so the installed game has no executable.
// This mirrors extractor.findGameRoot; the two must agree because Merge can be
// handed either the raw extraction dir or the extractor's already-unwrapped one.
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

// patterns returns the user-data (overwrite-restore) preserve glob patterns
// for a given engine. These files are restored from the backup OVER the new
// version's copies, because they are genuine user data (saves, persistent
// state, user-added mods). Everything else: the new version wins.
func patterns(engine string) []string {
	specific, ok := enginePatterns[engine]
	if !ok {
		specific = enginePatterns["default"]
	}
	out := append([]string{}, commonPatterns...)
	out = append(out, specific...)
	return out
}

// keepIfAbsentPatterns returns patterns whose files are copied from the backup
// ONLY when the new version does not ship that file. Used for user-created
// config that a shipped default should override.
func keepIfAbsentPatterns(engine string) []string {
	specific, ok := keepIfAbsentEnginePatterns[engine]
	if !ok {
		specific = keepIfAbsentEnginePatterns["default"]
	}
	out := append([]string{}, specific...)
	return out
}

// allEnginePatterns returns both sets combined (used by tests that want the
// full preserve list for an engine).
func allPreservePatterns(engine string) []string {
	out := append([]string{}, patterns(engine)...)
	out = append(out, keepIfAbsentPatterns(engine)...)
	return out
}

var commonPatterns = []string{
	"*.sav", "*.save",
	"persistent",
}

// enginePatterns: user data restored over new files.
var enginePatterns = map[string][]string{
	"Others":       {"saves/*", "save/*", "mods/*", "mod/*"},
	"ADRIFT":       {"saves/*", "save/*"},
	"Flash":        {"save/*"},
	"Godot":        {"saves/*", "save/*", "mods/*", "mod/*"},
	"HTML":         {"save/*", "www/save/*"},
	"Java":         {"saves/*", "save/*"},
	"QSP":          {"saves/*", "save/*"},
	"RAGS":         {"saves/*", "save/*"},
	"RPGM":         {"save/*", "www/save/*", "achievements.rpgsave"},
	"RenPy":        {"game/saves/*", "game/save/*", "game/persistent"},
	"Tads":         {"saves/*", "save/*"},
	"Unity":        {"saves/*", "*_Data/saves/*"},
	"UnrealEngine": {"saves/*", "save/*", "mods/*", "mod/*"},
	"WebGL":        {"saves/*", "save/*"},
	"WolfRPG":      {"Save*/*", "Data/BasicData/*"},
	"default":      {"saves/*", "save/*", "Save*/*", "mods/*", "mod/*", "game/mods/*"},
}

// keepIfAbsentEnginePatterns: copied from backup only if absent in new version.
var keepIfAbsentEnginePatterns = map[string][]string{
	"RPGM":    {"Game.ini", "package.json"},
	"WolfRPG": {"Game.ini"},
	"Godot":   {"override.cfg"},
	"default": {},
}
