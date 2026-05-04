package steam

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	vdf "github.com/wakeful-cloud/vdf"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// testShortcutsVDF serializes ShortcutEntry values to binary VDF bytes.
// Tests call this to build in-memory fixtures instead of committing binary files.
func testShortcutsVDF(t *testing.T, entries []ShortcutEntry) []byte {
	t.Helper()
	m := buildShortcutsMap(entries)
	data, err := vdf.WriteVdf(m)
	if err != nil {
		t.Fatalf("vdf.WriteVdf: %v", err)
	}
	return data
}

// ---------------------------------------------------------------------------
// 1. GenerateAppID
// ---------------------------------------------------------------------------

func TestGenerateAppID(t *testing.T) {
	// Deterministic: same inputs -> same output.
	id1a := GenerateAppID("/path/to/game.exe", "My Game")
	id1b := GenerateAppID("/path/to/game.exe", "My Game")
	if id1a != id1b {
		t.Errorf("expected deterministic AppID, got %d vs %d", id1a, id1b)
	}

	// Different inputs -> different output.
	id2 := GenerateAppID("/other.exe", "Other Game")
	if id1a == id2 {
		t.Errorf("expected different AppIDs for different (exe, appName) pairs")
	}

	// High bit (0x80000000) always set for every input combination.
	tests := []struct {
		exe, name string
	}{
		{"/a.exe", "A"},
		{"/b.exe", "B"},
		{"", ""},
		{"/path/with spaces/game.exe", "My Game"},
		{`C:\Windows\game.exe`, "Windows Game"},
		{"relative/path", ""},
	}
	for _, tc := range tests {
		id := GenerateAppID(tc.exe, tc.name)
		if id&0x80000000 == 0 {
			t.Errorf("GenerateAppID(%q, %q) = %d (0x%08X): high bit not set",
				tc.exe, tc.name, id, id)
		}
	}

	// No collisions across 10 000 distinct (exe, appName) pairs.
	seen := make(map[uint32]bool)
	for i := 0; i < 10000; i++ {
		exe := fmt.Sprintf("/game/%d/exe", i)
		name := fmt.Sprintf("Game %d", i)
		id := GenerateAppID(exe, name)
		if seen[id] {
			t.Errorf("collision at iteration %d: %d (0x%08X)", i, id, id)
		}
		seen[id] = true
	}
}

// ---------------------------------------------------------------------------
// 2. ArtType Suffix
// ---------------------------------------------------------------------------

func TestArtType_Suffix(t *testing.T) {
	tests := []struct {
		art    ArtType
		suffix string
	}{
		{ArtVertical, "p"},
		{ArtHorizontal, ""},
		{ArtHero, "_hero"},
		{ArtLogo, "_logo"},
		{ArtIcon, "_icon"},
	}
	for _, tc := range tests {
		got := tc.art.Suffix()
		if got != tc.suffix {
			t.Errorf("ArtType(%d).Suffix() = %q, want %q", tc.art, got, tc.suffix)
		}
	}

	// Unknown type returns empty string.
	if ArtType(99).Suffix() != "" {
		t.Error("unknown ArtType.Suffix() should be empty")
	}
}

// ---------------------------------------------------------------------------
// 3. ArtType Dimensions
// ---------------------------------------------------------------------------

func TestArtType_Dimensions(t *testing.T) {
	tests := []struct {
		art      ArtType
		w, h     int
	}{
		{ArtVertical, 600, 900},
		{ArtHorizontal, 460, 215},
		{ArtHero, 1920, 620},
		{ArtLogo, 0, 0},
		{ArtIcon, 0, 0},
	}
	for _, tc := range tests {
		w, h := tc.art.Dimensions()
		if w != tc.w || h != tc.h {
			t.Errorf("ArtType(%d).Dimensions() = (%d,%d), want (%d,%d)",
				tc.art, w, h, tc.w, tc.h)
		}
	}

	// Unknown type returns (0,0).
	w, h := ArtType(99).Dimensions()
	if w != 0 || h != 0 {
		t.Errorf("unknown ArtType.Dimensions() = (%d,%d), want (0,0)", w, h)
	}
}

// ---------------------------------------------------------------------------
// 4. GridFilePath
// ---------------------------------------------------------------------------

