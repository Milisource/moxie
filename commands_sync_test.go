package main

import (
	"testing"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/scraper"
)

// ---------------------------------------------------------------------------
// applyThreadData — version field preservation
// ---------------------------------------------------------------------------

func TestApplyThreadData_VersionPreservation(t *testing.T) {
	t.Parallel()

	// applyThreadData must set LatestVersion from thread data, but NEVER
	// overwrite the locally-scanned Version field.

	t.Run("sets LatestVersion when Version is empty", func(t *testing.T) {
		game := &db.Game{
			Title:         "My Game",
			Version:       "",
			LatestVersion: "",
		}
		data := &scraper.ThreadData{
			Title:   "RPGM My Game [v1.0] [Dev]",
			Version: "1.0",
			ThreadID: 12345,
		}

		applyThreadData(game, data, "https://f95zone.to/threads/my-game.12345/")

		if game.LatestVersion != "1.0" {
			t.Errorf("LatestVersion = %q, want %q", game.LatestVersion, "1.0")
		}
		if game.Version != "" {
			t.Errorf("Version should remain empty, got %q", game.Version)
		}
	})

	t.Run("preserves existing Version when LatestVersion is set", func(t *testing.T) {
		game := &db.Game{
			Title:         "My Game",
			Version:       "0.9",
			LatestVersion: "",
		}
		data := &scraper.ThreadData{
			Title:   "RPGM My Game [v1.0] [Dev]",
			Version: "1.0",
			ThreadID: 12345,
		}

		applyThreadData(game, data, "https://f95zone.to/threads/my-game.12345/")

		if game.LatestVersion != "1.0" {
			t.Errorf("LatestVersion = %q, want %q", game.LatestVersion, "1.0")
		}
		if game.Version != "0.9" {
			t.Errorf("Version should be preserved as %q, got %q", "0.9", game.Version)
		}
	})

	t.Run("does not clear LatestVersion when data.Version is empty", func(t *testing.T) {
		game := &db.Game{
			Title:         "My Game",
			Version:       "0.9",
			LatestVersion: "1.0",
		}
		data := &scraper.ThreadData{
			Title:    "My Game",
			Version:  "", // no version in thread
			ThreadID: 12345,
		}

		applyThreadData(game, data, "https://f95zone.to/threads/my-game.12345/")

		if game.LatestVersion != "1.0" {
			t.Errorf("LatestVersion should be unchanged, got %q", game.LatestVersion)
		}
		if game.Version != "0.9" {
			t.Errorf("Version should be unchanged, got %q", game.Version)
		}
	})
}

// ---------------------------------------------------------------------------
// applyThreadData — engine detection from title prefix
// ---------------------------------------------------------------------------

func TestApplyThreadData_SetsEngineFromF95Prefix(t *testing.T) {
	t.Parallel()

	// When the F95Zone thread title starts with a known engine prefix
	// and the game's engine is empty/Unknown/Others, applyThreadData
	// should set the engine from the prefix.

	t.Run("sets engine from RPGM prefix when engine is empty", func(t *testing.T) {
		game := &db.Game{Title: "Local Name", Engine: ""}
		data := &scraper.ThreadData{
			Title: "RPGM Local Name [v1.0] [Dev]",
		}
		applyThreadData(game, data, "https://f95zone.to/threads/game.12345/")

		if game.Engine != "RPGM" {
			t.Errorf("Engine = %q, want %q", game.Engine, "RPGM")
		}
	})

	t.Run("sets engine from Unity prefix when engine is Unknown", func(t *testing.T) {
		game := &db.Game{Title: "Local Name", Engine: "Unknown"}
		data := &scraper.ThreadData{
			Title: "Unity Local Name [v1.0] [Dev]",
		}
		applyThreadData(game, data, "https://f95zone.to/threads/game.12345/")

		if game.Engine != "Unity" {
			t.Errorf("Engine = %q, want %q", game.Engine, "Unity")
		}
	})

	t.Run("sets engine from RenPy prefix when engine is Others", func(t *testing.T) {
		game := &db.Game{Title: "Local Name", Engine: "Others"}
		data := &scraper.ThreadData{
			Title: "Ren'Py Local Name [v1.0] [Dev]",
		}
		applyThreadData(game, data, "https://f95zone.to/threads/game.12345/")

		if game.Engine != "RenPy" {
			t.Errorf("Engine = %q, want %q", game.Engine, "RenPy")
		}
	})

	t.Run("does NOT overwrite explicitly-set engine", func(t *testing.T) {
		game := &db.Game{Title: "Local Name", Engine: "Unity"}
		data := &scraper.ThreadData{
			Title: "RPGM Local Name [v1.0] [Dev]",
		}
		applyThreadData(game, data, "https://f95zone.to/threads/game.12345/")

		if game.Engine != "Unity" {
			t.Errorf("Engine should remain Unity, got %q", game.Engine)
		}
	})
}

// ---------------------------------------------------------------------------
// applyThreadData — URL, ThreadID, Tags, Status
// ---------------------------------------------------------------------------

func TestApplyThreadData_URLAndThreadID(t *testing.T) {
	t.Parallel()

	game := &db.Game{}
	data := &scraper.ThreadData{
		Title:    "Game Title",
		ThreadID: 54321,
	}
	url := "https://f95zone.to/threads/game-title.54321/"

	applyThreadData(game, data, url)

	if game.F95URL != url {
		t.Errorf("F95URL = %q, want %q", game.F95URL, url)
	}
	if game.F95ThreadID != 54321 {
		t.Errorf("F95ThreadID = %d, want %d", game.F95ThreadID, 54321)
	}
}

func TestApplyThreadData_TagsAndStatus(t *testing.T) {
	t.Parallel()

	game := &db.Game{}
	data := &scraper.ThreadData{
		Title:  "Game Title",
		Tags:   []string{"adult", "rpg", "parody"},
		Status: "completed",
	}
	applyThreadData(game, data, "https://f95zone.to/threads/game.54321/")

	if len(game.Tags) != 3 || game.Tags[0] != "adult" {
		t.Errorf("Tags = %v, want [adult rpg parody]", game.Tags)
	}
	if game.Status != "completed" {
		t.Errorf("Status = %q, want %q", game.Status, "completed")
	}
}

func TestApplyThreadData_StripsTitlePrefix(t *testing.T) {
	t.Parallel()

	game := &db.Game{}
	data := &scraper.ThreadData{
		Title: "RPGM Completed My Game [v1.0] [Dev]",
	}
	applyThreadData(game, data, "https://f95zone.to/threads/my-game.54321/")

	// The title should have the engine/status prefix stripped.
	want := "My Game [v1.0] [Dev]"
	if game.Title != want {
		t.Errorf("Title = %q, want %q", game.Title, want)
	}
}
