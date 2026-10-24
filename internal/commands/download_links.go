package commands

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/downloader"
	"github.com/mili/moxie/internal/log"
	"github.com/mili/moxie/internal/scraper"
)

// selectBestLinkByPlatform selects the best download link based on platform priority.
// Skips online-only links that aren't downloadable.
func selectBestLinkByPlatform(links []db.DownloadLink, targetPlatform downloader.Platform) *db.DownloadLink {
	if len(links) == 0 {
		return nil
	}

	type scoredLink struct {
		link  db.DownloadLink
		score int
	}
	var scored []scoredLink

	for _, link := range links {
		// Skip online-only / browser-playable links
		if downloader.IsOnlineOnly(link.Name, link.URL) {
			continue
		}

		score := downloader.ScoreDownloadLink(link, targetPlatform)
		scored = append(scored, scoredLink{link, score})
	}

	if len(scored) == 0 {
		return nil
	}
	best := scored[0]
	for _, s := range scored[1:] {
		if s.score > best.score {
			best = s
		}
	}

	return &best.link
}

// hasF95CheckLinks reports whether any stored link is an F95Zone /dl/
// download-link page. Those point at the post revision that was live when
// they were generated, so the caller re-scrapes before downloading to match
// the current OP (and to pick up a new version).
func hasF95CheckLinks(links []db.DownloadLink) bool {
	for _, l := range links {
		if downloader.IsF95DownloadLink(l.URL) {
			return true
		}
	}
	return false
}

// dataHasF95CheckLinks reports whether a freshly scraped link set contains
// F95Zone's browser-checked /dl/ links.
func dataHasF95CheckLinks(links []scraper.DownloadLink) bool {
	for _, l := range links {
		if downloader.IsF95DownloadLink(l.URL) {
			return true
		}
	}
	return false
}

// containsF95CheckLink reports whether any scored link is a /dl/ check link.
func containsF95CheckLink(scored []db.DownloadLink) bool {
	for _, l := range scored {
		if downloader.IsF95DownloadLink(l.URL) {
			return true
		}
	}
	return false
}

// addCacheDownloadLinks appends the F95Checker cache API's /masked/ download
// links for the same thread version. F95Zone's 2026-10 /dl/ links require a
// browser-satisfied hCaptcha; the cache API still carries the older
// /masked/ form, which moxie unwraps over plain HTTP. Links are only used
// when the cache's version matches the freshly scraped one, so a stale
// cache can never silently install an older build.
//
// Returns the number of links inserted. Best-effort: any failure (thread
// untracked, cache behind, network) is logged and ignored.
func addCacheDownloadLinks(database *db.Database, game *db.Game, data *scraper.ThreadData, cookie string) int {
	if game.F95ThreadID <= 0 || len(data.DownloadLinks) == 0 {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	public := scraper.NewPublicAPIWithCookie(cookie)
	ct, err := public.CacheFullThread(ctx, game.F95ThreadID)
	if err != nil {
		log.Debug("cache download links unavailable", "game_id", game.ID, "error", err)
		return 0
	}
	if !sameVersion(ct.Version, data.Version) {
		log.Debug("cache download links skipped (stale version)",
			"game_id", game.ID, "cache", ct.Version, "thread", data.Version)
		return 0
	}

	existing, _ := database.ListDownloadLinks(game.ID, "", true)
	have := make(map[string]bool, len(existing))
	for _, l := range existing {
		have[l.URL] = true
	}

	added := 0
	for _, cd := range ct.Downloads {
		if have[cd.URL] {
			continue
		}
		// Only the older /masked/ (and rare direct-host) forms are useful:
		// a cached /dl/ link is just another browser-check page.
		if downloader.IsF95DownloadLink(cd.URL) {
			continue
		}
		host := downloader.IdentifyHostInURL(cd.URL)
		if host == "other" {
			continue
		}
		platform := downloader.DetectPlatform(cd.Platform, cd.URL)
		name := cd.Platform + " · " + cd.Host
		if cd.Platform == "" {
			name = cd.Host
		}
		link := &db.DownloadLink{
			GameID:   game.ID,
			URL:      cd.URL,
			Host:     host,
			Name:     name,
			Platform: db.Platform(platform),
		}
		if _, err := database.CreateDownloadLink(link); err != nil {
			log.Debug("failed to insert cache download link", "url", cd.URL, "error", err)
			continue
		}
		have[cd.URL] = true
		added++
	}
	return added
}

// sameVersion compares two version strings after reducing both to their
// numeric core, so "v1.2", "1.2" and "V1.2" match.
func sameVersion(a, b string) bool {
	norm := func(v string) string {
		v = strings.ToLower(strings.TrimSpace(scraper.StripVersionQualifier(v)))
		v = strings.TrimPrefix(v, "v")
		return strings.TrimSpace(v)
	}
	na, nb := norm(a), norm(b)
	return na != "" && na == nb
}

// ListDownloads shows all downloads and their status.
func ListDownloads(args []string) {
	fs := flag.NewFlagSet("downloads", flag.ExitOnError)
	filterStatus := fs.String("status", "", "Filter by status (pending, downloading, completed, failed)")
	fs.Parse(args)

	database := OpenDB()
	defer database.Close()

	downloads, err := database.ListDownloads(*filterStatus)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing downloads: %v\n", err)
		os.Exit(1)
	}

	if len(downloads) == 0 {
		fmt.Fprintln(os.Stderr, "No downloads found.")
		return
	}

	fmt.Printf("%-5s %-8s %-12s %-10s %-15s %s\n", "ID", "Game", "Status", "Progress", "Speed", "URL")
	fmt.Println(strings.Repeat("-", 100))

	for _, d := range downloads {
		progress := fmt.Sprintf("%.1f%%", d.PercentComplete)
		if d.Status == db.DownloadStatusCompleted {
			progress = "100%"
		}

		speed := formatSpeed(d.SpeedBytesPerSec)
		if d.Status != db.DownloadStatusDownloading {
			speed = "-"
		}

		url := d.URL
		if len(url) > 40 {
			url = url[:37] + "..."
		}

		fmt.Printf("%-5d %-8d %-12s %-10s %-15s %s\n",
			d.ID, d.GameID, d.Status, progress, speed, url)
	}
}

