package tui

import (
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
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
	games []db.GameSummary
}

type metaLoadedMsg struct {
	meta *db.ScrapedMeta
}

type gameDeletedMsg struct {
	err   error
	games []db.GameSummary
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

type collectionsLoadedMsg struct {
	collections []db.Collection
}

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
	mu          sync.Mutex
	gameID      int64
	url         string
	host        string
	destDir     string
	status      db.DownloadStatus
	progress    downloader.Progress
	err         string
	stepMsg     string // "Finding suitable host...", "Trying Pixeldrain...", etc.
	completedAt time.Time // set when status becomes Completed or Failed
}

// ─── Model ─────────────────────────────────────────────────────────────────

type model struct {
	db          *db.Database
	table       table.Model
	filterInput textinput.Model

	allGames     []db.GameSummary
	filtered     []db.GameSummary
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
	detailGame     *db.Game
	scrapedMeta    *db.ScrapedMeta
	detailViewport viewport.Model

	// edit (title rename)
	editing   bool
	editInput textinput.Model

	// exe path editing
	editingExe bool
	exeInput   textinput.Model

	// url assignment
	setUrl   bool
	urlInput textinput.Model

	// scraper
	scraperClient *scraper.Client

	// f95 cookie for authenticating download HEAD requests
	f95Cookie string

	// filters
	engineFilter      string
	statusFilter      string
	collectionFilter  int64  // 0 = no filter
	collectionName    string // name of the currently selected collection
	collections       []db.Collection

	// active downloads
	activeDownloadsMu *sync.Mutex
	activeDownloads   map[int64]*activeDownload

	// startup tip (auto-dismissed)
	showStartupTip bool

	// spinner for background operations
	spinner        spinner.Model
	spinnerActive  bool
}

// startupTipExpiredMsg is sent when the startup tip timer completes.
type startupTipExpiredMsg struct{}

// dismissStartupTip returns a tea.Cmd that sends startupTipExpiredMsg after 6 seconds.
func dismissStartupTip() tea.Cmd {
	return tea.Tick(6000*1000000, func(t time.Time) tea.Msg {
		return startupTipExpiredMsg{}
	})
}

// initialModel creates the root model with default Bubble Tea components.
func initialModel(database *db.Database, sc *scraper.Client, f95Cookie string) model {
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

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(purple)

	return model{
		db:                database,
		table:             t,
		filterInput:       fi,
		sortBy:            SortID,
		scraperClient:     sc,
		f95Cookie:         f95Cookie,
		activeDownloadsMu: &sync.Mutex{},
		activeDownloads:    make(map[int64]*activeDownload),
		detailViewport:     viewport.New(0, 0),
		showStartupTip:     true,
		spinner:            sp,
	}
}

// ─── Init ──────────────────────────────────────────────────────────────────

func (m model) Init() tea.Cmd {
	return tea.Batch(m.loadGames(), dismissStartupTip())
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
