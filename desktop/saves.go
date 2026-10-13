package main

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/mili/moxie/internal/config"
	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/saves"
)

// ---------------------------------------------------------------------------
// Saves data types serialized to the frontend
// ---------------------------------------------------------------------------

// DesktopSaveSet is a save set as the Saves view lists it: the user's own
// snapshot, an imported set, or one downloaded from F95Zone.
type DesktopSaveSet struct {
	Name        string    `json:"name"`
	Label       string    `json:"label"`
	Source      string    `json:"source"`
	Active      bool      `json:"active"`
	GameVersion string    `json:"gameVersion"`
	Files       int       `json:"files"`
	Bytes       int64     `json:"bytes"`
	CreatedAt   time.Time `json:"createdAt"`
}

// DesktopSavesState is everything the Saves view renders for one game: its sets
// plus the resolved live save location. The live fields mirror the TUI's
// loadSavesState, which is the single read path both surfaces share.
type DesktopSavesState struct {
	GameID      int64            `json:"gameId"`
	GameTitle   string           `json:"gameTitle"`
	GameVersion string           `json:"gameVersion"`
	LivePath    string           `json:"livePath"`
	LiveNote    string           `json:"liveNote"`
	LiveExists  bool             `json:"liveExists"`
	LiveFiles   int              `json:"liveFiles"`
	ManualPath  string           `json:"manualPath"`
	Sets        []DesktopSaveSet `json:"sets"`
}

// DesktopSaveScanCandidate is a directory that plausibly holds the player's live
// saves, as returned by the save scan.
type DesktopSaveScanCandidate struct {
	Path       string    `json:"path"`
	Files      int       `json:"files"`
	NewestFile string    `json:"newestFile"`
	NewestMod  time.Time `json:"newestMod"`
}

// DesktopGameSavesSummary is one row of the Saves overview: a library game that
// has at least one save set.
type DesktopGameSavesSummary struct {
	GameID    int64  `json:"gameId"`
	Title     string `json:"title"`
	Sets      int    `json:"sets"`
	HasActive bool   `json:"hasActive"`
	LivePath  string `json:"livePath"`
}

// DesktopSaveOpResult is the outcome of a snapshot, apply, restore, or import,
// flattened into the fields the Saves view shows. VersionWarn is "" when the set
// matches the game's version (or either version is unknown).
type DesktopSaveOpResult struct {
	Notice       string `json:"notice"`
	VersionWarn  string `json:"versionWarn"`
	SetName      string `json:"setName"`
	TargetPath   string `json:"targetPath"`
	FilesCopied  int    `json:"filesCopied"`
	FilesRemoved int    `json:"filesRemoved"`
	AutoSnapshot string `json:"autoSnapshot"`
	Replaced     bool   `json:"replaced"`
}

// ---------------------------------------------------------------------------
// Saves engine / game lookup
// ---------------------------------------------------------------------------

// savesOpenEngine builds a saves.Engine rooted at the configured saves_dir,
// falling back to config.DefaultSavesDir(). It mirrors the CLI's openSavesEngine
// (internal/commands/saves.go) so the desktop app and the CLI read and write
// one on-disk store, and it is built per call like the CLI's per-command engine
// rather than cached: an engine carries no state of its own beyond the database
// handle and root.
func (a *App) savesOpenEngine() (*saves.Engine, error) {
	if a.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	root := config.DefaultSavesDir()
	if cfg, err := config.ReadConfig(); err == nil {
		if v := cfg.Get("saves_dir"); v != "" {
			root = v
		}
	}
	engine, err := saves.NewEngine(a.db, root)
	if err != nil {
		return nil, fmt.Errorf("initializing saves engine: %w", err)
	}
	return engine, nil
}

// savesGameFor looks up a game by ID for the saves bindings.
func (a *App) savesGameFor(gameID int64) (*db.Game, error) {
	if a.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	game, err := a.db.GetGame(gameID)
	if err != nil {
		return nil, fmt.Errorf("game with id %d not found: %w", gameID, err)
	}
	if game == nil {
		return nil, fmt.Errorf("game with id %d not found", gameID)
	}
	return game, nil
}

// ---------------------------------------------------------------------------
// Saves bindings
// ---------------------------------------------------------------------------

