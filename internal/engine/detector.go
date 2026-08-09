package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/mili/moxie/internal/config"
	"github.com/mili/moxie/internal/log"
)

// Engine represents a detected game engine. Values match the canonical
// engine list used by the project.
type Engine string

// Canonical engine names.
const (
	Others       Engine = "Others"
	ADRIFT       Engine = "ADRIFT"
	Flash        Engine = "Flash"
	Godot        Engine = "Godot"
	HTML         Engine = "HTML"
	Java         Engine = "Java"
	QSP          Engine = "QSP"
	RAGS         Engine = "RAGS"
	RPGM         Engine = "RPGM"
	RenPy        Engine = "RenPy"
	Tads         Engine = "Tads"
	Unity        Engine = "Unity"
	UnrealEngine Engine = "UnrealEngine"
	WebGL        Engine = "WebGL"
	WolfRPG      Engine = "WolfRPG"
)

// Result is the outcome of engine detection for a single directory.
type Result struct {
	Engine     Engine  `json:"engine"`
	Confidence float64 `json:"confidence"` // 0.0 - 1.0
	MatchedBy  string  `json:"matched_by"` // which rule matched
}

// CustomProfile represents a user-defined engine detection rule loaded from a
// JSON file in the engines config directory. At least one detection criterion
// (Subdirs, Filenames, or Extensions) must be specified, and Confidence must
// be between 0 and 100 (stored as 0-100 in JSON, converted to 0.0-1.0 internally).
type CustomProfile struct {
	Name       string   `json:"name"`
	Engine     Engine   `json:"engine"`
	Confidence float64  `json:"confidence"`
	Subdirs    []string `json:"subdirs,omitempty"`
	Filenames  []string `json:"filenames,omitempty"`
	Extensions []string `json:"extensions,omitempty"`
}

// Validate checks that a custom profile has all required fields and valid
// values. Returns a descriptive error if validation fails.
func (cp *CustomProfile) Validate() error {
	if cp.Name == "" {
		return fmt.Errorf("profile name is required")
	}
	if cp.Engine == "" {
		return fmt.Errorf("profile %q: engine is required", cp.Name)
	}
	if cp.Confidence < 0 || cp.Confidence > 100 {
		return fmt.Errorf("profile %q: confidence must be between 0 and 100, got %f", cp.Name, cp.Confidence)
	}
	if len(cp.Subdirs) == 0 && len(cp.Filenames) == 0 && len(cp.Extensions) == 0 {
		return fmt.Errorf("profile %q: at least one detection criterion (subdirs, filenames, or extensions) is required", cp.Name)
	}
	return nil
}

// toProfile converts a CustomProfile to an internal profile for matching.
func (cp *CustomProfile) toProfile() profile {
	return profile{
		engine:     cp.Engine,
		confidence: cp.Confidence / 100.0,
		subdirs:    cp.Subdirs,
		files:      cp.Filenames,
		extensions: cp.Extensions,
		name:       cp.Name,
	}
}

// loadCustomProfiles reads all .json files from the engine profiles directory
// and returns validated custom profiles. Invalid files are logged and skipped.
func loadCustomProfiles() ([]profile, error) {
	dir := config.EngineProfilesDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read engine profiles dir: %w", err)
	}

	var custom []profile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			log.Warn("skipping unreadable engine profile", "file", entry.Name(), "error", err)
			continue
		}

		var cp CustomProfile
		if err := json.Unmarshal(data, &cp); err != nil {
			log.Warn("skipping invalid engine profile JSON", "file", entry.Name(), "error", err)
			continue
		}

		if err := cp.Validate(); err != nil {
			log.Warn("skipping invalid engine profile", "file", entry.Name(), "error", err)
			continue
		}

		custom = append(custom, cp.toProfile())
	}

	return custom, nil
}

