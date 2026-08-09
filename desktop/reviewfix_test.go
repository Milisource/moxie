package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/engine"
	"github.com/mili/moxie/internal/scraper"
)

// TestRenameGameValidation covers the path-traversal hardening: titles that
// could escape the game's parent directory must be rejected before the
// database is touched, virtual-path games must refuse rename, and valid
// renames must round-trip the directory on disk.
func TestRenameGameValidation(t *testing.T) {
	a := newTestApp(t)

	root := t.TempDir()
	validDir := filepath.Join(root, "My Game")
	if err := os.Mkdir(validDir, 0o755); err != nil {
		t.Fatal(err)
	}
	validID := addGame(t, a, "My Game", validDir)

	t.Run("rejects empty title", func(t *testing.T) {
		if err := a.RenameGame(validID, "   "); err == nil {
			t.Fatal("expected error for empty title")
		}
	})

	t.Run("rejects traversal titles before DB touch", func(t *testing.T) {
		for _, bad := range []string{"../Elsewhere", "a/../b", "sub/dir", "..", "a..b/.."} {
			if err := a.RenameGame(validID, bad); err == nil {
				t.Errorf("RenameGame(%q) = nil, want traversal rejection", bad)
			}
		}
		g, err := a.db.GetGame(validID)
		if err != nil || g == nil {
			t.Fatalf("game should still exist after rejected renames: %v", err)
		}
		if g.Path != validDir {
			t.Errorf("game path changed after rejected renames: %q", g.Path)
		}
	})

	t.Run("rejects nonexistent game", func(t *testing.T) {
		if err := a.RenameGame(999999, "Whatever"); err == nil {
			t.Fatal("expected error for nonexistent game")
		}
	})

	t.Run("refuses virtual-path games", func(t *testing.T) {
		virtual := filepath.Join(db.VirtualPathPrefix, "f95zone", "123")
		vid := addGame(t, a, "Virtual Game", virtual)
		if err := a.RenameGame(vid, "New Name"); err == nil {
			t.Fatal("expected error renaming a virtual-path game")
		}
	})

	t.Run("renames directory and keeps exact title in DB", func(t *testing.T) {
		if err := a.RenameGame(validID, "New & Shiny [v2]"); err != nil {
			t.Fatalf("RenameGame: %v", err)
		}
		g, err := a.db.GetGame(validID)
		if err != nil || g == nil {
			t.Fatalf("GetGame: %v", err)
		}
		if g.Title != "New & Shiny [v2]" {
			t.Errorf("title = %q, want exact user title", g.Title)
		}
		if g.Path != filepath.Join(root, "New & Shiny [v2]") {
			t.Errorf("path = %q, want sanitized dir", g.Path)
		}
		if _, err := os.Stat(filepath.Join(root, "New & Shiny [v2]")); err != nil {
			t.Errorf("renamed dir missing: %v", err)
		}
		if _, err := os.Stat(validDir); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("old dir should be gone, stat err = %v", err)
		}
	})

	t.Run("target exists error", func(t *testing.T) {
		other := filepath.Join(root, "Taken")
		if err := os.Mkdir(other, 0o755); err != nil {
			t.Fatal(err)
		}
		otherID := addGame(t, a, "Taken", other)
		if err := a.RenameGame(otherID, "New & Shiny [v2]"); err == nil {
			t.Fatal("expected error when target directory exists")
		}
	})
}

