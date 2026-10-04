package scanner

import (
	"bytes"
	"context"
	"fmt"
	"io"
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
	Title      string        `json:"title"`    // directory name as title fallback
	Path       string        `json:"path"`     // absolute directory path
	ExePath    string        `json:"exe_path"` // path to main executable
	Engine     engine.Engine `json:"engine"`
	Version    string        `json:"version"` // version extracted from directory name
	SizeBytes  int64         `json:"size_bytes"`
	MatchedBy  string        `json:"matched_by,omitempty"` // which detection rule matched
	Confidence float64       `json:"confidence,omitempty"` // 0.0 - 1.0
}

// Scan recursively scans a directory and returns detected games.
// It skips known non-game paths and engine crash handlers.
// Sizes are accumulated in a single walk — no separate dirSize pass.
func Scan(ctx context.Context, root string) ([]DetectedGame, error) {
	return ScanFiltered(ctx, root, nil, nil)
}

// ScanFiltered is like Scan but skips game directories whose paths
// are present in skipPaths. When skipPaths is nil or empty, behaves
// identically to Scan. This allows callers to implement incremental
// scans by passing the set of already-known game paths.
// progress is an optional callback that reports dirs examined and games found.
// The walk and detection pass check ctx and abort promptly on cancellation
// (e.g. application shutdown on a slow or network-mounted scan path).
func ScanFiltered(ctx context.Context, root string, skipPaths map[string]bool, progress ScanProgressFunc) ([]DetectedGame, error) {
	root = filepath.Clean(root)

	// Single walk: detect game directories and accumulate file sizes
	// simultaneously by tracking which game dir we're currently inside.
	gameDirs := make(map[string]*trackedGame)
	var currentGameDir string

	dirsExamined := 0

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
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
		// Skip __MACOSX (macOS resource fork duplicates) and .old directories
		// (updater rollback backups from Merge). The scan root itself is
		// exempt — a library folder literally named "__MACOSX" or ending in
		// ".old" must still be scanned (see the shouldSkip guard below).
		if path != root {
			if name == "__MACOSX" {
				return filepath.SkipDir
			}
			if strings.HasSuffix(name, ".old") {
				return filepath.SkipDir
			}
		}
		// Skip excluded directories.
		if path != root && shouldSkip(name) {
			return filepath.SkipDir
		}
		// Don't recurse into subdirectories of already-detected game dirs.
		// Use separator-aware comparison to avoid path-prefix collisions
		// (e.g., /games/foo must not match /games/foobar/SomeGame).
		// This guard is only needed when currentGameDir is empty (we're
		// between game dirs) — inside a game dir the check above catches
		// all descendants first. currentGameDir short-circuits all
		// descendants in DFS order, so any directory still seen here is
		// provably outside every game dir — no extra scan needed.
		if currentGameDir == "" {
			parent := filepath.Dir(path)
			if parent != root && isUnderAnyGameDir(parent, gameDirs) {
				return filepath.SkipDir
			}
		}
		// If --new-only is active and this path is already known, skip it.
		// Checked before the directory read so incremental scans don't pay
		// an os.ReadDir + marker scan for every already-known game dir.
		if skipPaths != nil && skipPaths[path] {
			return filepath.SkipDir
		}
		// Check if this directory looks like a game root using a single
		// directory read (avoids redundant os.ReadDir in hasGameMarkers).
		entries, readErr := os.ReadDir(path)
		if readErr != nil {
			return nil
		}
		if !isGameRoot(path, entries) {
			// Collapse release wrappers: an outer folder that holds exactly one
			// game (possibly through nested wrappers) and shares its name with
			// that game becomes the registered entry, so the path, title, and
			// version come from the release folder instead of an inner
			// duplicate (e.g. "Brothel King/" not "Brothel King/Brothel
			// King/"). Engine-named category folders never collapse, and the
			// scan root is the user's container, never a game.
			if path != root && !isEngineName(strings.ToLower(name)) {
				if inner := soleGameDir(path, entries, 4); inner != "" &&
					wrapperMatchesName(name, filepath.Base(inner)) {
					gameDirs[path] = &trackedGame{resolveTo: inner}
					currentGameDir = path
				}
			}
			return nil
		}
		// Skip tool/utility directories (decrypters, unpackers, RPG Maker
		// toolkits) that sit inside a directory tree containing a real game.
		// Such dirs look like games because they ship executables (e.g.
		// SetupMenu.exe), but they are bundled utilities, not games. The
		// check is content-based, so it works regardless of walk order.
		// Standalone tool-named dirs are still scanned to avoid over-matching.
		if isToolDirName(name) && path != root && nestedInGameTree(path, root) {
			return filepath.SkipDir
		}
		// If it's named after a known engine and has subdirectories,
		// it's a category folder — walk children instead.
		if isEngineName(strings.ToLower(name)) && hasSubDir(entries) {
			return nil
		}
		// A directory that contains several complete games is a container
		// (library root, or a release folder bundling multiple games), not a
		// game itself — register its children instead. Without this a loose
		// engine file at a library root (e.g. a top-level .swf) would register
		// the whole library as a single game and swallow every child.
		if isContainerDir(path, entries) {
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
		if cerr := ctx.Err(); cerr != nil {
			break
		}
		wg.Add(1)
		go func(d string, tg *trackedGame) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// A collapsed release wrapper detects against the inner game dir
			// while reporting the wrapper's path and title.
			detectDir := d
			if tg.resolveTo != "" {
				detectDir = tg.resolveTo
			}
			result := engine.Detect(detectDir)
			exe := findGameExe(detectDir)
			g := DetectedGame{
				Title:      filepath.Base(d),
				Path:       d,
				ExePath:    exe,
				Engine:     result.Engine,
				Version:    resolveVersion(d, detectDir, exe),
				SizeBytes:  tg.size,
				MatchedBy:  result.MatchedBy,
				Confidence: result.Confidence,
			}
			mu.Lock()
			games = append(games, g)
			detectedCount++
			if progress != nil {
				progress(dirsExamined, int(detectedCount), "detect")
			}
			mu.Unlock()
		}(dir, tg)
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
//
// It mirrors the scanner's release-wrapper collapse so `moxie detect` reports
// what a scan would have registered: when dir is not itself a game root but
// wraps exactly one name-matching game (e.g. "Monster Girl Quest" around
// "Monster girl quest Part1,2,3 English"), engine detection, exe discovery,
// and version resolution descend into the inner game while the reported path
// and title stay the wrapper's. Without this, detect would report "no matching
// profile" for a wrapper the scanner classified correctly.
func analyzeDir(dir, root string) DetectedGame {
	detectDir := dir
	if entries, err := os.ReadDir(dir); err == nil {
		base := filepath.Base(dir)
		if !isGameRoot(dir, entries) && !isEngineName(strings.ToLower(base)) {
			if inner := soleGameDir(dir, entries, 4); inner != "" &&
				wrapperMatchesName(base, filepath.Base(inner)) {
				detectDir = inner
			}
		}
	}

	result := engine.Detect(detectDir)
	name := filepath.Base(dir)
	exe := findGameExe(detectDir)

	return DetectedGame{
		Title:      name,
		Path:       dir,
		ExePath:    exe,
		Engine:     result.Engine,
		Version:    resolveVersion(dir, detectDir, exe),
		SizeBytes:  dirSize(dir),
		MatchedBy:  result.MatchedBy,
		Confidence: result.Confidence,
	}
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

	// verPrefixRE normalises spelled-out version prefixes to a bare "v" so
	// the patterns above see them: "ver1.11", "Ver.0.80", "version 2",
	// "v.0.1", and the odd "ov1.0.3" some RPGM titles carry.
	verPrefixRE = regexp.MustCompile(`(?i)\b(?:ver(?:sion)?|o?v)\.?\s*(\d)`)
)

// ExtractVersion attempts to pull a version string from a directory/file name.
// Patterns are tried: date-like, dot-separated, then dash/underscore-separated,
// and finally single/double-digit with v prefix.
// Returns empty string if no version is found.
func ExtractVersion(name string) string {
	if name == "" {
		return ""
	}
	name = verPrefixRE.ReplaceAllString(name, "v$1")
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

// soleGameDir returns the single game directory reachable from dir through
// nested single-child wrapper folders, or "" when dir's subtree is not exactly
// one game. Internal/asset/tool folders are ignored; maxDepth bounds descent.
func soleGameDir(dir string, entries []os.DirEntry, maxDepth int) string {
	if maxDepth <= 0 {
		return ""
	}
	if isGameRoot(dir, entries) {
		return dir
	}
	var childDir string
	dirCount := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		lower := strings.ToLower(name)
		if engineInternalDirs[lower] || shouldSkip(name) || isToolDirName(name) ||
			strings.HasPrefix(lower, "jre") || strings.HasSuffix(lower, ".old") {
			continue
		}
		dirCount++
		if dirCount > 1 {
			return ""
		}
		childDir = filepath.Join(dir, name)
	}
	if dirCount != 1 {
		return ""
	}
	ce, err := os.ReadDir(childDir)
	if err != nil {
		return ""
	}
	return soleGameDir(childDir, ce, maxDepth-1)
}

// normalizeName lowercases a name and reduces punctuation to single spaces so
// folder names can be compared without caring about separators.
func normalizeName(s string) string {
	var b strings.Builder
	lastSpace := true
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastSpace = false
		} else if !lastSpace {
			b.WriteByte(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

// wrapperMatchesName reports whether a wrapper and its sole game child share a
// name (one is a prefix of the other, ignoring case, punctuation, and version
// suffixes). This targets duplicate-name release folders (Brothel King/Brothel
// King, Fox Girls v1.03.01/Fox Girls) without collapsing arbitrary grouping
// folders that merely happen to hold one game.
func wrapperMatchesName(wrapper, child string) bool {
	w, c := normalizeName(wrapper), normalizeName(child)
	if w == "" || c == "" {
		return false
	}
	return strings.HasPrefix(w, c) || strings.HasPrefix(c, w)
}

// isDateVersion reports whether v is a bare calendar date (a release date, not
// a game version). Such strings are common in download folder names
// (…_2024-08-17) and must not be reported as the installed version when a real
// version exists.
func isDateVersion(v string) bool {
	return dateVerRE.MatchString(v) || yyyymmddRE.MatchString(v)
}

// resolveVersion finds the best version string for a game directory, in
// priority order: directory name, file contents under gameDir, up to three
// parent directories (release wrappers), then the executable filename. A bare
// date is only returned when no non-date version exists anywhere. gameDir
// differs from dir when a release wrapper was collapsed: the version then
// comes from the wrapper name first, but from the inner game's files next.
func resolveVersion(dir, gameDir, exePath string) string {
	var dateFallback string

	// Name candidates: the registered dir (release wrapper) and, when a
	// wrapper was collapsed, the inner game dir. Prefer the more specific
	// version (more dot-separated components, then longer).
	best := ""
	bestScore := -1
	tryName := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if isDateVersion(v) {
			if dateFallback == "" {
				dateFallback = v
			}
			return
		}
		if score := strings.Count(v, ".")*1000 + len(v); score > bestScore {
			best, bestScore = v, score
		}
	}
	tryName(ExtractVersion(filepath.Base(dir)))
	if gameDir != dir {
		tryName(ExtractVersion(filepath.Base(gameDir)))
	}
	if best != "" {
		return best
	}

	consider := func(v string) (string, bool) {
		v = strings.TrimSpace(v)
		if v == "" {
			return "", false
		}
		if isDateVersion(v) {
			if dateFallback == "" {
				dateFallback = v
			}
			return "", false
		}
		return v, true
	}
	if v, ok := consider(ExtractVersionFromDir(gameDir)); ok {
		return v
	}
	p := dir
	for i := 0; i < 3; i++ {
		parent := filepath.Dir(p)
		if parent == p || parent == "" {
			break
		}
		if v, ok := consider(ExtractVersion(filepath.Base(parent))); ok {
			return v
		}
		p = parent
	}
	if exePath != "" {
		if v, ok := consider(ExtractVersion(filepath.Base(exePath))); ok {
			return v
		}
	}
	return dateFallback
}

// looksLikeGameRoot checks if a directory contains game-like files.
func looksLikeGameRoot(dir string) bool {
	return hasGameMarkers(dir)
}

// isUnderAnyGameDir reports whether parent is a subdirectory of any
// already-detected game root.
// trackedGame is the per-game-dir state accumulated during the single walk.
type trackedGame struct {
	size int64
	// resolveTo is the inner game directory when the registered path is a
	// collapsed release wrapper; empty when the path is the game root itself.
	resolveTo string
}

// isUnderAnyGameDir reports whether parent is a subdirectory of any
// already-detected game root.
func isUnderAnyGameDir(parent string, gameDirs map[string]*trackedGame) bool {
	for dir := range gameDirs {
		if strings.HasPrefix(parent, dir+string(filepath.Separator)) && parent != dir {
			return true
		}
	}
	return false
}

// hasGameMarkersFromEntries checks a pre-read directory listing for game
// engine files and executables. Extracted from hasGameMarkers so callers
// can avoid redundant os.ReadDir calls. It does not read file contents; for
// the full game-root test including the HTML content sniff use isGameRoot.
func hasGameMarkersFromEntries(entries []os.DirEntry) bool {
	hasExe := false
	hasMarkers := false

	for _, e := range entries {
		name := e.Name()
		lower := strings.ToLower(name)
		if e.IsDir() {
			switch {
			case name == "renpy", name == "www", name == "Engine",
				strings.HasSuffix(name, "_Data"),
				name == "game":
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
			case lower == "package.json",
				lower == "data.win",
				lower == "nscript.dat",
				lower == "flutter_windows.dll",
				strings.HasSuffix(lower, ".pck"),
				strings.HasSuffix(lower, ".rpyc"),
				strings.HasSuffix(lower, ".rpa"),
				strings.HasPrefix(lower, "game.rgss"),
				strings.HasSuffix(lower, ".swf"),
				strings.HasSuffix(lower, ".jar"),
				strings.HasSuffix(lower, ".qsp"),
				strings.HasSuffix(lower, ".qsps"),
				strings.HasSuffix(lower, ".taf"),
				strings.HasSuffix(lower, ".gam"),
				strings.HasSuffix(lower, ".t3"),
				strings.HasSuffix(lower, ".wolf"):
				hasMarkers = true
			}
		}
	}

	return hasExe || hasMarkers
}

// htmlSniffHead / htmlSniffTail bound the content-sniff read. Game entry
// HTML pages (Twine exports, canvas apps) put their signature either near the
// top (Twine <tw-storydata>, framework script tags) or at the end (Twine 1
// story data), and can be tens of MB, so a bounded head+tail read is used
// rather than reading the whole file.
const (
	htmlSniffHead = 256 << 10
	htmlSniffTail = 128 << 10
)

// htmlGameMarkers are lowercase substrings that mark an HTML file as a
// playable game rather than a static page, a docs site, or a viewer. Matched
// case-insensitively against a bounded head+tail read.
var htmlGameMarkers = [][]byte{
	[]byte("tw-storydata"), // Twine (all story formats)
	[]byte("sugarcube"),    // Twine SugarCube
	[]byte("harlowe"),      // Twine Harlowe
	[]byte("snowman"),      // Twine Snowman
	[]byte("<canvas"),      // canvas-driven games
	[]byte("pixi"),         // PixiJS
	[]byte("phaser"),       // Phaser
	[]byte("createjs"),     // CreateJS (Flash ports)
	[]byte("easeljs"),      // CreateJS
	[]byte("babylon"),      // Babylon.js
	[]byte("rpg_core.js"),  // RPG Maker MV/MZ web build
	[]byte("rpgmaker"),     // RPG Maker web
	[]byte("unityloader"),  // Unity WebGL
	[]byte("godot"),        // Godot web export
	[]byte("gamefiles"),    // hand-rolled JS games (e.g. Hentai University)
	[]byte("js/engine/"),   // hand-rolled JS game engines (e.g. A Lot of Ways)
}

// isGameRoot reports whether a directory is a game root, combining the
// fast file/dir markers with the HTML content sniff. The scanner and
// engine.Detect must agree on what counts as a game; this is the scanner side.
func isGameRoot(dir string, entries []os.DirEntry) bool {
	return hasGameMarkersFromEntries(entries) || hasHTMLGameSignature(dir, entries)
}

// engineInternalDirs names directories that belong to a game's own runtime or
// assets rather than being a separate game. They are excluded when deciding
// whether a directory is a container of multiple games.
var engineInternalDirs = map[string]bool{
	"game": true, "renpy": true, "www": true, "engine": true, "data": true,
	"resources": true, "locales": true, "swiftshader": true, "plugin": true,
	"chars": true, "stages": true, "sound": true, "font": true,
	"img": true, "images": true, "js": true, "css": true, "lib": true,
	"mod": true, "mods": true, "saves": true, "save": true, "savedata": true,
	"audio": true, "bgm": true, "se": true, "voice": true, "movie": true,
	"movies": true, "source": true, "src": true, "dist": true,
	"node_modules": true, "__macosx": true, ".git": true, "downloads": true,
	"credits": true, "docs": true, "video": true, "videos": true,
	"bin": true, "obj": true, "build": true, "target": true, "cache": true,
}

// isContainerDir reports whether dir holds at least two complete games rather
// than being a game itself (a library root, or a release folder bundling
// several games). Such directories are not registered; the walk descends into
// their children. Counting uses the stricter child predicate countsAsChildGame
// so bundled runtimes and asset folders do not inflate the count.
func isContainerDir(dir string, entries []os.DirEntry) bool {
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		lower := strings.ToLower(name)
		if engineInternalDirs[lower] || strings.HasPrefix(lower, "jre") ||
			strings.HasSuffix(lower, "_data") || shouldSkip(name) || isToolDirName(name) {
			continue
		}
		if countsAsChildGame(filepath.Join(dir, name)) {
			count++
			if count >= 2 {
				return true
			}
		}
	}
	return false
}

// countsAsChildGame reports whether a child directory is a complete game: it
// has a root-level executable, a game HTML entry page, or a strong engine
// marker. It deliberately omits loose formats (.jar/.swf/...) and weak markers
// so bundled runtimes (a jre*/ full of jars) and asset folders are not counted.
func countsAsChildGame(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".exe", ".sh", ".app", ".x86_64", ".x86":
			if !shouldSkip(e.Name()) {
				return true
			}
		}
	}
	return hasHTMLGameSignature(dir, entries) || hasStrongEngineMarker(entries)
}

