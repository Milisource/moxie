package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/mili/moxie/internal/browser"
	"github.com/mili/moxie/internal/config"
	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/downloader"
	"github.com/mili/moxie/internal/extractor"
	"github.com/mili/moxie/internal/engine"
	"github.com/mili/moxie/internal/launcher"
	"github.com/mili/moxie/internal/log"
	"github.com/mili/moxie/internal/scanner"
	"github.com/mili/moxie/internal/scraper"
	"github.com/mili/moxie/internal/steam"
	"github.com/mili/moxie/internal/updater"
)

// App is the main application struct for the Wails desktop GUI.
// Its exported methods are bound to the frontend and callable from JavaScript.
type App struct {
	ctx        context.Context
	db         *db.Database
	watcherMu  sync.Mutex // guards watcher; Wails dispatches each bound call on its own goroutine
	watcher    *DirectoryWatcher
	startupErr string // non-empty if startup failed; exposed to frontend

	// Long-running work started by bound methods (scan, sync, game updates)
	// runs on goroutines that outlive the call. They all derive from bgCtx and
	// register with bgWG so shutdown can cancel them and wait before the
	// database is closed out from under them.
	bgCtx    context.Context
	bgCancel context.CancelFunc
	bgWG     sync.WaitGroup

	// updateRunning serialises the game-update pipeline. Two concurrent runs
	// would extract and merge into the same game directory at once, so both
	// the single and batch entry points take this.
	updateRunning atomic.Bool

	// updateCancel aborts the in-flight update run. Guarded by updateCancelMu
	// because CancelGameUpdate arrives on a different goroutine than the one
	// that installs it.
	updateCancelMu sync.Mutex
	updateCancel   context.CancelFunc
}

// NewApp creates a new App instance.
func NewApp() *App {
	return &App{}
}

// startup is called when the Wails runtime starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.bgCtx, a.bgCancel = context.WithCancel(context.Background())

	log.InitWithConsole(config.LogDir())
	slog.Info("moxie desktop starting")

	if err := os.MkdirAll(config.ConfigDir(), 0755); err != nil {
		slog.Error("failed to create config directory", "error", err)
		a.startupErr = fmt.Sprintf("Cannot create config directory: %v", err)
		return
	}

	database, err := db.Open(config.DbPath())
	if err != nil {
		slog.Error("failed to open database", "error", err)
		a.startupErr = fmt.Sprintf("Cannot open database: %v", err)
		return
	}
	a.db = database
	slog.Info("database opened successfully")

	a.startWatcher()
}

// startWatcher begins watching configured scan paths for filesystem changes.
func (a *App) startWatcher() {
	a.watcherMu.Lock()
	defer a.watcherMu.Unlock()
	a.startWatcherLocked()
}

// startWatcherLocked is startWatcher with a.watcherMu already held.
func (a *App) startWatcherLocked() {
	paths := a.GetScanPaths()
	if len(paths) == 0 {
		return
	}
	w := NewDirectoryWatcher(a)
	if err := w.Start(paths); err != nil {
		slog.Warn("failed to start directory watcher", "error", err)
		return
	}
	a.watcher = w
	slog.Info("directory watcher started", "paths", paths)
}

// stopWatcherLocked stops and clears the watcher with a.watcherMu already held.
func (a *App) stopWatcherLocked() {
	if a.watcher == nil {
		return
	}
	_ = a.watcher.Stop()
	a.watcher = nil
}

// restartWatcher stops and restarts the directory watcher to pick up
// changes to the configured scan paths.
func (a *App) restartWatcher() {
	a.watcherMu.Lock()
	defer a.watcherMu.Unlock()
	a.stopWatcherLocked()
	a.startWatcherLocked()
}

// bgShutdownGrace bounds how long shutdown waits for background work to
// unwind. A download or extraction may not notice cancellation instantly, but
// the window must stay short enough that quitting still feels immediate.
const bgShutdownGrace = 5 * time.Second

// goBackground runs fn on a tracked goroutine with the app-wide background
// context. Panics are logged rather than taking down the whole desktop app.
func (a *App) goBackground(name string, fn func(ctx context.Context)) {
	a.bgWG.Add(1)
	go func() {
		defer a.bgWG.Done()
		defer func() {
			if r := recover(); r != nil {
				slog.Error("background task panic", "task", name, "recover", r)
				runtime.EventsEmit(a.ctx, name+":error", map[string]string{
					"error": fmt.Sprintf("internal error: %v", r),
				})
			}
		}()
		fn(a.bgCtx)
	}()
}

// shutdown is called when the application is closing.
func (a *App) shutdown(ctx context.Context) {
	// Stop blocks until any in-flight auto-scan has finished writing, so the
	// database is not closed out from under it.
	a.watcherMu.Lock()
	a.stopWatcherLocked()
	a.watcherMu.Unlock()

	// Same guarantee for scan/sync/update goroutines: cancel, then wait so
	// their database writes land before Close. Bounded — a stuck network read
	// must not hang the quit.
	if a.bgCancel != nil {
		a.bgCancel()
	}
	done := make(chan struct{})
	go func() {
		a.bgWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(bgShutdownGrace):
		slog.Warn("background tasks did not finish before shutdown; closing database anyway",
			"grace", bgShutdownGrace)
	}

	if a.db != nil {
		if err := a.db.Close(); err != nil {
			slog.Error("error closing database", "error", err)
		}
	}
	slog.Info("moxie desktop shutting down")
}

// ---------------------------------------------------------------------------
// Version / Config
// ---------------------------------------------------------------------------

// appVersion is the desktop app's version. Single source of truth — the
// update check compares against it, so a second literal would silently drift.
const appVersion = "0.4.0-alpha"

// GetVersion returns the current application version.
func (a *App) GetVersion() string {
	return appVersion
}

// GetStartupError returns any error that occurred during app initialization.
// Empty string means startup was successful.
func (a *App) GetStartupError() string {
	return a.startupErr
}

// GetDbPath returns the path to the SQLite database file.
func (a *App) GetDbPath() string {
	return config.DbPath()
}

// GetConfigDir returns the path to the configuration directory.
func (a *App) GetConfigDir() string {
	return config.ConfigDir()
}

// ---------------------------------------------------------------------------
// Data types serialized to the frontend
// ---------------------------------------------------------------------------

// DesktopGameSummary is the game data sent to the frontend for the list view.
type DesktopGameSummary struct {
	ID            int64  `json:"id"`
	Title         string `json:"title"`
	Engine        string `json:"engine"`
	Version       string `json:"version"`
	LatestVersion string `json:"latestVersion"`
	Status        string `json:"status"`
	Path          string `json:"path"`
	ExePath       string `json:"exePath"`
	SizeBytes     int64  `json:"sizeBytes"`
	SizeLabel     string `json:"sizeLabel"`
	HasCover      bool   `json:"hasCover"`
}

// DesktopGameDetail is the full game data for the detail view.
type DesktopGameDetail struct {
	DesktopGameSummary
	Developer   string                `json:"developer"`
	Overview    string                `json:"overview"`
	CoverURL    string                `json:"coverUrl"`
	F95URL      string                `json:"f95Url"`
	Tags        []string              `json:"tags"`
	Notes       string                `json:"notes"`
	StoreLinks  map[string]string     `json:"storeLinks"`
	SteamAppID  int64                 `json:"steamAppId"`
	WinePrefix  string                `json:"winePrefix"`
	DownloadLinks []DesktopDownloadLink `json:"downloadLinks"`
	PlayHistory []DesktopPlayEntry    `json:"playHistory"`
}

