package saves

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mili/moxie/internal/db"
)

// saveTestMkdir creates path and every missing parent.
func saveTestMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

// saveTestWrite writes content to path, creating parent directories.
func saveTestWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// saveLocationIsZero reports whether a Location carries no data. Location is not
// comparable because Globs is a slice, so tests check fields individually.
func saveLocationIsZero(l Location) bool {
	return l.Kind == "" && l.Path == "" && l.Globs == nil && l.Confidence == 0 && l.Note == ""
}

// saveFindLocation returns the first candidate whose Path equals want.
func saveFindLocation(t *testing.T, candidates []Location, want string) (Location, bool) {
	t.Helper()
	for _, c := range candidates {
		if filepath.Clean(c.Path) == filepath.Clean(want) {
			return c, true
		}
	}
	return Location{}, false
}

func TestResolveRenpyPortableGameSaves(t *testing.T) {
	t.Parallel()
	gameDir := t.TempDir()
	savesDir := filepath.Join(gameDir, "game", "saves")
	saveTestMkdir(t, savesDir)

	game := &db.Game{Engine: "RenPy", Title: "My Game", Path: gameDir}
	got := resolveCandidates(game, t.TempDir(), nil)

	loc, ok := saveFindLocation(t, got, savesDir)
	if !ok {
		t.Fatalf("expected portable candidate %s, got %+v", savesDir, got)
	}
	if loc.Kind != KindDir {
		t.Errorf("Kind = %q, want %q", loc.Kind, KindDir)
	}
	if loc.Confidence != 0.95 {
		t.Errorf("Confidence = %v, want 0.95", loc.Confidence)
	}
	if loc.Note != "portable saves dir" {
		t.Errorf("Note = %q, want %q", loc.Note, "portable saves dir")
	}
}

func TestResolveRenpyPortableTopLevel(t *testing.T) {
	t.Parallel()
	gameDir := t.TempDir()
	savesDir := filepath.Join(gameDir, "saves")
	saveTestMkdir(t, savesDir)

	game := &db.Game{Engine: "RenPy", Title: "My Game", Path: gameDir}
	got := resolveCandidates(game, t.TempDir(), nil)

	loc, ok := saveFindLocation(t, got, savesDir)
	if !ok {
		t.Fatalf("expected top-level portable candidate %s, got %+v", savesDir, got)
	}
	if loc.Confidence != 0.95 {
		t.Errorf("Confidence = %v, want 0.95", loc.Confidence)
	}
}

func TestResolveRenpyDetectedByScripts(t *testing.T) {
	t.Parallel()
	gameDir := t.TempDir()
	saveTestWrite(t, filepath.Join(gameDir, "game", "script.rpyc"), "x")
	savesDir := filepath.Join(gameDir, "game", "saves")
	saveTestMkdir(t, savesDir)

	// Engine left blank: detection must come from the .rpyc on disk.
	game := &db.Game{Title: "My Game", Path: gameDir}
	got := resolveCandidates(game, t.TempDir(), nil)

	if _, ok := saveFindLocation(t, got, savesDir); !ok {
		t.Fatalf("expected portable candidate from .rpyc detection, got %+v", got)
	}
}

func TestResolveRenpyOptionsNameMatch(t *testing.T) {
	t.Parallel()
	gameDir := t.TempDir()
	saveTestWrite(t, filepath.Join(gameDir, "game", "options.rpy"),
		"define config.name = _(\"My Game\")\ndefine config.save_directory = \"MyGame-1559587701\"\n")

	home := t.TempDir()
	saveDir := filepath.Join(home, ".renpy", "MyGame-1559587701")
	saveTestMkdir(t, saveDir)

	game := &db.Game{Engine: "RenPy", Title: "My Game", Path: gameDir}
	got := resolveCandidates(game, home, nil)

	loc, ok := saveFindLocation(t, got, saveDir)
	if !ok {
		t.Fatalf("expected options-name candidate %s, got %+v", saveDir, got)
	}
	if loc.Confidence != 0.93 {
		t.Errorf("Confidence = %v, want 0.93", loc.Confidence)
	}
	if loc.Note != "renpy native" {
		t.Errorf("Note = %q, want %q", loc.Note, "renpy native")
	}
}

