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

// TestResolveExecutableNestedExe guards the bounded descent fallback: a game
// whose executable sits one folder below the root still resolves.
func TestResolveExecutableNestedExe(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "GameFolder")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(nested, "Game.exe")
	if err := os.WriteFile(exe, make([]byte, 100), 0644); err != nil {
		t.Fatal(err)
	}

	if got := ResolveExecutable(dir, ""); got != exe {
		t.Errorf("ResolveExecutable = %q, want nested %q", got, exe)
	}
}

// TestResolveExecutableNestedHTML guards the nested HTML fallback for a
// browser game whose entry page sits below the root.
func TestResolveExecutableNestedHTML(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "src")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	html := filepath.Join(nested, "index.html")
	if err := os.WriteFile(html, []byte("<html><tw-storydata></tw-storydata></html>"), 0644); err != nil {
		t.Fatal(err)
	}

	if got := ResolveExecutable(dir, ""); got != html {
		t.Errorf("ResolveExecutable = %q, want nested html %q", got, html)
	}
}

// TestResolveExecutableSkipsRuntimeDirs guards that a bundled runtime (jre/)
// is not mistaken for the game during the nested descent.
func TestResolveExecutableSkipsRuntimeDirs(t *testing.T) {
	dir := t.TempDir()
	jre := filepath.Join(dir, "jre", "bin")
	if err := os.MkdirAll(jre, 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(jre, "java.exe"), make([]byte, 100), 0644)

	if got := ResolveExecutable(dir, ""); got != "" {
		t.Errorf("ResolveExecutable = %q, want empty (runtime must be skipped)", got)
	}
}

// TestResolveExecutableNestedDepthBounded guards that the descent stays
// bounded: a game several levels down is not picked up.
func TestResolveExecutableNestedDepthBounded(t *testing.T) {
	dir := t.TempDir()
	deep := filepath.Join(dir, "a", "b", "c")
	if err := os.MkdirAll(deep, 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(deep, "Game.exe"), make([]byte, 100), 0644)

	if got := ResolveExecutable(dir, ""); got != "" {
		t.Errorf("ResolveExecutable = %q, want empty (depth-bounded)", got)
	}
}
