package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mili/moxie/internal/config"
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

// TestDetectHTMLShallowIndex verifies the fallback search for index.html at
// shallow depth (source-repo layout, e.g. src/index.html).
func TestDetectHTMLShallowIndex(t *testing.T) {
	dir := makeDir(t, "src/index.html", "css/style.css")
	result := Detect(dir)
	if result.Engine != HTML {
		t.Errorf("expected HTML from shallow index.html, got %s (%s)", result.Engine, result.MatchedBy)
	}
}

// TestDetectHTMLIndexTooDeep verifies the shallow search does not match
// index.html beyond maxIndexDepth levels.
func TestDetectHTMLIndexTooDeep(t *testing.T) {
	dir := makeDir(t, "a/b/c/index.html")
	result := Detect(dir)
	if result.Engine == HTML {
		t.Errorf("expected NOT HTML with index.html at depth > 2, got %s (%s)", result.Engine, result.MatchedBy)
	}
}

// TestDetectHTMLIndexInContentDir verifies the shallow search skips
// non-content directories (js/, css/, img/, ...).
func TestDetectHTMLIndexInContentDir(t *testing.T) {
	dir := makeDir(t, "js/index.html")
	result := Detect(dir)
	if result.Engine == HTML {
		t.Errorf("expected NOT HTML with index.html inside non-content dir, got %s (%s)", result.Engine, result.MatchedBy)
	}
}

// TestDetectHTMLShallowTwine verifies the fallback also matches any .html
// entry file at shallow depth — Twine-compiled games ship precompiled.html
// (or similar) instead of index.html.
func TestDetectHTMLShallowTwine(t *testing.T) {
	dir := makeDir(t, "dist/precompiled.html", "src/config.json")
	result := Detect(dir)
	if result.Engine != HTML {
		t.Errorf("expected HTML from shallow precompiled.html, got %s (%s)", result.Engine, result.MatchedBy)
	}
}

