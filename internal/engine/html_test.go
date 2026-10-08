package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// TestIsHTMLGameFile guards the content sniff: a game-signature page is
// playable, a plain documentation page is not, and a signature in the tail of
// a large file is still found (bounded head+tail read).
func TestIsHTMLGameFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	twine := write("twine.html", "<html><body><tw-storydata name='x'></tw-storydata></body></html>")
	canvas := write("canvas.htm", `<html><canvas id="c"></canvas></html>`)
	plain := write("docs.html", "<html><body>just docs</body></html>")
	notHTML := write("notes.txt", "<tw-storydata>")

	// Marker only in the tail of a file larger than the head window.
	big := filepath.Join(dir, "big.html")
	if err := os.WriteFile(big, append(make([]byte, htmlSniffHead+1024), []byte("<canvas")...), 0644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		path string
		want bool
	}{
		{"twine", twine, true},
		{"canvas .htm", canvas, true},
		{"plain docs", plain, false},
		{"non-html", notHTML, false},
		{"marker in tail", big, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsHTMLGameFile(tc.path); got != tc.want {
				t.Errorf("IsHTMLGameFile(%s) = %v, want %v", filepath.Base(tc.path), got, tc.want)
			}
		})
	}
}

// TestFindHTMLEntry guards entry-page selection: index.html wins, then a
// game-signature page over a larger plain page, then the largest file.
func TestFindHTMLEntry(t *testing.T) {
	t.Run("prefers index.html", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html><tw-storydata></tw-storydata></html>"), 0644)
		os.WriteFile(filepath.Join(dir, "game.html"), []byte("<html><canvas></canvas></html>"), 0644)
		if got := FindHTMLEntry(dir); filepath.Base(got) != "index.html" {
			t.Errorf("FindHTMLEntry = %q, want index.html", got)
		}
	})

	t.Run("prefers game signature over larger plain", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "manual.html"), make([]byte, 4096), 0644)
		game := filepath.Join(dir, "play.html")
		os.WriteFile(game, []byte("<html><canvas></canvas></html>"), 0644)
		if got := FindHTMLEntry(dir); got != game {
			t.Errorf("FindHTMLEntry = %q, want %q", got, game)
		}
	})

	t.Run("falls back to largest", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "a.html"), make([]byte, 10), 0644)
		big := filepath.Join(dir, "b.html")
		os.WriteFile(big, make([]byte, 100), 0644)
		if got := FindHTMLEntry(dir); got != big {
			t.Errorf("FindHTMLEntry = %q, want %q", got, big)
		}
	})

	t.Run("no html", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("x"), 0644)
		if got := FindHTMLEntry(dir); got != "" {
			t.Errorf("FindHTMLEntry = %q, want empty", got)
		}
	})
}

// TestFindHTMLEntryShallow guards the bounded nested search: a root entry is
// returned unchanged, an entry one level down is found, engine-internal
// directories (game/) are skipped, and the descent stays bounded.
func TestFindHTMLEntryShallow(t *testing.T) {
	t.Run("root entry unchanged", func(t *testing.T) {
		dir := t.TempDir()
		root := filepath.Join(dir, "index.html")
		os.WriteFile(root, []byte("<html></html>"), 0644)
		if got := FindHTMLEntryShallow(dir); got != root {
			t.Errorf("FindHTMLEntryShallow = %q, want %q", got, root)
		}
	})

	t.Run("nested index.html", func(t *testing.T) {
		dir := t.TempDir()
		sub := filepath.Join(dir, "src")
		if err := os.MkdirAll(sub, 0755); err != nil {
			t.Fatal(err)
		}
		entry := filepath.Join(sub, "index.html")
		os.WriteFile(entry, []byte("<html><tw-storydata></tw-storydata></html>"), 0644)
		if got := FindHTMLEntryShallow(dir); got != entry {
			t.Errorf("FindHTMLEntryShallow = %q, want %q", got, entry)
		}
	})

	t.Run("skips engine-internal dirs", func(t *testing.T) {
		dir := t.TempDir()
		game := filepath.Join(dir, "game")
		if err := os.MkdirAll(game, 0755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(game, "index.html"), []byte("<html></html>"), 0644)
		if got := FindHTMLEntryShallow(dir); got != "" {
			t.Errorf("FindHTMLEntryShallow = %q, want empty (game/ is internal)", got)
		}
	})

	t.Run("depth bounded", func(t *testing.T) {
		dir := t.TempDir()
		deep := filepath.Join(dir, "a", "b", "c")
		if err := os.MkdirAll(deep, 0755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(deep, "index.html"), []byte("<html></html>"), 0644)
		if got := FindHTMLEntryShallow(dir); got != "" {
			t.Errorf("FindHTMLEntryShallow = %q, want empty (depth-bounded)", got)
		}
	})
}
