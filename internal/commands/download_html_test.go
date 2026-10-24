package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mili/moxie/internal/db"
)

// TestInstallHTMLGameFile covers the single-file HTML update flow: the new
// page is copied in, the previous entry is retired rather than deleted, and
// the caller learns both paths.
func TestInstallHTMLGameFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	oldEntry := filepath.Join(dir, "Ashfall_RPG_v0.3.html")
	if err := os.WriteFile(oldEntry, []byte("<html>old</html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(t.TempDir(), "Ashfall_RPG_v0.5.0.html")
	if err := os.WriteFile(src, []byte("<html>new</html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	installed, retired, err := installHTMLGameFile(dir, oldEntry, src)
	if err != nil {
		t.Fatalf("installHTMLGameFile: %v", err)
	}
	want := filepath.Join(dir, "Ashfall_RPG_v0.5.0.html")
	if installed != want {
		t.Errorf("installed = %q, want %q", installed, want)
	}
	got, err := os.ReadFile(want)
	if err != nil || string(got) != "<html>new</html>" {
		t.Errorf("installed content = %q (err %v), want the new page", got, err)
	}
	if _, err := os.Stat(oldEntry); !os.IsNotExist(err) {
		t.Errorf("old entry still at its original path")
	}
	if retired != oldEntry+".pre-update" {
		t.Errorf("retired = %q, want %q", retired, oldEntry+".pre-update")
	}
	if _, err := os.Stat(retired); err != nil {
		t.Errorf("retired copy missing: %v", err)
	}
}

// TestInstallHTMLGameFile_PrefersExistingExePath: when the record already
// names its entry page, that page is the one retired — not whatever the scan
// happens to pick.
func TestInstallHTMLGameFile_PrefersExistingExePath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	entry := filepath.Join(dir, "entry.html")
	other := filepath.Join(dir, "index.html")
	for _, p := range []string{entry, other} {
		if err := os.WriteFile(p, []byte("<html>x</html>"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	src := filepath.Join(t.TempDir(), "new.html")
	if err := os.WriteFile(src, []byte("<html>new</html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, retired, err := installHTMLGameFile(dir, entry, src)
	if err != nil {
		t.Fatalf("installHTMLGameFile: %v", err)
	}
	if retired != entry+".pre-update" {
		t.Errorf("retired = %q, want the recorded ExePath", retired)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("unrelated index.html must be left alone: %v", err)
	}
}

func TestApplyHTMLGameInstall_NonHTMLIgnored(t *testing.T) {
	t.Parallel()
	if applyHTMLGameInstall(nil, &db.Game{}, filepath.Join(t.TempDir(), "game.zip")) {
		t.Error("applyHTMLGameInstall claimed a non-HTML file")
	}
}

// TestApplyHTMLGameInstall_RecordsVersion verifies the record is updated
// (entry page + installed version) and the downloaded copy is consumed.
func TestApplyHTMLGameInstall_RecordsVersion(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	database, err := db.Open(filepath.Join(base, "moxie.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	gameDir := filepath.Join(base, "Ashfall RPG v0.3")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	game := &db.Game{
		Title:         "Ashfall RPG",
		Engine:        "HTML",
		Path:          gameDir,
		EngineSource:  "scan",
		Version:       "0.3",
		LatestVersion: "0.5",
		Status:        "active",
	}
	id, err := database.InsertGame(game)
	if err != nil {
		t.Fatalf("InsertGame: %v", err)
	}
	game.ID = id

	src := filepath.Join(base, "Ashfall_RPG_v0.5.0.html")
	if err := os.WriteFile(src, []byte("<html>new</html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !applyHTMLGameInstall(database, game, src) {
		t.Fatal("applyHTMLGameInstall returned false for an HTML file")
	}
	if game.Version != "0.5" || game.VersionSource != "f95" {
		t.Errorf("version = %q (%s), want 0.5 (f95)", game.Version, game.VersionSource)
	}
	if filepath.Base(game.ExePath) != "Ashfall_RPG_v0.5.0.html" {
		t.Errorf("ExePath = %q", game.ExePath)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("downloaded copy should be consumed")
	}
	stored, err := database.GetGame(id)
	if err != nil || stored == nil {
		t.Fatalf("GetGame: %v", err)
	}
	if stored.Version != "0.5" || filepath.Base(stored.ExePath) != "Ashfall_RPG_v0.5.0.html" {
		t.Errorf("stored game = version %q exe %q", stored.Version, stored.ExePath)
	}
}