func TestReadSaveDirectoryName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"double quoted", `define config.save_directory = "MyGame-1"`, "MyGame-1"},
		{"single quoted", `define config.save_directory = 'OtherGame-2'`, "OtherGame-2"},
		{"spacing", "config.save_directory=\"Tight-3\"", "Tight-3"},
		{"dynamic percent", `config.save_directory = "game_%s"`, ""},
		{"dynamic dollar", `config.save_directory = "saves/$USER"`, ""},
		{"dynamic paren", `config.save_directory = "My (Game)"`, ""},
		{"unquoted dynamic", `config.save_directory = str(config.name)`, ""},
		{"not present", "define config.name = _(\"My Game\")\n", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			options := filepath.Join(t.TempDir(), "options.rpy")
			saveTestWrite(t, options, tc.content)
			if got := readSaveDirectoryName(options); got != tc.want {
				t.Errorf("readSaveDirectoryName(%q) = %q, want %q", tc.content, got, tc.want)
			}
		})
	}

	t.Run("missing file", func(t *testing.T) {
		t.Parallel()
		if got := readSaveDirectoryName(filepath.Join(t.TempDir(), "nope.rpy")); got != "" {
			t.Errorf("missing file: got %q, want empty", got)
		}
	})
}

func TestResolveRenpyTitleMatchAndSkip(t *testing.T) {
	t.Parallel()
	gameDir := filepath.Join(t.TempDir(), "MyGame")
	saveTestMkdir(t, gameDir)

	home := t.TempDir()
	want := filepath.Join(home, ".renpy", "MyGame-1559587701")
	saveTestMkdir(t, want)
	unrelated := filepath.Join(home, ".renpy", "SomethingElse-42")
	saveTestMkdir(t, unrelated)

	game := &db.Game{Engine: "RenPy", Title: "My Game", Path: gameDir}
	got := resolveCandidates(game, home, nil)

	loc, ok := saveFindLocation(t, got, want)
	if !ok {
		t.Fatalf("expected title-matched candidate %s, got %+v", want, got)
	}
	if loc.Confidence != 0.9 {
		t.Errorf("Confidence = %v, want 0.9", loc.Confidence)
	}
	if _, ok := saveFindLocation(t, got, unrelated); ok {
		t.Errorf("unrelated dir must not be emitted, got %+v", got)
	}
}

func TestResolveRenpyWineRoot(t *testing.T) {
	t.Parallel()
	gameDir := filepath.Join(t.TempDir(), "MyGame")
	saveTestMkdir(t, gameDir)

	home := t.TempDir()
	want := filepath.Join(home, ".wine", "drive_c", "users", "steamuser",
		"AppData", "Roaming", "RenPy", "MyGame-123")
	saveTestMkdir(t, want)

	game := &db.Game{Engine: "RenPy", Title: "My Game", Path: gameDir}
	got := resolveCandidates(game, home, nil)

	loc, ok := saveFindLocation(t, got, want)
	if !ok {
		t.Fatalf("expected wine candidate %s, got %+v", want, got)
	}
	if loc.Note != "wine prefix" {
		t.Errorf("Note = %q, want %q", loc.Note, "wine prefix")
	}
	if loc.Confidence != 0.9 {
		t.Errorf("Confidence = %v, want 0.9", loc.Confidence)
	}
}

func TestResolveRenpySteamProton(t *testing.T) {
	t.Parallel()
	gameDir := filepath.Join(t.TempDir(), "MyGame")
	saveTestMkdir(t, gameDir)

	home := t.TempDir()
	steamRoot := t.TempDir()
	proton := filepath.Join(steamRoot, "steamapps", "compatdata", "12345",
		"pfx", "drive_c", "users", "steamuser", "AppData", "Roaming", "RenPy",
		"MyGame-123")
	saveTestMkdir(t, proton)

	game := &db.Game{Engine: "RenPy", Title: "My Game", Path: gameDir, SteamAppID: 12345}
	got := resolveCandidates(game, home, []string{steamRoot})

	loc, ok := saveFindLocation(t, got, proton)
	if !ok {
		t.Fatalf("expected proton candidate %s, got %+v", proton, got)
	}
	if loc.Note != "steam proton prefix" {
		t.Errorf("Note = %q, want %q", loc.Note, "steam proton prefix")
	}

	// Without a SteamAppID the compatdata tree is irrelevant and must be skipped.
	game.SteamAppID = 0
	got = resolveCandidates(game, home, []string{steamRoot})
	for _, c := range got {
		if c.Note == "steam proton prefix" {
			t.Errorf("SteamAppID == 0 must skip proton prefixes, got %+v", c)
		}
	}
}

