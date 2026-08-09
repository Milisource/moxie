package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ── Help Overlay ───────────────────────────────────────────────────────────

func (m model) helpView() string {
	w := m.width
	if w <= 0 {
		w = 80
	}
	h := m.height
	if h <= 0 {
		h = 24
	}

	// Build context-aware help sections.
	// Library view is the default; detail view has extra actions.
	libraryKeys := []string{
		"  ↑/k          Move selection up",
		"  ↓/j          Move selection down",
		"  Enter        View game details",
		"  /            Filter/search games",
		"  s            Cycle sort field",
		"  r            Reverse sort direction",
		"  Ctrl+E       Cycle engine filter",
		"  Ctrl+S       Cycle status filter",
		"  c            Cycle collection filter",
		"  d            Delete selected game",
	}

	detailKeys := []string{
		"  Esc/←        Return to library list",
		"  e            Edit game title",
		"  s            Cycle game status",
		"  x            Edit executable path",
		"  Ctrl+U       Set/edit F95Zone URL",
		"  o            Show game folder path",
		"  p            Play game",
		"  g            Download game",
	}
	if m.viewMode == DetailView {
		// In detail view, q quits directly too
		detailKeys = append(detailKeys, "  q            Quit")
	}

	globalKeys := []string{
		"  ?            Toggle this help screen",
		"  q            Quit",
		"  Ctrl+C       Force quit (anywhere)",
	}

	// ── Build left column: Library Navigation & Actions ─────────
	leftLines := []string{
		"  ── Library Navigation ──",
	}
	leftLines = append(leftLines, libraryKeys...)
	leftLines = append(leftLines, "")

	leftLines = append(leftLines, "  ── Global ──")
	leftLines = append(leftLines, globalKeys...)

	leftContent := strings.Join(leftLines, "\n")

	// ── Build right column: Detail Actions ────────────────────
	rightLines := []string{
		"  ── Detail View Actions ──",
	}
	rightLines = append(rightLines, detailKeys...)

	rightContent := strings.Join(rightLines, "\n")

	// Combine into two-column layout
	colGap := 4
	combined := lipgloss.JoinHorizontal(lipgloss.Top,
		leftContent,
		strings.Repeat(" ", colGap),
		rightContent,
	)

	// Add a header
	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(purple).
		Render("  Keyboard Shortcuts  ")

	// Wrap in a box
	content := header + "\n\n" + combined
	boxed := helpBoxStyle.Render(content)

	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, boxed)
}
