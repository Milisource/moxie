package engine

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mili/moxie/internal/db"
)

// EngineTagVariants maps canonical engine names to substrings that commonly
// appear in F95Zone thread tags.  A match is found when any variant appears
// within any tag (case-insensitive).
var EngineTagVariants = map[string][]string{
	"RenPy":        {"ren'py", "renpy"},
	"Unity":        {"unity"},
	"RPGM":         {"rpg maker", "rpgm", "rmmv", "rmmz"},
	"HTML":         {"html", "html5"},
	"Flash":        {"flash"},
	"Java":         {"java"},
	"Godot":        {"godot"},
	"UnrealEngine": {"unreal", "unreal engine"},
	"WebGL":        {"webgl"},
	"WolfRPG":      {"wolf rpg", "wolfrpg"},
	"ADRIFT":       {"adrift"},
	"QSP":          {"qsp"},
	"RAGS":         {"rags"},
	"Tads":         {"tads"},
}

// EngineCompat maps engines that are implementation-compatible.
// For example, RPG Maker MV/MZ uses NW.js (HTML/JS) as its runtime,
// so scanner-detected "HTML" is compatible with F95Zone-prefixed "RPGM".
var EngineCompat = map[string]map[string]bool{
	"RPGM":    {"HTML": true, "RPGM": true},
	"HTML":    {"RPGM": true, "HTML": true, "WebGL": true},
	"WebGL":   {"HTML": true, "WebGL": true},
	"WolfRPG": {"HTML": true, "WolfRPG": true},
}

// findInText searches text for engine variant keywords and returns
// the first matching engine name, or "" if none found.
func findInText(text string) string {
	lower := strings.ToLower(text)
	for engine, variants := range EngineTagVariants {
		for _, variant := range variants {
			if strings.Contains(lower, variant) {
				return engine
			}
		}
	}
	return ""
}

// FindInText searches text for engine variant keywords and returns
// the first matching engine name, or "" if none found.
// This is the exported wrapper for use by external packages.
func FindInText(text string) string {
	return findInText(text)
}

// EngineMatchesTags checks whether the scanner-detected engine is consistent
// with the F95Zone thread tags.  Returns true when:
//   - There are no tags to compare (inconclusive)
//   - The detected engine is "Others" or empty (inconclusive)
//   - No tag variant mapping exists for the engine (inconclusive)
//   - At least one tag contains a variant of the detected engine (match)
//
// Returns false only when there is a clear engine mismatch (specific engine
// was detected, tags exist, and no variant matches any tag).
func EngineMatchesTags(detected Result, tags []string) bool {
	if len(tags) == 0 {
		return true // no metadata to compare against
	}
	if detected.Engine == "Others" || detected.Engine == "" {
		return true // detection inconclusive — don't flag
	}

	variants := EngineTagVariants[string(detected.Engine)]
	if len(variants) == 0 {
		return true // no mapping known for this engine — don't flag
	}

	for _, tag := range tags {
		tagLower := strings.ToLower(tag)
		for _, variant := range variants {
			if strings.Contains(tagLower, variant) {
				return true // engine matches
			}
		}
	}

	return false
}

