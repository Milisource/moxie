package version

import "testing"

func TestNormalize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		// Behaviour inherited from commands.NormalizeVersion.
		{"v1.0", "1"},
		{"V1.0.0", "1"},
		{"1.0.3", "1.0.3"},
		{"0.13.4", "0.13.4"},
		{"", ""},
		{"v0.5", "0.5"},
		{"1.0.0.0", "1"},
		{"  v1.0  ", "1"},
		{"0.0.0", "0"},

		// Separator collapsing — versions recovered from directory names.
		{"0_8_1", "0.8.1"},
		{"v1_2_3", "1.2.3"},
		{"0-8-1", "0.8.1"},
		{"1 0 2", "1.0.2"},

		// Case folding and build letters.
		{"V1.5B", "1.5b"},
		{"Final", "final"},

		// Dates keep their dashes so they stay distinguishable.
		{"2018-07-18", "2018-07-18"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := Normalize(tt.input); got != tt.want {
				t.Errorf("Normalize(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCompare(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		remote string
		known  string
		want   Diff
	}{
		// Equivalence across formatting differences.
		{"identical", "0.8.1", "0.8.1", Same},
		{"v prefix", "v0.8.1", "0.8.1", Same},
		{"trailing zeros", "1.0.0", "1", Same},
		{"underscores from dir name", "0.8.1", "0_8_1", Same},
		{"case", "V1.5B", "v1.5b", Same},

		// Real updates.
		{"minor bump", "0.9", "0.8", Newer},
		{"patch bump", "0.8.2", "0.8.1", Newer},
		{"added patch segment", "0.8.1", "0.8", Newer},
		{"major bump", "2.0", "1.9.9", Newer},
		{"double digit", "0.12.0", "0.9", Newer},
		{"build letter", "1.5b", "1.5a", Newer},
		{"chapter bump", "Ch.5 Free", "Ch.4 Free", Newer},
		{"reached final", "Final", "0.9", Newer},
		{"newer date", "2019-01-02", "2018-07-18", Newer},

		// Regressions — the case the old equality check reported as updates.
		{"older minor", "0.8", "0.9", Older},
		{"dropped patch segment", "0.8", "0.8.1", Older},
		{"older date", "2018-07-18", "2019-01-02", Older},
		{"older build letter", "1.5a", "1.5b", Older},

		// Genuinely incomparable.
		{"date replaces number", "2018-07-18", "0.9", Changed},
		{"number replaces date", "0.9", "2018-07-18", Changed},
		{"final regresses", "0.9", "Final", Changed},
		{"suffix only", "0.8 extra", "0.8", Changed},
		{"non numeric", "Beta", "Alpha", Changed},
		// One-sided build letters are hotfix bumps: "0.8.1" → "0.8.1b".
		{"hotfix bump", "0.8.1b", "0.8.1", Newer},
		{"hotfix older", "0.8.1", "0.8.1b", Older},
		// Chapter-style versions order among themselves but never against
		// plain numerics ("Ch.4 Free" must not beat "0.12.0").
		{"chapter bump", "Ch.5 Free", "Ch.4 Free", Newer},
		{"chapter vs numeric", "Ch.4 Free", "0.12.0", Changed},
		{"numeric vs chapter", "0.12.0", "Ch.4 Free", Changed},
		{"remote empty", "", "0.8", Changed},
		{"known empty", "0.8", "", Changed},
		{"both empty", "", "", Same},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Compare(tt.remote, tt.known); got != tt.want {
				t.Errorf("Compare(%q, %q) = %v, want %v", tt.remote, tt.known, got, tt.want)
			}
		})
	}
}

func TestIsNewer(t *testing.T) {
	t.Parallel()
	if !IsNewer("0.9", "0.8") {
		t.Error("IsNewer(0.9, 0.8) = false, want true")
	}
	// The key regression guard: a differing-but-not-newer version must not
	// be reported as an available update.
	if IsNewer("0.8", "0.9") {
		t.Error("IsNewer(0.8, 0.9) = true, want false")
	}
	if IsNewer("0.8", "0.8") {
		t.Error("IsNewer(0.8, 0.8) = true, want false")
	}
	if IsNewer("2018-07-18", "0.9") {
		t.Error("IsNewer on incomparable kinds = true, want false")
	}
}
