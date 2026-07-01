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
)

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

			// Use slug-agnostic URL from thread ID when available so version
			// changes in the URL slug don't break future scrapes.
			scrapeURL := scraper.ResolveScrapeURL(g.F95URL, g.F95ThreadID)
			data, err := client.ScrapeThread(scrapeURL)
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
			if !engine.EngineMatchesThread(detEngine, data.Tags, data.Title) {
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
