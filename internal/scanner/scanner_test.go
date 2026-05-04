package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mili/moxie/internal/engine"
)

func TestScanUnityGame(t *testing.T) {
	root := t.TempDir()
	gameDir := filepath.Join(root, "TestGame")
	os.MkdirAll(gameDir, 0755)
	os.MkdirAll(filepath.Join(gameDir, "TestGame_Data"), 0755)
	os.WriteFile(filepath.Join(gameDir, "TestGame.exe"), []byte("fake exe"), 0644)
	os.WriteFile(filepath.Join(gameDir, "UnityPlayer.dll"), []byte("fake dll"), 0644)

	games, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 {
		t.Fatalf("expected 1 game, got %d", len(games))
	}
	if games[0].Engine != engine.Unity {
		t.Errorf("expected Unity, got %s", games[0].Engine)
	}
	if games[0].Title != "TestGame" {
		t.Errorf("expected title 'TestGame', got %q", games[0].Title)
	}
}

func TestScanRenPyGame(t *testing.T) {
	root := t.TempDir()
	gameDir := filepath.Join(root, "RenPyGame")
	os.MkdirAll(filepath.Join(gameDir, "renpy"), 0755)
	os.MkdirAll(filepath.Join(gameDir, "game"), 0755)

	games, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 {
		t.Fatalf("expected 1 game, got %d", len(games))
	}
	if games[0].Engine != engine.RenPy {
		t.Errorf("expected RenPy, got %s", games[0].Engine)
	}
}

func TestScanMultipleGames(t *testing.T) {
	root := t.TempDir()

	// Ren'Py game
	os.MkdirAll(filepath.Join(root, "RenPyGame", "renpy"), 0755)
	// Unity game
	os.MkdirAll(filepath.Join(root, "UnityGame", "UnityGame_Data"), 0755)
	os.WriteFile(filepath.Join(root, "UnityGame", "UnityGame.exe"), []byte("exe"), 0644)
	os.WriteFile(filepath.Join(root, "UnityGame", "UnityPlayer.dll"), []byte("dll"), 0644)
	// Non-game directory
	os.MkdirAll(filepath.Join(root, "Notes"), 0755)
	os.WriteFile(filepath.Join(root, "Notes", "readme.txt"), []byte("hello"), 0644)

	games, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 2 {
		t.Fatalf("expected 2 games, got %d", len(games))
	}
	engines := make(map[engine.Engine]bool)
	for _, g := range games {
		engines[g.Engine] = true
	}
	if !engines[engine.RenPy] || !engines[engine.Unity] {
		t.Errorf("expected RenPy and Unity, got %v", engines)
	}
}

func TestScanNonGameDir(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "Notes"), 0755)
	os.WriteFile(filepath.Join(root, "Notes", "readme.txt"), []byte("hello"), 0644)

	games, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 0 {
		t.Errorf("expected 0 games, got %d", len(games))
	}
}

func TestScanEmptyDir(t *testing.T) {
	root := t.TempDir()
	games, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 0 {
		t.Errorf("expected 0 games, got %d", len(games))
	}
}

func TestExtractVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want string
	}{
		// Date versions (highest priority)
		{"Game-2025-11-14", "2025-11-14"},
		{"2025-01-01-Release", "2025-01-01"},

		// Dot-separated versions
		{"Game-v1.0.3", "1.0.3"},
		{"Game-1.0", "1.0"},
		{"V2.0.0-Final", "2.0.0"},
		{"v0.5.2", "0.5.2"},
		{"B.0.10.7.5.2", "0.10.7.5.2"},

		// Dash-separated versions (converted to dots)
		{"SummertimeSaga-0-20-16-pc", "0.20.16"},
		{"Game-v1-0-3", "1.0.3"},
		{"succumama-1.1.6-pc", "1.1.6"},

		// Underscore-separated versions (converted to dots)
		{"My_Hentai_Fantasy-0.11-pc", "0.11"},

		// Edge cases
		{"", ""},
		{"NoVersionHere", ""},
		{"Game", ""},
		{"v", ""},
		{"just-a-name", ""},

		// Mixed formats - date takes priority
		{"Game-v1.0-2025-11-14", "2025-11-14"},

		// Version with letter prefix
		{"Game-0.11-pc", "0.11"},
		{"Training_The_Demon-0.1.2-pc", "0.1.2"},

		// Versions delimited by underscores (fix: \b doesn't work with _)
		{"FullEmberDoors_v0.1.7_Linux", "0.1.7"},
		{"Vice_Empire_Tycoon_V1.6.1_Trial_Build", "1.6.1"},
		{"Society_v1.28", "1.28"},
		{"Game_V1.0.0_HotFix", "1.0.0"},
		{"Zaras_School_Life_v0.6.6_Free", "0.6.6"},

		// Versions with trailing build letter (fix: [a-zA-Z]? at end)
		{"Course_of_Temptation_v0.7.7i", "0.7.7i"},

		// Underscore before version + extension after (fix: old regex matched 0.2 not 1.0.2)
		{"CoC_1.0.2.swf", "1.0.2"},

		// Single/double-digit versions with v prefix (fix: new singleVerRE)
		{"Island SAGA v5", "5"},
		{"ToBeSIgma_v0", "0"},

		// Version inside category dir prefix with underscores
		{"The Fapocalypse v0.5.13", "0.5.13"},
		{"The SUP v0.9.75", "0.9.75"},
		{"Sultry Secrets 2.1", "2.1"},

		// False-positive guard: no v prefix, just trailing digits
		{"Boneka_Ascension_WINv01", ""}, // N+v both letters, no delimiter
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractVersion(tt.name)
			if got != tt.want {
				t.Errorf("ExtractVersion(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestIsEngineName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want bool
	}{
		// Canonical engine names
		{"unity", true},
		{"renpy", true},
		{"ren'py", true},
		{"rpgm", true},
		{"rpgmaker", true},
		{"godot", true},
		{"unreal", true},
		{"electron", true},
		{"html", true},
		{"java", true},
		{"flash", true},
		{"mugen", true},

		// Category names
		{"other", true},
		{"others", true},
		{"tools", true},
		{"jre", true},

		// Non-matches
		{"game", false},
		{"completed", false},
		{"abandoned", false},
		{"Unity", false}, // case-sensitive (lowercase required)
		{"RENPY", false},
		{"", false},
		{"download", false},
		{"mods", false},
		{"saves", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isEngineName(tt.name)
			if got != tt.want {
				t.Errorf("isEngineName(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestIsCategoryDir(t *testing.T) {
	t.Run("unity dir with game subdirectories", func(t *testing.T) {
		dir := t.TempDir()
		categoryDir := filepath.Join(dir, "Unity")
		os.MkdirAll(categoryDir, 0755)
		gameSub := filepath.Join(categoryDir, "MyGame")
		os.MkdirAll(gameSub, 0755)
		os.WriteFile(filepath.Join(gameSub, "Game.exe"), []byte("fake"), 0644)

		if !isCategoryDir(categoryDir) {
			t.Error("Unity dir with game subdirs should be detected as category dir")
		}
	})

	t.Run("unity dir without game subdirectories", func(t *testing.T) {
		dir := t.TempDir()
		categoryDir := filepath.Join(dir, "Unity")
		os.MkdirAll(categoryDir, 0755)
		os.MkdirAll(filepath.Join(categoryDir, "readme"), 0755)
		os.WriteFile(filepath.Join(categoryDir, "readme", "info.txt"), []byte("hello"), 0644)

		if isCategoryDir(categoryDir) {
			t.Error("Unity dir without game subdirs should NOT be a category dir")
		}
	})

	t.Run("non-engine dir with game subdirectories", func(t *testing.T) {
		dir := t.TempDir()
		nonEngineDir := filepath.Join(dir, "MyGames")
		os.MkdirAll(nonEngineDir, 0755)
		gameSub := filepath.Join(nonEngineDir, "RenPyGame")
		os.MkdirAll(filepath.Join(gameSub, "renpy"), 0755)

		if isCategoryDir(nonEngineDir) {
			t.Error("Non-engine-name dir should NOT be a category dir")
		}
	})

	t.Run("rpgm dir with renpy game subdirectory", func(t *testing.T) {
		dir := t.TempDir()
		categoryDir := filepath.Join(dir, "RPGM")
		os.MkdirAll(categoryDir, 0755)
		gameSub := filepath.Join(categoryDir, "SomeGame")
		os.MkdirAll(filepath.Join(gameSub, "renpy"), 0755)
		os.MkdirAll(filepath.Join(gameSub, "game"), 0755)

		if !isCategoryDir(categoryDir) {
			t.Error("RPGM dir with renpy game subdirs should be a category dir")
		}
	})

	t.Run("empty engine-named dir", func(t *testing.T) {
		dir := t.TempDir()
		emptyDir := filepath.Join(dir, "Godot")
		os.MkdirAll(emptyDir, 0755)

		if isCategoryDir(emptyDir) {
			t.Error("Empty engine-named dir should NOT be a category dir")
		}
	})
}

func TestShouldSkip(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"UnityCrashHandler64.exe", true},
		{"uninstall.exe", true},
		{"unins000.exe", true},
		{"python.exe", true},
		{"pythonw.exe", true},
		{"vc_redist.x64.exe", true},
		{"DXSETUP.exe", true},
		{"zsync.exe", true},
		{"notification_helper.exe", true},
		{"Game.exe", false},
		{"mygame.exe", false},
		{"renpy.exe", false},
	}
	for _, tt := range tests {
		if got := shouldSkip(tt.name); got != tt.expected {
			t.Errorf("shouldSkip(%q) = %v, want %v", tt.name, got, tt.expected)
		}
	}
}

func TestScanSingle(t *testing.T) {
	gameDir := t.TempDir()
	os.MkdirAll(filepath.Join(gameDir, "Game_Data"), 0755)
	os.WriteFile(filepath.Join(gameDir, "Game.exe"), []byte("exe"), 0644)

	g, err := ScanSingle(gameDir)
	if err != nil {
		t.Fatal(err)
	}
	if g.Engine != engine.Unity {
		t.Errorf("expected Unity, got %s", g.Engine)
	}
	if g.Title != filepath.Base(gameDir) {
		t.Errorf("expected title to be dir name")
	}
}

func TestFindGameExe(t *testing.T) {
	dir := t.TempDir()
	// Write two exes, one large (the game) and one small (a launcher).
	os.WriteFile(filepath.Join(dir, "small.exe"), make([]byte, 100), 0644)
	os.WriteFile(filepath.Join(dir, "Game.exe"), make([]byte, 10000), 0644)

	exe := findGameExe(dir)
	if filepath.Base(exe) != "Game.exe" {
		t.Errorf("expected Game.exe (largest), got %s", filepath.Base(exe))
	}
	// Small exe shouldn't be found since Game.exe is larger.
}

func TestScanNonexistentDir(t *testing.T) {
	_, err := Scan("/nonexistent/path/that/does/not/exist/12345")
	if err == nil {
		t.Fatal("expected error for non-existent directory")
	}
}

func TestScanSingleNonexistentDir(t *testing.T) {
	_, err := ScanSingle("/nonexistent/path/12345")
	if err != nil {
		t.Fatal("ScanSingle should not error on nonexistent dir — it uses engine.Detect which handles it")
	}
}

func TestLooksLikeGameRoot(t *testing.T) {
	t.Run("with exe", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "game.exe"), []byte("x"), 0644)
		if !looksLikeGameRoot(dir) {
			t.Error("should detect as game root")
		}
	})
	t.Run("with renpy", func(t *testing.T) {
		dir := t.TempDir()
		os.MkdirAll(filepath.Join(dir, "renpy"), 0755)
		if !looksLikeGameRoot(dir) {
			t.Error("should detect as game root")
		}
	})
	t.Run("with rpyc", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "script.rpyc"), []byte("x"), 0644)
		if !looksLikeGameRoot(dir) {
			t.Error("should detect as game root")
		}
	})
	t.Run("plain dir", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("x"), 0644)
		if looksLikeGameRoot(dir) {
			t.Error("should NOT detect as game root")
		}
	})
}

