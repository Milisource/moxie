package saves

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mili/moxie/internal/db"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// saveStoreEngine returns an Engine rooted at <base>/saves backed by a fresh
// temp database holding one game. It delegates to saveStoreEngineAt with a
// fresh temp directory.
func saveStoreEngine(t *testing.T, title string) (*Engine, *db.Game) {
	t.Helper()
	return saveStoreEngineAt(t, t.TempDir(), title)
}

// saveStoreEngineAt is saveStoreEngine with an explicit base directory. The
// database and the storage root both live under base, so tests can place
// sentinel files relative to the engine root (e.g. to prove a hostile set name
// cannot escape it).
func saveStoreEngineAt(t *testing.T, base, title string) (*Engine, *db.Game) {
	t.Helper()
	database, err := db.Open(filepath.Join(base, "moxie.sqlite"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	game := &db.Game{Title: title, Engine: "RenPy", Path: filepath.Join(base, "game")}
	if _, err := database.InsertGame(game); err != nil {
		t.Fatalf("insert game: %v", err)
	}

	engine, err := NewEngine(database, filepath.Join(base, "saves"))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	return engine, game
}

// saveStoreZip writes a zip archive at path containing the given files
// (entry name -> content).
func saveStoreZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("zip create %q: %v", name, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %q: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write zip: %v", err)
	}
}

// saveStoreAssertFile fails unless path exists with exactly wantContent.
func saveStoreAssertFile(t *testing.T, path, wantContent string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(data) != wantContent {
		t.Errorf("%s = %q, want %q", path, data, wantContent)
	}
}

// saveStoreAssertAbsent fails unless path does not exist.
func saveStoreAssertAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("%s should not exist (stat err = %v)", path, err)
	}
}

// ---------------------------------------------------------------------------
// Import from a plain directory
// ---------------------------------------------------------------------------

