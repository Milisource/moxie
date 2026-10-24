package commands

import (
	"errors"
	"testing"
	"time"

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

	summary := runAutoUpdate(database, games, autoUpdateOptions{DryRun: true, Extract: true})
	if summary.Candidates != 2 {
		t.Errorf("Candidates = %d, want 2 (newer + ambiguous)", summary.Candidates)
	}
	if summary.Ambiguous != 1 {
		t.Errorf("Ambiguous = %d, want 1", summary.Ambiguous)
	}
	if summary.Installed != 0 {
		t.Errorf("dry run installed %d games", summary.Installed)
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
