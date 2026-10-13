// Package saves manages per-game save sets: named copies of a game's save
// data that can be snapshotted, imported, applied, and restored.
//
// Sets are stored under a root directory (see config.DefaultSavesDir) with
// metadata in the save_sets table (internal/db). Live save locations for
// supported engines are resolved by paths.go.
//
// Typical lifecycle: import or snapshot creates a set; apply copies a set's
// contents into the game's live save location; restore re-applies the
// player's own (mine-*) set.
package saves

import (
	"fmt"
	"os"

	"github.com/mili/moxie/internal/db"
)

// LocationKind describes how a save location is structured on disk.
type LocationKind string

const (
	// KindDir means the location's directory contents are the game's save
	// data (e.g. Ren'Py game/saves/, RPG Maker MV www/save/).
	KindDir LocationKind = "dir"

	// KindFiles means only files matching Globs within Path are save data
	// (e.g. RPG Maker VX Ace Save*.rvdata2 files in the game directory).
	KindFiles LocationKind = "files"
)

// Location is a candidate or resolved live save location for a single game.
type Location struct {
	Kind       LocationKind // KindDir or KindFiles
	Path       string       // directory holding the save data
	Globs      []string     // KindFiles only: patterns relative to Path
	Confidence float64      // 0..1, higher = more likely the game's real save data
	Note       string       // short provenance, e.g. "portable saves dir", "wine prefix"
}

// Engine is the entry point for save-set operations. Construct one with
// NewEngine and reuse it for the lifetime of a command or TUI session.
type Engine struct {
	db   *db.Database
	root string // root directory holding per-game set directories
}

// NewEngine returns an Engine that stores set data under rootDir and records
// set metadata in database. The root directory is created if missing.
func NewEngine(database *db.Database, rootDir string) (*Engine, error) {
	if database == nil {
		return nil, fmt.Errorf("saves: database is required")
	}
	if rootDir == "" {
		return nil, fmt.Errorf("saves: storage root is required")
	}
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return nil, fmt.Errorf("saves: create storage root: %w", err)
	}
	return &Engine{db: database, root: rootDir}, nil
}

// DB returns the database handle backing the engine.
func (e *Engine) DB() *db.Database { return e.db }

// Root returns the storage root directory for save sets.
func (e *Engine) Root() string { return e.root }
