package tui

import (
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mili/moxie/internal/db"
)

// ─── View Mode ─────────────────────────────────────────────────────────────

// ViewMode represents which screen the TUI is currently showing.
type ViewMode int

const (
	LibraryView ViewMode = iota
	DetailView
)

// ─── Sort Field ────────────────────────────────────────────────────────────

// SortField represents the field to sort the game list by.
type SortField int

const (
	SortID SortField = iota
	SortTitle
	SortEngine
	SortVersion
)

func (s SortField) String() string {
	switch s {
	case SortID:
		return "ID"
	case SortTitle:
		return "Title"
	case SortEngine:
		return "Engine"
	case SortVersion:
		return "Version"
	default:
		return "ID"
	}
}

// Indicator returns the sort field name with a direction arrow.
func (s SortField) Indicator() string {
	switch s {
	case SortID:
		return "ID ↑"
	case SortTitle:
		return "Title ↑"
	case SortEngine:
		return "Engine ↑"
	case SortVersion:
		return "Version ↓"
	default:
		return "ID ↑"
	}
}

// ─── Custom Messages ───────────────────────────────────────────────────────

type gamesLoadedMsg struct {
	games []db.Game
}

type metaLoadedMsg struct {
	meta *db.ScrapedMeta
}

type gameDeletedMsg struct {
	err   error
	games []db.Game
}

type errMsg struct {
	err error
}

// ─── Model ─────────────────────────────────────────────────────────────────

type model struct {
	db          *db.Database
	table       table.Model
	filterInput textinput.Model

	allGames     []db.Game
	filtered     []db.Game
	viewMode     ViewMode
	selectedID   int64
	filterText   string
	sortBy       SortField
	showHelp     bool
	width        int
	height       int
	err          error
	confirmDelete bool
	deleteID     int64
	deleteTitle  string

	// detail view
	scrapedMeta *db.ScrapedMeta

	// edit (title rename)
	editing   bool
	editInput textinput.Model

	// url assignment
	setUrl   bool
	urlInput textinput.Model

	// filters
	engineFilter string // empty = all
	statusFilter string // empty = all
}

// initialModel creates the root model with default Bubble Tea components.
func initialModel(database *db.Database) model {
	cols := []table.Column{
		{Title: "ID", Width: 5},
		{Title: "Title", Width: 48},
		{Title: "Engine", Width: 16},
		{Title: "Version", Width: 10},
		{Title: "Status", Width: 12},
	}

	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
		table.WithHeight(15),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(purple).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(white).
		Background(purpleBg).
		Bold(false)
	s.Cell = s.Cell.
		Foreground(lipgloss.Color("252"))
	t.SetStyles(s)

	fi := textinput.New()
	fi.Placeholder = "filter by title…"
	fi.Prompt = "🔍 "
	fi.CharLimit = 60
	fi.PromptStyle = lipgloss.NewStyle().Foreground(purple)
	fi.PlaceholderStyle = lipgloss.NewStyle().Foreground(subtle)

	return model{
		db:          database,
		table:       t,
		filterInput: fi,
		sortBy:      SortID,
	}
}

// ─── Init ──────────────────────────────────────────────────────────────────

func (m model) Init() tea.Cmd {
	return m.loadGames()
}

// ─── View ──────────────────────────────────────────────────────────────────

func (m model) View() string {
	switch {
	case m.showHelp:
		return m.helpView()
	case m.viewMode == DetailView:
		return m.detailView()
	default:
		return m.libraryView()
	}
}
