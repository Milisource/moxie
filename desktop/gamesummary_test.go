package main

import (
	"testing"
	"time"
)

// TestGetGames_PopulatesCreatedAtAndLastPlayed guards the backend-track fix:
// DesktopGameSummary.CreatedAt/LastPlayed used to be zero-value always, so
// the library's recency-first sort and "Recently played" quick view
// (89b6359) only ever worked against the frontend mock's synthetic
// timestamps. See docs/desktop-perf-virtualization-handoff.md's backend
// track note.
func TestGetGames_PopulatesCreatedAtAndLastPlayed(t *testing.T) {
	a := newTestApp(t)
	playedID := addGame(t, a, "Played Game", "/tmp/played-game")
	neverPlayedID := addGame(t, a, "Never Played", "/tmp/never-played")

	if err := a.db.RecordPlay(playedID, "linux"); err != nil {
		t.Fatal(err)
	}

	games, err := a.GetGames()
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 2 {
		t.Fatalf("expected 2 games, got %d", len(games))
	}

	byID := make(map[int64]DesktopGameSummary, len(games))
	for _, g := range games {
		byID[g.ID] = g
	}

	for _, g := range games {
		if g.CreatedAt == "" {
			t.Errorf("game %d: CreatedAt should be populated from db.Game.CreatedAt", g.ID)
		}
	}

	if got := byID[playedID].LastPlayed; got == "" {
		t.Error("played game: LastPlayed should be populated")
	} else if _, err := time.Parse(time.RFC3339, got); err != nil {
		t.Errorf("played game: LastPlayed %q is not RFC3339: %v", got, err)
	}

	if got := byID[neverPlayedID].LastPlayed; got != "" {
		t.Errorf("never-played game: LastPlayed should be empty, got %q", got)
	}
}

// TestGetGameDetail_PopulatesLastPlayed verifies the detail endpoint derives
// LastPlayed from the play history it already loads, rather than needing (or
// skipping) a second query.
func TestGetGameDetail_PopulatesLastPlayed(t *testing.T) {
	a := newTestApp(t)
	id := addGame(t, a, "Detail Game", "/tmp/detail-game")

	detail, err := a.GetGameDetail(id)
	if err != nil {
		t.Fatal(err)
	}
	if detail.LastPlayed != "" {
		t.Errorf("unplayed game: LastPlayed should be empty, got %q", detail.LastPlayed)
	}

	if err := a.db.RecordPlay(id, "linux"); err != nil {
		t.Fatal(err)
	}

	detail, err = a.GetGameDetail(id)
	if err != nil {
		t.Fatal(err)
	}
	if detail.LastPlayed == "" {
		t.Error("played game: LastPlayed should be populated after RecordPlay")
	}
	if len(detail.PlayHistory) != 1 || detail.PlayHistory[0].PlayedAt != detail.LastPlayed {
		t.Errorf("LastPlayed %q should match the most recent PlayHistory entry %+v", detail.LastPlayed, detail.PlayHistory)
	}
}
