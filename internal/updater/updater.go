// Package updater merges downloaded game updates into existing game directories,
// preserving user saves, mods, and configuration files based on engine detection.
package updater

import (
	"fmt"
	"io"
	"os"
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
func Merge(gameDir, engine, extractedDir string, backup bool) (*MergeResult, error) {
	if _, err := os.Stat(gameDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("game directory does not exist: %s", gameDir)
	}

	srcDir := findGameRoot(extractedDir)
	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("extracted directory does not exist: %s", srcDir)
	}

	preserve := patterns(engine)
	result := &MergeResult{}

	// Optional backup
	if backup {
		backupPath := gameDir + ".old"
		os.RemoveAll(backupPath)
		if err := os.Rename(gameDir, backupPath); err != nil {
			log.Warn("merge backup failed", "game_dir", gameDir, "error", err)
		} else {
			result.BackupPath = backupPath
			if err := os.MkdirAll(gameDir, 0755); err != nil {
				return nil, fmt.Errorf("recreate game dir: %w", err)
			}
		}
	}

	// Copy new files
	if err := copyNew(srcDir, gameDir, preserve, result); err != nil {
		return result, err
	}

	// If we have a backup, restore preserved files from it
	if result.BackupPath != "" {
		restorePreserved(result.BackupPath, gameDir, preserve)
	}

	log.Info("merge complete", "game_dir", gameDir, "engine", engine,
		"copied", result.FilesCopied, "preserved", result.FilesPreserved)
	return result, nil
}

// copyNew walks srcDir and copies files to destDir, skipping any that match
// preserve patterns AND already exist in the destination.
func copyNew(srcDir, destDir string, preserve []string, result *MergeResult) error {
	return filepath.Walk(srcDir, func(srcPath string, info os.FileInfo, err error) error {
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
func restorePreserved(backupDir, gameDir string, preserve []string) {
	filepath.Walk(backupDir, func(srcPath string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		relPath, _ := filepath.Rel(backupDir, srcPath)
		if shouldPreserve(relPath, "", preserve) {
			destPath := filepath.Join(gameDir, relPath)
			os.MkdirAll(filepath.Dir(destPath), 0755)
			if copyFile(srcPath, destPath) == nil {
				log.Debug("restored user file from backup", "path", relPath)
			}
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
	// Exact path match
	if matched, _ := filepath.Match(pattern, relPath); matched {
		return true
	}
	// Basename match
	if matched, _ := filepath.Match(pattern, filepath.Base(relPath)); matched {
		return true
	}
	// Directory prefix match: "saves/*" matches "saves/anything"
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		return strings.HasPrefix(relPath, prefix+"/") || relPath == prefix
	}
	return false
}

func copyFile(src, dst string) error {
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()

	d, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer d.Close()

	if _, err := io.Copy(d, s); err != nil {
		return err
	}
	return d.Sync()
}

// findGameRoot checks if extractedDir contains exactly one subdirectory;
// if so, returns that subdirectory (the actual game root inside the extraction).
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
	if len(subdirs) == 1 {
		return filepath.Join(dir, subdirs[0])
	}
	return dir
}

// patterns returns the preserve glob patterns for a given engine.
func patterns(engine string) []string {
	specific, ok := enginePatterns[engine]
	if !ok {
		specific = enginePatterns["default"]
	}
	return append(commonPatterns, specific...)
}

var commonPatterns = []string{
	"*.sav", "*.save",
	"persistent",
}

var enginePatterns = map[string][]string{
	"RPGM": {
		"save/*",
		"www/save/*",
		"Game.ini",
		"package.json",
		"achievements.rpgsave",
	},
	"RenPy": {
		"game/saves/*",
		"game/save/*",
		"game/options.rpy",
		"game/screens.rpy",
		"game/gui.rpy",
		"game/persistent",
	},
	"Unity": {
		"saves/*",
		"*_Data/saves/*",
	},
	"HTML": {
		"save/*",
		"www/save/*",
		"package.json",
	},
	"WolfRPG": {
		"Save*/*",
		"Game.ini",
		"Data/BasicData/*",
	},
	"Flash": {
		"save/*",
	},
	"Java": {
		"saves/*",
		"save/*",
	},
	"default": {
		"saves/*", "save/*",
		"Save*/*",
		"mods/*", "mod/*", "game/mods/*",
		"Game.ini",
		"options.rpy", "options.*",
		"*.cfg", "*.json",
	},
}
