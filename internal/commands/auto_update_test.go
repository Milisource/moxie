package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mili/moxie/internal/config"
	"github.com/mili/moxie/internal/db"
)

func TestParseConfigBool(t *testing.T) {
	t.Parallel()
	truthy := []string{"1", "true", "TRUE", "yes", "on", "enabled", " true "}
	for _, v := range truthy {
		if !parseConfigBool(v) {
			t.Errorf("parseConfigBool(%q) = false, want true", v)
		}
	}
	falsy := []string{"", "0", "false", "no", "off", "disabled", "banana"}
	for _, v := range falsy {
		if parseConfigBool(v) {
			t.Errorf("parseConfigBool(%q) = true, want false", v)
		}
	}
}

func TestAutoUpdateBackoff(t *testing.T) {
	t.Parallel()
	state := &autoUpdateState{Games: map[string]autoUpdateAttempt{
		"1": {LastAttempt: time.Now().Add(-time.Minute), Status: updateNeedsVerification, TargetVersion: "1.1"},
		"2": {LastAttempt: time.Now().Add(-time.Hour), Status: updateFailed, TargetVersion: "2.0"},
		"3": {LastAttempt: time.Now().Add(-time.Minute), Status: updateInstalled, TargetVersion: "3.1"},
	}}

	if !recentlyAttempted(state, 1, "1.1") {
		t.Error("a captcha stop one minute ago should be backing off")
	}
	if recentlyAttempted(state, 2, "2.0") {
		t.Error("a failure an hour ago should be retried")
	}
	if recentlyAttempted(state, 3, "3.1") {
		t.Error("a successful install must not block a later release")
	}
	if recentlyAttempted(state, 1, "1.2") {
		t.Error("a different target version should be attempted immediately")
	}
	if recentlyAttempted(state, 99, "1.0") {
		t.Error("an unknown game has no backoff")
	}
}

func TestRecordAutoUpdateAttempt(t *testing.T) {
	t.Parallel()
	state := &autoUpdateState{Games: map[string]autoUpdateAttempt{}}
	game := &db.Game{ID: 7, LatestVersion: "2.0"}

	recordAutoUpdateAttempt(state, game, updateFailed, errors.New("boom"))
	first := state.Games["7"]
	if first.Attempts != 1 || first.Status != updateFailed || first.LastError != "boom" {
		t.Fatalf("unexpected first attempt: %+v", first)
	}

	recordAutoUpdateAttempt(state, game, updateInstalled, nil)
	second := state.Games["7"]
	if second.Attempts != 2 || second.Status != updateInstalled || second.LastError != "" {
		t.Fatalf("unexpected second attempt: %+v", second)
	}
}

