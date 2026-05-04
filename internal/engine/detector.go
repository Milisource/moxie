package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Engine represents a detected game engine. Values match the canonical
// engine list used by the project.
type Engine string

// Canonical engine names.
const (
	Others        Engine = "Others"
	ADRIFT        Engine = "ADRIFT"
	Flash         Engine = "Flash"
	HTML          Engine = "HTML"
	Java          Engine = "Java"
	QSP           Engine = "QSP"
	RAGS          Engine = "RAGS"
	RPGM          Engine = "RPGM"
	RenPy         Engine = "RenPy"
	Tads          Engine = "Tads"
	Unity         Engine = "Unity"
	UnrealEngine  Engine = "UnrealEngine"
	WebGL         Engine = "WebGL"
	WolfRPG       Engine = "WolfRPG"
)

// Result is the outcome of engine detection for a single directory.
type Result struct {
	Engine     Engine `json:"engine"`
	Confidence float64 `json:"confidence"` // 0.0 - 1.0
	MatchedBy  string `json:"matched_by"`  // which rule matched
}

// profile defines a single detection rule.
type profile struct {
	engine     Engine
	confidence float64
	subdirs    []string // at least one of these subdirs must exist
	files      []string // file names (no path) that must be present
	extensions []string // file extensions to look for
	name       string   // human-readable name for the rule
}

// profiles is ordered by priority — first match wins.
var profiles = []profile{
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
		files:  []string{"Game.rgss2a"},
		name:   "RPG Maker VX (Game.rgss2a)",
	},
	{
		engine: RPGM, confidence: 0.88,
		files:  []string{"Game.rgssad"},
		name:   "RPG Maker XP (Game.rgssad)",
	},
	{
		engine: RPGM, confidence: 0.75,
		files:   []string{"Game.exe", "Game.ini"},
		subdirs: []string{"Data"},
		name:    "RPG Maker (Game.exe + Game.ini + Data/)",
	},
	{
		engine: RPGM, confidence: 0.65,
		files:   []string{"Game.ini"},
		name:    "Possible RPG Maker (Game.ini found)",
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
		engine: Others, confidence: 0.85,
		extensions: []string{".pck"},
		name:       "Godot .pck file found",
	},
	{
		engine: Others, confidence: 0.80,
		files: []string{"resources.pak", "package.json"},
		name:  "Electron/nw.js resources",
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

	// Check engine profiles in priority order.
	for _, p := range profiles {
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

	return Result{Engine: Others, Confidence: 0, MatchedBy: "no matching profile"}
}

// matchesProfile checks if a directory's contents match a detection profile.
func matchesProfile(p profile, files, dirs, exts map[string]bool, dir string) bool {
	if len(p.files) > 0 && !anyMatches(p.files, files) {
		return false
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
	for _, p := range profiles {
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
