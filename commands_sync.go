package main

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
	"github.com/mili/moxie/internal/scraper"
)

// ---------------------------------------------------------------------------
// check-updates command
// ---------------------------------------------------------------------------

// checkUpdateResult holds the outcome of checking one game for updates.
type checkUpdateResult struct {
	Game    db.Game `json:"game"`
	Current string  `json:"current"`
	Latest  string  `json:"latest"`
	IsNew   bool    `json:"is_new"`
	Error   string  `json:"error,omitempty"`
}

const updateCheckCooldown = 24 * time.Hour

// runUpdateCheck scrapes each game's F95Zone thread, compares versions, and
// updates the database. It returns the count of games with new versions and
// a result for each game processed.
func runUpdateCheck(database *db.Database, client *scraper.Client, games []db.Game, force bool) (int, []checkUpdateResult) {
	// Skip games checked within the last 24 hours (unless --force).
	cooldownSkipped := 0
	var filtered []db.Game
	for _, g := range games {
		if g.F95URL == "" {
			continue
		}
		if !force && !g.VersionCheckedAt.IsZero() && time.Since(g.VersionCheckedAt) < updateCheckCooldown {
			cooldownSkipped++
			continue
		}
		filtered = append(filtered, g)
	}
	games = filtered

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

	var results []checkUpdateResult
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
				results = append(results, checkUpdateResult{Game: g, Error: err.Error()})
				mu.Unlock()
				return
			}
			latest := data.Version
			knownVer := g.Version
			if knownVer == "" {
				knownVer = g.LatestVersion
			}
			isNew := latest != "" && knownVer != "" && normalizeVersion(latest) != normalizeVersion(knownVer)

			// Signal: check for engine mismatch between scanner and F95Zone tags.
			var engineWarn string
			if len(data.Tags) > 0 {
				detEngine := engine.Detect(g.Path)
				if !engineMatchesTags(detEngine, data.Tags) {
					engineWarn = fmt.Sprintf(" ⚠ engine mismatch (scanner: %s)",
						detEngine.Engine)
				}
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
				_ = database.UpsertScrapedMeta(&db.ScrapedMeta{
					GameID:    g.ID,
					Developer: data.Developer,
					Overview:  data.Overview,
					CoverURL:  data.CoverURL,
				})
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
			} else {
				fmt.Fprintf(os.Stderr, "  %q ? no version detected%s\n", g.Title, engineWarn)
			}
			results = append(results, checkUpdateResult{Game: g, Current: knownVer, Latest: latest, IsNew: isNew})
			mu.Unlock()
		}(g)
	}
	wg.Wait()

	return updatesFound, results
}

// normalizeVersion strips trailing .0 segments and leading v/V prefix for comparison.
func normalizeVersion(v string) string {
	v = strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	v = strings.TrimSpace(v)
	for strings.HasSuffix(v, ".0") {
		v = strings.TrimSuffix(v, ".0")
	}
	return v
}

