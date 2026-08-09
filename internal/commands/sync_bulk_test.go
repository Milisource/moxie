package commands

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/scraper"
)

// ---------------------------------------------------------------------------
// RunUpdateCheck — cookie-free bulk version path
// ---------------------------------------------------------------------------

// newBulkTestAPI wires a PublicAPI to a single httptest server that routes by
// URL path: /sam/checker.php → bulk versions, /fast and /full → cache API.
// Returns the API and the handler state for assertions.
func newBulkTestAPI(t *testing.T) (*scraper.PublicAPI, *bulkTestState) {
	t.Helper()
	state := &bulkTestState{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "checker.php"):
			state.bulkCalls.Add(1)
			fmt.Fprint(w, state.bulkResponse)
		case strings.HasSuffix(r.URL.Path, "/fast"):
			state.fastCalls.Add(1)
			fmt.Fprint(w, state.fastResponse)
		case strings.Contains(r.URL.Path, "/full/"):
			state.fullCalls.Add(1)
			fmt.Fprint(w, state.fullResponse)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	api := scraper.NewPublicAPI()
	api.Host = srv.URL
	api.CacheHost = srv.URL
	return api, state
}

type bulkTestState struct {
	bulkResponse string
	fastResponse string
	fullResponse string
	bulkCalls    atomic.Int64
	fastCalls    atomic.Int64
	fullCalls    atomic.Int64
}

// TestRunUpdateCheck_BulkPath: games with thread IDs are version-checked via
// checker.php in a single request — newer versions are reported as updates
// and persisted, matching versions are not.
func TestRunUpdateCheck_BulkPath(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	api, state := newBulkTestAPI(t)
	state.bulkResponse = `{"status":"ok","msg":{"100":"v2.0","200":"v1.0","300":"Unknown"}}`

	older := &db.Game{Title: "Older Game", Engine: "RenPy", Path: "/older",
		F95URL: "https://f95zone.to/threads/x.100/", F95ThreadID: 100,
		Version: "v1.0", LatestVersion: "v1.0"}
	same := &db.Game{Title: "Same Game", Engine: "Unity", Path: "/same",
		F95URL: "https://f95zone.to/threads/x.200/", F95ThreadID: 200,
		Version: "v1.0", LatestVersion: "v1.0"}
	for _, g := range []*db.Game{older, same} {
		if _, err := database.InsertGame(g); err != nil {
			t.Fatal(err)
		}
	}

	// force=true bypasses the cooldown so the bulk path runs.
	updatesFound, results := RunUpdateCheck(database, nil, []db.Game{*older, *same}, true, api)

	if updatesFound != 1 {
		t.Errorf("expected 1 update, got %d", updatesFound)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if !results[0].IsNew || results[0].Latest != "v2.0" {
		t.Errorf("first result = %+v, want update to v2.0", results[0])
	}
	if results[1].IsNew {
		t.Errorf("second result should not be an update: %+v", results[1])
	}
	if state.bulkCalls.Load() != 1 {
		t.Errorf("expected exactly 1 bulk request, got %d", state.bulkCalls.Load())
	}

	// DB must have been updated with the new version.
	got, err := database.GetGame(older.ID)
	if err != nil || got == nil {
		t.Fatalf("failed to reload game: %v", err)
	}
	if got.LatestVersion != "v2.0" {
		t.Errorf("persisted LatestVersion = %q, want v2.0", got.LatestVersion)
	}
}

// TestRunUpdateCheck_BulkUntrackedViaCache: threads missing from checker.php
// are resolved through the cache API instead of direct scraping — still
// cookie-free.
func TestRunUpdateCheck_BulkUntrackedViaCache(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	api, state := newBulkTestAPI(t)
	state.bulkResponse = `{"status":"ok","msg":{"100":"v9.0"}}`
	state.fullResponse = `{"name":"Untracked Game","version":"v3.0","status":2,"developer":"DevCo"}`

	untracked := &db.Game{Title: "Untracked Game", Engine: "RenPy", Path: "/untracked",
		F95URL: "https://f95zone.to/threads/x.999/", F95ThreadID: 999,
		Version: "v2.0", LatestVersion: "v2.0"}
	if _, err := database.InsertGame(untracked); err != nil {
		t.Fatal(err)
	}

	updatesFound, results := RunUpdateCheck(database, nil, []db.Game{*untracked}, true, api)

	if updatesFound != 1 {
		t.Errorf("expected 1 update via cache API, got %d", updatesFound)
	}
	if len(results) != 1 || results[0].Latest != "v3.0" {
		t.Errorf("results = %+v, want v3.0 via cache API", results)
	}
	if state.fullCalls.Load() != 1 {
		t.Errorf("expected 1 cache full fetch, got %d", state.fullCalls.Load())
	}

	// Status from the cache API must have been persisted.
	got, err := database.GetGame(untracked.ID)
	if err != nil || got == nil {
		t.Fatalf("failed to reload game: %v", err)
	}
	if got.Status != "completed" {
		t.Errorf("status = %q, want completed", got.Status)
	}
}

// TestRunUpdateCheck_BulkUntrackedFallsBack: when the cache API has no data
// for an untracked thread, the game falls back to direct scraping.
func TestRunUpdateCheck_BulkUntrackedFallsBack(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	api, state := newBulkTestAPI(t)
	state.bulkResponse = `{"status":"ok","msg":{"100":"v9.0"}}`

	// Thread page server for the direct-scrape fallback.
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, threadPage("Untracked Game [v3.0]", "v3.0", ""))
	}))
	defer page.Close()

	client := scraper.NewClientWithHTTP("", page.Client())

	untracked := &db.Game{Title: "Untracked Game", Engine: "RenPy", Path: "/untracked",
		F95URL: page.URL + "/threads/x.999/", // no thread ID — direct scrape only
		Version: "v2.0", LatestVersion: "v2.0"}
	if _, err := database.InsertGame(untracked); err != nil {
		t.Fatal(err)
	}

	updatesFound, results := RunUpdateCheck(database, client, []db.Game{*untracked}, true, api)

	if updatesFound != 1 {
		t.Errorf("expected 1 update via fallback scrape, got %d", updatesFound)
	}
	if len(results) != 1 || results[0].Latest != "3.0" {
		t.Errorf("results = %+v, want 3.0 via direct scrape", results)
	}
}

