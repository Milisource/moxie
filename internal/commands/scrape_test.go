package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/scraper"
)

// ---------------------------------------------------------------------------
// ResolveCookie
// ---------------------------------------------------------------------------

func TestResolveCookie_Explicit(t *testing.T) {
	t.Parallel()
	// Explicit cookie string should be returned directly.
	got := ResolveCookie("my-cookie-value", "")
	if got != "my-cookie-value" {
		t.Errorf("ResolveCookie(%q, \"\") = %q, want %q", "my-cookie-value", got, "my-cookie-value")
	}
}

func TestResolveCookie_ExplicitTakesPriority(t *testing.T) {
	t.Parallel()
	// When both explicit and file are provided, explicit should win.
	got := ResolveCookie("explicit-value", "some-file.txt")
	if got != "explicit-value" {
		t.Errorf("ResolveCookie should prefer explicit, got %q", got)
	}
}

func TestResolveCookie_File(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cookieFile := filepath.Join(dir, "cookie.txt")
	if err := os.WriteFile(cookieFile, []byte("  file-cookie-value  \n"), 0644); err != nil {
		t.Fatal(err)
	}

	got := ResolveCookie("", cookieFile)
	if got != "file-cookie-value" {
		t.Errorf("ResolveCookie(\"\", %q) = %q, want %q", cookieFile, got, "file-cookie-value")
	}
}

func TestResolveCookie_FileNotFound(t *testing.T) {
	t.Parallel()
	got := ResolveCookie("", "/nonexistent/cookie.txt")
	if got != "" {
		t.Errorf("ResolveCookie with missing file should return empty, got %q", got)
	}
}

