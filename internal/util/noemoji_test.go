package util

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestNoEmojiInSource guards the UI against emoji glyphs. Moxie uses plain
// text or non-emoji Unicode symbols (arrows, box drawing, ✓ ✗ ✕) only, because
// emoji render inconsistently (colour bitmaps, double width) across terminals
// and webview fonts.
//
// Scanned: Go sources under internal/, cmd/, main.go and desktop/*.go, plus the
// desktop frontend sources (*.svelte, *.js, *.css) excluding generated wailsjs/
// bindings and node_modules.
func TestNoEmojiInSource(t *testing.T) {
	root := repoRoot(t)

	var files []string
	addGlob := func(pattern string) {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			t.Fatalf("glob %s: %v", pattern, err)
		}
		files = append(files, matches...)
	}
	walk := func(dir string, exts ...string) {
		dir = filepath.Join(root, dir)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return
		}
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				switch d.Name() {
				case "node_modules", "wailsjs", "dist", "testdata":
					return filepath.SkipDir
				}
				return nil
			}
			for _, ext := range exts {
				if strings.HasSuffix(path, ext) {
					files = append(files, path)
					break
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}

	walk("internal", ".go")
	walk("cmd", ".go")
	addGlob("main.go")
	addGlob("desktop/*.go")
	walk("desktop/frontend/src", ".svelte", ".js", ".css")

	if len(files) == 0 {
		t.Fatal("no files scanned; repo root detection is broken")
	}

	var violations []string
	for _, path := range files {
		violations = append(violations, scanFileForEmoji(t, root, path)...)
	}
	if len(violations) > 0 {
		t.Errorf("found %d emoji glyph(s); use plain text or non-emoji symbols instead:\n%s",
			len(violations), strings.Join(violations, "\n"))
	}
}

func scanFileForEmoji(t *testing.T, root, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	rel, _ := filepath.Rel(root, path)
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for lineNo := 1; sc.Scan(); lineNo++ {
		runes := []rune(sc.Text())
		for i, r := range runes {
			var next rune
			if i+1 < len(runes) {
				next = runes[i+1]
			}
			if isForbiddenGlyph(r, next) {
				out = append(out, fmt.Sprintf("  %s:%d: %q (U+%04X)", rel, lineNo, string(r), r))
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return out
}

// textSymbolAllowList holds codepoints inside the forbidden ranges that have
// no emoji presentation and are used intentionally as text symbols.
var textSymbolAllowList = map[rune]bool{
	'✓': true, // ✓ check mark
	'✕': true, // ✕ multiplication x
	'✗': true, // ✗ ballot x
	'☰': true, // ☰ trigram (list-view icon)
}

// isForbiddenGlyph reports whether r is an emoji (or emoji-prone) codepoint.
// next is the following rune (0 at end of line); U+25B6/U+25C0 are allowed
// only when forced to text presentation with U+FE0E.
func isForbiddenGlyph(r, next rune) bool {
	switch {
	case textSymbolAllowList[r]:
		return false
	case r >= 0x1F000 && r <= 0x1FAFF: // pictographs, emoticons, transport, etc.
		return true
	case r >= 0x2600 && r <= 0x27BF: // misc symbols + dingbats (warning sign, sun, sparkles ...)
		return true
	case r >= 0x2B00 && r <= 0x2BFF: // misc symbols and arrows (down/up arrows, star)
		return true
	case r >= 0x23E9 && r <= 0x23FA: // media controls (pause, fast-forward, stopwatch ...)
		return true
	case r == 0xFE0F: // emoji presentation selector
		return true
	case r == 0x25B6 || r == 0x25C0: // U+25B6/U+25C0 render as emoji without U+FE0E
		return next != 0xFE0E
	}
	return false
}

// repoRoot walks up from this source file to the directory holding go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above " + file)
		}
		dir = parent
	}
}

func TestIsForbiddenGlyph(t *testing.T) {
	cases := []struct {
		r, next rune
		want    bool
	}{
		{'\u26A0', 0, true},
		{'\U0001F504', 0, true},
		{'\u2B07', 0, true},
		{'\u23F8', 0, true},
		{'\uFE0F', 0, true},
		{'\u25B6', ' ', true},
		{'\u25B6', '\uFE0E', false},
		{'✓', 0, false},
		{'✗', 0, false},
		{'↑', 0, false},
		{'↓', 0, false},
		{'•', 0, false},
		{'─', 0, false},
		{'‖', 0, false},
	}
	for _, c := range cases {
		if got := isForbiddenGlyph(c.r, c.next); got != c.want {
			t.Errorf("isForbiddenGlyph(%q, %q) = %v, want %v", c.r, c.next, got, c.want)
		}
	}
}
