//go:build windows

package f95session

import (
	"os/exec"
	"strconv"
	"syscall"
)

// setProcessGroup hides the console window; tree teardown uses taskkill.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

// killTree terminates the browser and its child processes.
func killTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
	_, _ = cmd.Process.Wait()
}