// TestDownloadWithRetries exercises the retry/backoff/attempt-counting
// contract: transient failures retry up to maxRetries with fresh temp dirs,
// non-transient failures abort immediately, cancellation is honoured during
// backoff, and the reported attempt count is the real one.
func TestDownloadWithRetries(t *testing.T) {
	ctx := context.Background()

	t.Run("succeeds on first attempt", func(t *testing.T) {
		attempts, err := downloadWithRetries(ctx, t.TempDir(), 3, time.Millisecond,
			func(attempt int, dir string) error { return nil })
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if attempts != 1 {
			t.Errorf("attempts = %d, want 1", attempts)
		}
	})

	t.Run("retries transient failures and reports real attempt count", func(t *testing.T) {
		var count int
		attempts, err := downloadWithRetries(ctx, t.TempDir(), 3, time.Millisecond,
			func(attempt int, dir string) error {
				count++
				return &netTimeoutError{}
			})
		if err == nil {
			t.Fatal("expected error after all retries")
		}
		if attempts != 3 || count != 3 {
			t.Errorf("attempts = %d (fn calls %d), want 3", attempts, count)
		}
	})

	t.Run("aborts on non-transient error", func(t *testing.T) {
		want := errors.New("permanent")
		var count int
		attempts, err := downloadWithRetries(ctx, t.TempDir(), 3, time.Millisecond,
			func(attempt int, dir string) error { count++; return want })
		if !errors.Is(err, want) {
			t.Fatalf("err = %v, want %v", err, want)
		}
		if attempts != 1 || count != 1 {
			t.Errorf("attempts = %d (fn calls %d), want 1", attempts, count)
		}
	})

	t.Run("succeeds on retry with fresh temp dir", func(t *testing.T) {
		var seen []string
		attempts, err := downloadWithRetries(ctx, t.TempDir(), 3, time.Millisecond,
			func(attempt int, dir string) error {
				seen = append(seen, dir)
				if attempt == 1 {
					return &netTimeoutError{}
				}
				return nil
			})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if attempts != 2 {
			t.Errorf("attempts = %d, want 2", attempts)
		}
		if len(seen) != 2 || seen[0] == seen[1] {
			t.Errorf("retry should use a fresh temp dir, saw %v", seen)
		}
	})

	t.Run("honours cancellation during backoff", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		go func() {
			time.Sleep(30 * time.Millisecond)
			cancel()
		}()
		attempts, err := downloadWithRetries(cctx, t.TempDir(), 3, 5*time.Second,
			func(attempt int, dir string) error { return &netTimeoutError{} })
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if attempts != 1 {
			t.Errorf("attempts = %d, want 1 (cancelled during first backoff)", attempts)
		}
	})

	t.Run("honours pre-cancelled context", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		attempts, err := downloadWithRetries(cctx, t.TempDir(), 3, time.Millisecond,
			func(attempt int, dir string) error { return nil })
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if attempts != 0 {
			t.Errorf("attempts = %d, want 0", attempts)
		}
	})
}

// netTimeoutError satisfies net.Error with Timeout() true so the retry loop
// treats it as transient without needing a real network round trip.
type netTimeoutError struct{}

func (e *netTimeoutError) Error() string   { return "simulated timeout" }
func (e *netTimeoutError) Timeout() bool   { return true }
func (e *netTimeoutError) Temporary() bool { return true }

// TestPickDownloadedFile verifies the largest-file selection and its error
// cases (empty dir, dirs-only).
func TestPickDownloadedFile(t *testing.T) {
	t.Run("picks largest file", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "small.bin"), 10)
		writeTestFile(t, filepath.Join(dir, "big.bin"), 100)
		got, err := pickDownloadedFile(dir)
		if err != nil {
			t.Fatalf("pickDownloadedFile: %v", err)
		}
		if filepath.Base(got) != "big.bin" {
			t.Errorf("picked %q, want big.bin", got)
		}
	})

	t.Run("skips directories", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(dir, "only.bin"), 5)
		got, err := pickDownloadedFile(dir)
		if err != nil {
			t.Fatalf("pickDownloadedFile: %v", err)
		}
		if filepath.Base(got) != "only.bin" {
			t.Errorf("picked %q, want only.bin", got)
		}
	})

	t.Run("empty dir errors", func(t *testing.T) {
		if _, err := pickDownloadedFile(t.TempDir()); err == nil {
			t.Fatal("expected error for empty dir")
		}
	})

	t.Run("dirs-only errors", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := pickDownloadedFile(dir); err == nil {
			t.Fatal("expected error for dirs-only dir")
		}
	})

	t.Run("missing dir errors", func(t *testing.T) {
		if _, err := pickDownloadedFile(filepath.Join(t.TempDir(), "nope")); err == nil {
			t.Fatal("expected error for missing dir")
		}
	})
}

