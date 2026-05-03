package scraper

import "testing"

func TestIsNonGameThread(t *testing.T) {
	t.Parallel()
	tests := []struct {
		title string
		want  bool
	}{
		// Non-game threads
		{"Request Some Game Title", true},
		{"REQ Some Game", true},
		{"Recommendation for games like XYZ", true},
		{"Recommending strategy RPGs", true},
		{"Identify this game", true},
		{"Identifying a game from screenshot", true},
		{"Identification help", true},
		{"Discussion about game mechanics", true},
		{"Question about modding", true},
		{"help with installation", true},
		{"Tutorial how to mod RPG Maker", true},
		{"Guide to Unity development", true},
		{"Looking for games like Summertime Saga", true},
		{"Searching for a specific VN", true},
		{"Translation Request Some Game", true},
		{"translation help needed", true},

		// Non-game threads with non-breaking space (U+00A0)
		{"REQ\u00a0Kunoichi Sekiren", true},
		{"Seeking\u00a0Games like something", true},
		{"Seeking\u00a0PREGNANCY GAMES", true},

		// Game release threads (should NOT be filtered)
		{"Unity Completed My Game [v1.0]", false},
		{"RPGM Abandoned Game Title", false},
		{"Ren'Py Completed Visual Novel", false},
		{"HTML Game Name", false},
		{"Flash My Game", false},
		{"Completed Game Without Engine Prefix", false},
		{"", false},
		{"AnyPort in a Storm", false}, // "Any" at start but it's part of a game name
	}
	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			got := IsNonGameThread(tt.title)
			if got != tt.want {
				t.Errorf("IsNonGameThread(%q) = %v, want %v", tt.title, got, tt.want)
			}
		})
	}
}
