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
	}{
		{"thread version stands in without a payload version", "1.0", "1.1", "Game.zip", "1.1"},
		{"payload newer than thread", "1.0", "1.1", "Game_v1.2_Win.zip", "1.2"},
		{"payload lags the thread", "0.4", "0.5", "Game_v0.4.5_Win.zip", "0.4.5"},
		{"payload equals the thread release", "1.0", "1.1", "Game_v1.1.0.zip", "1.1"},
		{"payload older than current never downgrades", "1.2", "1.3", "Game_v1.1_Win.zip", "1.2"},
		{"reverted thread never downgrades", "1.2", "1.1", "Game.zip", "1.2"},
		{"same release is a no-op", "1.0.0", "1.0", "Game.zip", "1.0.0"},
		{"manual install uses archive name", "0.9", "", "Game_v1.0.zip", "1.0"},
		{"nothing usable", "1.0", "", "download.zip", "1.0"},
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
			}
			if _, err := database.InsertGame(game); err != nil {
				t.Fatalf("insert: %v", err)
			}

			got, err := recordInstalledVersion(database, game, tt.archive)
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
			if tt.want != tt.current && stored.VersionSource != "f95" {
				t.Errorf("version_source = %q, want f95", stored.VersionSource)
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

	if _, err := recordInstalledVersion(database, game, "Game_v1.1.zip"); err != nil {
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
