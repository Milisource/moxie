package tui

import (
	"testing"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/mili/moxie/internal/db"
)

// ---------------------------------------------------------------------------
// truncate
// ---------------------------------------------------------------------------

func TestTruncate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"short enough", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"needs truncation", "hello world", 8, "hello…"},
		{"newlines stripped", "hello\nworld", 15, "hello world"},
		{"unicode aware", "héllo wörld", 8, "héllo…"},
		{"empty string", "", 5, ""},
		{"minimum truncation", "hello", 3, "…"}, // n-3 = 0, so zero content chars + ellipsis
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.s, tt.n)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// orDash
// ---------------------------------------------------------------------------

func TestOrDash(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"", "-"},
		{"hello", "hello"},
		{"   ", "   "}, // spaces are not empty
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := orDash(tt.input)
			if got != tt.want {
				t.Errorf("orDash(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// renderTags
// ---------------------------------------------------------------------------

func TestRenderTags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		tags []string
		want string
	}{
		{"nil tags", nil, "-"},
		{"empty tags", []string{}, "-"},
		{"single tag", []string{"adult"}, "adult"},
		{"multiple tags", []string{"rpg", "fantasy", "completed"}, "rpg, fantasy, completed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderTags(tt.tags)
			if got != tt.want {
				t.Errorf("renderTags(%v) = %q, want %q", tt.tags, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// max
// ---------------------------------------------------------------------------

func TestMax(t *testing.T) {
	t.Parallel()
	tests := []struct {
		a, b int
		want int
	}{
		{1, 2, 2},
		{2, 1, 2},
		{5, 5, 5},
		{-1, 0, 0},
		{-5, -10, -5},
	}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := max(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("max(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// formatSize
// ---------------------------------------------------------------------------

func TestFormatSize_TUI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatSize(tt.bytes)
			if got != tt.want {
				t.Errorf("formatSize(%d) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// nextStatus
// ---------------------------------------------------------------------------

func TestNextStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"unknown", "active"},
		{"active", "completed"},
		{"completed", "abandoned"},
		{"abandoned", "on_hold"},
		{"on_hold", "unknown"},
		{"nonexistent", "unknown"},
		{"", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := nextStatus(tt.input)
			if got != tt.want {
				t.Errorf("nextStatus(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// statusColor
// ---------------------------------------------------------------------------

func TestStatusColor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status string
		want   lipgloss.Color
	}{
		{"active", green},
		{"completed", cyan},
		{"abandoned", red},
		{"on_hold", yellow},
		{"", subtle},
		{"unknown", subtle},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := statusColor(tt.status)
			if got != tt.want {
				t.Errorf("statusColor(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// engineColor
// ---------------------------------------------------------------------------

func TestEngineColor(t *testing.T) {
	t.Parallel()
	// Every canonical engine should return a non-default color.
	engines := []string{
		"Unity", "RenPy", "RPGM", "UnrealEngine", "HTML",
		"Flash", "Java", "ADRIFT", "QSP", "RAGS", "Tads",
		"WebGL", "WolfRPG",
	}
	for _, e := range engines {
		t.Run(e, func(t *testing.T) {
			got := engineColor(e)
			if got == subtle {
				t.Errorf("engineColor(%q) should not return subtle (the default): got %q", e, got)
			}
			if got == "" {
				t.Errorf("engineColor(%q) should not be empty", e)
			}
		})
	}
	// Unknown engine returns subtle.
	if got := engineColor("UnknownEngine"); got != subtle {
		t.Errorf("engineColor(unknown) = %q, want %q (subtle)", got, subtle)
	}
	if got := engineColor(""); got != subtle {
		t.Errorf("engineColor(empty) = %q, want %q (subtle)", got, subtle)
	}
}

// ---------------------------------------------------------------------------
// filterAndSort
// ---------------------------------------------------------------------------

func TestFilterAndSort_NoFilters(t *testing.T) {
	t.Parallel()
	games := []db.GameSummary{
		{ID: 3, Title: "Charlie", Engine: "Unity", Status: "active"},
		{ID: 1, Title: "Alpha", Engine: "RenPy", Status: "completed"},
		{ID: 2, Title: "Bravo", Engine: "Unity", Status: "active"},
	}
	result := filterAndSort(games, "", "", "", SortID, false)
	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}
	if result[0].ID != 1 || result[1].ID != 2 || result[2].ID != 3 {
		t.Errorf("expected sorted by ID ascending: got IDs %d, %d, %d", result[0].ID, result[1].ID, result[2].ID)
	}
}

func TestFilterAndSort_TitleFilter(t *testing.T) {
	t.Parallel()
	games := []db.GameSummary{
		{ID: 1, Title: "Alpha", Engine: "Unity"},
		{ID: 2, Title: "Bravo", Engine: "RenPy"},
		{ID: 3, Title: "Champion", Engine: "Unity"},
	}
	result := filterAndSort(games, "ch", "", "", SortID, false)
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
	if result[0].Title != "Champion" {
		t.Errorf("expected Champion, got %q", result[0].Title)
	}
}

func TestFilterAndSort_EngineFilter(t *testing.T) {
	t.Parallel()
	games := []db.GameSummary{
		{ID: 1, Title: "A", Engine: "Unity"},
		{ID: 2, Title: "B", Engine: "RenPy"},
		{ID: 3, Title: "C", Engine: "Unity"},
	}
	result := filterAndSort(games, "", "Unity", "", SortID, false)
	if len(result) != 2 {
		t.Fatalf("expected 2 Unity games, got %d", len(result))
	}
	for _, g := range result {
		if g.Engine != "Unity" {
			t.Errorf("expected Unity, got %q", g.Engine)
		}
	}
}

func TestFilterAndSort_StatusFilter(t *testing.T) {
	t.Parallel()
	games := []db.GameSummary{
		{ID: 1, Title: "A", Status: "active"},
		{ID: 2, Title: "B", Status: "completed"},
		{ID: 3, Title: "C", Status: "active"},
	}
	result := filterAndSort(games, "", "", "completed", SortID, false)
	if len(result) != 1 {
		t.Fatalf("expected 1 completed game, got %d", len(result))
	}
	if result[0].Title != "B" {
		t.Errorf("expected B, got %q", result[0].Title)
	}
}

func TestFilterAndSort_TitleSort(t *testing.T) {
	t.Parallel()
	games := []db.GameSummary{
		{ID: 3, Title: "Charlie"},
		{ID: 1, Title: "Alpha"},
		{ID: 2, Title: "Bravo"},
	}
	result := filterAndSort(games, "", "", "", SortTitle, false)
	if result[0].Title != "Alpha" || result[1].Title != "Bravo" || result[2].Title != "Charlie" {
		t.Errorf("expected sorted by title, got %v", result)
	}
}

func TestFilterAndSort_EngineSort(t *testing.T) {
	t.Parallel()
	games := []db.GameSummary{
		{ID: 1, Title: "A", Engine: "RenPy"},
		{ID: 2, Title: "B", Engine: "Unity"},
		{ID: 3, Title: "C", Engine: "RenPy"},
	}
	result := filterAndSort(games, "", "", "", SortEngine, false)
	// RenPy < Unity alphabetically
	if result[0].Engine != "RenPy" || result[1].Engine != "RenPy" || result[2].Engine != "Unity" {
		t.Errorf("expected sorted by engine, got %v", result)
	}
	// Within RenPy, titles should be sorted
	if result[0].Title != "A" || result[1].Title != "C" {
		t.Errorf("expected RenPy sorted by title: got %v", result)
	}
}

func TestFilterAndSort_VersionSort(t *testing.T) {
	t.Parallel()
	games := []db.GameSummary{
		{ID: 1, Title: "A", Version: "0.5"},
		{ID: 2, Title: "B", Version: "1.0"},
		{ID: 3, Title: "C", Version: "0.5"},
	}
	result := filterAndSort(games, "", "", "", SortVersion, false)
	// Version sort: newest first (descending)
	if result[0].Version != "1.0" {
		t.Errorf("expected 1.0 first (newest), got %v", result[0].Version)
	}
	// Within same version, titles sorted
	if result[1].Title != "A" || result[2].Title != "C" {
		t.Errorf("expected same-version sorted by title: got %v", result)
	}
}

func TestFilterAndSort_EmptyList(t *testing.T) {
	t.Parallel()
	result := filterAndSort(nil, "", "", "", SortID, false)
	if len(result) != 0 {
		t.Errorf("expected 0 from nil input, got %d", len(result))
	}
}

func TestFilterAndSort_Reverse(t *testing.T) {
	t.Parallel()
	games := []db.GameSummary{
		{ID: 3, Title: "Charlie", Engine: "RenPy"},
		{ID: 1, Title: "Alpha", Engine: "Unity"},
		{ID: 2, Title: "Bravo", Engine: "RenPy"},
	}
	// desc=true reverses the default ascending ID order.
	result := filterAndSort(games, "", "", "", SortID, true)
	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}
	if result[0].ID != 3 || result[1].ID != 2 || result[2].ID != 1 {
		t.Errorf("expected descending IDs 3,2,1: got %d, %d, %d", result[0].ID, result[1].ID, result[2].ID)
	}

	// desc=true on SortVersion flips the default newest-first order.
	result = filterAndSort(games, "", "", "", SortVersion, true)
	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}
	if result[0].ID != 3 {
		t.Errorf("expected reverse version sort to start with ID 3 (empty versions tie on title asc, reversed), got %d", result[0].ID)
	}

	// Filtering still applies when descending.
	result = filterAndSort(games, "", "RenPy", "", SortID, true)
	if len(result) != 2 {
		t.Fatalf("expected 2 RenPy games, got %d", len(result))
	}
	if result[0].ID != 3 || result[1].ID != 2 {
		t.Errorf("expected RenPy games descending 3,2: got %d, %d", result[0].ID, result[1].ID)
	}
}

// ---------------------------------------------------------------------------
// SortField String and Indicator
// ---------------------------------------------------------------------------

func TestSortField_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		field SortField
		want  string
	}{
		{SortID, "ID"},
		{SortTitle, "Title"},
		{SortEngine, "Engine"},
		{SortVersion, "Version"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.field.String()
			if got != tt.want {
				t.Errorf("SortField(%d).String() = %q, want %q", tt.field, got, tt.want)
			}
		})
	}
}

func TestSortField_Indicator(t *testing.T) {
	t.Parallel()
	tests := []struct {
		field SortField
		want  string
	}{
		{SortID, "ID ↑"},
		{SortTitle, "Title ↑"},
		{SortEngine, "Engine ↑"},
		{SortVersion, "Version ↓"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.field.Indicator()
			if got != tt.want {
				t.Errorf("SortField(%d).Indicator() = %q, want %q", tt.field, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// cycleSort
// ---------------------------------------------------------------------------

func TestCycleSort(t *testing.T) {
	t.Parallel()
	tests := []struct {
		start SortField
		want  SortField
	}{
		{SortID, SortTitle},
		{SortTitle, SortEngine},
		{SortEngine, SortVersion},
		{SortVersion, SortID},
	}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			m := &model{sortBy: tt.start, table: table.New(table.WithColumns(nil))}
			m.cycleSort()
			if m.sortBy != tt.want {
				t.Errorf("cycleSort(%v) = %v, want %v", tt.start, m.sortBy, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// cycleEngineFilter
// ---------------------------------------------------------------------------

func TestCycleEngineFilter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		start string
		want  string
	}{
		{"", "Unity"},
		{"Unity", "RenPy"},
		{"RenPy", "RPGM"},
		{"RPGM", "UnrealEngine"},
		{"UnrealEngine", "HTML"},
		{"HTML", "Java"},
		{"Java", "Godot"},
		{"Godot", "Flash"},
		{"Flash", "Others"},
		{"Others", "ADRIFT"},
		{"ADRIFT", "QSP"},
		{"QSP", "RAGS"},
		{"RAGS", "Tads"},
		{"Tads", "WebGL"},
		{"WebGL", "WolfRPG"},
		{"WolfRPG", ""},
	}
	for _, tt := range tests {
		t.Run(tt.start, func(t *testing.T) {
			m := &model{
				engineFilter: tt.start,
				table:        table.New(table.WithColumns(nil)),
			}
			m.cycleEngineFilter()
			if m.engineFilter != tt.want {
				t.Errorf("cycleEngineFilter(%q) = %q, want %q", tt.start, m.engineFilter, tt.want)
			}
		})
	}
}

func TestCycleEngineFilter_Unknown(t *testing.T) {
	t.Parallel()
	m := &model{
		engineFilter: "nonexistent",
		table:        table.New(table.WithColumns(nil)),
	}
	m.cycleEngineFilter()
	if m.engineFilter != "" {
		t.Errorf("expected reset to empty string, got %q", m.engineFilter)
	}
}

// ---------------------------------------------------------------------------
// cycleStatusFilter
// ---------------------------------------------------------------------------

func TestCycleStatusFilter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		start string
		want  string
	}{
		{"", "active"},
		{"active", "completed"},
		{"completed", "abandoned"},
		{"abandoned", "on_hold"},
		{"on_hold", "unknown"},
		{"unknown", ""},
	}
	for _, tt := range tests {
		t.Run(tt.start, func(t *testing.T) {
			m := &model{
				statusFilter: tt.start,
				table:        table.New(table.WithColumns(nil)),
			}
			m.cycleStatusFilter()
			if m.statusFilter != tt.want {
				t.Errorf("cycleStatusFilter(%q) = %q, want %q", tt.start, m.statusFilter, tt.want)
			}
		})
	}
}

func TestCycleStatusFilter_Unknown(t *testing.T) {
	t.Parallel()
	m := &model{
		statusFilter: "nonexistent",
		table:        table.New(table.WithColumns(nil)),
	}
	m.cycleStatusFilter()
	if m.statusFilter != "" {
		t.Errorf("expected reset to empty string, got %q", m.statusFilter)
	}
}
