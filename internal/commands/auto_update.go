package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mili/moxie/internal/config"
	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/downloader"
	"github.com/mili/moxie/internal/log"
	"github.com/mili/moxie/internal/scraper"
	"github.com/mili/moxie/internal/version"
)

// autoUpdateOptions controls one auto-update run.
type autoUpdateOptions struct {
	Cookie   string
	Platform string
	Extract  bool
	Max      int // 0 = no limit
	DryRun   bool
	Force    bool // ignore the per-game retry backoff
	Client   *scraper.Client
}

// autoUpdateSummary reports what a run did.
type autoUpdateSummary struct {
	Candidates int
	Installed  int
	NeedsHuman int
	Failed     int
	NoLinks    int
	Skipped    int
	Ambiguous  int
}

// autoUpdateRetryAfter is how long a game waits before another automatic
// attempt after a failure, so a permanently broken mirror is not hammered.
const autoUpdateRetryAfter = 30 * time.Minute

func autoUpdateLockPath() string {
	return filepath.Join(config.ConfigDir(), "auto-update.lock")
}

func autoUpdateStatePath() string {
	return filepath.Join(config.ConfigDir(), "auto-update.json")
}

// autoUpdateState is disposable retry bookkeeping. It only prevents repeating
// a failed download (and, when browser fallback is in play, re-opening the same
// window) every few minutes.
type autoUpdateState struct {
	Games map[string]autoUpdateAttempt `json:"games"`
}

type autoUpdateAttempt struct {
	LastAttempt   time.Time `json:"last_attempt"`
	Attempts      int       `json:"attempts"`
	Status        string    `json:"status"`
	LastError     string    `json:"last_error,omitempty"`
	TargetVersion string    `json:"target_version,omitempty"`
}

// AutoUpdate checks for new versions and installs every update it can finish
// without a person. Challenge-graded links are handed to the installed
// browser by the downloader's browser fallback, so most updates complete
// unattended; the rest are reported for a manual retry.
func AutoUpdate(args []string) {
	fs := flag.NewFlagSet("auto-update", flag.ExitOnError)
	cookieStr := fs.String("cookie", "", "Cookie header (recommended: needed to refresh download links)")
	cookieFile := fs.String("cookie-file", "", "File containing cookie header")
	platform := fs.String("platform", "", "Platform preference (linux/windows/macos, default: auto-detect)")
	noExtract := fs.Bool("no-extract", false, "Do not extract/merge; download only")
	max := fs.Int("max", 0, "Maximum number of games to update this run (0 = all)")
	dryRun := fs.Bool("dry-run", false, "Show what would be updated without downloading")
	force := fs.Bool("force", false, "Ignore the 24h check cooldown and retry backoff")
	unsafe := fs.Bool("unsafe", false, "Skip rate limiting when re-checking versions")
	checkOnly := fs.Bool("check-only", false, "Only check for updates; do not download")
	noCheck := fs.Bool("no-check", false, "Skip the version check; use pending updates already in the DB")
	fs.Parse(hoistFlags(args, autoUpdateValueFlags))

	cookie := ResolveCookie(*cookieStr, *cookieFile)

	database := OpenDB()
	defer database.Close()
	prepareDownloader(database)

	release, err := acquireAutoUpdateLock()
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ %v\n", err)
		os.Exit(1)
	}
	defer release()
	// os.Exit skips defers, so release explicitly on the exits below; SIGINT
	// must also release the lock or the next run waits out the stale window.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		if _, ok := <-sigCh; ok {
			release()
			os.Exit(1)
		}
	}()

	targetPlatform := *platform
	if targetPlatform == "" {
		targetPlatform = string(downloader.CurrentPlatform())
	}

	client := scraper.NewClient(cookie)
	if *unsafe {
		client = scraper.NewUnsafeClient(cookie)
	}
	public := scraper.NewPublicAPIWithCookie(cookie)

	if !*checkOnly && !*noCheck {
		trackable, err := database.GamesWithF95URL()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			release()
			os.Exit(1)
		}
		if len(trackable) > 0 {
			if _, err := RunUpdateCheck(database, client, trackable, *force, public); err != nil {
				log.Warn("update check failed", "error", err)
			}
		}
	}

	pending, err := database.GamesNeedingUpdate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		release()
		os.Exit(1)
	}

	if *checkOnly {
		fmt.Fprintf(os.Stderr, "\n=== %d updates available ===\n", len(pending))
		for i := range pending {
			fmt.Fprintf(os.Stderr, "  %s: %s → %s\n", pending[i].Title, pending[i].Version, pending[i].LatestVersion)
		}
		return
	}

	maxPerRun := *max
	if maxPerRun == 0 {
		maxPerRun = autoUpdateMaxPerRun()
	}
	summary := runAutoUpdate(database, pending, autoUpdateOptions{
		Cookie:   cookie,
		Client:   client,
		Platform: targetPlatform,
		Extract:  !*noExtract,
		Max:      maxPerRun,
		DryRun:   *dryRun,
		Force:    *force,
	})
	printAutoUpdateSummary(summary)
}

