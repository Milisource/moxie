package commands

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/saves"
	"github.com/mili/moxie/internal/scraper"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// fetchTestEngine returns a saves engine and a game backed by a fresh temp DB.
func fetchTestEngine(t *testing.T, title string) (*saves.Engine, *db.Game) {
	t.Helper()
	base := t.TempDir()
	database, err := db.Open(filepath.Join(base, "moxie.sqlite"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	game := &db.Game{
		Title:       title,
		Engine:      "RenPy",
		Path:        filepath.Join(base, "game"),
		Version:     "1.2.3",
		F95ThreadID: 4242,
	}
	if _, err := database.InsertGame(game); err != nil {
		t.Fatalf("insert game: %v", err)
	}

	engine, err := saves.NewEngine(database, filepath.Join(base, "saves"))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	return engine, game
}

// fetchTestZip builds a zip archive in memory from entry name -> content.
func fetchTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("zip create %q: %v", name, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %q: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// ---------------------------------------------------------------------------
// selectSaveCandidate
// ---------------------------------------------------------------------------

func TestSelectSaveCandidate(t *testing.T) {
	t.Parallel()
	candidates := []scraper.SaveCandidate{
		{URL: "u1", Kind: scraper.SaveCandidateAttachment, Filename: "a.zip"},
		{URL: "u2", Kind: scraper.SaveCandidateExternal, Filename: "b.zip"},
	}

	tests := []struct {
		name      string
		cands     []scraper.SaveCandidate
		pick      int
		wantURL   string
		wantError string
	}{
		{
			name:      "empty list",
			cands:     nil,
			pick:      0,
			wantError: "save candidates found",
		},
		{
			name:      "empty list with pick",
			cands:     nil,
			pick:      1,
			wantError: "save candidates found",
		},
		{
			name:    "single candidate auto-picked",
			cands:   candidates[:1],
			pick:    0,
			wantURL: "u1",
		},
		{
			name:    "single candidate explicit pick",
			cands:   candidates[:1],
			pick:    1,
			wantURL: "u1",
		},
		{
			name:      "multiple requires pick",
			cands:     candidates,
			pick:      0,
			wantError: "--pick",
		},
		{
			name:    "multiple pick second",
			cands:   candidates,
			pick:    2,
			wantURL: "u2",
		},
		{
			name:      "pick out of range high",
			cands:     candidates,
			pick:      3,
			wantError: "out of range",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectSaveCandidate(tt.cands, tt.pick)
			if tt.wantError != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil (candidate %+v)", tt.wantError, got)
				}
				if !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantError)
				}
				if got != nil {
					t.Errorf("expected nil candidate on error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got == nil {
				t.Fatal("expected a candidate, got nil")
			}
			if got.URL != tt.wantURL {
				t.Errorf("selected URL = %q, want %q", got.URL, tt.wantURL)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// printSaveCandidates
// ---------------------------------------------------------------------------

func TestPrintSaveCandidates(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	printSaveCandidates(&buf, []scraper.SaveCandidate{
		{URL: "u", Kind: scraper.SaveCandidateAttachment, Host: "f95zone", Filename: "full-save.zip", Context: "Full save 100%"},
		{URL: "v", Kind: scraper.SaveCandidateExternal, Host: "mega", Filename: "extra.zip", Context: "extras"},
	})

	out := buf.String()
	for _, want := range []string{"KIND", "HOST", "FILENAME", "CONTEXT", "f95zone", "full-save.zip", "mega", "extra.zip", "Full save 100%"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestPrintSaveCandidatesEmpty(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	printSaveCandidates(&buf, nil)
	if got := buf.String(); !strings.Contains(got, "No save candidates found in the first post") {
		t.Errorf("empty output = %q", got)
	}
}

// ---------------------------------------------------------------------------
// resolveFetchURL
// ---------------------------------------------------------------------------

func TestResolveFetchURL(t *testing.T) {
	t.Parallel()
	game := &db.Game{F95URL: "https://f95zone.to/threads/slug.777/", F95ThreadID: 777}

	if got := resolveFetchURL("https://example.com/override", game); got != "https://example.com/override" {
		t.Errorf("override = %q, want the override URL", got)
	}
	if got := resolveFetchURL("", game); got != "https://f95zone.to/threads/777/" {
		t.Errorf("derived = %q, want thread URL from thread ID", got)
	}
	if got := resolveFetchURL("", &db.Game{}); got != "" {
		t.Errorf("no thread = %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// filename / source / password hint helpers
// ---------------------------------------------------------------------------

func TestSaveFetchFilename(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want string
	}{
		{"full-save.zip", "full-save.zip"},
		{"  spaced.zip  ", "spaced.zip"},
		{"a/b.zip", "b.zip"},
		{"", "save-download"},
		{"..", "save-download"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := saveFetchFilename(scraper.SaveCandidate{Filename: tt.in})
			if got != tt.want {
				t.Errorf("saveFetchFilename(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSourceForCandidate(t *testing.T) {
	t.Parallel()
	if got := sourceForCandidate(scraper.SaveCandidate{Kind: scraper.SaveCandidateAttachment}); got != "f95_attachment" {
		t.Errorf("attachment source = %q, want f95_attachment", got)
	}
	if got := sourceForCandidate(scraper.SaveCandidate{Kind: scraper.SaveCandidateExternal}); got != "f95_link" {
		t.Errorf("external source = %q, want f95_link", got)
	}
}

func TestPasswordHint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		err      error
		password string
		wantHint bool
	}{
		{"nil error", nil, "", false},
		{"password already given", fmt.Errorf("saves: extract archive: bad password"), "f95zone", false},
		{"unrelated error", os.ErrNotExist, "", false},
		{"extract failure", fmt.Errorf("saves: extract archive: %w", errors.New("bad password")), "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := passwordHint(tt.err, tt.password)
			if hint := got != ""; hint != tt.wantHint {
				t.Errorf("passwordHint() = %q, want non-empty=%v", got, tt.wantHint)
			}
		})
	}

	if got := passwordHint(fmt.Errorf("saves: extract archive: failed"), ""); !strings.Contains(got, "--password") {
		t.Errorf("passwordHint(extract err) = %q, want a --password hint", got)
	}
}

// ---------------------------------------------------------------------------
// fetchSaveSet — download → import pipeline over a local HTTP server
// ---------------------------------------------------------------------------

func TestFetchSaveSetAttachment(t *testing.T) {
	t.Parallel()

	zipBytes := fetchTestZip(t, map[string]string{
		"persistent": "hello",
		"slot1.save": "slot",
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(zipBytes)
	}))
	defer srv.Close()

	engine, game := fetchTestEngine(t, "My Game")
	client := scraper.NewClientWithHTTP("", &http.Client{})
	candidate := scraper.SaveCandidate{
		URL:      srv.URL + "/attachments/full-save.zip.12345/",
		Kind:     scraper.SaveCandidateAttachment,
		Host:     "f95zone",
		Filename: "full-save.zip",
	}

	set, err := fetchSaveSet(context.Background(), engine, client, "", game, candidate, "")
	if err != nil {
		t.Fatalf("fetchSaveSet: %v", err)
	}
	if set.Source != "f95_attachment" {
		t.Errorf("Source = %q, want f95_attachment", set.Source)
	}
	if set.SourceURL != candidate.URL {
		t.Errorf("SourceURL = %q, want %q", set.SourceURL, candidate.URL)
	}
	if set.GameVersion != game.Version {
		t.Errorf("GameVersion = %q, want %q", set.GameVersion, game.Version)
	}
	if set.Name != "full-save-zip" {
		t.Errorf("Name = %q, want full-save-zip", set.Name)
	}

	setDir := engine.SetPath(game, set.Name)
	data, err := os.ReadFile(filepath.Join(setDir, "persistent"))
	if err != nil {
		t.Fatalf("read imported file: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("persistent = %q, want hello", data)
	}
}

func TestFetchSaveSetUnsupportedKind(t *testing.T) {
	t.Parallel()
	engine, game := fetchTestEngine(t, "My Game")
	_, err := fetchSaveSet(context.Background(), engine, nil, "", game, scraper.SaveCandidate{Kind: "bogus"}, "")
	if err == nil || !strings.Contains(err.Error(), "unsupported save candidate kind") {
		t.Fatalf("err = %v, want unsupported-kind error", err)
	}
}

func TestFetchSaveSetNilEngine(t *testing.T) {
	t.Parallel()
	_, err := fetchSaveSet(context.Background(), nil, nil, "", &db.Game{}, scraper.SaveCandidate{}, "")
	if err == nil {
		t.Fatal("expected an error for a nil engine")
	}
}
