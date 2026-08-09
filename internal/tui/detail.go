package tui

import (
	"fmt"
	"path/filepath"

	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/downloader"
)

// availableStatuses lists all valid game statuses for the TUI status selector.
var availableStatuses = []string{"active", "completed", "abandoned", "on_hold", "unknown"}

// statusKeyHints returns a string showing the key bindings for each status.
// Example: "[a] active  [c] completed  [b] abandoned  [h] on_hold  [u] unknown"
func statusKeyHints(current string) string {
	var parts []string
	for _, s := range availableStatuses {
		key := string(s[0])
		// For "on_hold" use "h" instead of "o"
		if s == "on_hold" {
			key = "h"
		}
		styled := fmt.Sprintf("[%s] %s", key, s)
		if s == current {
			styled = accentStyle.Render(styled)
		}
		parts = append(parts, styled)
	}
	return strings.Join(parts, "  ")
}

// statusKeyForRune maps a rune keypress to the corresponding status string.
func statusKeyForRune(r rune) string {
	switch r {
	case 'a':
		return "active"
	case 'c':
		return "completed"
	case 'b':
		return "abandoned"
	case 'h':
		return "on_hold"
	case 'u':
		return "unknown"
	}
	return ""
}

func (m model) loadingView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("◂  Back to Library  "))
	b.WriteString("\n")
	b.WriteString(separatorStyle.Render(strings.Repeat("━", max(0, m.width-4))))
	b.WriteString("\n\n")
	b.WriteString(valueStyle.Render("  Loading…"))
	b.WriteString("\n")
	return appStyle.Render(b.String())
}

func (m model) detailView() string {
	if m.detailGame == nil {
		return m.loadingView()
	}

	// Build the full scrollable content and set it on the viewport.
	content := m.buildDetailContent()
	m.detailViewport.SetContent(content)

	return appStyle.Render(m.detailViewport.View())
}