// TestScanCategoryDirectory verifies that Scan skips category folders
// (named after engines) and correctly identifies game subdirectories within them.
func TestScanCategoryDirectory(t *testing.T) {
	root := t.TempDir()

	// Create a "Unity" category folder containing actual game subdirectories.
	unityCategory := filepath.Join(root, "Unity")
	os.MkdirAll(unityCategory, 0755)

	// Game 1: A Unity game inside the Unity/ category folder.
	game1 := filepath.Join(unityCategory, "TestUnityGame")
	os.MkdirAll(filepath.Join(game1, "TestUnityGame_Data"), 0755)
	os.WriteFile(filepath.Join(game1, "TestUnityGame.exe"), []byte("fake exe"), 0644)
	os.WriteFile(filepath.Join(game1, "UnityPlayer.dll"), []byte("fake dll"), 0644)

	// Game 2: A Ren'Py game also inside the Unity/ category folder.
	game2 := filepath.Join(unityCategory, "RenPyVN")
	os.MkdirAll(filepath.Join(game2, "renpy"), 0755)
	os.MkdirAll(filepath.Join(game2, "game"), 0755)

	// Non-game folder inside category (should be ignored by looksLikeGameRoot).
	nonGame := filepath.Join(unityCategory, "Notes")
	os.MkdirAll(nonGame, 0755)
	os.WriteFile(filepath.Join(nonGame, "readme.txt"), []byte("notes"), 0644)

	games, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}

	// The Unity/ directory itself should NOT be detected as a game.
	for _, g := range games {
		if g.Path == unityCategory {
			t.Errorf("Unity category folder should NOT be detected as a game, but was: %+v", g)
		}
	}

	// Both subdirectories should be detected as games.
	if len(games) != 2 {
		t.Fatalf("expected 2 games in category directory, got %d: %v", len(games), games)
	}

	// Verify engines.
	foundUnity := false
	foundRenPy := false
	for _, g := range games {
		if g.Title == "TestUnityGame" && g.Engine == engine.Unity {
			foundUnity = true
		}
		if g.Title == "RenPyVN" && g.Engine == engine.RenPy {
			foundRenPy = true
		}
	}
	if !foundUnity {
		t.Error("Unity game inside category folder not detected")
	}
	if !foundRenPy {
		t.Error("RenPy game inside category folder not detected")
	}
}

