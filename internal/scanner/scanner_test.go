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
