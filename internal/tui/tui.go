// Package tui provides a Bubble Tea terminal UI for browsing and managing
// the F95 game library. It presents an interactive table view of all games
// with filtering, sorting, detail inspection, and game management.
package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mili/moxie/internal/config"
	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/saves"
	"github.com/mili/moxie/internal/scraper"
)

// Run opens the database at dbPath, creates the Bubble Tea program, and runs
// the interactive terminal UI. The database is closed when the program exits.
// scraperClient may be nil — if nil, URL updates won't trigger re-scraping.
// f95Cookie is the F95Zone session cookie used to authenticate download HEAD requests.
func Run(dbPath string, scraperClient *scraper.Client, f95Cookie string) error {
	database, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	// Build the saves engine up front. A failure here is not fatal — the TUI
	// still browses the library, and the Saves screen surfaces the message.
	savesEngine, savesErr := openSavesEngine(database)

	p := tea.NewProgram(
		initialModel(database, scraperClient, f95Cookie, savesEngine, savesErr),
		tea.WithAltScreen(), tea.WithMouseAllMotion())
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}

// openSavesEngine builds a saves.Engine rooted at the configured saves_dir,
// falling back to config.DefaultSavesDir(). It mirrors the CLI construction in
// internal/commands/saves.go without importing that package.
func openSavesEngine(database *db.Database) (*saves.Engine, error) {
	root := config.DefaultSavesDir()
	if cfg, err := config.ReadConfig(); err == nil {
		if v := cfg.Get("saves_dir"); v != "" {
			root = v
		}
	}
	return saves.NewEngine(database, root)
}