func TestImportDirectoryPlain(t *testing.T) {
	t.Parallel()
	engine, game := saveStoreEngine(t, "My Game")

	src := t.TempDir()
	const persistent = "persistent-data"
	const slot = "slot-one"
	saveTestWrite(t, filepath.Join(src, "persistent"), persistent)
	saveTestWrite(t, filepath.Join(src, "slot1.save"), slot)

	set, err := engine.Import(game, src, ImportOptions{Name: "My Saves"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if set.Name != "my-saves" {
		t.Errorf("Name = %q, want %q", set.Name, "my-saves")
	}
	if set.Source != "import" {
		t.Errorf("Source = %q, want %q", set.Source, "import")
	}
	if set.ApplyRoot != "" {
		t.Errorf("ApplyRoot = %q, want empty", set.ApplyRoot)
	}
	if set.FileCount != 2 {
		t.Errorf("FileCount = %d, want 2", set.FileCount)
	}
	if want := int64(len(persistent) + len(slot)); set.SizeBytes != want {
		t.Errorf("SizeBytes = %d, want %d", set.SizeBytes, want)
	}

	setDir := engine.SetPath(game, set.Name)
	saveStoreAssertFile(t, filepath.Join(setDir, "persistent"), persistent)
	saveStoreAssertFile(t, filepath.Join(setDir, "slot1.save"), slot)
}

// ---------------------------------------------------------------------------
// apply_root derivation from the staged tree
// ---------------------------------------------------------------------------

func TestImportWrappedSavesDir(t *testing.T) {
	t.Parallel()
	engine, game := saveStoreEngine(t, "My Game")

	src := t.TempDir()
	inner := filepath.Join(src, "CoolGame", "game", "saves")
	saveTestWrite(t, filepath.Join(inner, "persistent"), "p")
	saveTestWrite(t, filepath.Join(inner, "slot1.save"), "s")

	set, err := engine.Import(game, src, ImportOptions{Name: "wrapped"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if set.ApplyRoot != "CoolGame/game/saves" {
		t.Errorf("ApplyRoot = %q, want %q", set.ApplyRoot, "CoolGame/game/saves")
	}

	// The staged tree is copied verbatim: the wrapper chain survives on disk.
	setDir := engine.SetPath(game, set.Name)
	saveStoreAssertFile(t, filepath.Join(setDir, "CoolGame", "game", "saves", "persistent"), "p")
}

func TestImportWrapperChainOnly(t *testing.T) {
	t.Parallel()
	engine, game := saveStoreEngine(t, "My Game")

	src := t.TempDir()
	saveTestWrite(t, filepath.Join(src, "MyGame", "version-1.0", "a.save"), "a")

	set, err := engine.Import(game, src, ImportOptions{Name: "chain"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if set.ApplyRoot != "MyGame/version-1.0" {
		t.Errorf("ApplyRoot = %q, want %q", set.ApplyRoot, "MyGame/version-1.0")
	}
}

// ---------------------------------------------------------------------------
// Import from archives and single files
// ---------------------------------------------------------------------------

func TestImportZipArchive(t *testing.T) {
	t.Parallel()
	engine, game := saveStoreEngine(t, "My Game")

	dir := t.TempDir()
	zipPath := filepath.Join(dir, "saves.zip")
	saveStoreZip(t, zipPath, map[string]string{
		"persistent": "persist-zip",
		"slot1.save": "slot-zip",
	})

	set, err := engine.Import(game, zipPath, ImportOptions{Name: "from zip"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if set.Name != "from-zip" {
		t.Errorf("Name = %q, want %q", set.Name, "from-zip")
	}
	if set.ApplyRoot != "" {
		t.Errorf("ApplyRoot = %q, want empty", set.ApplyRoot)
	}
	if set.FileCount != 2 {
		t.Errorf("FileCount = %d, want 2", set.FileCount)
	}

	// Flat payload: files land directly in the set directory regardless of
	// whether extraction nested them under an archive-named subdirectory.
	setDir := engine.SetPath(game, set.Name)
	saveStoreAssertFile(t, filepath.Join(setDir, "persistent"), "persist-zip")
	saveStoreAssertFile(t, filepath.Join(setDir, "slot1.save"), "slot-zip")
}

// TestImportAcceptsPasswordOption checks the Password field is accepted and
// plumbed into the extractor without breaking a plain (unencrypted) archive
// import. A true encrypted-archive round-trip is impractical here: the stdlib
// zip reader can neither produce nor read encrypted entries, and 7z/rar
// backends are external tools that may be absent.
func TestImportAcceptsPasswordOption(t *testing.T) {
	t.Parallel()
	engine, game := saveStoreEngine(t, "My Game")

	dir := t.TempDir()
	zipPath := filepath.Join(dir, "saves.zip")
	saveStoreZip(t, zipPath, map[string]string{"persistent": "pw-bytes"})

	set, err := engine.Import(game, zipPath, ImportOptions{Name: "enc", Password: "f95zone"})
	if err != nil {
		t.Fatalf("Import with password option: %v", err)
	}
	if set.Name != "enc" {
		t.Errorf("Name = %q, want %q", set.Name, "enc")
	}
	saveStoreAssertFile(t, filepath.Join(engine.SetPath(game, set.Name), "persistent"), "pw-bytes")
}

func TestImportSingleFile(t *testing.T) {
	t.Parallel()
	engine, game := saveStoreEngine(t, "My Game")

	src := filepath.Join(t.TempDir(), "Save01.rvdata2")
	const content = "save-bytes"
	saveTestWrite(t, src, content)

	set, err := engine.Import(game, src, ImportOptions{Name: "single"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if set.ApplyRoot != "" {
		t.Errorf("ApplyRoot = %q, want empty", set.ApplyRoot)
	}
	if set.FileCount != 1 {
		t.Errorf("FileCount = %d, want 1", set.FileCount)
	}
	if want := int64(len(content)); set.SizeBytes != want {
		t.Errorf("SizeBytes = %d, want %d", set.SizeBytes, want)
	}
	saveStoreAssertFile(t, filepath.Join(engine.SetPath(game, set.Name), "Save01.rvdata2"), content)
}

// ---------------------------------------------------------------------------
// Name uniqueness
// ---------------------------------------------------------------------------

func TestImportNameUniqueness(t *testing.T) {
	t.Parallel()
	engine, game := saveStoreEngine(t, "My Game")

	first := t.TempDir()
	saveTestWrite(t, filepath.Join(first, "persistent"), "a")
	second := t.TempDir()
	saveTestWrite(t, filepath.Join(second, "persistent"), "b")

	set1, err := engine.Import(game, first, ImportOptions{Name: "same"})
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	set2, err := engine.Import(game, second, ImportOptions{Name: "same"})
	if err != nil {
		t.Fatalf("second import: %v", err)
	}

	if set1.Name != "same" {
		t.Errorf("first Name = %q, want %q", set1.Name, "same")
	}
	if set2.Name != "same-2" {
		t.Errorf("second Name = %q, want %q", set2.Name, "same-2")
	}

	for _, name := range []string{"same", "same-2"} {
		got, err := engine.DB().GetSaveSet(game.ID, name)
		if err != nil {
			t.Fatalf("GetSaveSet(%q): %v", name, err)
		}
		if got == nil {
			t.Errorf("save set row %q missing", name)
		}
	}
}

// TestImportReservesPreExistingDir proves the name claim is atomic: a directory
// already on disk (with no database row, e.g. left by a crashed import) is never
// reused or written into — the import reserves a suffixed name instead.
func TestImportReservesPreExistingDir(t *testing.T) {
	t.Parallel()
	engine, game := saveStoreEngine(t, "My Game")

	pre := engine.SetPath(game, "same")
	saveTestMkdir(t, pre)
	saveTestWrite(t, filepath.Join(pre, "sentinel"), "keep")

	src := t.TempDir()
	saveTestWrite(t, filepath.Join(src, "persistent"), "new")

	set, err := engine.Import(game, src, ImportOptions{Name: "same"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if set.Name != "same-2" {
		t.Errorf("Name = %q, want %q (pre-existing dir must be skipped)", set.Name, "same-2")
	}

	// The pre-existing dir is untouched and gains no database row; the new set
	// lands under the suffixed name.
	saveStoreAssertFile(t, filepath.Join(pre, "sentinel"), "keep")
	saveStoreAssertAbsent(t, filepath.Join(pre, "persistent"))
	saveStoreAssertFile(t, filepath.Join(engine.SetPath(game, "same-2"), "persistent"), "new")
	if got, err := engine.DB().GetSaveSet(game.ID, "same"); err != nil {
		t.Fatalf("GetSaveSet(same): %v", err)
	} else if got != nil {
		t.Errorf("no row should exist for %q, got %+v", "same", got)
	}
}

// TestImportFailureRemovesOnlyReservedDir proves failure cleanup only removes
// the directory this call reserved, never a sibling owned by someone else.
func TestImportFailureRemovesOnlyReservedDir(t *testing.T) {
	t.Parallel()
	engine, game := saveStoreEngine(t, "My Game")

	// A sibling directory owns the base name; the import reserves "taken-2".
	other := engine.SetPath(game, "taken")
	saveTestMkdir(t, other)
	saveTestWrite(t, filepath.Join(other, "sentinel"), "keep")

	src := t.TempDir()
	saveTestWrite(t, filepath.Join(src, "persistent"), "x")
	if err := os.Symlink("persistent", filepath.Join(src, "link.save")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := engine.Import(game, src, ImportOptions{Name: "taken"}); err == nil {
		t.Fatal("Import of a source containing a symlink should fail")
	}

	// The pre-existing dir survives; the reserved dir was cleaned up.
	saveStoreAssertFile(t, filepath.Join(other, "sentinel"), "keep")
	saveStoreAssertAbsent(t, engine.SetPath(game, "taken-2"))
	if got, err := engine.DB().GetSaveSet(game.ID, "taken-2"); err != nil {
		t.Fatalf("GetSaveSet(taken-2): %v", err)
	} else if got != nil {
		t.Errorf("row for taken-2 should not exist, got %+v", got)
	}
}

// ---------------------------------------------------------------------------
// Remove
// ---------------------------------------------------------------------------

func TestRemove(t *testing.T) {
	t.Parallel()

	t.Run("removes dir and row", func(t *testing.T) {
		t.Parallel()
		engine, game := saveStoreEngine(t, "My Game")
		src := t.TempDir()
		saveTestWrite(t, filepath.Join(src, "persistent"), "x")

		set, err := engine.Import(game, src, ImportOptions{Name: "to-remove"})
		if err != nil {
			t.Fatalf("Import: %v", err)
		}
		setDir := engine.SetPath(game, set.Name)
		if _, err := os.Stat(setDir); err != nil {
			t.Fatalf("set dir should exist before Remove: %v", err)
		}

		if err := engine.Remove(game, set.Name); err != nil {
			t.Fatalf("Remove: %v", err)
		}
		saveStoreAssertAbsent(t, setDir)

		got, err := engine.DB().GetSaveSet(game.ID, set.Name)
		if err != nil {
			t.Fatalf("GetSaveSet: %v", err)
		}
		if got != nil {
			t.Errorf("row still present after Remove: %+v", got)
		}
	})

	t.Run("unknown set errors", func(t *testing.T) {
		t.Parallel()
		engine, game := saveStoreEngine(t, "My Game")
		if err := engine.Remove(game, "does-not-exist"); err == nil {
			t.Fatal("Remove of unknown set should error")
		}
	})

	t.Run("refuses the active set", func(t *testing.T) {
		t.Parallel()
		engine, game := saveStoreEngine(t, "My Game")
		src := t.TempDir()
		saveTestWrite(t, filepath.Join(src, "persistent"), "x")

		set, err := engine.Import(game, src, ImportOptions{Name: "active-set"})
		if err != nil {
			t.Fatalf("Import: %v", err)
		}
		if err := engine.DB().SetActiveSaveSet(game.ID, set.ID); err != nil {
			t.Fatalf("SetActiveSaveSet: %v", err)
		}
		setDir := engine.SetPath(game, set.Name)

		err = engine.Remove(game, set.Name)
		if err == nil {
			t.Fatal("Remove of the active set should error")
		}
		if !strings.Contains(err.Error(), "active") {
			t.Errorf("error = %v, want it to mention the active set", err)
		}
		// Both the directory and the row must survive a refused removal.
		saveStoreAssertFile(t, filepath.Join(setDir, "persistent"), "x")
		got, err := engine.DB().GetSaveSet(game.ID, set.Name)
		if err != nil {
			t.Fatalf("GetSaveSet: %v", err)
		}
		if got == nil {
			t.Error("row should survive a refused Remove")
		}

		// Once another set is active, the formerly-active set can be removed.
		other, err := engine.Import(game, src, ImportOptions{Name: "newer"})
		if err != nil {
			t.Fatalf("second Import: %v", err)
		}
		if err := engine.DB().SetActiveSaveSet(game.ID, other.ID); err != nil {
			t.Fatalf("SetActiveSaveSet(other): %v", err)
		}
		if err := engine.Remove(game, set.Name); err != nil {
			t.Fatalf("Remove of a no-longer-active set: %v", err)
		}
		saveStoreAssertAbsent(t, setDir)
	})

	t.Run("hostile name cannot escape root", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		engine, game := saveStoreEngineAt(t, base, "My Game")

		// The engine root is <base>/saves; "../../evil" cleans to <base>/evil.
		sentinel := filepath.Join(base, "evil", "keep.txt")
		saveTestWrite(t, sentinel, "do-not-delete")

		const hostile = "../../evil"
		if _, err := engine.DB().CreateSaveSet(&db.SaveSet{GameID: game.ID, Name: hostile}); err != nil {
			t.Fatalf("seed hostile row: %v", err)
		}

		if err := engine.Remove(game, hostile); err == nil {
			t.Fatal("Remove with escaping name should error")
		}

		saveStoreAssertFile(t, sentinel, "do-not-delete")
		got, err := engine.DB().GetSaveSet(game.ID, hostile)
		if err != nil {
			t.Fatalf("GetSaveSet: %v", err)
		}
		if got == nil {
			t.Error("row should survive a refused Remove")
		}
	})
}

// ---------------------------------------------------------------------------
// List and SetPath
// ---------------------------------------------------------------------------

func TestListAndSetPath(t *testing.T) {
	t.Parallel()
	engine, game := saveStoreEngine(t, "My Game")

	srcA := t.TempDir()
	saveTestWrite(t, filepath.Join(srcA, "persistent"), "A")
	srcB := t.TempDir()
	saveTestWrite(t, filepath.Join(srcB, "persistent"), "B")

	setA, err := engine.Import(game, srcA, ImportOptions{Name: "alpha"})
	if err != nil {
		t.Fatalf("import alpha: %v", err)
	}
	setB, err := engine.Import(game, srcB, ImportOptions{Name: "beta"})
	if err != nil {
		t.Fatalf("import beta: %v", err)
	}

	list, err := engine.List(game)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len(List) = %d, want 2", len(list))
	}
	if list[0].Name != setB.Name {
		t.Errorf("List[0].Name = %q, want newest %q", list[0].Name, setB.Name)
	}
	if list[1].Name != setA.Name {
		t.Errorf("List[1].Name = %q, want %q", list[1].Name, setA.Name)
	}

	// SetPath is <root>/<gameID>/<name>.
	want := filepath.Join(engine.Root(), fmt.Sprintf("%d", game.ID), setA.Name)
	if got := engine.SetPath(game, setA.Name); got != want {
		t.Errorf("SetPath = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Legacy set-directory migration
// ---------------------------------------------------------------------------

func TestSetPathLegacyMigration(t *testing.T) {
	t.Parallel()

	t.Run("single legacy dir is renamed", func(t *testing.T) {
		t.Parallel()
		engine, game := saveStoreEngine(t, "Old Title")
		legacyRoot := filepath.Join(engine.Root(), fmt.Sprintf("%d-old-title", game.ID))
		saveTestWrite(t, filepath.Join(legacyRoot, "my-saves", "persistent"), "legacy-bytes")

		newRoot := filepath.Join(engine.Root(), fmt.Sprintf("%d", game.ID))
		if got, want := engine.SetPath(game, "my-saves"), filepath.Join(newRoot, "my-saves"); got != want {
			t.Errorf("SetPath = %q, want %q", got, want)
		}
		saveStoreAssertFile(t, filepath.Join(newRoot, "my-saves", "persistent"), "legacy-bytes")
		saveStoreAssertAbsent(t, legacyRoot)
	})

	t.Run("existing id dir wins over legacy", func(t *testing.T) {
		t.Parallel()
		engine, game := saveStoreEngine(t, "Old Title")
		idRoot := filepath.Join(engine.Root(), fmt.Sprintf("%d", game.ID))
		saveTestWrite(t, filepath.Join(idRoot, "my-saves", "persistent"), "new-bytes")
		saveTestWrite(t, filepath.Join(engine.Root(), fmt.Sprintf("%d-old-title", game.ID), "my-saves", "persistent"), "legacy-bytes")

		if got, want := engine.SetPath(game, "my-saves"), filepath.Join(idRoot, "my-saves"); got != want {
			t.Errorf("SetPath = %q, want %q", got, want)
		}
		saveStoreAssertFile(t, filepath.Join(idRoot, "my-saves", "persistent"), "new-bytes")
	})

	t.Run("ambiguous legacy dirs are left alone", func(t *testing.T) {
		t.Parallel()
		engine, game := saveStoreEngine(t, "Old Title")
		saveTestWrite(t, filepath.Join(engine.Root(), fmt.Sprintf("%d-old-a", game.ID), "s.keep"), "a")
		saveTestWrite(t, filepath.Join(engine.Root(), fmt.Sprintf("%d-old-b", game.ID), "s.keep"), "b")

		if got, want := engine.SetPath(game, "nope"), filepath.Join(engine.Root(), fmt.Sprintf("%d", game.ID), "nope"); got != want {
			t.Errorf("SetPath = %q, want %q", got, want)
		}
		saveStoreAssertFile(t, filepath.Join(engine.Root(), fmt.Sprintf("%d-old-a", game.ID), "s.keep"), "a")
		saveStoreAssertFile(t, filepath.Join(engine.Root(), fmt.Sprintf("%d-old-b", game.ID), "s.keep"), "b")
	})
}

// ---------------------------------------------------------------------------
// Symlink refusal
// ---------------------------------------------------------------------------

func TestCopyTreeRefusesSymlink(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	saveTestWrite(t, filepath.Join(src, "persistent"), "data")
	if err := os.Symlink("persistent", filepath.Join(src, "link.save")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "out")
	err := copyTree(src, dst)
	if err == nil {
		t.Fatal("copyTree should refuse a symlink entry")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("error = %v, want it to mention symlink", err)
	}
	if _, statErr := os.Lstat(filepath.Join(dst, "link.save")); !os.IsNotExist(statErr) {
		t.Errorf("symlink recreated at destination (stat err = %v)", statErr)
	}
}

// ---------------------------------------------------------------------------
// Explicit apply_root override
// ---------------------------------------------------------------------------

func TestImportApplyRootOverride(t *testing.T) {
	t.Parallel()

	t.Run("explicit existing root honored", func(t *testing.T) {
		t.Parallel()
		engine, game := saveStoreEngine(t, "My Game")
		src := t.TempDir()
		saveTestWrite(t, filepath.Join(src, "sub", "dir", "persistent"), "x")

		set, err := engine.Import(game, src, ImportOptions{Name: "rooted", ApplyRoot: "sub/dir"})
		if err != nil {
			t.Fatalf("Import: %v", err)
		}
		if set.ApplyRoot != "sub/dir" {
			t.Errorf("ApplyRoot = %q, want %q", set.ApplyRoot, "sub/dir")
		}
	})

	t.Run("escaping root rejected with no partial dir", func(t *testing.T) {
		t.Parallel()
		engine, game := saveStoreEngine(t, "My Game")
		src := t.TempDir()
		saveTestWrite(t, filepath.Join(src, "persistent"), "x")

		_, err := engine.Import(game, src, ImportOptions{Name: "escaping", ApplyRoot: "../outside"})
		if err == nil {
			t.Fatal("escaping apply_root should error")
		}
		saveStoreAssertAbsent(t, engine.SetPath(game, "escaping"))
	})
}

// ---------------------------------------------------------------------------
// Option defaults
// ---------------------------------------------------------------------------

func TestImportDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		opts        ImportOptions
		wantName    string
		wantPrefix  string
		wantLabel   string // "" means "same as the resulting name"
		wantSource  string
		wantVersion string
	}{
		{
			name:        "name wins over label",
			opts:        ImportOptions{Name: "Chosen Name", Label: "Ignored Label"},
			wantName:    "chosen-name",
			wantLabel:   "Ignored Label",
			wantSource:  "import",
			wantVersion: "1.2.3",
		},
		{
			// Label supplies the default *name*, but the Label field itself
			// keeps the raw value when one was given.
			name:        "label is the fallback name",
			opts:        ImportOptions{Label: "Nice Label"},
			wantName:    "nice-label",
			wantLabel:   "Nice Label",
			wantSource:  "import",
			wantVersion: "1.2.3",
		},
		{
			name:        "no name uses import prefix",
			opts:        ImportOptions{},
			wantPrefix:  "import-",
			wantSource:  "import",
			wantVersion: "1.2.3",
		},
		{
			name:        "source defaults to import",
			opts:        ImportOptions{Name: "src-default"},
			wantName:    "src-default",
			wantLabel:   "src-default",
			wantSource:  "import",
			wantVersion: "1.2.3",
		},
		{
			name:        "explicit source preserved",
			opts:        ImportOptions{Name: "src-explicit", Source: "f95_link"},
			wantName:    "src-explicit",
			wantLabel:   "src-explicit",
			wantSource:  "f95_link",
			wantVersion: "1.2.3",
		},
		{
			name:        "version defaults to game version",
			opts:        ImportOptions{Name: "ver-default"},
			wantName:    "ver-default",
			wantLabel:   "ver-default",
			wantSource:  "import",
			wantVersion: "1.2.3",
		},
		{
			name:        "explicit version preserved",
			opts:        ImportOptions{Name: "ver-explicit", GameVersion: "9.9"},
			wantName:    "ver-explicit",
			wantLabel:   "ver-explicit",
			wantSource:  "import",
			wantVersion: "9.9",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			engine, game := saveStoreEngine(t, "Defaults Game")
			game.Version = "1.2.3" // Import reads the in-memory game.

			src := t.TempDir()
			saveTestWrite(t, filepath.Join(src, "persistent"), "x")

			set, err := engine.Import(game, src, tc.opts)
			if err != nil {
				t.Fatalf("Import: %v", err)
			}

			if tc.wantName != "" && set.Name != tc.wantName {
				t.Errorf("Name = %q, want %q", set.Name, tc.wantName)
			}
			if tc.wantPrefix != "" && !strings.HasPrefix(set.Name, tc.wantPrefix) {
				t.Errorf("Name = %q, want prefix %q", set.Name, tc.wantPrefix)
			}

			wantLabel := tc.wantLabel
			if wantLabel == "" {
				wantLabel = set.Name
			}
			if set.Label != wantLabel {
				t.Errorf("Label = %q, want %q", set.Label, wantLabel)
			}
			if set.Source != tc.wantSource {
				t.Errorf("Source = %q, want %q", set.Source, tc.wantSource)
			}
			if set.GameVersion != tc.wantVersion {
				t.Errorf("GameVersion = %q, want %q", set.GameVersion, tc.wantVersion)
			}
		})
	}
}
