package util

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mili/moxie/internal/scraper"
)

// Pre-compiled regexps used by FilesystemSafe.
var (
	MultiSpaceRE = regexp.MustCompile(`\s{2,}`)
	MultiDashRE  = regexp.MustCompile(`-{2,}`)
)

// FormatSize returns a human-readable size string (e.g. "1.5 KB").
func FormatSize(bytes int64) string {
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

// FormatDuration returns a human-readable duration string.
func FormatDuration(d time.Duration) string {
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

// IsBlocked returns true if the error indicates we've been blocked/rate-limited.
func IsBlocked(err error) bool {
	if err == nil {
		return false
	}
	// Typed match first: the scraper surfaces blocks as *scraper.BlockedError
	// (its Error() format is "scraper: blocked — <reason> (HTTP <n>)", so the
	// old string match on "BlockedError" never hit). String checks remain as a
	// fallback for errors from other sources that describe a block.
	var blockedErr *scraper.BlockedError
	if errors.As(err, &blockedErr) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "blocked") ||
		strings.Contains(msg, "Cloudflare challenge")
}

// Truncate truncates a string to maxLen, appending "..." if shortened.
func Truncate(s string, maxLen int) string {
	// Strip newlines and truncate.
	s = strings.ReplaceAll(s, "\n", " ")
	if maxLen <= 0 {
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// TruncateVer returns a display string for a version value.
func TruncateVer(v string) string {
	if v == "" {
		return "(none)"
	}
	return v
}

// MustParseInt parses a string as int64, printing an error and exiting on failure.
func MustParseInt(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid ID: %s\n", s)
		os.Exit(1)
	}
	return n
}

// WrapText wraps text to a given width.
func WrapText(s string, width int) string {
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
