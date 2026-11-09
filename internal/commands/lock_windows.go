//go:build windows

package commands

// processAlive is conservative on Windows: os.FindProcess does not prove
// liveness, so a lock is only broken by the age window.
func processAlive(pid int) bool { return true }
