package commands

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/mili/moxie/internal/browser"
	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/scraper"
	"github.com/mili/moxie/internal/util"
)

// Scrape scrapes F95Zone metadata for a single game.
func Scrape(args []string) {
	fs := flag.NewFlagSet("scrape", flag.ExitOnError)
	cookieStr := fs.String("cookie", "", "Cookie header from browser")
	cookieFile := fs.String("cookie-file", "", "File containing cookie header")
	threadURL := fs.String("url", "", "F95Zone thread URL")
	autoMode := fs.Bool("auto", false, "Auto-associate games using Firefox cookies + search")
	unsafe := fs.Bool("unsafe", false, "⚠ Skip rate limiting (fast but risky — may get IP banned)")
	fs.Parse(args)

	// Get cookie string — try Firefox auto-detect first.
	cookie := ResolveCookie(*cookieStr, *cookieFile)

	if *autoMode {
		ScrapeAutoWrapper(cookie, *unsafe)
		return
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie scrape <game-id> [flags]\n")
		fmt.Fprintf(os.Stderr, "       moxie scrape --auto\n")
		os.Exit(1)
	}
	id := util.MustParseInt(fs.Arg(0))

	if cookie == "" {
		fmt.Fprintf(os.Stderr, "Cookie required. Use --cookie, --cookie-file, or log into f95zone.to in Firefox.\n")
		os.Exit(1)
	}

	database := util.OpenDB()
	defer database.Close()

	game, err := database.GetGame(id)
	if err != nil || game == nil {
		fmt.Fprintf(os.Stderr, "Game with ID %d not found.\n", id)
		os.Exit(1)
	}

	url := *threadURL
	if url == "" {
		url = game.F95URL
	}
	if url == "" {
		fmt.Fprintf(os.Stderr, "No F95Zone URL specified. Use --url or set it on the game first.\n")
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Scraping %s...\n", url)

	client := scraper.NewClient(cookie)
	data, err := client.ScrapeThread(url)
	if err != nil {
		if util.IsBlocked(err) {
			fmt.Fprintf(os.Stderr, "\n⚠ BLOCKED: %v\n", err)
			fmt.Fprintf(os.Stderr, "Try refreshing your F95Zone session in Firefox and running again.\n")
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Error scraping: %v\n", err)
		os.Exit(1)
	}

	// Update game with scraped data.
	ApplyThreadData(game, data, url)

	if err := database.UpdateGame(game); err != nil {
		fmt.Fprintf(os.Stderr, "Error updating game: %v\n", err)
		os.Exit(1)
	}

	// Save scraped metadata.
	if data.Developer != "" || data.Overview != "" || data.CoverURL != "" {
		meta := &db.ScrapedMeta{
			GameID:    id,
			Developer: data.Developer,
			Overview:  data.Overview,
			CoverURL:  data.CoverURL,
		}
		if err := database.UpsertScrapedMeta(meta); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving metadata: %v\n", err)
		}
	}

	fmt.Printf("Scraped: %s", data.Title)
	if data.Version != "" {
		fmt.Printf(" [v%s]", data.Version)
	}
	fmt.Println()
	if data.Developer != "" {
		fmt.Printf("Developer: %s\n", data.Developer)
	}
	if len(data.Tags) > 0 {
		fmt.Printf("Tags: %s\n", strings.Join(data.Tags, ", "))
	}
	if len(data.DownloadLinks) > 0 {
		fmt.Printf("Download links: %d found\n", len(data.DownloadLinks))
		for _, dl := range data.DownloadLinks {
			fmt.Printf("  [%s] %s\n", dl.Host, dl.Name)
		}
	}
}

// ScrapeBatch scrapes F95Zone metadata for multiple games from a file.
// File format: one entry per line — "<id> <url>"
// Lines starting with # and blank lines are ignored.
func ScrapeBatch(args []string) {
	fs := flag.NewFlagSet("scrape-batch", flag.ExitOnError)
	cookieStr := fs.String("cookie", "", "Cookie header from browser")
	cookieFile := fs.String("cookie-file", "", "File containing cookie header")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie scrape-batch <file>\n")
		fmt.Fprintf(os.Stderr, "File format: one entry per line — \"<id> <url>\"\n")
		os.Exit(1)
	}

	cookie := ResolveCookie(*cookieStr, *cookieFile)
	if cookie == "" {
		fmt.Fprintf(os.Stderr, "Cookie required. Log into f95zone.to in Firefox.\n")
		os.Exit(1)
	}

	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	type entry struct {
		id  int64
		url string
	}
	var entries []entry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			fmt.Fprintf(os.Stderr, "Skipping malformed line: %q\n", line)
			continue
		}
		id, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Skipping invalid ID %q: %v\n", parts[0], err)
			continue
		}
		entries = append(entries, entry{id: id, url: parts[1]})
	}

	if len(entries) == 0 {
		fmt.Fprintln(os.Stderr, "No valid entries found in file.")
		os.Exit(1)
	}

	database := util.OpenDB()
	defer database.Close()

	client := scraper.NewClient(cookie)
	ok, failed := 0, 0

	for i, e := range entries {
		fmt.Fprintf(os.Stderr, "[%d/%d] Scraping ID %d — %s\n", i+1, len(entries), e.id, e.url)

		game, err := database.GetGame(e.id)
		if err != nil || game == nil {
			fmt.Fprintf(os.Stderr, "  ✗ Game ID %d not found\n", e.id)
			failed++
			continue
		}

		td, err := client.ScrapeThread(e.url)
		if err != nil {
			if util.IsBlocked(err) {
				fmt.Fprintf(os.Stderr, "\n⚠ BLOCKED: %v\nStopping batch.\n", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "  ✗ %v\n", err)
			failed++
			continue
		}

		ApplyThreadData(game, td, e.url)
		if err := database.UpdateGame(game); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ Save error: %v\n", err)
			failed++
			continue
		}

		if td.Developer != "" || td.Overview != "" || td.CoverURL != "" {
			meta := &db.ScrapedMeta{
				GameID:    e.id,
				Developer: td.Developer,
				Overview:  td.Overview,
				CoverURL:  td.CoverURL,
			}
			_ = database.UpsertScrapedMeta(meta)
		}

		fmt.Fprintf(os.Stderr, "  ✓ %s", td.Title)
		if td.Version != "" {
			fmt.Fprintf(os.Stderr, " [v%s]", td.Version)
		}
		fmt.Fprintln(os.Stderr)
		ok++
	}

	fmt.Fprintf(os.Stderr, "\nDone: %d scraped, %d failed.\n", ok, failed)
}