// runAutoUpdate installs updates for games already known to have one. It
// returns a summary and never calls os.Exit, so sync/check-updates can embed
// it.
func runAutoUpdate(database *db.Database, games []db.Game, opts autoUpdateOptions) autoUpdateSummary {
	summary := autoUpdateSummary{}
	state := loadAutoUpdateState()

	max := opts.Max
	if max <= 0 || max > len(games) {
		max = len(games)
	}

	for i := range games {
		if summary.Candidates >= max {
			break
		}
		game := &games[i]
		if !version.NeedsUpdate(game.LatestVersion, game.Version) {
			continue
		}
		summary.Candidates++

		// Only an unambiguously newer version is installed automatically. A
		// "changed" ordering means the two strings cannot be compared, which
		// is a person's decision, not a reason to replace a working install.
		if !version.IsNewer(game.LatestVersion, game.Version) {
			summary.Ambiguous++
			fmt.Fprintf(os.Stderr, "  – %s: %s → %s (ordering unclear; install manually)\n",
				game.Title, game.Version, game.LatestVersion)
			continue
		}

		if !opts.Force && recentlyAttempted(state, game.ID, game.LatestVersion) {
			summary.Skipped++
			fmt.Fprintf(os.Stderr, "  – %s: %s → %s (retried recently; use --force)\n",
				game.Title, game.Version, game.LatestVersion)
			continue
		}

		if opts.DryRun {
			fmt.Fprintf(os.Stderr, "  → would update %s: %s → %s\n", game.Title, game.Version, game.LatestVersion)
			continue
		}

		fmt.Fprintf(os.Stderr, "\n[%d/%d] %s: %s → %s\n",
			summary.Candidates, max, game.Title, game.Version, game.LatestVersion)

		err := downloadSingle(database, game, opts.Cookie, "", opts.Platform, opts.Extract, opts.Client)
		status := updateInstalled
		switch {
		case err == nil:
			summary.Installed++
			fmt.Fprintf(os.Stderr, "\n  ✓ Updated %s to %s\n", game.Title, game.Version)
		case isCaptchaError(err):
			status = updateNeedsVerification
			summary.NeedsHuman++
			fmt.Fprintf(os.Stderr, "\n  ! %s still needs a browser check\n", game.Title)
		default:
			status = updateFailed
			summary.Failed++
			fmt.Fprintf(os.Stderr, "\n  ✗ %s: %v\n", game.Title, err)
		}
		recordAutoUpdateAttempt(state, game, status, err)
	}

	saveAutoUpdateState(state)
	return summary
}

// updateOutcome labels recorded in the retry state.
const (
	updateInstalled         = "installed"
	updateNeedsVerification = "needs_verification"
	updateFailed            = "failed"
)

// isCaptchaError reports whether the failure is a human-verification gate.
// Downloader errors from the browser path are wrapped, so match on the text
// the resolver uses.
func isCaptchaError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "captcha") ||
		strings.Contains(msg, "human verification") ||
		strings.Contains(msg, "browser check") ||
		strings.Contains(msg, "challenge") ||
		strings.Contains(msg, "browser fallback")
}

// printAutoUpdateSummary reports the run's outcome.
func printAutoUpdateSummary(s autoUpdateSummary) {
	fmt.Fprintf(os.Stderr, "\n=== Auto-update: %d installed, %d need a browser check, %d failed, %d without links ===\n",
		s.Installed, s.NeedsHuman, s.Failed, s.NoLinks)
	if s.Skipped > 0 {
		fmt.Fprintf(os.Stderr, "    %d skipped (recently attempted)\n", s.Skipped)
	}
	if s.Ambiguous > 0 {
		fmt.Fprintf(os.Stderr, "    %d need review (version ordering unclear)\n", s.Ambiguous)
	}
}