func cmdCheckUpdates(args []string) {
	fs := flag.NewFlagSet("check-updates", flag.ExitOnError)
	cookieStr := fs.String("cookie", "", "Cookie header")
	cookieFile := fs.String("cookie-file", "", "Cookie file")
	jsonOut := fs.Bool("json", false, "JSON output")
	unsafe := fs.Bool("unsafe", false, "⚠ Skip rate limiting")
	force := fs.Bool("force", false, "Force re-check even if checked within 24h")
	fs.Parse(args)

	cookie := resolveCookie(*cookieStr, *cookieFile)
	if cookie == "" {
		fmt.Fprintf(os.Stderr, "Cookie required. Log into f95zone.to in Firefox.\n")
		os.Exit(1)
	}

	database := openDB()
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

	updatesFound, results := runUpdateCheck(database, client, trackable, *force)

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

// ---------------------------------------------------------------------------
// sync command — full library sync (associate + check updates)
// ---------------------------------------------------------------------------

// cmdSyncGame syncs a single game: associate it with F95Zone (if needed)
// and check for version updates.
func cmdSyncGame(id int64, cookie string, unsafe bool, force bool) {
	database := openDB()
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

	// Phase 1: Associate if needed.
	if game.F95URL == "" {
		fmt.Fprintf(os.Stderr, "  Searching F95Zone for %q...\n", game.Title)
		query := scraper.SanitizeTitle(game.Title)
		if query == "" {
			query = game.Title
		}
		results, err := client.SearchF95Zone(query)
		if err != nil {
			if isBlocked(err) {
				fmt.Fprintf(os.Stderr, "  ⚠ BLOCKED: %v\n", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "  ✗ Search failed: %v\n", err)
			os.Exit(1)
		}
		if len(results) == 0 {
			fmt.Fprintf(os.Stderr, "  ✗ No F95Zone results found for %q.\n", game.Title)
			os.Exit(1)
		}

		// Show top results and pick best.
		var best *scraper.SearchResult
		var bestScore float64
		for i, r := range results {
			score := scraper.ComputeMatchScore(game.Title, r.Title)
			marker := "  "
			if score > bestScore {
				bestScore = score
				best = &results[i]
				marker = "→ "
			}
			fmt.Fprintf(os.Stderr, "  %s[%.0f%%] %s\n", marker, score*100, r.Title)
		}

		if best == nil || bestScore < 0.3 {
			fmt.Fprintf(os.Stderr, "  ✗ No good match found.\n")
			os.Exit(1)
		}

		fmt.Fprintf(os.Stderr, "  Scraping %s...\n", best.URL)
		data, err := client.ScrapeThread(best.URL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ Scrape failed: %v\n", err)
			os.Exit(1)
		}

		// Signal: check engine consistency before associating.
		if len(data.Tags) > 0 {
			detEngine := engine.Detect(game.Path)
			if !engineMatchesTags(detEngine, data.Tags) {
				fmt.Fprintf(os.Stderr, "  ⚠ Engine mismatch (scanner: %s, thread tags: %s)\n",
					detEngine.Engine, formatTagsBrief(data.Tags, 4))
				fmt.Fprintf(os.Stderr, "  Associate anyway? [y/N]: ")
				var answer string
				fmt.Scanln(&answer)
				if strings.ToLower(answer) != "y" {
					fmt.Fprintln(os.Stderr, "  Cancelled.")
					os.Exit(1)
				}
			}
		}

		applyThreadData(game, data, best.URL)
		game.VersionCheckedAt = time.Now() // prevent double-scrape in Phase 2
		if err := database.UpdateGame(game); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ Save failed: %v\n", err)
			os.Exit(1)
		}
		if data.Developer != "" || data.Overview != "" || data.CoverURL != "" {
			meta := &db.ScrapedMeta{
				GameID:    id,
				Developer: data.Developer,
				Overview:  data.Overview,
				CoverURL:  data.CoverURL,
			}
			_ = database.UpsertScrapedMeta(meta)
		}

		fmt.Fprintf(os.Stderr, "  ✓ Associated: %s", data.Title)
		if data.Version != "" {
			fmt.Fprintf(os.Stderr, " [v%s]", data.Version)
		}
		fmt.Fprintln(os.Stderr)
	}

	// Phase 2: Check for updates (skip if recently checked, unless --force).
	if !force && !game.VersionCheckedAt.IsZero() && time.Since(game.VersionCheckedAt) < updateCheckCooldown {
		fmt.Fprintf(os.Stderr, "  ✓ Skipped (checked within 24h; use --force to override)\n")
		return
	}
	fmt.Fprintf(os.Stderr, "  Checking for updates...\n")
	data, err := client.ScrapeThread(game.F95URL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ✗ %v\n", err)
		os.Exit(1)
	}

	// Signal: check engine consistency with freshly scraped tags.
	if len(data.Tags) > 0 {
		detEngine := engine.Detect(game.Path)
		if !engineMatchesTags(detEngine, data.Tags) {
			fmt.Fprintf(os.Stderr, "  ⚠ Engine mismatch (scanner: %s, thread tags: %s)\n",
				detEngine.Engine, formatTagsBrief(data.Tags, 4))
		}
	}

	latest := data.Version
	knownVer := game.Version

	game.LatestVersion = latest
	game.VersionCheckedAt = time.Now()
	if err := database.UpdateGame(game); err != nil {
		fmt.Fprintf(os.Stderr, "  ⚠ Failed to save version data: %v\n", err)
	}

	// Save scraped metadata (cover, developer, overview) from the scrape.
	if data.Developer != "" || data.Overview != "" || data.CoverURL != "" {
		_ = database.UpsertScrapedMeta(&db.ScrapedMeta{
			GameID:    id,
			Developer: data.Developer,
			Overview:  data.Overview,
			CoverURL:  data.CoverURL,
		})
	}

	if latest != "" && knownVer != "" && latest != knownVer {
		fmt.Fprintf(os.Stderr, "  🔄 Update available: %s → %s\n", knownVer, latest)
	} else if knownVer != "" {
		fmt.Fprintf(os.Stderr, "  ✓ Up to date: %s\n", latest)
	} else if latest != "" {
		fmt.Fprintf(os.Stderr, "  ? F95Zone has %s (local version unknown)\n", latest)
	} else {
		fmt.Fprintf(os.Stderr, "  ? No version detected\n")
	}

	// Also print scraped metadata if available.
	if data.Developer != "" {
		fmt.Fprintf(os.Stderr, "  Developer: %s\n", data.Developer)
	}
	if len(data.Tags) > 0 {
		fmt.Fprintf(os.Stderr, "  Tags: %s\n", strings.Join(data.Tags, ", "))
	}
}

func cmdSync(args []string) {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	cookieStr := fs.String("cookie", "", "Cookie header")
	cookieFile := fs.String("cookie-file", "", "Cookie file")
	unsafe := fs.Bool("unsafe", false, "⚠ Skip rate limiting")
	force := fs.Bool("force", false, "Force re-check even if checked within 24h")
	fs.Parse(args)

	cookie := resolveCookie(*cookieStr, *cookieFile)
	if cookie == "" {
		fmt.Fprintf(os.Stderr, "Cookie required. Log into f95zone.to in Firefox.\n")
		os.Exit(1)
	}

	// Single-game sync: moxie sync <game-id>
	if fs.NArg() >= 1 {
		id := mustParseInt(fs.Arg(0))
		cmdSyncGame(id, cookie, *unsafe, *force)
		return
	}

	database := openDB()
	defer database.Close()

	// Phase 1: Associate games with F95Zone threads.
	fmt.Fprintln(os.Stderr, "\n=== Phase 1/2: Associating games with F95Zone threads ===")
	runScrapeAuto(database, cookie, *unsafe)

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

	updatesFound, _ := runUpdateCheck(database, client, trackable, *force)
	fmt.Fprintf(os.Stderr, "\n=== %d updates available ===\n", updatesFound)
}

// runScrapeAuto finds and associates F95Zone threads for unassociated games.
func runScrapeAuto(database *db.Database, cookie string, unsafe bool) {
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
	var queue []db.Game
	for _, g := range allGames {
		if g.F95URL == "" {
			queue = append(queue, g)
		}
	}
	if len(queue) == 0 {
		fmt.Println("All games already have F95Zone URLs. Nothing to associate.")
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

	// Estimate: each game = 1 search + maybe 1 thread read
	estSeconds := total * 8
	if unsafe {
		estSeconds = total * 2 // much faster without delays
	}
	estDuration := time.Duration(estSeconds) * time.Second
	fmt.Fprintf(os.Stderr, "\n=== Auto-Associating %d games ===\n", total)
	if unsafe {
		fmt.Fprintf(os.Stderr, "Estimated time: ~%s (unsafe mode — no rate limiting).\n", formatDuration(estDuration))
	} else {
		fmt.Fprintf(os.Stderr, "Estimated time: ~%s at current rate limits.\n", formatDuration(estDuration))
	}
	fmt.Fprintf(os.Stderr, "This is a background task — let it run. It'll pause occasionally to avoid rate limits.\n\n")

	searchCache := make(map[string][]scraper.SearchResult) // sanitized_title -> search results
	urlCache := make(map[string]string)                    // sanitized_title -> thread URL

	for i, game := range queue {
		elapsed := time.Since(startTime).Truncate(time.Second)
		fmt.Fprintf(os.Stderr, "[%d/%d] %s %q",
			i+1, total, elapsed, game.Title)

		// Search (with caching).
		query := scraper.SanitizeTitle(game.Title)
		if query == "" {
			query = game.Title
		}
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
					if isBlocked(err) {
						fmt.Fprintf(os.Stderr, "  ⚠ BLOCKED: stopping auto-association\n    %v\n", err)
						fmt.Fprintf(os.Stderr, "  Try refreshing your F95Zone session in Firefox and running again.\n")
						interrupted = true
						break
					}
					fmt.Fprintf(os.Stderr, "  ✗ Search failed: %v\n\n", err)
					skipped++
					continue
				}
				searchCache[query] = results
			}
		}

		if len(results) == 0 {
			fmt.Fprintln(os.Stderr, "  ✗ No search results")
			skipped++
			continue
		}

		// Show results with scores.
		var best *scraper.SearchResult
		var bestScore float64
		for j, r := range results {
			score := scraper.ComputeMatchScore(game.Title, r.Title)
			marker := "  "
			if score > bestScore {
				bestScore = score
				best = &results[j]
				marker = "→ "
			}
			fmt.Fprintf(os.Stderr, "  %s[%.0f%%] %s\n", marker, score*100, truncate(r.Title, 55))
		}

		if best == nil || bestScore < 0.3 {
			fmt.Fprintf(os.Stderr, "  ✗ No good match (best: %.0f%%)\n\n", bestScore*100)
			skipped++
			continue
		}

		// Scrape the best match.
		fmt.Fprintf(os.Stderr, "  ⬇ Scraping %s...\n", best.URL)
		data, err := client.ScrapeThread(best.URL)
		if err != nil {
			if isBlocked(err) {
				fmt.Fprintf(os.Stderr, "  ⚠ BLOCKED: stopping auto-association\n    %v\n", err)
				fmt.Fprintf(os.Stderr, "  Try refreshing your F95Zone session in Firefox.\n")
				break
			}
			fmt.Fprintf(os.Stderr, "  ✗ Scrape failed: %v\n\n", err)
			skipped++
			continue
		}

		// Signal: prevent bad associations by checking engine consistency
		// BEFORE saving.  If the scanner-detected engine conflicts with
		// the F95Zone thread tags, skip this candidate — it's probably
		// the wrong thread.
		if len(data.Tags) > 0 {
			detEngine := engine.Detect(game.Path)
			if !engineMatchesTags(detEngine, data.Tags) {
				fmt.Fprintf(os.Stderr, "  ⚠ Engine mismatch (scanner: %s, thread tags: %s) — skipping\n",
					detEngine.Engine, formatTagsBrief(data.Tags, 4))
				skipped++
				continue
			}
		}

		applyThreadData(&game, data, best.URL)
		game.VersionCheckedAt = time.Now()

		if err := database.UpdateGame(&game); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ Save failed: %v\n\n", err)
			skipped++
			continue
		}

		// Cache the successful association so future runs return instantly.
		urlCache[query] = best.URL

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
			_ = database.UpsertScrapedMeta(meta)
		}

		associated++
		fmt.Fprintln(os.Stderr)
	}

	if interrupted {
		fmt.Fprintf(os.Stderr, "=== INTERRUPTED (blocked by F95Zone) ===\n")
	}
	elapsed := time.Since(startTime).Truncate(time.Second)
	fmt.Fprintf(os.Stderr, "=== Done: %d associated, %d skipped, %d/%d total in %s ===\n",
		associated, skipped, associated+skipped, total, elapsed)
}
