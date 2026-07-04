package tui

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/table"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/launcher"
)

// reflowTable adjusts column widths and table height to fit the current
// terminal dimensions.
func (m *model) reflowTable() {
	avail := m.width - 4
	if avail < 40 {
		avail = 40
	}

	idW := 5
	engineW := 18
	verW := 10
	statusW := 12
	titleW := avail - idW - engineW - verW - statusW - 4 // 4 for col separators
	if titleW < 20 {
		titleW = 20
	}

	m.table.SetColumns([]table.Column{
		{Title: "ID", Width: idW},
		{Title: "Title", Width: titleW},
		{Title: "Engine", Width: engineW},
		{Title: "Version", Width: verW},
		{Title: "Status", Width: statusW},
	})
	m.table.SetWidth(avail)

	th := m.height - 7
	if th < 5 {
		th = 5
	}
	m.table.SetHeight(th)
}

// rebuildFiltered re-filters and re-sorts the game list, then updates the
// table rows.
func (m *model) rebuildFiltered() {
	m.filtered = filterAndSort(m.allGames, m.filterText, m.engineFilter, m.statusFilter, m.sortBy)
	m.updateTableRows()
}

// filterAndSort returns a filtered and sorted copy of the games slice.
func filterAndSort(games []db.GameSummary, titleFilter, engineFilter, statusFilter string, sortBy SortField) []db.GameSummary {
	var out []db.GameSummary
	flw := strings.ToLower(titleFilter)
	for _, g := range games {
		if titleFilter != "" && !strings.Contains(strings.ToLower(g.Title), flw) {
			continue
		}
		if engineFilter != "" && g.Engine != engineFilter {
			continue
		}
		if statusFilter != "" && g.Status != statusFilter {
			continue
		}
		out = append(out, g)
	}

	switch sortBy {
	case SortID:
		sort.Slice(out, func(i, j int) bool {
			return out[i].ID < out[j].ID
		})
	case SortTitle:
		sort.Slice(out, func(i, j int) bool {
			return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
		})
	case SortEngine:
		sort.Slice(out, func(i, j int) bool {
			if out[i].Engine != out[j].Engine {
				return out[i].Engine < out[j].Engine
			}
			return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
		})
	case SortVersion:
		sort.Slice(out, func(i, j int) bool {
			if out[i].Version != out[j].Version {
				return out[i].Version > out[j].Version // newest first
			}
			return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
		})
	}
	return out
}

func (m *model) updateTableRows() {
	rows := make([]table.Row, 0, len(m.filtered))
	for _, g := range m.filtered {
		ver := g.Version
		if ver == "" {
			if g.LatestVersion != "" {
				ver = g.LatestVersion
			} else {
				ver = "unknown"
			}
		}
		engineStyled := engineStyle(g.Engine).Render(g.Engine)

		// Determine if this game has a known path on disk.
		// First, reject empty paths and virtual paths.
		hasPath := g.Path != "" && !strings.HasPrefix(g.Path, "/virtual/")
		// For non-virtual paths, verify the directory actually exists.
		if hasPath {
			if info, err := os.Stat(g.Path); err != nil || !info.IsDir() {
				hasPath = false
			}
		}

		// ── Title column ───────────────────────────────────────────
		title := truncate(g.Title, 72)
		if !hasPath {
			title = noPathStyle.Render(title)
		}

		// Update-available indicator (yellow)
		if g.LatestVersion != "" && g.Version != "" && g.LatestVersion != g.Version {
			indicator := updateAvailableStyle.Render(" 🔄")
			title += indicator
		}

		// Download status indicator
		m.activeDownloadsMu.Lock()
		ad, ok := m.activeDownloads[g.ID]
		m.activeDownloadsMu.Unlock()
		if ok {
			ad.mu.Lock()
			status := ad.status
			ad.mu.Unlock()
			switch status {
			case db.DownloadStatusDownloading:
				title += greenStyle.Render(" ↓")
			case db.DownloadStatusFailed:
				title += redStyle.Render(" ✗")
			case db.DownloadStatusCompleted:
				title += greenStyle.Render(" ✓")
			}
		}

		// ── ID column: status-colored "tag" effect ─────────────────
		idStr := fmt.Sprintf("%d", g.ID)
		idStyled := statusStyle(g.Status).Render(idStr)
		if !hasPath {
			idStyled = noPathStyle.Render(idStr)
		}

		// ── Status column: color-coded text ────────────────────────
		statusStyled := statusStyle(g.Status).Render(g.Status)

		rows = append(rows, table.Row{
			idStyled,
			title,
			engineStyled,
			ver,
			statusStyled,
		})
	}
	m.table.SetRows(rows)
}

func nextStatus(s string) string {
	cycle := []string{"unknown", "active", "completed", "abandoned", "on_hold"}
	for i, st := range cycle {
		if st == s {
			return cycle[(i+1)%len(cycle)]
		}
	}
	return "unknown"
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-3]) + "…"
}

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

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// orDefault returns s if non-empty, otherwise returns def.
func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// listExecutables delegates to the shared launcher package.
// Returns all playable executables in a directory (non-recursive).
func listExecutables(dir string) []string {
	return launcher.ListExecutables(dir)
}

// patchGameSummary applies a mutation to the in-memory allGames entry for the
// given game ID and rebuilds filtered/table views. Avoids a full list reload.
func (m *model) patchGameSummary(mutate func(s *db.GameSummary)) {
	for i := range m.allGames {
		if m.allGames[i].ID == m.selectedID {
			mutate(&m.allGames[i])
			break
		}
	}
	m.rebuildFiltered()
}

// renderTags joins tags with commas, returning "-" for empty.
func renderTags(tags []string) string {
	if len(tags) == 0 {
		return "-"
	}
	return strings.Join(tags, ", ")
}

// max returns the larger of a and b.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
