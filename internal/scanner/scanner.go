package scanner

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mili/moxie/internal/engine"
)

// DetectedGame is the result of scanning a game directory.
type DetectedGame struct {
	Title     string        `json:"title"`     // directory name as title fallback
	Path      string        `json:"path"`      // absolute directory path
	ExePath   string        `json:"exe_path"`  // path to main executable
	Engine    engine.Engine `json:"engine"`
	Version   string        `json:"version"`   // version extracted from directory name
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
			// If the root itself is inaccessible, surface the error.
			if path == root {
				return err
			}
			// Skip individual inaccessible paths (permission errors on
			// single files/dirs). Do NOT return the error — that would
			// stop the walk.
			return nil
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
		return nil, fmt.Errorf("scan: %w", err)
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
	name := filepath.Base(dir)
	g := DetectedGame{
		Title:     name,
		Path:      dir,
		Engine:    result.Engine,
		Version:   ExtractVersion(name),
		SizeBytes: dirSize(dir),
	}
	// Find the main executable.
	if exe := findGameExe(dir); exe != "" {
		g.ExePath = exe
	}
	return g
}

// version patterns tried in order; first match wins.
var (
	// Date-based versions: "2025-11-14", "2026-03-31"
	dateVerRE = regexp.MustCompile(`\b(\d{4}-\d{2}-\d{2})\b`)
	// Dot-separated with optional v/V prefix: v1.0.3, 1.0, V5.4.91, B.0.10.7.5.2
	dotVerRE = regexp.MustCompile(`\b[vV]?[a-zA-Z]?\d+\.\d+(?:\.\d+)*(?:\s*HotFix)?\b`)
	// Underscore-separated: v1_0_3, 1_0, V5_4_91
	usVerRE = regexp.MustCompile(`\b[vV]?\d+_\d+(?:_\d+)*\b`)
	// Dash-separated: 0-20-16, v1-0-3 (converts to dots)
	dashVerRE = regexp.MustCompile(`\b[vV]?\d+(?:[._-]\d+)+\b`)
)

// ExtractVersion attempts to pull a version string from a directory/file name.
// Patterns are tried: date-like, dot-separated, then underscore-separated.
// Returns empty string if no version is found.
func ExtractVersion(name string) string {
	if name == "" {
		return ""
	}
	// Try date pattern first (most specific).
	if m := dateVerRE.FindString(name); m != "" {
		return m
	}
	// Try dot-separated version.
	if m := dotVerRE.FindString(name); m != "" {
		// Clean: strip leading v/V prefix.
		ver := strings.TrimLeft(m, "vV")
		return strings.TrimSpace(ver)
	}
	// Try dash-separated, convert dashes to dots.
	if m := dashVerRE.FindString(name); m != "" {
		ver := strings.TrimLeft(m, "vV")
		ver = strings.ReplaceAll(ver, "-", ".")
		ver = strings.ReplaceAll(ver, "_", ".")
		return strings.TrimSpace(ver)
	}
	// Try underscore-separated, convert underscores to dots.
	if m := usVerRE.FindString(name); m != "" {
		ver := strings.TrimLeft(m, "vV")
		return strings.ReplaceAll(ver, "_", ".")
	}
	return ""
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
