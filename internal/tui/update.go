package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// ─── Update ────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Clear transient notice from the previous frame.
	m.notice = ""

	var cmd tea.Cmd

	switch msg := msg.(type) {
	// ── Window resize ────────────────────────────────────────────
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.reflowTable()
		return m, nil

	// ── Key events ───────────────────────────────────────────────
	case tea.KeyMsg:
		key := msg.String()

		// Global force-quit works everywhere, always.
		if key == "ctrl+c" {
			return m, tea.Quit
		}

		// ── Help overlay: any key dismisses ─────────────────
		if m.showHelp {
			m.showHelp = false
			return m, nil
		}

		// ── Delete confirmation overlay ─────────────────────
		if m.confirmDelete {
			return m.handleDeleteConfirm(key)
		}

		// ── URL input overlay ───────────────────────────────
		if m.setUrl {
			return m.handleUrlInput(msg, key)
		}

		// ── Title edit overlay ──────────────────────────────
		if m.editing {
			return m.handleEditKey(msg, key)
		}

		// ── Detail view key handlers ────────────────────────
		if m.viewMode == DetailView {
			return m.handleDetailKey(key)
		}

		// ── Filter input focused ────────────────────────────
		if m.filterInput.Focused() {
			return m.handleFilterInput(msg, key)
		}

		// ── Library view key handlers ───────────────────────
		switch key {
		case "q":
			return m, tea.Quit
		case "/":
			m.filterInput.Focus()
			m.filterInput.SetValue(m.filterText)
			m.filterInput.CursorEnd()
			return m, textinput.Blink
		case "s":
			m.cycleSort()
			m.rebuildFiltered()
			return m, nil
		case "ctrl+e":
			m.cycleEngineFilter()
			return m, nil
		case "ctrl+s":
			m.cycleStatusFilter()
			return m, nil
		case "d":
			row := m.table.SelectedRow()
			if len(row) > 0 {
				var id int64
				if n, _ := fmt.Sscanf(row[0], "%d", &id); n > 0 {
					m.deleteID = id
					m.selectedID = id
					m.confirmDelete = true
					m.deleteTitle = ""
					for _, g := range m.allGames {
						if g.ID == id {
							m.deleteTitle = g.Title
							break
						}
					}
				}
			}
			return m, nil
		case "e", "enter":
			row := m.table.SelectedRow()
			if len(row) > 0 {
				var id int64
				if n, _ := fmt.Sscanf(row[0], "%d", &id); n > 0 {
					m.selectedID = id
					m.viewMode = DetailView
					m.err = nil
					m.detailGame = nil // clear stale cache so loading indicator shows
					return m, tea.Batch(m.loadDetailGame(id), m.loadMeta(id))
				}
			}
			return m, nil
		case "?":
			m.showHelp = true
			return m, nil
		default:
			// Type-to-search: any printable character activates the filter
			// and starts typing immediately (no / required).
			if len(key) == 1 {
				r := key[0]
				if r >= 32 && r <= 126 {
					m.filterInput.Focus()
					m.filterInput.SetValue(m.filterText + key)
					m.filterInput.CursorEnd()
					m.filterText = m.filterInput.Value()
					m.filterDirty = true
					return m, tea.Batch(textinput.Blink, m.debouncedFilterTick())
				}
			}
		}

		// Unhandled library-view keys → pass to table for navigation.
		m.table, cmd = m.table.Update(msg)
		return m, cmd

	// ── Async messages ─────────────────────────────────────────
	case gamesLoadedMsg:
		m.allGames = msg.games
		m.rebuildFiltered()
		m.err = nil
		return m, nil

	case metaLoadedMsg:
		m.scrapedMeta = msg.meta
		return m, nil

	case detailGameLoadedMsg:
		m.detailGame = msg.game
		return m, nil

	case gameDeletedMsg:
		if msg.err != nil {
			m.err = fmt.Errorf("delete failed: %w", msg.err)
		} else {
			m.allGames = msg.games
			m.rebuildFiltered()
			m.err = nil
		}
		return m, nil

	case metaScrapedMsg:
		if msg.err != nil {
			m.err = fmt.Errorf("⚠ scrape: %v", msg.err)
		} else if msg.meta != nil {
			m.scrapedMeta = msg.meta
			m.notice = "Metadata refreshed from new URL"
		}
		return m, nil

	case errMsg:
		m.err = msg.err
		return m, nil

	case filterTickMsg:
		if m.filterDirty {
			m.filterDirty = false
			m.rebuildFiltered()
		}
		return m, nil
	}

	return m, cmd
}

