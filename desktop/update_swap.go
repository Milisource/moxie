package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/mili/moxie/internal/config"
)

// pendingUpdateMarker is written by ApplyUpdate on Windows — where the
// running executable is locked and cannot be renamed or replaced — and
// consumed by the swap-agent process spawned from the staged binary.
type pendingUpdateMarker struct {
	Staged string   `json:"staged"`            // path of the downloaded binary
	Exe    string   `json:"exe"`               // path of the installed executable
	Args   []string `json:"args"`              // original command-line arguments to replay
	SHA256 string   `json:"sha256,omitempty"`  // expected SHA-256 of the staged binary (optional)
}

func pendingUpdateMarkerPath() string {
	return filepath.Join(config.ConfigDir(), "updates", "pending-update.json")
}

// stageWindowsUpdate writes the pending-update marker and spawns the staged
// binary as the swap agent. The agent waits for this process to exit — only
// then is the installed exe unlocked on Windows — swaps the binaries, and
// relaunches the installed copy. The agent is detached and survives this
// process's exit. On any setup failure the marker is removed and the update
// is left untouched.
func stageWindowsUpdate(tmpPath, exe string) error {
	marker := pendingUpdateMarker{Staged: tmpPath, Exe: exe, Args: os.Args[1:]}
	// Carry the expected digest so the swap agent can re-verify the staged
	// binary before touching the installed executable. Best-effort: if the
	// hash cannot be computed the marker is written without it and the agent
	// logs a warning and proceeds as before the field existed.
	if sum, err := sha256File(tmpPath); err != nil {
		slog.Warn("update: could not hash staged binary", "staged", tmpPath, "error", err)
	} else {
		marker.SHA256 = sum
	}
	data, err := json.Marshal(marker)
	if err != nil {
		return fmt.Errorf("encode pending update marker: %w", err)
	}
	markerPath := pendingUpdateMarkerPath()
	if err := os.WriteFile(markerPath, data, 0600); err != nil {
		return fmt.Errorf("write pending update marker: %w", err)
	}

	cmd := exec.Command(tmpPath)
	cmd.Env = append(os.Environ(),
		"MOXIE_UPDATE_SWAP=1",
		"MOXIE_UPDATE_MARKER="+markerPath,
		"MOXIE_UPDATE_PARENT="+strconv.Itoa(os.Getpid()),
	)
	if err := cmd.Start(); err != nil {
		os.Remove(markerPath)
		return fmt.Errorf("start update agent: %w", err)
	}
	return nil
}

// applyPendingUpdateIfRequested performs the Windows staged-update swap when
// this process was spawned as the swap agent (MOXIE_UPDATE_SWAP=1). It
// returns true when the process was an agent and must not start the app.
//
// Diagnostics go to update-agent.log under config.LogDir(): the agent runs
// before the app's logging is initialized, so the default slog logger would
// write to the stderr of a detached, console-less process — invisible when
// something goes wrong. Unrecoverable failures additionally surface a
// Windows message box.
func applyPendingUpdateIfRequested() bool {
	if os.Getenv("MOXIE_UPDATE_SWAP") != "1" {
		return false
	}
	agent := updateAgentLog()

	markerPath := os.Getenv("MOXIE_UPDATE_MARKER")
	if markerPath == "" {
		markerPath = pendingUpdateMarkerPath()
	}

	// Wait for the old process to exit: the installed exe stays locked on
	// Windows while it runs. If the parent is already gone (or unreadable),
	// Wait returns immediately and the swap proceeds.
	if pidStr := os.Getenv("MOXIE_UPDATE_PARENT"); pidStr != "" {
		if pid, err := strconv.Atoi(pidStr); err == nil && pid > 0 {
			if p, err := os.FindProcess(pid); err == nil {
				_, _ = p.Wait()
			}
		}
	}

	data, err := os.ReadFile(markerPath)
	if err != nil {
		agent.Error("update agent: cannot read marker", "path", markerPath, "error", err)
		showFatalError("Moxie update failed", "The update agent could not read its update marker. Please run the update again.")
		return true
	}
	var m pendingUpdateMarker
	if err := json.Unmarshal(data, &m); err != nil {
		agent.Error("update agent: malformed marker", "error", err)
		showFatalError("Moxie update failed", "The update marker was unreadable. Please run the update again.")
		return true
	}
	if m.Staged == "" || m.Exe == "" {
		agent.Error("update agent: marker missing paths")
		showFatalError("Moxie update failed", "The update marker was incomplete. Please run the update again.")
		return true
	}
	if _, err := os.Stat(m.Staged); err != nil {
		agent.Error("update agent: staged binary missing", "staged", m.Staged, "error", err)
		showFatalError("Moxie update failed", "The downloaded update file is missing. Please run the update again.")
		return true
	}

	// Re-verify the staged binary against the expected digest carried by the
	// marker (written by stageWindowsUpdate). Markers written before the hash
	// field existed are accepted with a warning.
	if m.SHA256 != "" {
		sum, err := sha256File(m.Staged)
		if err != nil {
			agent.Error("update agent: cannot hash staged binary", "staged", m.Staged, "error", err)
			showFatalError("Moxie update failed", "The downloaded update file could not be verified.")
			return true
		}
		if !strings.EqualFold(sum, m.SHA256) {
			agent.Error("update agent: staged binary failed integrity check",
				"staged", m.Staged, "expected", m.SHA256, "actual", sum)
			showFatalError("Moxie update failed",
				"The downloaded update failed its integrity check and was not installed.")
			return true
		}
		agent.Info("update agent: staged binary verified", "sha256", sum)
	} else {
		agent.Warn("update agent: marker carries no expected SHA-256; skipping verification")
	}

	if err := swapBinaries(m.Staged, m.Exe, nil); err != nil {
		agent.Error("update agent: swap failed", "error", err)
		showFatalError("Moxie update failed",
			"Installing the update failed: "+err.Error()+"\nThe previous version was restored.")
		return true
	}
	// The staged file is this process's own image; with the copy above
	// complete it can be dropped — otherwise every update leaves a full exe
	// copy behind in the updates directory.
	if err := os.Remove(m.Staged); err != nil {
		agent.Warn("update agent: could not remove staged binary", "staged", m.Staged, "error", err)
	}
	os.Remove(m.Exe + ".bak")
	os.Remove(markerPath)
	agent.Info("update applied by agent; relaunching installed binary")

	// Relaunch the installed binary in this process's place. The swap env
	// vars must NOT leak into it — a stale MOXIE_UPDATE_SWAP=1 would turn
	// the fresh process into another agent.
	if err := relaunchInstalled(m.Exe, m.Args, withoutUpdateEnv(os.Environ())); err != nil {
		agent.Error("update agent: relaunch failed", "error", err)
		showFatalError("Moxie update failed",
			"The update was installed but the app could not be relaunched: "+err.Error())
		return true
	}
	return true
}

