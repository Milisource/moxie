package updater

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

// A Ren'Py "Update only" archive holding just game/ must land in game/ and
// leave every other file (launcher, other archives, saves) intact.
func TestMergeOverlay_RenPyPatchKeepsOtherFiles(t *testing.T) {
	root := t.TempDir()
	game := filepath.Join(root, "Bunker")
	write(t, filepath.Join(game, "Bunker.exe"), "exe")
	write(t, filepath.Join(game, "game", "images.rpa"), "old-images")
	write(t, filepath.Join(game, "game", "scripts.rpa"), "old-scripts")
	write(t, filepath.Join(game, "game", "saves", "1-1-LT1.save"), "my-save")

	extract := filepath.Join(root, "extract")
	// Extractor unwrapped the single "game" folder, so contents arrive here.
	patch := filepath.Join(extract, "game")
	write(t, filepath.Join(patch, "scripts.rpa"), "new-scripts")
	write(t, filepath.Join(patch, "ch18.rpa"), "new-chapter")

	res, err := MergeOverlay(context.Background(), game, "renpy", patch)
	if err != nil {
		t.Fatal(err)
	}
	if res.FilesCopied != 2 {
		t.Errorf("copied = %d, want 2", res.FilesCopied)
	}
	checks := map[string]string{
		"Bunker.exe":              "exe",
		"game/images.rpa":         "old-images",
		"game/scripts.rpa":        "new-scripts",
		"game/ch18.rpa":           "new-chapter",
		"game/saves/1-1-LT1.save": "my-save",
	}
	for rel, want := range checks {
		if got := read(t, filepath.Join(game, rel)); got != want {
			t.Errorf("%s = %q, want %q", rel, got, want)
		}
	}
	if _, err := os.Stat(game + ".patch-old"); !os.IsNotExist(err) {
		t.Error("patch backup dir should be removed on success")
	}
}

func TestMergeOverlay_CancelRestoresOriginals(t *testing.T) {
	root := t.TempDir()
	game := filepath.Join(root, "G")
	write(t, filepath.Join(game, "a.txt"), "orig")
	src := filepath.Join(root, "x", "G-patch")
	write(t, filepath.Join(src, "a.txt"), "new")
	write(t, filepath.Join(src, "b.txt"), "new")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := MergeOverlay(ctx, game, "", filepath.Dir(src)); err == nil {
		t.Fatal("want error on cancelled ctx")
	}
	if got := read(t, filepath.Join(game, "a.txt")); got != "orig" {
		t.Errorf("a.txt = %q, want orig", got)
	}
	if got := read(t, filepath.Join(game, "b.txt")); got != "<missing>" {
		t.Errorf("b.txt should not exist, got %q", got)
	}
}

func TestContainsLauncher(t *testing.T) {
	root := t.TempDir()
	full := filepath.Join(root, "full", "Game-1.0-pc")
	write(t, filepath.Join(full, "Game.exe"), "x")
	write(t, filepath.Join(full, "game", "a.rpa"), "x")
	if !ContainsLauncher(filepath.Join(root, "full"), "Game.exe") {
		t.Error("full build should contain launcher")
	}
	patch := filepath.Join(root, "patch", "game")
	write(t, filepath.Join(patch, "a.rpa"), "x")
	if ContainsLauncher(filepath.Join(root, "patch"), "Game.exe") {
		t.Error("patch should not contain launcher")
	}
	if !ContainsLauncher(filepath.Join(root, "patch"), "") {
		t.Error("unknown launcher keeps full-merge behaviour")
	}
}

func TestRemoveBackup(t *testing.T) {
	root := t.TempDir()
	game := filepath.Join(root, "G")
	write(t, filepath.Join(game+".old", "x"), "x")
	write(t, filepath.Join(game+".old.old", "x"), "x")
	if err := RemoveBackup(game); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{game + ".old", game + ".old.old"} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s still exists", p)
		}
	}
}
