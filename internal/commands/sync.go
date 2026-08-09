package commands

import (
	"context"
	"errors"
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
	// IsNew is true when the thread version is newer than the known one, or
	// differs in a way that cannot be ordered. Read Diff to tell them apart.
	IsNew bool `json:"is_new"`
	// Diff is the version.Diff outcome: same, newer, older, or changed.
	Diff  string `json:"diff,omitempty"`
	Error string `json:"error,omitempty"`
}

const UpdateCheckCooldown = 24 * time.Hour

// ErrSyncInterrupted is returned by RunScrapeAuto when a worker reports a
// block from F95Zone. Callers (RunSync, runScanDir, ScrapeAutoWrapper) must
// NOT proceed to a Phase 2 update check after it — the just-blocked IP
// should not be hammered further. The message is surfaced as
// "sync interrupted — refresh session and retry".
var ErrSyncInterrupted = errors.New("sync interrupted")

// SyncGameResult holds the outcome of a single-game sync operation.
type SyncGameResult struct {
	Associated      bool                   // true if a new F95Zone association was made
	SearchResults   []scraper.SearchResult // search results (Phase 1 only)
	BestMatch       *scraper.SearchResult  // the chosen best match (Phase 1 only)
	BestScore       float64                // match score of the best result
	ThreadData      *scraper.ThreadData    // scraped thread data
	EngineMismatch  bool                   // engine mismatch detected between scanner and thread
	VersionUpdated  bool                   // true if a newer version was found
	NewVersion      string                 // latest version from F95Zone
	OldVersion      string                 // previously known version
	ScrapedMetadata bool                   // metadata (developer/overview/cover) was saved
	CooldownSkipped bool                   // skipped because of 24h cooldown
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
//
// Sync runs through F95Zone's public JSON endpoints (checker.php for bulk
// versions, latest_data.php for search, the F95Checker cache API for thread
// metadata) carrying the resolved session cookie when one is available — it
// lifts F95Zone's hard anonymous rate limit and the cache API's per-hour cap.
// The endpoints themselves work cookie-free, so sync still succeeds when the
// browser session is expired or blocked; cookies are required only for the
// direct-scrape fallback layer.
func RunSync(database *db.Database, cfg SyncConfig) error {
	cookie := ResolveCookie(cfg.Cookie, cfg.CookieFile)

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
	public := scraper.NewPublicAPIWithCookie(cookie)
	if err := RunScrapeAuto(database, client, cfg.Force, cfg.Parallel, public); err != nil {
		// A block interrupts Phase 1; skip Phase 2 entirely — the IP was
		// just flagged by F95Zone and the update check would hammer it.
		if errors.Is(err, ErrSyncInterrupted) {
			return err
		}
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

	updatesFound, _ := RunUpdateCheck(database, client, trackable, cfg.Force, public)
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
	cookieStr := fs.String("cookie", "", "Cookie header (only needed for the direct-scrape fallback)")
	cookieFile := fs.String("cookie-file", "", "Cookie file")
	unsafe := fs.Bool("unsafe", false, "⚠ Skip rate limiting")
	force := fs.Bool("force", false, "Force re-check even if checked within 24h")
	parallel := fs.Int("parallel", 3, "Number of concurrent scrapers (default 3)")
	fs.Parse(args)

	cookie := ResolveCookie(*cookieStr, *cookieFile)

	database := OpenDB()
	defer database.Close()

	// Single-game sync: moxie sync <game-id>
	if fs.NArg() >= 1 {
		if cookie == "" {
			fmt.Fprintf(os.Stderr, "Cookie required for single-game sync. Log into f95zone.to in Firefox.\n")
			os.Exit(1)
		}
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
		if errors.Is(err, ErrSyncInterrupted) {
			fmt.Fprintln(os.Stderr, "\nsync interrupted — refresh session and retry")
			os.Exit(1)
			return
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// RunScrapeAuto finds and associates F95Zone threads for unassociated games.
// When force is false, games that were recently searched without success
// (within UpdateCheckCooldown) are skipped to avoid redundant API calls.
// The workers parameter controls how many concurrent scrapers run
// (1 = sequential, the default for backward compatibility).
//
// When public is non-nil, search runs cookie-free through F95Zone's
// latest-updates endpoint and association data comes from the F95Checker
// cache API; direct thread scraping (cookie path) is the fallback layer.
func RunScrapeAuto(database *db.Database, client *scraper.Client, force bool, workers int, public *scraper.PublicAPI) error {
	// Cancellable across the whole run: when a worker reports a block, the
	// collector cancels so every in-flight worker aborts immediately and
	// wg.Wait below joins them — otherwise the CLI process exits mid-write
	// with workers still doing network I/O.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
				// searchMeta runs parallel to searchRes and carries
				// cookie-free endpoint metadata (thread ID, prefixes,
				// version) for candidates that came from latest-updates.
				var searchMeta []scraper.LatestSearchResult
				if cachedID := scraper.GetCachedThreadID(query); cachedID > 0 {
					cachedURL := scraper.ThreadURL(cachedID)
					searchRes = []scraper.SearchResult{{Title: game.Title, URL: cachedURL}}
					searchMeta = []scraper.LatestSearchResult{{Title: game.Title, URL: cachedURL, ThreadID: cachedID}}
					fmt.Fprintf(os.Stderr, "  (cached thread %d)\n", cachedID)
				} else if public != nil {
					// Cookie-free search: F95Zone's latest-updates endpoint.
					latestRes, err := public.SearchTitle(ctx, query)
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
						log.Debug("latest-updates search failed, falling back to XenForo search",
							"query", query, "error", err)
					} else {
						searchRes = make([]scraper.SearchResult, 0, len(latestRes))
						searchMeta = make([]scraper.LatestSearchResult, 0, len(latestRes))
						for _, r := range latestRes {
							searchRes = append(searchRes, scraper.SearchResult{Title: r.Title, URL: r.URL})
							searchMeta = append(searchMeta, r)
						}
					}
				}
				if len(searchRes) == 0 {
					var err error
					searchRes, err = client.SearchF95ZoneWithContext(ctx, query)
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
				bestIdx := -1
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
					// Non-game rejection is title-based only: latest_data.php
					// prefix numbering is not the F95Checker Type enum, so
					// prefix IDs cannot distinguish games from mods/tools
					// (a real RenPy game returned [7]).
					isNonGame := scraper.IsNonGameThread(r.Title)
					marker := "  "
					if !isNonGame && score > bestScore {
						bestScore = score
						best = &searchRes[j]
						bestIdx = j
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

				// Association path 1: cookie-free — cache API full data for
				// the candidate (available when it came from latest-updates
				// and carries a thread ID). Falls through to path 2 on error.
				var associated bool
				if public != nil && bestIdx >= 0 && bestIdx < len(searchMeta) && searchMeta[bestIdx].ThreadID > 0 {
					meta := searchMeta[bestIdx]

					// Fetch the cache data first: the cache API's Type enum
					// (ct.Engine) is the reliable engine signal and feeds
					// both the consistency check and the association data.
					ct, cacheErr := public.CacheFullThread(ctx, meta.ThreadID)
					if cacheErr == nil {
						// Engine consistency: the cache API's Type enum
						// (ct.Engine) is the only reliable engine signal —
						// latest_data.php prefix numbering is not the Type
						// enum, so prefixes are never consulted. When the
						// type is absent, skip the check: the direct-scrape
						// path validates with real content tags.
						eng := ct.Engine
						if eng != "" && !engine.EngineMatchesThread(detEngine, []string{eng}, best.Title) {
							atomic.AddInt64(&completedCount, 1)
							resultCh <- workResult{
								game: game, query: query,
								skipped: true,
								msg: fmt.Sprintf("  ⚠ Engine mismatch (scanner: %s, thread engine: %s) — skipping\n",
									detEngine.Engine, eng),
							}
							continue
						}

						scraper.ApplyCacheThreadData(&game, ct, meta.ThreadID, meta.Prefixes, bestScore)
						if game.LatestVersion == "" {
							game.LatestVersion = meta.Version
						}
						game.VersionCheckedAt = time.Now()

						saveMu.Lock()
						if err := database.UpdateGame(&game); err != nil {
							saveMu.Unlock()
							fmt.Fprintf(os.Stderr, "  ✗ Save failed: %v\n\n", err)
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
						scraper.SetCachedThreadID(query, meta.ThreadID)

						// Save metadata from the cache API.
						if ct.Developer != "" || ct.Description != "" || ct.ImageURL != "" {
							saveMu.Lock()
							if err := database.UpsertScrapedMeta(&db.ScrapedMeta{
								GameID:    game.ID,
								Developer: ct.Developer,
								Overview:  ct.Description,
								CoverURL:  ct.ImageURL,
							}); err != nil {
								fmt.Fprintf(os.Stderr, "  ⚠ Failed to save metadata for %q: %v\n", game.Title, err)
							}
							saveMu.Unlock()
						}

						associated = true
						log.Info("game associated via cache API", "title", game.Title, "version", ct.Version)

						savedMsg := fmt.Sprintf("  ✓ Saved (%s)", game.Title)
						if ct.Version != "" {
							savedMsg += fmt.Sprintf(" v%s", strings.TrimPrefix(ct.Version, "v"))
						}
						if ct.Developer != "" {
							savedMsg += fmt.Sprintf(" • %s", ct.Developer)
						}
						savedMsg += "\n"
						atomic.AddInt64(&completedCount, 1)
						resultCh <- workResult{
							game:       game,
							query:      query,
							associated: true,
							msg:        savedMsg,
						}
					} else {
						log.Debug("cache API unavailable for association; falling back to thread scrape",
							"thread", meta.ThreadID, "error", cacheErr)
					}
				}

				if associated {
					continue
				}

				// Association path 2: direct thread scrape (cookie path).
				fmt.Fprintf(os.Stderr, "  ⬇ Scraping %s...\n", best.URL)
				data, err := client.ScrapeThreadWithContext(ctx, best.URL)
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
			// Abort the remaining workers: they are still doing network I/O
			// and SQLite writes, and the CLI process exits right after this
			// returns. ctx makes them fail fast on their next request.
			cancel()
			break
		}
		if res.associated {
			associated++
		} else {
			skipped++
		}
		fmt.Fprint(os.Stderr, res.msg)
	}

	// Join the workers so their writes land before we return (and so no
	// goroutine keeps running after the process starts tearing down).
	wg.Wait()

	elapsed := time.Since(startTime).Truncate(time.Second)

	if interrupted {
		log.Warn("auto-association interrupted by block", "associated", associated, "skipped", skipped)
		fmt.Fprintf(os.Stderr, "=== INTERRUPTED (blocked by F95Zone) ===\n")
		fmt.Fprintf(os.Stderr, "=== Done: %d associated, %d skipped, %d/%d total in %s ===\n",
			associated, skipped, associated+skipped, total, elapsed)
		// Sentinel: Phase 2 must not run against the just-blocked IP.
		return ErrSyncInterrupted
	}
	log.Info("auto-association complete",
		"associated", associated,
		"skipped", skipped,
		"total", total,
		"elapsed", elapsed.String(),
	)
	fmt.Fprintf(os.Stderr, "=== Done: %d associated, %d skipped, %d/%d total in %s ===\n",
		associated, skipped, associated+skipped, total, elapsed)
	return nil
}
