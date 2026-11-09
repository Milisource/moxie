package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mili/moxie/internal/archive"
	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/downloader"
	"github.com/mili/moxie/internal/log"
)

// When an automatic update fails, the user often downloads the archive from
// the browser page Moxie opened. Without a watcher that file is invisible to
// the pipeline: the row stays in its manual-required state until the user
// finds the "provide file" action. This watcher closes that loop — it watches
// the browser download directory for a stable new archive, matches it to a
// game whose update is waiting, and resumes the normal extract/merge path.

const (
	manualWatchInterval = 3 * time.Second
	manualWatchSettle   = 2 * time.Second
	manualWatchTTL      = 30 * time.Minute
)

type manualWaiter struct {
	title string
	since time.Time
}

type manualWatcher struct {
	mu      sync.Mutex
	waiters map[int64]manualWaiter
	seen    map[string]bool
	running bool
}

var manualDownloadWatch = &manualWatcher{
	waiters: make(map[int64]manualWaiter),
	seen:    make(map[string]bool),
}

// watchForManualUpdate starts (or joins) the download-directory watcher for a
// game whose automatic update failed.
func (a *App) watchForManualUpdate(gameID int64, title string) {
	dir := manualDownloadDir()
	if dir == "" {
		log.Debug("manual update watcher: no download directory found")
		return
	}

	w := manualDownloadWatch
	w.mu.Lock()
	if !w.running && len(w.waiters) == 0 {
		// A fresh watch starts from what is already on disk: those files
		// predate this failure, and old names must not block a new download
		// of the same file later.
		w.seen = make(map[string]bool)
		for _, name := range listArchiveFiles(dir) {
			w.seen[name] = true
		}
	}
	w.waiters[gameID] = manualWaiter{title: title, since: time.Now()}
	start := !w.running
	w.running = true
	w.mu.Unlock()

	log.Info("manual update watcher started", "gameID", gameID, "dir", dir)
	if start {
		go a.manualDownloadLoop()
	}
}

// manualDownloadLoop polls the download directory until every waiter is
// satisfied or expires.
func (a *App) manualDownloadLoop() {
	w := manualDownloadWatch
	ticker := time.NewTicker(manualWatchInterval)
	defer ticker.Stop()

	for range ticker.C {
		dir := manualDownloadDir()
		if dir != "" {
			for _, name := range listArchiveFiles(dir) {
				if w.alreadySeen(name) {
					continue
				}
				full := filepath.Join(dir, name)
				info, err := os.Stat(full)
				if err != nil || time.Since(info.ModTime()) < manualWatchSettle {
					continue // still being written (or vanished)
				}

				gameID, ok := w.match(name)
				if !ok {
					// Nothing waiting for this name; remember it and move on.
					w.markSeen(name)
					continue
				}
				if err := a.provideUpdateFileFromPath(gameID, full); err != nil {
					// The previous run may still hold the game's claim; retry
					// on the next tick rather than dropping the file.
					log.Debug("manual update watcher: resuming failed, will retry",
						"gameID", gameID, "file", name, "error", err)
					continue
				}
				w.markSeen(name)
				w.removeWaiter(gameID)
				log.Info("manual update watcher: applied downloaded archive",
					"gameID", gameID, "file", name)
			}
		}

		w.mu.Lock()
		for id, wt := range w.waiters {
			if time.Since(wt.since) > manualWatchTTL {
				delete(w.waiters, id)
				log.Info("manual update watcher: stopped waiting", "gameID", id)
			}
		}
		empty := len(w.waiters) == 0
		if empty {
			w.running = false
		}
		w.mu.Unlock()
		if empty {
			return
		}
	}
}

func (w *manualWatcher) alreadySeen(name string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.seen[name]
}

func (w *manualWatcher) markSeen(name string) {
	w.mu.Lock()
	w.seen[name] = true
	w.mu.Unlock()
}

func (w *manualWatcher) removeWaiter(gameID int64) {
	w.mu.Lock()
	delete(w.waiters, gameID)
	w.mu.Unlock()
}

// match finds the waiting game an archive belongs to: a title match wins; with
// exactly one waiter, that waiter is the only possible answer.
func (w *manualWatcher) match(filename string) (int64, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.waiters) == 0 {
		return 0, false
	}

	bestID := int64(0)
	bestScore := 0
	for id, wt := range w.waiters {
		score := titleMatchScore(filename, wt.title)
		if score > bestScore {
			bestScore, bestID = score, id
		}
	}
	if bestID != 0 {
		return bestID, true
	}
	if len(w.waiters) == 1 {
		for id := range w.waiters {
			return id, true
		}
	}
	return 0, false
}

// titleMatchScore reports how strongly an archive filename names a game. Both
// sides are normalized to lowercase words, so "Luna" cannot claim "Lunatic".
func titleMatchScore(filename, title string) int {
	name := " " + normalizeMatchText(filename) + " "
	want := normalizeMatchText(title)
	if len(want) < 4 {
		return 0
	}
	if strings.Contains(name, " "+want+" ") {
		return len(want)
	}
	return 0
}

// normalizeMatchText lowercases and joins the alphanumeric words so
// "Ashfall_RPG_v0.5.5.zip" matches "Ashfall RPG" without matching inside a
// longer word.
func normalizeMatchText(s string) string {
	var words []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			words = append(words, b.String())
			b.Reset()
		}
	}
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return strings.Join(words, " ")
}

// manualDownloadDir returns the browser download directory, or "" when none
// exists.
func manualDownloadDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, "Downloads")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return ""
}

// listArchiveFiles returns the archive file names directly inside dir.
func listArchiveFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if archive.IsArchiveFile(filepath.Join(dir, e.Name())) {
			out = append(out, e.Name())
		}
	}
	return out
}

// provideUpdateFileFromPath applies a manually downloaded archive to gameID,
// sharing the extract/merge pipeline with the file picker. It never modifies
// or deletes the user's file.
func (a *App) provideUpdateFileFromPath(gameID int64, archivePath string) error {
	if a.db == nil {
		return fmt.Errorf("database not initialized")
	}
	if a.ctx == nil {
		return fmt.Errorf("application context not initialized")
	}
	return a.startGameRun(gameID, "game-update", true, func(ctx context.Context) {
		game, err := a.db.GetGame(gameID)
		if err != nil {
			a.emitUpdateError(gameID, "lookup", err.Error())
			return
		}
		if game == nil {
			a.emitUpdateError(gameID, "lookup", fmt.Sprintf("game with id %d not found", gameID))
			return
		}
		if db.IsVirtualPath(game.Path) {
			a.emitUpdateError(gameID, "check",
				fmt.Sprintf("%q was added from F95Zone but not yet downloaded — install it from the Downloads view before updating", game.Title))
			return
		}
		if !updatable(game.LatestVersion, game.Version) {
			a.emitUpdateError(gameID, "check",
				fmt.Sprintf("no update available (current version: %s)", game.Version))
			return
		}
		if _, err := os.Stat(archivePath); err != nil {
			a.emitUpdateError(gameID, "select-file", fmt.Sprintf("Downloaded file not found: %v", err))
			return
		}
		if !archive.IsArchiveFile(archivePath) {
			a.emitUpdateError(gameID, "select-file",
				"Downloaded file is not a recognized archive (zip, 7z, rar, tar, tar.gz, tar.bz2, tar.xz)")
			return
		}
		log.Info("game update: manual archive picked up from downloads", "gameID", gameID, "file", archivePath)
		a.applyGameUpdateArchive(ctx, game, archivePath,
			downloader.ClassifyLink(filepath.Base(archivePath)).Kind == downloader.LinkPatch)
	})
}