// GetSavesState returns the save sets and resolved live save location for a
// game, mirroring the TUI's loadSavesState.
func (a *App) GetSavesState(gameID int64) (*DesktopSavesState, error) {
	engine, err := a.savesOpenEngine()
	if err != nil {
		return nil, err
	}
	game, err := a.savesGameFor(gameID)
	if err != nil {
		return nil, err
	}

	state := &DesktopSavesState{
		GameID:      game.ID,
		GameTitle:   game.Title,
		GameVersion: game.Version,
	}

	sets, err := engine.List(game)
	if err != nil {
		return nil, fmt.Errorf("listing save sets: %w", err)
	}
	state.Sets = savesSetsToDTO(sets)

	loc, _, err := engine.ResolveLocation(game)
	if err != nil {
		return nil, fmt.Errorf("resolving save location: %w", err)
	}
	state.LivePath = loc.Path
	state.LiveNote = loc.Note
	state.LiveExists = savesLiveExists(loc)
	state.LiveFiles = savesLiveFiles(loc)

	// The manual override is shown alongside the resolved path (the TUI reads
	// it the same way, ignoring a read failure).
	if cfg, err := engine.DB().GetSaveConfig(game.ID); err == nil && cfg != nil {
		state.ManualPath = cfg.ManualPath
	}
	return state, nil
}

// ApplySaveSet applies a set to the game's live save location. replace mirrors
// the set — removing live save files the set does not contain — instead of
// merging into it.
func (a *App) ApplySaveSet(gameID int64, setName string, replace bool) (*DesktopSaveOpResult, error) {
	engine, err := a.savesOpenEngine()
	if err != nil {
		return nil, err
	}
	game, err := a.savesGameFor(gameID)
	if err != nil {
		return nil, err
	}

	res, err := engine.ApplyWith(game, setName, saves.ApplyOptions{Replace: replace})
	if err != nil {
		return nil, fmt.Errorf("applying save set: %w", err)
	}
	return savesApplyResultToDTO("Applied", res, game), nil
}

// SnapshotSaveSet captures the game's current live save data as a new set. An
// empty label uses the engine's default ("my saves").
func (a *App) SnapshotSaveSet(gameID int64, label string) (*DesktopSaveOpResult, error) {
	engine, err := a.savesOpenEngine()
	if err != nil {
		return nil, err
	}
	game, err := a.savesGameFor(gameID)
	if err != nil {
		return nil, err
	}

	set, err := engine.Snapshot(game, "", label)
	if err != nil {
		return nil, fmt.Errorf("snapshotting saves: %w", err)
	}
	return &DesktopSaveOpResult{
		Notice: fmt.Sprintf("Snapshotted %q as %s (%d files, %s)",
			game.Title, set.Name, set.FileCount, savesSizeLabel(set.SizeBytes)),
		SetName:     set.Name,
		FilesCopied: set.FileCount,
	}, nil
}

// RestoreSaveSet re-applies the game's newest snapshot ("my saves") to its live
// save location.
func (a *App) RestoreSaveSet(gameID int64) (*DesktopSaveOpResult, error) {
	engine, err := a.savesOpenEngine()
	if err != nil {
		return nil, err
	}
	game, err := a.savesGameFor(gameID)
	if err != nil {
		return nil, err
	}

	res, err := engine.Restore(game)
	if err != nil {
		return nil, fmt.Errorf("restoring saves: %w", err)
	}
	return savesApplyResultToDTO("Restored", res, game), nil
}

// RemoveSaveSet deletes a set's stored files and database row. The active set
// cannot be removed; apply another set (or restore your own saves) first.
func (a *App) RemoveSaveSet(gameID int64, setName string) error {
	engine, err := a.savesOpenEngine()
	if err != nil {
		return err
	}
	game, err := a.savesGameFor(gameID)
	if err != nil {
		return err
	}

	if err := engine.Remove(game, setName); err != nil {
		return fmt.Errorf("removing save set: %w", err)
	}
	return nil
}

// ImportSaveSet imports a save set from an archive or directory path, labeled
// label. An empty label derives the label from the set name, as the CLI's
// `saves add` does.
func (a *App) ImportSaveSet(gameID int64, srcPath string, label string) (*DesktopSaveOpResult, error) {
	engine, err := a.savesOpenEngine()
	if err != nil {
		return nil, err
	}
	game, err := a.savesGameFor(gameID)
	if err != nil {
		return nil, err
	}

	set, err := engine.Import(game, srcPath, saves.ImportOptions{Label: label})
	if err != nil {
		return nil, fmt.Errorf("importing save set: %w", err)
	}
	return &DesktopSaveOpResult{
		Notice: fmt.Sprintf("Added set %s for %q (%d files, %s)",
			set.Name, game.Title, set.FileCount, savesSizeLabel(set.SizeBytes)),
		SetName:     set.Name,
		FilesCopied: set.FileCount,
	}, nil
}

