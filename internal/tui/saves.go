package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/saves"
)

// errSavesUnavailable is reported when the saves engine could not be built
// (e.g. the storage root is unwritable) or no game is loaded yet.
var errSavesUnavailable = errors.New("saves engine unavailable")

// savesState is the refreshable portion of the Saves screen: the game's set
// list plus its resolved live save location and candidate locations.
type savesState struct {
	sets       []db.SaveSet
	location   saves.Location
	candidates []saves.Location
	manual     string // manual_path override from save_configs ("" when auto)
}

// loadSavesState gathers everything the Saves screen renders for game. It is
// the single read path shared by the initial load and every post-mutation
// refresh, so the screen can never drift from the database.
func loadSavesState(engine *saves.Engine, game *db.Game) (savesState, error) {
	if engine == nil {
		return savesState{}, errSavesUnavailable
	}
	if game == nil {
		return savesState{}, errors.New("no game selected")
	}
	sets, err := engine.List(game)
	if err != nil {
		return savesState{}, err
	}
	loc, candidates, err := engine.ResolveLocation(game)
	if err != nil {
		return savesState{}, err
	}
	var manual string
	if cfg, err := engine.DB().GetSaveConfig(game.ID); err == nil && cfg != nil {
		manual = cfg.ManualPath
	}
	return savesState{
		sets:       sets,
		location:   loc,
		candidates: candidates,
		manual:     manual,
	}, nil
}

// ─── Async commands ────────────────────────────────────────────────────────

// gameIDOf returns game's ID, or 0 when game is nil. It keeps the gameID
// stamped on async saves messages in step with the game the command captured.
func gameIDOf(game *db.Game) int64 {
	if game == nil {
		return 0
	}
	return game.ID
}

// loadSaves reads the game's save sets and resolved location.
func (m model) loadSaves() tea.Cmd {
	engine, game := m.savesEngine, m.detailGame
	gameID := gameIDOf(game)
	return func() tea.Msg {
		state, err := loadSavesState(engine, game)
		return savesLoadedMsg{gameID: gameID, state: state, err: err}
	}
}

// applySaveSetCmd applies the named set to the live save location and reloads.
func (m model) applySaveSetCmd(name string) tea.Cmd {
	engine, game := m.savesEngine, m.detailGame
	gameID := gameIDOf(game)
	return func() tea.Msg {
		if engine == nil || game == nil {
			return savesOpMsg{gameID: gameID, err: errSavesUnavailable}
		}
		res, err := engine.Apply(game, name)
		if err != nil {
			return savesOpMsg{gameID: gameID, err: err}
		}
		return savesOpMsg{gameID: gameID, notice: applyResultNotice("Applied", res), state: mustSavesState(engine, game)}
	}
}

// snapshotSavesCmd captures the current live saves as a new set and reloads.
func (m model) snapshotSavesCmd() tea.Cmd {
	engine, game := m.savesEngine, m.detailGame
	gameID := gameIDOf(game)
	return func() tea.Msg {
		if engine == nil || game == nil {
			return savesOpMsg{gameID: gameID, err: errSavesUnavailable}
		}
		set, err := engine.Snapshot(game, "", "")
		if err != nil {
			return savesOpMsg{gameID: gameID, err: err}
		}
		notice := fmt.Sprintf("Snapshotted %q as %s (%d files, %s)",
			game.Title, set.Name, set.FileCount, formatSize(set.SizeBytes))
		return savesOpMsg{gameID: gameID, notice: notice, state: mustSavesState(engine, game)}
	}
}

// restoreSavesCmd re-applies the newest snapshot ("my saves") and reloads.
func (m model) restoreSavesCmd() tea.Cmd {
	engine, game := m.savesEngine, m.detailGame
	gameID := gameIDOf(game)
	return func() tea.Msg {
		if engine == nil || game == nil {
			return savesOpMsg{gameID: gameID, err: errSavesUnavailable}
		}
		res, err := engine.Restore(game)
		if err != nil {
			return savesOpMsg{gameID: gameID, err: err}
		}
		return savesOpMsg{gameID: gameID, notice: applyResultNotice("Restored", res), state: mustSavesState(engine, game)}
	}
}

// removeSaveSetCmd deletes the named set (files + row) and reloads.
func (m model) removeSaveSetCmd(name string) tea.Cmd {
	engine, game := m.savesEngine, m.detailGame
	gameID := gameIDOf(game)
	return func() tea.Msg {
		if engine == nil || game == nil {
			return savesOpMsg{gameID: gameID, err: errSavesUnavailable}
		}
		if err := engine.Remove(game, name); err != nil {
			return savesOpMsg{gameID: gameID, err: err}
		}
		notice := fmt.Sprintf("Removed save set %s", name)
		return savesOpMsg{gameID: gameID, notice: notice, state: mustSavesState(engine, game)}
	}
}