// EngineMatchesThread checks whether the scanner-detected engine is consistent
// with F95Zone thread metadata.  Unlike EngineMatchesTags (which only checks
// content tags like "2dcg", "bdsm"), this function ALSO checks the thread
// title for engine prefix indicators — the primary signal on F95Zone.
//
// On F95Zone, search result titles include the engine prefix (e.g.
// "Unity Completed A Queen Confined"), while content tags are
// genre/theme tags.  This function checks both sources.
//
// Returns true when:
//   - The detected engine is "Others" or empty (inconclusive)
//   - No variant mapping exists for the engine (inconclusive)
//   - The thread title contains a variant of the detected engine
//   - At least one tag contains a variant of the detected engine
//
// Returns false only when a specific engine was detected, the title and tags
// contain engine metadata, and neither matches the detected engine.
func EngineMatchesThread(detected Result, tags []string, title string) bool {
	if detected.Engine == "Others" || detected.Engine == "" {
		return true // detection inconclusive — don't flag
	}

	variants := EngineTagVariants[string(detected.Engine)]
	if len(variants) == 0 {
		return true // no mapping known for this engine — don't flag
	}

	// 1. Primary signal: thread title prefix.
	//    F95Zone search result titles include engine prefix like
	//    "Unity Completed A Queen Confined" or "RPGM Game Name".
	titleLower := strings.ToLower(title)
	for _, variant := range variants {
		if strings.Contains(titleLower, variant) {
			return true // engine found in title
		}
	}

	// 2. Secondary signal: content tags (genre/theme tags might include
	//    engine info on some threads).
	if len(tags) > 0 {
		for _, tag := range tags {
			tagLower := strings.ToLower(tag)
			for _, variant := range variants {
				if strings.Contains(tagLower, variant) {
					return true // engine found in tags
				}
			}
		}
	}

	// No engine info found in either title or tags — can't verify.
	// Don't flag as mismatch (the metadata just didn't include engine info).
	if title == "" && len(tags) == 0 {
		return true
	}

	// We found tags/title but neither contained the expected engine.
	// Only flag as false if there's engine metadata present that contradicts.
	// Extract any engine from title.
	f95Engine := findInText(titleLower)
	// Also check tags for any engine indicator.
	if f95Engine == "" {
		for _, tag := range tags {
			if f95Engine = findInText(tag); f95Engine != "" {
				break
			}
		}
	}

	if f95Engine == "" {
		// No engine info in either title or tags — inconclusive.
		return true
	}

	// Title/tags indicate engine X, but scanner found engine Y.
	// True mismatch.  But check compatibility (RPGM↔HTML, etc.).
	if f95Engine == string(detected.Engine) {
		return true
	}
	if compat, ok := EngineCompat[f95Engine]; ok && compat[string(detected.Engine)] {
		return true
	}

	return false
}

// FindF95Engine looks through F95Zone thread tags, the game title,
// and the parent directory for an engine indicator.  Returns the
// engine name implied by the data, or "" if no engine is found.
func FindF95Engine(g db.Game) string {
	// 1. Check tags (most reliable — explicitly tagged by thread author).
	for _, tag := range g.Tags {
		if engine := findInText(tag); engine != "" {
			return engine
		}
	}
	// 2. Fall back to title prefix (RPGM, Unity, RenPy, etc. in thread title).
	titleLower := strings.ToLower(g.Title)
	for engine, variants := range EngineTagVariants {
		for _, variant := range variants {
			if strings.HasPrefix(titleLower, variant+" ") || strings.HasPrefix(titleLower, variant+"\t") {
				return engine
			}
		}
	}
	// 3. Check parent directory name — users often organize by engine.
	parent := strings.ToLower(filepath.Base(filepath.Dir(g.Path)))
	if engine := findInText(parent); engine != "" {
		return engine
	}
	return ""
}

// ExtractEngineFromTitle pulls the engine prefix from a raw F95Zone
// thread title (e.g. "RPGM Completed Game Name" → "RPGM").
func ExtractEngineFromTitle(title string) string {
	words := strings.Fields(title)
	if len(words) == 0 {
		return ""
	}
	// Check if the first word is a known engine prefix.
	first := strings.ToLower(strings.TrimRight(words[0], "•"))
	for engine, variants := range EngineTagVariants {
		for _, variant := range variants {
			if first == variant {
				return engine
			}
		}
	}
	return ""
}

// FormatTagsBrief returns a comma-separated tag string, limited to max tags
// to keep output readable.
func FormatTagsBrief(tags []string, max int) string {
	if len(tags) == 0 {
		return ""
	}
	if len(tags) <= max {
		return strings.Join(tags, ", ")
	}
	return strings.Join(tags[:max], ", ") + fmt.Sprintf(" (+%d more)", len(tags)-max)
}
