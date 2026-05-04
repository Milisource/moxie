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

	lines := []string{
		"               Keyboard Shortcuts               ",
		"",
		"  ── Navigation ─────────────────────────────",
		"    ↑ / k       Move selection up",
		"    ↓ / j       Move selection down",
		"    Enter       View game details",
		"    Esc / ←     Return to library list",
		"",
		"  ── Actions ────────────────────────────────",
		"    /           Filter / search games",
		"    s           Cycle sort field",
		"    Ctrl+E      Cycle engine filter",
		"    Ctrl+S      Cycle status filter",
		"    d           Delete selected game",
		"    e           Edit game title (detail view)",
		"    s           Cycle game status (detail view)",
		"    o           Show game folder path",
		"    u           Set / edit F95Zone URL (detail view)",
		"    p           Play game hint (detail view)",
		"    g           Download game (detail view)",
		"",
		"  ── General ────────────────────────────────",
		"    ?           Toggle this help screen",
		"    q           Quit (library view only)",
		"    Ctrl+C      Force quit (anywhere)",
		"",
		"  ── CLI Quick Start (outside TUI) ────────",
		"    moxie scan ~/Downloads     Scan for games",
		"    moxie scrape --auto        Scrape F95Zone metadata",
		"    moxie steam add <id>       Add a game to Steam",
		"    moxie steam fix-artwork <id>  Download Steam artwork",
		"    moxie check-updates        Check for game updates",
		"",
		"  Press any key to close help  ",
	}
	content := strings.Join(lines, "\n")
	boxed := helpBoxStyle.Render(content)

	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, boxed)
}