// hasStrongEngineMarker reports whether a directory listing holds an
// unambiguous engine file or runtime directory. Used only for container
// detection, so it omits loose formats that bundled runtimes carry (e.g. jars
// inside a jre*/ directory).
func hasStrongEngineMarker(entries []os.DirEntry) bool {
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			switch name {
			case "renpy", "www", "Engine":
				return true
			}
			if strings.HasSuffix(name, "_Data") {
				return true
			}
			continue
		}
		lower := strings.ToLower(name)
		switch {
		case lower == "unityplayer.dll", lower == "data.win",
			lower == "nw.dll", lower == "nscript.dat",
			strings.HasSuffix(lower, ".pck"),
			strings.HasSuffix(lower, ".rpyc"),
			strings.HasSuffix(lower, ".rpa"),
			strings.HasPrefix(lower, "game.rgss"):
			return true
		}
	}
	return false
}

// hasHTMLGameSignature reports whether a root-level .html file in dir carries
// a recognizable game signature. Root-level only: a game's entry page lives at
// its root, so asset subdirectories and nested source trees are not read here.
func hasHTMLGameSignature(dir string, entries []os.DirEntry) bool {
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".html") {
			continue
		}
		if htmlFileHasGameMarker(filepath.Join(dir, e.Name())) {
			return true
		}
	}
	return false
}

