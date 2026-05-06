package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mili/moxie/internal/db"
)

// ---------------------------------------------------------------------------
// EngineMatchesTags
// ---------------------------------------------------------------------------

func TestEngineMatchesTags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		detected Result
		tags     []string
		want     bool
	}{
		{
			name:     "RPGM detected, tags contain rpg maker variant",
			detected: Result{Engine: RPGM},
			tags:     []string{"rpg maker", "2dcg"},
			want:     true,
		},
		{
			name:     "RPGM detected, tags contain rpgm abbreviation",
			detected: Result{Engine: RPGM},
			tags:     []string{"rpgm", "completed"},
			want:     true,
		},
		{
			name:     "RPGM detected, tags contain rmmz variant",
			detected: Result{Engine: RPGM},
			tags:     []string{"rmmz", "adult"},
			want:     true,
		},
		{
			name:     "HTML detected, tags only contain rpgm — no direct match",
			detected: Result{Engine: HTML},
			tags:     []string{"rpgm", "completed"},
			want:     false,
		},
		{
			name:     "RenPy detected, tags contain ren'py",
			detected: Result{Engine: RenPy},
			tags:     []string{"ren'py", "completed"},
			want:     true,
		},
		{
			name:     "RenPy detected, tags contain renpy (no apostrophe)",
			detected: Result{Engine: RenPy},
			tags:     []string{"renpy", "adult"},
			want:     true,
		},
		{
			name:     "Unity detected, tags contain unity",
			detected: Result{Engine: Unity},
			tags:     []string{"unity", "3dcg"},
			want:     true,
		},
		{
			name:     "Unity detected, tags only contain renpy — mismatch",
			detected: Result{Engine: Unity},
			tags:     []string{"ren'py", "completed"},
			want:     false,
		},
		{
			name:     "Flash detected, tags contain flash",
			detected: Result{Engine: Flash},
			tags:     []string{"flash", "adult"},
			want:     true,
		},
		{
			name:     "Flash detected, tags only contain html — mismatch",
			detected: Result{Engine: Flash},
			tags:     []string{"html"},
			want:     false,
		},
		{
			name:     "Others engine — inconclusive detection, returns true",
			detected: Result{Engine: Others},
			tags:     []string{"unity"},
			want:     true,
		},
		{
			name:     "Empty engine — inconclusive, returns true",
			detected: Result{Engine: ""},
			tags:     []string{"unity"},
			want:     true,
		},
		{
			name:     "RPGM detected, empty tags — no data to compare",
			detected: Result{Engine: RPGM},
			tags:     []string{},
			want:     true,
		},
		{
			name:     "RPGM detected, tags have no engine info — false",
			detected: Result{Engine: RPGM},
			tags:     []string{"completed", "on_hold"},
			want:     false,
		},
		{
			name:     "Java detected, tags contain java",
			detected: Result{Engine: Java},
			tags:     []string{"java", "adult"},
			want:     true,
		},
		// Unknown engine (not in EngineTagVariants) → EngineMatchesTags returns true
		{
			name:     "Unknown engine with tags — no known variants, inconclusive",
			detected: Result{Engine: "Unknown"},
			tags:     []string{"unity"},
			want:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EngineMatchesTags(tt.detected, tt.tags)
			if got != tt.want {
				t.Errorf("EngineMatchesTags(%v, %v) = %v, want %v",
					tt.detected, tt.tags, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// EngineMatchesThread
// ---------------------------------------------------------------------------

func TestEngineMatchesThread(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		detected Result
		tags     []string
		title    string
		want     bool
	}{
		// === Title prefix matches (primary signal) ===
		{
			name:     "Unity detected, search result title has Unity prefix",
			detected: Result{Engine: Unity},
			tags:     []string{"2dcg", "bdsm", "big tits", "group sex"},
			title:    "VN Unity Completed A Queen Confined [Final] [Banana King]",
			want:     true,
		},
		{
			name:     "RPGM detected, search result title has RPGM prefix",
			detected: Result{Engine: RPGM},
			tags:     []string{"2d game", "2dcg", "ahegao", "animated"},
			title:    "RPGM Completed A Red Flower Shining in the Moonlight v1.03",
			want:     true,
		},
		{
			name:     "RenPy detected, search result title has Ren'Py prefix",
			detected: Result{Engine: RenPy},
			tags:     []string{"adult", "completed"},
			title:    "Ren'Py Completed My Game [v0.5]",
			want:     true,
		},
		{
			name:     "Unity detected, title has unity lowercase",
			detected: Result{Engine: Unity},
			tags:     []string{"3dcg"},
			title:    "unity completed some game",
			want:     true,
		},

		// === Tag fallback (secondary signal) ===
		{
			name:     "RPGM detected, tags have rpg maker but title is ambiguous",
			detected: Result{Engine: RPGM},
			tags:     []string{"rpg maker", "completed"},
			title:    "Some Game Without Prefix",
			want:     true,
		},

		// === Inconclusive: Others engine, empty engine, unknown variants ===
		{
			name:     "Others engine — always inconclusive",
			detected: Result{Engine: Others},
			tags:     []string{"unity"},
			title:    "Unity Completed Game",
			want:     true,
		},
		{
			name:     "Empty engine — always inconclusive",
			detected: Result{Engine: ""},
			tags:     []string{"unity"},
			title:    "Unity Completed Game",
			want:     true,
		},
		{
			name:     "Unknown engine with no variants — inconclusive",
			detected: Result{Engine: "UnknownEngine"},
			tags:     []string{"unity"},
			title:    "Unity Completed Game",
			want:     true,
		},

		// === No engine metadata present (inconclusive) ===
		{
			name:     "RPGM detected, no engine info in title or tags — inconclusive",
			detected: Result{Engine: RPGM},
			tags:     []string{"completed", "on_hold"},
			title:    "Game Name [v1.0]",
			want:     true, // no engine in either source — can't verify, don't flag
		},
		{
			name:     "RPGM detected, empty tags and title — inconclusive",
			detected: Result{Engine: RPGM},
			tags:     []string{},
			title:    "",
			want:     true,
		},

		// === Real mismatches (contradictory engine info exists) ===
		{
			name:     "Unity detected, title says Ren'Py — true mismatch",
			detected: Result{Engine: Unity},
			tags:     []string{"ren'py", "completed"},
			title:    "Ren'Py Completed Some Other Game",
			want:     false,
		},
		{
			name:     "Unity detected, title says RPGM — true mismatch",
			detected: Result{Engine: Unity},
			tags:     []string{"rpgm", "completed"},
			title:    "RPGM Completed Different Game",
			want:     false,
		},
		{
			name:     "HTML detected, title says RPGM — compatible via EngineCompat",
			detected: Result{Engine: HTML},
			tags:     []string{"rpgm"},
			title:    "RPGM Completed Game",
			want:     true, // HTML↔RPGM compatibility
		},
		{
			name:     "RPGM detected, title says HTML — compatible",
			detected: Result{Engine: RPGM},
			tags:     []string{"html5"},
			title:    "HTML Game Name",
			want:     true, // RPGM↔HTML compatibility
		},

		// === Edge cases ===
		{
			name:     "Unity detected, title has unity in middle of text",
			detected: Result{Engine: Unity},
			tags:     []string{"3dcg"},
			title:    "The unity Engine Game",
			want:     true,
		},
		{
			name:     "Flash detected, title says Flash Game",
			detected: Result{Engine: Flash},
			tags:     []string{"adult"},
			title:    "Flash Completed My Game",
			want:     true,
		},
		{
			name:     "Flash detected, tags say HTML, title says HTML — mismatch",
			detected: Result{Engine: Flash},
			tags:     []string{"html"},
			title:    "HTML Game",
			want:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EngineMatchesThread(tt.detected, tt.tags, tt.title)
			if got != tt.want {
				t.Errorf("EngineMatchesThread(%v, %v, %q) = %v, want %v",
					tt.detected, tt.tags, tt.title, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// FindF95Engine
// ---------------------------------------------------------------------------

func TestFindF95Engine_FromTags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		game db.Game
		want string
	}{
		{
			name: "rpg maker tag → RPGM",
			game: db.Game{Tags: []string{"rpg maker", "completed"}},
			want: "RPGM",
		},
		{
			name: "renpy tag → RenPy",
			game: db.Game{Tags: []string{"renpy", "adult"}},
			want: "RenPy",
		},
		{
			name: "unity tag → Unity",
			game: db.Game{Tags: []string{"unity", "3dcg"}},
			want: "Unity",
		},
		{
			name: "wolf rpg tag → WolfRPG",
			game: db.Game{Tags: []string{"wolf rpg", "adult"}},
			want: "WolfRPG",
		},
		{
			name: "html5 tag → HTML",
			game: db.Game{Tags: []string{"html5"}},
			want: "HTML",
		},
		{
			name: "unreal engine tag → UnrealEngine",
			game: db.Game{Tags: []string{"unreal engine", "3dcg"}},
			want: "UnrealEngine",
		},
		{
			name: "flash tag → Flash",
			game: db.Game{Tags: []string{"flash"}},
			want: "Flash",
		},
		{
			name: "webgl tag → WebGL",
			game: db.Game{Tags: []string{"webgl"}},
			want: "WebGL",
		},
		{
			name: "adrift tag → ADRIFT",
			game: db.Game{Tags: []string{"adrift"}},
			want: "ADRIFT",
		},
		{
			name: "qsp tag → QSP",
			game: db.Game{Tags: []string{"qsp"}},
			want: "QSP",
		},
		{
			name: "rags tag → RAGS",
			game: db.Game{Tags: []string{"rags"}},
			want: "RAGS",
		},
		{
			name: "tads tag → Tads",
			game: db.Game{Tags: []string{"tads"}},
			want: "Tads",
		},
		{
			name: "no matching tags → empty",
			game: db.Game{Tags: []string{"completed", "adult"}},
			want: "",
		},
		{
			name: "empty tags → empty",
			game: db.Game{Tags: []string{}},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindF95Engine(tt.game)
			if got != tt.want {
				t.Errorf("FindF95Engine(%+v) = %q, want %q", tt.game, got, tt.want)
			}
		})
	}
}

func TestFindF95Engine_FromTitle(t *testing.T) {
	t.Parallel()
	// FindF95Engine falls back to checking the title prefix when tags
	// don't contain an engine indicator.  It checks HasPrefix on the
	// lowercased title against each variant.
	tests := []struct {
		name string
		game db.Game
		want string
	}{
		{
			name: "title starts with 'rpgm ' → RPGM",
			game: db.Game{Title: "RPGM Completed Game Name"},
			want: "RPGM",
		},
		{
			name: "title starts with 'ren'py ' → RenPy",
			game: db.Game{Title: "Ren'Py Abandoned Game [v1.0]"},
			want: "RenPy",
		},
		{
			name: "title starts with 'unity ' → Unity",
			game: db.Game{Title: "Unity 3D Game Title"},
			want: "Unity",
		},
		{
			name: "title starts with 'html ' → HTML",
			game: db.Game{Title: "HTML Game Title"},
			want: "HTML",
		},
		{
			name: "title starts with 'unreal engine ' → UnrealEngine",
			game: db.Game{Title: "Unreal Engine Game"},
			want: "UnrealEngine",
		},
		{
			name: "title starts with 'flash ' → Flash",
			game: db.Game{Title: "Flash Game Title"},
			want: "Flash",
		},
		{
			name: "title has no engine prefix → empty",
			game: db.Game{Title: "My Awesome Game"},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindF95Engine(tt.game)
			if got != tt.want {
				t.Errorf("FindF95Engine(%+v) = %q, want %q", tt.game, got, tt.want)
			}
		})
	}
}

func TestFindF95Engine_TagsTakePriority(t *testing.T) {
	t.Parallel()
	// Tags should be checked before title.  Even if the title has an
	// engine prefix, tags that don't contain any engine info should
	// cause FindF95Engine to fall through to the title check.
	game := db.Game{
		Tags:  []string{"completed", "adult"},
		Title: "Unity My Game",
	}
	got := FindF95Engine(game)
	if got != "Unity" {
		t.Errorf("FindF95Engine should fall back to title when tags have no engine, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// EngineTagVariants — sanity checks
// ---------------------------------------------------------------------------

func TestEngineTagVariants_AllEnginesPresent(t *testing.T) {
	t.Parallel()
	// Every canonical engine name that appears in the codebase should
	// have at least one tag variant defined.  This test catches new
	// engines added to the detector but not to the tag variant map.
	// Use AllEngines() to derive the list dynamically, skipping
	// Others because it's a catch-all rather than a specific engine.
	for _, eng := range AllEngines() {
		if eng == Others {
			continue
		}
		key := string(eng)
		if len(EngineTagVariants[key]) == 0 {
			t.Errorf("EngineTagVariants missing entry for %q", key)
		}
	}
}

func TestEngineTagVariants_NoEmptyVariants(t *testing.T) {
	t.Parallel()
	for eng, variants := range EngineTagVariants {
		if len(variants) == 0 {
			t.Errorf("EngineTagVariants[%q] has empty variants list", eng)
		}
		for _, v := range variants {
			if v == "" {
				t.Errorf("EngineTagVariants[%q] contains empty variant string", eng)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// EngineCompat — sanity checks
// ---------------------------------------------------------------------------

func TestEngineCompat_SelfCompatible(t *testing.T) {
	t.Parallel()
	// Every engine in the compat map should be compatible with itself.
	for eng, compat := range EngineCompat {
		if !compat[eng] {
			t.Errorf("EngineCompat[%q] should be self-compatible but isn't", eng)
		}
	}
}

func TestEngineCompat_SymmetricPairs(t *testing.T) {
	t.Parallel()
	// Certain pairs MUST be symmetric (e.g., RPGM↔HTML share a runtime).
	// Other pairs are intentionally one-directional (e.g., WolfRPG→HTML
	// because WolfRPG uses HTML internally, but not all HTML games are
	// WolfRPG — so HTML shouldn't list WolfRPG back).
	type symPair struct{ a, b string }
	requiredSymmetric := []symPair{
		{"RPGM", "HTML"},
		{"HTML", "WebGL"},
	}
	for _, p := range requiredSymmetric {
		aCompat := EngineCompat[p.a]
		bCompat := EngineCompat[p.b]
		if aCompat == nil || !aCompat[p.b] {
			t.Errorf("expected EngineCompat[%q][%q] = true", p.a, p.b)
		}
		if bCompat == nil || !bCompat[p.a] {
			t.Errorf("expected EngineCompat[%q][%q] = true (symmetric pair)", p.b, p.a)
		}
	}
}

func TestEngineCompat_ExpectedPairs(t *testing.T) {
	t.Parallel()
	// Verify specific expected compatibility relationships.
	type pair struct{ a, b string }
	expected := []pair{
		{"RPGM", "HTML"},
		{"HTML", "WebGL"},
		{"WolfRPG", "HTML"},
	}
	for _, p := range expected {
		if compat, ok := EngineCompat[p.a]; !ok || !compat[p.b] {
			t.Errorf("expected EngineCompat[%q][%q] = true, got false or missing", p.a, p.b)
		}
	}

	// WolfRPG→HTML is one-directional: WolfRPG uses HTML internally,
	// but not all HTML games are WolfRPG.
	t.Run("WolfRPG→HTML is one-directional", func(t *testing.T) {
		t.Parallel()
		htmlCompat := EngineCompat["HTML"]
		if htmlCompat != nil && htmlCompat["WolfRPG"] {
			t.Error("HTML should NOT be compatible with WolfRPG — it's one-directional")
		}
	})
}

// ---------------------------------------------------------------------------
// FormatTagsBrief
// ---------------------------------------------------------------------------

func TestFormatTagsBrief(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tags []string
		max  int
		want string
	}{
		{[]string{}, 4, ""},
		{[]string{"a"}, 4, "a"},
		{[]string{"a", "b", "c"}, 4, "a, b, c"},
		{[]string{"a", "b", "c", "d", "e"}, 3, "a, b, c (+2 more)"},
		{[]string{"a", "b"}, 2, "a, b"},
		{[]string{"a", "b", "c"}, 1, "a (+2 more)"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%v/%d", tt.tags, tt.max), func(t *testing.T) {
			got := FormatTagsBrief(tt.tags, tt.max)
			if got != tt.want {
				t.Errorf("FormatTagsBrief(%v, %d) = %q, want %q", tt.tags, tt.max, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ExtractEngineFromTitle
// ---------------------------------------------------------------------------

func TestExtractEngineFromTitle(t *testing.T) {
	t.Parallel()
	tests := []struct {
		title string
		want  string
		note  string // optional context
	}{
		{"RPGM Completed Game Name", "RPGM", ""},
		{"Ren'Py Abandoned My Game [v0.5]", "RenPy", ""},
		{"Unity Game Title", "Unity", ""},
		{"Completed Visual Novel", "", "no engine prefix — first word is 'completed'"},
		{"Wolf RPG Something", "", "first word is 'Wolf', variants are 'wolf rpg'/'wolfrpg' — no exact match"},
		{"HTML5 Web Game", "HTML", "first word 'html5' matches HTML variant"},
		{"Flash Game Title", "Flash", ""},
		{"", "", "empty title"},
	}
	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			got := ExtractEngineFromTitle(tt.title)
			if got != tt.want {
				t.Errorf("ExtractEngineFromTitle(%q) = %q, want %q%s",
					tt.title, got, tt.want, optionalNote(tt.note))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// optionalNote returns a colon-prefixed note if s is non-empty.
func optionalNote(s string) string {
	if s == "" {
		return ""
	}
	return " // " + s
}

// ---------------------------------------------------------------------------
// Helper assertions for test readability
// ---------------------------------------------------------------------------

// assertStringContains fails t if s does not contain substr.
func assertStringContains(t *testing.T, s, substr, context string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("%s: expected %q to contain %q", context, s, substr)
	}
}
