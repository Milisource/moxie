package scraper

import (
	"testing"
)

// ---------------------------------------------------------------------------
// isOnlineOnlyLink
// ---------------------------------------------------------------------------

func TestIsOnlineOnlyLink(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		url  string
		want bool
	}{
		// Should be detected as online-only
		{"ONLINE text", "ONLINE", "", true},
		{"Online text lowercase", "Online", "", true},
		{"online mixed case", "OnLiNe", "", true},
		{"gamejolt URL", "", "https://gamejolt.com/games/test/12345", true},
		{"gamejolt in text", "GameJolt link", "", true},
		{"online in URL", "", "https://example.com/online-play", true},
		{"Online multiplayer", "Online multiplayer version", "", true},

		// Should NOT be detected as online-only
		{"BUZZHEAVIER text", "BUZZHEAVIER", "", false},
		{"Mega text", "Mega", "", false},
		{"empty text and url", "", "", false},
		{"normal download link", "Download Link", "https://example.com/file.zip", false},
		{"pixeldrain link", "Pixeldrain", "https://pixeldrain.com/u/abc123", false},
		{"regular host", "MEGA", "https://mega.nz/file/abc123", false},
		{"numbers and symbols", "v1.0 [Linux]", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isOnlineOnlyLink(tt.text, tt.url)
			if got != tt.want {
				t.Errorf("isOnlineOnlyLink(%q, %q) = %v, want %v", tt.text, tt.url, got, tt.want)
			}
		})
	}
}

func TestIsOnlineOnlyLink_EdgeCases(t *testing.T) {
	t.Parallel()
	// "online" substring check - ensure it's not doing exact match
	if !isOnlineOnlyLink("play online now", "") {
		t.Error("expected 'play online now' to be online-only")
	}
	// "Online" at start
	if !isOnlineOnlyLink("Online Version", "") {
		t.Error("expected 'Online Version' to be online-only")
	}
	// "ONLINE" at end
	if !isOnlineOnlyLink("PLAY ONLINE", "") {
		t.Error("expected 'PLAY ONLINE' to be online-only")
	}
	// Make sure "online" must be present, not just partial match on "on"
	if isOnlineOnlyLink("on the go", "") {
		t.Error("expected 'on the go' NOT to be online-only")
	}
	if isOnlineOnlyLink("bone", "") {
		t.Error("expected 'bone' NOT to be online-only")
	}
}
