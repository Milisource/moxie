//go:build !windows

package commands

import "syscall"

// processAlive reports whether pid names a live process. Signal 0 performs
// the permission and existence checks without delivering anything.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
