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
	"sync"
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
	Cookie      string
	Platform    string
	Extract     bool
	Max         int // 0 = no limit
	DryRun      bool
	Force       bool // ignore the per-game retry backoff
	Concurrency int  // 0 = read the update-concurrency config key
	Client      *scraper.Client

	// ExcludeHosts are skipped when ranking mirrors, for a rate-limited host
	// nothing else can detect (a slow transfer is not an error).
	ExcludeHosts map[string]bool

	// install overrides the download+install step in tests. Production leaves
	// it nil and downloadSingle runs.
	install func(game *db.Game) error
}

// autoUpdateSummary reports what a run did.
type autoUpdateSummary struct {
	Candidates int // installable games considered (ambiguous ones excluded)
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

// autoUpdateParseFlags is autoUpdateValueFlags plus --concurrency, which this
// file adds; hoistFlags needs to know the flag takes a value so the value
// stays next to it when flags and positionals are reordered.
var autoUpdateParseFlags = func() map[string]bool {
	m := make(map[string]bool, len(autoUpdateValueFlags)+1)
	for k, v := range autoUpdateValueFlags {
		m[k] = v
	}
	m["--concurrency"] = true
	return m
}()

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
	concurrencyFlag := fs.Int("concurrency", 0, "Maximum games to update at once (1-4; 0 = update-concurrency config)")
	excludeHost := fs.String("exclude-host", "", "Comma-separated hosts to skip (e.g. pixeldrain when rate-limited)")
	fs.Parse(hoistFlags(args, autoUpdateParseFlags))

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
		Cookie:       cookie,
		Client:       client,
		Platform:     targetPlatform,
		Extract:      !*noExtract,
		Max:          maxPerRun,
		DryRun:       *dryRun,
		Force:        *force,
		Concurrency:  autoUpdateConcurrencyOption(*concurrencyFlag),
		ExcludeHosts: parseHostExclusions(*excludeHost),
	})
	printAutoUpdateSummary(summary)
}

// autoUpdateJob is one installable game and its 1-based position in the run.
type autoUpdateJob struct {
	game  *db.Game
	index int
}

// autoUpdateStep is one line of a run's plan: either a note printed as the
// batch is classified (ambiguous version, retry backoff, dry run) or an
// install job. A single-game run executes steps in order, which keeps its
// output identical to the pre-parallel loop.
type autoUpdateStep struct {
	note string
	job  *autoUpdateJob
}

// runAutoUpdate installs updates for games already known to have one. It
// returns a summary and never calls os.Exit, so sync/check-updates can embed
// it. Games run in parallel up to opts.Concurrency (the update-concurrency
// config key when unset).
func runAutoUpdate(database *db.Database, games []db.Game, opts autoUpdateOptions) autoUpdateSummary {
	summary := autoUpdateSummary{}
	state := loadAutoUpdateState()

	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = autoUpdateConcurrency()
	} else {
		concurrency = clampAutoUpdateConcurrency(strconv.Itoa(concurrency))
	}

	// Only an unambiguously newer version can be installed, so only those
	// games count toward the progress denominator and --max. Ambiguous
	// ("changed") versions are reported, never budgeted.
	denominator, limited := autoUpdateDenominator(games, opts.Max)

	// Parallel downloads would write \r frames from several games into the
	// same terminal; keep the per-line pipeline output instead.
	prevProgress := progressBarEnabled.Load()
	progressBarEnabled.Store(prevProgress && concurrency <= 1)
	defer progressBarEnabled.Store(prevProgress)

	steps := make([]autoUpdateStep, 0, len(games))
	index := 0
	for i := range games {
		if limited && index >= denominator {
			break
		}
		game := &games[i]
		if !version.NeedsUpdate(game.LatestVersion, game.Version) {
			continue
		}

		// Only an unambiguously newer version is installed automatically. A
		// "changed" ordering means the two strings cannot be compared, which
		// is a person's decision, not a reason to replace a working install.
		if !version.IsNewer(game.LatestVersion, game.Version) {
			summary.Ambiguous++
			steps = append(steps, autoUpdateStep{note: fmt.Sprintf(
				"  – %s: %s → %s (ordering unclear; install manually)\n",
				game.Title, game.Version, game.LatestVersion)})
			continue
		}

		index++
		if !opts.Force && recentlyAttempted(state, game.ID, game.LatestVersion) {
			summary.Skipped++
			steps = append(steps, autoUpdateStep{note: fmt.Sprintf(
				"  – %s: %s → %s (retried recently; use --force)\n",
				game.Title, game.Version, game.LatestVersion)})
			continue
		}

		if opts.DryRun {
			steps = append(steps, autoUpdateStep{note: fmt.Sprintf(
				"  → would update %s: %s → %s\n",
				game.Title, game.Version, game.LatestVersion)})
			continue
		}

		steps = append(steps, autoUpdateStep{job: &autoUpdateJob{game: game, index: index}})
	}
	summary.Candidates = index

	install := opts.install
	if install == nil {
		install = func(game *db.Game) error {
			return downloadSingle(database, game, opts.Cookie, "", opts.Platform, opts.Extract, opts.Client, opts.ExcludeHosts)
		}
	}

	// Summary counters, retry-state mutations and console lines are shared by
	// every worker, so they are serialised. The download itself runs outside
	// the lock.
	var mu sync.Mutex
	runJob := func(job autoUpdateJob) {
		game := job.game
		mu.Lock()
		fmt.Fprintf(os.Stderr, "\n[%d/%d] %s: %s → %s\n",
			job.index, denominator, game.Title, game.Version, game.LatestVersion)
		mu.Unlock()

		err := install(game)

		mu.Lock()
		defer mu.Unlock()
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

	if concurrency <= 1 {
		for _, step := range steps {
			if step.job == nil {
				fmt.Fprint(os.Stderr, step.note)
				continue
			}
			runJob(*step.job)
		}
	} else {
		jobs := make([]autoUpdateJob, 0, len(steps))
		for _, step := range steps {
			if step.job == nil {
				fmt.Fprint(os.Stderr, step.note)
				continue
			}
			jobs = append(jobs, *step.job)
		}
		runAutoUpdateJobs(jobs, concurrency, runJob)
	}

	saveAutoUpdateState(state)
	return summary
}

