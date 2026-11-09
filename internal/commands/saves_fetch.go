package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mili/moxie/internal/browser"
	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/downloader"
	"github.com/mili/moxie/internal/log"
	"github.com/mili/moxie/internal/saves"
	"github.com/mili/moxie/internal/scraper"
	"github.com/mili/moxie/internal/util"
)

const (
	// savesFetchScrapeTimeout bounds the thread page fetch (metadata only).
	savesFetchScrapeTimeout = 60 * time.Second
	// savesFetchDownloadTimeout bounds an attachment transfer. Save archives
	// are usually a few MB; 15 minutes is generous but still bounded.
	savesFetchDownloadTimeout = 15 * time.Minute
)

// noSaveCandidatesMsg is shown to users when a thread's first post has no
// save files; errNoSaveCandidates wraps the same text for error returns.
const noSaveCandidatesMsg = "No save candidates found in the first post."

// errNoSaveCandidates is returned when a thread's first post has no save files.
var errNoSaveCandidates = errors.New(noSaveCandidatesMsg)

// savesFetch discovers save files posted in a thread's first post and imports
// one of them as a save set. It is the CLI half of the Phase 3 fetch pipeline;
// selection is non-interactive (--pick) per the project's shell policy.
func savesFetch(args []string) {
	fs := flag.NewFlagSet("saves-fetch", flag.ExitOnError)
	list := fs.Bool("list", false, "List save candidates and exit")
	pick := fs.Int("pick", 0, "1-based candidate index to download")
	password := fs.String("password", "", "Password for encrypted archives")
	threadURL := fs.String("url", "", "Override the thread URL (for testing)")
	positionals := parseSavesArgs(fs, args)

	if len(positionals) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: moxie saves fetch <id|name> [--list] [--pick N] [--password P] [--url URL]\n")
		os.Exit(1)
	}

	database := OpenDB()
	defer database.Close()
	game := ResolveGame(database, positionals[0])

	url := resolveFetchURL(*threadURL, game)
	if url == "" {
		fmt.Fprintf(os.Stderr, "Game %q has no F95 thread — run `moxie scrape %d` first (or pass --url).\n",
			game.Title, game.ID)
		os.Exit(1)
	}

	cookie, cookieErr := browser.GetF95Cookies()
	if cookieErr != nil {
		fmt.Fprintf(os.Stderr, "Firefox cookie detection: %v\n", cookieErr)
	}
	if cookie == "" {
		fmt.Fprintf(os.Stderr, "Cookie required. Log into f95zone.to in Firefox.\n")
		os.Exit(1)
	}

	client := scraper.NewClient(cookie)
	scrapeCtx, cancelScrape := context.WithTimeout(context.Background(), savesFetchScrapeTimeout)
	data, scrapeErr := client.ScrapeThreadWithContext(scrapeCtx, url)
	cancelScrape()
	if scrapeErr != nil {
		if util.IsBlocked(scrapeErr) {
			fmt.Fprintf(os.Stderr, "\nBLOCKED: %v\n", scrapeErr)
			fmt.Fprintf(os.Stderr, "Try refreshing your F95Zone session in Firefox and running again.\n")
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Error scraping thread: %v\n", scrapeErr)
		os.Exit(1)
	}

	candidates := data.SaveCandidates
	if *list {
		printSaveCandidates(os.Stdout, candidates)
		return
	}
	if len(candidates) == 0 {
		fmt.Fprintln(os.Stdout, noSaveCandidatesMsg)
		return
	}

	candidate, selErr := selectSaveCandidate(candidates, *pick)
	if selErr != nil {
		// Ambiguous selection: show the list so the user can pick a number.
		if *pick <= 0 && len(candidates) > 1 {
			printSaveCandidates(os.Stderr, candidates)
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", selErr)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Fetching %s (%s) from %s...\n", candidate.Filename, candidate.Kind, candidate.Host)

	engine := openSavesEngine(database)
	dlCtx, cancelDownload := context.WithTimeout(context.Background(), savesFetchDownloadTimeout)
	defer cancelDownload()

	set, err := fetchSaveSet(dlCtx, engine, client, cookie, game, *candidate, *password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching save: %v%s\n", err, passwordHint(err, *password))
		os.Exit(1)
	}

	fmt.Printf("Fetched %s as set %s (%d files, %s)\n",
		saveFetchFilename(*candidate), set.Name, set.FileCount, formatSize(set.SizeBytes))
	fmt.Printf("Apply it with: moxie saves apply %d %q\n", game.ID, set.Name)
}

// resolveFetchURL picks the thread URL to scrape: an explicit override wins,
// otherwise it is derived from the game's thread ID/URL.
func resolveFetchURL(override string, game *db.Game) string {
	if override != "" {
		return override
	}
	if game == nil {
		return ""
	}
	return scraper.ResolveScrapeURL(game.F95URL, game.F95ThreadID)
}

// selectSaveCandidate chooses which candidate to download. pick is a 1-based
// index; 0 means "not specified". With no pick, exactly one candidate is
// auto-selected and multiple candidates are ambiguous. Out-of-range picks and
// empty candidate lists are errors.
func selectSaveCandidate(candidates []scraper.SaveCandidate, pick int) (*scraper.SaveCandidate, error) {
	switch {
	case len(candidates) == 0:
		return nil, errNoSaveCandidates
	case pick > 0:
		if pick > len(candidates) {
			return nil, fmt.Errorf("--pick %d is out of range (1-%d)", pick, len(candidates))
		}
		return &candidates[pick-1], nil
	case len(candidates) == 1:
		return &candidates[0], nil
	default:
		return nil, fmt.Errorf("%d save candidates found; re-run with --pick N to choose one", len(candidates))
	}
}

// printSaveCandidates renders a numbered candidate table. It writes to w so
// tests can capture the shape without touching os.Stdout.
func printSaveCandidates(w io.Writer, candidates []scraper.SaveCandidate) {
	if len(candidates) == 0 {
		fmt.Fprintln(w, noSaveCandidatesMsg)
		return
	}
	fmt.Fprintf(w, "%-4s %-11s %-14s %-34s %s\n", "#", "KIND", "HOST", "FILENAME", "CONTEXT")
	fmt.Fprintln(w, strings.Repeat("-", 100))
	for i, c := range candidates {
		fmt.Fprintf(w, "%-4d %-11s %-14s %-34s %s\n",
			i+1,
			string(c.Kind),
			util.Truncate(c.Host, 14),
			util.Truncate(c.Filename, 34),
			util.Truncate(c.Context, 40),
		)
	}
}

// fetchSaveSet downloads candidate into a temp dir and imports it as a new save
// set for game. The temp dir is removed on every return path; the set's own
// files live under the engine root once Import succeeds.
func fetchSaveSet(ctx context.Context, engine *saves.Engine, client *scraper.Client, cookie string, game *db.Game, candidate scraper.SaveCandidate, password string) (*db.SaveSet, error) {
	if engine == nil {
		return nil, fmt.Errorf("saves engine is not initialized")
	}
	if game == nil {
		return nil, fmt.Errorf("game is required")
	}

	tmpDir, err := os.MkdirTemp("", "moxie-saves-fetch-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tmpPath, n, err := downloadCandidate(ctx, client, cookie, candidate, tmpDir)
	if err != nil {
		return nil, err
	}
	log.Info("saves fetch downloaded candidate",
		"kind", string(candidate.Kind),
		"host", candidate.Host,
		"filename", candidate.Filename,
		"bytes", n,
	)

	label := strings.TrimSpace(candidate.Filename)
	if label == "" {
		label = "f95 save"
	}
	set, err := engine.Import(game, tmpPath, saves.ImportOptions{
		Label:       label,
		Source:      sourceForCandidate(candidate),
		SourceURL:   candidate.URL,
		GameVersion: game.Version,
		Password:    password,
	})
	if err != nil {
		return nil, err
	}
	return set, nil
}

// downloadCandidate fetches candidate into dir and returns the downloaded path
// and its byte count.
//
// Attachments (F95Zone-hosted) stream through the scraper client, which shares
// the authenticated cookie session and request pacing; ctx carries the
// deadline. External file-host links are delegated to internal/downloader so
// hosts like Mega/GoFile/Pixeldrain get their URL resolution for free (that
// package owns its own transfer timeout, so ctx does not bound this branch).
func downloadCandidate(ctx context.Context, client *scraper.Client, cookie string, candidate scraper.SaveCandidate, dir string) (string, int64, error) {
	log.Info("saves fetch candidate",
		"kind", string(candidate.Kind),
		"host", candidate.Host,
		"filename", candidate.Filename,
	)

	switch candidate.Kind {
	case scraper.SaveCandidateAttachment:
		if client == nil {
			return "", 0, fmt.Errorf("scraper client is required for attachment downloads")
		}
		path := filepath.Join(dir, saveFetchFilename(candidate))
		f, err := os.Create(path)
		if err != nil {
			return "", 0, fmt.Errorf("create temp file: %w", err)
		}
		n, dlErr := client.DownloadToWriter(ctx, candidate.URL, f)
		if closeErr := f.Close(); dlErr == nil && closeErr != nil {
			dlErr = closeErr
		}
		if dlErr != nil {
			os.Remove(path)
			return "", n, fmt.Errorf("download attachment: %w", dlErr)
		}
		return path, n, nil

	case scraper.SaveCandidateExternal:
		if err := downloader.Download(candidate.URL, dir, 0, nil, cookie); err != nil {
			return "", 0, fmt.Errorf("download from %s: %w", candidate.Host, err)
		}
		path := downloader.FindMostRecentFile(dir)
		if path == "" {
			return "", 0, fmt.Errorf("download from %s produced no file", candidate.Host)
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", 0, fmt.Errorf("stat downloaded file: %w", err)
		}
		return path, info.Size(), nil

	default:
		return "", 0, fmt.Errorf("unsupported save candidate kind %q", candidate.Kind)
	}
}

// sourceForCandidate maps a candidate to its save_set source value (the values
// allowed by the schema's CHECK constraint).
func sourceForCandidate(candidate scraper.SaveCandidate) string {
	if candidate.Kind == scraper.SaveCandidateAttachment {
		return "f95_attachment"
	}
	return "f95_link"
}

// saveFetchFilename returns a safe basename for the staged download. The last
// path element is used so a hostile candidate filename cannot escape dir.
func saveFetchFilename(candidate scraper.SaveCandidate) string {
	name := filepath.Base(strings.TrimSpace(candidate.Filename))
	switch name {
	case "", ".", "..", string(filepath.Separator):
		return "save-download"
	}
	return name
}

// passwordHint returns a trailing hint when an import fails and no archive
// password was supplied, since encrypted archives are common on F95Zone.
func passwordHint(err error, password string) string {
	if err == nil || password != "" {
		return ""
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "extract archive") ||
		strings.Contains(msg, "password") ||
		strings.Contains(msg, "encrypted") {
		return "\n  hint: the archive may be password-protected — re-run with --password P"
	}
	return ""
}