// DesktopDownloadLink is a download link for the detail view.
type DesktopDownloadLink struct {
	ID       int64  `json:"id"`
	URL      string `json:"url"`
	Host     string `json:"host"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	IsDead   bool   `json:"isDead"`
}

// DesktopPlayEntry is a play history entry.
type DesktopPlayEntry struct {
	PlayedAt  string `json:"playedAt"`
	Platform  string `json:"platform"`
	DurationS int    `json:"durationS"`
}

// DesktopDownloadLinkWithGame pairs a download link with its parent game info.
type DesktopDownloadLinkWithGame struct {
	DesktopDownloadLink
	GameID    int64  `json:"gameId"`
	GameTitle string `json:"gameTitle"`
	GamePath  string `json:"gamePath"`
}

// StatusCount is the count of games in a given status.
type StatusCount struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

// ---------------------------------------------------------------------------
// Game list methods
// ---------------------------------------------------------------------------

// GetGames returns all active games.
func (a *App) GetGames() ([]DesktopGameSummary, error) {
	if a.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	games, err := a.db.ListActiveGames("", "")
	if err != nil {
		return nil, fmt.Errorf("failed to list games: %w", err)
	}

	result := make([]DesktopGameSummary, 0, len(games))
	for _, g := range games {
		result = append(result, gameToSummary(&g))
	}
	return result, nil
}

// SearchGames performs FTS5 full-text search across game titles, tags, and developer.
func (a *App) SearchGames(query string) ([]DesktopGameSummary, error) {
	if a.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	games, err := a.db.SearchGames(query)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	result := make([]DesktopGameSummary, 0, len(games))
	for _, g := range games {
		result = append(result, gameToSummary(&g))
	}
	return result, nil
}

// GetGameCount returns the total number of active games.
func (a *App) GetGameCount() (int, error) {
	if a.db == nil {
		return 0, fmt.Errorf("database not initialized")
	}
	return a.db.GameCount()
}

// GetGameCountByStatus returns counts for each status.
func (a *App) GetGameCountByStatus() ([]StatusCount, error) {
	if a.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	counts, err := a.db.CountGamesByStatus()
	if err != nil {
		return nil, err
	}

	result := make([]StatusCount, 0, len(counts))
	for status, count := range counts {
		result = append(result, StatusCount{Status: status, Count: count})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Count > result[j].Count
	})
	return result, nil
}

// ---------------------------------------------------------------------------
// Update / version-check methods
// ---------------------------------------------------------------------------

// GetUpdatableGames returns all games where latestVersion differs from
// the locally installed version (i.e., an update is available on F95Zone).
func (a *App) GetUpdatableGames() ([]DesktopGameSummary, error) {
	if a.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	games, err := a.db.GamesNeedingUpdate()
	if err != nil {
		return nil, fmt.Errorf("failed to list updatable games: %w", err)
	}

	result := make([]DesktopGameSummary, 0, len(games))
	for _, g := range games {
		result = append(result, gameToSummary(&g))
	}
	return result, nil
}

// GetUpdatableCount returns the number of games with a pending version update.
func (a *App) GetUpdatableCount() (int, error) {
	if a.db == nil {
		return 0, fmt.Errorf("database not initialized")
	}

	n, err := a.db.CountGamesNeedingUpdate()
	if err != nil {
		return 0, fmt.Errorf("failed to count updatable games: %w", err)
	}
	return n, nil
}

// ---------------------------------------------------------------------------
// Game detail methods
// ---------------------------------------------------------------------------

// GetGameDetail returns full metadata for a single game.
func (a *App) GetGameDetail(id int64) (*DesktopGameDetail, error) {
	if a.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	game, err := a.db.GetGame(id)
	if err != nil {
		return nil, fmt.Errorf("game not found: %w", err)
	}
	if game == nil {
		return nil, fmt.Errorf("game with id %d not found", id)
	}

	detail := &DesktopGameDetail{
		DesktopGameSummary: gameToSummary(game),
		F95URL:             game.F95URL,
		Tags:               game.Tags,
		Notes:              game.Notes,
		StoreLinks:         game.StoreLinks,
		SteamAppID:         game.SteamAppID,
		WinePrefix:         game.WinePrefix,
	}

	// Scraped metadata
	meta, err := a.db.GetScrapedMeta(id)
	if err == nil && meta != nil {
		detail.Developer = meta.Developer
		detail.Overview = meta.Overview
		detail.CoverURL = meta.CoverURL
	}

	// Download links
	links, err := a.db.ListDownloadLinks(id, "", true)
	if err == nil {
		detail.DownloadLinks = make([]DesktopDownloadLink, 0, len(links))
		for _, l := range links {
			detail.DownloadLinks = append(detail.DownloadLinks, DesktopDownloadLink{
				ID:       l.ID,
				URL:      l.URL,
				Host:     l.Host,
				Name:     l.Name,
				Platform: string(l.Platform),
				IsDead:   l.IsDead,
			})
		}
	}

	// Play history for this game only.
	plays, err := a.db.PlaysForGame(id, 200)
	if err == nil {
		detail.PlayHistory = make([]DesktopPlayEntry, 0, len(plays))
		for _, p := range plays {
			detail.PlayHistory = append(detail.PlayHistory, DesktopPlayEntry{
				PlayedAt:  p.PlayedAt.Format(time.RFC3339),
				Platform:  p.Platform,
				DurationS: p.DurationS,
			})
		}
	}

	return detail, nil
}

// PlayGame resolves a game's executable and launches it, using the
// DB-stored Wine prefix when set, and records a play history entry.
// It returns a short human-readable message for the frontend.
func (a *App) PlayGame(id int64) (string, error) {
	if a.db == nil {
		return "", fmt.Errorf("database not initialized")
	}

	game, err := a.db.GetGame(id)
	if err != nil || game == nil {
		return "", fmt.Errorf("game with id %d not found", id)
	}

	exe := launcher.ResolveExecutable(game.Path, game.ExePath)
	if exe == "" {
		// Virtual game added from F95Zone but not yet downloaded.
		if strings.HasPrefix(game.Path, db.VirtualPathPrefix) {
			return "", fmt.Errorf("%q was added from F95Zone but not yet downloaded. Use Install on its detail page to download it.", game.Title)
		}
		return "", fmt.Errorf("no executable found for %q", game.Title)
	}

	if err := launcher.Launch(exe, game.Path, game.WinePrefix); err != nil {
		return "", fmt.Errorf("cannot launch %q: %w", exe, err)
	}

	// Record play history — non-fatal if it fails.
	if err := a.db.RecordPlay(game.ID, goruntime.GOOS); err != nil {
		slog.Warn("failed to record play history", "game_id", game.ID, "error", err)
	}

	return fmt.Sprintf("Launching %s", filepath.Base(exe)), nil
}

// SetGameWinePrefix updates the Wine prefix for a game. An empty string
// clears the stored prefix so launches fall back to the system default.
func (a *App) SetGameWinePrefix(id int64, prefix string) error {
	if a.db == nil {
		return fmt.Errorf("database not initialized")
	}
	game, err := a.db.GetGame(id)
	if err != nil || game == nil {
		return fmt.Errorf("game with id %d not found", id)
	}
	if err := a.db.UpdateGameWinePrefix(id, strings.TrimSpace(prefix)); err != nil {
		return fmt.Errorf("failed to update wine prefix: %w", err)
	}
	slog.Info("wine prefix updated", "game_id", id, "title", game.Title)
	return nil
}

// ---------------------------------------------------------------------------
// Dependencies
// ---------------------------------------------------------------------------

// DependencyStatus describes whether a system dependency is available.
type DependencyStatus struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Details string `json:"details"`
}

// CheckDependencies verifies system dependencies.
func (a *App) CheckDependencies() []DependencyStatus {
	results := []DependencyStatus{}

	results = append(results, DependencyStatus{
		Name:    "Database",
		Status:  "ok",
		Details: config.DbPath(),
	})

	cookieStr, err := browser.GetF95Cookies()
	if err != nil || cookieStr == "" {
		results = append(results, DependencyStatus{
			Name:    "F95Zone Cookies",
			Status:  "not_found",
			Details: "Log into F95Zone in your browser first",
		})
	} else {
		results = append(results, DependencyStatus{
			Name:    "F95Zone Cookies",
			Status:  "ok",
			Details: "Browser cookies detected",
		})
	}

	_, err = steam.FindSteamRoot()
	if err != nil {
		results = append(results, DependencyStatus{
			Name:    "Steam",
			Status:  "not_found",
			Details: "Steam not detected: " + err.Error(),
		})
	} else {
		results = append(results, DependencyStatus{
			Name:    "Steam",
			Status:  "ok",
			Details: "Steam installation found",
		})
	}

	return results
}

// ---------------------------------------------------------------------------
// Scan methods
// ---------------------------------------------------------------------------

// ScanProgress is emitted as a Wails event during scanning.
type ScanProgress struct {
	DirsExamined int    `json:"dirsExamined"`
	GamesFound   int    `json:"gamesFound"`
	Phase        string `json:"phase"` // "walk" or "detect"
}

// ScanResult is emitted when a scan completes.
type ScanResult struct {
	GamesFound int      `json:"gamesFound"`
	Inserted   int      `json:"inserted"`
	Updated    int      `json:"updated"`
	Errors     []string `json:"errors"`
}

// ScanDirectory scans a directory and emits progress events.
func (a *App) ScanDirectory(path string) error {
	if a.db == nil {
		return fmt.Errorf("database not initialized")
	}

	// games.path is UNIQUE and everything downstream (watcher root matching,
	// removeMissingUnder) compares absolute paths, so normalise here rather
	// than storing whatever the caller happened to type.
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolving scan path: %w", err)
	}

	a.goBackground("scan", func(ctx context.Context) {
		progress := func(dirsExamined, gamesFound int, phase string) {
			runtime.EventsEmit(a.ctx, "scan:progress", ScanProgress{
				DirsExamined: dirsExamined,
				GamesFound:   gamesFound,
				Phase:        phase,
			})
		}

		detected, err := scanner.ScanFiltered(abs, nil, progress)
		if err != nil {
			runtime.EventsEmit(a.ctx, "scan:error", map[string]string{
				"error": err.Error(),
			})
			return
		}

		// Share the watcher's upsert path. Inserting blindly here would hit the
		// UNIQUE constraint on games.path for every already-known game, so
		// re-scanning a directory used to report zero found and N errors.
		if ctx.Err() != nil {
			return
		}
		inserted, updated, errs := a.upsertDetected(detected)

		runtime.EventsEmit(a.ctx, "scan:complete", ScanResult{
			GamesFound: len(detected),
			Inserted:   inserted,
			Updated:    updated,
			Errors:     errs,
		})
	})

	return nil
}

// GetScanPaths returns saved scan directories from config.
func (a *App) GetScanPaths() []string {
	cfg, err := config.ReadConfig()
	if err != nil {
		return nil
	}
	return cfg.ScanPaths
}

// AddScanPath saves a directory to the scan paths config.
func (a *App) AddScanPath(path string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		cfg = &config.Config{}
	}

	// Check for duplicates
	for _, p := range cfg.ScanPaths {
		if p == path {
			return nil // already exists
		}
	}

	cfg.ScanPaths = append(cfg.ScanPaths, path)
	if err := config.WriteConfig(cfg); err != nil {
		return err
	}
	a.restartWatcher()
	return nil
}

// RemoveScanPath removes a directory from the scan paths config.
func (a *App) RemoveScanPath(path string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		return err
	}

	filtered := make([]string, 0, len(cfg.ScanPaths))
	for _, p := range cfg.ScanPaths {
		if p != path {
			filtered = append(filtered, p)
		}
	}
	cfg.ScanPaths = filtered
	if err := config.WriteConfig(cfg); err != nil {
		return err
	}
	a.restartWatcher()
	return nil
}

// PickDirectory opens a native directory picker dialog via Wails runtime.
// Returns the selected directory path, or empty string if cancelled.
func (a *App) PickDirectory() string {
	if a.ctx == nil {
		return ""
	}
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Game Directory",
	})
	if err != nil {
		slog.Error("directory picker failed", "error", err)
		return ""
	}
	return dir
}

// ---------------------------------------------------------------------------
// Download management methods
// ---------------------------------------------------------------------------

// GetGameDownloadLinks returns download links for a specific game.
func (a *App) GetGameDownloadLinks(gameID int64) ([]DesktopDownloadLink, error) {
	if a.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	links, err := a.db.ListDownloadLinks(gameID, "", true)
	if err != nil {
		return nil, fmt.Errorf("failed to list download links: %w", err)
	}

	result := make([]DesktopDownloadLink, 0, len(links))
	for _, l := range links {
		result = append(result, DesktopDownloadLink{
			ID:       l.ID,
			URL:      l.URL,
			Host:     l.Host,
			Name:     l.Name,
			Platform: string(l.Platform),
			IsDead:   l.IsDead,
		})
	}
	return result, nil
}

// OpenDownloadURL opens a download link's URL in the system browser.
func (a *App) OpenDownloadURL(linkID int64) error {
	if a.db == nil {
		return fmt.Errorf("database not initialized")
	}
	if a.ctx == nil {
		return fmt.Errorf("application context not initialized")
	}

	link, err := a.db.GetDownloadLink(linkID)
	if err != nil {
		return fmt.Errorf("failed to get download link: %w", err)
	}
	if link == nil {
		return fmt.Errorf("download link with id %d not found", linkID)
	}

	runtime.BrowserOpenURL(a.ctx, link.URL)
	return nil
}

// GetGamesWithDownloadLinks returns all active games that have at least one
// download link in the database.
func (a *App) GetGamesWithDownloadLinks() ([]DesktopGameSummary, error) {
	if a.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	allLinks, err := a.db.AllDownloadLinks(true)
	if err != nil {
		return nil, fmt.Errorf("failed to query download links: %w", err)
	}

	// Deduplicate by game ID while preserving title and sorting.
	seen := make(map[int64]bool)
	var result []DesktopGameSummary
	for _, l := range allLinks {
		if seen[l.GameID] {
			continue
		}
		seen[l.GameID] = true
		result = append(result, DesktopGameSummary{
			ID:    l.GameID,
			Title: l.GameTitle,
			Path:  l.GamePath,
		})
	}

	return result, nil
}

// GetAllDownloadLinks returns all download links with their associated game
// title and path, ordered by game title.
func (a *App) GetAllDownloadLinks() ([]DesktopDownloadLinkWithGame, error) {
	if a.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	links, err := a.db.AllDownloadLinks(true)
	if err != nil {
		return nil, fmt.Errorf("failed to query all download links: %w", err)
	}

	result := make([]DesktopDownloadLinkWithGame, 0, len(links))
	for _, l := range links {
		result = append(result, DesktopDownloadLinkWithGame{
			DesktopDownloadLink: DesktopDownloadLink{
				ID:       l.ID,
				URL:      l.URL,
				Host:     l.Host,
				Name:     l.Name,
				Platform: string(l.Platform),
				IsDead:   l.IsDead,
			},
			GameID:    l.GameID,
			GameTitle: l.GameTitle,
			GamePath:  l.GamePath,
		})
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Game update download
// ---------------------------------------------------------------------------

// downloadGameFile downloads a game update archive from a download link,
// emitting progress events to the frontend. Returns the path to the
// downloaded temp file. The caller is responsible for cleaning up the
// returned temp file (typically an umbrella temp directory).
//
// The ctx parameter supports cancellation; the download is aborted if the
// context is cancelled. The function retries up to 3 times on transient
// network errors with a 2-second backoff between attempts.
func (a *App) downloadGameFile(ctx context.Context, evPrefix string, gameID int64, link db.DownloadLink) (string, error) {
	if a.db == nil {
		return "", fmt.Errorf("database not initialized")
	}
	if a.ctx == nil {
		return "", fmt.Errorf("application context not initialized")
	}

	// Obtain the F95Zone cookie for masked URL resolution.
	cookie, err := browser.GetF95Cookies()
	if err != nil || cookie == "" {
		errMsg := "F95Zone cookies not available. Log into F95Zone in your browser first"
		runtime.EventsEmit(a.ctx, evPrefix+":error", map[string]interface{}{
			"gameID":  gameID,
			"step":    "cookie",
			"message": errMsg,
		})
		return "", fmt.Errorf("%s", errMsg)
	}

	// Create a temp directory to hold the downloaded file.
	// Using MkdirTemp so each download gets its own isolated directory.
	tempDir, err := os.MkdirTemp(os.TempDir(), fmt.Sprintf("moxie-update-%d-*", gameID))
	if err != nil {
		runtime.EventsEmit(a.ctx, evPrefix+":error", map[string]interface{}{
			"gameID":  gameID,
			"step":    "temp",
			"message": fmt.Sprintf("Failed to create temp directory: %v", err),
		})
		return "", fmt.Errorf("create temp dir: %w", err)
	}

	// Progress callback — throttled to ~200ms to avoid flooding the frontend.
	var lastProgress time.Time
	progressCb := func(p downloader.Progress) {
		now := time.Now()
		if now.Sub(lastProgress) < 200*time.Millisecond && p.Percent < 100 {
			return
		}
		lastProgress = now
		runtime.EventsEmit(a.ctx, evPrefix+":download-progress", map[string]interface{}{
			"gameID":           gameID,
			"bytesDownloaded":  p.BytesDownloaded,
			"totalBytes":       p.TotalBytes,
			"speedBytesPerSec": p.SpeedBytesPerSec,
			"percent":          p.Percent,
		})
	}

	// Download with retry for transient network errors.
	const maxRetries = 3
	const retryDelay = 2 * time.Second

	var lastErr error
	downloaded := false

	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Check for cancellation before each attempt.
		select {
		case <-ctx.Done():
			os.RemoveAll(tempDir)
			return "", ctx.Err()
		default:
		}

		if attempt > 1 {
			slog.Info("retrying game update download",
				"gameID", gameID,
				"attempt", attempt,
				"maxRetries", maxRetries,
				"host", link.Host,
			)

			// Backoff before retry.
			select {
			case <-ctx.Done():
				os.RemoveAll(tempDir)
				return "", ctx.Err()
			case <-time.After(retryDelay):
			}

			// Recreate temp dir on retry to avoid partial-file conflicts.
			os.RemoveAll(tempDir)
			tempDir, err = os.MkdirTemp(os.TempDir(), fmt.Sprintf("moxie-update-%d-*", gameID))
			if err != nil {
				return "", fmt.Errorf("create temp dir: %w", err)
			}
		}

		err = downloader.DownloadWithHost(
			link.URL,
			link.Host,
			tempDir,
			0, // expectedTotal unknown; Content-Length from response is used
			progressCb,
			cookie,
		)
		if err == nil {
			downloaded = true
			break
		}

		lastErr = err
		if !isTransientDownloadError(err) {
			break
		}
	}

	if !downloaded {
		errMsg := fmt.Sprintf("Download failed after %d attempts: %v", maxRetries, lastErr)
		runtime.EventsEmit(a.ctx, evPrefix+":error", map[string]interface{}{
			"gameID":  gameID,
			"step":    "download",
			"message": errMsg,
		})
		os.RemoveAll(tempDir)
		return "", fmt.Errorf("download after %d attempts: %w", maxRetries, lastErr)
	}

	// Find the downloaded file in the temp directory.
	entries, err := os.ReadDir(tempDir)
	if err != nil || len(entries) == 0 {
		os.RemoveAll(tempDir)
		return "", fmt.Errorf("no file found after download in %s", tempDir)
	}

	// Pick the non-directory entry with the largest file size
	// (there should be exactly one, but be defensive).
	var bestFile string
	var bestSize int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.Size() > bestSize {
			bestSize = info.Size()
			bestFile = filepath.Join(tempDir, e.Name())
		}
	}

	if bestFile == "" {
		os.RemoveAll(tempDir)
		return "", fmt.Errorf("no downloadable file found in temp directory")
	}

	slog.Info("game update download complete",
		"gameID", gameID,
		"file", filepath.Base(bestFile),
		"size", bestSize,
	)

	// Emit completion event (caller is responsible for cleanup).
	runtime.EventsEmit(a.ctx, evPrefix+":download-complete", map[string]interface{}{
		"gameID": gameID,
		"path":   bestFile,
		"size":   bestSize,
	})

	return bestFile, nil
}

// selectDownloadLink selects the best non-dead download link matching the
// user's current platform. It uses platform priority scoring (native > Wine >
// cross-platform > unknown) combined with host reliability scoring.
// Returns an error if no compatible link is found.
func selectDownloadLink(links []DesktopDownloadLink) (*DesktopDownloadLink, error) {
	if len(links) == 0 {
		return nil, fmt.Errorf("no download links provided")
	}

	currentPlatform := downloader.CurrentPlatform()

	type scoredLink struct {
		link  DesktopDownloadLink
		score int
	}

	var candidates []scoredLink
	for _, link := range links {
		// Skip dead links.
		if link.IsDead {
			continue
		}

		// Skip online-only / browser-playable links.
		if downloader.IsOnlineOnly(link.Name, link.URL) {
			continue
		}

		// Filter by platform compatibility.
		dlPlatform := downloader.Platform(link.Platform)
		if !downloader.PlatformMatches(dlPlatform, currentPlatform) {
			continue
		}

		// Composite score: platform priority + host reliability.
		score := downloader.PlatformPriority(dlPlatform, currentPlatform) +
			downloader.ScoreLinkHost(link.Host)
		candidates = append(candidates, scoredLink{link: link, score: score})
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no compatible download link found for platform %s (have %d links total)",
			currentPlatform, len(links))
	}

	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.score > best.score {
			best = c
		}
	}

	return &best.link, nil
}

// isTransientDownloadError returns true if the error is likely a transient
// network issue that can be retried (timeouts, connection resets, HTTP 5xx).
func isTransientDownloadError(err error) bool {
	if err == nil {
		return false
	}

	// Check for network-level transient errors. net.Error.Temporary is
	// deprecated and unreliable, so only Timeout is consulted here.
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET) {
		return true
	}

	// The downloader surfaces server-side failures as "HTTP <code>"; any 5xx
	// is worth another attempt.
	return strings.Contains(err.Error(), "HTTP 5")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// gameToSummary converts a db.Game to DesktopGameSummary.
func gameToSummary(g *db.Game) DesktopGameSummary {
	s := DesktopGameSummary{
		ID:            g.ID,
		Title:         g.Title,
		Engine:        g.Engine,
		Version:       g.Version,
		LatestVersion: g.LatestVersion,
		Status:        g.Status,
		Path:          g.Path,
		ExePath:       g.ExePath,
		SizeBytes:     g.SizeBytes,
		SizeLabel:     formatBytes(g.SizeBytes),
	}

	// Check if a cached cover exists on disk (cheap file stat).
	coverPath := filepath.Join(config.CoverDir(), strconv.FormatInt(g.ID, 10))
	if _, err := os.Stat(coverPath); err == nil {
		s.HasCover = true
	}

	return s
}

// formatBytes returns a human-readable size string.
func formatBytes(b int64) string {
	if b == 0 {
		return ""
	}
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	const units = "KMGTPE"
	if exp >= len(units) {
		return fmt.Sprintf("%.1f EB", float64(b)/float64(div))
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), units[exp])
}

// ---------------------------------------------------------------------------
// Update types
// ---------------------------------------------------------------------------

// UpdateInfo holds update check results for the frontend.
type UpdateInfo struct {
	HasUpdate      bool   `json:"hasUpdate"`
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion"`
	ReleaseURL     string `json:"releaseUrl"`
	Error          string `json:"error,omitempty"`
}