// autoUpdateDenominator returns the progress denominator: the number of
// installable games in the batch, capped by max. limited reports whether max
// actually cuts the run short, so an uncapped run still reports every
// ambiguous version note.
func autoUpdateDenominator(games []db.Game, max int) (denominator int, limited bool) {
	installable := 0
	for i := range games {
		if isInstallableUpdate(&games[i]) {
			installable++
		}
	}
	if max > 0 && max < installable {
		return max, true
	}
	return installable, false
}

// isInstallableUpdate reports whether a game may be installed without human
// judgement: the thread has a newer version and the two versions compare
// unambiguously.
func isInstallableUpdate(game *db.Game) bool {
	return version.NeedsUpdate(game.LatestVersion, game.Version) &&
		version.IsNewer(game.LatestVersion, game.Version)
}

// runAutoUpdateJobs calls work for every job with at most concurrency in
// flight, then returns once all of them finish. work must be safe to call
// concurrently.
func runAutoUpdateJobs(jobs []autoUpdateJob, concurrency int, work func(autoUpdateJob)) {
	if len(jobs) == 0 {
		return
	}
	if concurrency > len(jobs) {
		concurrency = len(jobs)
	}
	if concurrency <= 1 {
		for _, job := range jobs {
			work(job)
		}
		return
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for _, job := range jobs {
		wg.Add(1)
		go func(job autoUpdateJob) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			work(job)
		}(job)
	}
	wg.Wait()
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

// lockStale decides whether an existing lock file may be broken. A lock
// whose recorded PID no longer exists is stale immediately: that is the
// common case (a killed process, or a run whose stdout pipe closed mid-way).
// A lock without a readable PID falls back to the two-hour age window.
func lockStale(path string) bool {
	if data, err := os.ReadFile(path); err == nil {
		if pid, perr := strconv.Atoi(strings.TrimSpace(string(data))); perr == nil {
			return !processAlive(pid)
		}
	}
	fi, err := os.Stat(path)
	return err == nil && time.Since(fi.ModTime()) > 2*time.Hour
}

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
		if lockStale(path) {
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

// Update concurrency bounds, mirroring the desktop's "update-concurrency"
// key (desktop/update_runs.go). The CLI re-declares the tiny clamp rather
// than importing package main; keep the two in step.
const (
	defaultAutoUpdateConcurrency = 2
	maxAutoUpdateConcurrency     = 4
	autoUpdateConcurrencyKey     = "update-concurrency"
)

// clampAutoUpdateConcurrency parses the config value, falling back to the
// default for empty/invalid input and clamping to 1..maxAutoUpdateConcurrency.
func clampAutoUpdateConcurrency(v string) int {
	n, err := strconv.Atoi(v)
	if err != nil || n == 0 {
		return defaultAutoUpdateConcurrency
	}
	if n < 1 {
		return 1
	}
	if n > maxAutoUpdateConcurrency {
		return maxAutoUpdateConcurrency
	}
	return n
}

// autoUpdateConcurrency reads the configured parallelism for game updates.
func autoUpdateConcurrency() int {
	cfg, err := config.ReadConfig()
	if err != nil {
		return defaultAutoUpdateConcurrency
	}
	return clampAutoUpdateConcurrency(cfg.Get(autoUpdateConcurrencyKey))
}

// autoUpdateConcurrencyOption resolves the --concurrency flag for one run:
// 0 means "use the configured value".
func autoUpdateConcurrencyOption(flagValue int) int {
	if flagValue == 0 {
		return autoUpdateConcurrency()
	}
	return clampAutoUpdateConcurrency(strconv.Itoa(flagValue))
}

func parseConfigBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on", "enabled":
		return true
	}
	return false
}
