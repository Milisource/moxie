package updater

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mili/moxie/internal/log"
)

// MergeOverlay applies a patch-style ("Update only") archive: files from
// extractedDir are copied over gameDir in place and nothing is deleted, so
// game files the patch doesn't carry survive. (Merge, by contrast, rebuilds
// gameDir from the archive and would drop them.)
//
// Every file the overlay overwrites is first copied to gameDir+".patch-old";
// on failure or cancellation those originals are restored and files the
// overlay created are removed, so the game is never left half-patched. On
// success the backup is discarded.
//
// Preserved user files (saves, configs) that already exist are left alone,
// exactly as in Merge.
func MergeOverlay(ctx context.Context, gameDir, engine, extractedDir string) (*MergeResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if fi, err := os.Stat(gameDir); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("game directory does not exist: %s", gameDir)
	}
	srcDir := findGameRoot(extractedDir)
	destDir := overlayTarget(srcDir, gameDir)

	backupDir := gameDir + ".patch-old"
	os.RemoveAll(backupDir)
	preserve := patterns(engine)
	result := &MergeResult{}

	var created []string  // files that did not exist before
	var replaced []string // rel paths (to gameDir) backed up in backupDir
	committed := false
	defer func() {
		if committed {
			os.RemoveAll(backupDir)
			return
		}
		for _, p := range created {
			os.Remove(p)
		}
		for _, rel := range replaced {
			if err := copyFile(filepath.Join(backupDir, rel), filepath.Join(gameDir, rel)); err != nil {
				log.Warn("overlay rollback: restore file", "path", rel, "error", err)
			}
		}
		os.RemoveAll(backupDir)
	}()

	err := filepath.Walk(srcDir, func(srcPath string, info os.FileInfo, err error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err != nil {
			return err
		}
		relSrc, _ := filepath.Rel(srcDir, srcPath)
		if relSrc == "." {
			return nil
		}
		destPath := filepath.Join(destDir, relSrc)
		if info.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}
		relGame, _ := filepath.Rel(gameDir, destPath)
		if shouldPreserve(relGame, destPath, preserve) {
			result.FilesPreserved++
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return err
		}
		if _, statErr := os.Stat(destPath); statErr == nil {
			bak := filepath.Join(backupDir, relGame)
			if err := os.MkdirAll(filepath.Dir(bak), 0755); err != nil {
				return err
			}
			if err := copyFile(destPath, bak); err != nil {
				return fmt.Errorf("back up %s: %w", relGame, err)
			}
			replaced = append(replaced, relGame)
		} else {
			created = append(created, destPath)
		}
		if err := copyFile(srcPath, destPath); err != nil {
			return fmt.Errorf("copy %s: %w", relGame, err)
		}
		result.FilesCopied++
		return nil
	})
	if err != nil {
		return result, err
	}
	committed = true
	log.Info("overlay merge complete", "game_dir", gameDir, "target", destDir,
		"copied", result.FilesCopied, "replaced", len(replaced), "preserved", result.FilesPreserved)
	return result, nil
}

// overlayTarget decides where a patch's root lands inside gameDir. The
// extractor unwraps a single top-level folder, so a Ren'Py patch holding
// only "game/" arrives as the contents of game/ — those must go to
// gameDir/game, not gameDir. Whichever of gameDir and gameDir/<base(src)>
// already holds more of the patch's top-level entries wins; ties go to
// gameDir.
func overlayTarget(srcDir, gameDir string) string {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return gameDir
	}
	nested := filepath.Join(gameDir, filepath.Base(srcDir))
	if fi, err := os.Stat(nested); err != nil || !fi.IsDir() {
		return gameDir
	}
	rootHits, nestedHits := 0, 0
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(gameDir, e.Name())); err == nil {
			rootHits++
		}
		if _, err := os.Stat(filepath.Join(nested, e.Name())); err == nil {
			nestedHits++
		}
	}
	if nestedHits > rootHits {
		return nested
	}
	return gameDir
}

// RemoveBackup deletes the gameDir+".old" snapshot Merge leaves behind
// (and a stale ".old.old" staging copy). Call it once the merged game has
// been verified; until then the backup is the only intact pre-merge copy.
func RemoveBackup(gameDir string) error {
	var firstErr error
	for _, p := range []string{gameDir + ".old.old", gameDir + ".old"} {
		if err := os.RemoveAll(p); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ContainsLauncher reports whether an extracted update carries the game's
// launcher (exeRel, relative to the game dir) — at the same relative path
// or, failing that, by basename anywhere in the top two levels. An archive
// without the launcher is a patch, not a full build, and must be overlaid
// rather than replace the install. An empty exeRel returns true (unknown;
// keep the full-merge behaviour).
func ContainsLauncher(extractedDir, exeRel string) bool {
	if exeRel == "" {
		return true
	}
	srcDir := findGameRoot(extractedDir)
	if _, err := os.Stat(filepath.Join(srcDir, exeRel)); err == nil {
		return true
	}
	base := filepath.Base(exeRel)
	found := false
	filepath.WalkDir(srcDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(srcDir, p)
		if d.IsDir() {
			if rel != "." && depth(rel) >= 2 {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == base {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func depth(rel string) int {
	n := 1
	for _, c := range rel {
		if c == filepath.Separator {
			n++
		}
	}
	return n
}
