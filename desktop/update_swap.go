package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mili/moxie/internal/config"
)

// pendingUpdateMarker is written by ApplyUpdate on Windows — where the
// running executable is locked and cannot be renamed or replaced — and
// consumed by the swap-agent process spawned from the staged binary.
type pendingUpdateMarker struct {
	Staged string   `json:"staged"` // path of the downloaded binary
	Exe    string   `json:"exe"`    // path of the installed executable
	Args   []string `json:"args"`   // original command-line arguments to replay
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
func applyPendingUpdateIfRequested() bool {
	if os.Getenv("MOXIE_UPDATE_SWAP") != "1" {
		return false
	}

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
		slog.Error("update agent: cannot read marker", "error", err)
		return true
	}
	var m pendingUpdateMarker
	if err := json.Unmarshal(data, &m); err != nil {
		slog.Error("update agent: malformed marker", "error", err)
		return true
	}
	if m.Staged == "" || m.Exe == "" {
		slog.Error("update agent: marker missing paths")
		return true
	}
	if _, err := os.Stat(m.Staged); err != nil {
		slog.Error("update agent: staged binary missing", "staged", m.Staged, "error", err)
		return true
	}

	backupPath := m.Exe + ".bak"
	os.Remove(backupPath) // stale backup from an earlier failed attempt
	if err := os.Rename(m.Exe, backupPath); err != nil {
		slog.Error("update agent: cannot move installed binary aside", "error", err)
		return true
	}
	// Copy, never rename: this process IS running from the staged file, and
	// a running image cannot be renamed on Windows (copy reads it fine).
	if err := copyFile(m.Staged, m.Exe); err != nil {
		// Put the old binary back.
		_ = os.Rename(backupPath, m.Exe)
		slog.Error("update agent: cannot install staged binary", "error", err)
		return true
	}
	_ = os.Chmod(m.Exe, 0755)
	os.Remove(backupPath)
	os.Remove(markerPath)
	slog.Info("update applied by agent; relaunching installed binary")

	// Relaunch the installed binary in this process's place. The swap env
	// vars must NOT leak into it — a stale MOXIE_UPDATE_SWAP=1 would turn
	// the fresh process into another agent.
	exe := m.Exe
	if abs, err := filepath.Abs(exe); err == nil {
		exe = abs
	}
	proc, err := os.StartProcess(exe, append([]string{exe}, m.Args...), &os.ProcAttr{
		Env:   withoutUpdateEnv(os.Environ()),
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
	})
	if err != nil {
		slog.Error("update agent: relaunch failed", "error", err)
		return true
	}
	_ = proc.Release()
	return true
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
