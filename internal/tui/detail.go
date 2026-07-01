package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/downloader"
)

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
	hint := subtleStyle.Render("[Esc] back  [e] edit  [s] status  [x] exe  [d] delete  [o] path  [u] url  [p] play  [g] download")
	gap := max(0, w-lipgloss.Width(back)-lipgloss.Width(hint)-4)
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, back, strings.Repeat(" ", gap), hint))
	b.WriteString("\n")
	sep := max(0, w-4)
	b.WriteString(separatorStyle.Render(strings.Repeat("━", sep)))
	b.WriteString("\n\n")

	// ── Game info ─────────────────────────────────────────────────
	ver := game.Version
	if ver == "" {
		if game.LatestVersion != "" {
			ver = game.LatestVersion
		} else {
			ver = "unknown"
		}
	}
	tags := renderTags(game.Tags)
	added := game.CreatedAt.Format("2006-01-02")
	updated := game.UpdatedAt.Format("2006-01-02")

	var info []string
	info = append(info, fmt.Sprintf("%s  %s", labelStyle.Render("Title:"), valueStyle.Render(game.Title)))
	info = append(info, fmt.Sprintf("%s  %s", labelStyle.Render("Engine:"), valueStyle.Render(game.Engine)))
	if game.LatestVersion != "" && game.Version != "" && game.LatestVersion != game.Version {
		latestStr := fmt.Sprintf(" (latest: %s 🆕)", game.LatestVersion)
		info = append(info, fmt.Sprintf("%s  %s%s", labelStyle.Render("Version:"), valueStyle.Render(ver), updateAvailableStyle.Render(latestStr)))
	} else {
		info = append(info, fmt.Sprintf("%s  %s", labelStyle.Render("Version:"), valueStyle.Render(ver)))
	}
	info = append(info, fmt.Sprintf("%s  %s", labelStyle.Render("Status:"),
		valueStyle.Copy().Foreground(statusColor(game.Status)).Render(game.Status)))
	info = append(info, "")
	info = append(info, fmt.Sprintf("%s  %s", labelStyle.Render("Path:"), valueStyle.Render(game.Path)))
	info = append(info, fmt.Sprintf("%s  %s", labelStyle.Render("Exe:"), valueStyle.Render(orDash(game.ExePath))))
	info = append(info, fmt.Sprintf("%s  %s", labelStyle.Render("Size:"), valueStyle.Render(formatSize(game.SizeBytes))))
	info = append(info, fmt.Sprintf("%s  %s", labelStyle.Render("Added:"), valueStyle.Render(added)))
	info = append(info, fmt.Sprintf("%s  %s", labelStyle.Render("Updated:"), valueStyle.Render(updated)))
	info = append(info, "")
	info = append(info, fmt.Sprintf("%s  %s", labelStyle.Render("F95Zone:"), valueStyle.Render(orDash(game.F95URL))))
	info = append(info, fmt.Sprintf("%s  %s", labelStyle.Render("Tags:"), valueStyle.Render(tags)))

	if m.scrapedMeta != nil {
		if m.scrapedMeta.Developer != "" {
			info = append(info, fmt.Sprintf("%s  %s", labelStyle.Render("Developer:"), valueStyle.Render(m.scrapedMeta.Developer)))
		}
		if m.scrapedMeta.Overview != "" {
			ov := truncate(m.scrapedMeta.Overview, 70)
			info = append(info, fmt.Sprintf("%s  %s", labelStyle.Render("Overview:"), valueStyle.Render(ov)))
		}
		info = append(info, fmt.Sprintf("%s  %s", labelStyle.Render("Scraped:"), valueStyle.Render(m.scrapedMeta.LastScraped.Format("2006-01-02"))))
	}

	boxContent := strings.Join(info, "\n")
	b.WriteString(boxStyle.Width(max(0, w-6)).Render(boxContent))
	b.WriteString("\n")

	// ── Download section ──────────────────────────────────────────
	progress, status, errStr, stepMsg := m.getDownloadProgress(game.ID)
	if status != "" {
		b.WriteString(accentStyle.Render("  Download"))
		b.WriteString("\n")
		b.WriteString(downloadSection(game.ID, progress, status, errStr, stepMsg, w))
	}

	// ── Action buttons ────────────────────────────────────────────
	actions := subtleStyle.Render(
		"  [e] Edit Title  [s] Cycle Status  [x] Edit Exe  [d] Delete  [o] Show Path  [u] Set URL  [p] Play  [g] Download  [Esc] Back  ",
	)
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
