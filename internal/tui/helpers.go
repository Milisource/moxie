package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/table"

	"github.com/mili/moxie/internal/db"
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
func filterAndSort(games []db.Game, titleFilter, engineFilter, statusFilter string, sortBy SortField) []db.Game {
	var out []db.Game
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
			ver = "-"
		}
		engineStyled := engineStyle(g.Engine).Render(g.Engine)
		title := truncate(g.Title, 72)
		if g.LatestVersion != "" && g.LatestVersion != g.Version {
			indicator := updateAvailableStyle.Render(" 🔄")
			title += indicator
		}
		rows = append(rows, table.Row{
			fmt.Sprintf("%d", g.ID),
			title,
			engineStyled,
			ver,
			g.Status,
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

// findPlayableExe finds the best executable in a game directory.
// Prefers native Linux binaries over .exe files.
func findPlayableExe(dir, knownExe string) string {
	// If we know the exe and it exists, use it.
	if knownExe != "" {
		if _, err := os.Stat(knownExe); err == nil {
			return knownExe
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	var nativeBinaries []string
	var appImages []string
	var exes []string

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		fullPath := filepath.Join(dir, name)
		ext := strings.ToLower(filepath.Ext(name))

		// Skip non-game executables.
		lower := strings.ToLower(name)
		if strings.Contains(lower, "unitycrashhandler") ||
			strings.Contains(lower, "unins") ||
			strings.Contains(lower, "setup") {
			continue
		}

		switch {
		case strings.HasSuffix(name, ".AppImage"):
			appImages = append(appImages, fullPath)
		case ext == ".x86_64" || ext == ".x86" || ext == ".sh":
			nativeBinaries = append(nativeBinaries, fullPath)
		case ext == ".exe":
			exes = append(exes, fullPath)
		}
	}

	// Prefer AppImages, then native binaries, then .exe (via Wine).
	if len(appImages) > 0 {
		return pickLargest(appImages)
	}
	if len(nativeBinaries) > 0 {
		return pickLargest(nativeBinaries)
	}
	if len(exes) > 0 {
		return pickLargest(exes)
	}
	return ""
}

func pickLargest(paths []string) string {
	if len(paths) == 1 {
		return paths[0]
	}
	var best string
	var bestSize int64
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if info.Size() > bestSize {
			bestSize = info.Size()
			best = p
		}
	}
	return best
}

// launchExe starts a game executable. Uses Wine on Linux/macOS for .exe files,
// and tries CrossOver on macOS as a fallback.
func launchExe(exe string) error {
	ext := strings.ToLower(filepath.Ext(exe))
	var cmd *exec.Cmd
	switch {
	case ext == ".appimage":
		cmd = exec.Command(exe)
	case ext == ".sh":
		cmd = exec.Command("sh", exe)
	case ext == ".exe":
		if runtime.GOOS == "windows" {
			cmd = exec.Command(exe)
		} else if winePath, err := exec.LookPath("wine"); err == nil {
			cmd = exec.Command(winePath, exe)
		} else if runtime.GOOS == "darwin" {
			crossoverWine := "/Applications/CrossOver.app/Contents/SharedSupport/CrossOver/bin/wine"
			if _, err := os.Stat(crossoverWine); err == nil {
				cmd = exec.Command(crossoverWine, exe)
			} else {
				return fmt.Errorf("wine not found and CrossOver not available — cannot launch .exe files on %s", runtime.GOOS)
			}
		} else {
			return fmt.Errorf("wine not found — cannot launch .exe files on %s", runtime.GOOS)
		}
	default:
		cmd = exec.Command(exe)
	}
	return cmd.Start()
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
