package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/engine"
)

// ---------------------------------------------------------------------------
// engineMatchesTags
// ---------------------------------------------------------------------------

func TestEngineMatchesTags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		detected engine.Result
		tags     []string
		want     bool
	}{
		{
			name:     "RPGM detected, tags contain rpg maker variant",
			detected: engine.Result{Engine: engine.RPGM},
			tags:     []string{"rpg maker", "2dcg"},
			want:     true,
		},
		{
			name:     "RPGM detected, tags contain rpgm abbreviation",
			detected: engine.Result{Engine: engine.RPGM},
			tags:     []string{"rpgm", "completed"},
			want:     true,
		},
		{
			name:     "RPGM detected, tags contain rmmz variant",
			detected: engine.Result{Engine: engine.RPGM},
			tags:     []string{"rmmz", "adult"},
			want:     true,
		},
		{
			name:     "HTML detected, tags only contain rpgm — no direct match",
			detected: engine.Result{Engine: engine.HTML},
			tags:     []string{"rpgm", "completed"},
			want:     false,
		},
		{
			name:     "RenPy detected, tags contain ren'py",
			detected: engine.Result{Engine: engine.RenPy},
			tags:     []string{"ren'py", "completed"},
			want:     true,
		},
		{
			name:     "RenPy detected, tags contain renpy (no apostrophe)",
			detected: engine.Result{Engine: engine.RenPy},
			tags:     []string{"renpy", "adult"},
			want:     true,
		},
		{
			name:     "Unity detected, tags contain unity",
			detected: engine.Result{Engine: engine.Unity},
			tags:     []string{"unity", "3dcg"},
			want:     true,
		},
		{
			name:     "Unity detected, tags only contain renpy — mismatch",
			detected: engine.Result{Engine: engine.Unity},
			tags:     []string{"ren'py", "completed"},
			want:     false,
		},
		{
			name:     "Flash detected, tags contain flash",
			detected: engine.Result{Engine: engine.Flash},
			tags:     []string{"flash", "adult"},
			want:     true,
		},
		{
			name:     "Flash detected, tags only contain html — mismatch",
			detected: engine.Result{Engine: engine.Flash},
			tags:     []string{"html"},
			want:     false,
		},
		{
			name:     "Others engine — inconclusive detection, returns true",
			detected: engine.Result{Engine: engine.Others},
			tags:     []string{"unity"},
			want:     true,
		},
		{
			name:     "Empty engine — inconclusive, returns true",
			detected: engine.Result{Engine: ""},
			tags:     []string{"unity"},
			want:     true,
		},
		{
			name:     "RPGM detected, empty tags — no data to compare",
			detected: engine.Result{Engine: engine.RPGM},
			tags:     []string{},
			want:     true,
		},
		{
			name:     "RPGM detected, tags have no engine info — false",
			detected: engine.Result{Engine: engine.RPGM},
			tags:     []string{"completed", "on_hold"},
			want:     false,
		},
		{
			name:     "Java detected, tags contain java",
			detected: engine.Result{Engine: engine.Java},
			tags:     []string{"java", "adult"},
			want:     true,
		},
		// Unknown engine (not in engineTagVariants) → engineMatchesTags returns true
		{
			name:     "Unknown engine with tags — no known variants, inconclusive",
			detected: engine.Result{Engine: "Unknown"},
			tags:     []string{"unity"},
			want:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engineMatchesTags(tt.detected, tt.tags)
			if got != tt.want {
				t.Errorf("engineMatchesTags(%v, %v) = %v, want %v",
					tt.detected, tt.tags, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// findF95Engine
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
			got := findF95Engine(tt.game)
			if got != tt.want {
				t.Errorf("findF95Engine(%+v) = %q, want %q", tt.game, got, tt.want)
			}
		})
	}
}

func TestFindF95Engine_FromTitle(t *testing.T) {
	t.Parallel()
	// findF95Engine falls back to checking the title prefix when tags
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
			got := findF95Engine(tt.game)
			if got != tt.want {
				t.Errorf("findF95Engine(%+v) = %q, want %q", tt.game, got, tt.want)
			}
		})
	}
}

func TestFindF95Engine_TagsTakePriority(t *testing.T) {
	t.Parallel()
	// Tags should be checked before title.  Even if the title has an
	// engine prefix, tags that don't contain any engine info should
	// cause findF95Engine to fall through to the title check.
	game := db.Game{
		Tags:  []string{"completed", "adult"},
		Title: "Unity My Game",
	}
	got := findF95Engine(game)
	if got != "Unity" {
		t.Errorf("findF95Engine should fall back to title when tags have no engine, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// engineTagVariants — sanity checks
// ---------------------------------------------------------------------------

func TestEngineTagVariants_AllEnginesPresent(t *testing.T) {
	t.Parallel()
	// Every canonical engine name that appears in the codebase should
	// have at least one tag variant defined.  This test catches new
	// engines added to the detector but not to the tag variant map.
	// Use engine.AllEngines() to derive the list dynamically, skipping
	// Others because it's a catch-all rather than a specific engine.
	for _, eng := range engine.AllEngines() {
		if eng == engine.Others {
			continue
		}
		key := string(eng)
		if len(engineTagVariants[key]) == 0 {
			t.Errorf("engineTagVariants missing entry for %q", key)
		}
	}
}

func TestEngineTagVariants_NoEmptyVariants(t *testing.T) {
	t.Parallel()
	for eng, variants := range engineTagVariants {
		if len(variants) == 0 {
			t.Errorf("engineTagVariants[%q] has empty variants list", eng)
		}
		for _, v := range variants {
			if v == "" {
				t.Errorf("engineTagVariants[%q] contains empty variant string", eng)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// engineCompat — sanity checks
// ---------------------------------------------------------------------------

func TestEngineCompat_SelfCompatible(t *testing.T) {
	t.Parallel()
	// Every engine in the compat map should be compatible with itself.
	for eng, compat := range engineCompat {
		if !compat[eng] {
			t.Errorf("engineCompat[%q] should be self-compatible but isn't", eng)
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
		aCompat := engineCompat[p.a]
		bCompat := engineCompat[p.b]
		if aCompat == nil || !aCompat[p.b] {
			t.Errorf("expected engineCompat[%q][%q] = true", p.a, p.b)
		}
		if bCompat == nil || !bCompat[p.a] {
			t.Errorf("expected engineCompat[%q][%q] = true (symmetric pair)", p.b, p.a)
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
		if compat, ok := engineCompat[p.a]; !ok || !compat[p.b] {
			t.Errorf("expected engineCompat[%q][%q] = true, got false or missing", p.a, p.b)
		}
	}

	// WolfRPG→HTML is one-directional: WolfRPG uses HTML internally,
	// but not all HTML games are WolfRPG.
	t.Run("WolfRPG→HTML is one-directional", func(t *testing.T) {
		t.Parallel()
		htmlCompat := engineCompat["HTML"]
		if htmlCompat != nil && htmlCompat["WolfRPG"] {
			t.Error("HTML should NOT be compatible with WolfRPG — it's one-directional")
		}
	})
}

// ---------------------------------------------------------------------------
// formatTagsBrief
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
			got := formatTagsBrief(tt.tags, tt.max)
			if got != tt.want {
				t.Errorf("formatTagsBrief(%v, %d) = %q, want %q", tt.tags, tt.max, got, tt.want)
			}
		})
	}
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
