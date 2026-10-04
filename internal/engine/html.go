package engine

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// HTML games have no native executable — their entry point is an HTML page
// opened in a browser. The helpers here are shared by the scanner (which
// records the entry page as a game's exe path) and the launcher (which opens
// it in the browser rather than exec'ing it).

// htmlSniffHead / htmlSniffTail bound the content-sniff read. Game entry
// HTML pages (Twine exports, canvas apps) put their signature either near the
// top (Twine <tw-storydata>, framework script tags) or at the end (Twine 1
// story data), and can be tens of MB, so a bounded head+tail read is used
// rather than reading the whole file.
const (
	htmlSniffHead = 256 << 10
	htmlSniffTail = 128 << 10
)

// htmlGameMarkers are lowercase substrings that mark an HTML file as a
// playable game rather than a static page, a docs site, or a viewer. Matched
// case-insensitively against a bounded head+tail read.
var htmlGameMarkers = [][]byte{
	[]byte("tw-storydata"), // Twine (all story formats)
	[]byte("sugarcube"),    // Twine SugarCube
	[]byte("harlowe"),      // Twine Harlowe
	[]byte("snowman"),      // Twine Snowman
	[]byte("<canvas"),      // canvas-driven games
	[]byte("pixi"),         // PixiJS
	[]byte("phaser"),       // Phaser
	[]byte("createjs"),     // CreateJS (Flash ports)
	[]byte("easeljs"),      // CreateJS
	[]byte("babylon"),      // Babylon.js
	[]byte("rpg_core.js"),  // RPG Maker MV/MZ web build
	[]byte("rpgmaker"),     // RPG Maker web
	[]byte("unityloader"),  // Unity WebGL
	[]byte("godot"),        // Godot web export
	[]byte("gamefiles"),    // hand-rolled JS games (e.g. Hentai University)
	[]byte("js/engine/"),   // hand-rolled JS game engines (e.g. A Lot of Ways)
}

// IsHTMLGameFile reports whether path is an .html/.htm file whose bounded
// head+tail content carries a recognizable playable-game signature. A bare
// index.html (documentation page) returns false.
func IsHTMLGameFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".html" && ext != ".htm" {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, htmlSniffHead)
	n, _ := io.ReadFull(f, buf)
	if containsHTMLMarker(bytes.ToLower(buf[:n])) {
		return true
	}

	fi, err := f.Stat()
	if err != nil || fi.Size() <= int64(htmlSniffHead) {
		return false
	}
	off := fi.Size() - int64(htmlSniffTail)
	if off < 0 {
		off = 0
	}
	tail := make([]byte, htmlSniffTail)
	if _, err := f.ReadAt(tail, off); err != nil {
		return false
	}
	return containsHTMLMarker(bytes.ToLower(tail))
}

// containsHTMLMarker reports whether data (lowercased) contains any marker.
func containsHTMLMarker(data []byte) bool {
	for _, m := range htmlGameMarkers {
		if bytes.Contains(data, m) {
			return true
		}
	}
	return false
}

// FindHTMLEntry returns the best HTML entry point for a game directory at
// dir, or "" when the directory holds no .html/.htm file. Preference order:
// a root index.html (case-insensitive), then the largest file carrying a game
// signature, then the largest HTML file. Only the top level is searched —
// a game's entry page lives at its root.
func FindHTMLEntry(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	var candidates []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".html" && ext != ".htm" {
			continue
		}
		candidates = append(candidates, filepath.Join(dir, e.Name()))
	}
	if len(candidates) == 0 {
		return ""
	}

	// 1. A root index.html is the conventional entry point.
	for _, p := range candidates {
		if strings.EqualFold(filepath.Base(p), "index.html") {
			return p
		}
	}

	// 2. Otherwise prefer the largest file carrying a game signature.
	best := ""
	var bestSize int64 = -1
	for _, p := range candidates {
		if !IsHTMLGameFile(p) {
			continue
		}
		if fi, err := os.Stat(p); err == nil && fi.Size() > bestSize {
			bestSize = fi.Size()
			best = p
		}
	}
	if best != "" {
		return best
	}

	// 3. Fall back to the largest HTML file.
	for _, p := range candidates {
		if fi, err := os.Stat(p); err == nil && fi.Size() > bestSize {
			bestSize = fi.Size()
			best = p
		}
	}
	return best
}
