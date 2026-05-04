package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/engine"
	"github.com/mili/moxie/internal/log"
	"github.com/mili/moxie/internal/scraper"
	"github.com/mili/moxie/internal/util"
)

// UpdateResult holds the outcome of checking one game for updates.
type UpdateResult struct {
	Game    db.Game `json:"game"`
	Current string  `json:"current"`
	Latest  string  `json:"latest"`
	IsNew   bool    `json:"is_new"`
	Error   string  `json:"error,omitempty"`
}

const UpdateCheckCooldown = 24 * time.Hour

// RunUpdateCheck scrapes each game's F95Zone thread, compares versions, and
// updates the database. It returns the count of games with new versions and
// a result for each game processed.
func RunUpdateCheck(database *db.Database, client *scraper.Client, games []db.Game, force bool) (int, []UpdateResult) {
	// Skip games checked within the last 24 hours (unless --force).
	cooldownSkipped := 0
	var filtered []db.Game
	for _, g := range games {
		if g.F95URL == "" {
			continue
		}
		if !force && !g.VersionCheckedAt.IsZero() && time.Since(g.VersionCheckedAt) < UpdateCheckCooldown {
			cooldownSkipped++
			continue
		}
		filtered = append(filtered, g)
	}
	games = filtered

	log.Info("update check started",
		"total", len(games),
		"cooldown_skipped", cooldownSkipped,
		"force", force,
	)

	if cooldownSkipped > 0 {
		fmt.Fprintf(os.Stderr, "  (skipped %d games checked within the last 24h; use --force to override)\n",
			cooldownSkipped)
	}

	if len(games) == 0 {
		if cooldownSkipped > 0 {
			fmt.Fprintln(os.Stderr, "  (all games are within the 24h cooldown — nothing to check)")
		} else {
			fmt.Fprintln(os.Stderr, "  (no games to check)")
		}
		return 0, nil
	}

	var results []UpdateResult
	updatesFound := 0

	// Worker pool for concurrent scraping.
	sem := make(chan struct{}, 3) // max 3 concurrent
	var mu sync.Mutex            // protects results slice and updatesFound counter
	var saveMu sync.Mutex        // serializes SQLite writes (prevents BUSY errors)
	var wg sync.WaitGroup

	for _, g := range games {
		sem <- struct{}{}
		wg.Add(1)
		go func(g db.Game) {
			defer wg.Done()
			defer func() { <-sem }()

			data, err := client.ScrapeThread(g.F95URL)
			if err != nil {
				mu.Lock()
				fmt.Fprintf(os.Stderr, "  %q ✗ %v\n", g.Title, err)
				results = append(results, UpdateResult{Game: g, Error: err.Error()})
				mu.Unlock()
				return
			}
			latest := data.Version
			knownVer := g.Version
			if knownVer == "" {
				knownVer = g.LatestVersion
			}
			isNew := latest != "" && knownVer != "" && NormalizeVersion(latest) != NormalizeVersion(knownVer)

			// Signal: check for engine mismatch between scanner and F95Zone metadata.
			var engineWarn string
			detEngine := engine.Detect(g.Path)
			if !EngineMatchesThread(detEngine, data.Tags, data.Title) {
				engineWarn = fmt.Sprintf(" ⚠ engine mismatch (scanner: %s)",
					detEngine.Engine)
			}

			g.LatestVersion = latest
			g.VersionCheckedAt = time.Now()
			saveMu.Lock()
			if err := database.UpdateGame(&g); err != nil {
				fmt.Fprintf(os.Stderr, "  ⚠ Failed to save version data for %q: %v\n", g.Title, err)
			}
			saveMu.Unlock()
			// Save scraped metadata (cover, developer, overview).
			if data.Developer != "" || data.Overview != "" || data.CoverURL != "" {
		if err := database.UpsertScrapedMeta(&db.ScrapedMeta{
				GameID:    g.ID,
				Developer: data.Developer,
				Overview:  data.Overview,
				CoverURL:  data.CoverURL,
				}); err != nil {
				fmt.Fprintf(os.Stderr, "  ⚠ Failed to save metadata for %q: %v\n", g.Title, err)
			}
			}
			mu.Lock()
			if isNew {
				updatesFound++
				fmt.Fprintf(os.Stderr, "  %q 🔄 %s → %s%s\n", g.Title, knownVer, latest, engineWarn)
			} else if knownVer != "" {
				// Local version is known and matches F95Zone.
				fmt.Fprintf(os.Stderr, "  %q ✓ %s%s\n", g.Title, latest, engineWarn)
			} else if latest != "" {
				// F95Zone has a version but we don't know the local version.
				fmt.Fprintf(os.Stderr, "  %q ? %s (local version unknown)%s\n", g.Title, latest, engineWarn)
			}
			results = append(results, UpdateResult{Game: g, Current: knownVer, Latest: latest, IsNew: isNew})
			mu.Unlock()
		}(g)
	}
	wg.Wait()

	log.Info("update check complete",
		"updates", updatesFound,
		"total", len(games)+cooldownSkipped,
	)

	return updatesFound, results
}