// githubRelease maps the GitHub API release response for update checks.
type githubRelease struct {
	TagName string        `json:"tag_name"`
	HTMLURL string        `json:"html_url"`
	Body    string        `json:"body"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	// Digest is "sha256:<hex>" when GitHub has computed one. It is absent for
	// older assets, so verification is best-effort.
	Digest string `json:"digest"`
}

// updateStagePath returns the path the downloaded update is staged at, and
// ensures its parent directory exists with owner-only permissions.
//
// This lives under the user's config directory rather than os.TempDir(): the
// staged file is later executed as the application binary, and a predictable
// name in a world-writable /tmp lets any other local user pre-create or
// symlink the path and choose what the app runs after an update.
func updateStagePath(assetName string) (string, error) {
	dir := filepath.Join(config.ConfigDir(), "updates")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create update staging directory: %w", err)
	}
	return filepath.Join(dir, assetName), nil
}

// CheckForUpdate checks GitHub for a newer release and returns structured
// results for the frontend.
func (a *App) CheckForUpdate() UpdateInfo {
	currentVersion := appVersion

	req, err := http.NewRequest("GET", "https://api.github.com/repos/Milisource/moxie/releases/latest", nil)
	if err != nil {
		return UpdateInfo{Error: fmt.Sprintf("Failed to create request: %v", err)}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "moxie-desktop")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return UpdateInfo{Error: fmt.Sprintf("GitHub API request failed: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return UpdateInfo{Error: fmt.Sprintf("GitHub API: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))}
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return UpdateInfo{Error: fmt.Sprintf("Failed to parse release data: %v", err)}
	}

	latest := strings.TrimPrefix(release.TagName, "v")
	current := strings.TrimPrefix(currentVersion, "v")

	hasUpdate := isNewerVersion(latest, current)

	return UpdateInfo{
		HasUpdate:      hasUpdate,
		CurrentVersion: currentVersion,
		LatestVersion:  latest,
		ReleaseURL:     release.HTMLURL,
	}
}

// DownloadUpdate downloads the latest release binary with progress events
// emitted to the frontend.
func (a *App) DownloadUpdate() error {
	release, err := fetchLatestRelease()
	if err != nil {
		runtime.EventsEmit(a.ctx, "update:error", map[string]string{"error": err.Error()})
		return fmt.Errorf("fetch latest release: %w", err)
	}

	assetName := binaryName()
	if assetName == "" {
		errMsg := fmt.Sprintf("in-app updates are not supported on %s/%s — download from the release page instead",
			goruntime.GOOS, goruntime.GOARCH)
		runtime.EventsEmit(a.ctx, "update:error", map[string]string{"error": errMsg})
		return fmt.Errorf("%s", errMsg)
	}

	var downloadURL, expectedDigest string
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			downloadURL = asset.BrowserDownloadURL
			expectedDigest = asset.Digest
			break
		}
	}
	// Fail rather than guessing at another asset: the CLI assets in the same
	// release have a near-identical naming scheme, and installing one over the
	// desktop binary is unrecoverable.
	if downloadURL == "" {
		errMsg := fmt.Sprintf("release %s has no desktop build for %s/%s (expected asset %q) — download it manually from %s",
			release.TagName, goruntime.GOOS, goruntime.GOARCH, assetName, release.HTMLURL)
		runtime.EventsEmit(a.ctx, "update:error", map[string]string{"error": errMsg})
		return fmt.Errorf("%s", errMsg)
	}

	req, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		runtime.EventsEmit(a.ctx, "update:error", map[string]string{"error": err.Error()})
		return fmt.Errorf("create download request: %w", err)
	}
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("User-Agent", "moxie-desktop")

	httpClient := &http.Client{Timeout: 120 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		runtime.EventsEmit(a.ctx, "update:error", map[string]string{"error": fmt.Sprintf("Download failed: %v", err)})
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("download: HTTP %d", resp.StatusCode)
		runtime.EventsEmit(a.ctx, "update:error", map[string]string{"error": errMsg})
		return fmt.Errorf("%s", errMsg)
	}

	tmpPath, err := updateStagePath(assetName)
	if err != nil {
		runtime.EventsEmit(a.ctx, "update:error", map[string]string{"error": err.Error()})
		return err
	}
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0700)
	if err != nil {
		runtime.EventsEmit(a.ctx, "update:error", map[string]string{"error": err.Error()})
		return fmt.Errorf("create staged update file: %w", err)
	}

	total := resp.ContentLength
	var downloaded int64
	digest := sha256.New()
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			digest.Write(buf[:n])
			if _, writeErr := f.Write(buf[:n]); writeErr != nil {
				f.Close()
				os.Remove(tmpPath)
				runtime.EventsEmit(a.ctx, "update:error", map[string]string{"error": writeErr.Error()})
				return fmt.Errorf("write temp: %w", writeErr)
			}
			downloaded += int64(n)
			if total > 0 {
				runtime.EventsEmit(a.ctx, "update:progress", map[string]interface{}{
					"downloaded": downloaded,
					"total":      total,
				})
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			f.Close()
			os.Remove(tmpPath)
			runtime.EventsEmit(a.ctx, "update:error", map[string]string{"error": readErr.Error()})
			return fmt.Errorf("read response: %w", readErr)
		}
	}

	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp: %w", err)
	}

	// Verify against the digest GitHub publishes for the asset. It is absent
	// on older releases, so a missing digest is a warning rather than a hard
	// failure — but a mismatch always discards the download.
	if expectedDigest != "" {
		got := "sha256:" + hex.EncodeToString(digest.Sum(nil))
		if !strings.EqualFold(got, expectedDigest) {
			os.Remove(tmpPath)
			errMsg := fmt.Sprintf("update integrity check failed: expected %s, got %s", expectedDigest, got)
			runtime.EventsEmit(a.ctx, "update:error", map[string]string{"error": errMsg})
			return fmt.Errorf("%s", errMsg)
		}
		slog.Info("update digest verified", "asset", assetName, "digest", got)
	} else {
		slog.Warn("release asset has no digest; skipping integrity check", "asset", assetName)
	}

	runtime.EventsEmit(a.ctx, "update:progress", map[string]interface{}{
		"downloaded": downloaded,
		"total":      downloaded,
	})
	runtime.EventsEmit(a.ctx, "update:complete", map[string]string{
		"path": tmpPath,
	})

	return nil
}

// ApplyUpdate replaces the current binary with a previously downloaded update.
// It renames the current binary to .bak, moves the temp file to the exe path,
// and removes the .bak on success.
func (a *App) ApplyUpdate() error {
	assetName := binaryName()
	if assetName == "" {
		return fmt.Errorf("in-app updates are not supported on %s/%s", goruntime.GOOS, goruntime.GOARCH)
	}
	tmpPath, err := updateStagePath(assetName)
	if err != nil {
		return err
	}

	// Verify the staged binary is there before moving the running one aside —
	// otherwise a missing download leaves the app relying on the restore path
	// to put its own executable back.
	if _, err := os.Stat(tmpPath); err != nil {
		return fmt.Errorf("no downloaded update to apply: %w", err)
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot find current binary: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("cannot resolve symlink: %w", err)
	}

	backupPath := exe + ".bak"
	if err := renameOrCopy(exe, backupPath); err != nil {
		return fmt.Errorf("cannot back up current binary: %w", err)
	}

	if err := renameOrCopy(tmpPath, exe); err != nil {
		// Restore backup on failure. If that also fails the app has no
		// executable left, so say so loudly rather than swallowing it.
		if rerr := renameOrCopy(backupPath, exe); rerr != nil {
			slog.Error("update failed AND backup could not be restored",
				"backup", backupPath, "exe", exe, "restoreError", rerr)
			return fmt.Errorf("cannot install update (%w) and restoring the backup failed (%v) — the previous binary is at %s", err, rerr, backupPath)
		}
		return fmt.Errorf("cannot install update: %w", err)
	}

	// The staged file is 0700 so other users cannot tamper with it; the
	// installed binary needs the usual executable permissions.
	if err := os.Chmod(exe, 0755); err != nil {
		slog.Warn("could not set permissions on updated binary", "exe", exe, "error", err)
	}

	os.Remove(backupPath)
	return nil
}

// binaryName returns the expected update asset filename for the current
// platform, or an empty string if this platform has no desktop asset naming.
//
// These are deliberately "moxie-desktop-*" and NOT the "moxie-*" assets built
// by .github/workflows/release.yml — those are the CLI. Installing a CLI
// binary over the running desktop app would replace the GUI with a terminal
// program and leave no way back. If a release ships no desktop asset, the
// updater must fail rather than fall back to a same-shaped CLI name.
func binaryName() string {
	switch goruntime.GOOS {
	case "linux":
		return fmt.Sprintf("moxie-desktop-linux-%s", goruntime.GOARCH)
	case "darwin":
		return fmt.Sprintf("moxie-desktop-macos-%s", goruntime.GOARCH)
	case "windows":
		return fmt.Sprintf("moxie-desktop-windows-%s.exe", goruntime.GOARCH)
	default:
		return ""
	}
}

// fetchLatestRelease fetches the latest release info from GitHub.
func fetchLatestRelease() (*githubRelease, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/repos/Milisource/moxie/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "moxie-desktop")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github API: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("github API: decode: %w", err)
	}
	return &release, nil
}

// isNewerVersion returns true if latest > current using semver-like comparison.
func isNewerVersion(latest, current string) bool {
	clean := func(v string) string {
		v = strings.TrimPrefix(v, "v")
		if idx := strings.IndexAny(v, "-+"); idx >= 0 {
			v = v[:idx]
		}
		return v
	}

	latest = clean(latest)
	current = clean(current)

	partsL := strings.Split(latest, ".")
	partsC := strings.Split(current, ".")
	maxLen := len(partsL)
	if len(partsC) > maxLen {
		maxLen = len(partsC)
	}

	for i := 0; i < maxLen; i++ {
		var a, b int
		if i < len(partsL) {
			a, _ = strconv.Atoi(partsL[i])
		}
		if i < len(partsC) {
			b, _ = strconv.Atoi(partsC[i])
		}
		if a != b {
			return a > b
		}
	}
	return false
}

// renameOrCopy attempts an atomic rename, falling back to copy+delete
// when src and dest are on different mount points.
func renameOrCopy(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "invalid cross-device link") ||
		strings.Contains(err.Error(), "The system cannot move the file") {
		return copyFile(src, dst)
	}
	return err
}

// copyFile copies a file from src to dst, preserving permissions.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------
// F95Zone Browser types
// ---------------------------------------------------------------------------

// F95SearchResult represents a search result from F95Zone for the browser view.
type F95SearchResult struct {
	Title        string `json:"title"`
	URL          string `json:"url"`
	Prefix       string `json:"prefix"`       // e.g., "[Ren'Py]", "[Unity]"
	ThumbnailURL string `json:"thumbnailUrl"` // empty until preview loads
	MatchScore   int    `json:"matchScore"`
}

// ThreadPreview holds preview data for an F95Zone thread.
type ThreadPreview struct {
	Title         string            `json:"title"`
	Developer     string            `json:"developer"`
	Version       string            `json:"version"`
	Overview      string            `json:"overview"`
	CoverURL      string            `json:"coverUrl"`
	Tags          []string          `json:"tags"`
	Status        string            `json:"status"`
	StoreLinks    map[string]string `json:"storeLinks"`
	DownloadLinks []F95DownloadLink `json:"downloadLinks"`
	Prefix        string            `json:"prefix"`
}

// F95DownloadLink is a download link for the F95Zone browser preview.
type F95DownloadLink struct {
	URL      string `json:"url"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Platform string `json:"platform"`
}

