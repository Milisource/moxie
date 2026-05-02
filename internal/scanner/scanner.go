package scanner

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mili/moxie/internal/engine"
)

// DetectedGame is the result of scanning a game directory.
type DetectedGame struct {
	Title     string        `json:"title"`     // directory name as title fallback
	Path      string        `json:"path"`      // absolute directory path
	ExePath   string        `json:"exe_path"`  // path to main executable
	Engine    engine.Engine `json:"engine"`
	SizeBytes int64         `json:"size_bytes"`
}

// Scan recursively scans a directory and returns detected games.
// It skips known non-game paths and engine crash handlers.
func Scan(root string) ([]DetectedGame, error) {
	root = filepath.Clean(root)
	var games []DetectedGame

	// First pass: collect all potential game directories.
	gameDirs := make(map[string]bool)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible paths
		}
		if !d.IsDir() {
			return nil
		}

		name := d.Name()
		// Skip __MACOSX (macOS resource fork duplicates).
		if name == "__MACOSX" {
			return filepath.SkipDir
		}
		// Skip excluded directories.
		if shouldSkip(name) {
			return filepath.SkipDir
		}
		// Don't recurse into subdirectories of already-detected game dirs.
		parent := filepath.Dir(path)
		for dir := range gameDirs {
			if strings.HasPrefix(parent, dir) && parent != dir {
				return filepath.SkipDir
			}
		}
		// Check if this directory looks like a game root.
		if looksLikeGameRoot(path) {
			// If this directory is named after a known engine AND contains
			// game-like subdirectories, it's a category folder — skip it
			// and scan its children instead.
			if isCategoryDir(path) {
				return nil
			}
			gameDirs[path] = true
			return filepath.SkipDir // don't recurse into game dirs
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Second pass: run engine detection on each game directory.
	for dir := range gameDirs {
		g := analyzeDir(dir, root)
		games = append(games, g)
	}

	return games, nil
}

// ScanSingle detects the engine for a single directory without walking.
func ScanSingle(dir string) (DetectedGame, error) {
	return analyzeDir(filepath.Clean(dir), ""), nil
}

// analyzeDir runs engine detection and computes size for a directory.
func analyzeDir(dir, root string) DetectedGame {
	result := engine.Detect(dir)
	g := DetectedGame{
		Title:     filepath.Base(dir),
		Path:      dir,
		Engine:    result.Engine,
		SizeBytes: dirSize(dir),
	}
	// Find the main executable.
	if exe := findGameExe(dir); exe != "" {
		g.ExePath = exe
	}
	return g
}

// looksLikeGameRoot checks if a directory contains game-like files.
func looksLikeGameRoot(dir string) bool {
	return hasGameMarkers(dir)
}

// hasGameMarkers checks for game engine files and executables.
func hasGameMarkers(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}

	hasExe := false
	hasGameMarkers := false

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			// Check for engine-specific directories.
			switch {
			case name == "renpy", name == "www", name == "Engine",
				strings.HasSuffix(name, "_Data"),
				strings.HasPrefix(name, "game") && !strings.HasPrefix(name, "games"):
				hasGameMarkers = true
			}
		} else {
			// Check for executable files.
			ext := strings.ToLower(filepath.Ext(name))
			switch ext {
			case ".exe", ".sh", ".app", ".x86_64", ".x86":
				if !shouldSkip(name) {
					hasExe = true
				}
			}
			// Check for engine marker files.
			switch {
			case name == "package.json",
				strings.HasSuffix(name, ".pck"),
				strings.HasSuffix(name, ".rpyc"),
				strings.HasSuffix(name, ".rpa"),
				strings.HasPrefix(name, "Game.rgss"):
				hasGameMarkers = true
			}
		}
	}

	return hasExe || hasGameMarkers
}

// isCategoryDir returns true if the directory name matches a known engine
// name AND the directory contains at least one subdirectory that looks like
// a game root. This prevents category folders like "UNITY/", "RPGM/" etc.
// from being detected as games themselves.
func isCategoryDir(dir string) bool {
	name := strings.ToLower(filepath.Base(dir))
	if !isEngineName(name) {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() && hasGameMarkers(filepath.Join(dir, e.Name())) {
			return true
		}
	}
	return false
}

// isEngineName returns true if the given lowercase name matches a known
// game engine or common category directory name.
func isEngineName(name string) bool {
	switch name {
	case "unity", "ren'py", "renpy", "rpgm", "rpgmaker",
		"godot", "unreal", "electron", "html", "java",
		"flash", "mugen", "other", "others", "tools", "jre":
		return true
	}
	return false
}

// findGameExe finds the main executable in a game directory.
func findGameExe(dir string) string {
	var best string
	var bestSize int64

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".exe" && ext != ".sh" && ext != ".x86_64" && ext != ".x86" {
			continue
		}
		if shouldSkip(name) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.Size() > bestSize {
			bestSize = info.Size()
			best = filepath.Join(dir, name)
		}
	}
	return best
}

// dirSize calculates the total size of a directory recursively.
func dirSize(dir string) int64 {
	var size int64
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		size += info.Size()
		return nil
	})
	return size
}

// shouldSkip returns true if a file/dir name matches the exclusion list.
func shouldSkip(name string) bool {
	lower := strings.ToLower(name)
	excluded := []string{
		"unins", "uninstall", "uninst",
		"unitycrashhandler", "notification_helper",
		"python", "pythonw", "zsync", "zsyncmake",
		"dxsetup", "vc_redist",
		"config", "saved", "logs", "crashes",
	}
	for _, ex := range excluded {
		if strings.Contains(lower, ex) {
			return true
		}
	}
	return false
}