func TestResolveRPGMMVExisting(t *testing.T) {
	t.Parallel()
	gameDir := t.TempDir()
	saveDir := filepath.Join(gameDir, "www", "save")
	saveTestMkdir(t, saveDir)

	game := &db.Game{Engine: "RPGM", Title: "Game", Path: gameDir}
	got := resolveCandidates(game, t.TempDir(), nil)

	if len(got) != 1 {
		t.Fatalf("expected exactly 1 candidate, got %+v", got)
	}
	loc := got[0]
	if loc.Kind != KindDir || filepath.Clean(loc.Path) != filepath.Clean(saveDir) {
		t.Errorf("location = %+v, want dir %s", loc, saveDir)
	}
	if loc.Confidence != 0.85 {
		t.Errorf("Confidence = %v, want 0.85", loc.Confidence)
	}
	if loc.Note != "rpg maker mv save dir" {
		t.Errorf("Note = %q, want %q", loc.Note, "rpg maker mv save dir")
	}
}

func TestResolveRPGMMZNotYetCreated(t *testing.T) {
	t.Parallel()
	gameDir := t.TempDir()
	want := filepath.Join(gameDir, "save")

	game := &db.Game{Engine: "RPGM", Title: "Game", Path: gameDir}
	got := resolveCandidates(game, t.TempDir(), nil)

	if len(got) != 1 {
		t.Fatalf("expected exactly 1 candidate, got %+v", got)
	}
	loc := got[0]
	if filepath.Clean(loc.Path) != filepath.Clean(want) {
		t.Errorf("Path = %q, want %q", loc.Path, want)
	}
	if loc.Confidence != 0.7 {
		t.Errorf("Confidence = %v, want 0.7 (not created yet)", loc.Confidence)
	}
	if loc.Note != "rpg maker mz save dir (not created yet)" {
		t.Errorf("Note = %q, want the not-created MZ note", loc.Note)
	}
}

func TestResolveRPGMVXAceFiles(t *testing.T) {
	t.Parallel()
	gameDir := t.TempDir()
	saveTestWrite(t, filepath.Join(gameDir, "Save1.rvdata2"), "x")

	game := &db.Game{Engine: "RPGM", Title: "Game", Path: gameDir}
	got := resolveCandidates(game, t.TempDir(), nil)

	if len(got) != 1 {
		t.Fatalf("expected exactly 1 candidate, got %+v", got)
	}
	loc := got[0]
	if loc.Kind != KindFiles {
		t.Errorf("Kind = %q, want %q", loc.Kind, KindFiles)
	}
	if filepath.Clean(loc.Path) != filepath.Clean(gameDir) {
		t.Errorf("Path = %q, want %q", loc.Path, gameDir)
	}
	wantGlobs := []string{"Save*.rvdata2", "Global.rvdata2"}
	if len(loc.Globs) != len(wantGlobs) {
		t.Fatalf("Globs = %v, want %v", loc.Globs, wantGlobs)
	}
	for i, g := range wantGlobs {
		if loc.Globs[i] != g {
			t.Errorf("Globs[%d] = %q, want %q", i, loc.Globs[i], g)
		}
	}
	if loc.Confidence != 0.9 {
		t.Errorf("Confidence = %v, want 0.9", loc.Confidence)
	}
}

func TestResolveRPGMVXAceGlobal(t *testing.T) {
	t.Parallel()
	gameDir := t.TempDir()
	saveTestWrite(t, filepath.Join(gameDir, "Global.rvdata2"), "x")

	game := &db.Game{Engine: "RPGM", Path: gameDir}
	got := resolveCandidates(game, t.TempDir(), nil)
	if len(got) != 1 || got[0].Kind != KindFiles {
		t.Fatalf("expected VX Ace files location, got %+v", got)
	}
}

func TestResolveUnknownEngine(t *testing.T) {
	t.Parallel()
	for _, engine := range []string{"Unity", "Godot", "Others", ""} {
		game := &db.Game{Engine: engine, Title: "Game", Path: t.TempDir()}
		if got := resolveCandidates(game, t.TempDir(), nil); got != nil {
			t.Errorf("engine %q: got %+v, want nil", engine, got)
		}
	}
}