// setSavesPathCmd pins value as the manual save path, or clears the override
// when value is empty (reverting to auto-detection), then reloads.
func (m model) setSavesPathCmd(value string) tea.Cmd {
	engine, game := m.savesEngine, m.detailGame
	gameID := gameIDOf(game)
	return func() tea.Msg {
		if engine == nil || game == nil {
			return savesOpMsg{gameID: gameID, err: errSavesUnavailable}
		}
		notice := "Pinned save path: " + value
		var err error
		if value == "" {
			err = engine.ClearManualPath(game)
			notice = "Cleared manual save path — using auto-detection"
		} else {
			err = engine.SetManualPath(game, value)
		}
		if err != nil {
			return savesOpMsg{gameID: gameID, err: err}
		}
		return savesOpMsg{gameID: gameID, notice: notice, state: mustSavesState(engine, game)}
	}
}

// mustSavesState reloads state after a successful mutation. A reload failure is
// reported as a zero state so the caller shows an error rather than stale data;
// the mutation itself already succeeded.
func mustSavesState(engine *saves.Engine, game *db.Game) savesState {
	state, err := loadSavesState(engine, game)
	if err != nil {
		return savesState{}
	}
	return state
}

// applyResultNotice formats the confirmation shown after an apply or restore,
// including the auto-snapshot note when one was created.
func applyResultNotice(verb string, res *saves.ApplyResult) string {
	if res == nil {
		return verb + " save set"
	}
	notice := fmt.Sprintf("%s %s to %s (%d files)",
		verb, savesDisplayName(res.Set), res.Target.Path, res.FilesCopied)
	if res.AutoSnapshot != "" {
		notice += fmt.Sprintf(" — auto-snapshotted your saves as %s", res.AutoSnapshot)
	}
	return notice
}

// ─── Key handling ──────────────────────────────────────────────────────────

// handleSavesKey handles keys on the Saves screen. The screen has its own key
// scope, so it never falls through to the detail or library handlers.
func (m model) handleSavesKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "left", "backspace":
		return m.leaveSaves()
	case "?":
		m.showHelp = true
		return m, nil
	case "up", "k":
		m.moveSavesCursor(-1)
		return m, nil
	case "down", "j":
		m.moveSavesCursor(1)
		return m, nil
	case "a":
		if m.savesLoading || len(m.savesSets) == 0 {
			return m, nil
		}
		set := m.savesSets[m.savesCursor]
		if set.Active {
			m.notice = fmt.Sprintf("%s is already the active set", savesDisplayName(set))
			return m, nil
		}
		m.savesLoading = true
		m.err = nil
		return m, m.applySaveSetCmd(set.Name)
	case "b":
		if m.savesLoading {
			return m, nil
		}
		m.savesLoading = true
		m.err = nil
		return m, m.snapshotSavesCmd()
	case "r":
		if m.savesLoading {
			return m, nil
		}
		m.savesLoading = true
		m.err = nil
		return m, m.restoreSavesCmd()
	case "p":
		if m.savesLoading {
			return m, nil
		}
		return m.startSavesPathEdit()
	case "d":
		if m.savesLoading || len(m.savesSets) == 0 {
			return m, nil
		}
		set := m.savesSets[m.savesCursor]
		m.savesConfirmDelete = true
		m.savesDeleteName = set.Name
		m.savesDeleteLabel = savesDisplayName(set)
		return m, nil
	}
	return m, nil
}

// leaveSaves returns to the detail view, discarding any transient overlays.
func (m model) leaveSaves() (tea.Model, tea.Cmd) {
	m.viewMode = DetailView
	m.savesConfirmDelete = false
	m.savesEditingPath = false
	m.savesLoading = false
	m.savesDeleteName = ""
	m.savesDeleteLabel = ""
	m.err = nil
	return m, nil
}

// handleSavesDeleteConfirm resolves the y/N confirmation for removing a set.
func (m model) handleSavesDeleteConfirm(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "y", "Y":
		name := m.savesDeleteName
		m.savesConfirmDelete = false
		m.savesDeleteName = ""
		m.savesDeleteLabel = ""
		m.savesLoading = true
		m.err = nil
		return m, m.removeSaveSetCmd(name)
	case "n", "N", "esc", "enter":
		m.savesConfirmDelete = false
		m.savesDeleteName = ""
		m.savesDeleteLabel = ""
		return m, nil
	}
	return m, nil
}