// mergeProfiles merges custom profiles into the built-in profile list.
// Profiles with a name matching a built-in profile replace it (overriding).
// Profiles with new names are appended after all built-in profiles.
func mergeProfiles(builtin, custom []profile) []profile {
	if len(custom) == 0 {
		return builtin
	}

	// Build a set of custom profile names for quick lookup.
	customByName := make(map[string]profile, len(custom))
	for _, cp := range custom {
		customByName[cp.name] = cp
	}

	merged := make([]profile, 0, len(builtin)+len(custom))
	seen := make(map[string]bool)

	// Walk built-in profiles, replacing any that have a custom override.
	for _, bp := range builtin {
		if cp, ok := customByName[bp.name]; ok {
			merged = append(merged, cp)
			seen[bp.name] = true
		} else {
			merged = append(merged, bp)
		}
	}

	// Append any custom profiles that didn't match a built-in name.
	for _, cp := range custom {
		if !seen[cp.name] {
			merged = append(merged, cp)
		}
	}

	return merged
}

// profile defines a single detection rule.
type profile struct {
	engine     Engine
	confidence float64
	subdirs    []string // at least one of these subdirs must exist
	files      []string // file names (no path) that must be present
	filesAll   bool     // when true, EVERY file in `files` must exist (default: any)
	extensions []string // file extensions to look for
	name       string   // human-readable name for the rule
}

