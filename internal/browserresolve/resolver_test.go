package browserresolve

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeEngine records every request and returns a canned result or error,
// letting the resolver's orchestration (profile copy, verify, move,
// teardown) be tested offline.
type fakeEngine struct {
	mu     sync.Mutex
	reqs   []engineRequest
	result engineResult
	err    error
}

func (f *fakeEngine) run(_ context.Context, req engineRequest) (engineResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, req)
	if f.err != nil {
		return engineResult{}, f.err
	}
	return f.result, nil
}

func (f *fakeEngine) requests() []engineRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]engineRequest(nil), f.reqs...)
}

// newTestResolver builds a resolver wired to a fake engine and a fixture
// profile for discovery.
func newTestResolver(t *testing.T, f *fakeEngine, opts ...Option) *resolver {
	t.Helper()
	o := defaultOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return &resolver{engine: f, opts: o}
}

// resolveWithProfile runs the resolver with the fixture profile via the
// profile override.
func resolveWithProfile(t *testing.T, r *resolver, url, destDir string) (string, error) {
	t.Helper()
	if r.opts.ProfileDir == "" {
		r.opts.ProfileDir = buildProfileFixture(t)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return r.resolve(ctx, url, destDir)
}

func TestResolver_Success(t *testing.T) {
	t.Parallel()
	src := filepath.Join(t.TempDir(), "stage.bin")
	writeFile(t, src, "payload-bytes")
	f := &fakeEngine{result: engineResult{
		path:       src,
		suggested:  "Game v1.2.zip",
		totalBytes: int64(len("payload-bytes")),
	}}
	r := newTestResolver(t, f)
	dest := filepath.Join(t.TempDir(), "games")

	got, err := resolveWithProfile(t, r, "https://cdn.example.com/f/abc", dest)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := filepath.Join(dest, "Game v1.2.zip")
	if got != want {
		t.Errorf("resolve returned %q, want %q", got, want)
	}
	assertFileContent(t, want, "payload-bytes")
	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("staged file must be moved away, stat err = %v", err)
	}

	reqs := f.requests()
	if len(reqs) != 1 {
		t.Fatalf("engine called %d times, want 1", len(reqs))
	}
	if reqs[0].url != "https://cdn.example.com/f/abc" {
		t.Errorf("engine url = %q", reqs[0].url)
	}
}

func TestResolver_EngineError_Teardown(t *testing.T) {
	t.Parallel()
	f := &fakeEngine{err: fmt.Errorf("engine boom")}
	r := newTestResolver(t, f)
	dest := filepath.Join(t.TempDir(), "games")

	_, err := resolveWithProfile(t, r, "https://cdn.example.com/f/abc", dest)
	if err == nil || !strings.Contains(err.Error(), "engine boom") {
		t.Fatalf("expected engine error, got %v", err)
	}

	// The profile copy and the download dir must be deleted on the error
	// path (teardown guarantee), and the destination must not receive a
	// file.
	reqs := f.requests()
	if len(reqs) != 1 {
		t.Fatalf("engine called %d times, want 1", len(reqs))
	}
	for _, label := range []struct{ name, dir string }{
		{"profile copy", reqs[0].profileDir},
		{"download dir", reqs[0].downloadDir},
	} {
		if _, err := os.Stat(label.dir); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s %q must be removed after engine failure, stat err = %v", label.name, label.dir, err)
		}
	}
	entries, err := os.ReadDir(dest)
	if err == nil && len(entries) > 0 {
		t.Errorf("destination dir must stay empty on failure, got %v", entries)
	}
}

func TestResolver_ChallengePage(t *testing.T) {
	t.Parallel()
	htmlPath := filepath.Join(t.TempDir(), "stage")
	writeFile(t, htmlPath, "<!DOCTYPE html><html><body>turnstile</body></html>")
	f := &fakeEngine{result: engineResult{path: htmlPath, suggested: "x.html"}}
	r := newTestResolver(t, f)
	dest := filepath.Join(t.TempDir(), "games")

	_, err := resolveWithProfile(t, r, "https://cdn.example.com/f/abc", dest)
	if !errors.Is(err, ErrChallengePage) {
		t.Fatalf("expected ErrChallengePage, got %v", err)
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Errorf("challenge page must not be moved into dest, got %v", entries)
	}
}