// ResolveCookie returns a cookie string from the most available source:
// 1. Explicit --cookie flag, 2. --cookie-file, 3. Firefox auto-detect.
func ResolveCookie(explicit, file string) string {
	if explicit != "" {
		return explicit
	}
	if file != "" {
		if strings.HasSuffix(strings.ToLower(file), ".sqlite") {
			cookie, err := browser.GetF95CookiesFromSQLite(file)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Reading cookie database: %v\n", err)
				return ""
			}
			return cookie
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(data))
	}
	cookie, err := browser.GetF95Cookies()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Firefox cookie detection: %v\n", err)
		return ""
	}
	fmt.Fprintf(os.Stderr, "Using cookies from Firefox.\n")
	return cookie
}

// ApplyThreadData copies scraped ThreadData fields onto a Game.
// Non-empty fields in data overwrite the corresponding game fields.
func ApplyThreadData(game *db.Game, data *scraper.ThreadData, url string) {
	if data.Title != "" {
		// Extract engine from F95Zone thread prefix before stripping.
		// Useful when scanner can't determine the engine (e.g., RPG Maker
		// MV/MZ games with HTML/JS files get detected as "HTML").
		if f95Eng := ExtractEngineFromTitle(data.Title); f95Eng != "" {
			if game.Engine == "" || game.Engine == "Unknown" || game.Engine == "Others" {
				game.Engine = f95Eng
			}
		}
		game.Title = StripThreadPrefix(data.Title)
	}
	if data.Version != "" {
		// LatestVersion tracks the F95Zone version.
		// Version is the locally-installed version (from directory
		// name scan) — never overwrite it with F95Zone data.
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
}

// ScrapeAutoWrapper opens the database and runs auto-association.
// This wrapper exists so Scrape can call it without already having a DB handle.
func ScrapeAutoWrapper(cookie string, unsafe bool) {
	database := util.OpenDB()
	defer database.Close()
	RunScrapeAuto(database, cookie, unsafe)
}