// startSavesPathEdit opens the path text input, prefilled with the manual
// override when one exists, else the auto-resolved path.
func (m model) startSavesPathEdit() (tea.Model, tea.Cmd) {
	pi := textinput.New()
	pi.SetValue(m.savesPathPrefill())
	pi.Focus()
	pi.CharLimit = 1024
	pi.Prompt = "Path: "
	pi.PromptStyle = accentStyle
	m.savesPathInput = pi
	m.savesEditingPath = true
	m.err = nil
	return m, textinput.Blink
}

// handleSavesPathInput drives the path text input. Enter pins the value; an
// empty submission clears the override and reverts to auto-detection.
func (m model) handleSavesPathInput(msg tea.KeyMsg, key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		m.savesEditingPath = false
		m.err = nil
		return m, nil
	case "enter":
		value := strings.TrimSpace(m.savesPathInput.Value())
		m.savesEditingPath = false
		m.savesLoading = true
		m.err = nil
		return m, m.setSavesPathCmd(value)
	}
	var cmd tea.Cmd
	m.savesPathInput, cmd = m.savesPathInput.Update(msg)
	return m, cmd
}

// ─── State helpers ─────────────────────────────────────────────────────────

// applySavesState installs a freshly loaded state and clamps the cursor.
func (m *model) applySavesState(state savesState) {
	m.savesSets = state.sets
	m.savesLocation = state.location
	m.savesCandidates = state.candidates
	m.savesManualPath = state.manual
	m.clampSavesCursor()
}

// resetSavesState clears every per-game field of the Saves screen and marks it
// loading. Opening the screen for a game must call this first so the previous
// game's rows can never render under the new game's title while the load is in
// flight, and so mutating keys cannot act on a stale selection.
func (m *model) resetSavesState() {
	m.savesSets = nil
	m.savesLocation = saves.Location{}
	m.savesCandidates = nil
	m.savesManualPath = ""
	m.savesCursor = 0
	m.savesLoading = true
	m.savesConfirmDelete = false
	m.savesDeleteName = ""
	m.savesDeleteLabel = ""
	m.savesEditingPath = false
}

// clampSavesCursor keeps the cursor inside the current set list.
func (m *model) clampSavesCursor() {
	n := len(m.savesSets)
	if n == 0 {
		m.savesCursor = 0
		return
	}
	if m.savesCursor < 0 {
		m.savesCursor = 0
	}
	if m.savesCursor >= n {
		m.savesCursor = n - 1
	}
}

// moveSavesCursor moves the selection by delta, wrapping at both ends.
func (m *model) moveSavesCursor(delta int) {
	n := len(m.savesSets)
	if n == 0 {
		m.savesCursor = 0
		return
	}
	m.savesCursor += delta
	if m.savesCursor < 0 {
		m.savesCursor = n - 1
	}
	if m.savesCursor >= n {
		m.savesCursor = 0
	}
}

// savesPathPrefill is the initial value for the path input.
func (m model) savesPathPrefill() string {
	if m.savesManualPath != "" {
		return m.savesManualPath
	}
	return m.savesLocation.Path
}

// ─── Display helpers ───────────────────────────────────────────────────────

// savesDisplayName renders a set as "name (label)" when the label adds
// information, else just the name.
func savesDisplayName(s db.SaveSet) string {
	if s.Label != "" && s.Label != s.Name {
		return fmt.Sprintf("%s (%s)", s.Name, s.Label)
	}
	return s.Name
}

// savesLocationText renders the live save location: the manual override when
// pinned, the auto-resolved path when known, or a hint when detection failed.
func savesLocationText(loc saves.Location, manual string) string {
	switch {
	case manual != "":
		return manual + "  (pinned)"
	case loc.Path == "":
		return "not detected — press [p] to pin a path"
	case loc.Note != "":
		return fmt.Sprintf("%s  (%s)", loc.Path, loc.Note)
	default:
		return loc.Path
	}
}

// savesStatusLine derives the "Current:" line: the active set, or "my saves"
// when the live data is the player's own.
func savesStatusLine(sets []db.SaveSet) string {
	for _, s := range sets {
		if !s.Active {
			continue
		}
		name := s.Label
		if name == "" {
			name = s.Name
		}
		if s.Source != "" {
			return fmt.Sprintf("Current: %s (%s)", name, s.Source)
		}
		return "Current: " + name
	}
	return "Current: my saves"
}

// ─── View ──────────────────────────────────────────────────────────────────

