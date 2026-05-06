package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mili/moxie/internal/downloader"
)

// renderProgressBar renders a terminal progress bar.
func renderProgressBar(p downloader.Progress) {
	width := 40
	filled := int(p.Percent / 100.0 * float64(width))
	if filled > width {
		filled = width
	}

	barStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("99"))
	emptyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	bar := strings.Repeat("█", filled)
	empty := strings.Repeat("░", width-filled)

	speed := formatSpeed(p.SpeedBytesPerSec)
	percent := fmt.Sprintf("%5.1f%%", p.Percent)
	size := fmt.Sprintf("%s / %s", formatBytes(p.BytesDownloaded), formatBytes(p.TotalBytes))

	fmt.Fprintf(os.Stderr, "\r%s%s %s %s %s",
		barStyle.Render(bar),
		emptyStyle.Render(empty),
		lipgloss.NewStyle().Bold(true).Render(percent),
		speed,
		size,
	)
}

func formatSpeed(bytesPerSec float64) string {
	if bytesPerSec < 1024 {
		return fmt.Sprintf("%.0f B/s", bytesPerSec)
	}
	if bytesPerSec < 1024*1024 {
		return fmt.Sprintf("%.1f KB/s", bytesPerSec/1024)
	}
	return fmt.Sprintf("%.1f MB/s", bytesPerSec/(1024*1024))
}

func formatBytes(b int64) string {
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	}
	if b < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	}
	if b < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
	}
	return fmt.Sprintf("%.1f GB", float64(b)/(1024*1024*1024))
}

// findDownloadedFile attempts to find the downloaded file in the destination directory.
func findDownloadedFile(destDir, linkName string) string {
	path := filepath.Join(destDir, linkName)
	if _, err := os.Stat(path); err == nil {
		return path
	}

	if idx := strings.Index(linkName, "?"); idx > 0 {
		path = filepath.Join(destDir, linkName[:idx])
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return downloader.FindMostRecentFile(destDir)
}
