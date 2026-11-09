package main

import (
	"os"
	"testing"
)

// TestMain replaces the Windows update dialog with a no-op for the whole test
// binary. showFatalError runs a modal PowerShell message box that blocks until
// dismissed and cannot appear on a headless CI runner, so a test that drives
// an update error path would otherwise hang until the 10-minute test timeout.
func TestMain(m *testing.M) {
	showFatalError = func(string, string) {}
	os.Exit(m.Run())
}