// builtinProfiles is the built-in detection profile list, ordered by priority
// — first match wins. Custom profiles (from EngineProfilesDir) are merged into
// this list by getProfiles() before detection.
var builtinProfiles = []profile{
	// --- Highest confidence signals ---

	{
		engine: RenPy, confidence: 0.98,
		subdirs: []string{"renpy"},
		name:    "renpy directory found",
	},
	{
		engine: RenPy, confidence: 0.85,
		extensions: []string{".rpyc", ".rpa"},
		subdirs:    []string{"game"},
		name:       "Ren'Py game directory with scripts",
	},
	{
		engine: Unity, confidence: 0.98,
		files: []string{"UnityPlayer.dll"},
		name:  "UnityPlayer.dll found",
	},
	{
		engine: Unity, confidence: 0.95,
		files: []string{"UnityCrashHandler64.exe", "UnityCrashHandler32.exe"},
		name:  "Unity crash handler found",
	},
	{
		engine: Unity, confidence: 0.90,
		files: []string{"globalgamemanagers", "data.unity3d"},
		name:  "Unity data files found",
	},
	// Unity _Data folder handled separately in detectUnityDataFolder

	// --- RPG Maker variants (all map to RPGM) ---

	// --- RPG Maker ---

	// NW.js runtime markers — definitive for RPG Maker MV/MZ.
	// icudtl.dat is the single best signal: present in 100% of MV/MZ games,
	// absent from pure HTML games. Combined with Game.exe (renamed nw.exe)
	// or nw_*.pak files for high-confidence detection.
	{
		engine: RPGM, confidence: 0.96,
		files: []string{"icudtl.dat", "Game.exe"},
		name:  "RPG Maker MV/MZ (NW.js: icudtl.dat + Game.exe)",
	},
	{
		engine: RPGM, confidence: 0.95,
		subdirs: []string{"www"},
		files:   []string{"package.json"},
		name:    "RPG Maker (www + package.json)",
	},
	{
		engine: RPGM, confidence: 0.93,
		files: []string{"Game.rgss3a"},
		name:  "RPG Maker VX Ace (Game.rgss3a)",
	},
	{
		engine: RPGM, confidence: 0.90,
		files: []string{"Game.rgss2a"},
		name:  "RPG Maker VX (Game.rgss2a)",
	},
	{
		engine: RPGM, confidence: 0.88,
		files: []string{"Game.rgssad"},
		name:  "RPG Maker XP (Game.rgssad)",
	},
	{
		engine: RPGM, confidence: 0.75,
		files:   []string{"Game.exe", "Game.ini"},
		subdirs: []string{"Data"},
		name:    "RPG Maker (Game.exe + Game.ini + Data/)",
	},
	{
		engine: RPGM, confidence: 0.65,
		files: []string{"Game.ini"},
		name:  "Possible RPG Maker (Game.ini found)",
	},

	// --- Other engines from the official list ---

	{
		engine: UnrealEngine, confidence: 0.92,
		subdirs: []string{"Engine"},
		name:    "Unreal Engine directory",
	},
	{
		engine: WebGL, confidence: 0.75,
		files:   []string{"index.html"},
		subdirs: []string{"Build"},
		name:    "WebGL (Unity WebGL build)",
	},
	{
		engine: HTML, confidence: 0.70,
		files: []string{"index.html"},
		name:  "HTML game (index.html found)",
	},
	{
		engine: HTML, confidence: 0.60,
		extensions: []string{".html"},
		name:       "HTML file found",
	},
	{
		engine: Java, confidence: 0.90,
		extensions: []string{".jar"},
		name:       "Java .jar file found",
	},
	{
		engine: Flash, confidence: 0.90,
		extensions: []string{".swf"},
		name:       "Flash .swf file found",
	},
	{
		engine: Flash, confidence: 0.85,
		files: []string{"ruffle.exe", "ruffle"},
		name:  "Flash via ruffle emulator",
	},

	// --- New engines ---

	{
		engine: WolfRPG, confidence: 0.90,
		subdirs: []string{"Data"},
		files:   []string{"WolfRPG.exe", "WolfRPGEditor.exe", "Game.ini"},
		name:    "Wolf RPG Editor",
	},
	{
		engine: WolfRPG, confidence: 0.80,
		extensions: []string{".wolf"},
		name:       "Wolf RPG .wolf file",
	},
	{
		engine: QSP, confidence: 0.90,
		extensions: []string{".qsp", ".qsps"},
		name:       "QSP game file",
	},
	{
		engine: QSP, confidence: 0.85,
		files: []string{"qspgui.exe", "quest.exe"},
		name:  "QSP player executable",
	},
	{
		engine: ADRIFT, confidence: 0.90,
		extensions: []string{".taf"},
		name:       "ADRIFT game file",
	},
	{
		engine: ADRIFT, confidence: 0.80,
		files: []string{"adrift.exe", "ADRIFT.exe"},
		name:  "ADRIFT runner executable",
	},
	{
		engine: RAGS, confidence: 0.85,
		files: []string{"RAGS.exe", "RAGS Player.exe", "RAGS2.exe"},
		name:  "RAGS player executable",
	},
	{
		engine: Tads, confidence: 0.90,
		extensions: []string{".gam", ".t3"},
		name:       "TADS game file",
	},

	// --- Community engines mapped to Others ---

	{
		engine: Godot, confidence: 0.85,
		extensions: []string{".pck"},
		name:       "Godot .pck file found",
	},
	{
		// package.json alone is far too weak — Twine source repos and RPGM
		// bundles ship one too. resources.pak is the nw.js/Electron resource
		// archive; require it too before claiming the nw.js family.
		engine: Others, confidence: 0.80,
		files:    []string{"resources.pak", "package.json"},
		filesAll: true,
		name:     "Electron/nw.js resources",
	},
	{
		engine: Others, confidence: 0.92,
		subdirs: []string{"chars", "data", "stages", "font", "sound"},
		name:    "M.U.G.E.N. directories",
	},
	{
		engine: Others, confidence: 0.80,
		files: []string{"mugen.cfg", "mugen.exe"},
		name:  "M.U.G.E.N. config",
	},
}

var (
	// profilesMu guards cachedProfiles and cachedProfilesSig.
	profilesMu        sync.RWMutex
	cachedProfiles    []profile
	cachedProfilesSig string
)

// profilesDirSig returns a signature for the engine profiles directory:
// its path plus mtime. getProfiles re-stats the directory on each call
// (cheap — one stat syscall) and reloads when the signature changes, so
// custom profiles edited while the process is running take effect without
// a restart, and a config-dir switch (e.g. under tests) invalidates the
// cache too. A missing directory yields a "missing" signature; creating
// the directory later changes the signature and triggers a reload.
// Only the directory mtime is observed: adding, removing, or replacing
// (rename-based) profile files is detected, while truncating a file in
// place within the same filesystem mtime tick is not. ResetProfilesForTest
// forces an immediate reload regardless.
func profilesDirSig() string {
	dir := config.EngineProfilesDir()
	fi, err := os.Stat(dir)
	if err != nil {
		return dir + ":missing"
	}
	return fmt.Sprintf("%s:%d", dir, fi.ModTime().UnixNano())
}

