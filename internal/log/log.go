// Package log provides structured logging via Go's standard slog package.
//
// Usage:
//
//	log.Info("batch started", "total", 100)
//	log.Warn("rate limited", "status", 429)
//	log.Error("request failed", "error", err)
//
// Production use: call Init(dir) once at startup to enable per-day log files
// alongside stderr. Without Init, output goes to stderr only.
//
//	func main() {
//	    log.Init(config.LogDir())
//	    // ... rest of app
//	}
package log

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// Logger is the package-level structured logger. By default writes to stderr
// at Info level. After Init(dir) it also writes to a daily log file.
var Logger = slog.New(slog.NewTextHandler(os.Stderr, nil))

// Init creates the log directory and redirects output to a per-day log file
// (moxie-YYYY-MM-DD.log). After Init, log messages go to the file only —
// stderr is intentionally excluded to avoid corrupting the TUI.
// Safe to call multiple times; the log file is reopened on each call.
func Init(dir string) {
	os.MkdirAll(dir, 0755)
	date := time.Now().Format("2006-01-02")
	logPath := filepath.Join(dir, "moxie-"+date+".log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		// Can't open log file — keep stderr-only, this is non-fatal.
		Warn("cannot open log file", "path", logPath, "error", err)
		return
	}
	Logger = slog.New(slog.NewTextHandler(f, nil))
}

// SetLevel adjusts the log level at runtime (stderr-only; overrides Init).
// Use slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, or slog.LevelError.
func SetLevel(level slog.Level) {
	Logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

// SetOutput redirects output to a single writer (useful for testing).
func SetOutput(w io.Writer) {
	Logger = slog.New(slog.NewTextHandler(w, nil))
}

// Debug logs at debug level with the given message and key-value pairs.
func Debug(msg string, args ...any) { Logger.Debug(msg, args...) }

// Info logs at info level with the given message and key-value pairs.
func Info(msg string, args ...any) { Logger.Info(msg, args...) }

// Warn logs at warn level with the given message and key-value pairs.
func Warn(msg string, args ...any) { Logger.Warn(msg, args...) }

// Error logs at error level with the given message and key-value pairs.
func Error(msg string, args ...any) { Logger.Error(msg, args...) }
