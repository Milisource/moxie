package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
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

	filterIndicator := subtleStyle.Render("—")
	if m.filterText != "" {
		filterIndicator = filterActiveStyle.Render(fmt.Sprintf("%q", m.filterText))
	}

	stat := statusBarStyle.Render(fmt.Sprintf(
		"  %s  │  Engine: %s  │  Status: %s  │  Sort: %s  │  Filter: %s  ",
		matchInfo,
		engineInfo,
		statusInfo,
		m.sortBy.Indicator(),
		filterIndicator,
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
	footer := subtleStyle.Render(
		"  ↑↓/jk navigate  Enter details  / filter  s sort  d delete  ? help  ",
	)
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

// renderMessage writes an error or notice line to b.
func (m model) renderMessage(b *strings.Builder) {
	if m.err == nil {
		return
	}
	msg := m.err.Error()
	b.WriteString("\n")
	if strings.HasPrefix(msg, "📁") || strings.HasPrefix(msg, "✅") {
		// Notices: path display, play hint, URL confirmation, etc.
		msg = strings.TrimPrefix(msg, "📁 ")
		msg = strings.TrimPrefix(msg, "✅ ")
		b.WriteString(noticeStyle.Render(msg))
	} else {
		b.WriteString(errorStyle.Render(fmt.Sprintf("  ✗ %s  ", msg)))
	}
}

// renderDeletePrompt returns a styled delete confirmation widget.
func (m model) renderDeletePrompt() string {
	title := m.deleteTitle
	if title == "" {
		return deletePromptStyle.Render("  Delete this game?  (y/N)  ")
	}
	return deletePromptStyle.Render(fmt.Sprintf("  Delete '%s'?  (y/N)  ", truncate(title, 40)))
}
