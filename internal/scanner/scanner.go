package scanner

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

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
// Sizes are accumulated in a single walk — no separate dirSize pass.
func Scan(root string) ([]DetectedGame, error) {
	root = filepath.Clean(root)

	// Single walk: detect game directories and accumulate file sizes
	// simultaneously by tracking which game dir we're currently inside.
	type trackedGame struct {
		size int64
	}
	gameDirs := make(map[string]*trackedGame)
	var currentGameDir string

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

		// Check if we're inside a known game directory. WalkDir is
		// depth-first, so once we enter a game dir we stay inside it
		// until all its descendants are visited.
		if currentGameDir != "" && strings.HasPrefix(path, currentGameDir+string(filepath.Separator)) {
			if !d.IsDir() {
				if info, infoErr := d.Info(); infoErr == nil {
					gameDirs[currentGameDir].size += info.Size()
				}
			}
			return nil
		}
		currentGameDir = "" // we've left the current game dir

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
		// Use separator-aware comparison to avoid path-prefix collisions
		// (e.g., /games/foo must not match /games/foobar/SomeGame).
		// This guard is only needed when currentGameDir is empty (we're
		// between game dirs) — inside a game dir the check above catches
		// all descendants first.
		parent := filepath.Dir(path)
		for dir := range gameDirs {
			if strings.HasPrefix(parent, dir+string(filepath.Separator)) && parent != dir {
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
			gameDirs[path] = &trackedGame{}
			currentGameDir = path // descend to accumulate sizes
			return nil
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	// Second pass: run engine detection on each game directory in parallel
	// (sizes are already computed from the single walk).
	var games []DetectedGame
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for dir, tg := range gameDirs {
		wg.Add(1)
		go func(d string, size int64) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			result := engine.Detect(d)
			g := DetectedGame{
				Title:     filepath.Base(d),
				Path:      d,
				Engine:    result.Engine,
				Version:   ExtractVersion(filepath.Base(d)),
				SizeBytes: size,
			}
			if exe := findGameExe(d); exe != "" {
				g.ExePath = exe
			}
			mu.Lock()
			games = append(games, g)
			mu.Unlock()
		}(dir, tg.size)
	}
	wg.Wait()

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
// NOTE: \b in Go regex treats _ as a word character, so versions delimited
// by underscores are not bounded by \b. Instead we use explicit
// (?:^|[^a-zA-Z0-9]) / (?:$|[^a-zA-Z0-9]) boundaries and capture groups.
var (
	// Date-based versions: "2025-11-14", "2026-03-31"
	dateVerRE = regexp.MustCompile(`(?:^|[^a-zA-Z0-9])(\d{4}-\d{2}-\d{2})(?:$|[^a-zA-Z0-9])`)
	// Dot-separated with optional v/V prefix: v1.0.3, 1.0, V5.4.91, B.0.10.7.5.2
	// Trailing [a-zA-Z]? catches build identifiers (e.g. v0.7.7i).
	dotVerRE = regexp.MustCompile(`(?:^|[^a-zA-Z0-9])([vV]?[a-zA-Z]?\d+\.\d+(?:\.\d+)*(?:\s*HotFix)?[a-zA-Z]?)(?:$|[^a-zA-Z0-9])`)
	// Dash/underscore/dot-separated: 0-20-16, v1-0-3, v1_0_3 (converts to dots)
	dashVerRE = regexp.MustCompile(`(?:^|[^a-zA-Z0-9])([vV]?\d+(?:[._-]\d+)+)(?:$|[^a-zA-Z0-9])`)
	// Underscore-separated fallback: v1_0_3, 1_0, V5_4_91
	usVerRE = regexp.MustCompile(`(?:^|[^a-zA-Z0-9])([vV]?\d+_\d+(?:_\d+)*)(?:$|[^a-zA-Z0-9])`)
	// Single/double-digit versions with v prefix: v5, v01, v0
	singleVerRE = regexp.MustCompile(`(?:^|[^a-zA-Z0-9])([vV]\d{1,2})(?:$|[^a-zA-Z0-9])`)
)

// ExtractVersion attempts to pull a version string from a directory/file name.
// Patterns are tried: date-like, dot-separated, then dash/underscore-separated,
// and finally single/double-digit with v prefix.
// Returns empty string if no version is found.
func ExtractVersion(name string) string {
	if name == "" {
		return ""
	}
	// Try date pattern first (most specific).
	if m := dateVerRE.FindStringSubmatch(name); len(m) > 1 {
		return m[1]
	}
	// Try dot-separated version.
	if m := dotVerRE.FindStringSubmatch(name); len(m) > 1 {
		ver := strings.TrimLeft(m[1], "vV")
		return strings.TrimSpace(ver)
	}
	// Try dash-separated, convert dashes/underscores to dots.
	if m := dashVerRE.FindStringSubmatch(name); len(m) > 1 {
		ver := strings.TrimLeft(m[1], "vV")
		ver = strings.ReplaceAll(ver, "-", ".")
		ver = strings.ReplaceAll(ver, "_", ".")
		return strings.TrimSpace(ver)
	}
	// Try underscore-separated, convert underscores to dots.
	if m := usVerRE.FindStringSubmatch(name); len(m) > 1 {
		ver := strings.TrimLeft(m[1], "vV")
		return strings.ReplaceAll(ver, "_", ".")
	}
	// Try single/double-digit versions with v prefix.
	if m := singleVerRE.FindStringSubmatch(name); len(m) > 1 {
		ver := strings.TrimLeft(m[1], "vV")
		return strings.TrimSpace(ver)
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