// runAutoUpdateIfEnabled drives auto-update after a check when the user
// enabled it in config (or asked for it on this command). A clean library
// produces no output.
func runAutoUpdateIfEnabled(database *db.Database, cookie string, client *scraper.Client, opts autoUpdateOptions, explicit bool) {
	if !explicit && !autoUpdateEnabled() {
		return
	}
	pending, err := database.GamesNeedingUpdate()
	if err != nil {
		log.Warn("auto-update: could not list pending updates", "error", err)
		return
	}
	if len(pending) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "\n=== Auto-update: attempting %d pending update(s) ===\n", len(pending))
	opts.Cookie = cookie
	opts.Client = client
	if opts.Max == 0 {
		opts.Max = autoUpdateMaxPerRun()
	}
	if opts.Platform == "" {
		opts.Platform = string(downloader.CurrentPlatform())
	}
	summary := runAutoUpdate(database, pending, opts)
	printAutoUpdateSummary(summary)
}

// ---------------------------------------------------------------------------
// Lock
// ---------------------------------------------------------------------------

func acquireAutoUpdateLock() (func(), error) {
	path := autoUpdateLockPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create config directory: %w", err)
	}
	acquire := func() (*os.File, error) {
		return os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	}
	f, err := acquire()
	if err != nil && os.IsExist(err) {
		// A crash leaves the lock behind. Break a stale one, never a fresh one.
		if fi, statErr := os.Stat(path); statErr == nil && time.Since(fi.ModTime()) > 2*time.Hour {
			os.Remove(path)
			f, err = acquire()
		}
	}
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("another auto-update is already running (lock: %s)", path)
		}
		return nil, fmt.Errorf("create auto-update lock: %w", err)
	}
	fmt.Fprintf(f, "%d\n", os.Getpid())
	f.Close()
	return func() { os.Remove(path) }, nil
}

// ---------------------------------------------------------------------------
// Retry state
// ---------------------------------------------------------------------------

func loadAutoUpdateState() *autoUpdateState {
	s := &autoUpdateState{Games: make(map[string]autoUpdateAttempt)}
	data, err := os.ReadFile(autoUpdateStatePath())
	if err != nil {
		return s
	}
	if err := json.Unmarshal(data, s); err != nil || s.Games == nil {
		return &autoUpdateState{Games: make(map[string]autoUpdateAttempt)}
	}
	return s
}

func saveAutoUpdateState(s *autoUpdateState) {
	if s == nil {
		return
	}
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	for id, attempt := range s.Games {
		if attempt.LastAttempt.Before(cutoff) {
			delete(s.Games, id)
		}
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	path := autoUpdateStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		log.Warn("could not create config directory for auto-update state", "error", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		log.Warn("could not save auto-update state", "error", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		log.Warn("could not replace auto-update state", "error", err)
	}
}

func recentlyAttempted(s *autoUpdateState, gameID int64, targetVersion string) bool {
	if s == nil {
		return false
	}
	attempt, ok := s.Games[strconv.FormatInt(gameID, 10)]
	if !ok || attempt.Status == updateInstalled {
		return false
	}
	if attempt.TargetVersion != "" && targetVersion != "" && attempt.TargetVersion != targetVersion {
		// A newer release appeared; try again even inside the backoff window.
		return false
	}
	return time.Since(attempt.LastAttempt) < autoUpdateRetryAfter
}

func recordAutoUpdateAttempt(s *autoUpdateState, game *db.Game, status string, err error) {
	if s == nil {
		return
	}
	key := strconv.FormatInt(game.ID, 10)
	prev := s.Games[key]
	attempt := autoUpdateAttempt{
		LastAttempt:   time.Now(),
		Attempts:      prev.Attempts + 1,
		Status:        status,
		TargetVersion: game.LatestVersion,
	}
	if err != nil {
		attempt.LastError = err.Error()
	}
	s.Games[key] = attempt
}

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

func autoUpdateEnabled() bool {
	cfg, err := config.ReadConfig()
	if err != nil {
		return false
	}
	return parseConfigBool(cfg.Get("auto_update"))
}

func autoUpdateMaxPerRun() int {
	cfg, err := config.ReadConfig()
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(cfg.Get("auto_update_max")))
	if n < 0 {
		return 0
	}
	return n
}

func parseConfigBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on", "enabled":
		return true
	}
	return false
}
