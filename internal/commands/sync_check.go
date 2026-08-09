package commands

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/engine"
	"github.com/mili/moxie/internal/log"
	"github.com/mili/moxie/internal/scraper"
	"github.com/mili/moxie/internal/util"
	"github.com/mili/moxie/internal/version"
)

// RunUpdateCheck checks each game's F95Zone thread for version updates and
// updates the database. It returns the count of games with new versions and
// a result for each game processed.
//
// When public is non-nil, games with known thread IDs are checked in bulk
// through F95Zone's cookie-free checker.php endpoint (one request for the
// whole library), with status/tags refreshed through the F95Checker cache
// API. Games without a thread ID, and any games the bulk endpoint doesn't
// track, fall back to direct thread scraping via client. When public is nil,
// every game is scraped directly.
func RunUpdateCheck(database *db.Database, client *scraper.Client, games []db.Game, force bool, public *scraper.PublicAPI) (int, []UpdateResult) {
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

	// Bulk pre-pass: cookie-free version check for games with thread IDs.
	// Games it doesn't cover are handled by the direct-scrape worker pool.
	var bulkResults []UpdateResult
	bulkUpdates := 0
	var bulkGames []bulkGameEntry
	var directGames []db.Game
	if public != nil {
		directGames, bulkResults, bulkUpdates, bulkGames = runBulkVersionCheck(database, games, public)
	} else {
		directGames = games
	}

	total := len(games)
	startTime := time.Now()

	// Estimate: ~5 seconds per game with rate limiting, less with concurrent workers.
	estSeconds := len(directGames) * 5 / 3 // 3 concurrent workers
	estDuration := time.Duration(estSeconds) * time.Second
	if len(directGames) < len(games) {
		fmt.Fprintf(os.Stderr, "  Checking %d games via bulk version API + %d directly (up to 3 concurrent) — est. %s\n\n",
			len(games)-len(directGames), len(directGames), util.FormatDuration(estDuration))
	} else {
		fmt.Fprintf(os.Stderr, "  Checking %d games (up to 3 concurrent) — est. %s\n\n", total, util.FormatDuration(estDuration))
	}

	var directResults []UpdateResult
	directUpdates := 0
	if len(directGames) > 0 {
		directResults, directUpdates = runDirectUpdateCheck(database, client, directGames, startTime)
	}

	// Refresh status/tags for bulk-checked games via the cache API. Best
	// effort — failures only degrade metadata freshness, not version data.
	if len(bulkGames) > 0 && public != nil {
		refreshStatusViaCache(database, bulkGames, public)
	}

	// Merge results in original game order for stable output.
	results := make([]UpdateResult, 0, len(bulkResults)+len(directResults))
	results = append(results, bulkResults...)
	results = append(results, directResults...)
	updatesFound := bulkUpdates + directUpdates

	elapsed := time.Since(startTime).Truncate(time.Second)

	log.Info("update check complete",
		"updates", updatesFound,
		"total", total+cooldownSkipped,
		"elapsed", elapsed.String(),
		"bulk", len(bulkResults),
		"direct", len(directResults),
	)

	fmt.Fprintf(os.Stderr, "\n  Done: %d checked, %d updates found in %s\n", total, updatesFound, elapsed)

	return updatesFound, results
}

// bulkGameEntry tracks a game handled by the bulk version API plus the
// timestamp of its previous check (needed to decide cache refreshes).
type bulkGameEntry struct {
	game        db.Game
	prevChecked time.Time
}

