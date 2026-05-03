package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func makeDir(t *testing.T, paths ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, p := range paths {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if p[len(p)-1] == '/' {
			if err := os.MkdirAll(full, 0755); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.WriteFile(full, []byte("test"), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}

func TestDetectUnity(t *testing.T) {
	dir := makeDir(t, "Game.exe", "Game_Data/", "UnityPlayer.dll")
	result := Detect(dir)
	if result.Engine != Unity {
		t.Errorf("expected Unity, got %s", result.Engine)
	}
}

func TestDetectUnityDataFolderOnly(t *testing.T) {
	dir := makeDir(t, "Game.exe", "Game_Data/")
	result := Detect(dir)
	if result.Engine != Unity {
		t.Errorf("expected Unity from _Data folder, got %s", result.Engine)
	}
}

func TestDetectRenPy(t *testing.T) {
	dir := makeDir(t, "renpy/", "game/")
	result := Detect(dir)
	if result.Engine != RenPy {
		t.Errorf("expected RenPy, got %s", result.Engine)
	}
}

func TestDetectRenPyScripts(t *testing.T) {
	dir := makeDir(t, "game/", "game/script.rpyc")
	result := Detect(dir)
	if result.Engine != RenPy {
		t.Errorf("expected RenPy from scripts, got %s", result.Engine)
	}
}

func TestDetectRPGMMV(t *testing.T) {
	dir := makeDir(t, "www/", "package.json")
	os.WriteFile(filepath.Join(dir, "www", "package.json"), []byte(`{"name":"KADOKAWA/RPGMV"}`), 0644)
	result := Detect(dir)
	if result.Engine != RPGM {
		t.Errorf("expected RPGM, got %s (%s)", result.Engine, result.MatchedBy)
	}
}

func TestDetectRPGMMZ(t *testing.T) {
	dir := makeDir(t, "www/", "package.json")
	os.WriteFile(filepath.Join(dir, "www", "package.json"), []byte(`{"name":"rmmz-game"}`), 0644)
	result := Detect(dir)
	if result.Engine != RPGM {
		t.Errorf("expected RPGM, got %s (%s)", result.Engine, result.MatchedBy)
	}
}

func TestDetectRPGMVXAce(t *testing.T) {
	dir := makeDir(t, "Game.rgss3a", "Game.exe")
	result := Detect(dir)
	if result.Engine != RPGM {
		t.Errorf("expected RPGM, got %s", result.Engine)
	}
}

func TestDetectRPGMVX(t *testing.T) {
	dir := makeDir(t, "Game.rgss2a")
	result := Detect(dir)
	if result.Engine != RPGM {
		t.Errorf("expected RPGM (VX), got %s", result.Engine)
	}
}

func TestDetectRPGMGeneric(t *testing.T) {
	dir := makeDir(t, "Game.exe", "Game.ini", "Data/")
	result := Detect(dir)
	if result.Engine != RPGM {
		t.Errorf("expected RPGM (generic), got %s", result.Engine)
	}
}

func TestDetectRPGMIniContent(t *testing.T) {
	dir := makeDir(t, "Game.ini")
	os.WriteFile(filepath.Join(dir, "Game.ini"), []byte("[RPGVXAce]\nLibrary=RGSS300.dll"), 0644)
	result := Detect(dir)
	if result.Engine != RPGM {
		t.Errorf("expected RPGM from INI content, got %s", result.Engine)
	}
}

func TestDetectRPGMIniNoContent(t *testing.T) {
	dir := makeDir(t, "Game.ini")
	os.WriteFile(filepath.Join(dir, "Game.ini"), []byte("[SomeGame]\nTitle=Foo"), 0644)
	result := Detect(dir)
	if result.Engine == RPGM {
		t.Errorf("expected NOT RPGM without RPG content in INI")
	}
}

func TestDetectUnrealEngine(t *testing.T) {
	dir := makeDir(t, "Engine/", "Engine/Binaries/")
	result := Detect(dir)
	if result.Engine != UnrealEngine {
		t.Errorf("expected UnrealEngine, got %s", result.Engine)
	}
}

func TestDetectHTML(t *testing.T) {
	dir := makeDir(t, "index.html")
	result := Detect(dir)
	if result.Engine != HTML {
		t.Errorf("expected HTML, got %s", result.Engine)
	}
}

func TestDetectHTMLGeneric(t *testing.T) {
	dir := makeDir(t, "game.html")
	result := Detect(dir)
	if result.Engine != HTML {
		t.Errorf("expected HTML from .html file, got %s", result.Engine)
	}
}

func TestDetectJava(t *testing.T) {
	dir := makeDir(t, "game.jar")
	result := Detect(dir)
	if result.Engine != Java {
		t.Errorf("expected Java, got %s", result.Engine)
	}
}

func TestDetectFlash(t *testing.T) {
	dir := makeDir(t, "game.swf")
	result := Detect(dir)
	if result.Engine != Flash {
		t.Errorf("expected Flash, got %s", result.Engine)
	}
}

func TestDetectGodotMapsToOthers(t *testing.T) {
	dir := makeDir(t, "game.pck")
	result := Detect(dir)
	if result.Engine != Others {
		t.Errorf("expected Others (Godot), got %s", result.Engine)
	}
}

func TestDetectElectronMapsToOthers(t *testing.T) {
	dir := makeDir(t, "resources.pak")
	result := Detect(dir)
	if result.Engine != Others {
		t.Errorf("expected Others (Electron), got %s", result.Engine)
	}
}

func TestDetectMugenDirsMapsToOthers(t *testing.T) {
	dir := makeDir(t, "chars/", "data/", "stages/", "font/")
	result := Detect(dir)
	if result.Engine != Others {
		t.Errorf("expected Others (Mugen), got %s (%s)", result.Engine, result.MatchedBy)
	}
}

func TestDetectMugenTooFewDirs(t *testing.T) {
	dir := makeDir(t, "chars/", "data/")
	result := Detect(dir)
	// With only 2 of 5 Mugen directories, the Mugen profile should NOT match.
	if result.Engine == Others && result.Confidence >= 0.90 {
		t.Errorf("expected Mugen profile NOT to match with only 2/5 dirs, got engine=%s confidence=%.2f matched_by=%q",
			result.Engine, result.Confidence, result.MatchedBy)
	}
}

func TestDetectQSP(t *testing.T) {
	dir := makeDir(t, "game.qsp")
	result := Detect(dir)
	if result.Engine != QSP {
		t.Errorf("expected QSP, got %s", result.Engine)
	}
}

func TestDetectADRIFT(t *testing.T) {
	dir := makeDir(t, "adventure.taf")
	result := Detect(dir)
	if result.Engine != ADRIFT {
		t.Errorf("expected ADRIFT, got %s", result.Engine)
	}
}

func TestDetectRAGS(t *testing.T) {
	dir := makeDir(t, "RAGS.exe")
	result := Detect(dir)
	if result.Engine != RAGS {
		t.Errorf("expected RAGS, got %s", result.Engine)
	}
}

func TestDetectTads(t *testing.T) {
	dir := makeDir(t, "story.gam")
	result := Detect(dir)
	if result.Engine != Tads {
		t.Errorf("expected Tads, got %s", result.Engine)
	}
}

func TestDetectWebGL(t *testing.T) {
	dir := makeDir(t, "index.html", "Build/")
	result := Detect(dir)
	if result.Engine != WebGL {
		t.Errorf("expected WebGL, got %s", result.Engine)
	}
}

func TestDetectWolfRPG(t *testing.T) {
	dir := makeDir(t, "Data/", "WolfRPGEditor.exe")
	result := Detect(dir)
	if result.Engine != WolfRPG {
		t.Errorf("expected WolfRPG, got %s", result.Engine)
	}
}

func TestDetectOthers(t *testing.T) {
	dir := makeDir(t, "random.txt", "data/")
	result := Detect(dir)
	if result.Engine != Others {
		t.Errorf("expected Others, got %s", result.Engine)
	}
	if result.Confidence != 0 {
		t.Errorf("expected confidence 0 for Others")
	}
}

func TestDetectEmptyDir(t *testing.T) {
	dir := makeDir(t)
	result := Detect(dir)
	if result.Engine != Others {
		t.Errorf("expected Others for empty dir, got %s", result.Engine)
	}
}

func TestDetectNonexistentDir(t *testing.T) {
	result := Detect("/nonexistent/path/12345")
	if result.Engine != Others {
		t.Errorf("expected Others for nonexistent dir, got %s", result.Engine)
	}
}

func TestAllEngines(t *testing.T) {
	engines := AllEngines()
	if len(engines) < 12 {
		t.Errorf("expected at least 12 engines, got %d", len(engines))
	}
	// Others should be last.
	if engines[len(engines)-1] != Others {
		t.Errorf("expected Others last, got %s", engines[len(engines)-1])
	}
}

func TestCanonicalEngines(t *testing.T) {
	engines := AllEngines()
	names := make(map[Engine]bool)
	for _, e := range engines {
		names[e] = true
	}
	canonical := []Engine{ADRIFT, Flash, HTML, Java, Others, QSP, RAGS, RPGM, RenPy, Tads, Unity, UnrealEngine, WebGL, WolfRPG}
	for _, want := range canonical {
		if !names[want] {
			t.Errorf("AllEngines() missing canonical engine: %s", want)
		}
	}
}

