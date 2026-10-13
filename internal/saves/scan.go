package saves

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/mili/moxie/internal/db"
)

const (
	// scanWalkDepth bounds how deep a scan descends below each root. Live save
	// directories sit shallowly (e.g. game/saves, www/save), so anything deeper
	// is a game's own asset tree, not a save location.
	scanWalkDepth = 6

	// scanCandidateLimit caps how many scanned candidates are returned.
	scanCandidateLimit = 10
)

// ScanCandidate is a directory that plausibly holds the player's live saves.
type ScanCandidate struct {
	// Path is the directory containing save-ish files.
	Path string
	// Files is the number of matching save files directly inside Path.
	Files int
	// NewestFile is the base name of the most recently modified match.
	NewestFile string
	// NewestMod is that file's modification time.
	NewestMod time.Time
}

// ScanLiveSaves hunts for the player's real save location: it walks the game
// directory and the engine's known external roots, matches saveFilePatterns, and
// returns the containing directories ranked by most recent modification (newest
// first, capped at 10).
//
// Unlike ResolveLocation it makes no attempt to tie a directory to the game by
// name: it scans every directory holding recognizable save files, so a caller can
// offer manual candidates for a game whose save location auto-detection misses.
// Each directory is reported once, even when several roots contain it.
// It is read-only and never touches the database; a nil game yields nil.
func (e *Engine) ScanLiveSaves(game *db.Game) ([]ScanCandidate, error) {
	home, _ := os.UserHomeDir()
	return e.scanLiveSaves(game, home, defaultSteamRoots())
}

// scanLiveSaves is the hermetic core of ScanLiveSaves. home and steamRoots
// are injected so tests can point at fabricated directory trees; steamRoots
// entries may be "~"-relative (expanded via home) or absolute.
func (e *Engine) scanLiveSaves(game *db.Game, home string, steamRoots []string) ([]ScanCandidate, error) {
	if game == nil {
		return nil, nil
	}

	var roots []string
	if game.Path != "" {
		// The game directory itself, bounded so a game's own data tree does
		// not flood the candidate list.
		roots = append(roots, game.Path)
	}
	if isRenPyGame(game) {
		for _, root := range renpySaveRoots(game, home, steamRoots) {
			roots = append(roots, root.path)
		}
	}
	roots = append(roots, wineSaveRoots(game)...)

	var out []ScanCandidate
	seen := make(map[string]bool)
	for _, root := range dedupePaths(roots) {
		found, err := scanSaveDirs(root)
		if err != nil {
			return nil, err
		}
		for _, c := range found {
			// A directory reachable from two roots (e.g. the game directory
			// holds an external root) is reported once.
			if seen[c.Path] {
				continue
			}
			seen[c.Path] = true
			out = append(out, c)
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].NewestMod.Equal(out[j].NewestMod) {
			return out[i].NewestMod.After(out[j].NewestMod)
		}
		return out[i].Path < out[j].Path
	})
	if len(out) > scanCandidateLimit {
		out = out[:scanCandidateLimit]
	}
	return out, nil
}

// wineSaveRoots returns the save roots living inside the game's configured Wine
// prefix (game.WinePrefix). Ren'Py games keep saves under the prefix's RenPy
// roaming directory; RPG Maker games keep them under the roaming directory of
// each prefix user, where the engine's own save folder lives. Unknown prefixes
// and engines yield nothing.
func wineSaveRoots(game *db.Game) []string {
	if game == nil || game.WinePrefix == "" {
		return nil
	}
	users := filepath.Join(game.WinePrefix, "drive_c", "users", "*")
	switch {
	case isRenPyGame(game):
		return globDirs(filepath.Join(users, "AppData", "Roaming", "RenPy"))
	case isRPGMGame(game):
		return globDirs(filepath.Join(users, "AppData", "Roaming"))
	default:
		return nil
	}
}

// globDirs returns every path matching pattern that exists and is a directory,
// sorted for determinism.
func globDirs(pattern string) []string {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	sort.Strings(matches)

	var dirs []string
	for _, m := range matches {
		if saveDirExists(m) {
			dirs = append(dirs, m)
		}
	}
	return dirs
}

// dedupePaths removes duplicate roots (after cleaning) while preserving order.
func dedupePaths(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	var out []string
	for _, p := range paths {
		if p == "" {
			continue
		}
		clean := filepath.Clean(p)
		if seen[clean] {
			continue
		}
		seen[clean] = true
		out = append(out, clean)
	}
	return out
}

// scanSaveDirs walks root (bounded by scanWalkDepth, symlinks skipped) and
// returns one ScanCandidate per directory that directly holds save-ish files.
// A missing or unreadable root yields nothing; an unreadable subdirectory is
// skipped so one bad folder cannot abort the whole scan.
func scanSaveDirs(root string) ([]ScanCandidate, error) {
	if !saveDirExists(root) {
		return nil, nil
	}
	root = filepath.Clean(root)

	var out []ScanCandidate
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.IsDir() || d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if path != root && dirDepth(root, path) > scanWalkDepth {
			return fs.SkipDir
		}
		files, newest, mod := scanDirSaveFiles(path)
		if files == 0 {
			return nil
		}
		out = append(out, ScanCandidate{Path: path, Files: files, NewestFile: newest, NewestMod: mod})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("saves: scan %s: %w", root, err)
	}
	return out, nil
}

// scanDirSaveFiles counts the save-ish files directly inside dir and reports the
// most recently modified one.
func scanDirSaveFiles(dir string) (files int, newestName string, newestMod time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, "", time.Time{}
	}
	for _, ent := range entries {
		if ent.IsDir() || !isSaveFileName(ent.Name()) {
			continue
		}
		info, err := ent.Info()
		if err != nil {
			continue
		}
		files++
		if info.ModTime().After(newestMod) {
			newestMod = info.ModTime()
			newestName = ent.Name()
		}
	}
	return files, newestName, newestMod
}
