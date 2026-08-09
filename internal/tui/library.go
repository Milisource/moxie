package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/mili/moxie/internal/db"
)

// ── Library View ───────────────────────────────────────────────────────────

func (m model) libraryView() string {
	var b strings.Builder
	w := m.width
	if w <= 0 {
		w = 80
	}

	// ── Title bar ─────────────────────────────────────────────────
	title := titleStyle.Render("◆  F95 Game Manager  ")
	helpHint := subtleStyle.Render("[?] Help  [q] Quit")
	gap := w - lipgloss.Width(title) - lipgloss.Width(helpHint) - 4
	if gap < 1 {
		gap = 1
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, title, strings.Repeat(" ", gap), helpHint))
	b.WriteString("\n")

	// ── Startup tip (auto-dismiss) ─────────────────────────────
	if m.showStartupTip {
		tip := noticeStyle.Render("  💡 Press / to search  •  s to sort  •  Ctrl+E/S to filter  •  Enter for details  •  ? for help  ")
		b.WriteString(tip)
		b.WriteString("\n")
	}

	// ── Separator ─────────────────────────────────────────────────
	sep := max(0, w-4)
	b.WriteString(separatorStyle.Render(strings.Repeat("━", sep)))
	b.WriteString("\n")

	// ── Status line ───────────────────────────────────────────────
	matchCount := len(m.filtered)
	totalCount := len(m.allGames)
	matchInfo := accentStyle.Render(fmt.Sprintf("%d/%d games", matchCount, totalCount))

	engineInfo := m.engineFilter
	if engineInfo == "" {
		engineInfo = "All"
	}

	statusInfo := m.statusFilter
	if statusInfo == "" {
		statusInfo = "All"
	}

	collectionInfo := m.collectionName
	if collectionInfo == "" {
		collectionInfo = "All"
	}

	filterIndicator := subtleStyle.Render("—")
	if m.filterText != "" {
		filterIndicator = filterActiveStyle.Render(fmt.Sprintf("%q", m.filterText))
	}

	// Count active downloads
	dlCount := 0
	m.activeDownloadsMu.Lock()
	for _, ad := range m.activeDownloads {
		ad.mu.Lock()
		if ad.status == db.DownloadStatusDownloading || ad.status == db.DownloadStatusPending {
			dlCount++
		}
		ad.mu.Unlock()
	}
	m.activeDownloadsMu.Unlock()

	downloadInfo := ""
	if dlCount > 0 {
		downloadInfo = fmt.Sprintf("  ⬇ %d", dlCount)
	}

	spinnerView := ""
	if m.spinnerActive {
		spinnerView = " " + m.spinner.View()
	}

	stat := statusBarStyle.Render(fmt.Sprintf(
		"  %s  │  Engine: %s  │  Status: %s  │  Coll: %s  │  Sort: %s  │  Filter: %s%s%s",
		matchInfo,
		engineInfo,
		statusInfo,
		collectionInfo,
		m.sortIndicator(),
		filterIndicator,
		downloadInfo,
		spinnerView,
	))
	b.WriteString(stat)
	b.WriteString("\n")

	// ── Filter input overlay ──────────────────────────────────────
	// Always show the filter bar.
	b.WriteString("\n")
	if m.filterInput.Focused() {
		b.WriteString(filterInputStyle.Render(m.filterInput.View()))
	} else {
		dimFilter := filterInputStyle.Copy().BorderForeground(subtle)
		if m.filterText != "" {
			dimFilter = dimFilter.Foreground(subtle)
			b.WriteString(dimFilter.Render("🔍 " + m.filterText))
		} else {
			b.WriteString(dimFilter.Render("type to filter..."))
		}
	}
	b.WriteString("\n")

	// ── Game table or empty state ─────────────────────────────────
	b.WriteString("\n")
	if len(m.filtered) == 0 {
		if totalCount == 0 {
			b.WriteString(subtleStyle.Render("  Your library is empty  "))
		} else {
			b.WriteString(subtleStyle.Render("  No games match your filter  "))
		}
	} else {
		b.WriteString(m.table.View())
	}

	// ── Footer / key hints ────────────────────────────────────────
	b.WriteString("\n")
	var footerParts []string
	footerParts = append(footerParts, "↑↓/jk navigate")
	footerParts = append(footerParts, "Enter details")
	if m.filterInput.Focused() || m.filterText != "" {
		footerParts = append(footerParts, "/ filter active")
	} else {
		footerParts = append(footerParts, "/ filter")
	}
	if m.engineFilter != "" {
		footerParts = append(footerParts, "Ctrl+E engine:"+m.engineFilter)
	}
	if m.statusFilter != "" {
		footerParts = append(footerParts, "Ctrl+S status:"+m.statusFilter)
	}
	footerParts = append(footerParts, "s sort")
	footerParts = append(footerParts, "r reverse")
	footerParts = append(footerParts, "b browse dirs")
	footerParts = append(footerParts, "? help")
	footer := subtleStyle.Render("  " + strings.Join(footerParts, "  •  ") + "  ")
	b.WriteString(footer)

	// ── Status / error message ────────────────────────────────────
	m.renderMessage(&b)

	// ── Delete confirmation ───────────────────────────────────────
	if m.confirmDelete {
		b.WriteString("\n\n")
		b.WriteString(m.renderDeletePrompt())
	}

	return appStyle.Render(b.String())
}

// ── Shared rendering helpers ───────────────────────────────────────────────

// renderMessage writes a notice and/or error line to b.
// Notices (informational, e.g. path display or URL confirmation) are shown
// in yellow via noticeStyle. Errors are shown in bold red via errorStyle.
func (m model) renderMessage(b *strings.Builder) {
	if m.notice != "" {
		b.WriteString("\n")
		b.WriteString(noticeStyle.Render(m.notice))
	}
	if m.err != nil {
		msg := m.err.Error()
		b.WriteString("\n")
		b.WriteString(errorStyle.Render(fmt.Sprintf("  ✗ %s  ", msg)))
	}
}

// ── File picker view ─────────────────────────────────────────────────

func (m model) filePickerView() string {
	var b strings.Builder
	w := m.width
	if w <= 0 {
		w = 80
	}

	// Header
	title := titleStyle.Render("◆  Browse Game Directory  ")
	hint := subtleStyle.Render("[Esc] cancel")
	gap := w - lipgloss.Width(title) - lipgloss.Width(hint) - 4
	if gap < 1 {
		gap = 1
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, title, strings.Repeat(" ", gap), hint))
	b.WriteString("\n")

	// Separator
	sep := max(0, w-4)
	b.WriteString(separatorStyle.Render(strings.Repeat("━", sep)))
	b.WriteString("\n\n")

	// File picker
	b.WriteString(m.filePicker.View())
	b.WriteString("\n")

	// Footer
	b.WriteString(subtleStyle.Render("  ↑/↓ navigate  •  Enter select directory  •  Esc cancel"))

	return appStyle.Render(b.String())
}

// renderDeletePrompt returns a styled delete confirmation widget.
func (m model) renderDeletePrompt() string {
	title := m.deleteTitle
	if title == "" {
		return deletePromptStyle.Render("  Delete this game?  (y/N)  ")
	}
	return deletePromptStyle.Render(fmt.Sprintf("  Delete '%s'?  (y/N)  ", truncate(title, 40)))
}