// swapBinaries installs the staged binary over the installed executable with
// a crash-safe ordering:
//
//  1. copy staged -> exe.tmp   (same directory, fsynced)
//  2. rename exe -> exe.bak
//  3. rename exe.tmp -> exe
//
// A crash before step 2 leaves the installed executable untouched. A crash
// between steps 2 and 3 leaves the executable absent with the previous
// version in exe.bak — recoverStaleUpdate restores it on the next launch.
// Every step that fails rolls its own effects back, and a re-run self-heals
// an interrupted attempt (backup restored, stale temp dropped) before
// starting over.
func swapBinaries(staged, exe string, ops *swapOps) error {
	if ops == nil {
		ops = defaultSwapOps()
	}
	tmp := exe + ".tmp"
	backup := exe + ".bak"

	// Self-heal an interrupted earlier attempt: if the installed exe is
	// missing but the backup exists, the previous run died between the two
	// renames. Restore the backup first so the swap below has a known-good
	// binary to move aside.
	if !ops.statOK(exe) && ops.statOK(backup) {
		if err := ops.rename(backup, exe); err != nil {
			return fmt.Errorf("restore backup from interrupted swap: %w", err)
		}
	}
	// A stale temp from a crashed copy must not block the O_EXCL copy below
	// (it is a duplicate of staged, which still exists — safe to drop).
	_ = ops.remove(tmp)

	if err := ops.copyFile(staged, tmp); err != nil {
		return fmt.Errorf("copy staged binary: %w", err)
	}
	// Crash here: exe untouched; only a .tmp file to clean up.

	if ops.statOK(exe) {
		// Windows rename does not overwrite; a stale .bak from an older
		// failed attempt would block the move-aside.
		_ = ops.remove(backup)
		if err := ops.rename(exe, backup); err != nil {
			_ = ops.remove(tmp)
			return fmt.Errorf("move installed binary aside: %w", err)
		}
	}
	// Crash here: exe absent, exe.bak holds the previous version. The next
	// launch (or agent re-run) restores it via recoverStaleUpdate.

	if err := ops.rename(tmp, exe); err != nil {
		// Put the old binary back.
		_ = ops.rename(backup, exe)
		_ = ops.remove(tmp)
		return fmt.Errorf("install staged binary: %w", err)
	}

	// The new binary is in place — the backup is no longer needed for
	// recovery. Drop it so a successful swap leaves no artifacts behind.
	_ = ops.remove(backup)

	if ops.chmod != nil {
		_ = ops.chmod(exe, 0o755)
	}
	return nil
}

// swapOps abstracts the filesystem operations the swap sequence performs so
// tests can simulate failures at every step (rollback behavior, crash-window
// ordering) without touching real binaries.
type swapOps struct {
	stat     func(name string) (os.FileInfo, error)
	copyFile func(src, dst string) error
	rename   func(old, new string) error
	remove   func(name string) error
	chmod    func(name string, mode os.FileMode) error
}