// NormalizeVersion strips trailing .0 segments and leading v/V prefix for comparison.
func NormalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	for strings.HasSuffix(v, ".0") {
		v = strings.TrimSuffix(v, ".0")
	}
	return v
}

// CheckUpdates checks all games for version updates from F95Zone.
func CheckUpdates(args []string) {
	fs := flag.NewFlagSet("check-updates", flag.ExitOnError)
	cookieStr := fs.String("cookie", "", "Cookie header")
	cookieFile := fs.String("cookie-file", "", "Cookie file")
	jsonOut := fs.Bool("json", false, "JSON output")
	unsafe := fs.Bool("unsafe", false, "⚠ Skip rate limiting")
	force := fs.Bool("force", false, "Force re-check even if checked within 24h")
	fs.Parse(args)

	cookie := ResolveCookie(*cookieStr, *cookieFile)
	if cookie == "" {
		fmt.Fprintf(os.Stderr, "Cookie required. Log into f95zone.to in Firefox.\n")
		os.Exit(1)
	}

	database := OpenDB()
	defer database.Close()

	allGames, err := database.ListGames("", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	var trackable []db.Game
	for _, g := range allGames {
		if g.F95URL != "" {
			trackable = append(trackable, g)
		}
	}
	if len(trackable) == 0 {
		fmt.Println("No games have F95Zone URLs. Run 'moxie sync' first.")
		return
	}

	var client *scraper.Client
	if *unsafe {
		client = scraper.NewUnsafeClient(cookie)
	} else {
		client = scraper.NewClient(cookie)
	}

	updatesFound, results := RunUpdateCheck(database, client, trackable, *force)

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(results)
		return
	}

	fmt.Fprintf(os.Stderr, "\n=== %d updates available ===\n", updatesFound)
	for _, r := range results {
		if r.IsNew {
			fmt.Printf("  %s: %s → %s\n", r.Game.Title, r.Current, r.Latest)
		}
	}
}

// SyncGameResult holds the outcome of a single-game sync operation.
type SyncGameResult struct {
	Associated      bool                    // true if a new F95Zone association was made
	SearchResults   []scraper.SearchResult  // search results (Phase 1 only)
	BestMatch       *scraper.SearchResult   // the chosen best match (Phase 1 only)
	BestScore       float64                 // match score of the best result
	ThreadData      *scraper.ThreadData     // scraped thread data
	EngineMismatch  bool                    // engine mismatch detected between scanner and thread
	VersionUpdated  bool                    // true if a newer version was found
	NewVersion      string                  // latest version from F95Zone
	OldVersion      string                  // previously known version
	ScrapedMetadata bool                    // metadata (developer/overview/cover) was saved
	CooldownSkipped bool                    // skipped because of 24h cooldown
}

