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
//
// Init also calls slog.SetDefault so stdlib slog.* calls (used throughout the
// desktop app) land in the same sink. The MOXIE_LOG_LEVEL environment
// variable (debug|info|warn|error) can raise or lower the level at startup;
// SetLevel does the same at runtime.
package log

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	logRetentionDays = 30
)

var (
	// Logger is the package-level structured logger. By default writes to
	// stderr at Info level. After Init(dir) it also writes to a daily log
	// file, and slog.Default is pointed at the same sink.
	Logger = slog.New(slog.NewTextHandler(os.Stderr, nil))

	// mu guards the fields below.
	mu sync.Mutex

	// fileWriter is the open daily log file, set by Init/InitWithConsole.
	fileWriter io.Writer

	// console mirrors InitWithConsole: write to stderr in addition to the file.
	console bool

	// level is the active log level. Init applies MOXIE_LOG_LEVEL if set.
	level slog.Level = slog.LevelInfo
)

// Init creates the log directory and redirects output to a per-day log file
// (moxie-YYYY-MM-DD.log). After Init, log messages go to the file only —
// stderr is intentionally excluded to avoid corrupting the TUI.
// Safe to call multiple times; the log file is reopened on each call.
func Init(dir string) {
	initLogger(dir, false)
}

// InitWithConsole creates the log directory and writes to BOTH a per-day log
// file AND stderr. Use this for desktop GUI apps where there's no TUI to
// corrupt. Safe to call multiple times; the log file is reopened on each call.
func InitWithConsole(dir string) {
	initLogger(dir, true)
}

func initLogger(dir string, alsoStderr bool) {
	invalidEnv := ""
	if raw := strings.TrimSpace(os.Getenv("MOXIE_LOG_LEVEL")); raw != "" {
		if lvl, ok := parseLevel(raw); ok {
			mu.Lock()
			level = lvl
			mu.Unlock()
		} else {
			invalidEnv = raw
		}
	}
	os.MkdirAll(dir, 0755)
	date := time.Now().Format("2006-01-02")
	logPath := filepath.Join(dir, "moxie-"+date+".log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		// Can't open log file — keep stderr-only, this is non-fatal.
		Warn("cannot open log file", "path", logPath, "error", err)
		return
	}
	mu.Lock()
	if old, ok := fileWriter.(io.Closer); ok {
		// Repeated Init calls (documented as safe) replace the file
		// writer — close the previous handle so it is not leaked.
		old.Close()
	}
	fileWriter = f
	console = alsoStderr
	mu.Unlock()
	// Clean up old log files after opening today's. Non-fatal if it fails.
	rotateOldLogs(dir)
	rebuildLogger()
	if invalidEnv != "" {
		Warn("ignoring invalid MOXIE_LOG_LEVEL", "value", invalidEnv)
	}
}

// parseLevel maps a MOXIE_LOG_LEVEL string to a slog level.
func parseLevel(raw string) (slog.Level, bool) {
	switch strings.ToLower(raw) {
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	}
	return slog.LevelInfo, false
}

// rebuildLogger recreates Logger from the current file/stderr composition and
// level, and points slog's package-level default at it so stdlib slog calls
// (used throughout the desktop app) land in the same sink.
func rebuildLogger() {
	mu.Lock()
	defer mu.Unlock()
	var w io.Writer = os.Stderr
	if fileWriter != nil {
		w = fileWriter
		if console {
			w = io.MultiWriter(fileWriter, os.Stderr)
		}
	}
	Logger = slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(Logger)
}

// rotateOldLogs removes log files older than logRetentionDays from the log directory.
// Only removes files matching the moxie-YYYY-MM-DD.log pattern.
func rotateOldLogs(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		Debug("cannot read log directory for rotation", "dir", dir, "error", err)
		return
	}
	cutoff := time.Now().AddDate(0, 0, -logRetentionDays)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		// Match moxie-YYYY-MM-DD.log pattern
		if len(entry.Name()) != len("moxie-2006-01-02.log") {
			continue
		}
		if !strings.HasPrefix(entry.Name(), "moxie-") || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		// Extract date portion: "moxie-" + "2006-01-02" + ".log"
		dateStr := entry.Name()[6 : 6+10] // "2006-01-02" is 10 chars
		logDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}
		if logDate.Before(cutoff) {
			fullPath := filepath.Join(dir, entry.Name())
			if err := os.Remove(fullPath); err != nil {
				Debug("cannot remove old log file", "path", fullPath, "error", err)
			}
		}
	}
}

// SetLevel adjusts the log level at runtime. The file/stderr composition
// established by Init/InitWithConsole is preserved — without Init, output
// stays on stderr. Use slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, or
// slog.LevelError.
func SetLevel(l slog.Level) {
	mu.Lock()
	level = l
	mu.Unlock()
	rebuildLogger()
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