// buildDetailContent renders all game detail content (header, info,
// download section, overlays) into a single string for the viewport.
func (m model) buildDetailContent() string {
	game := m.detailGame

	var b strings.Builder
	w := m.width
	if w <= 0 {
		w = 80
	}

	// ── Header ────────────────────────────────────────────────────
	back := titleStyle.Render("◂  Back to Library  ")
	hint := subtleStyle.Render("[Esc] back  [s] status  [e] edit  [x] exe  [Ctrl+U] url  [p] play  [g] dl  [d] delete  [?] help")
	gap := max(0, w-lipgloss.Width(back)-lipgloss.Width(hint)-4)
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, back, strings.Repeat(" ", gap), hint))
	b.WriteString("\n")
	sep := max(0, w-4)
	b.WriteString(separatorStyle.Render(strings.Repeat("━", sep)))
	b.WriteString("\n\n")

	// ── Section: Core Info ─────────────────────────────────────────
	b.WriteString(sectionHeaderStyle.Render("  Core Info  "))
	b.WriteString("\n")

	ver := game.Version
	if ver == "" {
		if game.LatestVersion != "" {
			ver = game.LatestVersion
		} else {
			ver = "unknown"
		}
	}

	writeField(&b, labelStyle, "Title:", game.Title, valueStyle)
	if game.LatestVersion != "" && game.Version != "" && game.LatestVersion != game.Version {
		latestStr := fmt.Sprintf(" (latest: %s 🆕)", game.LatestVersion)
		writeField(&b, labelStyle, "Version:", ver, valueStyle, updateAvailableStyle.Render(latestStr))
	} else {
		writeField(&b, labelStyle, "Version:", ver, valueStyle)
	}
	writeField(&b, labelStyle, "Engine:", game.Engine, engineStyle(game.Engine))
	writeField(&b, labelStyle, "Status:", game.Status,
		valueStyle.Copy().Foreground(statusColor(game.Status)))

	// ── Status selector (always shown) ─────────────────────────
	b.WriteString("\n")
	b.WriteString(subtleStyle.Render("  Status: "))
	b.WriteString(statusKeyHints(game.Status))
	b.WriteString("\n")

	// ── Section: F95Zone ──────────────────────────────────────────
	b.WriteString(sectionHeaderStyle.Render("  F95Zone  "))
	b.WriteString("\n")

	if game.F95URL != "" {
		writeField(&b, labelStyle, "URL:", game.F95URL, valueStyle)
		b.WriteString(copyHintStyle.Render("  (Press Enter on URL line to open in browser)"))
		b.WriteString("\n")
	} else {
		writeField(&b, labelStyle, "URL:", "not set — press [Ctrl+U] to add", subtleStyle)
	}

	if m.scrapedMeta != nil {
		if m.scrapedMeta.Developer != "" {
			writeField(&b, labelStyle, "Developer:", m.scrapedMeta.Developer, valueStyle)
		}
		if m.scrapedMeta.CoverURL != "" {
			writeField(&b, labelStyle, "Cover:", m.scrapedMeta.CoverURL, subtleStyle)
			b.WriteString(copyHintStyle.Render("  (Press Enter on Cover line to open)"))
			b.WriteString("\n")
		}
	}

	// Version info from F95Zone
	if game.LatestVersion != "" && game.LatestVersion != game.Version {
		writeField(&b, labelStyle, "Latest:", game.LatestVersion, updateAvailableStyle)
	}

	// Tags — show each tag individually styled
	if len(game.Tags) > 0 {
		writeField(&b, labelStyle, "Tags:", "", valueStyle)
		var tagParts []string
		for _, t := range game.Tags {
			if t != "" {
				tagParts = append(tagParts, tagStyle.Render(t))
			}
		}
		b.WriteString("  " + strings.Join(tagParts, " ") + "\n")
	} else {
		writeField(&b, labelStyle, "Tags:", "none", subtleStyle)
	}

	if m.scrapedMeta != nil {
		if m.scrapedMeta.Overview != "" {
			ov := truncate(m.scrapedMeta.Overview, w/3)
			if ov != "" {
				b.WriteString("\n")
				b.WriteString(subtleStyle.Render("  Overview:"))
				b.WriteString("\n")
				b.WriteString("  " + valueStyle.Render(ov))
				b.WriteString("\n")
			}
		}
		writeField(&b, labelStyle, "Scraped:", m.scrapedMeta.LastScraped.Format("2006-01-02"), subtleStyle)
	}

	// ── Section: Metadata ──────────────────────────────────────────
	b.WriteString(sectionHeaderStyle.Render("  Metadata  "))
	b.WriteString("\n")

	writeField(&b, labelStyle, "Path:", game.Path, valueStyle)
	writeField(&b, labelStyle, "Exe:", orDash(game.ExePath), valueStyle)
	writeField(&b, labelStyle, "Size:", formatSize(game.SizeBytes), valueStyle)
	writeField(&b, labelStyle, "Added:", game.CreatedAt.Format("2006-01-02"), subtleStyle)
	writeField(&b, labelStyle, "Updated:", game.UpdatedAt.Format("2006-01-02"), subtleStyle)

	// ── Download section ──────────────────────────────────────────
	progress, status, errStr, stepMsg := m.getDownloadProgress(game.ID)
	if status != "" {
		b.WriteString("\n")
		b.WriteString(accentStyle.Render("  Download"))
		b.WriteString("\n")
		b.WriteString(downloadSection(game.ID, progress, status, errStr, stepMsg, w))
	}

	// ── Action buttons ────────────────────────────────────────────
	actions := subtleStyle.Render(
		"  [e] Edit  [s] Status  [x] Exe  [Ctrl+U] URL  [o] Path  [p] Play  [g] Download  [d] Delete  [Esc] Back  ",
	)
	b.WriteString("\n\n")
	b.WriteString(actions)

	m.renderMessage(&b)

	if m.confirmDelete {
		b.WriteString("\n\n")
		b.WriteString(m.renderDeletePrompt())
	}

	if m.editing {
		b.WriteString("\n\n")
		editBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(purple).
			Padding(0, 1).
			Width(max(0, w-8)).
			Render(m.editInput.View())
		b.WriteString(editBox)
	}

	if m.editingExe {
		b.WriteString("\n\n")
		b.WriteString(accentStyle.Render("  Executable path"))
		b.WriteString("\n")
		b.WriteString(subtleStyle.Render("  Enter a path (or filename in game dir), or clear to auto-detect."))
		b.WriteString("\n")
		// Show available executables in the game directory
		if m.detailGame != nil && m.detailGame.Path != "" {
			exes := listExecutables(m.detailGame.Path)
			if len(exes) > 0 {
				b.WriteString(subtleStyle.Render("  Available:"))
				b.WriteString("\n")
				for _, exe := range exes {
					rel, _ := filepath.Rel(m.detailGame.Path, exe)
					b.WriteString(fmt.Sprintf("    %s\n", subtleStyle.Render(rel)))
				}
			}
		}
		b.WriteString("\n")
		exeBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(purple).
			Padding(0, 1).
			Width(max(0, w-8)).
			Render(m.exeInput.View())
		b.WriteString(exeBox)
	}

	if m.setUrl {
		b.WriteString("\n\n")
		urlBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(purple).
			Padding(0, 1).
			Width(max(0, w-8)).
			Render(m.urlInput.View())
		b.WriteString(urlBox)
	}

	return b.String()
}