// SyncGameLogic performs the core sync logic for a single game and returns
// structured results suitable for CLI rendering.  This is separated so the
// business logic can be tested without os.Exit or interactive prompts.
// When interactive is true, engine mismatch prompts will block for user input.
// When false, mismatches are returned as non-fatal warnings in the result.
func SyncGameLogic(database *db.Database, game *db.Game, client *scraper.Client, force bool, interactive bool) (*SyncGameResult, error) {
	result := &SyncGameResult{
		OldVersion: game.Version,
	}

	// Phase 1: Associate if needed.
	if game.F95URL == "" {
		if client == nil {
			return nil, fmt.Errorf("cannot search F95Zone: no scraper client available")
		}

		if interactive {
			fmt.Fprintf(os.Stderr, "  Searching F95Zone for %q...\n", game.Title)
		}
		query := scraper.SanitizeTitle(game.Title)
		if query == "" {
			query = game.Title
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
		detEngine := engine.Detect(game.Path)
		engVariants, hasEngVariants := EngineTagVariants[string(detEngine.Engine)]

		// Pick best match.
		var best *scraper.SearchResult
		var bestScore float64
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
		result.BestMatch = best
		result.BestScore = bestScore

		if best == nil || bestScore < 0.3 {
			return nil, fmt.Errorf("no good match found (best score: %.0f%%)", bestScore*100)
		}

		if interactive {
			fmt.Fprintf(os.Stderr, "  Scraping %s...\n", best.URL)
		}
		data, err := client.ScrapeThread(best.URL)
		if err != nil {
			return nil, fmt.Errorf("scrape failed: %w", err)
		}
		result.ThreadData = data

		// Check engine consistency before associating.
		if !EngineMatchesThread(detEngine, data.Tags, best.Title) {
			result.EngineMismatch = true
			if interactive {
				fmt.Fprintf(os.Stderr, "  ⚠ Engine mismatch (scanner: %s, thread: %q, tags: %s)\n",
					detEngine.Engine, util.Truncate(best.Title, 60), FormatTagsBrief(data.Tags, 4))
				fmt.Fprintf(os.Stderr, "  Associate anyway? [y/N]: ")
				var answer string
				fmt.Scanln(&answer)
				if strings.ToLower(answer) != "y" {
					fmt.Fprintln(os.Stderr, "  Cancelled.")
					return nil, fmt.Errorf("association cancelled")
				}
			}
		}

		ApplyThreadData(game, data, best.URL)
		if err := database.UpdateGame(game); err != nil {
			return nil, fmt.Errorf("save failed: %w", err)
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

		data, err := client.ScrapeThread(game.F95URL)
		if err != nil {
			return result, fmt.Errorf("scrape failed: %w", err)
		}
		result.ThreadData = data

		// Signal: check engine consistency with freshly scraped metadata.
		detEngine := engine.Detect(game.Path)
		if !EngineMatchesThread(detEngine, data.Tags, data.Title) {
			result.EngineMismatch = true
		}

		latest := data.Version
		knownVer := game.Version
		result.OldVersion = knownVer
		result.NewVersion = latest

		game.LatestVersion = latest
		game.VersionCheckedAt = time.Now()

		// Update StoreLinks and SteamAppID from scraped thread data.
		if len(data.StoreLinks) > 0 {
			game.StoreLinks = data.StoreLinks
		}
		if steamURL, hasSteam := data.StoreLinks["steam"]; hasSteam {
			if appID, ok := ExtractSteamAppID(steamURL); ok {
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

		if latest != "" && knownVer != "" && NormalizeVersion(latest) != NormalizeVersion(knownVer) {
			result.VersionUpdated = true
		}
	}

	return result, nil
}

// SyncGame syncs a single game: associate it with F95Zone (if needed)
// and check for version updates.
func SyncGame(id int64, cookie string, unsafe bool, force bool) {
	database := OpenDB()
	defer database.Close()

	game, err := database.GetGame(id)
	if err != nil || game == nil {
		fmt.Fprintf(os.Stderr, "Game with ID %d not found.\n", id)
		os.Exit(1)
	}

	var client *scraper.Client
	if unsafe {
		client = scraper.NewUnsafeClient(cookie)
	} else {
		client = scraper.NewClient(cookie)
	}

	fmt.Fprintf(os.Stderr, "Syncing: %s\n", game.Title)

	result, err := SyncGameLogic(database, game, client, force, true)
	if err != nil {
		errStr := err.Error()
		// "association cancelled" was already printed by SyncGameLogic
		// with "  Cancelled." — no need to re-print.
		if errStr == "association cancelled" {
			os.Exit(1)
			return
		}
		// For other errors, check whether it was a blocked response.
		if util.IsBlocked(err) {
			fmt.Fprintf(os.Stderr, "  ⚠ BLOCKED: %v\n", err)
		} else if strings.Contains(errStr, "no F95Zone results found for") {
			fmt.Fprintf(os.Stderr, "  ✗ No F95Zone results found for %q.\n", game.Title)
		} else {
			fmt.Fprintf(os.Stderr, "  ✗ %v\n", err)
		}
		os.Exit(1)
	}

	// Phase 1 result.
	if result.Associated && result.ThreadData != nil {
		fmt.Fprintf(os.Stderr, "  ✓ Associated: %s", result.ThreadData.Title)
		if result.ThreadData.Version != "" {
			fmt.Fprintf(os.Stderr, " [v%s]", result.ThreadData.Version)
		}
		fmt.Fprintln(os.Stderr)
	}

	// Phase 2: show skip / results.
	if result.CooldownSkipped {
		fmt.Fprintf(os.Stderr, "  ✓ Skipped (checked within 24h; use --force to override)\n")
		return
	}

	if result.ThreadData == nil {
		return
	}

	// Engine mismatch warning for Phase 2 (Phase 1 is handled inside SyncGameLogic).
	if result.EngineMismatch && game.F95URL != "" {
		detEngine := engine.Detect(game.Path)
		title := result.ThreadData.Title
		if title == "" {
			title = game.Title
		}
		fmt.Fprintf(os.Stderr, "  ⚠ Engine mismatch (scanner: %s, thread: %q, tags: %s)\n",
			detEngine.Engine, util.Truncate(title, 60), FormatTagsBrief(result.ThreadData.Tags, 4))
	}

	if result.VersionUpdated {
		fmt.Fprintf(os.Stderr, "  🔄 Update available: %s → %s\n", result.OldVersion, result.NewVersion)
	} else if result.OldVersion != "" {
		fmt.Fprintf(os.Stderr, "  ✓ Up to date: %s\n", result.NewVersion)
	} else if result.NewVersion != "" {
		fmt.Fprintf(os.Stderr, "  ? F95Zone has %s (local version unknown)\n", result.NewVersion)
	}

	// Also print scraped metadata if available.
	if result.ThreadData.Developer != "" {
		fmt.Fprintf(os.Stderr, "  Developer: %s\n", result.ThreadData.Developer)
	}
	if len(result.ThreadData.Tags) > 0 {
		fmt.Fprintf(os.Stderr, "  Tags: %s\n", strings.Join(result.ThreadData.Tags, ", "))
	}
}

// Sync performs a full library sync: associate games with F95Zone threads
// and check for version updates.
func Sync(args []string) {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	cookieStr := fs.String("cookie", "", "Cookie header")
	cookieFile := fs.String("cookie-file", "", "Cookie file")
	unsafe := fs.Bool("unsafe", false, "⚠ Skip rate limiting")
	force := fs.Bool("force", false, "Force re-check even if checked within 24h")
	fs.Parse(args)

	cookie := ResolveCookie(*cookieStr, *cookieFile)
	if cookie == "" {
		fmt.Fprintf(os.Stderr, "Cookie required. Log into f95zone.to in Firefox.\n")
		os.Exit(1)
	}

	// Single-game sync: moxie sync <game-id>
	if fs.NArg() >= 1 {
		id := util.MustParseInt(fs.Arg(0))
		SyncGame(id, cookie, *unsafe, *force)
		return
	}

	database := OpenDB()
	defer database.Close()

	// Phase 1: Associate games with F95Zone threads.
	fmt.Fprintln(os.Stderr, "\n=== Phase 1/2: Associating games with F95Zone threads ===")
	RunScrapeAuto(database, cookie, *unsafe, *force)

	// Phase 2: Check for version updates.
	fmt.Fprintln(os.Stderr, "\n=== Phase 2/2: Checking for version updates ===")

	allGames, err := database.ListGames("", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}

	var trackable []db.Game
	for _, g := range allGames {
		if g.F95URL != "" {
			trackable = append(trackable, g)
		}
	}
	if len(trackable) == 0 {
		fmt.Fprintln(os.Stderr, "No games have F95Zone URLs. Nothing to check.")
		return
	}

	var client *scraper.Client
	if *unsafe {
		client = scraper.NewUnsafeClient(cookie)
	} else {
		client = scraper.NewClient(cookie)
	}

	updatesFound, _ := RunUpdateCheck(database, client, trackable, *force)
	fmt.Fprintf(os.Stderr, "\n=== %d updates available ===\n", updatesFound)
}

// RunScrapeAuto finds and associates F95Zone threads for unassociated games.
// When force is false, games that were recently searched without success
// (within UpdateCheckCooldown) are skipped to avoid redundant API calls.
func RunScrapeAuto(database *db.Database, cookie string, unsafe bool, force bool) {
	if cookie == "" {
		fmt.Fprintf(os.Stderr, "Cookie required for auto-association.\n")
		fmt.Fprintf(os.Stderr, "Log into f95zone.to in Firefox, or use --cookie/--cookie-file.\n")
		os.Exit(1)
	}

	if unsafe {
		fmt.Fprintln(os.Stderr, "⚠  --unsafe: rate limiting disabled. You may get IP-banned or Cloudflare-blocked.")
		fmt.Fprintln(os.Stderr)
	}

	allGames, err := database.ListGames("", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading games: %v\n", err)
		os.Exit(1)
	}

	// Filter to unassociated games (no F95Zone URL).
	// Skip games that were recently searched without success to avoid
	// redundant API calls.  Use --force to override.
	var queue []db.Game
	var cooldownSkipped int
	for _, g := range allGames {
		if g.F95URL == "" {
			if !force && !g.VersionCheckedAt.IsZero() && time.Since(g.VersionCheckedAt) < UpdateCheckCooldown {
				cooldownSkipped++
				continue
			}
			queue = append(queue, g)
		}
	}
	if len(queue) == 0 {
		if cooldownSkipped > 0 {
			fmt.Fprintf(os.Stderr, "All unassociated games were searched within the last 24h. Use --force to re-search.\n")
		} else {
			fmt.Println("All games already have F95Zone URLs. Nothing to associate.")
		}
		return
	}

	var client *scraper.Client
	if unsafe {
		client = scraper.NewUnsafeClient(cookie)
	} else {
		client = scraper.NewClient(cookie)
	}
	total := len(queue)
	associated := 0
	skipped := 0
	interrupted := false
	startTime := time.Now()

	log.Info("auto-association started",
		"total", total,
		"unsafe", unsafe,
	)

	// Estimate: each game = 1 search + maybe 1 thread read
	estSeconds := total * 8
	if unsafe {
		estSeconds = total * 2 // much faster without delays
	}
	estDuration := time.Duration(estSeconds) * time.Second
	fmt.Fprintf(os.Stderr, "\n=== Auto-Associating %d games ===\n", total)
	if unsafe {
		fmt.Fprintf(os.Stderr, "Estimated time: ~%s (unsafe mode — no rate limiting).\n", util.FormatDuration(estDuration))
	} else {
		fmt.Fprintf(os.Stderr, "Estimated time: ~%s at current rate limits.\n", util.FormatDuration(estDuration))
	}
	fmt.Fprintf(os.Stderr, "This is a background task — let it run. It'll pause occasionally to avoid rate limits.\n\n")

	searchCache := make(map[string][]scraper.SearchResult) // sanitized_title -> search results
	urlCache := make(map[string]string)                    // sanitized_title -> thread URL
	processed := make(map[string]bool)                     // sanitized_title -> fully processed (skip duplicates)

	for i, game := range queue {
		elapsed := time.Since(startTime).Truncate(time.Second)

		// Deduplicate: skip games whose sanitized title was already processed.
		query := scraper.SanitizeTitle(game.Title)
		if query == "" {
			query = game.Title
		}
		if processed[query] {
			log.Debug("skipping duplicate", "title", game.Title, "index", i+1, "total", total)
			fmt.Fprintf(os.Stderr, "[%d/%d] %s %q — skipping (duplicate)\n",
				i+1, total, elapsed, game.Title)
			skipped++
			continue
		}

		log.Debug("processing game for association", "title", game.Title, "index", i+1, "total", total)

		fmt.Fprintf(os.Stderr, "[%d/%d] %s %q",
			i+1, total, elapsed, game.Title)
		if query != game.Title {
			fmt.Fprintf(os.Stderr, "  (search: %q)", query)
		}
		fmt.Fprintln(os.Stderr)

		// Use caches to avoid redundant searches.
		var results []scraper.SearchResult
		if cachedURL, ok := urlCache[query]; ok {
			results = []scraper.SearchResult{{Title: game.Title, URL: cachedURL}}
		} else {
			var cached bool
			results, cached = searchCache[query]
			if !cached {
				var err error
				results, err = client.SearchF95Zone(query)
				if err != nil {
					if util.IsBlocked(err) {
						log.Error("blocked during auto-association", "error", err)
						fmt.Fprintf(os.Stderr, "  ⚠ BLOCKED: stopping auto-association\n    %v\n", err)
						fmt.Fprintf(os.Stderr, "  Try refreshing your F95Zone session in Firefox and running again.\n")
						interrupted = true
						break
					}
					fmt.Fprintf(os.Stderr, "  ✗ Search failed: %v\n\n", err)
					processed[query] = true
					skipped++
					continue
				}
				searchCache[query] = results
			}
		}

		if len(results) == 0 {
			fmt.Fprintln(os.Stderr, "  ✗ No search results")
			processed[query] = true
			game.VersionCheckedAt = time.Now()
			if err := database.UpdateGame(&game); err != nil {
				fmt.Fprintf(os.Stderr, "  ⚠ Failed to update cooldown for %q: %v\n", game.Title, err)
			}
			skipped++
			continue
		}

		// Pre-detect engine for score boosting (prefer engine-tagged
		// release threads over bare-title request threads).
		detEngine := engine.Detect(game.Path)
		engVariants, hasEngVariants := EngineTagVariants[string(detEngine.Engine)]

		// Show results with scores.  Skip non-game threads (requests,
		// recommendations, identification threads, etc.) when choosing
		// the best match, but still display them for context.
		var best *scraper.SearchResult
		var bestScore float64
		for j, r := range results {
			score := scraper.ComputeMatchScore(game.Title, r.Title)
			// Boost engine-matching candidates (see SyncGameLogic for rationale).
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
			isNonGame := scraper.IsNonGameThread(r.Title)
			marker := "  "
			if !isNonGame && score > bestScore {
				bestScore = score
				best = &results[j]
				marker = "→ "
			}
			skipLabel := ""
			if isNonGame {
				skipLabel = " [non-game]"
			}
			fmt.Fprintf(os.Stderr, "  %s[%.0f%%] %s%s\n", marker, score*100,
				util.Truncate(r.Title, 55), skipLabel)
		}

		if best == nil || bestScore < 0.3 {
			fmt.Fprintf(os.Stderr, "  ✗ No good match (best: %.0f%%)\n\n", bestScore*100)
			processed[query] = true
			game.VersionCheckedAt = time.Now()
			if err := database.UpdateGame(&game); err != nil {
				fmt.Fprintf(os.Stderr, "  ⚠ Failed to update cooldown for %q: %v\n", game.Title, err)
			}
			skipped++
			continue
		}

		// Scrape the best match.
		fmt.Fprintf(os.Stderr, "  ⬇ Scraping %s...\n", best.URL)
		data, err := client.ScrapeThread(best.URL)
		if err != nil {
			if util.IsBlocked(err) {
				fmt.Fprintf(os.Stderr, "  ⚠ BLOCKED: stopping auto-association\n    %v\n", err)
				fmt.Fprintf(os.Stderr, "  Try refreshing your F95Zone session in Firefox.\n")
				break
			}
			fmt.Fprintf(os.Stderr, "  ✗ Scrape failed: %v\n\n", err)
			processed[query] = true
			skipped++
			continue
		}

		// Check engine consistency BEFORE saving. Uses the pre-detected
		// engine from score boosting (see above).
		if !EngineMatchesThread(detEngine, data.Tags, best.Title) {
			fmt.Fprintf(os.Stderr, "  ⚠ Engine mismatch (scanner: %s, thread: %q, tags: %s) — skipping\n",
				detEngine.Engine, util.Truncate(best.Title, 60), FormatTagsBrief(data.Tags, 4))
			processed[query] = true
			skipped++
			continue
		}

		ApplyThreadData(&game, data, best.URL)
		game.VersionCheckedAt = time.Now()

		if err := database.UpdateGame(&game); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ Save failed: %v\n\n", err)
			processed[query] = true
			skipped++
			continue
		}

		// Cache the successful association so future runs return instantly.
		urlCache[query] = best.URL

		log.Info("game associated",
			"title", game.Title,
			"version", data.Version,
		)
		fmt.Fprintf(os.Stderr, "  ✓ Saved (%s)", game.Title)
		if data.Version != "" {
			fmt.Fprintf(os.Stderr, " v%s", data.Version)
		}
		if data.Developer != "" {
			fmt.Fprintf(os.Stderr, " • %s", data.Developer)
		}
		fmt.Fprintln(os.Stderr)

		// Save scraped metadata.
		if data.Developer != "" || data.Overview != "" || data.CoverURL != "" {
			meta := &db.ScrapedMeta{
				GameID:    game.ID,
				Developer: data.Developer,
				Overview:  data.Overview,
				CoverURL:  data.CoverURL,
			}
			if err := database.UpsertScrapedMeta(meta); err != nil {
				fmt.Fprintf(os.Stderr, "  ⚠ Failed to save metadata for %q: %v\n", game.Title, err)
			}
		}

		associated++
		processed[query] = true
		fmt.Fprintln(os.Stderr)
	}

	elapsed := time.Since(startTime).Truncate(time.Second)

	if interrupted {
		log.Warn("auto-association interrupted by block", "associated", associated, "skipped", skipped)
		fmt.Fprintf(os.Stderr, "=== INTERRUPTED (blocked by F95Zone) ===\n")
	} else {
		log.Info("auto-association complete",
			"associated", associated,
			"skipped", skipped,
			"total", total,
			"elapsed", elapsed.String(),
		)
	}
	fmt.Fprintf(os.Stderr, "=== Done: %d associated, %d skipped, %d/%d total in %s ===\n",
		associated, skipped, associated+skipped, total, elapsed)
}
