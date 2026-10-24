package scraper

import (
	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/engine"
	"github.com/mili/moxie/internal/steam"
)

// ResolveStatus combines a freshly parsed status with the stored one.
// An explicit parsed status (completed/abandoned/on_hold) always wins.
// Otherwise the stored status is kept — except that an empty or
// "unknown" stored status is defaulted to "active": F95Zone threads
// carry no "active" tag, so the absence of a Completed/Abandoned/On
// Hold tag means the game is in active development (the same inference
// F95Checker makes). A stored non-unknown status — typically user-set
// via the TUI/desktop — is never clobbered by that inference.
func ResolveStatus(parsed, current string) string {
	if parsed != "" {
		return parsed
	}
	if current == "" || current == "unknown" {
		return "active"
	}
	return current
}

// ThreadEngine returns the engine implied by the thread's prefix labels,
// falling back to an engine word at the start of the title. Prefix labels are
// authoritative: F95Zone renders them separately from the title and the parser
// strips them, so the title does not carry the engine for those threads.
//
// Useful when the scanner can't determine the engine (e.g., RPG Maker MV/MZ
// games with HTML/JS files get detected as "HTML").
func ThreadEngine(data *ThreadData) string {
	if eng := engine.EngineFromPrefixes(data.Prefixes); eng != "" {
		return eng
	}
	return engine.ExtractEngineFromTitle(data.Title)
}

// ThreadTitle returns the game title from scraped thread data. The current
// parser strips F95Zone's prefix labels out of the title and exposes them in
// Prefixes, so stripping again would eat a legitimate leading word (a game
// genuinely titled "Unity of Command", for example). Thread data without
// prefixes is legacy or externally constructed and keeps the old strip
// behaviour.
func ThreadTitle(data *ThreadData) string {
	if len(data.Prefixes) > 0 {
		return data.Title
	}
	return StripThreadPrefix(data.Title)
}

// ApplyThreadData copies scraped ThreadData fields onto a Game.
// Non-empty fields in data overwrite the corresponding game fields.
func ApplyThreadData(game *db.Game, data *ThreadData, url string) {
	if data.Title != "" {
		if eng := ThreadEngine(data); eng != "" {
			if game.Engine == "" || game.Engine == "Unknown" || game.Engine == "Others" {
				game.Engine = eng
				game.EngineSource = "f95"
			}
		}
		game.Title = ThreadTitle(data)
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
	game.Status = ResolveStatus(data.Status, game.Status)
	if len(data.StoreLinks) > 0 {
		game.StoreLinks = data.StoreLinks
	}
	if steamURL, hasSteam := data.StoreLinks["steam"]; hasSteam {
		if appID, ok := steam.ExtractSteamAppID(steamURL); ok {
			game.SteamAppID = int64(appID)
		}
	}
}
