package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mili/moxie/internal/config"
)

// The relaunched process must never inherit the swap-agent env vars — a
// stale MOXIE_UPDATE_SWAP=1 would turn the fresh instance into an agent.
func TestWithoutUpdateEnv(t *testing.T) {
	env := []string{
		"HOME=/home/user",
		"MOXIE_UPDATE_SWAP=1",
		"PATH=/usr/bin",
		"MOXIE_UPDATE_MARKER=/x/pending-update.json",
		"MOXIE_UPDATE_PARENT=1234",
	}
	got := withoutUpdateEnv(env)
	want := []string{"HOME=/home/user", "PATH=/usr/bin"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("withoutUpdateEnv = %v, want %v", got, want)
	}
}

func TestWithoutUpdateEnvKeepsUnrelatedMoxieVars(t *testing.T) {
	env := []string{"MOXIE_FOO=bar", "MOXIE_UPDATE_SWAP=1"}
	got := withoutUpdateEnv(env)
	if len(got) != 1 || got[0] != "MOXIE_FOO=bar" {
		t.Errorf("withoutUpdateEnv = %v, want only MOXIE_FOO kept", got)
	}
}

// The marker must round-trip the staged path, exe path, and the original
// command-line arguments so the swap agent can replay them on relaunch. The
// SHA-256 field is optional and must survive a round trip when present.
func TestPendingUpdateMarkerRoundTrip(t *testing.T) {
	m := pendingUpdateMarker{
		Staged: `C:\Users\mili\AppData\Roaming\moxie\updates\moxie-desktop-windows-amd64.exe`,
		Exe:    `C:\Program Files\Moxie\moxie-desktop-windows-amd64.exe`,
		Args:   []string{"--flag", "value with spaces"},
		SHA256: "aabbcc",
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back pendingUpdateMarker
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Staged != m.Staged || back.Exe != m.Exe || !reflect.DeepEqual(back.Args, m.Args) || back.SHA256 != m.SHA256 {
		t.Errorf("round trip = %+v, want %+v", back, m)
	}
}

// A marker written without the hash field (older format) must still unmarshal
// with an empty SHA256 — the field is backward compatible.
func TestPendingUpdateMarkerWithoutHash(t *testing.T) {
	var m pendingUpdateMarker
	if err := json.Unmarshal([]byte(`{"staged":"/s","exe":"/e","args":[]}`), &m); err != nil {
		t.Fatalf("unmarshal legacy marker: %v", err)
	}
	if m.SHA256 != "" {
		t.Errorf("SHA256 = %q, want empty for legacy markers", m.SHA256)
	}
}

// fakeFileInfo is a minimal os.FileInfo for fakeSwapFS.
type fakeFileInfo struct{ name string }

func (fakeFileInfo) Name() string       { return "x" }
func (fakeFileInfo) Size() int64        { return 0 }
func (fakeFileInfo) Mode() os.FileMode  { return 0o644 }
func (fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (fakeFileInfo) IsDir() bool        { return false }
func (fakeFileInfo) Sys() interface{}   { return nil }

// fakeSwapFS simulates the filesystem the swap sequence operates on: it
// tracks which files exist, records every operation in order, and can fail a
// chosen call (1-based per operation kind) to exercise rollback and
// crash-window behavior without touching real binaries.
type fakeSwapFS struct {
	mu     sync.Mutex
	log    []string
	have   map[string]bool
	failOn map[string]int
	calls  map[string]int
}

func newFakeSwapFS(files ...string) *fakeSwapFS {
	fs := &fakeSwapFS{
		have:   make(map[string]bool),
		failOn: make(map[string]int),
		calls:  make(map[string]int),
	}
	for _, f := range files {
		fs.have[f] = true
	}
	return fs
}

// fail sets the N-th call of op (1-based) to return an error.
func (f *fakeSwapFS) fail(op string, n int) {
	f.failOn[op] = n
}

func (f *fakeSwapFS) maybeFail(op string) error {
	f.calls[op]++
	if f.failOn[op] == f.calls[op] {
		return fmt.Errorf("simulated %s failure", op)
	}
	return nil
}

func (f *fakeSwapFS) exists(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.have[name]
}

// ops returns a swapOps wired to the fake state.
func (f *fakeSwapFS) ops() *swapOps {
	return &swapOps{
		stat: func(name string) (os.FileInfo, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.log = append(f.log, "stat "+name)
			if !f.have[name] {
				return nil, os.ErrNotExist
			}
			return fakeFileInfo{}, nil
		},
		copyFile: func(src, dst string) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.log = append(f.log, "copy "+src+" -> "+dst)
			if err := f.maybeFail("copy"); err != nil {
				return err
			}
			f.have[dst] = true
			return nil
		},
		rename: func(old, new string) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.log = append(f.log, "rename "+old+" -> "+new)
			if err := f.maybeFail("rename"); err != nil {
				return err
			}
			f.have[new] = f.have[old]
			delete(f.have, old)
			return nil
		},
		remove: func(name string) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.log = append(f.log, "remove "+name)
			if err := f.maybeFail("remove"); err != nil {
				return err
			}
			delete(f.have, name)
			return nil
		},
		chmod: func(name string, mode os.FileMode) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.log = append(f.log, "chmod "+name)
			return nil
		},
	}
}