// TestDetectHTMLDeepTwine verifies a .html file beyond maxIndexDepth levels
// does not match the shallow fallback.
func TestDetectHTMLDeepTwine(t *testing.T) {
	dir := makeDir(t, "a/b/c/precompiled.html")
	result := Detect(dir)
	if result.Engine == HTML {
		t.Errorf("expected NOT HTML with precompiled.html at depth > 2, got %s (%s)", result.Engine, result.MatchedBy)
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

// TestDetectFlashRuffle verifies that a game played via the Ruffle Flash
// emulator (ruffle.exe) is classified as Flash even without a root .swf.
func TestDetectFlashRuffle(t *testing.T) {
	dir := makeDir(t, "ruffle.exe")
	result := Detect(dir)
	if result.Engine != Flash {
		t.Errorf("expected Flash from ruffle.exe, got %s (%s)", result.Engine, result.MatchedBy)
	}
}

// TestDetectJavaBundledJRE verifies that a JRE-bundled Java game (Lilith's
// Throne layout: LT.exe + jre1.8.0_172/lib/*.jar, no root .jar) is
// classified as Java.
func TestDetectJavaBundledJRE(t *testing.T) {
	dir := makeDir(t, "LT.exe", "jre1.8.0_172/", "jre1.8.0_172/lib/rt.jar")
	result := Detect(dir)
	if result.Engine != Java {
		t.Errorf("expected Java from bundled JRE, got %s (%s)", result.Engine, result.MatchedBy)
	}
}

// TestDetectJavaJREDirNoJar verifies that a jre*/ directory without any
// .jar files is not a Java signal.
func TestDetectJavaJREDirNoJar(t *testing.T) {
	dir := makeDir(t, "jre/", "jre/bin/java.exe")
	result := Detect(dir)
	if result.Engine == Java {
		t.Errorf("expected NOT Java without .jar in jre dir, got %s (%s)", result.Engine, result.MatchedBy)
	}
}

func TestDetectGodot(t *testing.T) {
	dir := makeDir(t, "game.pck")
	result := Detect(dir)
	if result.Engine != Godot {
		t.Errorf("expected Godot, got %s (%s)", result.Engine, result.MatchedBy)
	}
}

func TestDetectElectronMapsToOthers(t *testing.T) {
	dir := makeDir(t, "resources.pak", "package.json")
	result := Detect(dir)
	if result.Engine != Others {
		t.Errorf("expected Others (Electron), got %s", result.Engine)
	}
	if !strings.Contains(result.MatchedBy, "nw.js") {
		t.Errorf("expected Electron/nw.js profile, got %s", result.MatchedBy)
	}
}

// package.json alone must not trigger the Electron/nw.js profile — Twine
// source repos and RPGM bundles ship one too. Only resources.pak +
// package.json together are a real nw.js signal.
func TestDetectPackageJSONAloneNotElectron(t *testing.T) {
	dir := makeDir(t, "package.json", "index.html")
	result := Detect(dir)
	if strings.Contains(result.MatchedBy, "nw.js") {
		t.Errorf("package.json alone matched Electron/nw.js: %s (%s)", result.Engine, result.MatchedBy)
	}
	if result.Engine != HTML {
		t.Errorf("expected HTML (root index.html), got %s (%s)", result.Engine, result.MatchedBy)
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

// ---------------------------------------------------------------------------
// Custom profile cache invalidation
// ---------------------------------------------------------------------------

const testProfileJSON = `{"name":"test-profile","engine":"Others","confidence":100,"filenames":["MOXIE_TEST_MARKER.bin"]}`
const testProfile2JSON = `{"name":"test-profile-2","engine":"Others","confidence":100,"filenames":["MOXIE_TEST_MARKER2.bin"]}`

// detectOnMarker returns Detect's MatchedBy for a directory containing the
// given marker file (and nothing else).
func detectOnMarker(t *testing.T, marker string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, marker), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	return Detect(dir).MatchedBy
}

// TestProfilesReloadOnDirChange verifies the merged-profiles cache is keyed
// on the engine profiles directory mtime: creating the directory and adding
// profile files invalidates the cache, so a long-running process picks up
// profile edits without a restart.
func TestProfilesReloadOnDirChange(t *testing.T) {
	root := t.TempDir()
	config.SetConfigDirForTest(root)
	t.Cleanup(func() { config.SetConfigDirForTest("") })

	// Before any profiles dir exists: only built-ins; the marker matches
	// nothing.
	if got := detectOnMarker(t, "MOXIE_TEST_MARKER.bin"); got != "no matching profile" {
		t.Fatalf("expected no custom profile before engines dir exists, got %q", got)
	}

	// Create the engines dir with one profile: dir went from missing to
	// existing, so the signature changes and the cache must reload.
	profilesDir := filepath.Join(root, "engines")
	if err := os.MkdirAll(profilesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "profile.json"), []byte(testProfileJSON), 0644); err != nil {
		t.Fatal(err)
	}
	if got := detectOnMarker(t, "MOXIE_TEST_MARKER.bin"); got != "test-profile" {
		t.Fatalf("expected custom profile after adding profile.json, got %q", got)
	}

	// Add a second profile to the now-existing dir. Force a distinct mtime
	// so the reload is observable even within the same fs timestamp tick.
	if err := os.WriteFile(filepath.Join(profilesDir, "profile2.json"), []byte(testProfile2JSON), 0644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(profilesDir, now, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if got := detectOnMarker(t, "MOXIE_TEST_MARKER2.bin"); got != "test-profile-2" {
		t.Fatalf("expected second custom profile after dir mtime change, got %q", got)
	}
	// The first profile is still active alongside the second.
	if got := detectOnMarker(t, "MOXIE_TEST_MARKER.bin"); got != "test-profile" {
		t.Fatalf("expected first custom profile to survive reload, got %q", got)
	}
}

// TestResetProfilesForTest verifies the test-only reset forces a reload even
// for an in-place content edit, which a directory mtime does not expose.
func TestResetProfilesForTest(t *testing.T) {
	root := t.TempDir()
	config.SetConfigDirForTest(root)
	t.Cleanup(func() { config.SetConfigDirForTest("") })

	profilesDir := filepath.Join(root, "engines")
	if err := os.MkdirAll(profilesDir, 0755); err != nil {
		t.Fatal(err)
	}
	profilePath := filepath.Join(profilesDir, "profile.json")
	if err := os.WriteFile(profilePath, []byte(testProfileJSON), 0644); err != nil {
		t.Fatal(err)
	}
	if got := detectOnMarker(t, "MOXIE_TEST_MARKER.bin"); got != "test-profile" {
		t.Fatalf("expected custom profile after initial load, got %q", got)
	}

	// Edit the profile in place (same dir mtime): the cache keeps serving
	// the old profile — the documented limitation of mtime invalidation.
	if err := os.WriteFile(profilePath, []byte(testProfile2JSON), 0644); err != nil {
		t.Fatal(err)
	}
	if got := detectOnMarker(t, "MOXIE_TEST_MARKER2.bin"); got == "test-profile-2" {
		t.Fatal("in-place edit must not invalidate the mtime-keyed cache")
	}

	// ResetProfilesForTest forces a reload regardless.
	ResetProfilesForTest()
	if got := detectOnMarker(t, "MOXIE_TEST_MARKER2.bin"); got != "test-profile-2" {
		t.Fatalf("expected reloaded profile after ResetProfilesForTest, got %q", got)
	}
}