// SetSavesLivePath pins the game's live save location to path, overriding
// engine auto-detection. An empty path clears the override and returns to
// auto-detection (the TUI's [p] input behaves the same way).
func (a *App) SetSavesLivePath(gameID int64, path string) error {
	engine, err := a.savesOpenEngine()
	if err != nil {
		return err
	}
	game, err := a.savesGameFor(gameID)
	if err != nil {
		return err
	}

	if strings.TrimSpace(path) == "" {
		if err := engine.ClearManualPath(game); err != nil {
			return fmt.Errorf("clearing save path: %w", err)
		}
		return nil
	}
	if err := engine.SetManualPath(game, strings.TrimSpace(path)); err != nil {
		return fmt.Errorf("pinning save path: %w", err)
	}
	return nil
}

// ScanSaveDirs hunts for the game's real save directories: the candidate list
// `moxie saves scan` prints, newest first. Read-only; nothing is pinned.
func (a *App) ScanSaveDirs(gameID int64) ([]DesktopSaveScanCandidate, error) {
	engine, err := a.savesOpenEngine()
	if err != nil {
		return nil, err
	}
	game, err := a.savesGameFor(gameID)
	if err != nil {
		return nil, err
	}

	candidates, err := engine.ScanLiveSaves(game)
	if err != nil {
		return nil, fmt.Errorf("scanning live saves: %w", err)
	}
	out := make([]DesktopSaveScanCandidate, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, DesktopSaveScanCandidate{
			Path:       c.Path,
			Files:      c.Files,
			NewestFile: c.NewestFile,
			NewestMod:  c.NewestMod,
		})
	}
	return out, nil
}

// GetSavesOverview returns one row per library game that has at least one save
// set, for the Saves overview. Live save resolution is best-effort per game: a
// game whose location cannot be resolved (or whose sets cannot be read) still
// lists when it has sets, with an empty LivePath — a single broken game must not
// blank the whole overview.
func (a *App) GetSavesOverview() ([]DesktopGameSavesSummary, error) {
	if a.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	engine, err := a.savesOpenEngine()
	if err != nil {
		return nil, err
	}

	games, err := a.db.ListActiveGames("", "")
	if err != nil {
		return nil, fmt.Errorf("listing games: %w", err)
	}

	out := make([]DesktopGameSavesSummary, 0, len(games))
	for _, g := range games {
		sets, err := engine.List(&g)
		if err != nil {
			slog.Warn("failed to list save sets", "game_id", g.ID, "error", err)
			continue
		}
		if len(sets) == 0 {
			continue
		}
		row := DesktopGameSavesSummary{GameID: g.ID, Title: g.Title, Sets: len(sets)}
		for _, s := range sets {
			if s.Active {
				row.HasActive = true
				break
			}
		}
		if loc, _, err := engine.ResolveLocation(&g); err == nil {
			row.LivePath = loc.Path
		}
		out = append(out, row)
	}
	return out, nil
}

// PickSaveArchive opens a native file picker for save archives. Returns the
// selected path, or "" when the dialog is cancelled or unavailable.
func (a *App) PickSaveArchive() string {
	if a.ctx == nil {
		return ""
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Save Archive",
		Filters: []runtime.FileFilter{
			{DisplayName: "Save Archives", Pattern: "*.zip;*.7z;*.rar;*.tar;*.gz;*.tgz"},
			{DisplayName: "All Files", Pattern: "*.*"},
		},
	})
	if err != nil {
		slog.Error("save archive picker failed", "error", err)
		return ""
	}
	return path
}

// ---------------------------------------------------------------------------
// Saves display / mapping helpers
// ---------------------------------------------------------------------------

// savesSetsToDTO maps save-set rows to the frontend DTO, preserving the order
// the database and engine return them in (newest first).
func savesSetsToDTO(sets []db.SaveSet) []DesktopSaveSet {
	out := make([]DesktopSaveSet, 0, len(sets))
	for _, s := range sets {
		out = append(out, DesktopSaveSet{
			Name:        s.Name,
			Label:       s.Label,
			Source:      s.Source,
			Active:      s.Active,
			GameVersion: s.GameVersion,
			Files:       s.FileCount,
			Bytes:       s.SizeBytes,
			CreatedAt:   s.CreatedAt,
		})
	}
	return out
}

