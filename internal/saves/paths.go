package saves

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/mili/moxie/internal/db"
)

// saveCandidateLimit caps how many root-derived Ren'Py candidates are offered.
const saveCandidateLimit = 8

// rpgmEngines maps the engine labels that describe an RPG Maker game onto the
// RPGM resolver. The canonical detected label is "RPGM"; the F95Zone tag names
// (see db.Game.Engine) are accepted too so enriched libraries resolve as well.
var rpgmEngines = map[string]bool{
	"RPGM":          true,
	"RPGMaker":      true,
	"RPGMakerMV":    true,
	"RPGMakerMZ":    true,
	"RPGMakerVXAce": true,
}

// defaultSteamRoots returns the Steam install roots that are probed for Proton
// prefixes, in "~"-relative form (expanded against the user's home directory).
func defaultSteamRoots() []string {
	return []string{
		"~/.local/share/Steam",
		"~/.steam/steam",
		"~/.steam/root",
	}
}

// ResolveCandidates returns the possible live save locations for game, ordered
// best first (highest Confidence first). It is pure: it reads only the
// filesystem and never touches the database. It returns nil when no plausible
// location exists — an unsupported engine, or a supported one whose save data
// could not be located.
//
// Callers must not guess between equally plausible candidates; use
// PickLocation to decide, or surface the list to the user.
func ResolveCandidates(game *db.Game) []Location {
	if game == nil {
		return nil
	}
	home, _ := os.UserHomeDir()
	return resolveCandidates(game, home, defaultSteamRoots())
}

// resolveCandidates is the hermetic core of ResolveCandidates. home and
// steamRoots are injected so tests can point at fabricated directory trees;
// steamRoots entries may be "~"-relative (expanded via home) or absolute.
func resolveCandidates(game *db.Game, home string, steamRoots []string) []Location {
	if game == nil {
		return nil
	}
	switch {
	case isRPGMGame(game):
		return rpgmCandidates(game)
	case isRenPyGame(game):
		return renpyCandidates(game, home, steamRoots)
	default:
		return nil
	}
}

// isRPGMGame reports whether the game is an RPG Maker title (MV, MZ, or VX Ace).
func isRPGMGame(game *db.Game) bool {
	return rpgmEngines[game.Engine]
}

// isRenPyGame reports whether the game should be resolved as Ren'Py: either the
// engine says so, or the game directory contains a game/ subdirectory holding
// compiled or source Ren'Py scripts.
func isRenPyGame(game *db.Game) bool {
	if game.Engine == "RenPy" {
		return true
	}
	if game.Path == "" {
		return false
	}
	entries, err := os.ReadDir(filepath.Join(game.Path, "game"))
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".rpy", ".rpyc":
			return true
		}
	}
	return false
}

// renpyCandidates gathers every plausible Ren'Py save location for game:
// in-game portable directories, then title-matched subdirectories of the known
// Ren'Py save roots (native ~/.renpy, Wine prefixes, and Steam Proton prefixes).
// Results are sorted best-first and capped at saveCandidateLimit.
func renpyCandidates(game *db.Game, home string, steamRoots []string) []Location {
	var out []Location
	optionsName := ""

	if game.Path != "" {
		// Portable layout: saves live inside the game directory.
		for _, sub := range []string{filepath.Join("game", "saves"), "saves"} {
			p := filepath.Join(game.Path, sub)
			if saveDirExists(p) {
				out = append(out, Location{
					Kind:       KindDir,
					Path:       p,
					Confidence: 0.95,
					Note:       "portable saves dir",
				})
			}
		}
		// A portable build still names its non-portable save directory, which
		// lets us match it exactly inside the shared roots below.
		optionsName = readSaveDirectoryName(filepath.Join(game.Path, "game", "options.rpy"))
	}

	out = append(out, renpyRootCandidates(game, home, steamRoots, optionsName)...)

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Confidence > out[j].Confidence
	})
	if len(out) > saveCandidateLimit {
		out = out[:saveCandidateLimit]
	}
	return out
}

