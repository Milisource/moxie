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

// ScanProgressFunc is an optional callback for reporting scan progress.
// The dirsExamined and gamesFound counters update as the walk progresses.
// phase is "walk" (first pass, finding games) or "detect" (second pass, engine detection).
type ScanProgressFunc func(dirsExamined, gamesFound int, phase string)

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
	return ScanFiltered(root, nil, nil)
}

// ScanFiltered is like Scan but skips game directories whose paths
// are present in skipPaths. When skipPaths is nil or empty, behaves
// identically to Scan. This allows callers to implement incremental
// scans by passing the set of already-known game paths.
// progress is an optional callback that reports dirs examined and games found.
func ScanFiltered(root string, skipPaths map[string]bool, progress ScanProgressFunc) ([]DetectedGame, error) {
	root = filepath.Clean(root)

	// Single walk: detect game directories and accumulate file sizes
	// simultaneously by tracking which game dir we're currently inside.
	type trackedGame struct {
		size int64
	}
	gameDirs := make(map[string]*trackedGame)
	var currentGameDir string

	dirsExamined := 0

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

		dirsExamined++
		if progress != nil {
			progress(dirsExamined, len(gameDirs), "walk")
		}

		name := d.Name()
		// Skip __MACOSX (macOS resource fork duplicates).
		if name == "__MACOSX" {
			return filepath.SkipDir
		}
		// Skip .old directories (updater rollback backups from Merge).
		if strings.HasSuffix(name, ".old") {
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
		// Check if this directory looks like a game root using a single
		// directory read (avoids redundant os.ReadDir in hasGameMarkers).
		entries, readErr := os.ReadDir(path)
		if readErr != nil || !hasGameMarkersFromEntries(entries) {
			return nil
		}
		// If --new-only is active and this path is already known, skip it.
		if skipPaths != nil && skipPaths[path] {
			return filepath.SkipDir
		}
		// If it's named after a known engine and has subdirectories,
		// it's a category folder — walk children instead.
		if isEngineName(strings.ToLower(name)) && hasSubDir(entries) {
			return nil
		}
		gameDirs[path] = &trackedGame{}
		currentGameDir = path // descend to accumulate sizes
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
	var detectedCount int64
	for dir, tg := range gameDirs {
		wg.Add(1)
		go func(d string, size int64) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			result := engine.Detect(d)
			ver := ExtractVersion(filepath.Base(d))
			if ver == "" {
				ver = ExtractVersionFromDir(d)
			}
			if ver == "" {
				// Nested games: version is often in the parent directory
				// name (e.g. "Game v1.0/Game/" → "1.0" from parent).
				if parent := filepath.Dir(d); parent != d {
					ver = ExtractVersion(filepath.Base(parent))
				}
			}
			g := DetectedGame{
				Title:     filepath.Base(d),
				Path:      d,
				Engine:    result.Engine,
				Version:   ver,
				SizeBytes: size,
			}
			if exe := findGameExe(d); exe != "" {
				g.ExePath = exe
				// Some games only have the version in the executable
				// filename (e.g. "[Full]EmberDoors_v0.1.7_Linux.x86_64").
				if ver == "" {
					if exeVer := ExtractVersion(filepath.Base(exe)); exeVer != "" {
						ver = exeVer
						g.Version = ver
					}
				}
			}
			mu.Lock()
			games = append(games, g)
			detectedCount++
			if progress != nil {
				progress(dirsExamined, int(detectedCount), "detect")
			}
			mu.Unlock()
		}(dir, tg.size)
	}
	wg.Wait()

	return games, nil
}

// ScanSingle detects the engine for a single directory without walking.
// Returns a DetectedGame with engine, title, version, size, and exe path.
func ScanSingle(dir string) DetectedGame {
	return analyzeDir(filepath.Clean(dir), "")
}

