package main

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// resolveCookie
// ---------------------------------------------------------------------------

func TestResolveCookie_Explicit(t *testing.T) {
	t.Parallel()
	// Explicit cookie string should be returned directly.
	got := resolveCookie("my-cookie-value", "")
	if got != "my-cookie-value" {
		t.Errorf("resolveCookie(%q, \"\") = %q, want %q", "my-cookie-value", got, "my-cookie-value")
	}
}

func TestResolveCookie_ExplicitTakesPriority(t *testing.T) {
	t.Parallel()
	// When both explicit and file are provided, explicit should win.
	got := resolveCookie("explicit-value", "some-file.txt")
	if got != "explicit-value" {
		t.Errorf("resolveCookie should prefer explicit, got %q", got)
	}
}

func TestResolveCookie_File(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cookieFile := filepath.Join(dir, "cookie.txt")
	if err := os.WriteFile(cookieFile, []byte("  file-cookie-value  \n"), 0644); err != nil {
		t.Fatal(err)
	}

	got := resolveCookie("", cookieFile)
	if got != "file-cookie-value" {
		t.Errorf("resolveCookie(\"\", %q) = %q, want %q", cookieFile, got, "file-cookie-value")
	}
}

func TestResolveCookie_FileNotFound(t *testing.T) {
	t.Parallel()
	got := resolveCookie("", "/nonexistent/cookie.txt")
	if got != "" {
		t.Errorf("resolveCookie with missing file should return empty, got %q", got)
	}
}

func TestResolveCookie_EmptyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cookieFile := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(cookieFile, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	got := resolveCookie("", cookieFile)
	if got != "" {
		t.Errorf("resolveCookie with empty file should return empty, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// stripThreadPrefix
// ---------------------------------------------------------------------------

func TestStripThreadPrefix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"RPGM My Game", "My Game"},
		{"Ren'Py Game Title", "Game Title"},
		{"Unity 3D Game", "3D Game"},
		{"Completed Visual Novel", "Visual Novel"},
		{"Abandoned Old Game", "Old Game"},
		{"HTML5 Web Game", "Web Game"},
		{"Flash Game", "Game"},
		{"Java Game", "Game"},
		{"Godot Game", "Game"},
		{"No Prefix At All", "No Prefix At All"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := stripThreadPrefix(tt.input)
			if got != tt.want {
				t.Errorf("stripThreadPrefix(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestStripThreadPrefix_MultiplePrefixes(t *testing.T) {
	t.Parallel()
	// Multiple consecutive prefix words should all be stripped.
	got := stripThreadPrefix("RPGM Completed My Game")
	if got != "My Game" {
		t.Errorf("stripThreadPrefix(RPGM Completed My Game) = %q, want %q", got, "My Game")
	}
}

func TestStripThreadPrefix_NoClearTitle(t *testing.T) {
	t.Parallel()
	// If stripping removes everything, the original should be returned.
	got := stripThreadPrefix("Unity")
	if got != "Unity" {
		t.Errorf("stripThreadPrefix('Unity') = %q, want %q", got, "Unity")
	}
}
