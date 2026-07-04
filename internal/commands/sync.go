package commands

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
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

// SyncConfig holds the parameters for RunSync.
type SyncConfig struct {
	Cookie     string
	CookieFile string
	Unsafe     bool
	Force      bool
	Parallel   int
}

// RunSync performs a full library sync: associate games with F95Zone threads
// and check for version updates. This is the testable logic function — it
// returns errors instead of os.Exit.
func RunSync(database *db.Database, cfg SyncConfig) error {
	cookie := ResolveCookie(cfg.Cookie, cfg.CookieFile)
	if cookie == "" {
		return fmt.Errorf("cookie required. Log into f95zone.to in Firefox")
	}

	// Phase 1: Associate games with F95Zone threads.
	fmt.Fprintln(os.Stderr, "\n=== Phase 1/2: Associating games with F95Zone threads ===")
	var client *scraper.Client
	if cfg.Unsafe {
		client = scraper.NewUnsafeClient(cookie)
		fmt.Fprintln(os.Stderr, "⚠  --unsafe: rate limiting disabled. You may get IP-banned or Cloudflare-blocked.")
		fmt.Fprintln(os.Stderr)
	} else {
		client = scraper.NewClient(cookie)
	}
	if err := RunScrapeAuto(database, client, cfg.Force, cfg.Parallel); err != nil {
		return fmt.Errorf("auto-association: %w", err)
	}

	// Phase 2: Check for version updates.
	fmt.Fprintln(os.Stderr, "\n=== Phase 2/2: Checking for version updates ===")
	trackable, err := database.GamesWithF95URL()
	if err != nil {
		return fmt.Errorf("querying games with F95 URLs: %w", err)
	}
	if len(trackable) == 0 {
		fmt.Fprintln(os.Stderr, "No games have F95Zone URLs. Nothing to check.")
		return nil
	}

	updatesFound, _ := RunUpdateCheck(database, client, trackable, cfg.Force)
	fmt.Fprintf(os.Stderr, "\n=== %d updates available ===\n", updatesFound)
	return nil
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

	// Persist any new cache entries (e.g. from a successful association).
	scraper.SaveAssociationCache()

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
			detEngine.Engine, util.Truncate(title, 60), engine.FormatTagsBrief(result.ThreadData.Tags, 4))
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
	parallel := fs.Int("parallel", 3, "Number of concurrent scrapers (default 3)")
	fs.Parse(args)

	cookie := ResolveCookie(*cookieStr, *cookieFile)
	if cookie == "" {
		fmt.Fprintf(os.Stderr, "Cookie required. Log into f95zone.to in Firefox.\n")
		os.Exit(1)
	}

	database := OpenDB()
	defer database.Close()

	// Single-game sync: moxie sync <game-id>
	if fs.NArg() >= 1 {
		game := ResolveGame(database, fs.Arg(0))
		if game == nil {
			fmt.Fprintf(os.Stderr, "Cancelled.\n")
			os.Exit(1)
		}
		SyncGame(game.ID, cookie, *unsafe, *force)
		return
	}

	cfg := SyncConfig{
		Cookie:     *cookieStr,
		CookieFile: *cookieFile,
		Unsafe:     *unsafe,
		Force:      *force,
		Parallel:   *parallel,
	}
	if err := RunSync(database, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// RunScrapeAuto finds and associates F95Zone threads for unassociated games.
// When force is false, games that were recently searched without success
// (within UpdateCheckCooldown) are skipped to avoid redundant API calls.
// The workers parameter controls how many concurrent scrapers run
// (1 = sequential, the default for backward compatibility).
func RunScrapeAuto(database *db.Database, client *scraper.Client, force bool, workers int) error {
	unassociated, err := database.GamesWithoutF95URL()
	if err != nil {
		return fmt.Errorf("loading games: %w", err)
	}

	// Skip games that were recently searched without success to avoid
	// redundant API calls.  Use --force to override.
	var queue []db.Game
	var cooldownSkipped int
	for _, g := range unassociated {
		if !force && !g.VersionCheckedAt.IsZero() && time.Since(g.VersionCheckedAt) < UpdateCheckCooldown {
			cooldownSkipped++
			continue
		}
		queue = append(queue, g)
	}
	if len(queue) == 0 {
		if cooldownSkipped > 0 {
			fmt.Fprintf(os.Stderr, "All unassociated games were searched within the last 24h. Use --force to re-search.\n")
		} else {
			fmt.Println("All games already have F95Zone URLs. Nothing to associate.")
		}
		return nil
	}

	total := len(queue)
	startTime := time.Now()

	// Enforce positive parallelism (0 would deadlock).
	if workers < 1 {
		workers = 1
	}
	if workers > 10 {
		workers = 10
		fmt.Fprintf(os.Stderr, "  (capping parallel scrapers at 10)\n")
	}

	mode := "sequential"
	if workers > 1 {
		mode = fmt.Sprintf("parallel (%d workers)", workers)
	}

	log.Info("auto-association started",
		"total", total,
		"workers", workers,
	)

	// Estimate: each game = 1 search + 1 thread read (~6s average with new delays).
	estSeconds := total * 6
	if workers > 1 {
		estSeconds = total * 6 / workers // parallel throughput
	}
	estDuration := time.Duration(estSeconds) * time.Second
	fmt.Fprintf(os.Stderr, "\n=== Auto-Associating %d games (%s) ===\n", total, mode)
	fmt.Fprintf(os.Stderr, "Estimated time: ~%s at current rate limits.\n", util.FormatDuration(estDuration))
	fmt.Fprintf(os.Stderr, "This is a background task — let it run. It'll pause occasionally to avoid rate limits.\n\n")

	// Load persistent association cache (survives restarts).
	scraper.LoadAssociationCache()
	defer scraper.SaveAssociationCache() // persist any new entries

	// Track completed count for ETA estimation.
	var completedCount int64

	// Shared state between worker goroutines.
	type workResult struct {
		game        db.Game
		query       string
		associated  bool
		skipped     bool
		interrupted bool
		title       string // display title for output
		msg         string // one-line result message
	}

	// Channel of games to process.
	type workItem struct {
		game  db.Game
		query string
		index int
	}

	jobs := make(chan workItem, len(queue))
	resultCh := make(chan workResult, len(queue))

	// Build the job queue with dedup.
	var processedJobQueries = make(map[string]bool)
	for i, game := range queue {
		query := scraper.SanitizeTitle(game.Title)
		if query == "" {
			query = game.Title
		}
		if processedJobQueries[query] {
			log.Debug("skipping duplicate", "title", game.Title)
			continue
		}
		processedJobQueries[query] = true
		jobs <- workItem{game: game, query: query, index: i + 1}
	}
	close(jobs)

	// mutex for serialized SQLite writes (prevents SQLITE_BUSY)
	var saveMu sync.Mutex

	// Launch workers.
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for job := range jobs {
				game, query := job.game, job.query
				elapsed := time.Since(startTime).Truncate(time.Second)

				// Compute ETA if we have progress data.
				done := atomic.LoadInt64(&completedCount)
				eta := ""
				if done >= 5 && done < int64(total) {
					avgPerItem := time.Since(startTime) / time.Duration(done)
					remaining := time.Duration(int64(total)-done) * avgPerItem
					eta = fmt.Sprintf(" (ETA: %s)", remaining.Truncate(time.Second))
				}

				fmt.Fprintf(os.Stderr, "[%d/%d] %s%s %q", job.index, total, elapsed, eta, game.Title)
				if query != game.Title {
					fmt.Fprintf(os.Stderr, "  (search: %q)", query)
				}
				fmt.Fprintln(os.Stderr)

				// Check persistent cache first.
				var searchRes []scraper.SearchResult
				if cachedID := scraper.GetCachedThreadID(query); cachedID > 0 {
					cachedURL := scraper.ThreadURL(cachedID)
					searchRes = []scraper.SearchResult{{Title: game.Title, URL: cachedURL}}
					fmt.Fprintf(os.Stderr, "  (cached thread %d)\n", cachedID)
				} else {
					var err error
					searchRes, err = client.SearchF95Zone(query)
					if err != nil {
						if util.IsBlocked(err) {
							log.Error("blocked during auto-association", "error", err)
							resultCh <- workResult{
								game: game, query: query,
								interrupted: true,
								msg:         fmt.Sprintf("  ⚠ BLOCKED: %v\n  Try refreshing your F95Zone session.\n", err),
							}
							return // worker stops on block
						}
						resultCh <- workResult{
							game: game, query: query,
							skipped: true,
							msg:     fmt.Sprintf("  ✗ Search failed: %v\n\n", err),
						}
						atomic.AddInt64(&completedCount, 1)
						continue
					}
				}

				if len(searchRes) == 0 {
					saveMu.Lock()
					game.VersionCheckedAt = time.Now()
					if err := database.UpdateGame(&game); err != nil {
						fmt.Fprintf(os.Stderr, "  ⚠ Failed to update cooldown for %q: %v\n", game.Title, err)
					}
					saveMu.Unlock()
					resultCh <- workResult{
						game: game, query: query,
						skipped: true,
						msg:     "  ✗ No search results\n",
					}
					atomic.AddInt64(&completedCount, 1)
					continue
				}

				// Score and select best match.
				detEngine := engine.Detect(game.Path)
				engVariants, hasEngVariants := engine.EngineTagVariants[string(detEngine.Engine)]

				var best *scraper.SearchResult
				var bestScore float64
				for j, r := range searchRes {
					score := scraper.ComputeMatchScore(game.Title, r.Title)
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
						best = &searchRes[j]
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
					saveMu.Lock()
					game.VersionCheckedAt = time.Now()
					if err := database.UpdateGame(&game); err != nil {
						fmt.Fprintf(os.Stderr, "  ⚠ Failed to update cooldown for %q: %v\n", game.Title, err)
					}
					saveMu.Unlock()
					resultCh <- workResult{
						game: game, query: query,
						skipped: true,
						msg:     fmt.Sprintf("  ✗ No good match (best: %.0f%%)\n\n", bestScore*100),
					}
					atomic.AddInt64(&completedCount, 1)
					continue
				}

				// Scrape the best match.
				fmt.Fprintf(os.Stderr, "  ⬇ Scraping %s...\n", best.URL)
				data, err := client.ScrapeThread(best.URL)
				if err != nil {
					if util.IsBlocked(err) {
						log.Error("blocked during scrape", "error", err)
						resultCh <- workResult{
							game: game, query: query,
							interrupted: true,
							msg:         fmt.Sprintf("  ⚠ BLOCKED: %v\n  Try refreshing your F95Zone session.\n", err),
						}
						return // worker stops on block
					}
					resultCh <- workResult{
						game: game, query: query,
						skipped: true,
						msg:     fmt.Sprintf("  ✗ Scrape failed: %v\n\n", err),
					}
					atomic.AddInt64(&completedCount, 1)
					continue
				}

				// Check engine consistency.
				if !engine.EngineMatchesThread(detEngine, data.Tags, best.Title) {
					atomic.AddInt64(&completedCount, 1)
					resultCh <- workResult{
						game: game, query: query,
						skipped: true,
						msg: fmt.Sprintf("  ⚠ Engine mismatch (scanner: %s, thread: %q, tags: %s) — skipping\n",
							detEngine.Engine, util.Truncate(best.Title, 60), engine.FormatTagsBrief(data.Tags, 4)),
					}
					continue
				}

				scraper.ApplyThreadData(&game, data, best.URL)
				game.VersionCheckedAt = time.Now()

				saveMu.Lock()
				if err := database.UpdateGame(&game); err != nil {
					fmt.Fprintf(os.Stderr, "  ✗ Save failed: %v\n\n", err)
					saveMu.Unlock()
					atomic.AddInt64(&completedCount, 1)
					resultCh <- workResult{
						game: game, query: query,
						skipped: true,
						msg:     fmt.Sprintf("  ✗ Save failed: %v\n\n", err),
					}
					continue
				}
				saveMu.Unlock()

				// Cache the successful association.
				if data.ThreadID > 0 {
					scraper.SetCachedThreadID(query, data.ThreadID)
				}

				// Save scraped metadata.
				if data.Developer != "" || data.Overview != "" || data.CoverURL != "" {
					saveMu.Lock()
					meta := &db.ScrapedMeta{
						GameID:    game.ID,
						Developer: data.Developer,
						Overview:  data.Overview,
						CoverURL:  data.CoverURL,
					}
					if err := database.UpsertScrapedMeta(meta); err != nil {
						fmt.Fprintf(os.Stderr, "  ⚠ Failed to save metadata for %q: %v\n", game.Title, err)
					}
					saveMu.Unlock()
				}

				log.Info("game associated", "title", game.Title, "version", data.Version)

				savedMsg := fmt.Sprintf("  ✓ Saved (%s)", game.Title)
				if data.Version != "" {
					savedMsg += fmt.Sprintf(" v%s", data.Version)
				}
				if data.Developer != "" {
					savedMsg += fmt.Sprintf(" • %s", data.Developer)
				}
				savedMsg += "\n"

				atomic.AddInt64(&completedCount, 1)
				resultCh <- workResult{
					game:       game,
					query:      query,
					associated: true,
					msg:        savedMsg,
				}
			}
		}(w)
	}

	// Close results when all workers finish.
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Collect results.
	associated := 0
	skipped := 0
	interrupted := false
	processed := make(map[string]bool)

	for res := range resultCh {
		processed[res.query] = true
		if res.interrupted {
			interrupted = true
			fmt.Fprint(os.Stderr, res.msg)
			break
		}
		if res.associated {
			associated++
		} else {
			skipped++
		}
		fmt.Fprint(os.Stderr, res.msg)
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
	return nil
}
