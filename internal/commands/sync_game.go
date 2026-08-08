package commands

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/engine"
	"github.com/mili/moxie/internal/scraper"
	"github.com/mili/moxie/internal/steam"
	"github.com/mili/moxie/internal/util"
	"github.com/mili/moxie/internal/version"
)

// SyncGameLogic performs the core sync logic for a single game and returns
// structured results suitable for CLI rendering.  This is separated so the
// business logic can be tested without os.Exit or interactive prompts.
// When interactive is true, engine mismatch prompts will block for user input.
// When false, mismatches are returned as non-fatal warnings in the result.
func SyncGameLogic(database *db.Database, game *db.Game, client *scraper.Client, force bool, interactive bool) (*SyncGameResult, error) {
	result := &SyncGameResult{
		OldVersion: game.Version,
	}

	// Load the association cache so GetCachedThreadID works.
	scraper.LoadAssociationCache()

	// Phase 1: Associate if needed.
	if game.F95URL == "" {
		if client == nil {
			return nil, fmt.Errorf("cannot search F95Zone: no scraper client available")
		}

		query := scraper.SanitizeTitle(game.Title)
		if query == "" {
			query = game.Title
		}

		// Check persistent association cache first.
		var best *scraper.SearchResult
		var bestScore float64
		var fromCache bool
		var detEngine engine.Result // set below when not from cache

		if cachedID := scraper.GetCachedThreadID(query); cachedID > 0 {
			cachedURL := scraper.ThreadURL(cachedID)
			best = &scraper.SearchResult{Title: game.Title, URL: cachedURL}
			bestScore = 1.0
			fromCache = true
			if interactive {
				fmt.Fprintf(os.Stderr, "  Using cached thread %d for %q\n", cachedID, game.Title)
			}
		} else {
			if interactive {
				fmt.Fprintf(os.Stderr, "  Searching F95Zone for %q...\n", game.Title)
			}
			searchResults, err := client.SearchF95Zone(query)
			if err != nil {
				return nil, fmt.Errorf("search failed: %w", err)
			}
			result.SearchResults = searchResults

			if len(searchResults) == 0 {
				return nil, fmt.Errorf("no F95Zone results found for %q", game.Title)
			}

			// Pre-detect engine so we can boost candidates whose titles
			// contain matching engine keywords (e.g., "RPGM Completed
			// Demons Roots" over "[Translation Request] Demons Roots").
			detEngine = engine.Detect(game.Path)
			engVariants, hasEngVariants := engine.EngineTagVariants[string(detEngine.Engine)]

			// Pick best match.
			for i, r := range searchResults {
				score := scraper.ComputeMatchScore(game.Title, r.Title)
				// Boost candidates whose title contains engine keywords
				// matching the detected engine (e.g. RPGM, Unity, Flash).
				// +0.15 is enough to push a 0.85 engine-tagged release
				// thread above a 1.00 bare-title request thread.
				if hasEngVariants {
					titleLower := strings.ToLower(r.Title)
					for _, variant := range engVariants {
						if strings.Contains(titleLower, variant) {
							score += 0.15
							if score > 1.0 {
								score = 1.0
							}
							break
						}
					}
				}
				marker := "  "
				if score > bestScore {
					bestScore = score
					best = &searchResults[i]
					marker = "→ "
				}
				if interactive {
					fmt.Fprintf(os.Stderr, "  %s[%.0f%%] %s\n", marker, score*100, r.Title)
				}
			}
		}
		result.BestMatch = best
		result.BestScore = bestScore

		if best == nil || bestScore < 0.3 {
			return nil, fmt.Errorf("no good match found (best score: %.0f%%)", bestScore*100)
		}

		if interactive && !fromCache {
			fmt.Fprintf(os.Stderr, "  Scraping %s...\n", best.URL)
		}
		data, err := client.ScrapeThread(best.URL)
		if err != nil {
			return nil, fmt.Errorf("scrape failed: %w", err)
		}
		result.ThreadData = data

		// Check engine consistency before associating (skip when
		// using cached association — already verified previously).
		if !fromCache {
			if !engine.EngineMatchesThread(detEngine, data.Tags, best.Title) {
				result.EngineMismatch = true
				if interactive {
					fmt.Fprintf(os.Stderr, "  ⚠ Engine mismatch (scanner: %s, thread: %q, tags: %s)\n",
						detEngine.Engine, util.Truncate(best.Title, 60), engine.FormatTagsBrief(data.Tags, 4))
					fmt.Fprintf(os.Stderr, "  Associate anyway? [y/N]: ")
					var answer string
					fmt.Scanln(&answer)
					if strings.ToLower(answer) != "y" {
						fmt.Fprintln(os.Stderr, "  Cancelled.")
						return nil, fmt.Errorf("association cancelled")
					}
				}
			}
		}

		scraper.ApplyThreadData(game, data, best.URL)
		if err := database.UpdateGame(game); err != nil {
			return nil, fmt.Errorf("save failed: %w", err)
		}

		// Cache the successful association so future runs skip searching.
		if data.ThreadID > 0 {
			scraper.SetCachedThreadID(query, data.ThreadID)
		}

		if data.Developer != "" || data.Overview != "" || data.CoverURL != "" {
			meta := &db.ScrapedMeta{
				GameID:    game.ID,
				Developer: data.Developer,
				Overview:  data.Overview,
				CoverURL:  data.CoverURL,
			}
			if err := database.UpsertScrapedMeta(meta); err != nil {
				if interactive {
					fmt.Fprintf(os.Stderr, "  ⚠ Failed to save metadata for %q: %v\n", game.Title, err)
				}
			} else {
				result.ScrapedMetadata = true
			}
		}

		result.Associated = true
	}

	// Phase 2: Check for updates (skip if recently checked, unless --force).
	if !force && !game.VersionCheckedAt.IsZero() && time.Since(game.VersionCheckedAt) < UpdateCheckCooldown {
		result.CooldownSkipped = true
		return result, nil
	}
	if interactive {
		fmt.Fprintf(os.Stderr, "  Checking for updates...\n")
	}

	if game.F95URL != "" {
		if client == nil {
			return result, fmt.Errorf("scrape failed: no scraper client available")
		}

		// Use slug-agnostic URL from thread ID when available so version
		// changes in the URL slug don't break future scrapes.
		scrapeURL := scraper.ResolveScrapeURL(game.F95URL, game.F95ThreadID)
		data, err := client.ScrapeThread(scrapeURL)
		if err != nil {
			return result, fmt.Errorf("scrape failed: %w", err)
		}
		result.ThreadData = data

		// Signal: check engine consistency with freshly scraped metadata.
		detEngine := engine.Detect(game.Path)
		if !engine.EngineMatchesThread(detEngine, data.Tags, data.Title) {
			result.EngineMismatch = true
		}

		latest := data.Version
		knownVer := game.Version
		if knownVer == "" {
			knownVer = game.LatestVersion
		}
		result.OldVersion = knownVer
		result.NewVersion = latest

		// Status and tags are scraped here too; persist them so status
		// transitions are recorded on the update path, not just on
		// association (where ApplyThreadData handles them).
		if data.Status != "" {
			game.Status = data.Status
		}
		if len(data.Tags) > 0 {
			game.Tags = data.Tags
		}

		game.LatestVersion = latest
		game.VersionCheckedAt = time.Now()

		// Update StoreLinks and SteamAppID from scraped thread data.
		if len(data.StoreLinks) > 0 {
			game.StoreLinks = data.StoreLinks
		}
		if steamURL, hasSteam := data.StoreLinks["steam"]; hasSteam {
			if appID, ok := steam.ExtractSteamAppID(steamURL); ok {
				game.SteamAppID = int64(appID)
			}
		}

		if err := database.UpdateGame(game); err != nil {
			if interactive {
				fmt.Fprintf(os.Stderr, "  ⚠ Failed to save version data for %q: %v\n", game.Title, err)
			}
		}

		// Save scraped metadata (cover, developer, overview) from the scrape.
		if data.Developer != "" || data.Overview != "" || data.CoverURL != "" {
			if err := database.UpsertScrapedMeta(&db.ScrapedMeta{
				GameID:    game.ID,
				Developer: data.Developer,
				Overview:  data.Overview,
				CoverURL:  data.CoverURL,
			}); err != nil {
				if interactive {
					fmt.Fprintf(os.Stderr, "  ⚠ Failed to save metadata for %q: %v\n", game.Title, err)
				}
			} else {
				result.ScrapedMetadata = true
			}
		}

		if latest != "" && knownVer != "" {
			switch version.Compare(latest, knownVer) {
			case version.Newer, version.Changed:
				result.VersionUpdated = true
			}
		}
	}

	return result, nil
}