// ─── Sub-handlers ──────────────────────────────────────────────────────────

func (m model) handleDeleteConfirm(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "y", "Y":
		m.err = nil
		// Run delete + reload asynchronously so the UI stays responsive.
		// The detail view makes a synchronous GetGame() call during
		// rendering, so we switch back to LibraryView immediately to
		// avoid looking up the now-deleted game.
		id := m.deleteID
		m.confirmDelete = false
		m.viewMode = LibraryView
		m.detailGame = nil
		m.scrapedMeta = nil
		m.deleteID = 0
		m.deleteTitle = ""
		return m, m.deleteGame(id)
	case "n", "N", "esc", "enter":
		m.confirmDelete = false
		m.deleteID = 0
		m.deleteTitle = ""
		return m, nil
	}
	return m, nil
}

func (m model) handleEditKey(msg tea.KeyMsg, key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		m.editing = false
		return m, nil
	case "enter":
		newTitle := strings.TrimSpace(m.editInput.Value())
		if newTitle != "" {
			game, err := m.db.GetGame(m.selectedID)
			if err == nil && game != nil {
				game.Title = newTitle
				if err := m.db.UpdateGame(game); err != nil {
					m.err = fmt.Errorf("rename failed: %w", err)
				} else {
					m.err = nil
				}
			}
		}
		m.editing = false
		return m, tea.Batch(m.loadGames(), m.loadDetailGame(m.selectedID), m.loadMeta(m.selectedID))
	}
	var editCmd tea.Cmd
	m.editInput, editCmd = m.editInput.Update(msg)
	return m, editCmd
}

func (m model) handleUrlInput(msg tea.KeyMsg, key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		m.setUrl = false
		m.err = nil
		return m, nil
	case "enter":
		url := strings.TrimSpace(m.urlInput.Value())
		if url != "" {
			game, err := m.db.GetGame(m.selectedID)
			if err == nil && game != nil {
				game.F95URL = url
				if err := m.db.UpdateGame(game); err != nil {
					m.err = fmt.Errorf("URL update failed: %w", err)
					m.setUrl = false
					return m, tea.Batch(m.loadGames(), m.loadDetailGame(m.selectedID), m.loadMeta(m.selectedID))
				}
			}
			m.setUrl = false
			// Trigger scrape of new URL to fetch fresh metadata
			if m.scraperClient != nil {
				m.notice = "URL updated — scraping metadata..."
				return m, tea.Batch(m.loadGames(), m.loadDetailGame(m.selectedID), m.scrapeMeta(m.selectedID, url))
			}
			m.notice = "URL updated"
			return m, tea.Batch(m.loadGames(), m.loadDetailGame(m.selectedID), m.loadMeta(m.selectedID))
		}
	}
	var urlCmd tea.Cmd
	m.urlInput, urlCmd = m.urlInput.Update(msg)
	return m, urlCmd
}