func writeTestFile(t *testing.T, path string, size int) {
	t.Helper()
	data := make([]byte, size)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPickBestSearchResult covers the scoring contract: engine-variant
// bonus, non-game rejection, threshold cutoff, and best-of selection.
func TestPickBestSearchResult(t *testing.T) {
	a := &App{}
	detEngine := engine.Result{Engine: engine.RenPy}

	t.Run("engine variant bonus breaks ties between near matches", func(t *testing.T) {
		results := []scraper.SearchResult{
			{Title: "My Game Studio Edition", URL: "/threads/1"},
			{Title: "My Game Studio Edition Ren'Py", URL: "/threads/2"},
		}
		best := a.pickBestSearchResult(db.Game{Title: "My Game"}, detEngine, results)
		if best == nil {
			t.Fatal("expected a best result")
		}
		if best.URL != "/threads/2" {
			t.Errorf("best URL = %q, want /threads/2 (engine variant bonus should win)", best.URL)
		}
	})

	t.Run("non-game threads rejected", func(t *testing.T) {
		results := []scraper.SearchResult{
			{Title: "REQUEST: Corruption of Champions II", URL: "/threads/1"},
			{Title: "My Game", URL: "/threads/2"},
		}
		best := a.pickBestSearchResult(db.Game{Title: "My Game"}, detEngine, results)
		if best == nil || best.URL != "/threads/2" {
			t.Errorf("best = %+v, want URL /threads/2", best)
		}
	})

	t.Run("below threshold returns nil", func(t *testing.T) {
		results := []scraper.SearchResult{
			{Title: "Totally Unrelated Title", URL: "/threads/1"},
		}
		if best := a.pickBestSearchResult(db.Game{Title: "My Game"}, detEngine, results); best != nil {
			t.Errorf("best = %+v, want nil below 0.3 threshold", best)
		}
	})

	t.Run("empty results return nil", func(t *testing.T) {
		if best := a.pickBestSearchResult(db.Game{Title: "My Game"}, detEngine, nil); best != nil {
			t.Errorf("best = %+v, want nil for empty results", best)
		}
	})
}

// TestPickBestLatestResult mirrors the search-result scoring for the
// latest-updates feed and verifies the score is returned alongside.
func TestPickBestLatestResult(t *testing.T) {
	a := &App{}
	detEngine := engine.Result{Engine: engine.RenPy}

	t.Run("returns best with score", func(t *testing.T) {
		results := []scraper.LatestSearchResult{
			{Title: "My Game Studio Edition", ThreadID: 1, Version: "v0.9"},
			{Title: "My Game Studio Edition Ren'Py", ThreadID: 2, Version: "v1.0"},
		}
		best, score := a.pickBestLatestResult(db.Game{Title: "My Game"}, detEngine, results)
		if best == nil {
			t.Fatal("expected a best result")
		}
		if best.ThreadID != 2 {
			t.Errorf("best ThreadID = %d, want 2 (engine variant bonus)", best.ThreadID)
		}
		if score <= 0 {
			t.Errorf("score = %f, want > 0", score)
		}
	})

	t.Run("below threshold returns nil and zero score", func(t *testing.T) {
		results := []scraper.LatestSearchResult{{Title: "Unrelated", ThreadID: 1}}
		best, score := a.pickBestLatestResult(db.Game{Title: "My Game"}, detEngine, results)
		if best != nil || score != 0 {
			t.Errorf("best = %+v score = %f, want nil/0", best, score)
		}
	})
}

// testPublicAPI starts an httptest server routing by path and returns a
// scraper.PublicAPI pointed at it (mirrors the internal scraper test helper,
// which is not importable from this package).
func testPublicAPI(t *testing.T, handler http.Handler) *scraper.PublicAPI {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	api := scraper.NewPublicAPI()
	api.Host = srv.URL
	api.CacheHost = srv.URL
	return api
}

// TestCheckGameVersion covers the version-check branches: the checker.php
// bulk map hit, the cache-API fallback for untracked threads (httptest), and
// the newer/same/older comparison outcomes.
func TestCheckGameVersion(t *testing.T) {
	ctx := context.Background()

	t.Run("versions map hit detects newer", func(t *testing.T) {
		a := newTestApp(t)
		game := &db.Game{ID: 1, F95ThreadID: 100, Title: "My Game", Version: "v1.0"}
		api := testPublicAPI(t, http.NotFoundHandler())
		var allErrors []string
		isUpdate, blocked := a.checkGameVersion(ctx, game, api, nil, "",
			map[int64]string{100: "v1.1"}, &allErrors)
		if !isUpdate || blocked {
			t.Errorf("isUpdate = %v blocked = %v, want true/false", isUpdate, blocked)
		}
		if game.LatestVersion != "v1.1" {
			t.Errorf("LatestVersion = %q, want v1.1", game.LatestVersion)
		}
	})

	t.Run("versions map hit same version is no update", func(t *testing.T) {
		a := newTestApp(t)
		game := &db.Game{ID: 1, F95ThreadID: 100, Title: "My Game", Version: "v1.0"}
		api := testPublicAPI(t, http.NotFoundHandler())
		var allErrors []string
		isUpdate, _ := a.checkGameVersion(ctx, game, api, nil, "",
			map[int64]string{100: "v1.0"}, &allErrors)
		if isUpdate {
			t.Error("isUpdate = true, want false for same version")
		}
	})

	t.Run("versions map hit older is no update", func(t *testing.T) {
		a := newTestApp(t)
		game := &db.Game{ID: 1, F95ThreadID: 100, Title: "My Game", Version: "v1.0"}
		api := testPublicAPI(t, http.NotFoundHandler())
		var allErrors []string
		isUpdate, _ := a.checkGameVersion(ctx, game, api, nil, "",
			map[int64]string{100: "v0.9"}, &allErrors)
		if isUpdate {
			t.Error("isUpdate = true, want false for older version")
		}
	})

	t.Run("qualifier-stripped comparison avoids phantom updates", func(t *testing.T) {
		a := newTestApp(t)
		game := &db.Game{ID: 1, F95ThreadID: 100, Title: "My Game", Version: "v1.03 + DLC"}
		api := testPublicAPI(t, http.NotFoundHandler())
		var allErrors []string
		isUpdate, _ := a.checkGameVersion(ctx, game, api, nil, "",
			map[int64]string{100: "v1.03"}, &allErrors)
		if isUpdate {
			t.Error("isUpdate = true, want false (qualifier only)")
		}
	})

	t.Run("untracked thread falls back to cache API", func(t *testing.T) {
		var cacheHits atomic.Int64
		mux := http.NewServeMux()
		mux.HandleFunc("/full/200", func(w http.ResponseWriter, r *http.Request) {
			cacheHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"name":"My Game","version":"v2.0","developer":"Dev"}`)
		})
		api := testPublicAPI(t, mux)
		a := newTestApp(t)
		id := addGame(t, a, "My Game", "/tmp/nowhere")
		game := &db.Game{ID: id, F95ThreadID: 200, Title: "My Game", Version: "v1.5"}
		var allErrors []string
		isUpdate, blocked := a.checkGameVersion(ctx, game, api, nil, "",
			map[int64]string{}, &allErrors)
		if !isUpdate || blocked {
			t.Errorf("isUpdate = %v blocked = %v, want true/false", isUpdate, blocked)
		}
		if cacheHits.Load() != 1 {
			t.Errorf("cache hits = %d, want 1", cacheHits.Load())
		}
		if game.LatestVersion != "v2.0" {
			t.Errorf("LatestVersion = %q, want v2.0", game.LatestVersion)
		}
	})

	t.Run("cache API thread-not-found is not an update", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/full/300", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
		api := testPublicAPI(t, mux)
		a := newTestApp(t)
		game := &db.Game{ID: 1, F95ThreadID: 300, Title: "My Game", Version: "v1.0"}
		var allErrors []string
		isUpdate, blocked := a.checkGameVersion(ctx, game, api, nil, "",
			map[int64]string{}, &allErrors)
		if isUpdate || blocked {
			t.Errorf("isUpdate = %v blocked = %v, want false/false for vanished thread", isUpdate, blocked)
		}
	})

	t.Run("thread-id-less game without cookies reports error", func(t *testing.T) {
		a := newTestApp(t)
		game := &db.Game{ID: 1, F95ThreadID: 0, Title: "My Game", Version: "v1.0"}
		api := testPublicAPI(t, http.NotFoundHandler())
		var allErrors []string
		isUpdate, blocked := a.checkGameVersion(ctx, game, api, nil, "",
			map[int64]string{}, &allErrors)
		if isUpdate || blocked {
			t.Errorf("isUpdate = %v blocked = %v, want false/false", isUpdate, blocked)
		}
		if len(allErrors) != 1 {
			t.Errorf("allErrors = %v, want 1 entry", allErrors)
		}
	})
}
