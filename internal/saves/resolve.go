package saves

import (
	"fmt"

	"github.com/mili/moxie/internal/db"
)

// ResolveLocation resolves the live save location for game, combining the
// database-stored manual override with the engine-aware candidates discovered
// by paths.go.
//
// It reads save_configs (the manual_path override always wins) and returns the
// chosen Location plus the full candidate list. When nothing resolves — an
// unsupported engine, or a supported one with no discoverable save data — it
// returns a zero Location and the candidate list; when several candidates are
// equally plausible the Location is likewise zero, because ambiguity is never
// guessed (see PickLocation). A non-nil error is returned only for database
// failures.
//
// Resolution is read-only: the resolved_path cache is deliberately not written
// in v1 so a lookup never mutates state.
func (e *Engine) ResolveLocation(game *db.Game) (Location, []Location, error) {
	if e == nil || e.db == nil {
		return Location{}, nil, fmt.Errorf("saves: engine is not initialized")
	}
	if game == nil {
		return Location{}, nil, fmt.Errorf("saves: game is required")
	}

	manual := ""
	cfg, err := e.db.GetSaveConfig(game.ID)
	if err != nil {
		return Location{}, nil, fmt.Errorf("saves: read save config: %w", err)
	}
	if cfg != nil {
		manual = cfg.ManualPath
	}

	candidates := ResolveCandidates(game)
	resolved, _ := PickLocation(candidates, manual)
	return resolved, candidates, nil
}

// SetManualPath pins the game's live save location to path, overriding engine
// auto-detection. Any previously cached resolved_path is preserved.
func (e *Engine) SetManualPath(game *db.Game, path string) error {
	if e == nil || e.db == nil {
		return fmt.Errorf("saves: engine is not initialized")
	}
	if game == nil {
		return fmt.Errorf("saves: game is required")
	}

	cfg, err := e.db.GetSaveConfig(game.ID)
	if err != nil {
		return fmt.Errorf("saves: read save config: %w", err)
	}
	if cfg == nil {
		cfg = &db.SaveConfig{GameID: game.ID}
	}
	cfg.ManualPath = path
	if err := e.db.UpsertSaveConfig(cfg); err != nil {
		return fmt.Errorf("saves: save manual path: %w", err)
	}
	return nil
}

// ClearManualPath removes the game's manual save-location override, reverting
// to engine auto-detection. It is a no-op when the game has no config row; the
// row itself may remain with a NULL manual_path.
func (e *Engine) ClearManualPath(game *db.Game) error {
	if e == nil || e.db == nil {
		return fmt.Errorf("saves: engine is not initialized")
	}
	if game == nil {
		return fmt.Errorf("saves: game is required")
	}

	cfg, err := e.db.GetSaveConfig(game.ID)
	if err != nil {
		return fmt.Errorf("saves: read save config: %w", err)
	}
	if cfg == nil {
		return nil
	}
	cfg.ManualPath = ""
	if err := e.db.UpsertSaveConfig(cfg); err != nil {
		return fmt.Errorf("saves: clear manual path: %w", err)
	}
	return nil
}