// CheckDeadLinks validates all download links and marks dead ones.
func CheckDeadLinks(args []string) {
	fs := flag.NewFlagSet("check-links", flag.ExitOnError)
	gameID := fs.Int64("game", 0, "Check links for specific game ID only")
	deleteDead := fs.Bool("delete", false, "Delete dead links instead of just marking them")
	fs.Parse(args)

	database := OpenDB()
	defer database.Close()

	var allLinks []db.DownloadLinkWithGame
	var err error

	if *gameID > 0 {
		// Single game: load links via per-game query
		gameLinks, listErr := database.ListDownloadLinks(*gameID, "", true)
		if listErr != nil {
			fmt.Fprintf(os.Stderr, "Error listing links: %v\n", listErr)
			os.Exit(1)
		}
		game, getErr := database.GetGame(*gameID)
		if getErr != nil {
			fmt.Fprintf(os.Stderr, "Error loading game: %v\n", getErr)
			os.Exit(1)
		}
		title := ""
		path := ""
		if game != nil {
			title = game.Title
			path = game.Path
		}
		for _, l := range gameLinks {
			allLinks = append(allLinks, db.DownloadLinkWithGame{
				DownloadLink: l,
				GameTitle:    title,
				GamePath:     path,
			})
		}
	} else {
		allLinks, err = database.AllDownloadLinks(true)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing links: %v\n", err)
			os.Exit(1)
		}
	}

	if len(allLinks) == 0 {
		fmt.Fprintln(os.Stderr, "No download links found.")
		return
	}

	fmt.Printf("Checking %d download links...\n\n", len(allLinks))

	checked, dead := 0, 0
	for _, link := range allLinks {
		if link.IsDead {
			fmt.Printf("[%s] Already marked dead: %s\n", link.Host, link.Name)
			dead++
			continue
		}

		fmt.Printf("Checking [%s] %s... ", link.Host, link.Name)
		if downloader.IsF95DownloadLink(link.URL) {
			// The destination is behind F95Zone's browser check and the
			// token is short-lived: a HEAD only proves the check page
			// answered, not that the link still works.
			fmt.Println("OK (browser check required; link expires — re-scrape before downloading)")
			checked++
			continue
		}
		if err := downloader.CheckLink(link.URL); err != nil {
			fmt.Printf("DEAD: %v\n", err)
			if *deleteDead {
				database.DeleteDownloadLink(link.ID)
				fmt.Printf("  -> Deleted\n")
			} else {
				database.MarkDownloadLinkDead(link.ID, err.Error())
				fmt.Printf("  -> Marked as dead\n")
			}
			dead++
		} else {
			fmt.Println("OK")
		}
		checked++
	}

	fmt.Printf("\nDone: %d checked, %d dead links found\n", checked, dead)
}
