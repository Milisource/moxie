package version

import "testing"

func TestFinalMarkerTokens(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		raw  string
		want bool
	}{
		{"Final", true},
		{"1.0 complete", true},
		{"v0.9 [FINAL]", true},
		{"0.3 incomplete", false},
		{"1.0 finale", false},
		{"Completed", true},
		{"1.0Final", true},
		{"Final_1.0", true},
	} {
		t.Run(tt.raw, func(t *testing.T) {
			if got := hasFinalMarker(tt.raw); got != tt.want {
				t.Errorf("hasFinalMarker(%q) = %v, want %v", tt.raw, got, tt.want)
			}
			if got := Parse(tt.raw).Kind == "final"; got != tt.want {
				t.Errorf("Parse(%q).Kind final = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
	if got := Canonical("0.3 incomplete"); got == "Final" {
		t.Errorf("Canonical incomplete = %q, want a non-final version", got)
	}
}

func TestCompareFinalVersions(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		a, b string
		want Diff
	}{
		{"v0.9 Final", "v1.0 Final", Older},
		{"v1.0 Final", "v0.9 Final", Newer},
		// Preserve the existing final-vs-numeric upgrade semantics.
		{"1.0 Final", "1.0", Newer},
		{"1.0 complete", "1.0 Final", Same},
		{"1.0 Final", "1.0.0 Final", Same},
		{"Chapter 2 Final", "Chapter 3 Final", Older},
		{"Chapter 3 Final", "Chapter 2 Final", Newer},
		{"Chapter 2 Final", "Chapter 2 complete", Same},
		// Mixed final shapes without a common number are not orderable; they
		// must not read as "newer" in both directions.
		{"Final", "1.0 Final", Changed},
		{"1.0 Final", "Final", Changed},
	} {
		t.Run(tt.a+" vs "+tt.b, func(t *testing.T) {
			if got := CompareStructured(Parse(tt.a), Parse(tt.b)); got != tt.want {
				t.Errorf("CompareStructured(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
			if got := Compare(tt.a, tt.b); got != tt.want {
				t.Errorf("Compare(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestSameCore(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		a, b string
		want bool
	}{
		{"1.0", "1.0 Final", true},
		{"1.0.0", "v1.0 Final", true},
		{"1.0", "1.5", false},
		{"1.0", "2026-10-01", false},
		{"Final", "1.0 Final", false},
	} {
		if got := SameCore(tt.a, tt.b); got != tt.want {
			t.Errorf("SameCore(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}