func TestResolveNilGame(t *testing.T) {
	t.Parallel()
	if got := resolveCandidates(nil, t.TempDir(), nil); got != nil {
		t.Errorf("nil game: got %+v, want nil", got)
	}
}

func TestResolveBestFirstOrdering(t *testing.T) {
	t.Parallel()
	gameDir := filepath.Join(t.TempDir(), "MyGame")
	saveTestMkdir(t, gameDir)
	saveTestWrite(t, filepath.Join(gameDir, "game", "options.rpy"),
		`config.save_directory = "MyGame-1559587701"`)
	portable := filepath.Join(gameDir, "game", "saves")
	saveTestMkdir(t, portable)

	home := t.TempDir()
	saveTestMkdir(t, filepath.Join(home, ".renpy", "MyGame-1559587701"))

	game := &db.Game{Engine: "RenPy", Title: "My Game", Path: gameDir}
	got := resolveCandidates(game, home, nil)
	if len(got) < 2 {
		t.Fatalf("expected at least 2 candidates, got %+v", got)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Confidence < got[i].Confidence {
			t.Fatalf("candidates not best-first: %+v", got)
		}
	}
	if got[0].Confidence != 0.95 {
		t.Errorf("first candidate confidence = %v, want highest (0.95)", got[0].Confidence)
	}
}

func TestPickLocationManualOverrideUnknownPath(t *testing.T) {
	t.Parallel()
	candidates := []Location{{Kind: KindDir, Path: "/a", Confidence: 0.9, Note: "renpy native"}}
	got, rest := PickLocation(candidates, "/b")
	if rest != nil {
		t.Errorf("pick list = %+v, want nil", rest)
	}
	if got.Kind != KindDir || got.Path != "/b" || got.Confidence != 1 || got.Note != "manual override" {
		t.Errorf("got %+v, want dir /b confidence 1 manual override", got)
	}
}

func TestPickLocationManualOverrideMatchesCandidate(t *testing.T) {
	t.Parallel()
	candidates := []Location{{
		Kind:       KindFiles,
		Path:       "/save/",
		Globs:      []string{"Save*.rvdata2"},
		Confidence: 0.9,
		Note:       "vx ace save files in game dir",
	}}
	got, rest := PickLocation(candidates, "/save")
	if rest != nil {
		t.Errorf("pick list = %+v, want nil", rest)
	}
	if got.Kind != KindFiles {
		t.Errorf("Kind = %q, want %q (candidate Kind preserved)", got.Kind, KindFiles)
	}
	if len(got.Globs) != 1 || got.Globs[0] != "Save*.rvdata2" {
		t.Errorf("Globs = %v, want candidate Globs preserved", got.Globs)
	}
	if got.Confidence != 0.9 {
		t.Errorf("Confidence = %v, want candidate confidence 0.9", got.Confidence)
	}
	if got.Note != "manual override" {
		t.Errorf("Note = %q, want %q", got.Note, "manual override")
	}
}

func TestPickLocationAmbiguous(t *testing.T) {
	t.Parallel()
	candidates := []Location{{Path: "/a", Confidence: 0.9}, {Path: "/b", Confidence: 0.8}}
	got, rest := PickLocation(candidates, "")
	if !saveLocationIsZero(got) {
		t.Errorf("got %+v, want zero location", got)
	}
	if len(rest) != 2 {
		t.Errorf("pick list = %+v, want the 2 candidates", rest)
	}
}

func TestPickLocationSingle(t *testing.T) {
	t.Parallel()
	candidates := []Location{{Kind: KindDir, Path: "/a", Confidence: 0.95}}
	got, rest := PickLocation(candidates, "")
	if rest != nil {
		t.Errorf("pick list = %+v, want nil", rest)
	}
	if got.Path != "/a" || got.Confidence != 0.95 {
		t.Errorf("got %+v, want the single candidate", got)
	}
}

func TestPickLocationNone(t *testing.T) {
	t.Parallel()
	got, rest := PickLocation(nil, "")
	if !saveLocationIsZero(got) {
		t.Errorf("got %+v, want zero location", got)
	}
	if rest != nil {
		t.Errorf("pick list = %+v, want nil", rest)
	}
}