func (m model) handleDetailKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "left", "backspace":
		m.viewMode = LibraryView
		m.detailGame = nil
		m.scrapedMeta = nil
		m.err = nil
		return m, nil
	case "e":
		game, err := m.db.GetGame(m.selectedID)
		if err != nil || game == nil {
			return m, nil
		}
		ei := textinput.New()
		ei.SetValue(game.Title)
		ei.Focus()
		ei.CharLimit = 120
		ei.Prompt = "Title: "
		ei.PromptStyle = accentStyle
		m.editInput = ei
		m.editing = true
		m.err = nil
		return m, textinput.Blink
	case "s":
		game, err := m.db.GetGame(m.selectedID)
		if err != nil || game == nil {
			return m, nil
		}
		game.Status = nextStatus(game.Status)
		if err := m.db.UpdateGame(game); err != nil {
			m.err = fmt.Errorf("status update failed: %w", err)
		} else {
			m.err = nil
		}
		return m, tea.Batch(m.loadGames(), m.loadDetailGame(m.selectedID), m.loadMeta(m.selectedID))
	case "d":
		m.deleteID = m.selectedID
		m.confirmDelete = true
		m.deleteTitle = ""
		for _, g := range m.allGames {
			if g.ID == m.selectedID {
				m.deleteTitle = g.Title
				break
			}
		}
		return m, nil
	case "o":
		game, err := m.db.GetGame(m.selectedID)
		if err != nil || game == nil {
			return m, nil
		}
		m.notice = game.Path
		return m, nil
	case "u":
		game, err := m.db.GetGame(m.selectedID)
		if err != nil || game == nil {
			return m, nil
		}
		ui := textinput.New()
		ui.SetValue(game.F95URL)
		ui.Focus()
		ui.CharLimit = 255
		ui.Prompt = "F95 URL: "
		ui.PromptStyle = accentStyle
		m.urlInput = ui
		m.setUrl = true
		m.err = nil
		return m, textinput.Blink
	case "p":
		game, err := m.db.GetGame(m.selectedID)
		if err != nil || game == nil {
			return m, nil
		}
		exe := findPlayableExe(game.Path, game.ExePath)
		if exe == "" {
			m.err = fmt.Errorf("✗ No executable found in %s", game.Path)
			return m, nil
		}
		if err := launchExe(exe); err != nil {
			m.err = fmt.Errorf("✗ Failed to launch: %v", err)
		} else {
			m.notice = fmt.Sprintf("Launching: %s", filepath.Base(exe))
		}
		return m, nil
	}
	return m, nil
}

func (m model) handleFilterInput(msg tea.KeyMsg, key string) (tea.Model, tea.Cmd) {
	switch key {
	case "enter":
		m.filterInput.Blur()
		m.filterText = m.filterInput.Value()
		m.rebuildFiltered()
		return m, nil
	case "esc":
		m.filterInput.SetValue("")
		m.filterText = ""
		m.filterInput.Blur()
		m.rebuildFiltered()
		return m, nil
	}
	var filterCmd tea.Cmd
	m.filterInput, filterCmd = m.filterInput.Update(msg)
	// Debounced filtering: defer rebuild so rapid keystrokes batch.
	m.filterText = m.filterInput.Value()
	m.filterDirty = true
	return m, tea.Batch(filterCmd, m.debouncedFilterTick())
}

// debouncedFilterTick returns a command that triggers a deferred rebuild
// after 150 ms, allowing rapid keystrokes to settle.
func (m model) debouncedFilterTick() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(t time.Time) tea.Msg {
		return filterTickMsg{}
	})
}

// ─── Cycle helpers ─────────────────────────────────────────────────────────

func (m *model) cycleSort() {
	switch m.sortBy {
	case SortID:
		m.sortBy = SortTitle
	case SortTitle:
		m.sortBy = SortEngine
	case SortEngine:
		m.sortBy = SortVersion
	case SortVersion:
		m.sortBy = SortID
	}
}

func (m *model) cycleEngineFilter() {
	engines := []string{"", "Unity", "RenPy", "RPGM", "UnrealEngine", "HTML", "Java", "Flash", "Others", "ADRIFT", "QSP", "RAGS", "Tads", "WebGL", "WolfRPG"}
	for i, e := range engines {
		if e == m.engineFilter {
			m.engineFilter = engines[(i+1)%len(engines)]
			m.rebuildFiltered()
			return
		}
	}
	m.engineFilter = engines[0]
	m.rebuildFiltered()
}

func (m *model) cycleStatusFilter() {
	statuses := []string{"", "active", "completed", "abandoned", "on_hold", "unknown"}
	for i, s := range statuses {
		if s == m.statusFilter {
			m.statusFilter = statuses[(i+1)%len(statuses)]
			m.rebuildFiltered()
			return
		}
	}
	m.statusFilter = statuses[0]
	m.rebuildFiltered()
}
