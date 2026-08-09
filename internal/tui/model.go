package tui

import (
	"os"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/filepicker"
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

type scanCompletedMsg struct {
	saved   int
	updated int
	total   int
}

// downloadProgressMsg is a periodic tick sent by pollDownloads to trigger
// re-renders while a download is active. All progress data lives in the
// model's activeDownloads map (written by the download goroutine), so the
// message itself carries no payload.
type downloadProgressMsg struct{}

type downloadStartedMsg struct {
	gameID int64
	err    error
}

// downloadLinksMsg carries the result of resolving a game's download links.
// Resolution runs in a background goroutine: the fallback path scrapes
// F95Zone, which must never block the Update layer.
type downloadLinksMsg struct {
	gameID   int64
	links    []db.DownloadLink
	destDir  string
	gamePath string
	engine   string
	err      error
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
	stepMsg  string // "Finding suitable host...", "Trying Pixeldrain...", etc.
}

// ─── Model ─────────────────────────────────────────────────────────────────

type model struct {
	db          *db.Database
	table       table.Model
	filterInput textinput.Model

	allGames      []db.GameSummary
	filtered      []db.GameSummary
	viewMode      ViewMode
	selectedID    int64
	filterText    string
	sortBy        SortField
	sortDesc      bool // r toggles: reverse the current sort direction
	showHelp      bool
	width         int
	height        int
	err           error
	notice        string
	filterDirty   bool
	confirmDelete bool
	deleteID      int64
	deleteTitle   string

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
	engineFilter     string
	statusFilter     string
	collectionFilter int64  // 0 = no filter
	collectionName   string // name of the currently selected collection
	collections      []db.Collection

	// active downloads
	activeDownloadsMu *sync.Mutex
	activeDownloads   map[int64]*activeDownload

	// startup tip (auto-dismissed)
	showStartupTip bool

	// spinner for background operations
	spinner       spinner.Model
	spinnerActive bool

	// file picker mode
	showFilePicker bool
	filePicker     filepicker.Model
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
	initStyles()

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

	fp := filepicker.New()
	fp.AllowedTypes = []string{}
	fp.DirAllowed = true
	fp.FileAllowed = false
	fp.CurrentDirectory = os.Getenv("HOME")
	fp.Height = 15
	fp.AutoHeight = false
	fp.Styles = filepicker.Styles{
		File:             lipgloss.NewStyle().Foreground(white),
		DisabledFile:     lipgloss.NewStyle().Foreground(subtle),
		Directory:        lipgloss.NewStyle().Foreground(purple).Bold(true),
		Symlink:          lipgloss.NewStyle().Foreground(cyan),
		Selected:         lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Background(purpleBg).Bold(true),
		Cursor:           lipgloss.NewStyle().Foreground(purple),
		DisabledCursor:   lipgloss.NewStyle().Foreground(subtle),
		DisabledSelected: lipgloss.NewStyle().Foreground(subtle),
		Permission:       lipgloss.NewStyle().Foreground(subtle),
		FileSize:         subtleStyle.Copy(),
		EmptyDirectory:   lipgloss.NewStyle().Foreground(subtle).PaddingLeft(2).SetString("  No directories found."),
	}

	return model{
		db:                database,
		table:             t,
		filterInput:       fi,
		sortBy:            SortID,
		scraperClient:     sc,
		f95Cookie:         f95Cookie,
		activeDownloadsMu: &sync.Mutex{},
		activeDownloads:   make(map[int64]*activeDownload),
		detailViewport:    viewport.New(0, 0),
		showStartupTip:    true,
		spinner:           sp,
		showFilePicker:    false,
		filePicker:        fp,
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
	case m.showFilePicker:
		return m.filePickerView()
	case m.viewMode == DetailView:
		return m.detailView()
	default:
		return m.libraryView()
	}
}
