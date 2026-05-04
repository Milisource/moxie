package tui

import (
	"sync"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/downloader"
	"github.com/mili/moxie/internal/scraper"
)

// ─── View Mode ─────────────────────────────────────────────────────────────

type ViewMode int

const (
	LibraryView ViewMode = iota
	DetailView
)

// ─── Sort Field ────────────────────────────────────────────────────────────

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

type metaScrapedMsg struct {
	meta *db.ScrapedMeta
	err  error
}

type detailGameLoadedMsg struct {
	game *db.Game
}

type filterTickMsg struct{}

type downloadProgressMsg struct {
	gameID   int64
	progress downloader.Progress
	status   db.DownloadStatus
	err      string
}

type downloadStartedMsg struct {
	gameID int64
	err    error
}

// ─── Active Download ───────────────────────────────────────────────────────

type activeDownload struct {
	mu       sync.Mutex
	gameID   int64
	url      string
	host     string
	destDir  string
	status   db.DownloadStatus
	progress downloader.Progress
	err      string
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
	err           error
	notice        string
	filterDirty   bool
	confirmDelete bool
	deleteID     int64
	deleteTitle  string

	// detail view
	detailGame  *db.Game
	scrapedMeta *db.ScrapedMeta

	// edit (title rename)
	editing   bool
	editInput textinput.Model

	// url assignment
	setUrl   bool
	urlInput textinput.Model

	// scraper
	scraperClient *scraper.Client

	// filters
	engineFilter string
	statusFilter string

	// active downloads
	activeDownloads map[int64]*activeDownload
}

// initialModel creates the root model with default Bubble Tea components.
func initialModel(database *db.Database, sc *scraper.Client) model {
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
		db:              database,
		table:           t,
		filterInput:     fi,
		sortBy:          SortID,
		scraperClient:   sc,
		activeDownloads: make(map[int64]*activeDownload),
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
