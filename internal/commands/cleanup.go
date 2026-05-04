package commands

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/engine"
	"github.com/mili/moxie/internal/scanner"
	"github.com/mili/moxie/internal/util"
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

// findEngineInText searches text for engine variant keywords and returns
// the first matching engine name, or "" if none found.
func findEngineInText(text string) string {
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

// Cleanup detects and fixes wrong F95Zone thread associations.
func Cleanup(args []string) {
	fs := flag.NewFlagSet("cleanup", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "Preview issues without making changes")
	assumeYes := fs.Bool("assume-yes", false, "Auto-disassociate flagged games without prompting")
	yes := fs.Bool("y", false, "Auto-disassociate flagged games (shorthand for --assume-yes)")
	fs.Parse(args)

	database := OpenDB()
	defer database.Close()

	games, err := database.ListGames("", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading games: %v\n", err)
		os.Exit(1)
	}

	// Only check games that have an F95Zone URL.
	var toCheck []db.Game
	for _, g := range games {
		if g.F95URL != "" {
			toCheck = append(toCheck, g)
		}
	}

	if len(toCheck) == 0 {
		fmt.Println("No games have F95Zone URLs. Nothing to clean up.")
		return
	}

	autoDisassociate := *assumeYes || *yes

	flagged := 0
	disassociated := 0
	for _, g := range toCheck {
		var issues []string

		// Signal 1: Engine mismatch.
		if s := CheckEngineMismatch(g); s != "" {
			issues = append(issues, s)
		}

		// Signal 2: Executable name mismatch.
		if s := CheckExeMismatch(g); s != "" {
			issues = append(issues, s)
		}

		if len(issues) == 0 {
			continue
		}

		flagged++
		for _, issue := range issues {
			fmt.Printf("  #%d %q — %s\n", g.ID, g.Title, issue)
		}

		if *dryRun {
			continue
		}

		// Only prompt for disassociation on clear mismatches,
		// not "unverified" issues where F95Zone simply lacks engine tags.
		hasHardMismatch := false
		for _, s := range issues {
			if strings.Contains(s, "mismatch") && !strings.Contains(s, "unverified") {
				hasHardMismatch = true
				break
			}
		}
		if !hasHardMismatch {
			continue // just flag, don't prompt
		}

		if autoDisassociate {
			DisassociateGame(database, &g)
			disassociated++
			continue
		}

		// Interactive mode: prompt for each flagged game.
		fmt.Fprintf(os.Stderr, "  Disassociate? [y/N]: ")
		var answer string
		fmt.Scanln(&answer)
		if strings.ToLower(answer) == "y" {
			DisassociateGame(database, &g)
			disassociated++
		}
	}

	if *dryRun && flagged > 0 {
		fmt.Fprintf(os.Stderr, "\n%d game(s) flagged. Use --assume-yes or -y to auto-disassociate, or run without --dry-run for interactive mode.\n", flagged)
	}
	if flagged == 0 {
		fmt.Println("No issues found. All F95Zone associations look correct.")
	}
	if disassociated > 0 {
		fmt.Fprintf(os.Stderr, "\nDisassociated %d game(s).\n", disassociated)
	}
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
func EngineMatchesTags(detected engine.Result, tags []string) bool {
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
func EngineMatchesThread(detected engine.Result, tags []string, title string) bool {
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
	f95Engine := findEngineInText(titleLower)
	// Also check tags for any engine indicator.
	if f95Engine == "" {
		for _, tag := range tags {
			if f95Engine = findEngineInText(tag); f95Engine != "" {
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

// CheckEngineMismatch returns a description of the mismatch, or "" if no
// issue is found.  It compares the scanner-detected engine (via
// engine.Detect) against the F95Zone thread tags stored on the game.
func CheckEngineMismatch(g db.Game) string {
	if len(g.Tags) == 0 {
		return "" // no F95Zone metadata to compare against
	}

	detected := engine.Detect(g.Path)
	if detected.Engine == "Others" || detected.Engine == "" {
		return "" // can't determine local engine
	}

	f95Engine := FindF95Engine(g)
	if f95Engine == "" {
		return fmt.Sprintf("engine unverified — no engine info in F95Zone tags/title (scanner found: %s)", detected.Engine)
	}

	if f95Engine == string(detected.Engine) {
		return "" // both agree on the engine
	}

	// Check compatibility: RPGM games use HTML runtime (NW.js),
	// WolfRPG uses HTML, WebGL is compatible with HTML, etc.
	if compat, ok := EngineCompat[f95Engine]; ok && compat[string(detected.Engine)] {
		return "" // compatible engines — not a mismatch
	}

	return fmt.Sprintf("engine mismatch (scanner: %s, F95Zone: %s)",
		detected.Engine, f95Engine)
}

// FindF95Engine looks through F95Zone thread tags, the game title,
// and the parent directory for an engine indicator.  Returns the
// engine name implied by the data, or "" if no engine is found.
func FindF95Engine(g db.Game) string {
	// 1. Check tags (most reliable — explicitly tagged by thread author).
	for _, tag := range g.Tags {
		if engine := findEngineInText(tag); engine != "" {
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
	if engine := findEngineInText(parent); engine != "" {
		return engine
	}
	return ""
}

// CheckExeMismatch returns a description when no executable in the game
// directory shares a word (case-insensitive) with the game title, or ""
// if at least one executable matches.
func CheckExeMismatch(g db.Game) string {
	entries, err := os.ReadDir(g.Path)
	if err != nil {
		return "" // directory inaccessible — skip
	}

	// Build a set of lowercase meaningful words from the title.
	titleWords := make(map[string]bool)
	for _, w := range strings.Fields(strings.ToLower(g.Title)) {
		w = strings.Trim(w, ".,!?-:;\"'()[]{}")
		if w != "" {
			titleWords[w] = true
		}
	}

	if len(titleWords) == 0 {
		return ""
	}

	// Collect executable filenames.
	var exeNames []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		lower := strings.ToLower(name)
		if strings.HasSuffix(lower, ".exe") ||
			strings.HasSuffix(lower, ".sh") ||
			strings.HasSuffix(lower, ".x86_64") ||
			strings.HasSuffix(lower, ".appimage") {
			exeNames = append(exeNames, name)
		}
	}

	if len(exeNames) == 0 {
		return "" // no executables to check
	}

	// Check if any exe name contains any title word.
	// Skip generic short executable names (Game.exe, LT.exe, etc.)
	// which are ubiquitous across game engines and produce false positives.
	genericExeNames := map[string]bool{
		"game": true, "launcher": true, "start": true, "run": true,
		"setup": true, "install": true, "config": true, "patch": true,
		"update": true, "unins": true,
	}
	var meaningfulExes []string
	for _, exe := range exeNames {
		exeLower := strings.ToLower(exe)
		exeBase := strings.TrimSuffix(exeLower, filepath.Ext(exeLower))
		if len(exeBase) <= 4 || genericExeNames[exeBase] {
			continue // too generic to be diagnostic
		}
		meaningfulExes = append(meaningfulExes, exe)
	}
	if len(meaningfulExes) == 0 {
		return "" // only generic executables — not diagnostic
	}

	for _, exe := range meaningfulExes {
		exeLower := strings.ToLower(exe)
		// Strip extension for a cleaner comparison.
		exeBase := strings.TrimSuffix(exeLower, filepath.Ext(exeLower))
		for word := range titleWords {
			if strings.Contains(exeBase, word) {
				return "" // at least one executable shares a title word
			}
		}
	}

	return fmt.Sprintf("unmatched executable (found: %s)", strings.Join(meaningfulExes, ", "))
}

// DisassociateGame clears the F95Zone URL and thread ID from a game and
// saves the change to the database.
func DisassociateGame(database *db.Database, g *db.Game) {
	g.F95URL = ""
	g.F95ThreadID = 0
	if err := database.UpdateGame(g); err != nil {
		fmt.Fprintf(os.Stderr, "  ⚠ Failed to disassociate #%d: %v\n", g.ID, err)
	} else {
		fmt.Fprintf(os.Stderr, "  ✓ Disassociated #%d\n", g.ID)
	}
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

// RefreshVersions re-extracts versions from directory names.
func RefreshVersions(args []string) {
	database := OpenDB()
	defer database.Close()

	games, err := database.ListGames("", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading games: %v\n", err)
		os.Exit(1)
	}

	updated := 0
	for _, g := range games {
		dirVer := scanner.ExtractVersion(filepath.Base(g.Path))
		if dirVer == "" {
			dirVer = scanner.ExtractVersionFromDir(g.Path)
		}
		if dirVer == "" {
			continue // no version in directory name or files
		}
		if g.Version == dirVer {
			continue // already matches
		}
		oldVer := g.Version
		g.Version = dirVer
		if err := database.UpdateGame(&g); err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠ %q: failed to update version: %v\n", g.Title, err)
			continue
		}
		updated++
		fmt.Fprintf(os.Stderr, "  %-50s %s → %s\n",
			util.Truncate(g.Title, 48), util.TruncateVer(oldVer), dirVer)
	}

	if updated == 0 {
		fmt.Println("No version changes. All games are up to date.")
	} else {
		fmt.Fprintf(os.Stderr, "\nUpdated %d game(s).\n", updated)
	}
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


