//go:build !windows

package f95session

import (
	"os/exec"
	"syscall"
)

// setProcessGroup puts the browser in its own process group so killTree can
// terminate its content processes too.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killTree kills the browser's process group and reaps the main process.
func killTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_, _ = cmd.Process.Wait()
}