func TestResolver_EmptyFile(t *testing.T) {
	t.Parallel()
	empty := filepath.Join(t.TempDir(), "stage")
	writeFile(t, empty, "")
	f := &fakeEngine{result: engineResult{path: empty, suggested: "a.zip"}}
	r := newTestResolver(t, f)

	_, err := resolveWithProfile(t, r, "https://cdn.example.com/f/abc", t.TempDir())
	if !errors.Is(err, ErrDownload) {
		t.Fatalf("expected ErrDownload, got %v", err)
	}
}

func TestResolver_SizeMismatch(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "stage")
	writeFile(t, file, "short")
	f := &fakeEngine{result: engineResult{path: file, suggested: "a.zip", totalBytes: 99999}}
	r := newTestResolver(t, f)

	_, err := resolveWithProfile(t, r, "https://cdn.example.com/f/abc", t.TempDir())
	if !errors.Is(err, ErrDownload) {
		t.Fatalf("expected ErrDownload, got %v", err)
	}
}

func TestResolver_UnsafeSuggestedName(t *testing.T) {
	t.Parallel()
	src := filepath.Join(t.TempDir(), "stage")
	writeFile(t, src, "data")
	f := &fakeEngine{result: engineResult{path: src, suggested: "../../evil/name.zip"}}
	r := newTestResolver(t, f)
	dest := filepath.Join(t.TempDir(), "games")

	got, err := resolveWithProfile(t, r, "https://cdn.example.com/f/abc", dest)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != filepath.Join(dest, "name.zip") {
		t.Errorf("resolve = %q, want %q", got, filepath.Join(dest, "name.zip"))
	}
}

func TestResolver_NameCollision(t *testing.T) {
	t.Parallel()
	src := filepath.Join(t.TempDir(), "stage")
	writeFile(t, src, "new")
	dest := t.TempDir()
	writeFile(t, filepath.Join(dest, "Game.zip"), "old")
	f := &fakeEngine{result: engineResult{path: src, suggested: "Game.zip"}}
	r := newTestResolver(t, f)

	got, err := resolveWithProfile(t, r, "https://cdn.example.com/f/abc", dest)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != filepath.Join(dest, "Game (1).zip") {
		t.Errorf("resolve = %q, want Game (1).zip", got)
	}
	assertFileContent(t, got, "new")
	assertFileContent(t, filepath.Join(dest, "Game.zip"), "old")
}

func TestResolver_InvalidURLs(t *testing.T) {
	t.Parallel()
	f := &fakeEngine{}
	r := newTestResolver(t, f)
	tests := []string{
		"",
		"not-a-url",
		"ftp://example.com/file",
		"file:///etc/passwd",
		"https://",
	}
	for _, u := range tests {
		t.Run(fmt.Sprintf("%q", u), func(t *testing.T) {
			if _, err := r.resolve(context.Background(), u, t.TempDir()); err == nil {
				t.Errorf("expected error for URL %q", u)
			}
		})
	}
	if len(f.requests()) != 0 {
		t.Error("engine must not be called for invalid URLs")
	}
}

func TestResolver_EmptyDestDir(t *testing.T) {
	t.Parallel()
	f := &fakeEngine{}
	r := newTestResolver(t, f)
	if _, err := r.resolve(context.Background(), "https://example.com/f", "  "); err == nil {
		t.Fatal("expected error for empty destination dir")
	}
	if len(f.requests()) != 0 {
		t.Error("engine must not be called for an empty destination dir")
	}
}

func TestResolver_SuggestedNameFallback(t *testing.T) {
	t.Parallel()
	src := filepath.Join(t.TempDir(), "guid-12345")
	writeFile(t, src, "data")
	f := &fakeEngine{result: engineResult{path: src, suggested: ""}}
	r := newTestResolver(t, f)

	got, err := resolveWithProfile(t, r, "https://cdn.example.com/f/abc", t.TempDir())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if filepath.Base(got) != "guid-12345" {
		t.Errorf("resolve = %q, want on-disk name fallback guid-12345", got)
	}
}