// savesApplyResultToDTO flattens an apply or restore result for the frontend.
// verb is the confirmation verb ("Applied", "Restored").
func savesApplyResultToDTO(verb string, res *saves.ApplyResult, game *db.Game) *DesktopSaveOpResult {
	if res == nil {
		return &DesktopSaveOpResult{Notice: verb + " save set"}
	}
	gameVersion := ""
	if game != nil {
		gameVersion = game.Version
	}
	return &DesktopSaveOpResult{
		Notice:       savesApplyNotice(verb, res),
		VersionWarn:  savesVersionWarn(res.Set.GameVersion, gameVersion),
		SetName:      res.Set.Name,
		TargetPath:   res.Target.Path,
		FilesCopied:  res.FilesCopied,
		FilesRemoved: res.FilesRemoved,
		AutoSnapshot: res.AutoSnapshot,
		Replaced:     res.Replaced,
	}
}

// savesApplyNotice renders the confirmation line for an apply or restore,
// mirroring the TUI's applyResultNotice: it names the set, the target live
// location, and the copy count, then notes an auto-snapshot when one was taken
// and a replace when the live save data was mirrored.
func savesApplyNotice(verb string, res *saves.ApplyResult) string {
	if res == nil {
		return verb + " save set"
	}
	notice := fmt.Sprintf("%s %s to %s (%d files)",
		verb, savesDisplayLabel(res.Set), res.Target.Path, res.FilesCopied)
	if res.AutoSnapshot != "" {
		notice += fmt.Sprintf(" — auto-snapshotted your saves as %s", res.AutoSnapshot)
	}
	if res.Replaced {
		notice += fmt.Sprintf(" — replaced live saves (%d files removed)", res.FilesRemoved)
	}
	return notice
}

// savesDisplayLabel renders a set as "name (label)" when the label adds
// information, else just the name (matching the TUI and the CLI's set table).
func savesDisplayLabel(s db.SaveSet) string {
	if s.Label != "" && s.Label != s.Name {
		return fmt.Sprintf("%s (%s)", s.Name, s.Label)
	}
	return s.Name
}

// savesVersionWarn renders the short version-mismatch warning shown next to a set
// that was captured for a different game version, or "" when the versions match
// (or either version is unknown). It shares saves.VersionsDiffer with the CLI, so
// a set moxie warns about in the terminal is the one the GUI warns about.
func savesVersionWarn(setVersion, gameVersion string) string {
	if !saves.VersionsDiffer(setVersion, gameVersion) {
		return ""
	}
	return fmt.Sprintf("captured for %s, game is %s; saves may not load", setVersion, gameVersion)
}

// savesSizeLabel renders a byte count for save-set notices. formatBytes hides a
// zero size, but a set with no bytes is a real, showable count.
func savesSizeLabel(bytes int64) string {
	if bytes == 0 {
		return "0 B"
	}
	return formatBytes(bytes)
}

// savesLiveExists reports whether the resolved live save location exists on disk as
// a directory.
func savesLiveExists(loc saves.Location) bool {
	if loc.Path == "" {
		return false
	}
	info, err := os.Stat(loc.Path)
	return err == nil && info.IsDir()
}

// savesLiveFiles counts the save data the live location currently holds: the
// glob-matched regular files for a KindFiles location (the location's globs are the
// engine's own patterns), or every file in the tree for a KindDir location. It
// mirrors the engine's liveDataCount, so the desktop reports the same "N files"
// the CLI and TUI show.
func savesLiveFiles(loc saves.Location) int {
	if loc.Path == "" {
		return 0
	}

	if loc.Kind == saves.KindFiles {
		entries, err := os.ReadDir(loc.Path)
		if err != nil {
			return 0
		}
		n := 0
		for _, ent := range entries {
			if !ent.IsDir() && savesGlobMatch(ent.Name(), loc.Globs) {
				n++
			}
		}
		return n
	}

	n := 0
	_ = filepath.WalkDir(loc.Path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

// savesGlobMatch reports whether name matches any of globs, comparing
// case-insensitively.
func savesGlobMatch(name string, globs []string) bool {
	lower := strings.ToLower(name)
	for _, g := range globs {
		if ok, _ := filepath.Match(strings.ToLower(g), lower); ok {
			return true
		}
	}
	return false
}