// runBulkVersionCheck performs the cookie-free bulk version pass. It returns
// the games that still need direct scraping, the results produced, and the
// per-game records for the status-refresh pass.
func runBulkVersionCheck(database *db.Database, games []db.Game, public *scraper.PublicAPI) ([]db.Game, []UpdateResult, int, []bulkGameEntry) {
	ctx := context.Background()

	var ids []int64
	byID := make(map[int64][]int, len(games))
	for i, g := range games {
		if g.F95ThreadID > 0 {
			if _, dup := byID[g.F95ThreadID]; !dup {
				ids = append(ids, g.F95ThreadID)
			}
			byID[g.F95ThreadID] = append(byID[g.F95ThreadID], i)
		}
	}

	versions, err := public.BulkVersions(ctx, ids)
	if err != nil {
		log.Warn("bulk version API unavailable; falling back to direct scraping",
			"error", err,
			"games", len(games),
		)
		fmt.Fprintf(os.Stderr, "  ⚠ Bulk version API unavailable (%v) — falling back to direct scraping\n", err)
		return games, nil, 0, nil
	}

	handled := make(map[int64]bool, len(games))
	var results []UpdateResult
	var entries []bulkGameEntry
	updatesFound := 0
	index := 0

	// Pass 1: threads tracked by checker.php.
	for _, g := range games {
		if g.F95ThreadID <= 0 {
			continue
		}
		latest, tracked := versions[g.F95ThreadID]
		if !tracked {
			continue // pass 2 handles untracked threads via the cache API
		}
		index++
		handled[g.ID] = true
		res, entry, isNew := processBulkGame(database, g, latest)
		if isNew {
			updatesFound++
		}
		entries = append(entries, entry)
		results = append(results, res)
		printBulkResult(index, len(games), g.Title, res.Current, res.Latest, res.Diff)
	}

	// Pass 2: threads checker.php doesn't track — ask the F95Checker cache
	// API instead (it covers non-indexed threads, still cookie-free). Only
	// games whose thread ID the cache API doesn't know fall back to direct
	// scraping.
	for _, g := range games {
		if handled[g.ID] || g.F95ThreadID <= 0 {
			continue
		}
		ct, err := public.CacheFullThread(ctx, g.F95ThreadID)
		if err != nil {
			if !errors.Is(err, scraper.ErrThreadNotFound) {
				log.Debug("cache API has no data for untracked thread; direct scrape will cover it",
					"thread", g.F95ThreadID, "error", err)
			} else {
				log.Debug("cache API reports thread missing", "thread", g.F95ThreadID, "title", g.Title)
			}
			continue
		}
		index++
		handled[g.ID] = true
		res, entry, isNew := processBulkGame(database, g, ct.Version)
		// Persist the richer cache data (status, developer, cover).
		g2 := res.Game
		if ct.Status != "" && ct.Status != g2.Status {
			g2.Status = ct.Status
			if err := database.UpdateGame(&g2); err != nil {
				fmt.Fprintf(os.Stderr, "  ⚠ Failed to save cache data for %q: %v\n", g.Title, err)
			}
		}
		if ct.Developer != "" || ct.Description != "" || ct.ImageURL != "" {
			if err := database.UpsertScrapedMeta(&db.ScrapedMeta{
				GameID:    g.ID,
				Developer: ct.Developer,
				Overview:  ct.Description,
				CoverURL:  ct.ImageURL,
			}); err != nil {
				fmt.Fprintf(os.Stderr, "  ⚠ Failed to save metadata for %q: %v\n", g.Title, err)
			}
		}
		if isNew {
			updatesFound++
		}
		entries = append(entries, entry)
		results = append(results, res)
		printBulkResult(index, len(games), g.Title, res.Current, res.Latest, res.Diff)
	}

	var remaining []db.Game
	for _, g := range games {
		if !handled[g.ID] {
			remaining = append(remaining, g)
		}
	}
	return remaining, results, updatesFound, entries
}