// analyzeDir runs engine detection and computes size for a directory.
func analyzeDir(dir, root string) DetectedGame {
	result := engine.Detect(dir)
	name := filepath.Base(dir)

	ver := ExtractVersion(name)
	if ver == "" {
		ver = ExtractVersionFromDir(dir)
	}
	if ver == "" {
		if parent := filepath.Dir(dir); parent != dir {
			ver = ExtractVersion(filepath.Base(parent))
		}
	}

	g := DetectedGame{
		Title:     name,
		Path:      dir,
		Engine:    result.Engine,
		Version:   ver,
		SizeBytes: dirSize(dir),
	}
	if exe := findGameExe(dir); exe != "" {
		g.ExePath = exe
		if ver == "" {
			if exeVer := ExtractVersion(filepath.Base(exe)); exeVer != "" {
				g.Version = exeVer
			}
		}
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
	// Compact date versions: "20260403" (YYYYMMDD, no separators).
	// Uses \D boundary so dates attached to words like "Data20260403"
	// are matched. Year/month/day validation prevents false positives
	// on arbitrary 8-digit numbers like "Game12345678".
	yyyymmddRE = regexp.MustCompile(`(?:\D|^)((?:19|20)\d{2}(?:0[1-9]|1[0-2])(?:0[1-9]|[12]\d|3[01]))(?:\D|$)`)
	// Dot-separated with optional v/V prefix: v1.0.3, 1.0, V5.4.91, B.0.10.7.5.2
	// Trailing [a-zA-Z]? catches build identifiers (e.g. v0.7.7i).
	dotVerRE = regexp.MustCompile(`(?:^|[^a-zA-Z0-9])([vV]?[a-zA-Z]?\d+\.\d+(?:\.\d+)*(?:\s*HotFix)?[a-zA-Z]?)(?:$|[^a-zA-Z0-9])`)
	// Dash/underscore/dot-separated: 0-20-16, v1-0-3, v1_0_3 (converts to dots)
	dashVerRE = regexp.MustCompile(`(?:^|[^a-zA-Z0-9])([vV]?\d+(?:[._-]\d+)+)(?:$|[^a-zA-Z0-9])`)
	// Underscore-separated fallback: v1_0_3, 1_0, V5_4_91
	usVerRE = regexp.MustCompile(`(?:^|[^a-zA-Z0-9])([vV]?\d+_\d+(?:_\d+)*)(?:$|[^a-zA-Z0-9])`)
	// Single/double-digit versions with v prefix: v5, v01, v0
	singleVerRE = regexp.MustCompile(`(?:^|[^a-zA-Z0-9])([vV]\d{1,2})(?:$|[^a-zA-Z0-9])`)

	// File-content version regexes (used by ExtractVersionFromDir).
	verIniRE = regexp.MustCompile(`(?i)\bver(?:sion)?\.?\s*`)
	pkgVerRE = regexp.MustCompile(`"version"\s*:\s*"([^"]+)"`)
	rpyVerRE = regexp.MustCompile(`(?i)define\s+config\.version\s*=\s*"([^"]+)"`)
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
	// Try compact YYYYMMDD date (no separators).
	if m := yyyymmddRE.FindStringSubmatch(name); len(m) > 1 {
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

// hasGameMarkersFromEntries checks a pre-read directory listing for game
// engine files and executables. Extracted from hasGameMarkers so callers
// can avoid redundant os.ReadDir calls.
func hasGameMarkersFromEntries(entries []os.DirEntry) bool {
	hasExe := false
	hasMarkers := false

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			switch {
			case name == "renpy", name == "www", name == "Engine",
				strings.HasSuffix(name, "_Data"),
				strings.HasPrefix(name, "game") && !strings.HasPrefix(name, "games"):
				hasMarkers = true
			}
		} else {
			ext := strings.ToLower(filepath.Ext(name))
			switch ext {
			case ".exe", ".sh", ".app", ".x86_64", ".x86":
				if !shouldSkip(name) {
					hasExe = true
				}
			}
			switch {
			case name == "package.json",
				strings.HasSuffix(name, ".pck"),
				strings.HasSuffix(name, ".rpyc"),
				strings.HasSuffix(name, ".rpa"),
				strings.HasPrefix(name, "Game.rgss"):
				hasMarkers = true
			}
		}
	}

	return hasExe || hasMarkers
}

// hasGameMarkers checks for game engine files and executables by reading
// the directory listing. Prefer hasGameMarkersFromEntries when the listing
// has already been read to avoid redundant I/O.
func hasGameMarkers(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	return hasGameMarkersFromEntries(entries)
}

// hasSubDir returns true if the entries contain at least one subdirectory.
func hasSubDir(entries []os.DirEntry) bool {
	for _, e := range entries {
		if e.IsDir() {
			return true
		}
	}
	return false
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
// Uses exact-match map first, then substring fallback for prefix patterns.
func shouldSkip(name string) bool {
	lower := strings.ToLower(name)
	if exactExcluded[lower] {
		return true
	}
	for _, ex := range subExcluded {
		if strings.Contains(lower, ex) {
			return true
		}
	}
	return false
}

// Exclusion patterns for shouldSkip. Exact matches go in the map for O(1)
// lookup; substring patterns (prefixes like "unins") stay in the slice.
var (
	exactExcluded = map[string]bool{
		"config":    true,
		"saved":     true,
		"logs":      true,
		"crashes":   true,
	}
	subExcluded = []string{
		"unins",
		"unitycrashhandler",
		"notification_helper",
		"python",
		"pythonw",
		"zsync",
		"zsyncmake",
		"dxsetup",
		"vc_redist",
	}
)

// ExtractVersionFromDir tries to extract a version string from known files
// inside the game directory when the directory name itself contains no version.
// Checks, in order: Game.ini Title= field, package.json "version" field,
// game/options.rpy config.version (Ren'Py).
func ExtractVersionFromDir(dir string) string {
	// Try Game.ini (RPG Maker games) — Title= frequently contains a version.
	// Common patterns: "v1.05", "ver0.31", "v3.26", "B.0.7.9.1".
	iniPath := filepath.Join(dir, "Game.ini")
	if data, err := os.ReadFile(iniPath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(strings.ToLower(line), "title=") {
				continue
			}
			if idx := strings.Index(line, "="); idx >= 0 {
				val := strings.TrimSpace(line[idx+1:])
				val = verIniRE.ReplaceAllString(val, "v")
				if ver := ExtractVersion(val); ver != "" {
					return ver
				}
			}
		}
	}

	// Try package.json (HTML/NW.js/Electron games).
	if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		if m := pkgVerRE.FindStringSubmatch(string(data)); len(m) > 1 {
			if ver := m[1]; ver != "" {
				return ver
			}
		}
	}

	// Try game/options.rpy (Ren'Py games).
	if data, err := os.ReadFile(filepath.Join(dir, "game", "options.rpy")); err == nil {
		if m := rpyVerRE.FindStringSubmatch(string(data)); len(m) > 1 {
			if ver := m[1]; ver != "" {
				return ver
			}
		}
	}

	return ""
}
