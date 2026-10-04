package launcher

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestResolveExecutableFindsHTMLEntry guards that a browser-played HTML game
// with no native executable still resolves to its entry page.
func TestResolveExecutableFindsHTMLEntry(t *testing.T) {
	dir := t.TempDir()
	html := filepath.Join(dir, "index.html")
	os.WriteFile(html, []byte("<html><tw-storydata name='x'></tw-storydata></html>"), 0644)

	if got := ResolveExecutable(dir, ""); got != html {
		t.Errorf("ResolveExecutable = %q, want %q", got, html)
	}
}

// TestResolveExecutablePrefersNativeOverHTML guards that an HTML page bundled
// next to a real executable does not hijack the launch.
func TestResolveExecutablePrefersNativeOverHTML(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "Game.exe")
	os.WriteFile(exe, make([]byte, 100), 0644)
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html><canvas></canvas></html>"), 0644)

	if got := ResolveExecutable(dir, ""); got != exe {
		t.Errorf("ResolveExecutable = %q, want native %q", got, exe)
	}
}

// TestListExecutablesIncludesHTMLEntry guards that the manual exe picker
// offers HTML entry pages as selectable launch targets.
func TestListExecutablesIncludesHTMLEntry(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Game.exe"), []byte("x"), 0644)
	html := filepath.Join(dir, "index.html")
	os.WriteFile(html, []byte("<html></html>"), 0644)

	got := ListExecutables(dir)
	if !slices.Contains(got, html) {
		t.Errorf("ListExecutables = %v, want index.html included", got)
	}
}