// TestRunUpdateCheck_BulkAPIDownFallsBack: when checker.php is unavailable,
// every game falls back to the direct-scrape path.
func TestRunUpdateCheck_BulkAPIDownFallsBack(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	api, state := newBulkTestAPI(t)
	state.bulkResponse = `{"status":"error","msg":"boom"}`

	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, threadPage("Game [v5.0]", "v5.0", ""))
	}))
	defer page.Close()
	client := scraper.NewClientWithHTTP("", page.Client())

	game := &db.Game{Title: "Game", Engine: "RenPy", Path: "/game",
		F95URL: page.URL + "/threads/x.123/", // no thread ID — direct scrape only
		Version: "v4.0", LatestVersion: "v4.0"}
	if _, err := database.InsertGame(game); err != nil {
		t.Fatal(err)
	}

	updatesFound, results := RunUpdateCheck(database, client, []db.Game{*game}, true, api)

	if updatesFound != 1 {
		t.Errorf("expected 1 update via fallback scrape, got %d", updatesFound)
	}
	if len(results) != 1 || results[0].Latest != "5.0" {
		t.Errorf("results = %+v, want 5.0 via direct scrape", results)
	}
	// No games should have been processed via bulk.
	if state.fastCalls.Load() != 0 {
		t.Errorf("expected no cache refresh after bulk failure, got %d calls", state.fastCalls.Load())
	}
}

// TestRunUpdateCheck_BulkStatusRefresh: threads that changed since the last
// check get a full cache fetch that updates status in the database.
func TestRunUpdateCheck_BulkStatusRefresh(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	api, state := newBulkTestAPI(t)
	state.bulkResponse = `{"status":"ok","msg":{"100":"v1.0"}}`
	state.fastResponse = `{"100":1800000000}` // thread changed long ago
	state.fullResponse = `{"name":"Game","version":"v1.0","status":2,"developer":"DevCo",
		"description":"desc","image_url":"https://example.com/c.png"}`

	game := &db.Game{Title: "Game", Engine: "RenPy", Path: "/game",
		F95URL: "https://f95zone.to/threads/x.100/", F95ThreadID: 100,
		Version: "v1.0", LatestVersion: "v1.0",
		VersionCheckedAt: time.Now().Add(-48 * time.Hour)}
	if _, err := database.InsertGame(game); err != nil {
		t.Fatal(err)
	}

	updatesFound, results := RunUpdateCheck(database, nil, []db.Game{*game}, true, api)

	if updatesFound != 0 {
		t.Errorf("expected 0 version updates, got %d", updatesFound)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if state.fullCalls.Load() != 1 {
		t.Errorf("expected 1 full cache fetch, got %d", state.fullCalls.Load())
	}

	got, err := database.GetGame(game.ID)
	if err != nil || got == nil {
		t.Fatalf("failed to reload game: %v", err)
	}
	if got.Status != "completed" {
		t.Errorf("status = %q, want completed (refreshed from cache)", got.Status)
	}

	// Scraped metadata should have been saved too.
	meta, err := database.GetScrapedMeta(game.ID)
	if err != nil {
		t.Fatalf("GetScrapedMeta: %v", err)
	}
	if meta == nil || meta.Developer != "DevCo" || meta.CoverURL != "https://example.com/c.png" {
		t.Errorf("scraped meta = %+v, want DevCo + cover", meta)
	}
}

// TestRunUpdateCheck_BulkNoRefreshWhenUnchanged: threads whose last change
// predates the previous check are not fetched in full.
func TestRunUpdateCheck_BulkNoRefreshWhenUnchanged(t *testing.T) {
	t.Parallel()
	database := setupTestDB(t)
	defer database.Close()

	api, state := newBulkTestAPI(t)
	state.bulkResponse = `{"status":"ok","msg":{"100":"v1.0"}}`
	state.fastResponse = `{"100":1700000000}` // changed long before last check

	game := &db.Game{Title: "Game", Engine: "RenPy", Path: "/game",
		F95URL: "https://f95zone.to/threads/x.100/", F95ThreadID: 100,
		Version: "v1.0", LatestVersion: "v1.0",
		VersionCheckedAt: time.Now().Add(-24 * time.Hour)}
	if _, err := database.InsertGame(game); err != nil {
		t.Fatal(err)
	}

	RunUpdateCheck(database, nil, []db.Game{*game}, true, api)

	if state.fullCalls.Load() != 0 {
		t.Errorf("expected 0 full cache fetches (thread unchanged), got %d", state.fullCalls.Load())
	}
}
