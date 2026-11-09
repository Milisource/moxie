package scraper

import (
	"testing"

	"github.com/mili/moxie/internal/db"
)

func TestThreadTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data *ThreadData
		want string
	}{
		{
			name: "parsed title is already stripped",
			data: &ThreadData{Title: "Noxian Nights", Prefixes: []string{"RPGM", "Completed"}},
			want: "Noxian Nights",
		},
		{
			name: "legitimate engine word at the start is preserved",
			data: &ThreadData{Title: "Unity of Command", Prefixes: []string{"Unity"}},
			want: "Unity of Command",
		},
		{
			name: "legacy prefixed title is stripped when no labels were parsed",
			data: &ThreadData{Title: "RPGM Completed Noxian Nights"},
			want: "Noxian Nights",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ThreadTitle(tt.data); got != tt.want {
				t.Errorf("ThreadTitle() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestApplyThreadDataPreservesTitleStartingWithEngineWord(t *testing.T) {
	t.Parallel()

	game := &db.Game{Title: "old", Engine: "Unknown"}
	ApplyThreadData(game, &ThreadData{
		Title:    "Unity of Command",
		Prefixes: []string{"Unity", "Completed"},
	}, "https://f95zone.to/threads/1/")

	if game.Title != "Unity of Command" {
		t.Errorf("title = %q, want %q", game.Title, "Unity of Command")
	}
}

func TestThreadEngine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data *ThreadData
		want string
	}{
		{
			name: "prefix label wins",
			data: &ThreadData{Title: "FurrHouse", Prefixes: []string{"VN", "Ren'Py"}},
			want: "RenPy",
		},
		{
			name: "multi-word prefix label",
			data: &ThreadData{Title: "Some Game", Prefixes: []string{"Wolf RPG"}},
			want: "WolfRPG",
		},
		{
			name: "title fallback when no engine label",
			data: &ThreadData{Title: "Unity Some Game", Prefixes: []string{"VN"}},
			want: "Unity",
		},
		{
			name: "no engine signal",
			data: &ThreadData{Title: "Some Game", Prefixes: []string{"VN"}},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ThreadEngine(tt.data); got != tt.want {
				t.Errorf("ThreadEngine() = %q, want %q", got, tt.want)
			}
		})
	}
}