// processBulkGame compares latest against the game's known version, persists
// the result, and returns the outcome for reporting. latest must already be
// qualifier-stripped.
func processBulkGame(database *db.Database, g db.Game, latest string) (UpdateResult, bulkGameEntry, bool) {
	knownVer := g.Version
	if knownVer == "" {
		knownVer = g.LatestVersion
	}
	// Stored versions from older runs may carry qualifiers
	// ("v1.03 + DLC") — strip both sides so they compare clean.
	knownVer = scraper.StripVersionQualifier(knownVer)

	// Only an unambiguously newer version counts as an update.
	diff := version.Same
	if latest != "" && knownVer != "" {
		diff = version.Compare(latest, knownVer)
	}
	isNew := diff == version.Newer || diff == version.Changed

	entry := bulkGameEntry{game: g, prevChecked: g.VersionCheckedAt}
	// Never wipe a stored version with an empty bulk value ("Unknown").
	if latest != "" {
		g.LatestVersion = latest
	}
	g.VersionCheckedAt = time.Now()
	if err := database.UpdateGame(&g); err != nil {
		fmt.Fprintf(os.Stderr, "  ⚠ Failed to save version data for %q: %v\n", g.Title, err)
	}

	return UpdateResult{
		Game:    g,
		Current: knownVer,
		Latest:  latest,
		IsNew:   isNew,
		Diff:    diff.String(),
	}, entry, isNew
}

// printBulkResult renders one bulk-check line.
func printBulkResult(index, total int, title, knownVer, latest, diff string) {
	switch diff {
	case version.Newer.String():
		fmt.Fprintf(os.Stderr, "  [%d/%d] %q 🔄 %s → %s\n", index, total, title, knownVer, latest)
	case version.Changed.String():
		fmt.Fprintf(os.Stderr, "  [%d/%d] %q ≠ %s → %s (ordering unclear)\n", index, total, title, knownVer, latest)
	case version.Older.String():
		fmt.Fprintf(os.Stderr, "  [%d/%d] %q ⚠ thread version %s is older than local %s\n", index, total, title, latest, knownVer)
	case version.Same.String():
		if knownVer != "" {
			fmt.Fprintf(os.Stderr, "  [%d/%d] %q ✓ %s\n", index, total, title, latest)
		} else if latest != "" {
			fmt.Fprintf(os.Stderr, "  [%d/%d] %q ? %s (local version unknown)\n", index, total, title, latest)
		}
	default:
		fmt.Fprintf(os.Stderr, "  [%d/%d] %q ✓ %s\n", index, total, title, latest)
	}
}

// refreshStatusViaCache refreshes status/tags/metadata for bulk-checked games
// whose threads changed since the previous check, using the F95Checker cache
// API. Best effort: failures are logged, never fatal.
func refreshStatusViaCache(database *db.Database, entries []bulkGameEntry, public *scraper.PublicAPI) {
	ctx := context.Background()

	var ids []int64
	for _, e := range entries {
		ids = append(ids, e.game.F95ThreadID)
	}
	lastChanged, err := public.CacheFastCheck(ctx, ids)
	if err != nil {
		log.Debug("cache API fast check unavailable; skipping metadata refresh", "error", err)
		return
	}

	for _, e := range entries {
		g := e.game
		ts, ok := lastChanged[g.F95ThreadID]
		if !ok || ts <= e.prevChecked.Unix() {
			continue
		}
		ct, err := public.CacheFullThread(ctx, g.F95ThreadID)
		if err != nil {
			if !errors.Is(err, scraper.ErrThreadNotFound) {
				log.Debug("cache API full check failed", "thread", g.F95ThreadID, "error", err)
			}
			continue
		}

		var statusChange string
		if ct.Status != "" && ct.Status != g.Status {
			statusChange = fmt.Sprintf(" [%s → %s]", g.Status, ct.Status)
			g.Status = ct.Status
		}
		// An empty cache version must not wipe the stored latest version.
		if ct.Version != "" {
			g.LatestVersion = ct.Version
		}
		if err := database.UpdateGame(&g); err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠ Failed to save metadata for %q: %v\n", g.Title, err)
			continue
		}
		if statusChange != "" {
			fmt.Fprintf(os.Stderr, "  %q%s\n", g.Title, statusChange)
		}
		if ct.Developer != "" || ct.Description != "" || ct.ImageURL != "" {
			if err := database.UpsertScrapedMeta(&db.ScrapedMeta{
				GameID:    g.ID,
				Developer: ct.Developer,
				Overview:  ct.Description,
				CoverURL:  ct.ImageURL,
			}); err != nil {
				fmt.Fprintf(os.Stderr, "  ⚠ Failed to save metadata for %q: %v\n", g.Title, err)
			}
		}
	}
}