func defaultSwapOps() *swapOps {
	return &swapOps{
		stat:     os.Stat,
		copyFile: copyFileSync,
		rename:   os.Rename,
		remove:   os.Remove,
		chmod:    os.Chmod,
	}
}

func (o *swapOps) statOK(name string) bool {
	_, err := o.stat(name)
	return err == nil
}

// copyFileSync copies src to dst and fsyncs dst before returning, so a crash
// immediately after the copy cannot leave a truncated binary installed. The
// destination is created with O_EXCL (a stale file from an earlier failed
// attempt must not be silently reused) and executable permissions.
func copyFileSync(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	return nil
}

// sha256File returns the lowercase hex SHA-256 of a file's contents.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// relaunchInstalled starts the installed binary in this process's place. It
// is a package variable so tests can stub the process boundary.
var relaunchInstalled = func(exe string, args, env []string) error {
	if abs, err := filepath.Abs(exe); err == nil {
		exe = abs
	}
	proc, err := os.StartProcess(exe, append([]string{exe}, args...), &os.ProcAttr{
		Env:   env,
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
	})
	if err != nil {
		return err
	}
	return proc.Release()
}

// recoverStaleUpdate repairs the aftermath of a swap agent that died between
// moving the installed executable aside and installing the new one: the
// installed exe is absent but the previous version survives as exe.bak next
// to a pending-update marker. Called from main() on every normal launch
// before the app starts; a no-op when no stale marker exists.
func recoverStaleUpdate() {
	markerPath := pendingUpdateMarkerPath()
	data, err := os.ReadFile(markerPath)
	if err != nil {
		return // no stale marker — nothing to recover
	}
	agent := updateAgentLog()

	var m pendingUpdateMarker
	if err := json.Unmarshal(data, &m); err != nil {
		agent.Warn("update: stale marker unreadable; removing", "path", markerPath, "error", err)
		os.Remove(markerPath)
		return
	}
	if m.Exe == "" {
		agent.Warn("update: stale marker without exe path; removing", "path", markerPath)
		os.Remove(markerPath)
		return
	}
	if _, err := os.Stat(m.Exe); err == nil {
		// The installed exe is present: either the swap never started or it
		// completed and only the marker cleanup was interrupted. Either way
		// the marker is stale — drop it and any leftover backup.
		agent.Info("update: stale marker with installed exe present; cleaning up", "exe", m.Exe)
		os.Remove(markerPath)
		os.Remove(m.Exe + ".bak")
		return
	}

	// The installed exe is absent — an interrupted swap. Restore the backup.
	backup := m.Exe + ".bak"
	if _, err := os.Stat(backup); err == nil {
		if rerr := os.Rename(backup, m.Exe); rerr != nil {
			agent.Error("update: cannot restore backup after interrupted swap",
				"backup", backup, "exe", m.Exe, "error", rerr)
			return
		}
		agent.Warn("update: restored executable from backup after interrupted swap", "exe", m.Exe)
		os.Remove(markerPath)
		if m.Staged != "" {
			os.Remove(m.Staged)
		}
		return
	}

	// Neither exe nor backup exists. Keep the marker and staged file for
	// manual recovery and say so loudly.
	agent.Error("update: interrupted swap left no executable and no backup",
		"exe", m.Exe, "backup", backup, "marker", markerPath)
}

var (
	updateAgentLogOnce sync.Once
	updateAgentLogger  *slog.Logger
)

// updateAgentLog returns a logger that writes to update-agent.log under
// config.LogDir(). The swap agent runs before the app's logging is
// initialized, so the default slog logger would write to the stderr of a
// detached, console-less process — invisible when something goes wrong. The
// file handler guarantees the failure lands somewhere the user (or support)
// can find it. Falls back to the default logger when the file cannot be
// opened.
func updateAgentLog() *slog.Logger {
	updateAgentLogOnce.Do(func() {
		dir := config.LogDir()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			updateAgentLogger = slog.Default()
			return
		}
		f, err := os.OpenFile(filepath.Join(dir, "update-agent.log"),
			os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			updateAgentLogger = slog.Default()
			return
		}
		updateAgentLogger = slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug}))
	})
	return updateAgentLogger
}

// showFatalError surfaces an unrecoverable update failure to the user. The
// agent runs before the Wails runtime exists (runtime.MessageDialog is not
// available), so on Windows a PowerShell message box is used; on every other
// platform the failure has already been written to update-agent.log.
func showFatalError(title, msg string) {
	if runtime.GOOS != "windows" {
		return
	}
	// Single-quote the arguments for PowerShell.
	title = strings.ReplaceAll(title, "'", "''")
	msg = strings.ReplaceAll(msg, "'", "''")
	ps := fmt.Sprintf("Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.MessageBox]::Show('%s','%s','OK','Error')", msg, title)
	if err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps).Run(); err != nil {
		updateAgentLog().Warn("could not show error dialog", "error", err)
	}
}

// withoutUpdateEnv drops the swap-agent env vars from a process environment.
func withoutUpdateEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "MOXIE_UPDATE_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
