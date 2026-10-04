package commands

import (
	"testing"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/engine"
	"github.com/mili/moxie/internal/scanner"
)

// A collapsed release wrapper must adopt the existing inner-directory row
// (preserving curation) instead of inserting a duplicate.
func TestUpsertDetected_MergesCollapsedWrapper(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	if _, err := d.InsertGame(&db.Game{
		Title:        "Brothel King",
		Path:         "/root/Brothel King/Brothel King",
		Engine:       "RenPy",
		EngineSource: "scanner",
		Status:       "completed",
		Tags:         []string{"favorite"},
	}); err != nil {
		t.Fatal(err)
	}

	detected := []scanner.DetectedGame{{
		Title:   "Brothel King",
		Path:    "/root/Brothel King",
		Engine:  engine.RenPy,
		Version: "1.0",
	}}

	inserted, updated, errs := UpsertDetected(d, detected, false)
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	if inserted != 0 || updated != 1 {
		t.Fatalf("inserted=%d updated=%d, want 0/1", inserted, updated)
	}

	moved, err := d.GetGameByPath("/root/Brothel King")
	if err != nil || moved == nil {
		t.Fatalf("wrapper row not found: %v", err)
	}
	if moved.Status != "completed" {
		t.Errorf("status = %q, want preserved completed", moved.Status)
	}
	if len(moved.Tags) != 1 || moved.Tags[0] != "favorite" {
		t.Errorf("tags = %v, want preserved", moved.Tags)
	}
	if moved.Version != "1.0" {
		t.Errorf("version = %q, want detected 1.0", moved.Version)
	}

	if old, _ := d.GetGameByPath("/root/Brothel King/Brothel King"); old != nil {
		t.Errorf("inner row still present; expected it merged into the wrapper")
	}
}

// PruneSuperseded removes stale container/download rows under the root while
// preserving detected games and rows outside the root.
func TestPruneSuperseded_RemovesStaleRows(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	mk := func(title, path string) {
		if _, err := d.InsertGame(&db.Game{Title: title, Path: path, Engine: "Others", Status: "active"}); err != nil {
			t.Fatal(err)
		}
	}
	mk("Container", "/root/Container")  // ancestor of a detected child
	mk("Rec", "/root/downloads/REC")    // under downloads/
	mk("RealGame", "/root/RealGame")    // detected — keep
	mk("Outside", "/elsewhere/Outside") // outside root — keep

	detected := []scanner.DetectedGame{
		{Title: "RealGame", Path: "/root/RealGame", Engine: engine.Unity},
		{Title: "Child", Path: "/root/Container/Child", Engine: engine.Unity},
	}

	pruned := PruneSuperseded(d, "/root", detected)
	if pruned != 2 {
		t.Errorf("pruned = %d, want 2 (Container + downloads/REC)", pruned)
	}
	for _, p := range []string{"/root/RealGame", "/elsewhere/Outside"} {
		g, err := d.GetGameByPath(p)
		if err != nil || g == nil || !g.DeletedAt.IsZero() {
			t.Errorf("%s should be kept, got %+v (err %v)", p, g, err)
		}
	}
	for _, p := range []string{"/root/Container", "/root/downloads/REC"} {
		g, err := d.GetGameByPath(p)
		if err != nil || g == nil {
			t.Fatalf("%s missing: %v", p, err)
		}
		if g.DeletedAt.IsZero() {
			t.Errorf("%s should be soft-deleted", p)
		}
	}
}