// runDirectUpdateCheck scrapes each game's thread directly (cookie path) and
// returns results. This is the fallback layer: it handles games the bulk
// version API doesn't track, and all games when the public API is unavailable.
func runDirectUpdateCheck(database *db.Database, client *scraper.Client, games []db.Game, startTime time.Time) ([]UpdateResult, int) {
	total := len(games)

	var results []UpdateResult
	updatesFound := 0

	if client == nil {
		for _, g := range games {
			errMsg := "no scraper client available"
			results = append(results, UpdateResult{Game: g, Error: errMsg})
			fmt.Fprintf(os.Stderr, "  [%d/%d] %q ✗ %s\n", len(results), total, g.Title, errMsg)
		}
		return results, 0
	}

	// Worker pool for concurrent scraping.
	sem := make(chan struct{}, 3) // max 3 concurrent
	var mu sync.Mutex             // protects results slice and updatesFound counter
	var saveMu sync.Mutex         // serializes SQLite writes (prevents BUSY errors)
	var wg sync.WaitGroup
	var completed int64

	for i, g := range games {
		index := i + 1
		sem <- struct{}{}
		wg.Add(1)
		go func(g db.Game, idx int) {
			defer wg.Done()
			defer func() { <-sem }()

			if client.Blocked() {
				mu.Lock()
				results = append(results, UpdateResult{Game: g, Error: "skipped: session blocked — refresh F95Zone cookies and retry"})
				mu.Unlock()
				return
			}

			elapsed := time.Since(startTime).Truncate(time.Second)

			// Use slug-agnostic URL from thread ID when available so version
			// changes in the URL slug don't break future scrapes.
			scrapeURL := scraper.ResolveScrapeURL(g.F95URL, g.F95ThreadID)
			data, err := client.ScrapeThread(scrapeURL)
			if err != nil {
				mu.Lock()
				fmt.Fprintf(os.Stderr, "  [%d/%d] %s %q ✗ %v\n", idx, total, elapsed, g.Title, err)
				results = append(results, UpdateResult{Game: g, Error: err.Error()})
				mu.Unlock()
				atomic.AddInt64(&completed, 1)
				return
			}
			latest := data.Version
			knownVer := g.Version
			if knownVer == "" {
				knownVer = g.LatestVersion
			}
			// Stored versions from older runs may carry qualifiers
			// ("v1.03 + DLC") — strip both sides so they compare clean,
			// matching the bulk path and the desktop sync.
			knownVer = scraper.StripVersionQualifier(knownVer)

			// Only an unambiguously newer version counts as an update.
			// Compare treats an equal-but-differently-formatted version as
			// Same, and a regression (older or unorderable) as not-newer, so
			// a bad parse no longer surfaces as a phantom update.
			diff := version.Same
			if latest != "" && knownVer != "" {
				diff = version.Compare(latest, knownVer)
			}
			isNew := diff == version.Newer || diff == version.Changed

			// Signal: check for engine mismatch between scanner and F95Zone metadata.
			var engineWarn string
			detEngine := engine.Detect(g.Path)
			if !engine.EngineMatchesThread(detEngine, data.Tags, data.Title) {
				engineWarn = fmt.Sprintf(" ⚠ engine mismatch (scanner: %s)",
					detEngine.Engine)
			}

			// Status and tags are scraped on every check; persist them so a
			// game going Completed/Abandoned is recorded here and not only
			// on the association path.
			var statusChange string
			if data.Status != "" && data.Status != g.Status {
				statusChange = fmt.Sprintf(" [%s → %s]", g.Status, data.Status)
				g.Status = data.Status
			}
			if len(data.Tags) > 0 {
				g.Tags = data.Tags
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

			// Compute ETA for progress display.
			done := atomic.AddInt64(&completed, 1)
			elapsed = time.Since(startTime).Truncate(time.Second)
			eta := ""
			if done >= 5 && done < int64(total) {
				avgPerItem := time.Since(startTime) / time.Duration(done)
				remaining := time.Duration(int64(total)-done) * avgPerItem
				eta = fmt.Sprintf(" (ETA: %s)", remaining.Truncate(time.Second))
			}

			mu.Lock()
			switch {
			case diff == version.Newer:
				updatesFound++
				fmt.Fprintf(os.Stderr, "  [%d/%d] %s%s %q 🔄 %s → %s%s%s\n", idx, total, elapsed, eta, g.Title, knownVer, latest, statusChange, engineWarn)
			case diff == version.Changed:
				// Differs but not orderable (e.g. a date-form version
				// replacing a numeric one) — surface it, flagged as unclear.
				updatesFound++
				fmt.Fprintf(os.Stderr, "  [%d/%d] %s%s %q ≠ %s → %s (ordering unclear)%s%s\n", idx, total, elapsed, eta, g.Title, knownVer, latest, statusChange, engineWarn)
			case diff == version.Older:
				// The thread reports an older version than we hold. Almost
				// always a parse problem or an edited thread, never an update.
				fmt.Fprintf(os.Stderr, "  [%d/%d] %s%s %q ⚠ thread version %s is older than local %s%s%s\n", idx, total, elapsed, eta, g.Title, latest, knownVer, statusChange, engineWarn)
			case knownVer != "":
				// Local version is known and matches F95Zone.
				fmt.Fprintf(os.Stderr, "  [%d/%d] %s%s %q ✓ %s%s%s\n", idx, total, elapsed, eta, g.Title, latest, statusChange, engineWarn)
			case latest != "":
				// F95Zone has a version but we don't know the local version.
				fmt.Fprintf(os.Stderr, "  [%d/%d] %s%s %q ? %s (local version unknown)%s%s\n", idx, total, elapsed, eta, g.Title, latest, statusChange, engineWarn)
			}
			results = append(results, UpdateResult{
				Game:    g,
				Current: knownVer,
				Latest:  latest,
				IsNew:   isNew,
				Diff:    diff.String(),
			})
			mu.Unlock()
		}(g, index)
	}
	wg.Wait()

	return results, updatesFound
}

// NormalizeVersion reduces a version string to its canonical comparable
// form. Prefer version.Compare for update decisions — equal normal forms
// mean "same version", but unequal ones do not mean "newer".
func NormalizeVersion(v string) string {
	return version.Normalize(v)
}

// CheckUpdates checks all games for version updates from F95Zone.
func CheckUpdates(args []string) {
	fs := flag.NewFlagSet("check-updates", flag.ExitOnError)
	cookieStr := fs.String("cookie", "", "Cookie header (only needed for the direct-scrape fallback)")
	cookieFile := fs.String("cookie-file", "", "Cookie file")
	jsonOut := fs.Bool("json", false, "JSON output")
	unsafe := fs.Bool("unsafe", false, "⚠ Skip rate limiting")
	force := fs.Bool("force", false, "Force re-check even if checked within 24h")
	fs.Parse(args)

	cookie := ResolveCookie(*cookieStr, *cookieFile)

	database := OpenDB()
	defer database.Close()

	trackable, err := database.GamesWithF95URL()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if len(trackable) == 0 {
		fmt.Println("No games have F95Zone URLs. Run 'moxie sync' first.")
		return
	}

	// Version checks run cookie-free through F95Zone's public endpoints;
	// the cookie client is only the direct-scrape fallback layer.
	var client *scraper.Client
	if *unsafe {
		client = scraper.NewUnsafeClient(cookie)
	} else {
		client = scraper.NewClient(cookie)
	}
	public := scraper.NewPublicAPIWithCookie(cookie)

	updatesFound, results := RunUpdateCheck(database, client, trackable, *force, public)

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
