package commands

import (
	"bytes"
	"flag"
	"strings"
	"testing"
	"time"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/saves"
)

// ---------------------------------------------------------------------------
// parseSavesArgs — interleaved flags
// ---------------------------------------------------------------------------

// TestParseSavesArgs_ApplyReplaceFlag pins the interleaved surface of
// `saves apply <id> <set> [--replace]`: the flag is honored before, between,
// and after the positionals.
func TestParseSavesArgs_ApplyReplaceFlag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		args        []string
		wantReplace bool
		want        []string
	}{
		{"flag before positionals", []string{"--replace", "42", "100-percent"}, true, []string{"42", "100-percent"}},
		{"flag between positionals", []string{"42", "--replace", "100-percent"}, true, []string{"42", "100-percent"}},
		{"flag after positionals", []string{"42", "100-percent", "--replace"}, true, []string{"42", "100-percent"}},
		{"absent", []string{"42", "100-percent"}, false, []string{"42", "100-percent"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := flag.NewFlagSet("saves-apply", flag.ExitOnError)
			replace := fs.Bool("replace", false, "replace")
			got := parseSavesArgs(fs, tt.args)
			if *replace != tt.wantReplace {
				t.Errorf("replace = %v, want %v", *replace, tt.wantReplace)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("positionals = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("positionals[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Version-mismatch helpers
// ---------------------------------------------------------------------------

func TestSavesVersionMismatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		setVersion  string
		gameVersion string
		want        bool
	}{
		{"identical", "1.0", "1.0", false},
		{"v prefix and trailing zero", "v1.0.0", "1.0", false},
		{"separator form", "0_8_1", "0.8.1", false},
		{"differing", "0.9", "1.0", true},
		{"set version unknown", "", "1.0", false},
		{"game version unknown", "1.0", "", false},
		{"both unknown", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := savesVersionMismatch(tt.setVersion, tt.gameVersion); got != tt.want {
				t.Errorf("savesVersionMismatch(%q, %q) = %v, want %v",
					tt.setVersion, tt.gameVersion, got, tt.want)
			}
		})
	}
}

func TestSavesVersionWarning(t *testing.T) {
	t.Parallel()

	if got := savesVersionWarning("1.0", &db.Game{Version: "1.0"}); got != "" {
		t.Errorf("matching versions = %q, want empty", got)
	}
	if got := savesVersionWarning("0.9", nil); got != "" {
		t.Errorf("nil game = %q, want empty", got)
	}

	got := savesVersionWarning("0.9", &db.Game{Version: "1.0"})
	for _, want := range []string{"0.9", "1.0", "may not load"} {
		if !strings.Contains(got, want) {
			t.Errorf("warning %q missing %q", got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// printScanCandidates
// ---------------------------------------------------------------------------

func TestPrintScanCandidates(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	printScanCandidates(&buf, &db.Game{ID: 42, Title: "My Game"}, []saves.ScanCandidate{
		{
			Path:       "/live/renpy/saves",
			Files:      3,
			NewestFile: "slot1.save",
			NewestMod:  time.Date(2026, 10, 12, 14, 30, 0, 0, time.UTC),
		},
		{Path: "/live/other", Files: 0},
	})

	out := buf.String()
	for _, want := range []string{
		"DIR", "FILES", "NEWEST", "MODIFIED",
		"/live/renpy/saves", "3", "slot1.save", "2026-10-12 14:30",
		"/live/other",
		"moxie saves path 42 --set /live/renpy/saves",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestPrintScanCandidatesEmpty(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	printScanCandidates(&buf, &db.Game{Title: "My Game"}, nil)

	if got := buf.String(); !strings.Contains(got, "No save-like files found for My Game") {
		t.Errorf("empty output = %q", got)
	}
}