func (m model) savesView() string {
	var b strings.Builder
	w := m.width
	if w <= 0 {
		w = 80
	}

	// ── Header ────────────────────────────────────────────────────
	back := titleStyle.Render("◂  Back to Game  ")
	hint := subtleStyle.Render("[Esc] back  [a] apply  [b] snapshot  [r] restore  [p] path  [d] remove  [?] help")
	gap := max(0, w-lipgloss.Width(back)-lipgloss.Width(hint)-4)
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, back, strings.Repeat(" ", gap), hint))
	b.WriteString("\n")
	b.WriteString(separatorStyle.Render(strings.Repeat("━", max(0, w-4))))
	b.WriteString("\n\n")

	// ── Game + resolved save location ─────────────────────────────
	gameTitle := "?"
	if m.detailGame != nil {
		gameTitle = m.detailGame.Title
	}
	b.WriteString(sectionHeaderStyle.Render("  Saves — " + truncate(gameTitle, 60) + "  "))
	b.WriteString("\n")

	locText := savesLocationText(m.savesLocation, m.savesManualPath)
	locStyle := valueStyle
	if m.savesManualPath == "" && m.savesLocation.Path == "" {
		locStyle = subtleStyle
	}
	b.WriteString(labelStyle.Render("Location:"))
	b.WriteString("  ")
	b.WriteString(locStyle.Render(locText))
	b.WriteString("\n")

	if m.savesManualPath == "" && m.savesLocation.Path == "" && len(m.savesCandidates) > 0 {
		b.WriteString(subtleStyle.Render("  Candidates:"))
		b.WriteString("\n")
		for _, c := range m.savesCandidates {
			note := c.Note
			if note == "" {
				note = "-"
			}
			b.WriteString(fmt.Sprintf("    %s  %s\n",
				subtleStyle.Render(c.Path),
				subtleStyle.Render(fmt.Sprintf("(%s, conf %.2f)", note, c.Confidence))))
		}
	}

	// ── Current status ────────────────────────────────────────────
	b.WriteString("\n")
	b.WriteString(accentStyle.Render("  " + savesStatusLine(m.savesSets)))
	b.WriteString("\n\n")

	// ── Set list ──────────────────────────────────────────────────
	if len(m.savesSets) == 0 {
		b.WriteString(subtleStyle.Render("  No save sets yet — press [b] to snapshot your current saves  "))
		b.WriteString("\n")
	} else {
		b.WriteString(subtleStyle.Render(fmt.Sprintf("  %d save set(s) — [↑/↓] select", len(m.savesSets))))
		b.WriteString("\n\n")
		b.WriteString(m.renderSaveSetRows())
	}

	if m.savesLoading {
		b.WriteString("\n")
		b.WriteString(subtleStyle.Render("  … working"))
	}

	m.renderMessage(&b)

	if m.savesConfirmDelete {
		b.WriteString("\n\n")
		b.WriteString(m.renderSavesDeletePrompt())
	}

	if m.savesEditingPath {
		b.WriteString("\n\n")
		b.WriteString(accentStyle.Render("  Live save path"))
		b.WriteString("\n")
		b.WriteString(subtleStyle.Render("  Enter a directory to pin it, or submit an empty value to clear and use auto-detection."))
		b.WriteString("\n\n")
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(purple).
			Padding(0, 1).
			Width(max(0, w-8)).
			Render(m.savesPathInput.View())
		b.WriteString(box)
	}

	return appStyle.Render(b.String())
}

// renderSaveSetRows renders the set list, one row per set, with the cursor row
// highlighted and the active set marked with a green dot.
func (m model) renderSaveSetRows() string {
	var b strings.Builder
	for i, s := range m.savesSets {
		selected := i == m.savesCursor

		// The active marker is only colorized on unselected rows: a nested
		// style inside the highlighted row would reset its background.
		marker := " "
		if s.Active {
			if selected {
				marker = "●"
			} else {
				marker = greenStyle.Render("●")
			}
		}

		label := s.Label
		if label == "" {
			label = "-"
		}
		source := s.Source
		if source == "" {
			source = "-"
		}
		version := s.GameVersion
		if version == "" {
			version = "-"
		}
		created := "-"
		if !s.CreatedAt.IsZero() {
			created = s.CreatedAt.Format("2006-01-02")
		}
		row := fmt.Sprintf("%s %-24s %-22s %-14s %9s   %-8s %s",
			marker,
			truncate(s.Name, 24),
			truncate(label, 22),
			truncate(source, 14),
			formatSize(s.SizeBytes),
			truncate(version, 8),
			created,
		)
		if selected {
			b.WriteString(savesRowSelectedStyle.Render(" " + row))
		} else {
			b.WriteString(" " + row)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// renderSavesDeletePrompt returns the y/N confirmation for removing a set.
func (m model) renderSavesDeletePrompt() string {
	if m.savesDeleteLabel == "" {
		return deletePromptStyle.Render("  Remove this save set?  (y/N)  ")
	}
	return deletePromptStyle.Render(fmt.Sprintf("  Remove save set '%s'?  (y/N)  ", truncate(m.savesDeleteLabel, 40)))
}
