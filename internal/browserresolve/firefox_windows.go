//go:build windows

package browserresolve

import (
	"os/exec"
	"strconv"
)

// setProcessGroup is a no-op on Windows — tree teardown uses taskkill.
func setProcessGroup(_ *exec.Cmd) {}

// killTree terminates the browser and its child processes via taskkill.
func killTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
}