func (f *fakeSwapFS) opsLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.log))
	copy(out, f.log)
	return out
}

// indexOf returns the index of the first log entry with the given prefix,
// or -1.
func indexOf(log []string, prefix string) int {
	for i, e := range log {
		if strings.HasPrefix(e, prefix) {
			return i
		}
	}
	return -1
}

// The whole point of the reordering: the staged binary must be fully copied
// into the same-directory temp file BEFORE the installed executable is moved
// aside. The old order (rename exe -> .bak first) left a window where a
// crash stranded the user with no installed exe and no restore path.
func TestSwapBinariesCrashWindowOrdering(t *testing.T) {
	fs := newFakeSwapFS("staged", "exe")
	ops := fs.ops()

	if err := swapBinaries("staged", "exe", ops); err != nil {
		t.Fatalf("swapBinaries: %v", err)
	}
	log := fs.opsLog()

	copyIdx := indexOf(log, "copy staged -> exe.tmp")
	moveAside := indexOf(log, "rename exe -> exe.bak")
	install := indexOf(log, "rename exe.tmp -> exe")
	if copyIdx < 0 || moveAside < 0 || install < 0 {
		t.Fatalf("missing expected operations in %v", log)
	}
	if !(copyIdx < moveAside && moveAside < install) {
		t.Errorf("wrong order: copy(%d) must precede move-aside(%d) must precede install(%d)", copyIdx, moveAside, install)
	}
	// Nothing may touch the installed executable or its backup before the
	// staged copy is complete: a crash before the copy must leave the
	// previous version fully in place. (The stat and the stale-temp cleanup
	// that precede the copy are read-only / leave the exe untouched.)
	for _, e := range log[:copyIdx] {
		if strings.HasPrefix(e, "rename exe") || strings.HasPrefix(e, "remove exe.bak") || strings.HasPrefix(e, "remove exe ") {
			t.Errorf("operation %q precedes the staged copy — the installed exe must stay untouched until the copy is done", e)
		}
	}
	if !fs.exists("exe") {
		t.Error("exe missing after successful swap")
	}
	if fs.exists("exe.bak") || fs.exists("exe.tmp") {
		t.Error("no artifacts should remain after a successful swap")
	}
}

// If installing the copied temp over the exe fails, the move-aside must be
// rolled back: the .bak is renamed back over the exe.
func TestSwapBinariesRollsBackMoveAsideWhenInstallFails(t *testing.T) {
	fs := newFakeSwapFS("staged", "exe")
	fs.fail("rename", 2) // second rename: exe.tmp -> exe
	ops := fs.ops()

	err := swapBinaries("staged", "exe", ops)
	if err == nil {
		t.Fatal("swapBinaries succeeded, want simulated install failure")
	}
	log := fs.opsLog()
	if idx := indexOf(log, "rename exe.bak -> exe"); idx < 0 {
		t.Fatalf("no rollback rename of the backup in %v", log)
	}
	if !fs.exists("exe") {
		t.Error("exe must be restored after failed install")
	}
	if fs.exists("exe.bak") {
		t.Error("backup must be consumed by the rollback")
	}
	if fs.exists("exe.tmp") {
		t.Error("temp file must be removed after failed install")
	}
}