// renpyRoot is a directory containing one subdirectory per Ren'Py save
// directory, paired with a short provenance note.
type renpyRoot struct {
	path string
	note string
}

// renpySaveRoots enumerates the existing Ren'Py save roots for game.
func renpySaveRoots(game *db.Game, home string, steamRoots []string) []renpyRoot {
	var roots []renpyRoot

	if home != "" {
		if r := filepath.Join(home, ".renpy"); saveDirExists(r) {
			roots = append(roots, renpyRoot{path: r, note: "renpy native"})
		}
		pattern := filepath.Join(home, ".wine", "drive_c", "users", "*",
			"AppData", "Roaming", "RenPy")
		if matches, err := filepath.Glob(pattern); err == nil {
			sort.Strings(matches)
			for _, m := range matches {
				if saveDirExists(m) {
					roots = append(roots, renpyRoot{path: m, note: "wine prefix"})
				}
			}
		}
	}

	if game.SteamAppID > 0 {
		for _, root := range steamRoots {
			p := filepath.Join(saveExpandHome(root, home), "steamapps", "compatdata",
				fmt.Sprintf("%d", game.SteamAppID), "pfx", "drive_c", "users",
				"steamuser", "AppData", "Roaming", "RenPy")
			if saveDirExists(p) {
				roots = append(roots, renpyRoot{path: p, note: "steam proton prefix"})
			}
		}
	}

	return roots
}

// renpyRootCandidates scores each immediate subdirectory of each Ren'Py save
// root against the game. Directories that cannot be tied to the game are
// dropped rather than emitted with a low confidence.
func renpyRootCandidates(game *db.Game, home string, steamRoots []string, optionsName string) []Location {
	var out []Location
	for _, root := range renpySaveRoots(game, home, steamRoots) {
		entries, err := os.ReadDir(root.path)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			conf := scoreRenpyDir(e.Name(), game, optionsName)
			if conf <= 0 {
				continue
			}
			out = append(out, Location{
				Kind:       KindDir,
				Path:       filepath.Join(root.path, e.Name()),
				Confidence: conf,
				Note:       root.note,
			})
		}
	}
	return out
}

// scoreRenpyDir returns how strongly a Ren'Py save-directory name matches the
// game, or 0 when it does not match at all. An exact match against the name
// declared in options.rpy wins, then exact title/base-name matches, then
// substring matches (requiring at least 4 characters).
func scoreRenpyDir(dirName string, game *db.Game, optionsName string) float64 {
	if optionsName != "" && dirName == optionsName {
		return 0.93
	}

	cand := normalizeSaveName(stripSaveTimestamp(dirName))
	if cand == "" {
		return 0
	}
	targets := []string{
		normalizeSaveName(game.Title),
		normalizeSaveName(filepath.Base(game.Path)),
	}
	for _, target := range targets {
		if target != "" && cand == target {
			return 0.9
		}
	}
	for _, target := range targets {
		if saveNameContains(cand, target) {
			return 0.75
		}
	}
	return 0
}

// saveTimestampRe matches a trailing "-<digits>" Ren'Py appends to save
// directory names.
var saveTimestampRe = regexp.MustCompile(`-\d+$`)

// stripSaveTimestamp removes a trailing "-<digits>" suffix.
func stripSaveTimestamp(s string) string {
	return saveTimestampRe.ReplaceAllString(s, "")
}

