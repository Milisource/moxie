package scraper

import "testing"

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