// getProfiles returns the full profile list with custom profiles merged in.
// The result is cached and keyed on the engine-profiles directory mtime;
// the cache is reloaded when the directory changes, so a long-running
// process (desktop app) picks up profile edits without a restart.
func getProfiles() []profile {
	profilesMu.RLock()
	sig := profilesDirSig()
	if sig == cachedProfilesSig {
		profiles := cachedProfiles
		profilesMu.RUnlock()
		return profiles
	}
	profilesMu.RUnlock()

	// Reload under the write lock. Re-check the signature: another
	// goroutine may have reloaded (and possibly re-signed) in between.
	profilesMu.Lock()
	defer profilesMu.Unlock()
	if sig == cachedProfilesSig {
		return cachedProfiles
	}
	custom, err := loadCustomProfiles()
	if err != nil {
		log.Warn("failed to load custom engine profiles", "error", err)
		cachedProfiles = builtinProfiles
		cachedProfilesSig = sig
		return cachedProfiles
	}
	cachedProfiles = mergeProfiles(builtinProfiles, custom)
	cachedProfilesSig = sig
	return cachedProfiles
}

// ResetProfilesForTest clears the cached profile list so the next call to
// getProfiles reloads custom profiles from disk. Test-only: use it to
// exercise profile changes that a directory mtime would not expose (e.g.
// in-place content edits within the same mtime tick).
func ResetProfilesForTest() {
	profilesMu.Lock()
	defer profilesMu.Unlock()
	cachedProfiles = nil
	cachedProfilesSig = ""
}

// Detect scans the given directory and returns the most likely engine match.
// It reads the directory listing once and checks it against all profiles.
func Detect(dir string) Result {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Result{Engine: Others, Confidence: 0, MatchedBy: "error reading directory"}
	}

	entrySet := make(map[string]bool, len(entries))
	dirSet := make(map[string]bool)
	for _, e := range entries {
		entrySet[e.Name()] = true
		if e.IsDir() {
			dirSet[e.Name()] = true
		}
	}

	extSet := make(map[string]bool)
	for _, e := range entries {
		if !e.IsDir() {
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if ext != "" {
				extSet[ext] = true
			}
		}
	}

	// Check special Unity _Data folder pattern first (requires file name matching).
	if result := detectUnityDataFolder(dir, entries); result.Engine != Others {
		return result
	}

	// Check for a bundled Java runtime (jre*/ dir with .jar files) before the
	// profile loop: JRE-bundled games have no root-level .jar, and the JRE
	// signal is strong enough to outrank weak root-level matches like a stray
	// .html file.
	if result := detectBundledJRE(dir, entries); result.Engine != Others {
		return result
	}

	// Check engine profiles in priority order.
	allProfiles := getProfiles()
	for _, p := range allProfiles {
		if !matchesProfile(p, entrySet, dirSet, extSet, dir) {
			continue
		}

		// Special handling for RPGM Game.ini weak signal.
		if p.confidence == 0.65 && p.engine == RPGM {
			if engine := checkRPGMakerINI(dir); engine != Others {
				return Result{Engine: engine, Confidence: 0.75, MatchedBy: "RPGM confirmed via Game.ini content"}
			}
			continue // not actually a match, check next profile
		}

		// Special handling for RPGM www + package.json (check MV/MZ content).
		if p.confidence == 0.95 && p.engine == RPGM && len(p.subdirs) == 1 && p.subdirs[0] == "www" {
			subtype := checkRPGMakerPackage(dir)
			if subtype == "MV" {
				return Result{Engine: RPGM, Confidence: 0.95, MatchedBy: "RPG Maker MV (" + p.name + ")"}
			}
			if subtype == "MZ" {
				return Result{Engine: RPGM, Confidence: 0.95, MatchedBy: "RPG Maker MZ (" + p.name + ")"}
			}
			return Result{Engine: RPGM, Confidence: 0.95, MatchedBy: p.name}
		}

		// Mugen directories: require at least 3 of 5 dirs.
		if p.engine == Others && p.confidence == 0.92 {
			mugenDirs := []string{"chars", "data", "stages", "font", "sound"}
			count := 0
			for _, md := range mugenDirs {
				if dirSet[md] {
					count++
				}
			}
			if count < 3 {
				continue
			}
			return Result{Engine: Others, Confidence: 0.92, MatchedBy: fmt.Sprintf("M.U.G.E.N. (%d/5 dirs)", count)}
		}

		return Result{Engine: p.engine, Confidence: p.confidence, MatchedBy: p.name}
	}

	// Fallback: some HTML games (especially source-repo layouts) keep
	// index.html at shallow depth instead of the root. Only fire when no
	// profile matched at all — a wrong engine flip is worse than Others.
	if findShallowHTML(dir) {
		return Result{Engine: HTML, Confidence: 0.55, MatchedBy: "HTML file found at shallow depth"}
	}

	return Result{Engine: Others, Confidence: 0, MatchedBy: "no matching profile"}
}