// normalizeSaveName lowercases s and drops every non-alphanumeric rune, so
// "My Game!" and "my_game" compare equal.
func normalizeSaveName(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// saveNameContains reports whether either string contains the other, requiring
// the contained string to be at least 4 characters to avoid trivial matches.
func saveNameContains(a, b string) bool {
	if a == "" || b == "" || a == b {
		return false
	}
	if len(a) >= 4 && strings.Contains(b, a) {
		return true
	}
	return len(b) >= 4 && strings.Contains(a, b)
}

// saveDirectoryRe captures a single-line config.save_directory string literal.
var saveDirectoryRe = regexp.MustCompile(`config\.save_directory\s*=\s*(?:"([^"]*)"|'([^']*)')`)

// readSaveDirectoryName extracts the literal save-directory name from a Ren'Py
// options.rpy file. It returns "" when the file is missing, when no assignment
// is present, or when the value is dynamic (references another variable) —
// dynamic values cannot be resolved without executing the game.
func readSaveDirectoryName(optionsPath string) string {
	data, err := os.ReadFile(optionsPath)
	if err != nil {
		return ""
	}
	m := saveDirectoryRe.FindSubmatch(data)
	if m == nil {
		return ""
	}
	name := string(m[1])
	if name == "" {
		name = string(m[2])
	}
	if name == "" || strings.ContainsAny(name, "$%(") {
		return ""
	}
	return name
}

// rpgmCandidates resolves a single save location for an RPG Maker game.
// VX Ace keeps loose .rvdata2 files in the game directory; MV and MZ keep a
// save/ directory (MV nests it under www/, MZ does not). The MV/MZ target is
// emitted even before the game has created it, because it only appears on the
// first save.
func rpgmCandidates(game *db.Game) []Location {
	dir := game.Path
	if dir == "" {
		return nil
	}
	if hasVXAceSaveFiles(dir) {
		return []Location{{
			Kind:       KindFiles,
			Path:       dir,
			Globs:      []string{"Save*.rvdata2", "Global.rvdata2"},
			Confidence: 0.9,
			Note:       "vx ace save files in game dir",
		}}
	}

	variant := "mz"
	target := filepath.Join(dir, "save")
	if saveDirExists(filepath.Join(dir, "www")) {
		variant = "mv"
		target = filepath.Join(dir, "www", "save")
	}

	conf := 0.7
	note := fmt.Sprintf("rpg maker %s save dir (not created yet)", variant)
	if saveDirExists(target) {
		conf = 0.85
		note = fmt.Sprintf("rpg maker %s save dir", variant)
	}
	return []Location{{
		Kind:       KindDir,
		Path:       target,
		Confidence: conf,
		Note:       note,
	}}
}

// hasVXAceSaveFiles reports whether dir holds RPG Maker VX Ace save files.
func hasVXAceSaveFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "Global.rvdata2" {
			return true
		}
		if strings.HasPrefix(name, "Save") && strings.HasSuffix(name, ".rvdata2") {
			return true
		}
	}
	return false
}

// PickLocation chooses the save location to use from candidates and an optional
// manual override path. Its semantics are:
//
//   - manualPath != "": the override always wins. If its cleaned path equals a
//     candidate's cleaned path, that candidate is returned (preserving its Kind
//     and Globs) with Note "manual override"; otherwise a fresh KindDir location
//     with Confidence 1 and Note "manual override" is returned. The second
//     return value is always nil.
//   - exactly one candidate: it is returned, with a nil pick list.
//   - more than one candidate: the location is zero and the candidates are
//     returned so the caller can show a pick list. Ambiguity is never guessed.
//   - no candidates: a zero location and a nil pick list.
func PickLocation(candidates []Location, manualPath string) (Location, []Location) {
	if manualPath != "" {
		cleaned := filepath.Clean(manualPath)
		for _, c := range candidates {
			if filepath.Clean(c.Path) == cleaned {
				c.Note = "manual override"
				return c, nil
			}
		}
		return Location{
			Kind:       KindDir,
			Path:       cleaned,
			Confidence: 1,
			Note:       "manual override",
		}, nil
	}

	switch len(candidates) {
	case 0:
		return Location{}, nil
	case 1:
		return candidates[0], nil
	default:
		return Location{}, candidates
	}
}

// saveDirExists reports whether path exists and is a directory.
func saveDirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// saveExpandHome expands a leading "~" in p using home. It is a no-op for
// absolute paths and when home is unknown.
func saveExpandHome(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		if home == "" {
			return p
		}
		return filepath.Join(home, p[2:])
	}
	return p
}
