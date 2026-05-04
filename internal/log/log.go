// Package log provides structured logging via Go's standard slog package.
//
// Usage:
//
//	log.Info("batch started", "total", 100)
//	log.Warn("rate limited", "status", 429)
//	log.Error("request failed", "error", err)
//
// By default, output goes to stderr as newline-delimited key=value pairs.
// Debug-level messages are suppressed unless SetLevel(slog.LevelDebug) is called.
package log

import (
	"io"
	"log/slog"
	"os"
)

// Logger is the package-level structured logger writing to stderr.
// The default level is Info, so Debug messages are suppressed unless
// SetLevel(slog.LevelDebug) is called.
var Logger = slog.New(slog.NewTextHandler(os.Stderr, nil))

// SetLevel adjusts the log level at runtime.
// Use slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, or slog.LevelError.
func SetLevel(level slog.Level) {
	Logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

// SetOutput redirects output to a different writer (useful for testing).
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