// If moving the installed exe aside fails, nothing may be left half-done:
// the temp copy is dropped and the exe stays in place.
func TestSwapBinariesRollsBackCopyWhenMoveAsideFails(t *testing.T) {
	fs := newFakeSwapFS("staged", "exe")
	fs.fail("rename", 1) // first rename: exe -> exe.bak
	ops := fs.ops()

	err := swapBinaries("staged", "exe", ops)
	if err == nil {
		t.Fatal("swapBinaries succeeded, want simulated move-aside failure")
	}
	if !fs.exists("exe") {
		t.Error("exe must be untouched when the move-aside fails")
	}
	if fs.exists("exe.bak") {
		t.Error("no backup may appear when the move-aside failed")
	}
	if fs.exists("exe.tmp") {
		t.Error("temp copy must be removed when the move-aside fails")
	}
}

// A crash between the two renames leaves exe absent and exe.bak present. A
// re-run must heal itself: restore the backup first, then run the normal
// sequence.
func TestSwapBinariesSelfHealsInterruptedSwap(t *testing.T) {
	fs := newFakeSwapFS("staged", "exe.bak") // exe missing — crashed between renames
	ops := fs.ops()

	if err := swapBinaries("staged", "exe", ops); err != nil {
		t.Fatalf("swapBinaries: %v", err)
	}
	log := fs.opsLog()
	if idx := indexOf(log, "rename exe.bak -> exe"); idx < 0 {
		t.Fatalf("self-heal must restore the backup first, got %v", log)
	}
	if !fs.exists("exe") {
		t.Error("exe missing after self-healing re-run")
	}
}

// When the exe is missing and there is no backup to heal from, the swap
// still completes by installing the staged binary directly (nothing to move
// aside).
func TestSwapBinariesInstallsDirectlyWhenExeMissingAndNoBackup(t *testing.T) {
	fs := newFakeSwapFS("staged")
	ops := fs.ops()

	if err := swapBinaries("staged", "exe", ops); err != nil {
		t.Fatalf("swapBinaries: %v", err)
	}
	log := fs.opsLog()
	if idx := indexOf(log, "rename exe -> exe.bak"); idx >= 0 {
		t.Fatalf("must not move aside a missing exe: %v", log)
	}
	if !fs.exists("exe") {
		t.Error("exe must be installed even without a previous version")
	}
}

// A failed copy must leave the installed exe completely untouched.
func TestSwapBinariesCopyFailureLeavesExeUntouched(t *testing.T) {
	fs := newFakeSwapFS("staged", "exe")
	fs.fail("copy", 1)
	ops := fs.ops()

	if err := swapBinaries("staged", "exe", ops); err == nil {
		t.Fatal("swapBinaries succeeded, want simulated copy failure")
	}
	if !fs.exists("exe") {
		t.Error("exe must survive a copy failure")
	}
	if fs.exists("exe.bak") {
		t.Error("no backup may be created when the copy fails")
	}
}

// --- recoverStaleUpdate: real files on disk ---------------------------------

func testSwapConfigDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "config")
	configDir := filepath.Join(dir, "moxie")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	setTestConfigDir(t, configDir)
	return configDir
}

func setTestConfigDir(t *testing.T, dir string) {
	t.Helper()
	config.SetConfigDirForTest(dir)
	t.Cleanup(func() { config.SetConfigDirForTest("") })
}