// matchesProfile checks if a directory's contents match a detection profile.
func matchesProfile(p profile, files, dirs, exts map[string]bool, dir string) bool {
	if len(p.files) > 0 {
		if p.filesAll {
			for _, f := range p.files {
				if !files[f] {
					return false
				}
			}
		} else if !anyMatches(p.files, files) {
			return false
		}
	}
	if len(p.subdirs) > 0 && !anyMatches(p.subdirs, dirs) {
		return false
	}
	if len(p.extensions) > 0 {
		if !anyMatches(p.extensions, exts) {
			found := false
			for _, sd := range p.subdirs {
				if hasMatchingExtension(filepath.Join(dir, sd), p.extensions) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}

// detectUnityDataFolder checks for the Unity `<exe>_Data/` folder pattern.
func detectUnityDataFolder(dir string, entries []os.DirEntry) Result {
	var exeNames []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() && strings.HasSuffix(name, "_Data") {
			exeName := strings.TrimSuffix(name, "_Data")
			for _, e2 := range entries {
				if !e2.IsDir() {
					fname := e2.Name()
					fnameNoExt := strings.TrimSuffix(fname, filepath.Ext(fname))
					if strings.EqualFold(fnameNoExt, exeName) && isExe(fname) {
						exeNames = append(exeNames, fname)
					}
				}
			}
		}
	}
	if len(exeNames) > 0 {
		return Result{
			Engine:     Unity,
			Confidence: 0.93,
			MatchedBy:  "Unity _Data folder with matching exe: " + strings.Join(exeNames, ", "),
		}
	}
	return Result{Engine: Others}
}

// detectBundledJRE checks for a bundled Java runtime: a directory whose name
// starts with "jre" (e.g. jre1.8.0_172) containing .jar files under it
// (typically jre*/lib/*.jar). JRE-bundled Java games (Lilith's Throne, etc.)
// are launched via a native wrapper (LT.exe) and keep every .jar inside the
// runtime, so the root-level .jar profile never matches.
func detectBundledJRE(dir string, entries []os.DirEntry) Result {
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(e.Name()), "jre") {
			continue
		}
		if hasJarShallow(filepath.Join(dir, e.Name()), 0) {
			return Result{
				Engine:     Java,
				Confidence: 0.88,
				MatchedBy:  "bundled JRE directory with .jar files",
			}
		}
	}
	return Result{Engine: Others}
}

// maxJarDepth bounds the search for .jar files inside a JRE directory.
// jre/lib/rt.jar is depth 2; jre/lib/ext/x.jar is depth 3.
const maxJarDepth = 3

