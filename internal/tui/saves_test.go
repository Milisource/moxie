package tui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/saves"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newSavesTestEnv builds a model backed by a real temp database and saves
// engine, plus a persisted game, so the Saves-screen flows can run end to end
// without a live terminal.
func newSavesTestEnv(t *testing.T) (model, *db.Database, *db.Game) {
	t.Helper()

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "moxie.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	engine, err := saves.NewEngine(database, filepath.Join(dir, "saves"))
	if err != nil {
		t.Fatalf("saves.NewEngine: %v", err)
	}

	id, err := database.InsertGame(&db.Game{
		Title:  "Test Game",
		Engine: "RenPy",
		Path:   filepath.Join(dir, "game"),
	})
	if err != nil {
		t.Fatalf("InsertGame: %v", err)
	}
	game, err := database.GetGame(id)
	if err != nil || game == nil {
		t.Fatalf("GetGame(%d) = %v, %v", id, game, err)
	}

	m := model{
		db:          database,
		savesEngine: engine,
		detailGame:  game,
		viewMode:    DetailView,
		selectedID:  id,
		width:       100,
		height:      40,
	}
	return m, database, game
}

// mustModel unwraps the tea.Model returned by the value-receiver key handlers.
func mustModel(t *testing.T, m tea.Model) model {
	t.Helper()
	got, ok := m.(model)
	if !ok {
		t.Fatalf("handler returned %T, want model", m)
	}
	return got
}

// ---------------------------------------------------------------------------
// Pure derivation helpers
// ---------------------------------------------------------------------------

func TestSavesDisplayName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		set  db.SaveSet
		want string
	}{
		{"name only", db.SaveSet{Name: "mine"}, "mine"},
		{"label equals name", db.SaveSet{Name: "mine", Label: "mine"}, "mine"},
		{"label adds info", db.SaveSet{Name: "mine", Label: "my saves"}, "mine (my saves)"},
		{"empty label", db.SaveSet{Name: "100-percent", Label: ""}, "100-percent"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := savesDisplayName(tt.set); got != tt.want {
				t.Errorf("savesDisplayName(%+v) = %q, want %q", tt.set, got, tt.want)
			}
		})
	}
}

func TestSavesLocationText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		loc    saves.Location
		manual string
		want   string
	}{
		{
			"manual wins",
			saves.Location{Path: "/auto/renpy"},
			"/pinned/saves",
			"/pinned/saves  (pinned)",
		},
		{
			"unresolved",
			saves.Location{},
			"",
			"not detected — press [p] to pin a path",
		},
		{
			"resolved with note",
			saves.Location{Path: "/game/game/saves", Note: "portable saves dir"},
			"",
			"/game/game/saves  (portable saves dir)",
		},
		{
			"resolved without note",
			saves.Location{Path: "/game/save"},
			"",
			"/game/save",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := savesLocationText(tt.loc, tt.manual); got != tt.want {
				t.Errorf("savesLocationText(%+v, %q) = %q, want %q", tt.loc, tt.manual, got, tt.want)
			}
		})
	}
}

func TestSavesStatusLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		sets []db.SaveSet
		want string
	}{
		{"no sets", nil, "Current: my saves"},
		{"no active set", []db.SaveSet{
			{Name: "mine-1", Source: "snapshot"},
			{Name: "100-percent", Source: "import"},
		}, "Current: my saves"},
		{"active with label and source", []db.SaveSet{
			{Name: "mine-1", Source: "snapshot"},
			{Name: "100-percent", Label: "100% (community)", Source: "f95_attachment", Active: true},
		}, "Current: 100% (community) (f95_attachment)"},
		{"active without label uses name", []db.SaveSet{
			{Name: "100-percent", Source: "import", Active: true},
		}, "Current: 100-percent (import)"},
		{"active without source", []db.SaveSet{
			{Name: "mine-1", Label: "my saves", Active: true},
		}, "Current: my saves"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := savesStatusLine(tt.sets); got != tt.want {
				t.Errorf("savesStatusLine(%+v) = %q, want %q", tt.sets, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Enter / leave the screen
// ---------------------------------------------------------------------------

func TestDetailKeyV_OpensSavesScreen(t *testing.T) {
	t.Parallel()
	m, _, _ := newSavesTestEnv(t)

	res, cmd := m.handleDetailKey("v")
	got := mustModel(t, res)
	if got.viewMode != SavesView {
		t.Fatalf("viewMode = %v, want SavesView", got.viewMode)
	}
	if cmd == nil {
		t.Fatal("expected a load command when entering the saves screen")
	}
	if !got.savesLoading {
		t.Error("expected savesLoading to be set while the first load is in flight")
	}
	if got.savesCursor != 0 {
		t.Errorf("cursor = %d, want 0", got.savesCursor)
	}
}

// TestDetailKeyV_SetsLoadingAndClearsPreviousState covers the transition from
// one game's screen to another: the previous game's rows and resolved location
// must be discarded before the load for the new game is issued.
func TestDetailKeyV_SetsLoadingAndClearsPreviousState(t *testing.T) {
	t.Parallel()
	m, _, _ := newSavesTestEnv(t)
	// Simulate a previous game's screen still being populated.
	m.savesSets = []db.SaveSet{{Name: "previous-set"}}
	m.savesLocation = saves.Location{Path: "/previous"}
	m.savesCandidates = []saves.Location{{Path: "/candidate"}}
	m.savesManualPath = "/previous-manual"
	m.savesCursor = 3
	m.savesConfirmDelete = true
	m.savesDeleteName = "previous-set"
	m.savesDeleteLabel = "previous"

	res, cmd := m.handleDetailKey("v")
	got := mustModel(t, res)

	if got.viewMode != SavesView {
		t.Fatalf("viewMode = %v, want SavesView", got.viewMode)
	}
	if cmd == nil {
		t.Fatal("expected a load command when entering the saves screen")
	}
	if !got.savesLoading {
		t.Error("expected savesLoading to be set on entry")
	}
	if got.savesSets != nil {
		t.Errorf("savesSets = %+v, want cleared", got.savesSets)
	}
	if got.savesLocation.Path != "" || got.savesManualPath != "" || got.savesCandidates != nil {
		t.Errorf("location state not cleared: loc=%+v manual=%q cands=%+v",
			got.savesLocation, got.savesManualPath, got.savesCandidates)
	}
	if got.savesCursor != 0 {
		t.Errorf("cursor = %d, want 0", got.savesCursor)
	}
	if got.savesConfirmDelete || got.savesDeleteName != "" || got.savesDeleteLabel != "" {
		t.Errorf("delete overlay not cleared: confirm=%v name=%q label=%q",
			got.savesConfirmDelete, got.savesDeleteName, got.savesDeleteLabel)
	}
}

func TestDetailKeyV_NoGameIsNoop(t *testing.T) {
	t.Parallel()
	m := model{viewMode: DetailView}

	res, cmd := m.handleDetailKey("v")
	got := mustModel(t, res)
	if got.viewMode != DetailView {
		t.Errorf("viewMode = %v, want DetailView (unchanged)", got.viewMode)
	}
	if cmd != nil {
		t.Error("expected no command when no game is loaded")
	}
}

func TestSavesKeyEsc_ReturnsToDetailAndClearsOverlays(t *testing.T) {
	t.Parallel()
	m := model{
		viewMode:           SavesView,
		savesConfirmDelete: true,
		savesEditingPath:   true,
		savesDeleteName:    "100-percent",
		savesDeleteLabel:   "100%",
		savesLoading:       true,
	}

	res, cmd := m.handleSavesKey("esc")
	got := mustModel(t, res)
	if got.viewMode != DetailView {
		t.Errorf("viewMode = %v, want DetailView", got.viewMode)
	}
	if got.savesConfirmDelete || got.savesEditingPath || got.savesLoading {
		t.Errorf("overlays not cleared: confirm=%v edit=%v loading=%v",
			got.savesConfirmDelete, got.savesEditingPath, got.savesLoading)
	}
	if got.savesDeleteName != "" || got.savesDeleteLabel != "" {
		t.Errorf("pending delete not cleared: name=%q label=%q", got.savesDeleteName, got.savesDeleteLabel)
	}
	if cmd != nil {
		t.Error("esc should not return a command")
	}
}

func TestSavesKeyHelp(t *testing.T) {
	t.Parallel()
	m := model{viewMode: SavesView}

	res, _ := m.handleSavesKey("?")
	if got := mustModel(t, res); !got.showHelp {
		t.Error("expected [?] to open the help overlay from the saves screen")
	}
}

func TestSavesViewIsRendered(t *testing.T) {
	t.Parallel()
	m, _, _ := newSavesTestEnv(t)
	m.viewMode = SavesView

	view := m.View()
	if !strings.Contains(view, "Saves — Test Game") {
		t.Errorf("view missing game header:\n%s", view)
	}
	if !strings.Contains(view, "not detected") {
		t.Errorf("view missing unresolved-location hint:\n%s", view)
	}
	if !strings.Contains(view, "No save sets yet") {
		t.Errorf("view missing empty-state message:\n%s", view)
	}
	if !strings.Contains(view, "Current: my saves") {
		t.Errorf("view missing status line:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Cursor movement
// ---------------------------------------------------------------------------

func TestSavesCursor_MovesAndWraps(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		cursor int
		key    string
		want   int
	}{
		{"down advances", 0, "down", 1},
		{"j advances", 1, "j", 2},
		{"down wraps past end", 2, "down", 0},
		{"up moves back", 2, "up", 1},
		{"k moves back", 1, "k", 0},
		{"up wraps before start", 0, "up", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{
				viewMode:    SavesView,
				savesSets:   []db.SaveSet{{Name: "a"}, {Name: "b"}, {Name: "c"}},
				savesCursor: tt.cursor,
			}
			res, _ := m.handleSavesKey(tt.key)
			if got := mustModel(t, res); got.savesCursor != tt.want {
				t.Errorf("cursor after %q = %d, want %d", tt.key, got.savesCursor, tt.want)
			}
		})
	}
}

func TestSavesCursor_EmptyListStaysZero(t *testing.T) {
	t.Parallel()
	m := model{viewMode: SavesView}

	res, _ := m.handleSavesKey("down")
	if got := mustModel(t, res); got.savesCursor != 0 {
		t.Errorf("cursor = %d, want 0 for an empty list", got.savesCursor)
	}
}

// ---------------------------------------------------------------------------
// Apply / snapshot / restore
// ---------------------------------------------------------------------------

func TestSavesKeyApply_InactiveSetStartsCommand(t *testing.T) {
	t.Parallel()
	m := model{
		viewMode:  SavesView,
		savesSets: []db.SaveSet{{Name: "100-percent", Source: "import"}},
	}

	res, cmd := m.handleSavesKey("a")
	got := mustModel(t, res)
	if cmd == nil {
		t.Fatal("expected a command when applying an inactive set")
	}
	if !got.savesLoading {
		t.Error("expected the loading flag to be set while applying")
	}
}

func TestSavesKeyApply_ActiveSetIsNoop(t *testing.T) {
	t.Parallel()
	m := model{
		viewMode:  SavesView,
		savesSets: []db.SaveSet{{Name: "100-percent", Label: "100%", Source: "import", Active: true}},
	}

	res, cmd := m.handleSavesKey("a")
	got := mustModel(t, res)
	if cmd != nil {
		t.Error("applying the already-active set should not run a command")
	}
	if !strings.Contains(got.notice, "already the active set") {
		t.Errorf("notice = %q, want an already-active hint", got.notice)
	}
}

func TestSavesKeyApply_EmptyListIsNoop(t *testing.T) {
	t.Parallel()
	m := model{viewMode: SavesView}

	if _, cmd := m.handleSavesKey("a"); cmd != nil {
		t.Error("apply with no sets should not run a command")
	}
}

func TestSavesKeySnapshotAndRestore_StartCommands(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"b", "r"} {
		t.Run(key, func(t *testing.T) {
			m, _, _ := newSavesTestEnv(t)
			m.viewMode = SavesView

			res, cmd := m.handleSavesKey(key)
			got := mustModel(t, res)
			if cmd == nil {
				t.Fatalf("%q should return a command", key)
			}
			if !got.savesLoading {
				t.Errorf("%q should set the loading flag", key)
			}
		})
	}
}

func TestSavesKey_MutationsBlockedWhileLoading(t *testing.T) {
	t.Parallel()
	m := model{viewMode: SavesView, savesLoading: true}

	for _, key := range []string{"a", "b", "r", "p"} {
		if _, cmd := m.handleSavesKey(key); cmd != nil {
			t.Errorf("%q should be ignored while a saves op is in flight", key)
		}
	}
}

// ---------------------------------------------------------------------------
// Remove confirmation
// ---------------------------------------------------------------------------

func TestSavesKeyRemove_ConfirmThenCancel(t *testing.T) {
	t.Parallel()
	m := model{
		viewMode:  SavesView,
		savesSets: []db.SaveSet{{Name: "100-percent", Label: "100%"}, {Name: "mine"}},
	}

	res, _ := m.handleSavesKey("d")
	got := mustModel(t, res)
	if !got.savesConfirmDelete {
		t.Fatal("expected [d] to arm the remove confirmation")
	}
	if got.savesDeleteName != "100-percent" {
		t.Errorf("deleteName = %q, want the selected set", got.savesDeleteName)
	}

	res, cmd := got.handleSavesDeleteConfirm("n")
	got = mustModel(t, res)
	if got.savesConfirmDelete || got.savesDeleteName != "" {
		t.Errorf("cancel did not reset the confirmation: %+v", got)
	}
	if cmd != nil {
		t.Error("cancel should not run a command")
	}
}

func TestSavesKeyRemove_ConfirmThenRemove(t *testing.T) {
	t.Parallel()
	m := model{
		viewMode:  SavesView,
		savesSets: []db.SaveSet{{Name: "100-percent"}},
	}

	res, _ := m.handleSavesKey("d")
	got := mustModel(t, res)

	res, cmd := got.handleSavesDeleteConfirm("y")
	got = mustModel(t, res)
	if got.savesConfirmDelete {
		t.Error("confirmation should be dismissed after confirming")
	}
	if cmd == nil {
		t.Fatal("confirming a remove should return a command")
	}
	if !got.savesLoading {
		t.Error("expected the loading flag to be set while removing")
	}
}

// ---------------------------------------------------------------------------
// Async wiring against a real database + engine
// ---------------------------------------------------------------------------

func TestLoadSaves_ReadsSetsFromDatabase(t *testing.T) {
	t.Parallel()
	m, database, game := newSavesTestEnv(t)

	if _, err := database.CreateSaveSet(&db.SaveSet{
		GameID: game.ID, Name: "100-percent", Label: "100%",
		Source: "f95_attachment", SizeBytes: 2048, FileCount: 3, Active: true,
	}); err != nil {
		t.Fatalf("CreateSaveSet(active): %v", err)
	}
	if _, err := database.CreateSaveSet(&db.SaveSet{
		GameID: game.ID, Name: "mine-20260101", Label: "my saves",
		Source: "snapshot", SizeBytes: 1024, FileCount: 1,
	}); err != nil {
		t.Fatalf("CreateSaveSet(mine): %v", err)
	}

	msg := m.loadSaves()()
	loaded, ok := msg.(savesLoadedMsg)
	if !ok {
		t.Fatalf("loadSaves returned %T, want savesLoadedMsg", msg)
	}
	if loaded.gameID != game.ID {
		t.Errorf("gameID = %d, want %d", loaded.gameID, game.ID)
	}
	if loaded.err != nil {
		t.Fatalf("loadSaves err: %v", loaded.err)
	}

	next, _ := m.Update(loaded)
	got := mustModel(t, next)
	if len(got.savesSets) != 2 {
		t.Fatalf("loaded %d sets, want 2", len(got.savesSets))
	}
	if got.savesLoading {
		t.Error("loading flag should be cleared after a load")
	}
	if got.savesCursor != 0 {
		t.Errorf("cursor = %d, want 0", got.savesCursor)
	}
	if status := savesStatusLine(got.savesSets); !strings.Contains(status, "100%") {
		t.Errorf("status line = %q, want the active set's label", status)
	}
}

// TestSavesLoadedMsg_StaleGameIsDropped ensures a load that completes after the
// user has navigated to a different game is discarded rather than rendered
// under the new game's title.
func TestSavesLoadedMsg_StaleGameIsDropped(t *testing.T) {
	t.Parallel()
	m, _, game := newSavesTestEnv(t)
	// The screen currently shows `game` and its own state.
	m.savesSets = []db.SaveSet{{Name: "current-set"}}
	m.savesManualPath = "/current"
	m.savesLoading = true

	stale := savesLoadedMsg{
		gameID: game.ID + 1, // a different game
		state: savesState{
			sets:     []db.SaveSet{{Name: "stale-set"}},
			location: saves.Location{Path: "/stale"},
			manual:   "/stale-manual",
		},
	}
	next, _ := m.Update(stale)
	got := mustModel(t, next)

	if len(got.savesSets) != 1 || got.savesSets[0].Name != "current-set" {
		t.Errorf("stale load applied: sets = %+v, want the current game's set", got.savesSets)
	}
	if got.savesManualPath != "/current" {
		t.Errorf("manual path = %q, want /current (unchanged)", got.savesManualPath)
	}
	if got.savesLocation.Path != "" {
		t.Errorf("location = %+v, want unchanged", got.savesLocation)
	}
	if !got.savesLoading {
		t.Error("loading flag should stay set while the current game's load is pending")
	}
	if got.err != nil {
		t.Errorf("err = %v, want nil for a dropped stale load", got.err)
	}
}

// TestSavesLoadedMsg_NoGameIsDropped ensures a load is discarded when no game
// is currently displayed (e.g. the user backed out to the library).
func TestSavesLoadedMsg_NoGameIsDropped(t *testing.T) {
	t.Parallel()
	m := model{savesSets: []db.SaveSet{{Name: "keep"}}}

	next, _ := m.Update(savesLoadedMsg{
		gameID: 7,
		state:  savesState{sets: []db.SaveSet{{Name: "x"}}},
	})
	got := mustModel(t, next)

	if len(got.savesSets) != 1 || got.savesSets[0].Name != "keep" {
		t.Errorf("load with no displayed game applied: %+v", got.savesSets)
	}
	if got.err != nil {
		t.Errorf("err = %v, want nil", got.err)
	}
}

// TestSavesOpMsg_StaleGameIsDropped ensures an in-flight mutation that lands
// after the user has left its game cannot clobber the current game's screen or
// surface its notice/error.
func TestSavesOpMsg_StaleGameIsDropped(t *testing.T) {
	t.Parallel()
	m, _, game := newSavesTestEnv(t)
	m.savesSets = []db.SaveSet{{Name: "current-set"}}

	stale := savesOpMsg{
		gameID: game.ID + 1,
		notice: "Removed save set stale-set",
		state:  savesState{sets: []db.SaveSet{{Name: "stale-set"}}},
	}
	next, _ := m.Update(stale)
	got := mustModel(t, next)

	if got.notice != "" {
		t.Errorf("notice = %q, want empty for a dropped op", got.notice)
	}
	if len(got.savesSets) != 1 || got.savesSets[0].Name != "current-set" {
		t.Errorf("stale op applied: %+v", got.savesSets)
	}

	// A stale op carrying an error must not surface it either.
	next, _ = got.Update(savesOpMsg{gameID: game.ID + 1, err: errors.New("boom")})
	got = mustModel(t, next)
	if got.err != nil {
		t.Errorf("err = %v, want nil; a stale op's error must not display", got.err)
	}
}

func TestSetSavesPathCmd_PinsAndClears(t *testing.T) {
	t.Parallel()
	m, database, game := newSavesTestEnv(t)

	msg := m.setSavesPathCmd("/tmp/live-saves")()
	op, ok := msg.(savesOpMsg)
	if !ok {
		t.Fatalf("setSavesPathCmd returned %T, want savesOpMsg", msg)
	}
	if op.gameID != game.ID {
		t.Errorf("gameID = %d, want %d", op.gameID, game.ID)
	}
	if op.err != nil {
		t.Fatalf("pin err: %v", op.err)
	}
	cfg, err := database.GetSaveConfig(game.ID)
	if err != nil {
		t.Fatalf("GetSaveConfig: %v", err)
	}
	if cfg == nil || cfg.ManualPath != "/tmp/live-saves" {
		t.Fatalf("manual path = %+v, want /tmp/live-saves", cfg)
	}
	if op.state.manual != "/tmp/live-saves" {
		t.Errorf("reloaded state manual = %q, want the pinned path", op.state.manual)
	}

	msg = m.setSavesPathCmd("")()
	op, ok = msg.(savesOpMsg)
	if !ok {
		t.Fatalf("setSavesPathCmd(clear) returned %T, want savesOpMsg", msg)
	}
	if op.err != nil {
		t.Fatalf("clear err: %v", op.err)
	}
	cfg, err = database.GetSaveConfig(game.ID)
	if err != nil {
		t.Fatalf("GetSaveConfig: %v", err)
	}
	if cfg != nil && cfg.ManualPath != "" {
		t.Errorf("manual path = %q, want cleared", cfg.ManualPath)
	}
	if op.state.manual != "" {
		t.Errorf("reloaded state manual = %q, want empty", op.state.manual)
	}
}

func TestSavesPathEdit_PrefillsManualThenResolved(t *testing.T) {
	t.Parallel()

	t.Run("manual wins", func(t *testing.T) {
		m := model{savesManualPath: "/pinned", savesLocation: saves.Location{Path: "/auto"}}
		if got := m.savesPathPrefill(); got != "/pinned" {
			t.Errorf("prefill = %q, want /pinned", got)
		}
	})

	t.Run("falls back to resolved", func(t *testing.T) {
		m := model{savesLocation: saves.Location{Path: "/auto"}}
		if got := m.savesPathPrefill(); got != "/auto" {
			t.Errorf("prefill = %q, want /auto", got)
		}
	})

	t.Run("empty when unresolved", func(t *testing.T) {
		m := model{}
		if got := m.savesPathPrefill(); got != "" {
			t.Errorf("prefill = %q, want empty", got)
		}
	})
}

func TestSavesPathEdit_EmptySubmissionClears(t *testing.T) {
	t.Parallel()
	m, database, game := newSavesTestEnv(t)
	if err := m.savesEngine.SetManualPath(game, "/pinned"); err != nil {
		t.Fatalf("SetManualPath: %v", err)
	}

	// Load the screen state the way entering the view does, so the editor
	// prefills from the pinned path.
	msg := m.loadSaves()()
	loaded, ok := msg.(savesLoadedMsg)
	if !ok {
		t.Fatalf("loadSaves returned %T, want savesLoadedMsg", msg)
	}
	if loaded.err != nil {
		t.Fatalf("loadSaves err: %v", loaded.err)
	}
	next, _ := m.Update(loaded)
	m = mustModel(t, next)

	res, _ := m.startSavesPathEdit()
	got := mustModel(t, res)
	if !got.savesEditingPath {
		t.Fatal("expected the path editor to open")
	}
	if got.savesPathInput.Value() != "/pinned" {
		t.Errorf("input prefill = %q, want the existing manual path", got.savesPathInput.Value())
	}

	got.savesPathInput.SetValue("")
	res, cmd := got.handleSavesPathInput(tea.KeyMsg{}, "enter")
	got = mustModel(t, res)
	if got.savesEditingPath {
		t.Error("submitting should close the path editor")
	}
	if cmd == nil {
		t.Fatal("submitting should return a command")
	}

	op, ok := cmd().(savesOpMsg)
	if !ok {
		t.Fatalf("command returned %T, want savesOpMsg", cmd())
	}
	if op.err != nil {
		t.Fatalf("clear err: %v", op.err)
	}

	cfg, err := database.GetSaveConfig(game.ID)
	if err != nil {
		t.Fatalf("GetSaveConfig: %v", err)
	}
	if cfg != nil && cfg.ManualPath != "" {
		t.Errorf("manual path = %q, want cleared after empty submission", cfg.ManualPath)
	}
}

func TestSavesPathEdit_EscClosesWithoutCommand(t *testing.T) {
	t.Parallel()
	m, _, _ := newSavesTestEnv(t)

	res, _ := m.startSavesPathEdit()
	got := mustModel(t, res)

	res, cmd := got.handleSavesPathInput(tea.KeyMsg{}, "esc")
	got = mustModel(t, res)
	if got.savesEditingPath {
		t.Error("esc should close the path editor")
	}
	if cmd != nil {
		t.Error("esc should not return a command")
	}
}

func TestSavesEngineUnavailable_SurfacesError(t *testing.T) {
	t.Parallel()
	m := model{detailGame: &db.Game{ID: 1, Title: "Test"}}

	msg := m.loadSaves()()
	loaded, ok := msg.(savesLoadedMsg)
	if !ok {
		t.Fatalf("loadSaves returned %T, want savesLoadedMsg", msg)
	}
	if loaded.err == nil {
		t.Fatal("expected an error when the saves engine is nil")
	}

	next, _ := m.Update(loaded)
	got := mustModel(t, next)
	if got.err == nil {
		t.Error("expected the error to be surfaced on the model")
	}
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

func TestRenderSaveSetRows_ShowsFieldsAndActiveMarker(t *testing.T) {
	t.Parallel()
	m := model{
		width:       120,
		savesCursor: 1,
		savesSets: []db.SaveSet{
			{
				Name:        "mine-20260101",
				Label:       "my saves",
				Source:      "snapshot",
				SizeBytes:   1024,
				GameVersion: "0.9",
				CreatedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			{
				Name:        "100-percent",
				Label:       "100% (community)",
				Source:      "f95_attachment",
				SizeBytes:   2097152,
				GameVersion: "1.0",
				Active:      true,
				CreatedAt:   time.Date(2026, 2, 14, 0, 0, 0, 0, time.UTC),
			},
		},
	}

	rows := m.renderSaveSetRows()
	for _, want := range []string{
		"mine-20260101", "my saves", "snapshot", "1.0 KB", "0.9", "2026-01-01",
		"100-percent", "f95_attachment", "2.0 MB", "2026-02-14", "●",
	} {
		if !strings.Contains(rows, want) {
			t.Errorf("rows missing %q:\n%s", want, rows)
		}
	}
}

func TestRenderSaveSetRows_Empty(t *testing.T) {
	t.Parallel()
	m := model{width: 80}

	view := m.savesView()
	if !strings.Contains(view, "No save sets yet") {
		t.Errorf("empty list should show the empty-state message:\n%s", view)
	}
	if !strings.Contains(view, "Current: my saves") {
		t.Errorf("empty list should default the status line to my saves:\n%s", view)
	}
}
