package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/saves"
)

// ---------------------------------------------------------------------------
// savesVersionWarn
// ---------------------------------------------------------------------------

func TestSavesVersionWarn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		setVersion  string
		gameVersion string
		wantWarn    bool
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
			got := savesVersionWarn(tt.setVersion, tt.gameVersion)
			if tt.wantWarn {
				for _, want := range []string{tt.setVersion, tt.gameVersion, "may not load"} {
					if !strings.Contains(got, want) {
						t.Errorf("warning %q missing %q", got, want)
					}
				}
				return
			}
			if got != "" {
				t.Errorf("savesVersionWarn(%q, %q) = %q, want empty", tt.setVersion, tt.gameVersion, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// savesApplyNotice
// ---------------------------------------------------------------------------

func TestSavesApplyNotice(t *testing.T) {
	t.Parallel()

	if got := savesApplyNotice("Applied", nil); got != "Applied save set" {
		t.Errorf("nil result = %q, want %q", got, "Applied save set")
	}

	res := &saves.ApplyResult{
		Set:          db.SaveSet{Name: "100-percent", Label: "100%"},
		Target:       saves.Location{Path: "/live/saves"},
		FilesCopied:  4,
		AutoSnapshot: "mine-20261012-120000",
		Replaced:     true,
		FilesRemoved: 2,
	}
	got := savesApplyNotice("Applied", res)
	for _, want := range []string{
		"Applied", "100-percent (100%)", "/live/saves", "4 files",
		"auto-snapshotted your saves as mine-20261012-120000",
		"replaced live saves (2 files removed)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("notice %q missing %q", got, want)
		}
	}

	// A plain restore: no auto-snapshot, no replace, and the label is the
	// name, so it is not repeated.
	res = &saves.ApplyResult{
		Set:    db.SaveSet{Name: "mine-1", Label: "mine-1"},
		Target: saves.Location{Path: "/live/saves"},
	}
	if got := savesApplyNotice("Restored", res); got != "Restored mine-1 to /live/saves (0 files)" {
		t.Errorf("restore notice = %q", got)
	}
}

// ---------------------------------------------------------------------------
// savesSetsToDTO
// ---------------------------------------------------------------------------

func TestSavesSetsToDTO(t *testing.T) {
	t.Parallel()

	created := time.Date(2026, 10, 12, 14, 30, 0, 0, time.UTC)
	sets := savesSetsToDTO([]db.SaveSet{
		{
			Name:        "100-percent",
			Label:       "100% save",
			Source:      "import",
			Active:      true,
			GameVersion: "0.9",
			SizeBytes:   2048,
			FileCount:   3,
			CreatedAt:   created,
		},
		{Name: "mine-1"},
	})
	if len(sets) != 2 {
		t.Fatalf("sets = %d, want 2", len(sets))
	}
	if sets[0].Name != "100-percent" || sets[0].Source != "import" || !sets[0].Active ||
		sets[0].Files != 3 || sets[0].Bytes != 2048 || !sets[0].CreatedAt.Equal(created) {
		t.Errorf("set[0] = %+v", sets[0])
	}
	if sets[1].Name != "mine-1" || sets[1].Active || sets[1].Files != 0 {
		t.Errorf("set[1] = %+v", sets[1])
	}
}

// ---------------------------------------------------------------------------
// Live-location helpers
// ---------------------------------------------------------------------------

func TestSavesLiveFilesAndExists(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"slot1.save", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "slot2.save"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// KindFiles counts only the glob-matched files directly inside the path:
	// the location's globs are the engine's own patterns.
	filesLoc := saves.Location{Kind: saves.KindFiles, Path: dir, Globs: []string{"*.save"}}
	if got := savesLiveFiles(filesLoc); got != 1 {
		t.Errorf("KindFiles live files = %d, want 1", got)
	}
	if !savesLiveExists(filesLoc) {
		t.Error("existing live dir reported as missing")
	}

	// KindDir counts the whole tree.
	dirLoc := saves.Location{Kind: saves.KindDir, Path: dir}
	if got := savesLiveFiles(dirLoc); got != 3 {
		t.Errorf("KindDir live files = %d, want 3", got)
	}

	// An unknown location has no files and does not exist.
	if got := savesLiveFiles(saves.Location{}); got != 0 {
		t.Errorf("unknown location live files = %d, want 0", got)
	}
	if savesLiveExists(saves.Location{}) {
		t.Error("unknown location reported as existing")
	}
	if savesLiveExists(saves.Location{Kind: saves.KindDir, Path: filepath.Join(dir, "absent")}) {
		t.Error("missing dir reported as existing")
	}
}

// ---------------------------------------------------------------------------
// savesSizeLabel
// ---------------------------------------------------------------------------

func TestSavesSizeLabel(t *testing.T) {
	t.Parallel()

	if got := savesSizeLabel(0); got != "0 B" {
		t.Errorf("savesSizeLabel(0) = %q, want %q", got, "0 B")
	}
	if got := savesSizeLabel(2048); got == "" {
		t.Error("savesSizeLabel(2048) is empty")
	}
}