func TestIsCaptchaError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("captcha required"), true},
		{errors.New("f95zone now gates every download link behind a human hCaptcha check"), true},
		{errors.New("challenge page detected"), true},
		{errors.New("browser fallback also failed: timeout"), true},
		{errors.New("HTTP 404"), false},
	}
	for _, c := range cases {
		if got := isCaptchaError(c.err); got != c.want {
			t.Errorf("isCaptchaError(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

func TestRunAutoUpdate_SelectionAndAmbiguity(t *testing.T) {
	// Not parallel: runAutoUpdate writes retry state under the config dir.
	t.Setenv("HOME", t.TempDir())
	database := setupTestDB(t)
	defer database.Close()

	games := []db.Game{
		{Title: "Newer", Engine: "RenPy", Path: "/games/newer", Status: "active",
			Version: "1.0", LatestVersion: "1.1"},
		{Title: "Ambiguous", Engine: "RenPy", Path: "/games/ambiguous", Status: "active",
			Version: "1.0", LatestVersion: "2026-10-01"},
		{Title: "Reverted", Engine: "RenPy", Path: "/games/reverted", Status: "active",
			Version: "1.2", LatestVersion: "1.1"},
	}
	for i := range games {
		if _, err := database.InsertGame(&games[i]); err != nil {
			t.Fatal(err)
		}
	}

	summary := runAutoUpdate(database, games, autoUpdateOptions{DryRun: true, Extract: true, Concurrency: 1})
	if summary.Candidates != 1 {
		t.Errorf("Candidates = %d, want 1 (only the installable game)", summary.Candidates)
	}
	if summary.Ambiguous != 1 {
		t.Errorf("Ambiguous = %d, want 1", summary.Ambiguous)
	}
	if summary.Installed != 0 {
		t.Errorf("dry run installed %d games", summary.Installed)
	}
}

func TestClampAutoUpdateConcurrency(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want int
	}{
		{"", 2},       // unset
		{"banana", 2}, // invalid
		{"0", 2},      // zero means unset
		{"-3", 1},     // below range
		{"1", 1},      // minimum
		{"3", 3},      // in range
		{"4", 4},      // maximum
		{"99", 4},     // above range
	}
	for _, c := range cases {
		if got := clampAutoUpdateConcurrency(c.in); got != c.want {
			t.Errorf("clampAutoUpdateConcurrency(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestAutoUpdateConcurrencyConfigAndFlag(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// No config file: the desktop default.
	if got := autoUpdateConcurrencyOption(0); got != defaultAutoUpdateConcurrency {
		t.Errorf("default concurrency = %d, want %d", got, defaultAutoUpdateConcurrency)
	}

	writeCommandConfig(t, `{"update-concurrency":"3"}`)
	if got := autoUpdateConcurrency(); got != 3 {
		t.Errorf("configured concurrency = %d, want 3", got)
	}
	if got := autoUpdateConcurrencyOption(0); got != 3 {
		t.Errorf("flag 0 should use config: got %d, want 3", got)
	}

	// The flag overrides the config for one run, clamped like the config value.
	if got := autoUpdateConcurrencyOption(1); got != 1 {
		t.Errorf("--concurrency 1 = %d, want 1", got)
	}
	if got := autoUpdateConcurrencyOption(9); got != maxAutoUpdateConcurrency {
		t.Errorf("--concurrency 9 = %d, want %d", got, maxAutoUpdateConcurrency)
	}
	if got := autoUpdateConcurrencyOption(-2); got != 1 {
		t.Errorf("--concurrency -2 = %d, want 1", got)
	}
}

func TestAutoUpdateDenominatorCountsInstallable(t *testing.T) {
	t.Parallel()
	games := []db.Game{
		{Version: "1.0", LatestVersion: "1.1"},        // installable
		{Version: "1.0", LatestVersion: "2026-10-01"}, // ambiguous
		{Version: "1.2", LatestVersion: "1.1"},        // reverted: not a candidate
		{Version: "", LatestVersion: "9.9"},           // unknown current: not a candidate
		{Version: "2.0", LatestVersion: "2.1"},        // installable
	}

	got, limited := autoUpdateDenominator(games, 0)
	if got != 2 || limited {
		t.Errorf("unlimited denominator = (%d, %v), want (2, false)", got, limited)
	}
	got, limited = autoUpdateDenominator(games, 1)
	if got != 1 || !limited {
		t.Errorf("--max 1 denominator = (%d, %v), want (1, true)", got, limited)
	}
	got, limited = autoUpdateDenominator(games, 5)
	if got != 2 || limited {
		t.Errorf("--max above count denominator = (%d, %v), want (2, false)", got, limited)
	}
}

func TestRunAutoUpdateMaxCountsInstallableOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	database := setupTestDB(t)
	defer database.Close()

	// The ambiguous game comes first so it is still reported; it must not
	// consume the single install slot.
	games := []db.Game{
		{Title: "Ambiguous", Engine: "RenPy", Path: "/games/ambiguous", Status: "active",
			Version: "1.0", LatestVersion: "2026-10-01"},
		{Title: "First", Engine: "RenPy", Path: "/games/first", Status: "active",
			Version: "1.0", LatestVersion: "1.1"},
		{Title: "Second", Engine: "RenPy", Path: "/games/second", Status: "active",
			Version: "1.0", LatestVersion: "1.1"},
	}
	for i := range games {
		if _, err := database.InsertGame(&games[i]); err != nil {
			t.Fatal(err)
		}
	}

	var calls atomic.Int32
	var installedTitle string
	summary := runAutoUpdate(database, games, autoUpdateOptions{
		Extract:     true,
		Max:         1,
		Concurrency: 1,
		install: func(game *db.Game) error {
			calls.Add(1)
			installedTitle = game.Title
			return nil
		},
	})

	if calls.Load() != 1 {
		t.Errorf("installed %d games, want 1", calls.Load())
	}
	if installedTitle != "First" {
		t.Errorf("installed %q, want the first installable game", installedTitle)
	}
	if summary.Candidates != 1 {
		t.Errorf("Candidates = %d, want 1", summary.Candidates)
	}
	if summary.Ambiguous != 1 {
		t.Errorf("Ambiguous = %d, want 1 (reported without consuming --max)", summary.Ambiguous)
	}
	if summary.Installed != 1 {
		t.Errorf("Installed = %d, want 1", summary.Installed)
	}
}

func TestRunAutoUpdateParallelSummary(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	database := setupTestDB(t)
	defer database.Close()

	const n = 8
	games := make([]db.Game, n)
	for i := range games {
		games[i] = db.Game{
			Title:         fmt.Sprintf("Game %d", i),
			Engine:        "RenPy",
			Path:          fmt.Sprintf("/games/%d", i),
			Status:        "active",
			Version:       "1.0",
			LatestVersion: "1.1",
		}
		if _, err := database.InsertGame(&games[i]); err != nil {
			t.Fatal(err)
		}
	}

	var (
		mu       sync.Mutex
		seen     = make(map[int64]int, n)
		inFlight atomic.Int32
		peak     atomic.Int32
	)
	install := func(game *db.Game) error {
		cur := inFlight.Add(1)
		for {
			old := peak.Load()
			if cur <= old || peak.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
		inFlight.Add(-1)

		mu.Lock()
		seen[game.ID]++
		mu.Unlock()

		if game.ID%2 == 0 {
			return errors.New("mirror down")
		}
		return nil
	}

	summary := runAutoUpdate(database, games, autoUpdateOptions{
		Extract:     true,
		Concurrency: 3,
		install:     install,
	})

	if summary.Installed+summary.Failed != n {
		t.Errorf("counters = %d installed + %d failed, want %d total",
			summary.Installed, summary.Failed, n)
	}
	if summary.Installed != n/2 || summary.Failed != n/2 {
		t.Errorf("Installed = %d, Failed = %d, want %d each",
			summary.Installed, summary.Failed, n/2)
	}
	if got := peak.Load(); got > 3 {
		t.Errorf("peak in-flight downloads = %d, want <= 3", got)
	}

	mu.Lock()
	if len(seen) != n {
		t.Errorf("worked on %d distinct games, want %d", len(seen), n)
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("game %d worked on %d times, want once", id, count)
		}
	}
	mu.Unlock()

	state := loadAutoUpdateState()
	if len(state.Games) != n {
		t.Fatalf("retry state has %d games, want %d", len(state.Games), n)
	}
	for i := range games {
		attempt, ok := state.Games[strconv.FormatInt(games[i].ID, 10)]
		if !ok {
			t.Errorf("game %d missing from retry state", games[i].ID)
			continue
		}
		want := updateInstalled
		if games[i].ID%2 == 0 {
			want = updateFailed
		}
		if attempt.Status != want {
			t.Errorf("game %d status = %q, want %q", games[i].ID, attempt.Status, want)
		}
	}
}

func TestRunAutoUpdateJobsBounded(t *testing.T) {
	t.Parallel()
	const (
		total       = 12
		concurrency = 3
	)
	jobs := make([]autoUpdateJob, total)
	for i := range jobs {
		jobs[i] = autoUpdateJob{game: &db.Game{ID: int64(i + 1)}, index: i + 1}
	}

	// Every worker blocks until `concurrency` of them are in flight, so a
	// pool that ran the batch serially would deadlock into the timeout.
	full := make(chan struct{})
	var closeOnce sync.Once
	var inFlight, peak, done atomic.Int32

	runAutoUpdateJobs(jobs, concurrency, func(autoUpdateJob) {
		cur := inFlight.Add(1)
		for {
			old := peak.Load()
			if cur <= old || peak.CompareAndSwap(old, cur) {
				break
			}
		}
		if cur == concurrency {
			closeOnce.Do(func() { close(full) })
		}
		select {
		case <-full:
		case <-time.After(5 * time.Second):
			t.Error("worker slots never filled in parallel")
			return
		}
		inFlight.Add(-1)
		done.Add(1)
	})

	if done.Load() != total {
		t.Errorf("completed %d jobs, want %d", done.Load(), total)
	}
	if got := peak.Load(); got != concurrency {
		t.Errorf("peak in-flight jobs = %d, want %d", got, concurrency)
	}
}

func writeCommandConfig(t *testing.T, contents string) {
	t.Helper()
	if err := os.MkdirAll(config.ConfigDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ConfigPath(), []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestAcquireAutoUpdateLock(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	release, err := acquireAutoUpdateLock()
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if _, err := acquireAutoUpdateLock(); err == nil {
		t.Fatal("second acquire should fail while the lock is held")
	}
	release()
	release2, err := acquireAutoUpdateLock()
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	release2()
}

func TestRunAutoUpdate_CancelStopsNewJobs(t *testing.T) {
	for _, concurrency := range []int{1, 3} {
		t.Run(strconv.Itoa(concurrency), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var started, finished atomic.Int32
			games := make([]db.Game, 8)
			for i := range games {
				games[i] = db.Game{ID: int64(i + 1), Title: fmt.Sprintf("Game %d", i), Version: "1.0", LatestVersion: "2.0"}
			}
			summary := runAutoUpdate(nil, games, autoUpdateOptions{
				Ctx: ctx, Concurrency: concurrency, Force: true,
				install: func(*db.Game) error {
					started.Add(1)
					cancel()
					// Cancellation does not interrupt the in-flight install/merge.
					finished.Add(1)
					return nil
				},
			})
			if got := started.Load(); got < 1 || got > int32(concurrency) {
				t.Fatalf("started %d jobs after cancellation; want 1..%d", got, concurrency)
			}
			if finished.Load() != started.Load() || summary.Installed != int(finished.Load()) {
				t.Fatalf("in-flight jobs not accounted for: started=%d finished=%d summary=%+v", started.Load(), finished.Load(), summary)
			}
		})
	}
}

func TestRunAutoUpdateJobsContext_PreCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, concurrency := range []int{1, 3} {
		runAutoUpdateJobsContext(ctx, []autoUpdateJob{{}, {}, {}}, concurrency, func(autoUpdateJob) { t.Error("scheduled a job after cancellation") })
	}
}

func TestAutoUpdateExitCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		summary   autoUpdateSummary
		checkOnly bool
		dryRun    bool
		want      int
	}{
		{name: "empty success", want: 0},
		{name: "installed success", summary: autoUpdateSummary{Installed: 2}, want: 0},
		{name: "partial failure", summary: autoUpdateSummary{Installed: 1, Failed: 1}, want: 2},
		{name: "all failed", summary: autoUpdateSummary{Failed: 2}, want: 2},
		{name: "check only", summary: autoUpdateSummary{Failed: 1}, checkOnly: true, want: 0},
		{name: "dry run", summary: autoUpdateSummary{Failed: 1}, dryRun: true, want: 0},
		{name: "manual or skipped", summary: autoUpdateSummary{NeedsHuman: 1, Skipped: 1, Ambiguous: 1}, want: 2},
		{name: "needs human with an install", summary: autoUpdateSummary{Installed: 1, NeedsHuman: 1}, want: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := autoUpdateExitCode(tc.summary, tc.checkOnly, tc.dryRun); got != tc.want {
				t.Fatalf("autoUpdateExitCode(%+v, %v, %v) = %d, want %d", tc.summary, tc.checkOnly, tc.dryRun, got, tc.want)
			}
		})
	}
}