func TestGridFilePath(t *testing.T) {
	// Vertical grid: 0x81234567 = 2166572391 -> "2166572391p.png".
	got := GridFilePath("/home/.steam/steam", 123, 0x81234567, ArtVertical)
	want := filepath.Join("/home/.steam/steam", "userdata", "123", "config", "grid", "2166572391p.png")
	if got != want {
		t.Errorf("GridFilePath = %q, want %q", got, want)
	}

	// Horizontal grid (no suffix).
	got = GridFilePath("/steam", 1, 42, ArtHorizontal)
	want = filepath.Join("/steam", "userdata", "1", "config", "grid", "42.png")
	if got != want {
		t.Errorf("GridFilePath = %q, want %q", got, want)
	}

	// Hero grid (_hero suffix).
	got = GridFilePath("/steam", 999, 12345, ArtHero)
	want = filepath.Join("/steam", "userdata", "999", "config", "grid", "12345_hero.png")
	if got != want {
		t.Errorf("GridFilePath = %q, want %q", got, want)
	}

	// Logo grid (_logo suffix).
	got = GridFilePath("/steam", 777, 54321, ArtLogo)
	want = filepath.Join("/steam", "userdata", "777", "config", "grid", "54321_logo.png")
	if got != want {
		t.Errorf("GridFilePath = %q, want %q", got, want)
	}

	// Icon grid (_icon suffix).
	got = GridFilePath("/steam", 111, 99999, ArtIcon)
	want = filepath.Join("/steam", "userdata", "111", "config", "grid", "99999_icon.png")
	if got != want {
		t.Errorf("GridFilePath = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// 5. quotePath / stripQuotes
// ---------------------------------------------------------------------------

func TestQuotePath_StripQuotes(t *testing.T) {
	paths := []string{
		"/path/to/exe",
		"/path/with spaces/exe",
		`C:\Program Files\game.exe`,
		"/home/user/.local/share/Steam/steamapps/common/My Game/game.exe",
		"",
		"/",
		"a",
	}
	for _, p := range paths {
		quoted := quotePath(p)
		unquoted := stripQuotes(quoted)
		if unquoted != p {
			t.Errorf("round-trip failed:\n  quotePath(%q)  = %q\n  stripQuotes(...) = %q",
				p, quoted, unquoted)
		}
	}

	// stripQuotes on already-unquoted string is a no-op.
	if got := stripQuotes("noquotes"); got != "noquotes" {
		t.Errorf("stripQuotes('noquotes') = %q, want 'noquotes'", got)
	}

	// stripQuotes on string with only one quote.
	if got := stripQuotes(`"partial`); got != `"partial` {
		t.Errorf("stripQuotes('\"partial') = %q, want '\"partial'", got)
	}

	// quotePath on empty string returns empty.
	if got := quotePath(""); got != "" {
		t.Errorf("quotePath('') = %q, want ''", got)
	}
}

// ---------------------------------------------------------------------------
// 6. Add / Find / Remove game
// ---------------------------------------------------------------------------

func TestAddGame_FindGame_RemoveGame(t *testing.T) {
	var shortcuts []ShortcutEntry

	// Add a game.
	entry := &ShortcutEntry{
		AppName:  "Test Game",
		Exe:      "/path/to/game.exe",
		StartDir: "/path/to",
		Tags:     []string{"rpg", "adventure"},
	}
	if err := AddGame(&shortcuts, entry); err != nil {
		t.Fatalf("AddGame: %v", err)
	}
	if len(shortcuts) != 1 {
		t.Fatalf("expected 1 shortcut after AddGame, got %d", len(shortcuts))
	}

	// AppID should have been auto-generated.
	gameAppID := shortcuts[0].AppID
	if gameAppID == 0 {
		t.Fatal("AddGame should generate a non-zero AppID")
	}
	if gameAppID&0x80000000 == 0 {
		t.Error("generated AppID should have high bit set")
	}

	// F95Zone should be prepended as first tag.
	if len(shortcuts[0].Tags) == 0 || shortcuts[0].Tags[0] != "F95Zone" {
		t.Errorf("expected F95Zone as first tag, got %v", shortcuts[0].Tags)
	}
	// Original tags follow, deduplicated.
	if len(shortcuts[0].Tags) != 3 {
		t.Errorf("expected 3 tags (F95Zone + 2 original), got %d: %v",
			len(shortcuts[0].Tags), shortcuts[0].Tags)
	}

	// Find by title (case-insensitive).
	found := FindGame(shortcuts, "test game")
	if found == nil {
		t.Fatal("FindGame('test game') returned nil")
	}
	if found.AppID != gameAppID {
		t.Errorf("FindGame AppID = %d, want %d", found.AppID, gameAppID)
	}

	// Find by AppID.
	found = FindGameByAppID(shortcuts, gameAppID)
	if found == nil {
		t.Fatal("FindGameByAppID returned nil")
	}
	if !strings.EqualFold(found.AppName, "Test Game") {
		t.Errorf("AppName = %q, want %q", found.AppName, "Test Game")
	}

	// Non-existent title returns nil.
	if found := FindGame(shortcuts, "does not exist"); found != nil {
		t.Error("FindGame for non-existent title should return nil")
	}

	// Non-existent AppID returns nil.
	if found := FindGameByAppID(shortcuts, 0); found != nil {
		t.Error("FindGameByAppID for non-existent ID should return nil")
	}

	// Remove by AppID.
	if err := RemoveGame(&shortcuts, gameAppID); err != nil {
		t.Fatalf("RemoveGame: %v", err)
	}
	if len(shortcuts) != 0 {
		t.Errorf("expected 0 shortcuts after RemoveGame, got %d", len(shortcuts))
	}

	// Find after remove returns nil.
	if found := FindGame(shortcuts, "Test Game"); found != nil {
		t.Error("FindGame after remove should return nil")
	}

	// Idempotent remove -- removing again must not error.
	if err := RemoveGame(&shortcuts, gameAppID); err != nil {
		t.Errorf("idempotent RemoveGame should return nil, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// 7. AddGame duplicate detection
// ---------------------------------------------------------------------------

func TestAddGame_Duplicate(t *testing.T) {
	var shortcuts []ShortcutEntry

	// Add first game.
	entry1 := &ShortcutEntry{AppName: "My Game", Exe: "/game.exe"}
	if err := AddGame(&shortcuts, entry1); err != nil {
		t.Fatalf("first AddGame: %v", err)
	}

	// Add second game with different name -- must succeed.
	entry2 := &ShortcutEntry{AppName: "Other Game", Exe: "/other.exe"}
	if err := AddGame(&shortcuts, entry2); err != nil {
		t.Fatalf("second AddGame: %v", err)
	}

	// Add third game with same title (different case) -- must return ErrDuplicate.
	entry3 := &ShortcutEntry{AppName: "my game", Exe: "/game.exe"}
	if err := AddGame(&shortcuts, entry3); !errors.Is(err, ErrDuplicate) {
		t.Errorf("expected ErrDuplicate for duplicate title, got %v", err)
	}

	// Slice length must not have grown.
	if len(shortcuts) != 2 {
		t.Errorf("expected 2 shortcuts after duplicate rejection, got %d", len(shortcuts))
	}

	// Adding a completely different name still works after a duplicate attempt.
	entry4 := &ShortcutEntry{AppName: "Third Game", Exe: "/third.exe"}
	if err := AddGame(&shortcuts, entry4); err != nil {
		t.Fatalf("AddGame after duplicate: %v", err)
	}
	if len(shortcuts) != 3 {
		t.Errorf("expected 3 shortcuts, got %d", len(shortcuts))
	}
}

// ---------------------------------------------------------------------------
// 8. Round-trip: buildShortcutsMap -> vdf.WriteVdf -> vdf.ReadVdf -> parseShortcutsMap
// ---------------------------------------------------------------------------

func TestParseShortcutsMap_RoundTrip(t *testing.T) {
	original := []ShortcutEntry{
		{
			AppID:              0xABCD1234,
			AppName:            "Test Game",
			Exe:                "/path/to/game.exe",
			StartDir:           "/path/to",
			Icon:               "",
			LaunchOptions:      "-windowed --fullscreen",
			IsHidden:           false,
			AllowDesktopConfig: true,
			AllowOverlay:       true,
			OpenVR:             false,
			Devkit:             false,
			LastPlayTime:       12345678,
			FlatpakAppID:       "",
			SortAs:             "testgame",
			Tags:               []string{"rpg", "adventure", "fantasy"},
		},
		{
			AppID:              0x5678DEAD,
			AppName:            "Another Game",
			Exe:                "/path/to/another.exe",
			StartDir:           "/path/to/another",
			Icon:               "/path/to/icon.png",
			LaunchOptions:      "",
			IsHidden:           true,
			AllowDesktopConfig: false,
			AllowOverlay:       false,
			OpenVR:             true,
			Devkit:             false,
			LastPlayTime:       0,
			FlatpakAppID:       "com.example.game",
			SortAs:             "",
			Tags:               []string{},
		},
	}

	// Serialize.
	data := testShortcutsVDF(t, original)

	// Deserialize.
	m, err := vdf.ReadVdf(data)
	if err != nil {
		t.Fatalf("vdf.ReadVdf: %v", err)
	}
	parsed := parseShortcutsMap(m)

	if len(parsed) != len(original) {
		t.Fatalf("expected %d shortcuts, got %d", len(original), len(parsed))
	}

	for i, want := range original {
		got := parsed[i]

		if got.AppID != want.AppID {
			t.Errorf("parsed[%d].AppID = %d, want %d", i, got.AppID, want.AppID)
		}

		if got.AppName != want.AppName {
			t.Errorf("parsed[%d].AppName = %q, want %q", i, got.AppName, want.AppName)
		}
		if got.Exe != want.Exe {
			t.Errorf("parsed[%d].Exe = %q, want %q", i, got.Exe, want.Exe)
		}
		if got.StartDir != want.StartDir {
			t.Errorf("parsed[%d].StartDir = %q, want %q", i, got.StartDir, want.StartDir)
		}
		if got.Icon != want.Icon {
			t.Errorf("parsed[%d].Icon = %q, want %q", i, got.Icon, want.Icon)
		}
		if got.LaunchOptions != want.LaunchOptions {
			t.Errorf("parsed[%d].LaunchOptions = %q, want %q", i, got.LaunchOptions, want.LaunchOptions)
		}
		if got.IsHidden != want.IsHidden {
			t.Errorf("parsed[%d].IsHidden = %v, want %v", i, got.IsHidden, want.IsHidden)
		}
		if got.AllowDesktopConfig != want.AllowDesktopConfig {
			t.Errorf("parsed[%d].AllowDesktopConfig = %v, want %v", i, got.AllowDesktopConfig, want.AllowDesktopConfig)
		}
		if got.AllowOverlay != want.AllowOverlay {
			t.Errorf("parsed[%d].AllowOverlay = %v, want %v", i, got.AllowOverlay, want.AllowOverlay)
		}
		if got.OpenVR != want.OpenVR {
			t.Errorf("parsed[%d].OpenVR = %v, want %v", i, got.OpenVR, want.OpenVR)
		}
		if got.Devkit != want.Devkit {
			t.Errorf("parsed[%d].Devkit = %v, want %v", i, got.Devkit, want.Devkit)
		}
		if got.LastPlayTime != want.LastPlayTime {
			t.Errorf("parsed[%d].LastPlayTime = %d, want %d", i, got.LastPlayTime, want.LastPlayTime)
		}
		if got.FlatpakAppID != want.FlatpakAppID {
			t.Errorf("parsed[%d].FlatpakAppID = %q, want %q", i, got.FlatpakAppID, want.FlatpakAppID)
		}
		if got.SortAs != want.SortAs {
			t.Errorf("parsed[%d].SortAs = %q, want %q", i, got.SortAs, want.SortAs)
		}

		if len(got.Tags) != len(want.Tags) {
			t.Errorf("parsed[%d].Tags = %v, want %v", i, got.Tags, want.Tags)
		} else {
			for j := range got.Tags {
				if got.Tags[j] != want.Tags[j] {
					t.Errorf("parsed[%d].Tags[%d] = %q, want %q", i, j, got.Tags[j], want.Tags[j])
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 9. Gapped indices in shortcuts map
// ---------------------------------------------------------------------------

func TestParseShortcutsMap_GappedIndices(t *testing.T) {
	shortcuts := vdf.Map{
		"shortcuts": vdf.Map{
			"0": vdf.Map{
				"appid":    uint32(1001),
				"AppName":  "Game A",
				"exe":      `"/a.exe"`,
				"StartDir": `"/a"`,
				"tags":     vdf.Map{},
			},
			"2": vdf.Map{
				"appid":    uint32(1002),
				"AppName":  "Game B",
				"exe":      `"/b.exe"`,
				"StartDir": `"/b"`,
				"tags":     vdf.Map{},
			},
			"3": vdf.Map{
				"appid":    uint32(1003),
				"AppName":  "Game C",
				"exe":      `"/c.exe"`,
				"StartDir": `"/c"`,
				"tags":     vdf.Map{},
			},
		},
	}

	parsed := parseShortcutsMap(shortcuts)
	if len(parsed) != 3 {
		t.Fatalf("expected 3 shortcuts, got %d", len(parsed))
	}

	expected := []struct {
		name  string
		appID uint32
	}{
		{"Game A", 1001},
		{"Game B", 1002},
		{"Game C", 1003},
	}
	for i, e := range expected {
		if parsed[i].AppName != e.name {
			t.Errorf("parsed[%d].AppName = %q, want %q", i, parsed[i].AppName, e.name)
		}
		if parsed[i].AppID != e.appID {
			t.Errorf("parsed[%d].AppID = %d, want %d", i, parsed[i].AppID, e.appID)
		}
	}

	// Empty shortcuts map.
	empty := parseShortcutsMap(vdf.Map{})
	if empty != nil {
		t.Errorf("expected nil for empty map, got %v", empty)
	}

	// Missing "shortcuts" key.
	noKey := parseShortcutsMap(vdf.Map{"other": vdf.Map{}})
	if noKey != nil {
		t.Errorf("expected nil for map without 'shortcuts' key, got %v", noKey)
	}
}

// ---------------------------------------------------------------------------
// 10. getUint32 with various Go types
// ---------------------------------------------------------------------------

func TestGetUint32_Types(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
		want  uint32
	}{
		{"uint32", uint32(42), 42},
		{"uint64", uint64(100), 100},
		{"int32", int32(-1), 0xFFFFFFFF}, // -1 wraps to max uint32
		{"int", int(255), 255},
		{"float64", float64(3.14), 3},            // truncation
		{"float64_large", float64(1e9), 1000000000},
		{"string (unsupported)", "hello", 0},
		{"nil", nil, 0},
		{"bool (unsupported)", true, 0},
		{"vdf.Map (unsupported)", vdf.Map{}, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := vdf.Map{"key": tc.value}
			got := getUint32(m, "key")
			if got != tc.want {
				t.Errorf("getUint32(%T(%v)) = %d, want %d",
					tc.value, tc.value, got, tc.want)
			}
		})
	}

	// Missing key returns 0.
	if got := getUint32(vdf.Map{}, "nonexistent"); got != 0 {
		t.Errorf("getUint32(missing) = %d, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// 11. Tags round-trip
// ---------------------------------------------------------------------------

func TestBuildTagsMap_ParseTags(t *testing.T) {
	original := []string{"action", "rpg", "indie"}

	// Build tags map from slice.
	tm := buildTagsMap(original)

	// Parse tags back from a parent map.
	parent := vdf.Map{"tags": tm}
	parsed := parseTags(parent)

	if len(parsed) != len(original) {
		t.Fatalf("parseTags returned %d tags, want %d: %v", len(parsed), len(original), parsed)
	}
	for i := range original {
		if parsed[i] != original[i] {
			t.Errorf("tag[%d]: got %q, want %q", i, parsed[i], original[i])
		}
	}

	// Empty tag list.
	parsedEmpty := parseTags(vdf.Map{})
	if len(parsedEmpty) != 0 {
		t.Errorf("expected 0 tags for empty input, got %d: %v", len(parsedEmpty), parsedEmpty)
	}

	// Nil tags field in map.
	noTags := vdf.Map{"tags": nil}
	if got := parseTags(noTags); got != nil {
		t.Errorf("expected nil for nil tags field, got %v", got)
	}

	// Single tag.
	single := buildTagsMap([]string{"solo"})
	parsedSingle := parseTags(vdf.Map{"tags": single})
	if len(parsedSingle) != 1 || parsedSingle[0] != "solo" {
		t.Errorf("single tag round-trip: got %v, want [solo]", parsedSingle)
	}

	// Multiple tags (4).
	fourTags := buildTagsMap([]string{"a", "b", "c", "d"})
	parsedFour := parseTags(vdf.Map{"tags": fourTags})
	if len(parsedFour) != 4 {
		t.Errorf("expected 4 tags, got %d: %v", len(parsedFour), parsedFour)
	}
}

// ---------------------------------------------------------------------------
// 12. FindSteamUsers -- no userdata directory
// ---------------------------------------------------------------------------

func TestFindSteamUsers_NoUserdata(t *testing.T) {
	t.Run("nonexistent_directory", func(t *testing.T) {
		_, err := FindSteamUsers("/nonexistent/path/that/does/not/exist")
		if err == nil {
			t.Error("expected error for non-existent directory")
		}
	})

	t.Run("empty_userdata", func(t *testing.T) {
		root := t.TempDir()
		userdata := filepath.Join(root, "userdata")
		if err := os.MkdirAll(userdata, 0755); err != nil {
			t.Fatal(err)
		}
		_, err := FindSteamUsers(root)
		if !errors.Is(err, ErrNoUsers) {
			t.Errorf("expected ErrNoUsers for empty userdata, got %v", err)
		}
	})

	t.Run("non_numeric_dirs_only", func(t *testing.T) {
		root := t.TempDir()
		userdata := filepath.Join(root, "userdata")
		if err := os.MkdirAll(filepath.Join(userdata, "ac"), 0755); err != nil {
			t.Fatal(err)
		}
		_, err := FindSteamUsers(root)
		if !errors.Is(err, ErrNoUsers) {
			t.Errorf("expected ErrNoUsers for non-numeric dirs only, got %v", err)
		}
	})

	t.Run("numeric_dir_no_config", func(t *testing.T) {
		root := t.TempDir()
		userdata := filepath.Join(root, "userdata")
		if err := os.MkdirAll(filepath.Join(userdata, "12345"), 0755); err != nil {
			t.Fatal(err)
		}
		_, err := FindSteamUsers(root)
		if !errors.Is(err, ErrNoUsers) {
			t.Errorf("expected ErrNoUsers for numeric dir without config/, got %v", err)
		}
	})

	t.Run("user_id_zero_skipped", func(t *testing.T) {
		root := t.TempDir()
		userdata := filepath.Join(root, "userdata")
		if err := os.MkdirAll(filepath.Join(userdata, "0", "config"), 0755); err != nil {
			t.Fatal(err)
		}
		_, err := FindSteamUsers(root)
		if !errors.Is(err, ErrNoUsers) {
			t.Errorf("expected ErrNoUsers when only '0' user exists, got %v", err)
		}
	})

	t.Run("single_valid_user_success", func(t *testing.T) {
		root := t.TempDir()
		userdata := filepath.Join(root, "userdata")
		if err := os.MkdirAll(filepath.Join(userdata, "12345", "config"), 0755); err != nil {
			t.Fatal(err)
		}
		users, err := FindSteamUsers(root)
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if len(users) != 1 || users[0] != 12345 {
			t.Errorf("expected [12345], got %v", users)
		}
	})

	t.Run("multiple_users_sorted", func(t *testing.T) {
		root := t.TempDir()
		userdata := filepath.Join(root, "userdata")
		for _, id := range []string{"999", "111", "555"} {
			if err := os.MkdirAll(filepath.Join(userdata, id, "config"), 0755); err != nil {
				t.Fatal(err)
			}
		}
		users, err := FindSteamUsers(root)
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if len(users) != 3 {
			t.Fatalf("expected 3 users, got %d: %v", len(users), users)
		}
		if users[0] != 111 || users[1] != 555 || users[2] != 999 {
			t.Errorf("expected sorted [111, 555, 999], got %v", users)
		}
	})
}

// ---------------------------------------------------------------------------
// 13. ResolveSteamPaths
// ---------------------------------------------------------------------------

func TestResolveSteamPaths(t *testing.T) {
	root := t.TempDir()
	userID3 := uint32(123456789)

	// Create the user directory (needed for ResolveSteamPaths validation).
	userDir := filepath.Join(root, "userdata", fmt.Sprintf("%d", userID3))
	if err := os.MkdirAll(userDir, 0755); err != nil {
		t.Fatal(err)
	}

	paths, err := ResolveSteamPaths(root, userID3)
	if err != nil {
		t.Fatalf("ResolveSteamPaths: %v", err)
	}

	if paths.SteamRoot != root {
		t.Errorf("SteamRoot = %q, want %q", paths.SteamRoot, root)
	}
	if paths.UserID3 != userID3 {
		t.Errorf("UserID3 = %d, want %d", paths.UserID3, userID3)
	}

	wantShortcuts := filepath.Join(root, "userdata", "123456789", "config", "shortcuts.vdf")
	if paths.ShortcutsVDF != wantShortcuts {
		t.Errorf("ShortcutsVDF = %q, want %q", paths.ShortcutsVDF, wantShortcuts)
	}

	wantGrid := filepath.Join(root, "userdata", "123456789", "config", "grid")
	if paths.GridDir != wantGrid {
		t.Errorf("GridDir = %q, want %q", paths.GridDir, wantGrid)
	}

	wantConfig := filepath.Join(root, "config", "config.vdf")
	if paths.ConfigVDF != wantConfig {
		t.Errorf("ConfigVDF = %q, want %q", paths.ConfigVDF, wantConfig)
	}

	// Non-existent user directory returns an error.
	t.Run("nonexistent_user", func(t *testing.T) {
		_, err := ResolveSteamPaths(root, 99999)
		if err == nil {
			t.Error("expected error for non-existent user directory")
		}
	})
}

// ---------------------------------------------------------------------------
// 15. ReadShortcuts / WriteShortcuts round-trip via temp files
// ---------------------------------------------------------------------------

func TestReadShortcuts_WriteShortcuts_RoundTrip(t *testing.T) {
	original := []ShortcutEntry{
		{
			AppName:            "In-Game Game",
			Exe:                "/usr/bin/game",
			StartDir:           "/usr/bin",
			LaunchOptions:      "-opengl",
			AllowDesktopConfig: true,
			AllowOverlay:       true,
			LastPlayTime:       98765,
			SortAs:             "ingame",
			Tags:               []string{"action"},
		},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "shortcuts.vdf")

	// Write.
	if err := WriteShortcuts(path, original); err != nil {
		t.Fatalf("WriteShortcuts: %v", err)
	}

	// Read back.
	parsed, err := ReadShortcuts(path)
	if err != nil {
		t.Fatalf("ReadShortcuts: %v", err)
	}
	if len(parsed) != len(original) {
		t.Fatalf("expected %d shortcuts, got %d", len(original), len(parsed))
	}

	got := parsed[0]
	want := original[0]
	if got.AppName != want.AppName {
		t.Errorf("AppName = %q, want %q", got.AppName, want.AppName)
	}
	if got.Exe != want.Exe {
		t.Errorf("Exe = %q, want %q", got.Exe, want.Exe)
	}
	if got.StartDir != want.StartDir {
		t.Errorf("StartDir = %q, want %q", got.StartDir, want.StartDir)
	}
	if got.LaunchOptions != want.LaunchOptions {
		t.Errorf("LaunchOptions = %q, want %q", got.LaunchOptions, want.LaunchOptions)
	}
	if got.AllowDesktopConfig != want.AllowDesktopConfig {
		t.Errorf("AllowDesktopConfig = %v, want %v", got.AllowDesktopConfig, want.AllowDesktopConfig)
	}
	if got.AllowOverlay != want.AllowOverlay {
		t.Errorf("AllowOverlay = %v, want %v", got.AllowOverlay, want.AllowOverlay)
	}
	if got.LastPlayTime != want.LastPlayTime {
		t.Errorf("LastPlayTime = %d, want %d", got.LastPlayTime, want.LastPlayTime)
	}
	if got.SortAs != want.SortAs {
		t.Errorf("SortAs = %q, want %q", got.SortAs, want.SortAs)
	}
	if len(got.Tags) != len(want.Tags) || (len(got.Tags) > 0 && got.Tags[0] != want.Tags[0]) {
		t.Errorf("Tags = %v, want %v", got.Tags, want.Tags)
	}
}

// ReadShortcuts on a non-existent file returns (nil, nil) -- not an error.
// The file path doesn't exist, so the function returns an empty result.

func TestReadShortcuts_FileNotFound(t *testing.T) {
	entries, err := ReadShortcuts("/nonexistent/path/shortcuts.vdf")
	if err != nil {
		t.Fatalf("ReadShortcuts on non-existent file: %v", err)
	}
	if entries != nil {
		t.Errorf("expected nil for non-existent file, got %v", entries)
	}
}

// ---------------------------------------------------------------------------
// 16. Tag limit enforced by AddGame
// ---------------------------------------------------------------------------

func TestAddGame_TagLimit(t *testing.T) {
	var shortcuts []ShortcutEntry
	entry := &ShortcutEntry{
		AppName: "Tag Limit Test",
		Exe:     "/test",
		Tags:    []string{"a", "b", "c", "d", "e", "f"},
	}
	if err := AddGame(&shortcuts, entry); err != nil {
		t.Fatalf("AddGame: %v", err)
	}
	if len(shortcuts) != 1 {
		t.Fatalf("expected 1 shortcut, got %d", len(shortcuts))
	}
	// F95Zone counts as one, so total should be 4.
	if len(shortcuts[0].Tags) > 4 {
		t.Errorf("Tags exceeded limit: got %d tags: %v", len(shortcuts[0].Tags), shortcuts[0].Tags)
	}
	if shortcuts[0].Tags[0] != "F95Zone" {
		t.Errorf("first tag should be F95Zone, got %q", shortcuts[0].Tags[0])
	}
	// Tags: [F95Zone, a, b, c] (d, e, f cut off)
	if len(shortcuts[0].Tags) >= 2 && shortcuts[0].Tags[1] != "a" {
		t.Errorf("second tag should be 'a', got %q", shortcuts[0].Tags[1])
	}
}

// ---------------------------------------------------------------------------
// 17. isNumeric edge cases
// ---------------------------------------------------------------------------

func TestIsNumeric(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"", false}, // empty string is not numeric
		{"0", true},
		{"12345", true},
		{"9999999999999999", true},
		{"abc", false},
		{"123abc", false},
		{"12.34", false},
		{"-1", false},
		{" 1", false},
		{"1 ", false},
	}
	for _, tc := range tests {
		got := isNumeric(tc.s)
		if got != tc.want {
			t.Errorf("isNumeric(%q) = %v, want %v", tc.s, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// 18. RawFields round-trip preservation
// ---------------------------------------------------------------------------

func TestParseShortcutsMap_RawFieldsPreserved(t *testing.T) {
	// Build a shortcuts map with extra unknown fields.
	shortcuts := vdf.Map{
		"shortcuts": vdf.Map{
			"0": vdf.Map{
				"appid":        uint32(42),
				"AppName":      "RawFields Test",
				"exe":          `"/test"`,
				"StartDir":     `"/test"`,
				"tags":         vdf.Map{},
				"unknown_key":  "preserved_value",
				"another_key":  uint32(99),
			},
		},
	}

	parsed := parseShortcutsMap(shortcuts)
	if len(parsed) != 1 {
		t.Fatalf("expected 1 shortcut, got %d", len(parsed))
	}
	se := parsed[0]

	if se.RawFields == nil {
		t.Fatal("expected non-nil RawFields for unknown keys")
	}

	if v, ok := se.RawFields["unknown_key"].(string); !ok || v != "preserved_value" {
		t.Errorf("RawFields['unknown_key'] = %v (type %T), want 'preserved_value' (string)",
			se.RawFields["unknown_key"], se.RawFields["unknown_key"])
	}
	if v, ok := se.RawFields["another_key"].(uint32); !ok || v != 99 {
		t.Errorf("RawFields['another_key'] = %v (type %T), want 99 (uint32)",
			se.RawFields["another_key"], se.RawFields["another_key"])
	}

	// Known keys must NOT be in RawFields.
	knownKeys := []string{"appid", "AppName", "exe", "StartDir", "icon", "LaunchOptions",
		"IsHidden", "AllowDesktopConfig", "AllowOverlay", "OpenVR", "Devkit",
		"LastPlayTime", "FlatpakAppID", "sortas", "tags"}
	for _, k := range knownKeys {
		if _, exists := se.RawFields[k]; exists {
			t.Errorf("known key %q found in RawFields", k)
		}
	}
}

// ---------------------------------------------------------------------------
// 19. Pre-set AppID in AddGame is honoured
// ---------------------------------------------------------------------------

func TestAddGame_PresetAppID(t *testing.T) {
	var shortcuts []ShortcutEntry
	presetID := uint32(0xDEADBEEF)

	entry := &ShortcutEntry{
		AppID:   presetID,
		AppName: "Preset AppID Game",
		Exe:     "/preset",
		Tags:    []string{"test"},
	}
	if err := AddGame(&shortcuts, entry); err != nil {
		t.Fatalf("AddGame: %v", err)
	}
	if shortcuts[0].AppID != presetID {
		t.Errorf("AppID = %d, want preset %d", shortcuts[0].AppID, presetID)
	}
}

// ---------------------------------------------------------------------------
// 20. AddGame with existing F95Zone tag is not duplicated
// ---------------------------------------------------------------------------

func TestAddGame_F95ZoneTagNotDuplicated(t *testing.T) {
	var shortcuts []ShortcutEntry

	entry := &ShortcutEntry{
		AppName: "Game",
		Exe:     "/game",
		Tags:    []string{"F95Zone", "rpg"},
	}
	if err := AddGame(&shortcuts, entry); err != nil {
		t.Fatalf("AddGame: %v", err)
	}
	if len(shortcuts[0].Tags) != 2 {
		t.Errorf("expected 2 tags (F95Zone, rpg), got %d: %v",
			len(shortcuts[0].Tags), shortcuts[0].Tags)
	}
	if shortcuts[0].Tags[0] != "F95Zone" {
		t.Errorf("Tags[0] = %q, want 'F95Zone'", shortcuts[0].Tags[0])
	}
	if shortcuts[0].Tags[1] != "rpg" {
		t.Errorf("Tags[1] = %q, want 'rpg'", shortcuts[0].Tags[1])
	}
}

// ---------------------------------------------------------------------------
// proton.go: vdfEscape
// ---------------------------------------------------------------------------

func TestVDFEscape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{`back\slash`, `back\\slash`},
		{`quote"here`, `quote\"here`},
		{`both\and"`, `both\\and\"`},
		{"", ""},
		{"no special chars", "no special chars"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := vdfEscape(tt.input)
			if got != tt.want {
				t.Errorf("vdfEscape(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// proton.go: isValidProton
// ---------------------------------------------------------------------------

func TestIsValidProton(t *testing.T) {
	t.Parallel()
	tests := []struct {
		version string
		want    bool
	}{
		{"none", true},
		{"proton_experimental", true},
		{"proton_9.0", true},
		{"PROTON_8.0", true},
		{"GE-Proton9-10", true},
		{"ge-proton8-25", true},
		{"GE-Proton", true},
		{"invalid", false},
		{"", false},
		{"wine-9.0", false},
		{"Proton 9.0", false}, // space not handled
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			got := isValidProton(tt.version)
			if got != tt.want {
				t.Errorf("isValidProton(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// proton.go: getOrCreateMap
// ---------------------------------------------------------------------------

func TestGetOrCreateMap_Existing(t *testing.T) {
	t.Parallel()
	inner := map[string]interface{}{"x": 1}
	m := map[string]interface{}{"child": inner}
	got := getOrCreateMap(m, "child")
	if got["x"] != 1 {
		t.Errorf("expected inner map with x=1, got %v", got)
	}
}

func TestGetOrCreateMap_CreatesNew(t *testing.T) {
	t.Parallel()
	m := map[string]interface{}{}
	got := getOrCreateMap(m, "newkey")
	if got == nil {
		t.Fatal("expected non-nil map")
	}
	// Should have stored the new map back in parent.
	if _, ok := m["newkey"].(map[string]interface{}); !ok {
		t.Error("new map not stored in parent")
	}
}

func TestGetOrCreateMap_WrongType(t *testing.T) {
	t.Parallel()
	m := map[string]interface{}{"bad": "not a map"}
	got := getOrCreateMap(m, "bad")
	if got == nil {
		t.Fatal("expected new map when existing value has wrong type")
	}
	// Parent should now have the new map.
	if _, ok := m["bad"].(map[string]interface{}); !ok {
		t.Error("parent should contain map after getOrCreateMap with wrong type")
	}
}

// ---------------------------------------------------------------------------
// proton.go: getCompatToolMapping
// ---------------------------------------------------------------------------

func TestGetCompatToolMapping_FullPath(t *testing.T) {
	t.Parallel()
	ctm := map[string]interface{}{"12345": map[string]interface{}{"name": "proton_9.0"}}
	cfg := map[string]interface{}{
		"InstallConfigStore": map[string]interface{}{
			"Software": map[string]interface{}{
				"Valve": map[string]interface{}{
					"Steam": map[string]interface{}{
						"CompatToolMapping": ctm,
					},
				},
			},
		},
	}
	got := getCompatToolMapping(cfg)
	if got == nil {
		t.Fatal("expected non-nil CompatToolMapping")
	}
	if got["12345"].(map[string]interface{})["name"] != "proton_9.0" {
		t.Error("expected proton_9.0 mapping")
	}
}

func TestGetCompatToolMapping_EmptyConfig(t *testing.T) {
	t.Parallel()
	got := getCompatToolMapping(map[string]interface{}{})
	if got != nil {
		t.Error("expected nil for empty config")
	}
}

func TestGetCompatToolMapping_MissingSegment(t *testing.T) {
	t.Parallel()
	cfg := map[string]interface{}{
		"InstallConfigStore": map[string]interface{}{
			"Software": map[string]interface{}{}, // no Valve
		},
	}
	got := getCompatToolMapping(cfg)
	if got != nil {
		t.Error("expected nil when Valve segment is missing")
	}
}

func TestGetCompatToolMapping_MissingCompatToolMapping(t *testing.T) {
	t.Parallel()
	cfg := map[string]interface{}{
		"InstallConfigStore": map[string]interface{}{
			"Software": map[string]interface{}{
				"Valve": map[string]interface{}{
					"Steam": map[string]interface{}{}, // no CompatToolMapping
				},
			},
		},
	}
	got := getCompatToolMapping(cfg)
	if got != nil {
		t.Error("expected nil when CompatToolMapping is missing")
	}
}

// ---------------------------------------------------------------------------
// proton.go: encodeVDF + writeVDFMap round-trip
// ---------------------------------------------------------------------------

func TestEncodeVDF_RoundTrip(t *testing.T) {
	t.Parallel()
	input := map[string]interface{}{
		"CompatToolMapping": map[string]interface{}{
			"12345": map[string]interface{}{
				"name":     "proton_9.0",
				"config":   "",
				"Priority": "250",
			},
		},
	}
	var buf strings.Builder
	if err := encodeVDF(&buf, input); err != nil {
		t.Fatalf("encodeVDF: %v", err)
	}
	output := buf.String()
	if output == "" {
		t.Fatal("expected non-empty VDF output")
	}
	// Verify key structure is present.
	if !strings.Contains(output, `"CompatToolMapping"`) {
		t.Error("output missing CompatToolMapping key")
	}
	if !strings.Contains(output, `"12345"`) {
		t.Error("output missing 12345 app key")
	}
	if !strings.Contains(output, `"proton_9.0"`) {
		t.Error("output missing proton version value")
	}
}

func TestEncodeVDF_Empty(t *testing.T) {
	t.Parallel()
	var buf strings.Builder
	if err := encodeVDF(&buf, map[string]interface{}{}); err != nil {
		t.Fatalf("encodeVDF: %v", err)
	}
	if buf.String() != "" {
		t.Errorf("expected empty output for empty map, got %q", buf.String())
	}
}

func TestEncodeVDF_NestedEscaping(t *testing.T) {
	t.Parallel()
	input := map[string]interface{}{
		"key\\with\"quotes": "value\\with\"quotes",
	}
	var buf strings.Builder
	if err := encodeVDF(&buf, input); err != nil {
		t.Fatalf("encodeVDF: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, `key\\with\"quotes`) {
		t.Errorf("expected escaped key, got: %s", output)
	}
	if !strings.Contains(output, `value\\with\"quotes`) {
		t.Errorf("expected escaped value, got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// 21. BestGridImage
// ---------------------------------------------------------------------------

func TestBestGridImage_Empty(t *testing.T) {
	t.Parallel()

	// nil slice
	url, ok := BestGridImage(nil)
	if ok {
		t.Error("expected false for nil results, got true")
	}
	if url != "" {
		t.Errorf("expected empty URL, got %q", url)
	}

	// empty slice
	url, ok = BestGridImage([]SGDBImageResult{})
	if ok {
		t.Error("expected false for empty results, got true")
	}
	if url != "" {
		t.Errorf("expected empty URL, got %q", url)
	}
}

func TestBestGridImage_Single(t *testing.T) {
	t.Parallel()
	results := []SGDBImageResult{
		{URL: "https://example.com/image.png", Score: 10},
	}
	url, ok := BestGridImage(results)
	if !ok {
		t.Error("expected true for single image")
	}
	if url != "https://example.com/image.png" {
		t.Errorf("got URL %q, want %q", url, "https://example.com/image.png")
	}
}

func TestBestGridImage_HighestScore(t *testing.T) {
	t.Parallel()
	results := []SGDBImageResult{
		{URL: "https://example.com/low.png", Score: 1},
		{URL: "https://example.com/high.png", Score: 100},
		{URL: "https://example.com/medium.png", Score: 50},
	}
	url, ok := BestGridImage(results)
	if !ok {
		t.Error("expected true for multiple images")
	}
	if url != "https://example.com/high.png" {
		t.Errorf("got URL %q, want %q", url, "https://example.com/high.png")
	}
}

func TestBestGridImage_SkipsDataURI(t *testing.T) {
	t.Parallel()
	results := []SGDBImageResult{
		{URL: "data:image/png;base64,abc123", Score: 100},
		{URL: "https://example.com/good.png", Score: 50},
	}
	url, ok := BestGridImage(results)
	if !ok {
		t.Error("expected true when a valid URL exists alongside a data: URI")
	}
	if url != "https://example.com/good.png" {
		t.Errorf("got URL %q, want %q", url, "https://example.com/good.png")
	}
}

func TestBestGridImage_SkipsSVG(t *testing.T) {
	t.Parallel()
	results := []SGDBImageResult{
		{URL: "https://example.com/image.svg", Score: 200},
		{URL: "https://example.com/valid.png", Score: 10},
	}
	url, ok := BestGridImage(results)
	if !ok {
		t.Error("expected true when a valid PNG exists alongside an SVG")
	}
	if url != "https://example.com/valid.png" {
		t.Errorf("got URL %q, want %q", url, "https://example.com/valid.png")
	}
}

func TestBestGridImage_UsesThumbForICO(t *testing.T) {
	t.Parallel()
	results := []SGDBImageResult{
		{URL: "https://cdn.example.com/icon.ico", Thumb: "https://cdn.example.com/icon.png", Score: 100},
	}
	url, ok := BestGridImage(results)
	if !ok {
		t.Error("expected true when an ICO has a thumb available")
	}
	if url != "https://cdn.example.com/icon.png" {
		t.Errorf("got URL %q, want %q", url, "https://cdn.example.com/icon.png")
	}
}

func TestBestGridImage_ThumbReturnedForICO(t *testing.T) {
	t.Parallel()
	results := []SGDBImageResult{
		{URL: "https://cdn.example.com/icon.ico", Thumb: "https://cdn.example.com/thumb.png", Score: 100},
		{URL: "https://cdn.example.com/real.png", Score: 200},
	}
	url, ok := BestGridImage(results)
	if !ok {
		t.Error("expected true when a valid PNG exists alongside an ICO")
	}
	// PNG should be selected over ICO when it has a higher score.
	if url != "https://cdn.example.com/real.png" {
		t.Errorf("got URL %q, want %q", url, "https://cdn.example.com/real.png")
	}
}

func TestBestGridImage_AllSkipped(t *testing.T) {
	t.Parallel()
	results := []SGDBImageResult{
		{URL: "data:image/png;base64,abc", Score: 100},
		{URL: "https://example.com/bad.svg", Score: 50},
	}
	url, ok := BestGridImage(results)
	if ok {
		t.Error("expected false when all results are skipped")
	}
	if url != "" {
		t.Errorf("expected empty URL, got %q", url)
	}
}


