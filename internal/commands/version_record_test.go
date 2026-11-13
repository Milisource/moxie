package commands

import (
	"testing"

	"github.com/mili/moxie/internal/db"
)

func TestRecordInstalledVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		current string
		latest  string
		archive string
		want    string
		patch   bool
	}{
		{"thread version stands in without a payload version", "1.0", "1.1", "Game.zip", "1.1", false},
		{"payload newer than thread", "1.0", "1.1", "Game_v1.2_Win.zip", "1.2", false},
		// A payload build ahead of the recorded version is journalled even
		// when it trails the thread: only the payload's build is on disk.
		{"payload lags the thread", "0.4", "0.5", "Game_v0.4.5_Win.zip", "0.4.5", false},
		{"payload equals the thread release", "1.0", "1.1", "Game_v1.1.0.zip", "1.1", false},
		{"payload older than current never downgrades", "1.2", "1.3", "Game_v1.1_Win.zip", "1.2", false},
		{"reverted thread never downgrades", "1.2", "1.1", "Game.zip", "1.2", false},
		// "1.0" and "1.0.0" are the same release once missing segments pad
		// with zero, so the recorded spelling stays unchanged.
		{"same release is a no-op", "1.0.0", "1.0", "Game.zip", "1.0.0", false},
		{"manual install uses archive name", "0.9", "", "Game_v1.0.zip", "1.0", false},
		{"nothing usable", "1.0", "", "download.zip", "1.0", false},
		{"same payload records final qualifier", "1.0", "1.0 Final", "Game-1.0.zip", "1.0 Final", false},
		// A file named after the installed version is not evidence that the
		// thread's newer build is on disk.
		{"same payload does not adopt a newer thread", "1.0", "1.5", "Game-1.0.zip", "1.0", false},
		{"same payload does not adopt an unorderable thread", "1.0", "2026-10-01", "Game-1.0.zip", "1.0", false},
		{"same payload unchanged thread", "1.0", "1.0", "Game-1.0.zip", "1.0", false},
		{"patch named after its base adopts the thread", "1.0", "1.1", "Game-1.0.zip", "1.1", true},
		{"patch never downgrades", "1.2", "1.3", "Game-1.1.zip", "1.2", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			database := setupTestDB(t)
			defer database.Close()

			game := &db.Game{
				Title:         tt.name,
				Engine:        "RenPy",
				Path:          "/games/" + tt.name,
				Status:        "active",
				Version:       tt.current,
				LatestVersion: tt.latest,
				VersionSource: "scanner",
			}
			if _, err := database.InsertGame(game); err != nil {
				t.Fatalf("insert: %v", err)
			}

			got, err := RecordInstalledVersion(database, game, tt.archive, tt.patch)
			if err != nil {
				t.Fatalf("recordInstalledVersion: %v", err)
			}
			if got != tt.want {
				t.Errorf("returned %q, want %q", got, tt.want)
			}
			if game.Version != tt.want {
				t.Errorf("game.Version = %q, want %q", game.Version, tt.want)
			}

			// The database must agree, or the next check re-queues the game.
			stored, err := database.GetGame(game.ID)
			if err != nil {
				t.Fatalf("GetGame: %v", err)
			}
			if stored.Version != tt.want {
				t.Errorf("stored version = %q, want %q", stored.Version, tt.want)
			}
			wantSource := "scanner"
			if tt.want != tt.current {
				wantSource = "f95"
			}
			if stored.VersionSource != wantSource {
				t.Errorf("version_source = %q, want %q", stored.VersionSource, wantSource)
			}
		})
	}
}

func TestRecordInstalledVersion_ClearsQueue(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	game := &db.Game{
		Title: "Looping Game", Engine: "RenPy", Path: "/games/looping",
		Status: "active", Version: "1.0", LatestVersion: "1.1",
	}
	if _, err := database.InsertGame(game); err != nil {
		t.Fatal(err)
	}
	pending, err := database.GamesNeedingUpdate()
	if err != nil || len(pending) != 1 {
		t.Fatalf("expected one pending update, got %d (err %v)", len(pending), err)
	}

	if _, err := RecordInstalledVersion(database, game, "Game_v1.1.zip", false); err != nil {
		t.Fatal(err)
	}

	pending, err = database.GamesNeedingUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("game still queued after recording the installed version: %+v", pending)
	}
}

func TestRecordedVersion(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, current, latest, archive, want string
		patch                                bool
	}{
		{"same payload records final qualifier", "1.0", "1.0 Final", "Game-1.0.zip", "1.0 Final", false},
		{"older payload preserves installed", "1.2", "1.3", "Game-1.1.zip", "1.2", false},
		{"same payload unchanged thread", "1.0", "1.0", "Game-1.0.zip", "1.0", false},
		{"new payload keeps thread spelling", "1.0", "v1.1", "Game-1.1.0.zip", "v1.1", false},
		{"payload ahead of thread", "1.0", "1.1", "Game-1.2.zip", "1.2", false},
		{"no payload uses newer thread", "1.0", "1.1", "Game.zip", "1.1", false},
		{"no archive uses newer thread", "1.0", "1.1", "", "1.1", false},
		{"empty result preserves installed", "1.0", "", "", "1.0", false},
		{"empty inputs", "", "", "", "", false},
		// A first install has nothing to compare against, so the payload (or
		// the thread release when the filename has no version) is recorded.
		{"first install uses payload version", "", "1.5", "Game-1.4.zip", "1.4", false},
		{"first install keeps thread spelling", "", "1.5", "Game-1.5.0.zip", "1.5", false},
		{"first install falls back to thread", "", "1.5", "", "1.5", false},
		{"unorderable payload preserves installed", "2026-10-01", "1.3", "Game-1.2.zip", "2026-10-01", false},
		{"patch adopts the thread target", "1.0", "1.1", "Game-1.0.zip", "1.1", true},
		{"full build does not adopt a newer thread", "1.0", "1.5", "Game-1.0.zip", "1.0", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := RecordedVersion(tt.current, tt.latest, tt.archive, tt.patch); got != tt.want {
				t.Errorf("RecordedVersion(%q, %q, %q, patch=%v) = %q, want %q", tt.current, tt.latest, tt.archive, tt.patch, got, tt.want)
			}
		})
	}
}

func TestRecordInstalledVersion_FinalClearsQueue(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()
	game := &db.Game{
		Title: "Final release", Engine: "RenPy", Path: "/games/final",
		Status: "active", Version: "1.0", LatestVersion: "1.0 Final", VersionSource: "scanner",
	}
	if _, err := database.InsertGame(game); err != nil {
		t.Fatal(err)
	}
	pending, err := database.GamesNeedingUpdate()
	if err != nil || len(pending) != 1 {
		t.Fatalf("expected one pending update, got %d (err %v)", len(pending), err)
	}
	if got, err := RecordInstalledVersion(database, game, "Game-1.0.zip", false); err != nil || got != "1.0 Final" {
		t.Fatalf("recordInstalledVersion = %q, %v; want 1.0 Final", got, err)
	}
	pending, err = database.GamesNeedingUpdate()
	if err != nil || len(pending) != 0 {
		t.Fatalf("expected no pending update after final recording, got %d (err %v)", len(pending), err)
	}
}