// writeField formats a label+value pair for the detail view.
// If an extra string is provided, it is appended after the value.
// If valueStyle is nil, only the label is rendered.
func writeField(b *strings.Builder, label lipgloss.Style, labelText string, value interface{}, valueStyle lipgloss.Style, extra ...string) {
	valStr := fmt.Sprintf("%v", value)
	b.WriteString(label.Render(labelText))
	b.WriteString("  ")
	if valStr != "" {
		b.WriteString(valueStyle.Render(valStr))
	}
	for _, e := range extra {
		b.WriteString(" ")
		b.WriteString(e)
	}
	b.WriteString("\n")
}

// writeLabelField renders just a label without a value (for multi-line sections).
func writeLabelField(b *strings.Builder, label lipgloss.Style, labelText string, rest ...string) {
	b.WriteString(label.Render(labelText))
	for _, r := range rest {
		b.WriteString("  ")
		b.WriteString(r)
	}
	b.WriteString("\n")
}

// downloadSection renders download progress or status for a game.
func downloadSection(gameID int64, p downloader.Progress, status db.DownloadStatus, errStr string, stepMsg string, width int) string {
	var b strings.Builder

	switch status {
	case db.DownloadStatusDownloading, db.DownloadStatusPending:
		if stepMsg != "" && p.BytesDownloaded <= 0 {
			// Step-by-step host finding / trying phase
			if strings.HasPrefix(stepMsg, "✗") {
				b.WriteString(redStyle.Render("  " + stepMsg))
			} else {
				b.WriteString(accentStyle.Render("  " + stepMsg))
			}
		} else if p.BytesDownloaded > 0 {
			// Actual download progress
			bar := renderProgressBarWidget(p.Percent, width-8)
			speed := formatSpeed(p.SpeedBytesPerSec)
			downloaded := formatSize(p.BytesDownloaded)
			total := formatSize(p.TotalBytes)
			b.WriteString(accentStyle.Render("  Downloading:"))
			b.WriteString("\n\n")
			b.WriteString("  " + bar + "\n")
			b.WriteString(subtleStyle.Render(fmt.Sprintf("  %s / %s  •  %s  •  %.1f%%", downloaded, total, speed, p.Percent)))
		} else {
			b.WriteString(accentStyle.Render("  " + stepMsg))
		}

	case db.DownloadStatusCompleted:
		b.WriteString(greenStyle.Render("  ✓ Download completed!"))
		b.WriteString("\n")
		if stepMsg != "" {
			b.WriteString(subtleStyle.Render("  " + stepMsg))
			b.WriteString("\n")
		}
		b.WriteString(subtleStyle.Render(fmt.Sprintf("  %s downloaded", formatSize(p.BytesDownloaded))))

	case db.DownloadStatusFailed:
		b.WriteString(redStyle.Render("  ✗ Download failed"))
		b.WriteString("\n")
		b.WriteString(subtleStyle.Render(fmt.Sprintf("  %s", errStr)))

	case db.DownloadStatusExtracting:
		b.WriteString(accentStyle.Render("  " + stepMsg))

	default:
		return ""
	}

	return b.String()
}

// renderProgressBarWidget renders a visual progress bar for the TUI.
func renderProgressBarWidget(percent float64, width int) string {
	if width < 10 {
		width = 30
	}
	barWidth := width - 2
	filled := int(percent / 100.0 * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	if filled < 0 {
		filled = 0
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	barColor := lipgloss.Color("99") // purple
	if percent > 0 {
		switch {
		case percent < 33:
			barColor = lipgloss.Color("208") // orange
		case percent < 66:
			barColor = lipgloss.Color("220") // yellow
		default:
			barColor = lipgloss.Color("82") // green
		}
	}

	return lipgloss.NewStyle().
		Foreground(barColor).
		Render("[" + bar + "]")
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
