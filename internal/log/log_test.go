package log

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSlogDefaultWiredToFile(t *testing.T) {
	dir := t.TempDir()
	InitWithConsole(dir)
	slog.Info("desktop-style log", "key", "value")

	logPath := filepath.Join(dir, "moxie-"+timeNow()+".log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("log file missing: %v", err)
	}
	if !strings.Contains(string(data), "desktop-style log") {
		t.Fatalf("slog default not routed to file; got:\n%s", data)
	}
}

func TestMOXIELogLevelEnv(t *testing.T) {
	t.Setenv("MOXIE_LOG_LEVEL", "debug")
	dir := t.TempDir()
	InitWithConsole(dir)
	Debug("debug-line")
	slog.Debug("debug-line-slog")

	logPath := filepath.Join(dir, "moxie-"+timeNow()+".log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("log file missing: %v", err)
	}
	if !strings.Contains(string(data), "debug-line") || !strings.Contains(string(data), "debug-line-slog") {
		t.Fatalf("debug lines missing at debug level; got:\n%s", data)
	}
}

func TestSetLevelKeepsFileOutput(t *testing.T) {
	dir := t.TempDir()
	Init(dir)
	SetLevel(slog.LevelDebug)
	Debug("debug-after-setlevel")

	logPath := filepath.Join(dir, "moxie-"+timeNow()+".log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("log file missing: %v", err)
	}
	if !strings.Contains(string(data), "debug-after-setlevel") {
		t.Fatalf("SetLevel lost file output; got:\n%s", data)
	}
}

func timeNow() string {
	return time.Now().Format("2006-01-02")
}