// htmlFileHasGameMarker reads a bounded head and tail of path and reports
// whether any htmlGameMarkers appears.
func htmlFileHasGameMarker(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, htmlSniffHead)
	n, _ := io.ReadFull(f, buf)
	if containsHTMLMarker(bytes.ToLower(buf[:n])) {
		return true
	}

	fi, err := f.Stat()
	if err != nil || fi.Size() <= int64(htmlSniffHead) {
		return false
	}
	off := fi.Size() - int64(htmlSniffTail)
	if off < 0 {
		off = 0
	}
	tail := make([]byte, htmlSniffTail)
	if _, err := f.ReadAt(tail, off); err != nil {
		return false
	}
	return containsHTMLMarker(bytes.ToLower(tail))
}

// containsHTMLMarker reports whether data (lowercased) contains any marker.
func containsHTMLMarker(data []byte) bool {
	for _, m := range htmlGameMarkers {
		if bytes.Contains(data, m) {
			return true
		}
	}
	return false
}

// hasGameMarkers checks for game engine files and executables by reading
// the directory listing. Prefer isGameRoot when the listing has already
// been read to avoid redundant I/O, or hasGameMarkersFromEntries when the HTML
// content sniff is not required (e.g. locating nested tool directories).
func hasGameMarkers(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	return isGameRoot(dir, entries)
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
		"downloads": true, // downloader-owned dirs; extracted archives live here
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

// toolExcluded lists directory-name patterns identifying game tool/utility
// directories (decrypters, unpackers, RPG Maker toolkits) rather than games.
// Matches are substring-based on the lowercased directory name. The list is
// deliberately narrow — generic words like "setup", "patch", or "crack" are
// not included because they appear in legit game directory names.
var toolExcluded = []string{
	"rpg maker", // RPG Maker XP/VX/VX Ace toolkits (decrypter/uncpacker bundles)
	"decrypt",   // "decrypter", "decryptor", "RPG Maker Decrypter", ...
	"uncpacker", // the decrypter tool's own (misspelled) name
	"unpacker",
}

// isToolDirName reports whether the directory name matches a known
// game-tool pattern.
func isToolDirName(name string) bool {
	lower := strings.ToLower(name)
	for _, t := range toolExcluded {
		if strings.Contains(lower, t) {
			return true
		}
	}
	return false
}

// nestedInGameTree reports whether path lives inside a directory tree that
// also contains a game directory other than path itself: some ancestor of
// path (from its parent up to the scan root) has a child directory that
// looks like a game root. The check reads directory contents rather than
// the set of already-registered games, so it is independent of walk order.
func nestedInGameTree(path, root string) bool {
	base := filepath.Base(path)
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		entries, err := os.ReadDir(parent)
		if err == nil {
			for _, e := range entries {
				if !e.IsDir() || e.Name() == base {
					continue
				}
				if hasGameMarkers(filepath.Join(parent, e.Name())) {
					return true
				}
			}
		}
		if parent == root || filepath.Dir(parent) == parent {
			break
		}
	}
	return false
}

// ExtractVersionFromDir tries to extract a version string from known files
// inside the game directory when the directory name itself contains no version.
// Checks, in order: Game.ini Title= field, package.json "version" field,
// RPG Maker MV/MZ System.json gameTitle, then Ren'Py config.version (from
// options.rpy, compiled options.rpyc, or a small .rpa archive).
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

	if ver := rpgmVersionFromDir(dir); ver != "" {
		return ver
	}
	return renpyVersionFromDir(dir)
}
