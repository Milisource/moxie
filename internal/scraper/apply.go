package scraper

import (
	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/engine"
	"github.com/mili/moxie/internal/steam"
)

// ApplyThreadData copies scraped ThreadData fields onto a Game.
// Non-empty fields in data overwrite the corresponding game fields.
func ApplyThreadData(game *db.Game, data *ThreadData, url string) {
	if data.Title != "" {
		// Extract engine from F95Zone thread prefix before stripping.
		// Useful when scanner can't determine the engine (e.g., RPG Maker
		// MV/MZ games with HTML/JS files get detected as "HTML").
		if f95Eng := engine.ExtractEngineFromTitle(data.Title); f95Eng != "" {
			if game.Engine == "" || game.Engine == "Unknown" || game.Engine == "Others" {
				game.Engine = f95Eng
			}
		}
		game.Title = StripThreadPrefix(data.Title)
	}
	if data.Version != "" {
		// LatestVersion always tracks the F95Zone version for
		// update detection. Version is the locally-installed
		// version (from directory name scan) — never overwrite
		// it with F95Zone data so the update comparison works.
		game.LatestVersion = data.Version
	}
	if data.ThreadID > 0 {
		game.F95ThreadID = data.ThreadID
	}
	game.F95URL = url
	if len(data.Tags) > 0 {
		game.Tags = data.Tags
	}
	if data.Status != "" {
		game.Status = data.Status
	}
	if len(data.StoreLinks) > 0 {
		game.StoreLinks = data.StoreLinks
	}
	if steamURL, hasSteam := data.StoreLinks["steam"]; hasSteam {
		if appID, ok := steam.ExtractSteamAppID(steamURL); ok {
			game.SteamAppID = int64(appID)
		}
	}
}
