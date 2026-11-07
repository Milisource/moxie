package version

import (
	"regexp"
	"strconv"
	"strings"
)

// PreRelease holds an ordering qualifier such as alpha/beta/rc.
type PreRelease struct {
	Kind string // alpha, beta, rc
	N    int
}

// Version is the structured canonical form of a game version string.
type Version struct {
	Track  []int
	Core   []int
	Pre    PreRelease
	HasPre bool
	Fix    int
	Build  string
	Kind   string // empty, date, final, numeric, other
}

var (
	// trackReC matches a season/chapter/episode prefix and its number. The
	// leading \b matters: matching inside a word turned "patch 4.1" into
	// chapter 4 and "update 27fix2" into episode 27, which made unrelated
	// versions unorderable and hid real updates.
	trackReC = regexp.MustCompile(`(?i)\b(season|chapter|episode|ch|ep|s|e)\.?\s*(\d+)`)
)

func isNoiseToken(tok string) bool {
	t := strings.ToLower(strings.TrimSpace(tok))
	t = strings.Trim(t, "[](){}.,+")
	if t == "" {
		return true
	}
	if strings.HasPrefix(t, "wip") {
		return true
	}
	switch t {
	case "wip", "public", "patreon", "dlc", "steam", "english", "translation",
		"request", "full", "ver", "version", "cracked", "complete", "completed", "final", "ea", "+":
		return true
	}
	return false
}

func isPreTokenC(tok string) bool {
	t := strings.ToLower(tok)
	return strings.Contains(t, "alpha") || strings.Contains(t, "beta") ||
		strings.Contains(t, "rc") || strings.Contains(t, "β")
}

func parsePreC(tok string) (string, int) {
	t := strings.ToLower(tok)
	kind := ""
	if strings.Contains(t, "alpha") {
		kind = "alpha"
	} else if strings.Contains(t, "beta") || strings.Contains(t, "β") {
		kind = "beta"
	} else if strings.Contains(t, "rc") {
		kind = "rc"
	}
	n := 0
	for _, m := range regexp.MustCompile(`\d+`).FindAllString(t, -1) {
		nn, _ := strconv.Atoi(m)
		n = nn
		break
	}
	return kind, n
}

func isFixTokenC(tok string) bool {
	t := strings.ToLower(tok)
	return strings.Contains(t, "fix")
}

func parseFixC(tok string) int {
	n := 0
	for _, m := range regexp.MustCompile(`\d+`).FindAllString(tok, -1) {
		nn, _ := strconv.Atoi(m)
		n = nn
	}
	if n == 0 {
		n = 1
	}
	return n
}