// ---------------------------------------------------------------------------
// F95Zone Browser methods
// ---------------------------------------------------------------------------

// SearchF95Zone searches F95Zone for game threads matching the query.
// Returns results with engine prefix extracted from thread titles.
func (a *App) SearchF95Zone(query string) ([]F95SearchResult, error) {
	cookie, err := browser.GetF95Cookies()
	if err != nil || cookie == "" {
		return nil, fmt.Errorf("F95Zone cookies not available. Log into F95Zone in your browser first")
	}

	client := scraper.NewClient(cookie)
	results, err := client.SearchF95Zone(query)
	if err != nil {
		return nil, fmt.Errorf("F95Zone search failed: %w", err)
	}

	// Simple match scoring based on position + title quality.
	// Results come sorted by relevance from the search engine.
	desktop := make([]F95SearchResult, 0, len(results))
	queryLower := strings.ToLower(strings.TrimSpace(query))
	for i, r := range results {
		prefix := engine.ExtractEngineFromTitle(r.Title)
		score := computeSearchScore(r.Title, prefix, queryLower, i)

		desktop = append(desktop, F95SearchResult{
			Title:        r.Title,
			URL:          r.URL,
			Prefix:       prefix,
			ThumbnailURL: r.ThumbnailURL,
			MatchScore:   score,
		})
	}
	return desktop, nil
}

// computeSearchScore calculates a relevance score for a search result.
// Base: position bonus + title match quality + engine prefix match.
func computeSearchScore(title, prefix, queryLower string, position int) int {
	score := 100
	
	// Position bonus: earlier results get higher base scores.
	score -= position * 10
	if score < 20 {
		score = 20
	}

	// Title match quality.
	titleLower := strings.ToLower(title)
	switch {
	case titleLower == queryLower:
		score += 50
	case strings.HasPrefix(titleLower, queryLower):
		score += 30
	case strings.Contains(titleLower, queryLower):
		score += 15
	}

	// Engine prefix match: if query mentions the engine, bonus.
	if prefix != "" && strings.Contains(queryLower, strings.ToLower(prefix)) {
		score += 20
	}

	return score
}

// GetThreadPreview scrapes an F95Zone thread and returns preview data.
func (a *App) GetThreadPreview(url string) (*ThreadPreview, error) {
	cookie, err := browser.GetF95Cookies()
	if err != nil || cookie == "" {
		return nil, fmt.Errorf("F95Zone cookies not available. Log into F95Zone in your browser first")
	}

	client := scraper.NewClient(cookie)
	data, err := client.ScrapeThread(url)
	if err != nil {
		return nil, fmt.Errorf("scraping thread failed: %w", err)
	}

	prefix := engine.ExtractEngineFromTitle(data.Title)
	cleanTitle := strings.TrimSpace(scraper.StripThreadPrefix(data.Title))
	if cleanTitle == "" {
		cleanTitle = data.Title
	}

	downloads := make([]F95DownloadLink, 0, len(data.DownloadLinks))
	for _, dl := range data.DownloadLinks {
		platform := downloader.DetectPlatformFromLink(dl.Name, dl.URL)
		downloads = append(downloads, F95DownloadLink{
			URL:      dl.URL,
			Name:     dl.Name,
			Host:     dl.Host,
			Platform: platform,
		})
	}

	return &ThreadPreview{
		Title:         cleanTitle,
		Developer:     data.Developer,
		Version:       data.Version,
		Overview:      data.Overview,
		CoverURL:      data.CoverURL,
		Tags:          data.Tags,
		Status:        data.Status,
		StoreLinks:    data.StoreLinks,
		DownloadLinks: downloads,
		Prefix:        prefix,
	}, nil
}

// AddGameFromF95Zone scrapes an F95Zone thread, detects the engine, and creates
// a library entry pointing to the thread. It does NOT download the game.
// Returns the new game's ID.
// The engine parameter can be empty to auto-detect from the thread title prefix.
func (a *App) AddGameFromF95Zone(url, title, engineName string) (int64, error) {
	if a.db == nil {
		return 0, fmt.Errorf("database not initialized")
	}

	cookie, err := browser.GetF95Cookies()
	if err != nil || cookie == "" {
		return 0, fmt.Errorf("F95Zone cookies not available. Log into F95Zone in your browser first")
	}

	client := scraper.NewClient(cookie)
	data, err := client.ScrapeThread(url)
	if err != nil {
		return 0, fmt.Errorf("scraping thread failed: %w", err)
	}

	// Use detected engine from thread prefix if not explicitly provided.
	if engineName == "" {
		if f95Eng := engine.ExtractEngineFromTitle(data.Title); f95Eng != "" {
			engineName = f95Eng
		}
	}

	// Build the game record. Since this game is not locally installed,
	// use the thread title (sanitized) as a virtual path identifier.
	gameTitle := data.Title
	if title != "" {
		gameTitle = title
	}
	cleanTitle := strings.TrimSpace(scraper.StripThreadPrefix(gameTitle))
	if cleanTitle == "" {
		cleanTitle = gameTitle
	}

	// Extract thread ID from thread data.
	var threadID int64
	if data.ThreadID > 0 {
		threadID = data.ThreadID
	}

	// Use a virtual path convention for games not yet downloaded:
	// /virtual/f95zone/<thread_id>/
	// This distinguishes browser-added games from locally-installed ones
	// and allows the scanner/launcher to skip them gracefully.
	var virtualPath string
	if threadID > 0 {
		virtualPath = fmt.Sprintf("/virtual/f95zone/%d/", threadID)
	} else {
		virtualPath = fmt.Sprintf("/virtual/f95zone/0/%s", url)
	}

	game := &db.Game{
		Title:       cleanTitle,
		Engine:      engineName,
		Path:        virtualPath,
		F95URL:      url,
		F95ThreadID: threadID,
		Version:     data.Version,
		Tags:        data.Tags,
		Status:      "unknown",
	}
	if data.Status != "" {
		game.Status = data.Status
	}
	if len(data.StoreLinks) > 0 {
		game.StoreLinks = data.StoreLinks
		if steamURL, hasSteam := data.StoreLinks["steam"]; hasSteam {
			if appID, ok := steam.ExtractSteamAppID(steamURL); ok {
				game.SteamAppID = int64(appID)
			}
		}
	}

	id, err := a.db.InsertGame(game)
	if err != nil {
		return 0, fmt.Errorf("inserting game: %w", err)
	}

	// Save scraped metadata (developer, overview, cover).
	if data.Developer != "" || data.Overview != "" || data.CoverURL != "" {
		meta := &db.ScrapedMeta{
			GameID:    id,
			Developer: data.Developer,
			Overview:  data.Overview,
			CoverURL:  data.CoverURL,
		}
		if err := a.db.UpsertScrapedMeta(meta); err != nil {
			slog.Warn("failed to save scraped metadata", "game", cleanTitle, "error", err)
		}
	}

	// Save download links.
	for _, dl := range data.DownloadLinks {
		p := downloader.DetectPlatformFromLink(dl.Name, dl.URL)
		link := &db.DownloadLink{
			GameID:   id,
			URL:      dl.URL,
			Host:     dl.Host,
			Name:     dl.Name,
			Platform: db.Platform(p),
			IsDead:   false,
		}
		if _, err := a.db.CreateDownloadLink(link); err != nil {
			slog.Warn("failed to save download link", "game", cleanTitle, "error", err)
		}
	}

	// Cache the cover image for immediate display.
	if data.CoverURL != "" {
		a.cacheCover(id, data.CoverURL)
	}

	slog.Info("game added from F95Zone", "id", id, "title", cleanTitle, "engine", engineName)
	return id, nil
}