// TestScanCategoryDirNested verifies deeply nested category structures.
func TestScanCategoryDirNested(t *testing.T) {
	root := t.TempDir()

	// Create top-level game (not in a category).
	topGame := filepath.Join(root, "StandaloneGame")
	os.MkdirAll(filepath.Join(topGame, "game"), 0755)
	os.MkdirAll(filepath.Join(topGame, "renpy"), 0755)

	// Create RPGM/ category with a game inside.
	rpgmCategory := filepath.Join(root, "RPGM")
	os.MkdirAll(rpgmCategory, 0755)
	rpgmGame := filepath.Join(rpgmCategory, "DragonQuest")
	os.MkdirAll(filepath.Join(rpgmGame, "www"), 0755)
	os.WriteFile(filepath.Join(rpgmGame, "Game.exe"), []byte("exe"), 0644)
	os.WriteFile(filepath.Join(rpgmGame, "www", "package.json"), []byte(`{"name":"KADOKAWA/RPGMV"}`), 0644)

	games, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}

	if len(games) != 2 {
		t.Fatalf("expected 2 games (1 standalone + 1 in category), got %d", len(games))
	}

	rpgmCategoryDetected := false
	for _, g := range games {
		if g.Path == rpgmCategory {
			rpgmCategoryDetected = true
		}
	}
	if rpgmCategoryDetected {
		t.Error("RPGM category folder itself should NOT be detected as a game")
	}
}

// TestScanPathPrefixCollision verifies that a directory whose name is a prefix
// of another directory does not cause false positives in the subdirectory-skip
// guard. E.g., /root/foo (a game) must not cause /root/foobar/SomeGame to be
// incorrectly skipped because strings.HasPrefix("/root/foobar", "/root/foo")
// was true without a path-separator check.
func TestScanPathPrefixCollision(t *testing.T) {
	root := t.TempDir()

	// "foo" is a detected game directory (has an exe).
	foo := filepath.Join(root, "foo")
	os.MkdirAll(foo, 0755)
	os.WriteFile(filepath.Join(foo, "foo.exe"), []byte("exe"), 0644)

	// "foobar" is a non-game parent directory containing a real game.
	foobar := filepath.Join(root, "foobar")
	os.MkdirAll(foobar, 0755)
	someGame := filepath.Join(foobar, "SomeGame")
	os.MkdirAll(filepath.Join(someGame, "SomeGame_Data"), 0755)
	os.WriteFile(filepath.Join(someGame, "SomeGame.exe"), []byte("exe"), 0644)
	os.WriteFile(filepath.Join(someGame, "UnityPlayer.dll"), []byte("dll"), 0644)

	games, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}

	// Both games should be detected. If the prefix-collision bug is present,
	// "SomeGame" inside "foobar" is falsely skipped and we only get 1 game.
	if len(games) != 2 {
		t.Fatalf("expected 2 games (foo + foobar/SomeGame), got %d: %v", len(games), games)
	}

	foundFoo := false
	foundSomeGame := false
	for _, g := range games {
		switch g.Path {
		case foo:
			foundFoo = true
		case someGame:
			foundSomeGame = true
		}
	}
	if !foundFoo {
		t.Error("game 'foo' not found")
	}
	if !foundSomeGame {
		t.Error("game 'foobar/SomeGame' not found — path-prefix collision bug may still be present")
	}
}