func TestResolveCookie_EmptyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cookieFile := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(cookieFile, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	got := ResolveCookie("", cookieFile)
	if got != "" {
		t.Errorf("ResolveCookie with empty file should return empty, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// StripThreadPrefix
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
			got := scraper.StripThreadPrefix(tt.input)
			if got != tt.want {
				t.Errorf("StripThreadPrefix(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestStripThreadPrefix_MultiplePrefixes(t *testing.T) {
	t.Parallel()
	// Multiple consecutive prefix words should all be stripped.
	got := scraper.StripThreadPrefix("RPGM Completed My Game")
	if got != "My Game" {
		t.Errorf("StripThreadPrefix(RPGM Completed My Game) = %q, want %q", got, "My Game")
	}
}

func TestStripThreadPrefix_NoClearTitle(t *testing.T) {
	t.Parallel()
	// If stripping removes everything, the original should be returned.
	got := scraper.StripThreadPrefix("Unity")
	if got != "Unity" {
		t.Errorf("StripThreadPrefix('Unity') = %q, want %q", got, "Unity")
	}
}

// ---------------------------------------------------------------------------
// ApplyThreadData — StoreLinks and SteamAppID
// ---------------------------------------------------------------------------

func TestApplyThreadData_WithStoreLinks(t *testing.T) {
	t.Parallel()

	game := &db.Game{}
	data := &scraper.ThreadData{
		StoreLinks: map[string]string{
			"steam": "https://store.steampowered.com/app/12345/",
			"itch":  "https://dev.itch.io/game",
		},
		ThreadID: 100,
	}

	scraper.ApplyThreadData(game, data, "https://f95zone.to/threads/test.100/")

	if len(game.StoreLinks) != 2 {
		t.Fatalf("expected 2 store links, got %d: %v", len(game.StoreLinks), game.StoreLinks)
	}
	if game.StoreLinks["steam"] != "https://store.steampowered.com/app/12345/" {
		t.Errorf(`StoreLinks["steam"] = %q, want %q`,
			game.StoreLinks["steam"], "https://store.steampowered.com/app/12345/")
	}
	if game.StoreLinks["itch"] != "https://dev.itch.io/game" {
		t.Errorf(`StoreLinks["itch"] = %q, want %q`,
			game.StoreLinks["itch"], "https://dev.itch.io/game")
	}
}

func TestApplyThreadData_SteamAppIDExtracted(t *testing.T) {
	t.Parallel()

	game := &db.Game{}
	data := &scraper.ThreadData{
		StoreLinks: map[string]string{
			"steam": "https://store.steampowered.com/app/54321/GameName/",
		},
		ThreadID: 200,
	}

	scraper.ApplyThreadData(game, data, "https://f95zone.to/threads/test.200/")

	if game.SteamAppID != 54321 {
		t.Errorf("SteamAppID = %d, want 54321", game.SteamAppID)
	}
	if game.StoreLinks["steam"] != "https://store.steampowered.com/app/54321/GameName/" {
		t.Errorf(`StoreLinks["steam"] = %q, want %q`,
			game.StoreLinks["steam"], "https://store.steampowered.com/app/54321/GameName/")
	}
}

func TestApplyThreadData_NoStoreLinks(t *testing.T) {
	t.Parallel()

	game := &db.Game{
		Title:  "Existing Game",
		Engine: "Unity",
	}
	data := &scraper.ThreadData{
		Title:     "Existing Game",
		Version:   "2.0",
		ThreadID:  300,
		StoreLinks: nil, // no store links
	}

	scraper.ApplyThreadData(game, data, "https://f95zone.to/threads/test.300/")

	// StoreLinks should remain nil/unset when ThreadData has none.
	if game.StoreLinks != nil {
		t.Errorf("StoreLinks should be nil when ThreadData has no store links, got %v", game.StoreLinks)
	}
	if game.SteamAppID != 0 {
		t.Errorf("SteamAppID should be 0 without store links, got %d", game.SteamAppID)
	}
}

func TestApplyThreadData_EmptyStoreLinks(t *testing.T) {
	t.Parallel()

	game := &db.Game{
		StoreLinks: map[string]string{"steam": "https://store.steampowered.com/app/old/"},
		SteamAppID: 999,
	}
	data := &scraper.ThreadData{
		StoreLinks: map[string]string{}, // empty map, not nil
		ThreadID:   400,
	}

	scraper.ApplyThreadData(game, data, "https://f95zone.to/threads/test.400/")

	// When ThreadData has empty StoreLinks map, game should keep its links
	// because the condition is len(data.StoreLinks) > 0.
	if len(game.StoreLinks) != 1 {
		t.Fatalf("expected StoreLinks to be preserved, got %v", game.StoreLinks)
	}
	if game.StoreLinks["steam"] != "https://store.steampowered.com/app/old/" {
		t.Errorf(`StoreLinks["steam"] = %q, want original %q`,
			game.StoreLinks["steam"], "https://store.steampowered.com/app/old/")
	}
	if game.SteamAppID != 999 {
		t.Errorf("SteamAppID should be preserved, got %d", game.SteamAppID)
	}
}

func TestApplyThreadData_SteamURLWithoutAppID(t *testing.T) {
	t.Parallel()

	game := &db.Game{}
	data := &scraper.ThreadData{
		StoreLinks: map[string]string{
			// Steam URL that doesn't contain /app/\d+ pattern
			"steam": "https://store.steampowered.com/curator/12345/",
		},
		ThreadID: 500,
	}

	scraper.ApplyThreadData(game, data, "https://f95zone.to/threads/test.500/")

	// StoreLinks should still be set (the URL matched the steam matcher).
	if v, ok := game.StoreLinks["steam"]; !ok {
		t.Fatal("expected steam store link to be set")
	} else if v != "https://store.steampowered.com/curator/12345/" {
		t.Errorf("steam link = %q", v)
	}

	// But SteamAppID should be 0 because the URL doesn't contain a valid /app/\d+/ pattern.
	if game.SteamAppID != 0 {
		t.Errorf("SteamAppID should be 0 for malformed steam URL, got %d", game.SteamAppID)
	}
}

func TestApplyThreadData_NonSteamStoreLinkDoesNotSetSteamAppID(t *testing.T) {
	t.Parallel()

	game := &db.Game{}
	data := &scraper.ThreadData{
		StoreLinks: map[string]string{
			"itch": "https://dev.itch.io/game",
		},
		ThreadID: 600,
	}

	scraper.ApplyThreadData(game, data, "https://f95zone.to/threads/test.600/")

	if _, ok := game.StoreLinks["itch"]; !ok {
		t.Fatal("expected itch store link")
	}
	if game.SteamAppID != 0 {
		t.Errorf("SteamAppID should be 0 for non-steam store links, got %d", game.SteamAppID)
	}
}

func TestApplyThreadData_StoreLinksOverwriteExisting(t *testing.T) {
	t.Parallel()

	game := &db.Game{
		StoreLinks: map[string]string{
			"steam": "https://store.steampowered.com/app/old/",
		},
		SteamAppID: 111,
	}
	data := &scraper.ThreadData{
		StoreLinks: map[string]string{
			"steam": "https://store.steampowered.com/app/222/",
			"itch":  "https://new.itch.io/game",
		},
		ThreadID: 700,
	}

	scraper.ApplyThreadData(game, data, "https://f95zone.to/threads/test.700/")

	if len(game.StoreLinks) != 2 {
		t.Fatalf("expected 2 store links, got %d: %v", len(game.StoreLinks), game.StoreLinks)
	}
	if game.StoreLinks["steam"] != "https://store.steampowered.com/app/222/" {
		t.Errorf(`StoreLinks["steam"] = %q, want %q`,
			game.StoreLinks["steam"], "https://store.steampowered.com/app/222/")
	}
	if game.StoreLinks["itch"] != "https://new.itch.io/game" {
		t.Errorf(`StoreLinks["itch"] = %q, want %q`,
			game.StoreLinks["itch"], "https://new.itch.io/game")
	}
	// SteamAppID should be updated from the new steam URL.
	if game.SteamAppID != 222 {
		t.Errorf("SteamAppID = %d, want 222", game.SteamAppID)
	}
}
