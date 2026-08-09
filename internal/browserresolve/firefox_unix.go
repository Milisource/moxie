//go:build !windows

package browserresolve

import (
	"os/exec"
	"syscall"
)

// setProcessGroup puts the launched browser into its own process group so
// killTree can terminate the whole tree (Firefox spawns content processes).
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killTree kills the browser's process group, then reaps the main process.
func killTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_, _ = cmd.Process.Wait()
}
