package scraper

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mili/moxie/internal/db"
)

// ---------------------------------------------------------------------------
// FindMatches
// ---------------------------------------------------------------------------

func TestFindMatches_WithURLMap(t *testing.T) {
	t.Parallel()

	// Create a temporary URL map file.
	tmpDir := t.TempDir()
	urlMapPath := filepath.Join(tmpDir, "url_map.json")
	urlMapContent := `{
		"/path/to/HH TRAP": "https://f95zone.to/threads/hh-trap.12345/",
		"/path/to/Summer's Gone": "https://f95zone.to/threads/summers-gone.67890/"
	}`
	if err := os.WriteFile(urlMapPath, []byte(urlMapContent), 0644); err != nil {
		t.Fatal(err)
	}

	games := []db.Game{
		{Title: "HH TRAP", Path: "/path/to/HH TRAP", F95URL: ""},
		{Title: "Summer's Gone", Path: "/path/to/Summer's Gone", F95URL: ""},
		// This game already has a URL and should be skipped.
		{Title: "Already Matched", Path: "/path/matched", F95URL: "https://f95zone.to/threads/matched.11111/"},
	}

	results, err := FindMatches(AssociateOptions{
		Client:   NewClient(""),
		AllGames: games,
		URLMap:   urlMapPath,
	})
	if err != nil {
		t.Fatalf("FindMatches returned error: %v", err)
	}

	// Should have 2 results (the unmatched games matched via URL map).
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	foundHH := false
	foundSummer := false
	for _, r := range results {
		switch r.Game.Title {
		case "HH TRAP":
			foundHH = true
			if r.BestMatch == nil {
				t.Error("HH TRAP should have a best match from URL map")
			} else if r.BestMatch.URL != "https://f95zone.to/threads/hh-trap.12345/" {
				t.Errorf("HH TRAP URL = %q, want %q",
					r.BestMatch.URL, "https://f95zone.to/threads/hh-trap.12345/")
			}
		case "Summer's Gone":
			foundSummer = true
			if r.BestMatch == nil {
				t.Error("Summer's Gone should have a best match from URL map")
			} else if r.BestMatch.URL != "https://f95zone.to/threads/summers-gone.67890/" {
				t.Errorf("Summer's Gone URL = %q, want %q",
					r.BestMatch.URL, "https://f95zone.to/threads/summers-gone.67890/")
			}
		default:
			t.Errorf("unexpected result for game %q", r.Game.Title)
		}
	}
	if !foundHH {
		t.Error("HH TRAP not found in results")
	}
	if !foundSummer {
		t.Error("Summer's Gone not found in results")
	}
}

func TestFindMatches_FiltersGamesWithURL(t *testing.T) {
	t.Parallel()

	// Use a URL map so no HTTP calls are made for the unmatched game.
	tmpDir := t.TempDir()
	urlMapPath := filepath.Join(tmpDir, "url_map.json")
	urlMapContent := `{"/path/unmatched": "https://f95zone.to/threads/unmatched.99999/"}`
	if err := os.WriteFile(urlMapPath, []byte(urlMapContent), 0644); err != nil {
		t.Fatal(err)
	}

	games := []db.Game{
		{Title: "Has URL", Path: "/path/existing", F95URL: "https://f95zone.to/threads/existing.111/"},
		{Title: "No URL", Path: "/path/unmatched", F95URL: ""},
	}

	results, err := FindMatches(AssociateOptions{
		Client:   NewClient(""),
		AllGames: games,
		URLMap:   urlMapPath,
	})
	if err != nil {
		t.Fatalf("FindMatches returned error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result (only the unmatched game), got %d", len(results))
	}
	if results[0].Game.Title != "No URL" {
		t.Errorf("expected game 'No URL', got %q", results[0].Game.Title)
	}
}

func TestFindMatches_EmptyGames(t *testing.T) {
	t.Parallel()

	results, err := FindMatches(AssociateOptions{
		Client:   NewClient(""),
		AllGames: []db.Game{},
	})
	if err != nil {
		t.Fatalf("FindMatches returned error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestFindMatches_AllHaveURLs(t *testing.T) {
	t.Parallel()

	games := []db.Game{
		{Title: "Game A", Path: "/a", F95URL: "https://f95zone.to/threads/a.1/"},
		{Title: "Game B", Path: "/b", F95URL: "https://f95zone.to/threads/b.2/"},
	}

	results, err := FindMatches(AssociateOptions{
		Client:   NewClient(""),
		AllGames: games,
	})
	if err != nil {
		t.Fatalf("FindMatches returned error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results (all have URLs), got %d", len(results))
	}
}

func TestFindMatches_BadURLMap(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	badPath := filepath.Join(tmpDir, "nonexistent.json")

	_, err := FindMatches(AssociateOptions{
		Client:   NewClient(""),
		AllGames: []db.Game{{Title: "Game", Path: "/game"}},
		URLMap:   badPath,
	})
	if err == nil {
		t.Fatal("expected error for nonexistent URL map file, got nil")
	}
}

func TestFindMatches_InvalidURLMapJSON(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	badPath := filepath.Join(tmpDir, "bad.json")
	if err := os.WriteFile(badPath, []byte(`{invalid json}`), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := FindMatches(AssociateOptions{
		Client:   NewClient(""),
		AllGames: []db.Game{{Title: "Game", Path: "/game"}},
		URLMap:   badPath,
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON in URL map, got nil")
	}
}

func TestFindMatches_NilClient(t *testing.T) {
	t.Parallel()

	// A nil client will cause a panic when SearchF95Zone is called.
	// But with URL map, it should not be called.
	tmpDir := t.TempDir()
	urlMapPath := filepath.Join(tmpDir, "url_map.json")
	urlMapContent := `{"/path/game": "https://f95zone.to/threads/game.1/"}`
	if err := os.WriteFile(urlMapPath, []byte(urlMapContent), 0644); err != nil {
		t.Fatal(err)
	}

	results, err := FindMatches(AssociateOptions{
		Client:   nil,
		AllGames: []db.Game{{Title: "Game", Path: "/path/game", F95URL: ""}},
		URLMap:   urlMapPath,
	})
	if err != nil {
		t.Fatalf("FindMatches with URL map should not error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].BestMatch == nil {
		t.Fatal("expected best match from URL map")
	}
}