// A stale marker with the installed exe missing must trigger a restore of
// the .bak backup so the next launch works.
func TestRecoverStaleUpdateRestoresBackup(t *testing.T) {
	configDir := testSwapConfigDir(t)
	exe := filepath.Join(configDir, "moxie-desktop.exe")
	if err := os.WriteFile(exe+".bak", []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(configDir, "updates", "staged.exe")
	if err := os.MkdirAll(filepath.Dir(staged), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestMarker(t, pendingUpdateMarker{Staged: staged, Exe: exe})

	recoverStaleUpdate()

	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatalf("exe not restored: %v", err)
	}
	if string(data) != "old-binary" {
		t.Errorf("exe content = %q, want the backup content", data)
	}
	if _, err := os.Stat(exe + ".bak"); !os.IsNotExist(err) {
		t.Error("backup should be consumed by the restore")
	}
	if _, err := os.Stat(pendingUpdateMarkerPath()); !os.IsNotExist(err) {
		t.Error("marker should be removed after a successful restore")
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Error("staged file should be removed after restore")
	}
}

// A stale marker with the exe present (swap finished, only cleanup was
// interrupted) must just tidy up: keep the exe, drop marker and backup.
func TestRecoverStaleUpdateExePresentCleansUp(t *testing.T) {
	configDir := testSwapConfigDir(t)
	exe := filepath.Join(configDir, "moxie-desktop.exe")
	if err := os.WriteFile(exe, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe+".bak", []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestMarker(t, pendingUpdateMarker{Staged: filepath.Join(configDir, "updates", "staged.exe"), Exe: exe})

	recoverStaleUpdate()

	data, _ := os.ReadFile(exe)
	if string(data) != "new-binary" {
		t.Errorf("exe content = %q, want the installed binary untouched", data)
	}
	if _, err := os.Stat(pendingUpdateMarkerPath()); !os.IsNotExist(err) {
		t.Error("marker should be removed")
	}
	if _, err := os.Stat(exe + ".bak"); !os.IsNotExist(err) {
		t.Error("backup should be removed")
	}
}

// No marker: recovery must be a complete no-op (the normal healthy case).
func TestRecoverStaleUpdateNoMarkerNoop(t *testing.T) {
	testSwapConfigDir(t)
	recoverStaleUpdate() // must not panic, must not touch anything
}

// A malformed stale marker must be removed so it cannot block future
// update attempts.
func TestRecoverStaleUpdateRemovesMalformedMarker(t *testing.T) {
	testSwapConfigDir(t)
	if err := os.MkdirAll(filepath.Dir(pendingUpdateMarkerPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pendingUpdateMarkerPath(), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	recoverStaleUpdate()
	if _, err := os.Stat(pendingUpdateMarkerPath()); !os.IsNotExist(err) {
		t.Error("malformed marker should be removed")
	}
}

func writeTestMarker(t *testing.T, m pendingUpdateMarker) {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(pendingUpdateMarkerPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pendingUpdateMarkerPath(), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// --- applyPendingUpdateIfRequested: the agent end to end --------------------

// stubRelaunch replaces the relaunch boundary for the duration of a test and
// returns a function that reports the recorded relaunch attempt.
func stubRelaunch(t *testing.T) (*string, *[]string) {
	t.Helper()
	var gotExe string
	var gotArgs []string
	orig := relaunchInstalled
	relaunchInstalled = func(exe string, args, env []string) error {
		gotExe = exe
		gotArgs = args
		return nil
	}
	t.Cleanup(func() { relaunchInstalled = orig })
	return &gotExe, &gotArgs
}

func TestApplyPendingUpdateAgentSwapsBinaries(t *testing.T) {
	configDir := testSwapConfigDir(t)
	exe := filepath.Join(configDir, "moxie-desktop.exe")
	if err := os.WriteFile(exe, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(configDir, "updates", "moxie-desktop-windows-amd64.exe")
	if err := os.MkdirAll(filepath.Dir(staged), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestMarker(t, pendingUpdateMarker{Staged: staged, Exe: exe, Args: []string{"--flag"}})

	t.Setenv("MOXIE_UPDATE_SWAP", "1")
	t.Setenv("MOXIE_UPDATE_MARKER", "") // fall back to the default marker path
	t.Setenv("MOXIE_UPDATE_PARENT", "") // skip the parent wait

	gotExe, gotArgs := stubRelaunch(t)

	if !applyPendingUpdateIfRequested() {
		t.Fatal("agent process must return true (do not start the app)")
	}

	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatalf("installed exe missing after swap: %v", err)
	}
	if string(data) != "new-binary" {
		t.Errorf("exe content = %q, want the staged binary", data)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Error("staged binary must be removed after a successful swap")
	}
	if _, err := os.Stat(exe + ".bak"); !os.IsNotExist(err) {
		t.Error("backup must be removed after a successful swap")
	}
	if _, err := os.Stat(pendingUpdateMarkerPath()); !os.IsNotExist(err) {
		t.Error("marker must be removed after a successful swap")
	}
	if *gotExe != exe {
		t.Errorf("relaunch exe = %q, want %q", *gotExe, exe)
	}
	if !reflect.DeepEqual(*gotArgs, []string{"--flag"}) {
		t.Errorf("relaunch args = %v, want [--flag]", *gotArgs)
	}
}

// A marker carrying a matching SHA-256 must verify the staged binary before
// swapping; a mismatch must abort without touching the installed exe.
func TestApplyPendingUpdateHashVerification(t *testing.T) {
	configDir := testSwapConfigDir(t)
	exe := filepath.Join(configDir, "moxie-desktop.exe")
	if err := os.WriteFile(exe, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(configDir, "updates", "staged.exe")
	if err := os.MkdirAll(filepath.Dir(staged), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MOXIE_UPDATE_SWAP", "1")
	t.Setenv("MOXIE_UPDATE_PARENT", "")
	stubRelaunch(t)

	t.Run("mismatch aborts and leaves exe untouched", func(t *testing.T) {
		writeTestMarker(t, pendingUpdateMarker{
			Staged: staged, Exe: exe, SHA256: "deadbeef",
		})
		t.Setenv("MOXIE_UPDATE_MARKER", pendingUpdateMarkerPath())

		if !applyPendingUpdateIfRequested() {
			t.Fatal("agent process must return true even on verification failure")
		}
		data, _ := os.ReadFile(exe)
		if string(data) != "old-binary" {
			t.Errorf("exe content = %q, want untouched old binary on hash mismatch", data)
		}
		if _, err := os.Stat(staged); err != nil {
			t.Error("staged binary must remain for a retry after hash mismatch")
		}
		if _, err := os.Stat(pendingUpdateMarkerPath()); err != nil {
			t.Error("marker must remain after hash mismatch")
		}
	})

	t.Run("matching hash proceeds with the swap", func(t *testing.T) {
		sum, err := sha256File(staged)
		if err != nil {
			t.Fatal(err)
		}
		writeTestMarker(t, pendingUpdateMarker{
			Staged: staged, Exe: exe, SHA256: sum,
		})

		if !applyPendingUpdateIfRequested() {
			t.Fatal("agent process must return true")
		}
		data, _ := os.ReadFile(exe)
		if string(data) != "new-binary" {
			t.Errorf("exe content = %q, want staged binary installed", data)
		}
	})
}

// Markers written before the hash field existed (empty SHA256) must be
// accepted: the agent warns and proceeds exactly as before.
func TestApplyPendingUpdateWithoutHashProceeds(t *testing.T) {
	configDir := testSwapConfigDir(t)
	exe := filepath.Join(configDir, "moxie-desktop.exe")
	if err := os.WriteFile(exe, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(configDir, "updates", "staged.exe")
	if err := os.MkdirAll(filepath.Dir(staged), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestMarker(t, pendingUpdateMarker{Staged: staged, Exe: exe})

	t.Setenv("MOXIE_UPDATE_SWAP", "1")
	t.Setenv("MOXIE_UPDATE_MARKER", pendingUpdateMarkerPath())
	t.Setenv("MOXIE_UPDATE_PARENT", "")
	stubRelaunch(t)

	if !applyPendingUpdateIfRequested() {
		t.Fatal("agent process must return true")
	}
	data, _ := os.ReadFile(exe)
	if string(data) != "new-binary" {
		t.Errorf("exe content = %q, want staged binary installed", data)
	}
}

// A missing staged binary must abort the agent before anything is touched.
func TestApplyPendingUpdateMissingStagedAborts(t *testing.T) {
	configDir := testSwapConfigDir(t)
	exe := filepath.Join(configDir, "moxie-desktop.exe")
	if err := os.WriteFile(exe, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestMarker(t, pendingUpdateMarker{
		Staged: filepath.Join(configDir, "updates", "does-not-exist.exe"),
		Exe:    exe,
	})

	t.Setenv("MOXIE_UPDATE_SWAP", "1")
	t.Setenv("MOXIE_UPDATE_MARKER", pendingUpdateMarkerPath())
	t.Setenv("MOXIE_UPDATE_PARENT", "")
	stubRelaunch(t)

	if !applyPendingUpdateIfRequested() {
		t.Fatal("agent process must return true")
	}
	data, _ := os.ReadFile(exe)
	if string(data) != "old-binary" {
		t.Errorf("exe content = %q, want untouched", data)
	}
	if _, err := os.Stat(pendingUpdateMarkerPath()); err != nil {
		t.Error("marker must remain so the update can be retried")
	}
}