// hasJarShallow reports whether dir contains a .jar file within maxJarDepth
// levels (inclusive).
func hasJarShallow(dir string, depth int) bool {
	if depth > maxJarDepth {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			if hasJarShallow(filepath.Join(dir, e.Name()), depth+1) {
				return true
			}
			continue
		}
		if strings.EqualFold(filepath.Ext(e.Name()), ".jar") {
			return true
		}
	}
	return false
}

// maxIndexDepth bounds the fallback search for index.html below the game
// root. Source-repo layouts typically keep it at src/index.html (depth 1)
// or one folder deeper.
const maxIndexDepth = 2

// nonContentDirs lists directory names that never hold a game's entry HTML
// page — asset folders, libraries, VCS metadata. They are skipped during
// the shallow index.html search to avoid false positives.
var nonContentDirs = map[string]bool{
	"js": true, "css": true, "img": true, "images": true,
	"fonts": true, "lib": true, "node_modules": true,
	"__MACOSX": true, ".git": true,
}

// findShallowHTML reports whether an HTML entry file exists at most
// maxIndexDepth levels below dir, ignoring non-content directories. Any
// .html file qualifies — Twine-compiled games ship a single large file
// (precompiled.html, game.html, ...) instead of index.html.
func findShallowHTML(dir string) bool {
	return findHTMLFile(dir, 0)
}

func findHTMLFile(dir string, depth int) bool {
	if depth > maxIndexDepth {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			if nonContentDirs[name] {
				continue
			}
			if findHTMLFile(filepath.Join(dir, name), depth+1) {
				return true
			}
			continue
		}
		if strings.EqualFold(filepath.Ext(name), ".html") {
			return true
		}
	}
	return false
}

// checkRPGMakerPackage reads package.json to identify RPG Maker variant.
// Returns "MV", "MZ", or an empty string if unknown.
func checkRPGMakerPackage(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "www", "package.json"))
	if err != nil {
		return ""
	}
	content := string(data)
	if strings.Contains(content, "KADOKAWA/RPGMV") || strings.Contains(content, "RPGMV") {
		return "MV"
	}
	if strings.Contains(content, "rmmz-game") || strings.Contains(content, "RPGMZ") {
		return "MZ"
	}
	return ""
}

// checkRPGMakerINI reads Game.ini to detect RPG Maker games.
func checkRPGMakerINI(dir string) Engine {
	data, err := os.ReadFile(filepath.Join(dir, "Game.ini"))
	if err != nil {
		return Others
	}
	content := string(data)
	if strings.Contains(content, "RPGVXAce") || strings.Contains(content, "RPGXP") ||
		strings.Contains(content, "RPGVX") || strings.Contains(content, "RGSS") {
		return RPGM
	}
	return Others
}

// anyMatches returns true if any candidate exists in the set.
func anyMatches(candidates []string, set map[string]bool) bool {
	for _, c := range candidates {
		if set[c] {
			return true
		}
	}
	return false
}

// hasMatchingExtension returns true if any file in dir has one of the given extensions.
func hasMatchingExtension(dir string, exts []string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		for _, target := range exts {
			if ext == target {
				return true
			}
		}
	}
	return false
}

// isExe returns true if the filename has a .exe extension.
func isExe(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".exe")
}

// String returns the engine name as a string.
func (e Engine) String() string { return string(e) }

// AllEngines returns all known engine names sorted.
func AllEngines() []Engine {
	seen := make(map[Engine]bool)
	for _, p := range getProfiles() {
		if !seen[p.engine] {
			seen[p.engine] = true
			// Don't include Others from profiles.
		}
	}
	// Add canonical engines not covered by profiles yet.
	canonical := []Engine{ADRIFT, Flash, HTML, Java, Others, QSP, RAGS, RPGM, RenPy, Tads, Unity, UnrealEngine, WebGL, WolfRPG}
	for _, e := range canonical {
		seen[e] = true
	}
	engines := make([]Engine, 0, len(seen))
	for e := range seen {
		engines = append(engines, e)
	}
	sort.Slice(engines, func(i, j int) bool {
		if engines[i] == Others {
			return false
		}
		if engines[j] == Others {
			return true
		}
		return engines[i] < engines[j]
	})
	return engines
}
