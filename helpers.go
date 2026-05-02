package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Pre-compiled regexps used by filesystemSafe.
var (
	multiSpaceRE = regexp.MustCompile(`\s{2,}`)
	multiDashRE  = regexp.MustCompile(`-{2,}`)
)

func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// formatDuration returns a human-readable duration string.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		if s == 0 {
			return fmt.Sprintf("%dm", m)
		}
		return fmt.Sprintf("%dm%ds", m, s)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%dm", h, m)
}

// isBlocked returns true if the error indicates we've been blocked/rate-limited.
func isBlocked(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "BlockedError") ||
		strings.Contains(msg, "blocked") ||
		strings.Contains(msg, "Cloudflare challenge")
}

func truncate(s string, maxLen int) string {
	// Strip newlines and truncate.
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func truncateVer(v string) string {
	if v == "" {
		return "(none)"
	}
	return v
}

func wrapText(s string, width int) string {
	var result strings.Builder
	words := strings.Fields(s)
	lineLen := 0
	for _, w := range words {
		if lineLen+len(w)+1 > width && lineLen > 0 {
			result.WriteByte('\n')
			lineLen = 0
		}
		if lineLen > 0 {
			result.WriteByte(' ')
			lineLen++
		}
		result.WriteString(w)
		lineLen += len(w)
	}
	return result.String()
}
