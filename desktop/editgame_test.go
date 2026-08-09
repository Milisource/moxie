package main

import (
	"testing"

	"github.com/mili/moxie/internal/db"
)

// A nil field means "unchanged" — the existing value must survive.
func TestEditGameNilFieldsLeaveUnchanged(t *testing.T) {
	a := newTestApp(t)
	id := addGame(t, a, "Test Game", "/games/test-game")
	if err := a.db.UpdateGame(&db.Game{
		ID:      id,
		Title:   "Test Game",
		Path:    "/games/test-game",
		Engine:  "RenPy",
		Version: "1.0",
		ExePath: "/games/test-game/game.exe",
		Notes:   "old note",
		Status:  "active",
	}); err != nil {
		t.Fatalf("UpdateGame: %v", err)
	}

	err := a.EditGame(id, EditGameFields{})
	if err != nil {
		t.Fatalf("EditGame with nil fields: %v", err)
	}

	g, err := a.db.GetGame(id)
	if err != nil || g == nil {
		t.Fatalf("GetGame: %v", err)
	}
	if g.Engine != "RenPy" || g.Version != "1.0" || g.ExePath != "/games/test-game/game.exe" || g.Notes != "old note" {
		t.Errorf("nil fields changed values: %+v", g)
	}
}

// An empty string explicitly clears the field — the whole point of the
// nullable contract.
func TestEditGameEmptyStringClearsField(t *testing.T) {
	a := newTestApp(t)
	id := addGame(t, a, "Test Game", "/games/test-game")
	if err := a.db.UpdateGame(&db.Game{
		ID:      id,
		Title:   "Test Game",
		Path:    "/games/test-game",
		Engine:  "RenPy",
		Version: "1.0",
		ExePath: "/games/test-game/game.exe",
		Notes:   "stale note",
		Status:  "active",
	}); err != nil {
		t.Fatalf("UpdateGame: %v", err)
	}

	empty := ""
	err := a.EditGame(id, EditGameFields{ExePath: &empty, Notes: &empty})
	if err != nil {
		t.Fatalf("EditGame: %v", err)
	}

	g, err := a.db.GetGame(id)
	if err != nil || g == nil {
		t.Fatalf("GetGame: %v", err)
	}
	if g.ExePath != "" {
		t.Errorf("ExePath = %q, want cleared", g.ExePath)
	}
	if g.Notes != "" {
		t.Errorf("Notes = %q, want cleared", g.Notes)
	}
	if g.Engine != "RenPy" || g.Version != "1.0" {
		t.Errorf("unrelated fields changed: %+v", g)
	}
}

// An engine-only edit must not wipe the other fields — the regression the
// old empty-string contract made possible.
func TestEditGameEngineOnlyEditKeepsOtherFields(t *testing.T) {
	a := newTestApp(t)
	id := addGame(t, a, "Test Game", "/games/test-game")
	if err := a.db.UpdateGame(&db.Game{
		ID:      id,
		Title:   "Test Game",
		Path:    "/games/test-game",
		Engine:  "RenPy",
		Version: "1.0",
		ExePath: "/games/test-game/game.exe",
		Notes:   "note",
		Status:  "active",
	}); err != nil {
		t.Fatalf("UpdateGame: %v", err)
	}

	flash := "Flash"
	err := a.EditGame(id, EditGameFields{Engine: &flash})
	if err != nil {
		t.Fatalf("EditGame: %v", err)
	}

	g, err := a.db.GetGame(id)
	if err != nil || g == nil {
		t.Fatalf("GetGame: %v", err)
	}
	if g.Engine != "Flash" {
		t.Errorf("Engine = %q, want Flash", g.Engine)
	}
	if g.Version != "1.0" || g.ExePath != "/games/test-game/game.exe" || g.Notes != "note" {
		t.Errorf("engine-only edit wiped other fields: %+v", g)
	}
}

func TestEditGameMissingGame(t *testing.T) {
	a := newTestApp(t)
	val := "x"
	if err := a.EditGame(99999, EditGameFields{Engine: &val}); err == nil {
		t.Error("EditGame on missing game: expected error, got nil")
	}
}
