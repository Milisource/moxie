package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ── Detail View ────────────────────────────────────────────────────────────

func (m model) detailView() string {
	game, err := m.db.GetGame(m.selectedID)
	if err != nil || game == nil {
		return m.libraryView()
	}

	var b strings.Builder
	w := m.width
	if w <= 0 {
		w = 80
	}

	// ── Header ────────────────────────────────────────────────────
	back := titleStyle.Render("◂  Back to Library  ")
	hint := subtleStyle.Render("[Esc] back  [e] edit  [s] status  [d] delete  [o] path  [u] url  [p] play")
	gap := max(0, w-lipgloss.Width(back)-lipgloss.Width(hint)-4)
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, back, strings.Repeat(" ", gap), hint))
	b.WriteString("\n")
	sep := max(0, w-4)
	b.WriteString(separatorStyle.Render(strings.Repeat("━", sep)))
	b.WriteString("\n\n")

	// ── Game info ─────────────────────────────────────────────────
	ver := orDash(game.Version)
	tags := renderTags(game.Tags)
	added := game.CreatedAt.Format("2006-01-02")
	updated := game.UpdatedAt.Format("2006-01-02")

	var info []string
	info = append(info, fmt.Sprintf("%s  %s", labelStyle.Render("Title:"), valueStyle.Render(game.Title)))
	info = append(info, fmt.Sprintf("%s  %s", labelStyle.Render("Engine:"), valueStyle.Render(game.Engine)))
	if game.LatestVersion != "" && game.LatestVersion != game.Version {
		latestStr := fmt.Sprintf(" (latest: %s 🆕)", game.LatestVersion)
		info = append(info, fmt.Sprintf("%s  %s%s", labelStyle.Render("Version:"), valueStyle.Render(ver), updateAvailableStyle.Render(latestStr)))
	} else {
		info = append(info, fmt.Sprintf("%s  %s", labelStyle.Render("Version:"), valueStyle.Render(ver)))
	}
	info = append(info, fmt.Sprintf("%s  %s", labelStyle.Render("Status:"),
		valueStyle.Copy().Foreground(statusColor(game.Status)).Render(game.Status)))
	info = append(info, "")
	info = append(info, fmt.Sprintf("%s  %s", labelStyle.Render("Path:"), valueStyle.Render(game.Path)))
	info = append(info, fmt.Sprintf("%s  %s", labelStyle.Render("Size:"), valueStyle.Render(formatSize(game.SizeBytes))))
	info = append(info, fmt.Sprintf("%s  %s", labelStyle.Render("Added:"), valueStyle.Render(added)))
	info = append(info, fmt.Sprintf("%s  %s", labelStyle.Render("Updated:"), valueStyle.Render(updated)))
	info = append(info, "")
	info = append(info, fmt.Sprintf("%s  %s", labelStyle.Render("F95Zone:"), valueStyle.Render(orDash(game.F95URL))))
	info = append(info, fmt.Sprintf("%s  %s", labelStyle.Render("Tags:"), valueStyle.Render(tags)))

	// Scraped metadata.
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

	// ── Action buttons ────────────────────────────────────────────
	actions := subtleStyle.Render(
		"  [e] Edit Title  [s] Cycle Status  [d] Delete  [o] Show Path  [u] Set URL  [p] Play  [Esc] Back  ",
	)
	b.WriteString(actions)

	// ── Status / error message ────────────────────────────────────
	m.renderMessage(&b)

	// ── Delete confirmation ───────────────────────────────────────
	if m.confirmDelete {
		b.WriteString("\n\n")
		b.WriteString(m.renderDeletePrompt())
	}

	// ── Title edit overlay ────────────────────────────────────────
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

	// ── URL input overlay ─────────────────────────────────────────
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

	return appStyle.Render(b.String())
}