func looksLikeVersion(tok string) bool {
	t := strings.TrimLeft(tok, "vV")
	if t == "" {
		return false
	}
	for _, r := range t {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

func isTrackKeywordTokenC(tok string) bool {
	t := strings.ToLower(strings.Trim(tok, "."))
	for _, kw := range []string{"season", "chapter", "episode", "ch", "ep"} {
		if t == kw || strings.HasPrefix(t, kw) {
			return true
		}
	}
	if len(t) > 1 && (t[0] == 's' || t[0] == 'e') {
		rest := strings.TrimPrefix(t[1:], ".")
		if _, err := strconv.Atoi(rest); err == nil {
			return true
		}
	}
	return false
}

// Parse converts a raw version string into its structured form.
func Parse(raw string) Version {
	v := Version{Kind: "numeric"}
	s := strings.TrimSpace(raw)
	if s == "" {
		v.Kind = "empty"
		return v
	}
	low := strings.ToLower(s)
	if dateRe.MatchString(low) {
		v.Kind = "date"
		for _, m := range regexp.MustCompile(`\d+`).FindAllString(low, -1) {
			nn, _ := strconv.Atoi(m)
			v.Core = append(v.Core, nn)
		}
		return v
	}
	for _, m := range trackReC.FindAllStringSubmatch(s, -1) {
		nn, _ := strconv.Atoi(m[2])
		v.Track = append(v.Track, nn)
	}
	tokens := strings.Fields(s)
	for _, tok := range tokens {
		tl := strings.ToLower(tok)
		if isNoiseToken(tl) {
			continue
		}
		if isTrackKeywordTokenC(tl) {
			continue
		}
		if isPreTokenC(tl) {
			k, n := parsePreC(tl)
			v.Pre = PreRelease{Kind: k, N: n}
			v.HasPre = true
			continue
		}
		if isFixTokenC(tl) {
			v.Fix = parseFixC(tl)
			continue
		}
		if looksLikeVersion(tok) {
			if len(v.Core) == 0 {
				for _, m := range regexp.MustCompile(`\d+`).FindAllString(tok, -1) {
					nn, _ := strconv.Atoi(m)
					v.Core = append(v.Core, nn)
				}
				// trailing build letter, e.g. "0.8.1b"
				tt := strings.TrimLeft(tok, "vV")
				if len(tt) > 0 {
					last := tt[len(tt)-1]
					if (last >= 'a' && last <= 'z') || (last >= 'A' && last <= 'Z') {
						v.Build = strings.ToLower(string(last))
					}
				}
				continue
			}
		}
	}
	// A version that is only a chapter/season/episode number stays
	// incomparable with plain numeric versions ("Ch.4 Free" must never beat
	// "0.12.0"); chapter forms still order among themselves.
	if len(v.Track) > 0 && len(v.Core) == 0 && v.Kind == "numeric" {
		v.Kind = "chapter"
	}
	if strings.Contains(low, "final") || strings.Contains(low, "complete") {
		v.Kind = "final"
	}
	// Keep trailing zeros so "1.0" stays [1,0] and compares correctly
	// against "1.2"; missing components are handled in Compare.
	if len(v.Track) == 0 && len(v.Core) == 0 && v.Kind != "final" {
		v.Kind = "other"
	}
	return v
}

// Canonical returns a normalized string form of the version.
func Canonical(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	low := strings.ToLower(s)
	if dateRe.MatchString(low) {
		return low
	}
	tokens := strings.Fields(s)
	var out []string
	for _, tok := range tokens {
		tl := strings.ToLower(tok)
		if isNoiseToken(tl) {
			continue
		}
		// normalize leading v
		tl = strings.TrimPrefix(tl, "v")
		if !dateRe.MatchString(tl) {
			// normalize digit separators
			for i := 0; i < 8; i++ {
				nl := digitSepRe.ReplaceAllString(tl, "$1.$2")
				if nl == tl {
					break
				}
				tl = nl
			}
		}
		// trim trailing zeros
		if looksLikeVersion(tl) {
			for strings.HasSuffix(tl, ".0") {
				tl = strings.TrimSuffix(tl, ".0")
			}
		}
		out = append(out, tl)
	}
	res := strings.Join(out, " ")
	if (strings.Contains(low, "final") || strings.Contains(low, "complete")) && len(out) == 0 {
		res = "Final"
	}
	// normalize final capitalization? keep lowercase for canonical
	if strings.Contains(low, "final") && res != "final" {
		// keep as is
	}
	return res
}

func preRank(k string) int {
	switch k {
	case "alpha":
		return 0
	case "beta":
		return 1
	case "rc":
		return 2
	}
	return 3 // release
}

// CompareStructured compares two parsed versions.
func CompareStructured(a, b Version) Diff {
	if a.Kind != b.Kind && a.Kind != "numeric" && b.Kind != "numeric" {
		// different kinds (date vs numeric, etc.) -> not comparable
		// but allow numeric/other? handle below
	}
	if a.Kind != b.Kind && !(a.Kind == "numeric" && b.Kind == "numeric") && !(a.Kind == "other" && b.Kind == "other") && a.Kind != "final" && b.Kind != "final" {
		return Changed
	}
	if len(a.Core) == 0 && len(a.Track) == 0 && a.Kind != "final" && b.Kind != "final" {
		// No numeric substance: not orderable.
		if versionsEqualC(a, b) {
			return Same
		}
		return Changed
	}
	if a.Kind == "final" {
		if b.Kind == "final" {
			if versionsEqualC(a, b) {
				return Same
			}
			return Newer
		}
		return Newer
	}
	if b.Kind == "final" {
		return Changed
	}
	// track
	if len(a.Track) != len(b.Track) {
		minN := len(a.Track)
		if len(b.Track) < minN {
			minN = len(b.Track)
		}
		for i := 0; i < minN; i++ {
			if a.Track[i] != b.Track[i] {
				if a.Track[i] > b.Track[i] {
					return Newer
				}
				return Older
			}
		}
		if len(a.Track) != len(b.Track) {
			return Changed
		}
	} else {
		for i := range a.Track {
			if a.Track[i] != b.Track[i] {
				if a.Track[i] > b.Track[i] {
					return Newer
				}
				return Older
			}
		}
	}
	// core
	if len(a.Core) != len(b.Core) {
		minN := len(a.Core)
		if len(b.Core) < minN {
			minN = len(b.Core)
		}
		for i := 0; i < minN; i++ {
			if a.Core[i] != b.Core[i] {
				if a.Core[i] > b.Core[i] {
					return Newer
				}
				return Older
			}
		}
		return Changed
	}
	for i := range a.Core {
		if a.Core[i] != b.Core[i] {
			if a.Core[i] > b.Core[i] {
				return Newer
			}
			return Older
		}
	}
	// pre
	ar := 3
	if a.HasPre {
		ar = preRank(a.Pre.Kind)
	}
	br := 3
	if b.HasPre {
		br = preRank(b.Pre.Kind)
	}
	if ar != br {
		if ar > br {
			return Newer
		}
		return Older
	}
	if a.HasPre && b.HasPre && a.Pre.Kind == b.Pre.Kind {
		if a.Pre.N != b.Pre.N {
			if a.Pre.N > b.Pre.N {
				return Newer
			}
			return Older
		}
	}
	// fix
	if a.Fix != b.Fix {
		if a.Fix > b.Fix {
			return Newer
		}
		return Older
	}
	// build letter
	if a.Build != b.Build {
		if a.Build != "" && b.Build == "" {
			return Newer
		}
		if a.Build == "" && b.Build != "" {
			return Older
		}
		if a.Build > b.Build {
			return Newer
		}
		return Older
	}
	// empty/other without core -> Changed
	if len(a.Core) == 0 && len(a.Track) == 0 && a.Kind != "final" {
		return Changed
	}
	if len(b.Core) == 0 && len(b.Track) == 0 && b.Kind != "final" {
		return Changed
	}
	if versionsEqualC(a, b) {
		return Same
	}
	return Changed
}

func versionsEqualC(a, b Version) bool {
	if len(a.Track) != len(b.Track) || len(a.Core) != len(b.Core) {
		return false
	}
	for i := range a.Track {
		if a.Track[i] != b.Track[i] {
			return false
		}
	}
	for i := range a.Core {
		if a.Core[i] != b.Core[i] {
			return false
		}
	}
	if a.HasPre != b.HasPre {
		return false
	}
	if a.HasPre && (a.Pre.Kind != b.Pre.Kind || a.Pre.N != b.Pre.N) {
		return false
	}
	if a.Fix != b.Fix || a.Kind != b.Kind {
		return false
	}
	return true
}