// ---------------------------------------------------------------------------
// Cover art caching
// ---------------------------------------------------------------------------

// maxCoverBytes bounds a single cached cover image.
const maxCoverBytes int64 = 16 << 20 // 16 MiB

// imageMimeFromPrefix detects the image MIME type from the file's magic bytes.
// Returns "jpeg", "png", "webp", "gif", or "png" as fallback.
func imageMimeFromPrefix(data []byte) string {
	if len(data) < 4 {
		return "png"
	}
	switch {
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}):
		return "jpeg"
	case bytes.HasPrefix(data, []byte{0x89, 0x50, 0x4E, 0x47}):
		return "png"
	case bytes.HasPrefix(data, []byte{0x52, 0x49, 0x46, 0x46}):
		// WEBP: RIFF....WEBP
		if len(data) > 11 && string(data[8:12]) == "WEBP" {
			return "webp"
		}
		return "png"
	case bytes.HasPrefix(data, []byte{0x47, 0x49, 0x46}):
		return "gif"
	default:
		return "png"
	}
}

// cacheCover downloads a cover image from coverURL and caches it to
// config.CoverDir()/<gameID>. It is a no-op if the file already exists.
// Returns the local cached path, or empty string on failure.
//
// Unexported deliberately: the frontend reads covers through GetCachedCovers,
// and every exported App method becomes part of the Wails binding surface.
func (a *App) cacheCover(gameID int64, coverURL string) string {
	if coverURL == "" {
		return ""
	}

	coverDir := config.CoverDir()
	coverPath := filepath.Join(coverDir, strconv.FormatInt(gameID, 10))

	// Already cached.
	if _, err := os.Stat(coverPath); err == nil {
		return coverPath
	}

	// Ensure the cover directory exists.
	if err := os.MkdirAll(coverDir, 0755); err != nil {
		slog.Error("failed to create cover directory", "error", err)
		return ""
	}

	// Download the cover image.
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", coverURL, nil)
	if err != nil {
		slog.Error("failed to create cover request", "url", coverURL, "error", err)
		return ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; moxie/0.4)")
	// F95Zone covers may need auth cookies
	if strings.Contains(coverURL, "f95zone") {
		if cookie, err := browser.GetF95Cookies(); err == nil && cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		slog.Error("failed to download cover", "url", coverURL, "error", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("cover download returned non-200", "url", coverURL, "status", resp.StatusCode)
		return ""
	}

	// Cap the read: coverURL comes from a scraped page, and covers get
	// base64-encoded into memory for the frontend.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxCoverBytes+1))
	if err != nil {
		slog.Error("failed to read cover response", "error", err)
		return ""
	}

	if len(data) == 0 {
		slog.Warn("cover download returned empty body", "url", coverURL)
		return ""
	}
	if int64(len(data)) > maxCoverBytes {
		slog.Warn("cover exceeds size limit; skipping", "url", coverURL, "limit", maxCoverBytes)
		return ""
	}

	if err := os.WriteFile(coverPath, data, 0644); err != nil {
		slog.Error("failed to write cover file", "path", coverPath, "error", err)
		return ""
	}

	slog.Info("cover cached", "gameID", gameID, "path", coverPath, "size", len(data))
	return coverPath
}

// GetCachedCovers returns a batch map of gameID → base64 data URI for cached
// covers. Games without cached covers are omitted from the map.
func (a *App) GetCachedCovers(gameIDs []int64) map[int64]string {
	result := make(map[int64]string, len(gameIDs))
	coverDir := config.CoverDir()

	for _, id := range gameIDs {
		coverPath := filepath.Join(coverDir, strconv.FormatInt(id, 10))
		data, err := os.ReadFile(coverPath)
		if err != nil {
			continue
		}
		mime := imageMimeFromPrefix(data)
		encoded := base64.StdEncoding.EncodeToString(data)
		result[id] = "data:image/" + mime + ";base64," + encoded
	}

	return result
}

// ---------------------------------------------------------------------------
// Add Game types
// ---------------------------------------------------------------------------

// DetectionResult holds engine detection results for the manual add dialog.
type DetectionResult struct {
	Engine    string `json:"engine"`
	Version   string `json:"version"`
	Title     string `json:"title"`
	SizeBytes int64  `json:"sizeBytes"`
	SizeLabel string `json:"sizeLabel"`
	Path      string `json:"path"`
	Error     string `json:"error,omitempty"`
}

// DetectGame analyzes a path and returns detection info without saving.
func (a *App) DetectGame(path string) DetectionResult {
	info, err := os.Stat(path)
	if err != nil {
		return DetectionResult{
			Path:  path,
			Error: fmt.Sprintf("Path does not exist: %v", err),
		}
	}
	if !info.IsDir() {
		return DetectionResult{
			Path:  path,
			Error: "Path is not a directory",
		}
	}

	engResult := engine.Detect(path)
	detected := scanner.ScanSingle(path)
	title := filepath.Base(path)

	return DetectionResult{
		Engine:    string(engResult.Engine),
		Version:   detected.Version,
		Title:     title,
		SizeBytes: detected.SizeBytes,
		SizeLabel: formatBytes(detected.SizeBytes),
		Path:      path,
	}
}

// AddGame saves a game to the library. If engine is empty, it auto-detects.
// If title is empty, it derives from the path's base name.
func (a *App) AddGame(path string, title string, eng string, version string) (int64, error) {
	if a.db == nil {
		return 0, fmt.Errorf("database not initialized")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return 0, fmt.Errorf("resolving path: %w", err)
	}

	if title == "" {
		title = filepath.Base(absPath)
	}
	if eng == "" {
		detected := scanner.ScanSingle(absPath)
		if detected.Engine != "Unknown" {
			eng = string(detected.Engine)
			slog.Debug("auto-detected engine", "path", absPath, "engine", eng)
		}
	}

	// Check for duplicates by path.
	existing, err := a.db.GetGameByPath(absPath)
	if err != nil {
		return 0, fmt.Errorf("checking for existing game: %w", err)
	}
	if existing != nil {
		return 0, fmt.Errorf("game already exists at this path: %q (ID %d)", existing.Title, existing.ID)
	}

	game := &db.Game{
		Title:   title,
		Engine:  eng,
		Path:    absPath,
		Version: version,
		Status:  "unknown",
	}

	id, err := a.db.InsertGame(game)
	if err != nil {
		return 0, fmt.Errorf("inserting game: %w", err)
	}

	slog.Info("game added", "id", id, "title", title, "engine", eng)
	return id, nil
}

// ---------------------------------------------------------------------------
// Game CRUD methods
// ---------------------------------------------------------------------------

// RemoveGame soft-deletes a game (hard=true for permanent deletion).
func (a *App) RemoveGame(id int64, hard bool) error {
	if a.db == nil {
		return fmt.Errorf("database not initialized")
	}

	if hard {
		slog.Warn("permanently deleting game", "id", id)
		return a.db.DeleteGamePermanent(id)
	}
	slog.Info("soft-deleting game", "id", id)
	return a.db.DeleteGame(id)
}

// RestoreGame restores a soft-deleted game.
func (a *App) RestoreGame(id int64) error {
	if a.db == nil {
		return fmt.Errorf("database not initialized")
	}
	slog.Info("restoring game", "id", id)
	return a.db.RestoreGame(id)
}

// validStatuses lists the allowed game status values.
var validStatuses = []string{"active", "completed", "abandoned", "on_hold", "unknown"}

// isValidStatus checks whether a status string is one of the allowed values.
func isValidStatus(s string) bool {
	for _, vs := range validStatuses {
		if s == vs {
			return true
		}
	}
	return false
}

// SetGameStatus updates a game's status after validation.
func (a *App) SetGameStatus(id int64, status string) error {
	if a.db == nil {
		return fmt.Errorf("database not initialized")
	}
	if !isValidStatus(status) {
		return fmt.Errorf("invalid status %q. Valid: %s", status, strings.Join(validStatuses, ", "))
	}
	slog.Info("updating game status", "id", id, "status", status)
	return a.db.UpdateGameStatus(id, status)
}

// RenameGame renames a game's title and its directory on disk.
func (a *App) RenameGame(id int64, newTitle string) error {
	if a.db == nil {
		return fmt.Errorf("database not initialized")
	}
	if newTitle == "" {
		return fmt.Errorf("new title must not be empty")
	}

	game, err := a.db.GetGame(id)
	if err != nil {
		return fmt.Errorf("game not found: %w", err)
	}
	if game == nil {
		return fmt.Errorf("game with id %d not found", id)
	}

	// Rename directory on disk if it still exists.
	if _, statErr := os.Stat(game.Path); statErr == nil {
		parent := filepath.Dir(game.Path)
		newPath := filepath.Join(parent, newTitle)

		// Skip if the new path is the same as old.
		if newPath != game.Path {
			// Check target doesn't already exist.
			if _, existsErr := os.Stat(newPath); existsErr == nil {
				return fmt.Errorf("target directory already exists: %q", newPath)
			}
			if err := os.Rename(game.Path, newPath); err != nil {
				return fmt.Errorf("renaming directory: %w", err)
			}
			slog.Info("renamed game directory", "old", game.Path, "new", newPath)
			game.Path = newPath
		}
	} else {
		slog.Warn("game directory does not exist, updating title only", "path", game.Path, "id", id)
	}

	// Update title and path in DB.
	game.Title = newTitle
	if err := a.db.UpdateGame(game); err != nil {
		return fmt.Errorf("updating game in database: %w", err)
	}

	slog.Info("game renamed", "id", id, "title", newTitle)
	return nil
}

// PurgeDeleted permanently removes all soft-deleted games from the library.
func (a *App) PurgeDeleted() (int64, error) {
	if a.db == nil {
		return 0, fmt.Errorf("database not initialized")
	}
	count, err := a.db.PurgeDeleted()
	if err != nil {
		return 0, fmt.Errorf("purging deleted games: %w", err)
	}
	slog.Info("purged deleted games", "count", count)
	return count, nil
}

// ListDeletedGames returns all soft-deleted games as summaries.
func (a *App) ListDeletedGames() ([]DesktopGameSummary, error) {
	if a.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	games, err := a.db.ListDeletedGames()
	if err != nil {
		return nil, fmt.Errorf("listing deleted games: %w", err)
	}

	result := make([]DesktopGameSummary, 0, len(games))
	for _, g := range games {
		result = append(result, gameToSummary(&g))
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Edit game fields
// ---------------------------------------------------------------------------

// EditGameFields holds the editable fields for a game.
type EditGameFields struct {
	Engine  string `json:"engine"`
	Version string `json:"version"`
	ExePath string `json:"exePath"`
	Notes   string `json:"notes"`
}

// EditGame updates multiple editable fields on a game in one call.
//
// An empty string means "leave unchanged" — callers send "" for every field
// they are not editing. Consequently these fields cannot be cleared through
// this call; clearing would need nullable fields to tell "unset" apart from
// "set to empty".
func (a *App) EditGame(id int64, fields EditGameFields) error {
	if a.db == nil {
		return fmt.Errorf("database not initialized")
	}

	game, err := a.db.GetGame(id)
	if err != nil {
		return fmt.Errorf("game not found: %w", err)
	}
	if game == nil {
		return fmt.Errorf("game with id %d not found", id)
	}

	// Only update non-empty fields (empty = keep current)
	if fields.Engine != "" {
		game.Engine = fields.Engine
	}
	if fields.Version != "" {
		game.Version = fields.Version
	}
	if fields.ExePath != "" {
		game.ExePath = fields.ExePath
	}
	if fields.Notes != "" {
		game.Notes = fields.Notes
	}

	return a.db.UpdateGame(game)
}

// ---------------------------------------------------------------------------
// Collections
// ---------------------------------------------------------------------------

// DesktopCollection is a collection with its active-game count.
type DesktopCollection struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	GameCount int    `json:"gameCount"`
}

// GetCollections returns all collections with their active-game counts.
func (a *App) GetCollections() ([]DesktopCollection, error) {
	if a.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	collections, err := a.db.ListCollections()
	if err != nil {
		return nil, fmt.Errorf("failed to list collections: %w", err)
	}
	counts, err := a.db.CountGamesPerCollection()
	if err != nil {
		return nil, fmt.Errorf("failed to count collection members: %w", err)
	}

	result := make([]DesktopCollection, 0, len(collections))
	for _, c := range collections {
		result = append(result, DesktopCollection{
			ID:        c.ID,
			Name:      c.Name,
			GameCount: counts[c.ID],
		})
	}
	return result, nil
}

// CreateCollection creates a collection and returns its ID.
func (a *App) CreateCollection(name string) (int64, error) {
	if a.db == nil {
		return 0, fmt.Errorf("database not initialized")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, fmt.Errorf("collection name must not be empty")
	}

	c, err := a.db.CreateCollection(name)
	if err != nil {
		return 0, fmt.Errorf("failed to create collection: %w", err)
	}
	slog.Info("collection created", "id", c.ID, "name", c.Name)
	return c.ID, nil
}

// DeleteCollection removes a collection. Member games are unaffected; only the
// membership rows go away (ON DELETE CASCADE).
func (a *App) DeleteCollection(id int64) error {
	if a.db == nil {
		return fmt.Errorf("database not initialized")
	}
	if err := a.db.DeleteCollection(id); err != nil {
		return fmt.Errorf("failed to delete collection: %w", err)
	}
	slog.Info("collection deleted", "id", id)
	return nil
}

// GetCollectionGames returns the active games in a collection.
func (a *App) GetCollectionGames(collectionID int64) ([]DesktopGameSummary, error) {
	if a.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	games, err := a.db.GetGamesInCollection(collectionID)
	if err != nil {
		return nil, fmt.Errorf("failed to list collection games: %w", err)
	}

	result := make([]DesktopGameSummary, 0, len(games))
	for _, g := range games {
		if g == nil || !g.DeletedAt.IsZero() {
			continue
		}
		result = append(result, gameToSummary(g))
	}
	return result, nil
}

// GetGameCollections returns the collections a game belongs to.
func (a *App) GetGameCollections(gameID int64) ([]DesktopCollection, error) {
	if a.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	collections, err := a.db.GetCollectionsForGame(gameID)
	if err != nil {
		return nil, fmt.Errorf("failed to list collections for game: %w", err)
	}

	result := make([]DesktopCollection, 0, len(collections))
	for _, c := range collections {
		result = append(result, DesktopCollection{ID: c.ID, Name: c.Name})
	}
	return result, nil
}

// AddGameToCollection adds a game to a collection. Adding a game that is
// already a member is not an error.
func (a *App) AddGameToCollection(gameID, collectionID int64) error {
	if a.db == nil {
		return fmt.Errorf("database not initialized")
	}
	if err := a.db.AddGameToCollection(gameID, collectionID); err != nil {
		return fmt.Errorf("failed to add game to collection: %w", err)
	}
	return nil
}

// RemoveGameFromCollection removes a game from a collection.
func (a *App) RemoveGameFromCollection(gameID, collectionID int64) error {
	if a.db == nil {
		return fmt.Errorf("database not initialized")
	}
	if err := a.db.RemoveGameFromCollection(gameID, collectionID); err != nil {
		return fmt.Errorf("failed to remove game from collection: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Duplicate detection
// ---------------------------------------------------------------------------

// DuplicateGroup holds games that share a normalized title.
type DuplicateGroup struct {
	Title string              `json:"title"`
	Count int                 `json:"count"`
	Games []DesktopGameSummary `json:"games"`
}

// FindDuplicateGames finds active games whose normalized titles match.
// Normalization: lowercases, trims whitespace, strips [tags] and (parentheticals).
// Returns groups with 2+ matching games, ordered by group size descending.
func (a *App) FindDuplicateGames() ([]DuplicateGroup, error) {
	if a.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	games, err := a.db.ListDuplicateCandidates()
	if err != nil {
		return nil, fmt.Errorf("listing games: %w", err)
	}

	// Group by normalized title.
	groups := make(map[string][]db.GameDupSummary)
	for _, g := range games {
		key := normalizeTitle(g.Title)
		if key != "" {
			groups[key] = append(groups[key], g)
		}
	}

	// Build result, keeping only groups with 2+ games.
	var result []DuplicateGroup
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		summaries := make([]DesktopGameSummary, 0, len(group))
		for _, g := range group {
			summaries = append(summaries, DesktopGameSummary{
				ID:        g.ID,
				Title:     g.Title,
				Engine:    g.Engine,
				Version:   g.Version,
				Status:    g.Status,
				Path:      g.Path,
				ExePath:   g.ExePath,
				SizeBytes: g.SizeBytes,
				SizeLabel: formatBytes(g.SizeBytes),
			})
		}
		result = append(result, DuplicateGroup{
			Title: group[0].Title,
			Count: len(group),
			Games: summaries,
		})
	}

	// Sort by group size descending.
	sort.Slice(result, func(i, j int) bool {
		return result[i].Count > result[j].Count
	})

	return result, nil
}

// normalizeTitle strips common prefixes/suffixes, lowercases,
// and removes bracketed content [tags] for duplicate detection.
func normalizeTitle(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	// Strip leading bracketed tags: [SOLVED] [RELEASE] etc.
	s = stripBracketedPrefix(s)
	// Strip trailing bracketed content: [v0.1] [v1.0]
	s = stripBracketedSuffix(s)
	// Strip parenthetical content: (STEAM) (Demo)
	s = stripParentheticalSuffix(s)
	return strings.TrimSpace(s)
}

func stripBracketedPrefix(s string) string {
	for {
		trimmed := strings.TrimLeft(s, " ")
		if strings.HasPrefix(trimmed, "[") {
			if idx := strings.Index(trimmed, "]"); idx >= 0 {
				s = strings.TrimSpace(trimmed[idx+1:])
				continue
			}
		}
		break
	}
	return s
}

func stripBracketedSuffix(s string) string {
	for {
		trimmed := strings.TrimRight(s, " ")
		if strings.HasSuffix(trimmed, "]") {
			if idx := strings.LastIndex(trimmed, "["); idx >= 0 {
				s = strings.TrimSpace(trimmed[:idx])
				continue
			}
		}
		break
	}
	return s
}

func stripParentheticalSuffix(s string) string {
	for {
		trimmed := strings.TrimRight(s, " ")
		if strings.HasSuffix(trimmed, ")") {
			if idx := strings.LastIndex(trimmed, "("); idx >= 0 {
				between := strings.TrimSpace(trimmed[idx+1 : len(trimmed)-1])
				// Only strip common parentheticals, not prose
				if isSkippableParenthetical(between) {
					s = strings.TrimSpace(trimmed[:idx])
					continue
				}
			}
		}
		break
	}
	return s
}

var skippableWords = map[string]bool{
	"steam": true, "demo": true, "beta": true, "alpha": true,
	"early access": true, "public": true, "update": true, "patch": true,
	"v0": true, "v1": true, "v2": true, "v3": true, "v4": true, "v5": true,
}

func isSkippableParenthetical(s string) bool {
	lower := strings.ToLower(s)
	if skippableWords[lower] {
		return true
	}
	// Match version patterns like "v1.0", "v0.3.5"
	if strings.HasPrefix(lower, "v") && len(lower) > 1 {
		rest := lower[1:]
		parts := strings.Split(rest, ".")
		for _, p := range parts {
			if _, err := strconv.Atoi(p); err != nil {
				return false
			}
		}
		return len(parts) >= 1
	}
	return false
}

// ---------------------------------------------------------------------------
// Sync types
// ---------------------------------------------------------------------------

// SyncProgress holds sync progress data emitted to the frontend.
type SyncProgress struct {
	Current int    `json:"current"`
	Total   int    `json:"total"`
	Title   string `json:"title"`
	Phase   string `json:"phase"` // "associating", "checking-updates", "complete"
}

// SyncResult holds the final sync completion data.
type SyncResult struct {
	Associated int      `json:"associated"`
	Updated    int      `json:"updated"`
	Errors     []string `json:"errors"`
}

// SyncAllGames triggers a full library F95Zone sync in a goroutine.
// Phase 1: Auto-associate unassociated games with F95Zone threads.
// Phase 2: Check associated games for version updates.
//
// The F95Zone cookie is read from the browser here rather than passed in by
// the frontend — thread pages are login-walled, and an empty cookie makes
// every request in both phases silently return nothing useful.
func (a *App) SyncAllGames() error {
	if a.db == nil {
		return fmt.Errorf("database not initialized")
	}

	cookie, err := browser.GetF95Cookies()
	if err != nil || cookie == "" {
		return fmt.Errorf("F95Zone cookies not available. Log into F95Zone in your browser first")
	}

	a.goBackground("sync", func(ctx context.Context) {
		client := scraper.NewClient(cookie)
		var allErrors []string
		associated := 0
		updated := 0

		// Phase 1: Auto-association.
		unassociated, err := a.db.GamesWithoutF95URL()
		if err != nil {
			errMsg := fmt.Sprintf("failed to load unassociated games: %v", err)
			slog.Error(errMsg)
			allErrors = append(allErrors, errMsg)
		} else if len(unassociated) > 0 {
			slog.Info("sync phase 1: associating games", "count", len(unassociated))
			total := len(unassociated)

			for i, game := range unassociated {
				if ctx.Err() != nil {
					return
				}
				runtime.EventsEmit(a.ctx, "sync:progress", SyncProgress{
					Current: i + 1,
					Total:   total,
					Title:   game.Title,
					Phase:   "associating",
				})

				query := scraper.SanitizeTitle(game.Title)
				if query == "" {
					query = game.Title
				}

				results, searchErr := client.SearchF95Zone(query)
				if searchErr != nil {
					allErrors = append(allErrors, fmt.Sprintf("%s: search failed: %v", game.Title, searchErr))
					continue
				}

				if len(results) == 0 {
					continue
				}

				// Use the first result as best match.
				best := results[0]
				data, scrapeErr := client.ScrapeThread(best.URL)
				if scrapeErr != nil {
					allErrors = append(allErrors, fmt.Sprintf("%s: scrape failed: %v", game.Title, scrapeErr))
					continue
				}

				scraper.ApplyThreadData(&game, data, best.URL)
				if err := a.db.UpdateGame(&game); err != nil {
					allErrors = append(allErrors, fmt.Sprintf("%s: save failed: %v", game.Title, err))
					continue
				}

				// Save scraped metadata.
				if data.Developer != "" || data.Overview != "" || data.CoverURL != "" {
					meta := &db.ScrapedMeta{
						GameID:    game.ID,
						Developer: data.Developer,
						Overview:  data.Overview,
						CoverURL:  data.CoverURL,
					}
					if err := a.db.UpsertScrapedMeta(meta); err != nil {
						slog.Warn("failed to save metadata", "game", game.Title, "error", err)
					}
					if data.CoverURL != "" {
						a.cacheCover(game.ID, data.CoverURL)
					}
				}

				associated++

				runtime.EventsEmit(a.ctx, "sync:game-done", map[string]interface{}{
					"id":      game.ID,
					"title":   game.Title,
					"status":  game.Status,
					"version": data.Version,
				})
			}
		}

		// Phase 2: Version updates.
		trackable, err := a.db.GamesWithF95URL()
		if err != nil {
			errMsg := fmt.Sprintf("failed to load trackable games: %v", err)
			slog.Error(errMsg)
			allErrors = append(allErrors, errMsg)
		} else if len(trackable) > 0 {
			slog.Info("sync phase 2: checking for updates", "count", len(trackable))
			total := len(trackable)

			for i, game := range trackable {
				if ctx.Err() != nil {
					return
				}
				runtime.EventsEmit(a.ctx, "sync:progress", SyncProgress{
					Current: i + 1,
					Total:   total,
					Title:   game.Title,
					Phase:   "checking-updates",
				})

				url := scraper.ResolveScrapeURL(game.F95URL, game.F95ThreadID)
				if url == "" {
					continue
				}

				data, scrapeErr := client.ScrapeThread(url)
				if scrapeErr != nil {
					allErrors = append(allErrors, fmt.Sprintf("%s: version check failed: %v", game.Title, scrapeErr))
					continue
				}

				if data.Version != "" && data.Version != game.LatestVersion {
					game.LatestVersion = data.Version
					if err := a.db.UpdateGame(&game); err != nil {
						allErrors = append(allErrors, fmt.Sprintf("%s: save version failed: %v", game.Title, err))
						continue
					}
					updated++

					runtime.EventsEmit(a.ctx, "sync:game-done", map[string]interface{}{
						"id":      game.ID,
						"title":   game.Title,
						"status":  game.Status,
						"version": data.Version,
					})
				}
			}
		}

		runtime.EventsEmit(a.ctx, "sync:complete", SyncResult{
			Associated: associated,
			Updated:    updated,
			Errors:     allErrors,
		})
	})

	return nil
}

// GetCookieStatus checks whether F95Zone browser cookies are available.
func (a *App) GetCookieStatus() string {
	cookie, err := browser.GetF95Cookies()
	if err != nil || cookie == "" {
		return "not_found"
	}
	return "available"
}

// SyncSingleGame syncs metadata for a single game from the detail view.
func (a *App) SyncSingleGame(id int64) error {
	if a.db == nil {
		return fmt.Errorf("database not initialized")
	}

	game, err := a.db.GetGame(id)
	if err != nil {
		return fmt.Errorf("game not found: %w", err)
	}
	if game == nil {
		return fmt.Errorf("game with id %d not found", id)
	}

	cookie, err := browser.GetF95Cookies()
	if err != nil || cookie == "" {
		return fmt.Errorf("F95Zone cookies not available. Log into F95Zone in your browser first")
	}

	client := scraper.NewClient(cookie)

	url := scraper.ResolveScrapeURL(game.F95URL, game.F95ThreadID)
	if url == "" {
		return fmt.Errorf("game %q has no F95Zone URL or thread ID", game.Title)
	}

	slog.Info("scraping game", "id", id, "url", url)
	data, err := client.ScrapeThread(url)
	if err != nil {
		return fmt.Errorf("scraping failed: %w", err)
	}

	scraper.ApplyThreadData(game, data, url)
	if err := a.db.UpdateGame(game); err != nil {
		return fmt.Errorf("saving game data: %w", err)
	}

	// Save scraped metadata.
	if data.Developer != "" || data.Overview != "" || data.CoverURL != "" {
		meta := &db.ScrapedMeta{
			GameID:    game.ID,
			Developer: data.Developer,
			Overview:  data.Overview,
			CoverURL:  data.CoverURL,
		}
		if err := a.db.UpsertScrapedMeta(meta); err != nil {
			slog.Warn("failed to save scraped metadata", "game", game.Title, "error", err)
		}
		if data.CoverURL != "" {
			a.cacheCover(game.ID, data.CoverURL)
		}
	}

	// Refresh download links from the scraped thread data.
	// Stale links (pointing to old version files) are replaced with fresh ones.
	if len(data.DownloadLinks) > 0 {
		slog.Info("refreshing download links", "game", game.Title, "count", len(data.DownloadLinks))
		if err := a.db.DeleteDownloadLinksByGameID(game.ID); err != nil {
			slog.Warn("failed to clear old download links", "game", game.Title, "error", err)
		} else {
			for _, dl := range data.DownloadLinks {
				p := downloader.DetectPlatformFromLink(dl.Name, dl.URL)
				link := &db.DownloadLink{
					GameID:   game.ID,
					URL:      dl.URL,
					Host:     dl.Host,
					Name:     dl.Name,
					Platform: db.Platform(p),
					IsDead:   false,
				}
				if _, err := a.db.CreateDownloadLink(link); err != nil {
					slog.Warn("failed to save download link", "game", game.Title, "error", err)
				}
			}
		}
	}

	slog.Info("game sync complete", "id", id, "title", data.Title, "version", data.Version,
		"downloadLinks", len(data.DownloadLinks))
	return nil
}

// ---------------------------------------------------------------------------
// Game update pipeline
// ---------------------------------------------------------------------------

// GetGameDownloadLinksForUpdate retrieves the best download link for updating
// a game. It wraps GetGameDownloadLinks with selectDownloadLink and converts
// the result to db.DownloadLink for use with downloadGameFile.
func (a *App) GetGameDownloadLinksForUpdate(gameID int64) (*db.DownloadLink, error) {
	links, err := a.GetGameDownloadLinks(gameID)
	if err != nil {
		return nil, fmt.Errorf("get download links: %w", err)
	}

	best, err := selectDownloadLink(links)
	if err != nil {
		return nil, fmt.Errorf("select download link: %w", err)
	}

	return &db.DownloadLink{
		ID:       best.ID,
		URL:      best.URL,
		Host:     best.Host,
		Name:     best.Name,
		Platform: db.Platform(best.Platform),
		IsDead:   best.IsDead,
	}, nil
}

// runSingleGameUpdate executes the full update pipeline for a single game.
// It is designed to be called from a goroutine. Errors are communicated both
// via Wails events (game-update:error) and as the return value.
func (a *App) runSingleGameUpdate(ctx context.Context, gameID int64) error {
	title := ""
	game := &db.Game{}

	slog.Info("game update: starting pipeline", "gameID", gameID)

	// Track temp dirs for deferred cleanup on error.
	var downloadTempDir, extractDir string
	var needsCleanup bool
	defer func() {
		if needsCleanup {
			if downloadTempDir != "" {
				os.RemoveAll(downloadTempDir)
			}
			if extractDir != "" {
				os.RemoveAll(extractDir)
			}
		}
	}()

	// Mark cleanup needed; set to false on success.
	needsCleanup = true

	// Phase: syncing
	slog.Info("game update: syncing", "gameID", gameID)
	runtime.EventsEmit(a.ctx, "game-update:phase", map[string]interface{}{
		"gameID": gameID,
		"phase":  "syncing",
	})

	if err := a.SyncSingleGame(gameID); err != nil {
		slog.Error("game update: sync failed", "gameID", gameID, "error", err)
		runtime.EventsEmit(a.ctx, "game-update:error", map[string]interface{}{
			"gameID":  gameID,
			"step":    "sync",
			"message": err.Error(),
		})
		return fmt.Errorf("sync single game: %w", err)
	}

	// Get the refreshed game record.
	var err error
	game, err = a.db.GetGame(gameID)
	if err != nil {
		runtime.EventsEmit(a.ctx, "game-update:error", map[string]interface{}{
			"gameID":  gameID,
			"step":    "lookup",
			"message": err.Error(),
		})
		return fmt.Errorf("get game: %w", err)
	}
	if game == nil {
		err = fmt.Errorf("game with id %d not found", gameID)
		runtime.EventsEmit(a.ctx, "game-update:error", map[string]interface{}{
			"gameID":  gameID,
			"step":    "lookup",
			"message": err.Error(),
		})
		return err
	}

	title = game.Title

	// Games added from the F95Zone browser have no directory on disk — their
	// path is a /virtual/ placeholder. There is nothing to merge into, so the
	// pipeline would extract over a nonexistent tree. They must be downloaded
	// through the Downloads view first.
	if strings.HasPrefix(game.Path, db.VirtualPathPrefix) {
		err = fmt.Errorf("%q was added from F95Zone but not yet downloaded — install it from the Downloads view before updating", title)
		runtime.EventsEmit(a.ctx, "game-update:error", map[string]interface{}{
			"gameID":  gameID,
			"step":    "check",
			"message": err.Error(),
		})
		return err
	}

	// Check whether an update is actually needed.
	if game.LatestVersion == "" || game.LatestVersion == game.Version {
		err = fmt.Errorf("no update available (current version: %s)", game.Version)
		runtime.EventsEmit(a.ctx, "game-update:error", map[string]interface{}{
			"gameID":  gameID,
			"step":    "check",
			"message": err.Error(),
		})
		return err
	}

	oldVersion := game.Version

	// Phase: selecting-link
	slog.Info("game update: selecting link", "gameID", gameID)
	runtime.EventsEmit(a.ctx, "game-update:phase", map[string]interface{}{
		"gameID": gameID,
		"phase":  "selecting-link",
	})

	selectedLink, err := a.GetGameDownloadLinksForUpdate(gameID)
	if err != nil {
		runtime.EventsEmit(a.ctx, "game-update:error", map[string]interface{}{
			"gameID":  gameID,
			"step":    "select-link",
			"message": err.Error(),
		})
		return fmt.Errorf("select download link: %w", err)
	}

	// Phase: downloading
	slog.Info("game update: downloading", "gameID", gameID, "host", selectedLink.Host)
	runtime.EventsEmit(a.ctx, "game-update:phase", map[string]interface{}{
		"gameID": gameID,
		"phase":  "downloading",
	})

	archivePath, err := a.downloadGameFile(ctx, "game-update", gameID, *selectedLink)
	if err != nil {
		// downloadGameFile already emits its own error events.
		return fmt.Errorf("download game file: %w", err)
	}
	downloadTempDir = filepath.Dir(archivePath)

	// Phase: extracting
	slog.Info("game update: extracting", "gameID", gameID, "archive", archivePath)
	runtime.EventsEmit(a.ctx, "game-update:phase", map[string]interface{}{
		"gameID": gameID,
		"phase":  "extracting",
	})

	extractDir, err = os.MkdirTemp(os.TempDir(), fmt.Sprintf("moxie-extract-%d-*", gameID))
	if err != nil {
		runtime.EventsEmit(a.ctx, "game-update:error", map[string]interface{}{
			"gameID":  gameID,
			"step":    "extract",
			"message": fmt.Sprintf("Failed to create extraction temp directory: %v", err),
		})
		return fmt.Errorf("create extract temp dir: %w", err)
	}

	extractProgressCb := func(p extractor.Progress) {
		runtime.EventsEmit(a.ctx, "game-update:extract-progress", map[string]interface{}{
			"gameID":         gameID,
			"filesExtracted": p.FilesExtracted,
			"totalFiles":     p.TotalFiles,
			"currentFile":    p.CurrentFile,
		})
	}

	extractedRoot, err := extractor.Extract(ctx, archivePath, extractDir, extractProgressCb)
	if err != nil {
		runtime.EventsEmit(a.ctx, "game-update:error", map[string]interface{}{
			"gameID":  gameID,
			"step":    "extract",
			"message": fmt.Sprintf("Extraction failed: %v", err),
		})
		return fmt.Errorf("extract archive: %w", err)
	}

	// Phase: merging
	slog.Info("game update: merging", "gameID", gameID, "gamePath", game.Path, "engine", game.Engine)
	runtime.EventsEmit(a.ctx, "game-update:phase", map[string]interface{}{
		"gameID": gameID,
		"phase":  "merging",
	})

	mergeResult, err := updater.Merge(game.Path, game.Engine, extractedRoot, true)
	if err != nil {
		runtime.EventsEmit(a.ctx, "game-update:error", map[string]interface{}{
			"gameID":  gameID,
			"step":    "merge",
			"message": fmt.Sprintf("Merge failed: %v", err),
		})
		return fmt.Errorf("merge update: %w", err)
	}

	slog.Info("game update merged", "game", title, "copied", mergeResult.FilesCopied,
		"preserved", mergeResult.FilesPreserved)

	// Phase: updating-db
	slog.Info("game update: updating db", "gameID", gameID,
		"newVersion", game.LatestVersion,
		"oldVersion", game.Version)
	runtime.EventsEmit(a.ctx, "game-update:phase", map[string]interface{}{
		"gameID": gameID,
		"phase":  "updating-db",
	})

	game.Version = game.LatestVersion
	game.SizeBytes = updateDirSize(game.Path)
	game.LastScannedAt = time.Now()

	if err := a.db.UpdateGame(game); err != nil {
		runtime.EventsEmit(a.ctx, "game-update:error", map[string]interface{}{
			"gameID":  gameID,
			"step":    "update-db",
			"message": fmt.Sprintf("Database update failed: %v", err),
		})
		return fmt.Errorf("update game in db: %w", err)
	}

	// Clean up temp files on success.
	os.RemoveAll(downloadTempDir)
	os.RemoveAll(extractDir)
	needsCleanup = false

	// Emit completion.
	runtime.EventsEmit(a.ctx, "game-update:complete", map[string]interface{}{
		"gameID":     gameID,
		"title":      title,
		"oldVersion": oldVersion,
		"newVersion": game.Version,
	})

	slog.Info("game update complete", "game", title, "old", oldVersion, "new", game.Version)
	return nil
}

// DownloadGameUpdate downloads and applies an update for a single game.
// This is a Wails-bound method that runs the full update pipeline in a
// background goroutine to avoid blocking the Wails event loop.
//
// The function returns immediately; the frontend listens for Wails events
// to track progress:
//
//	game-update:phase       { gameID, phase }
//	game-update:download-progress { gameID, bytesDownloaded, totalBytes, speedBytesPerSec, percent }
//	game-update:extract-progress  { gameID, filesExtracted, totalFiles, currentFile }
//	game-update:error       { gameID, step, message }
//	game-update:complete    { gameID, title, oldVersion, newVersion }
func (a *App) DownloadGameUpdate(gameID int64) error {
	if a.db == nil {
		return fmt.Errorf("database not initialized")
	}
	if a.ctx == nil {
		return fmt.Errorf("application context not initialized")
	}

	if !a.updateRunning.CompareAndSwap(false, true) {
		return fmt.Errorf("an update is already in progress")
	}

	a.goBackground("game-update", func(ctx context.Context) {
		// Signal idle only after the lock is actually released — the pipeline
		// emits :complete before returning, so a client that queued the next
		// update on :complete would still find the lock held.
		defer func() {
			a.updateRunning.Store(false)
			runtime.EventsEmit(a.ctx, "game-update:idle", map[string]interface{}{})
		}()
		ctx = a.beginCancellableUpdate(ctx)
		defer a.endCancellableUpdate()
		a.runSingleGameUpdate(ctx, gameID)
	})
	return nil
}

// beginCancellableUpdate derives a cancellable child of ctx and publishes its
// cancel func so CancelGameUpdate can reach it.
func (a *App) beginCancellableUpdate(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	a.updateCancelMu.Lock()
	a.updateCancel = cancel
	a.updateCancelMu.Unlock()
	return ctx
}

// endCancellableUpdate releases the published cancel func.
func (a *App) endCancellableUpdate() {
	a.updateCancelMu.Lock()
	if a.updateCancel != nil {
		a.updateCancel()
		a.updateCancel = nil
	}
	a.updateCancelMu.Unlock()
}

// CancelGameUpdate aborts the in-flight game update, if any. It returns
// whether a run was actually cancelled so the frontend can report accurately.
func (a *App) CancelGameUpdate() bool {
	a.updateCancelMu.Lock()
	cancel := a.updateCancel
	a.updateCancelMu.Unlock()
	if cancel == nil {
		return false
	}
	slog.Info("game update cancelled by user")
	cancel()
	runtime.EventsEmit(a.ctx, "game-update:cancelled", map[string]interface{}{})
	return true
}

// DownloadAllUpdates downloads and applies updates for all games that have
// updates available. This is a Wails-bound method that runs the batch update
// pipeline in a background goroutine. Games are updated sequentially.
//
// The function returns immediately; the frontend listens for Wails events
// to track progress:
//
//	game-update:batch-start     { total }
//	game-update:batch-progress  { current, total, currentGameTitle }
//	game-update:game-done       { gameID, title, success, error }
//	game-update:batch-complete  { succeeded, failed, total }
//	game-update:error           { gameID, step, message }
func (a *App) DownloadAllUpdates() error {
	if a.db == nil {
		return fmt.Errorf("database not initialized")
	}
	if a.ctx == nil {
		return fmt.Errorf("application context not initialized")
	}

	if !a.updateRunning.CompareAndSwap(false, true) {
		return fmt.Errorf("an update is already in progress")
	}

	a.goBackground("game-update", func(ctx context.Context) {
		// Signal idle only after the lock is actually released — the pipeline
		// emits :complete before returning, so a client that queued the next
		// update on :complete would still find the lock held.
		defer func() {
			a.updateRunning.Store(false)
			runtime.EventsEmit(a.ctx, "game-update:idle", map[string]interface{}{})
		}()
		ctx = a.beginCancellableUpdate(ctx)
		defer a.endCancellableUpdate()

		all, err := a.GetUpdatableGames()
		if err != nil {
			runtime.EventsEmit(a.ctx, "game-update:error", map[string]interface{}{
				"gameID":  0,
				"step":    "list-updatable",
				"message": err.Error(),
			})
			return
		}

		// Drop not-yet-downloaded games up front so they don't show up as
		// batch failures — runSingleGameUpdate rejects them individually.
		games := make([]DesktopGameSummary, 0, len(all))
		for _, g := range all {
			if strings.HasPrefix(g.Path, db.VirtualPathPrefix) {
				continue
			}
			games = append(games, g)
		}
		if skipped := len(all) - len(games); skipped > 0 {
			slog.Info("batch update: skipping games not downloaded yet", "count", skipped)
		}

		runtime.EventsEmit(a.ctx, "game-update:batch-start", map[string]interface{}{
			"total": len(games),
		})

		succeeded := 0
		failed := 0

		for i, g := range games {
			if ctx.Err() != nil {
				break
			}
			runtime.EventsEmit(a.ctx, "game-update:batch-progress", map[string]interface{}{
				"current":          i + 1,
				"total":            len(games),
				"currentGameTitle": g.Title,
			})

			err := a.runSingleGameUpdate(ctx, g.ID)
			if err != nil {
				failed++
				runtime.EventsEmit(a.ctx, "game-update:game-done", map[string]interface{}{
					"gameID":  g.ID,
					"title":   g.Title,
					"success": false,
					"error":   err.Error(),
				})
			} else {
				succeeded++
				runtime.EventsEmit(a.ctx, "game-update:game-done", map[string]interface{}{
					"gameID":  g.ID,
					"title":   g.Title,
					"success": true,
				})
			}
		}

		runtime.EventsEmit(a.ctx, "game-update:batch-complete", map[string]interface{}{
			"succeeded": succeeded,
			"failed":    failed,
			"total":     len(games),
		})

		slog.Info("batch update complete", "succeeded", succeeded, "failed", failed, "total", len(games))
	})

	return nil
}

// ---------------------------------------------------------------------------
// Fresh install pipeline
// ---------------------------------------------------------------------------

// InstallTarget describes where a game can be installed to.
type InstallTarget struct {
	Path      string `json:"path"`
	Available bool   `json:"available"`
}

// GetInstallTargets returns the configured scan paths that currently exist on
// disk, for use as install destinations. Installing into a scan path means the
// directory watcher picks the game up immediately.
func (a *App) GetInstallTargets() []InstallTarget {
	paths := a.GetScanPaths()
	targets := make([]InstallTarget, 0, len(paths))
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		info, serr := os.Stat(abs)
		targets = append(targets, InstallTarget{
			Path:      abs,
			Available: serr == nil && info.IsDir(),
		})
	}
	return targets
}

// installableTitle turns a game title into a directory name that is safe on
// every supported platform.
func installableTitle(title string) string {
	replacer := strings.NewReplacer(
		"/", "-", "\\", "-", ":", "-", "*", "-", "?", "",
		"\"", "", "<", "", ">", "", "|", "-",
	)
	cleaned := strings.TrimSpace(replacer.Replace(title))
	cleaned = strings.Trim(cleaned, ".")
	if cleaned == "" {
		cleaned = "game"
	}
	return cleaned
}

// runGameInstall downloads a game and installs it into destParent, then
// rewrites the game record to point at the real directory. It is the path that
// turns a browser-added /virtual/ entry into an installed game.
func (a *App) runGameInstall(ctx context.Context, gameID int64, destParent string) error {
	emitErr := func(step string, err error) error {
		runtime.EventsEmit(a.ctx, "game-install:error", map[string]interface{}{
			"gameID":  gameID,
			"step":    step,
			"message": err.Error(),
		})
		return err
	}
	phase := func(name string) {
		runtime.EventsEmit(a.ctx, "game-install:phase", map[string]interface{}{
			"gameID": gameID,
			"phase":  name,
		})
	}

	game, err := a.db.GetGame(gameID)
	if err != nil {
		return emitErr("lookup", fmt.Errorf("get game: %w", err))
	}
	if game == nil {
		return emitErr("lookup", fmt.Errorf("game with id %d not found", gameID))
	}

	// Refuse to install over an existing installation — that is what the
	// update pipeline is for, and it knows how to preserve saves.
	if !strings.HasPrefix(game.Path, db.VirtualPathPrefix) {
		if _, serr := os.Stat(game.Path); serr == nil {
			return emitErr("check", fmt.Errorf("%q is already installed at %s — use Update to upgrade it", game.Title, game.Path))
		}
	}

	destParent = strings.TrimSpace(destParent)
	if destParent == "" {
		return emitErr("check", fmt.Errorf("no install directory chosen"))
	}
	destParent, err = filepath.Abs(destParent)
	if err != nil {
		return emitErr("check", fmt.Errorf("resolving install directory: %w", err))
	}
	if info, serr := os.Stat(destParent); serr != nil || !info.IsDir() {
		return emitErr("check", fmt.Errorf("install directory is not available: %s", destParent))
	}

	targetDir := filepath.Join(destParent, installableTitle(game.Title))
	if _, serr := os.Stat(targetDir); serr == nil {
		return emitErr("check", fmt.Errorf("target directory already exists: %s", targetDir))
	}

	// Track temp dirs for cleanup; targetDir is removed only if we created it
	// and then failed, so a partial install never lingers.
	var downloadTempDir, extractDir string
	createdTarget := false
	success := false
	defer func() {
		if downloadTempDir != "" {
			os.RemoveAll(downloadTempDir)
		}
		if extractDir != "" {
			os.RemoveAll(extractDir)
		}
		if !success && createdTarget {
			os.RemoveAll(targetDir)
		}
	}()

	phase("selecting-link")
	selectedLink, err := a.GetGameDownloadLinksForUpdate(gameID)
	if err != nil {
		return emitErr("select-link", err)
	}

	phase("downloading")
	archivePath, err := a.downloadGameFile(ctx, "game-install", gameID, *selectedLink)
	if err != nil {
		// downloadGameFile emits its own error events.
		return fmt.Errorf("download game file: %w", err)
	}
	downloadTempDir = filepath.Dir(archivePath)

	phase("extracting")
	extractDir, err = os.MkdirTemp(os.TempDir(), fmt.Sprintf("moxie-install-%d-*", gameID))
	if err != nil {
		return emitErr("extract", fmt.Errorf("create extract temp dir: %w", err))
	}
	extractedRoot, err := extractor.Extract(ctx, archivePath, extractDir, func(p extractor.Progress) {
		runtime.EventsEmit(a.ctx, "game-install:extract-progress", map[string]interface{}{
			"gameID":         gameID,
			"filesExtracted": p.FilesExtracted,
			"totalFiles":     p.TotalFiles,
			"currentFile":    p.CurrentFile,
		})
	})
	if err != nil {
		return emitErr("extract", fmt.Errorf("extract archive: %w", err))
	}

	phase("installing")
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return emitErr("install", fmt.Errorf("create target directory: %w", err))
	}
	createdTarget = true

	// Reuse the updater's copy logic. backup=false because the target is a
	// directory we just created — there is nothing to preserve.
	if _, err := updater.Merge(targetDir, game.Engine, extractedRoot, false); err != nil {
		return emitErr("install", fmt.Errorf("install files: %w", err))
	}

	phase("updating-db")
	game.Path = targetDir
	game.ExePath = launcher.ResolveExecutable(targetDir, "")
	game.SizeBytes = updateDirSize(targetDir)
	game.LastScannedAt = time.Now().UTC()
	game.DirMTime = dirModTime(targetDir)
	if game.LatestVersion != "" {
		game.Version = game.LatestVersion
	}
	if err := a.db.UpdateGame(game); err != nil {
		return emitErr("update-db", fmt.Errorf("update game in db: %w", err))
	}

	success = true
	runtime.EventsEmit(a.ctx, "game-install:complete", map[string]interface{}{
		"gameID":  gameID,
		"title":   game.Title,
		"path":    targetDir,
		"version": game.Version,
	})
	slog.Info("game installed", "gameID", gameID, "title", game.Title, "path", targetDir)
	return nil
}

// InstallGame downloads a game and installs it into destParent, which must be
// one of the configured scan paths. It returns immediately; progress arrives
// as game-install:* events mirroring the game-update:* set.
func (a *App) InstallGame(gameID int64, destParent string) error {
	if a.db == nil {
		return fmt.Errorf("database not initialized")
	}
	if a.ctx == nil {
		return fmt.Errorf("application context not initialized")
	}

	// Shares the update lock: both pipelines download, extract and write into
	// game directories, and running them together is asking for trouble.
	if !a.updateRunning.CompareAndSwap(false, true) {
		return fmt.Errorf("an update or install is already in progress")
	}

	a.goBackground("game-install", func(ctx context.Context) {
		defer func() {
			a.updateRunning.Store(false)
			runtime.EventsEmit(a.ctx, "game-update:idle", map[string]interface{}{})
		}()
		ctx = a.beginCancellableUpdate(ctx)
		defer a.endCancellableUpdate()
		a.runGameInstall(ctx, gameID, destParent)
	})
	return nil
}

// updateDirSize calculates the total size of a directory and all its contents
// recursively. It silently skips any files that cannot be read.
func updateDirSize(dir string) int64 {
	var total int64
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible entries
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total
}
