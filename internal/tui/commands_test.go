package tui

import (
	"testing"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/downloader"
)

// ---------------------------------------------------------------------------
// isOnlineOnlyLink
// ---------------------------------------------------------------------------

func TestIsOnlineOnlyLink_TUI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		url  string
		want bool
	}{
		// Online-only
		{"ONLINE text", "ONLINE", "", true},
		{"Online text", "Online", "", true},
		{"online mixed case", "OnLiNe", "", true},
		{"gamejolt URL", "", "https://gamejolt.com/games/test/12345", true},
		{"gamejolt in text", "GameJolt link", "", true},
		{"online in URL", "", "https://example.com/online", true},
		{"Online multiplayer", "Online multiplayer version", "", true},

		// NOT online-only
		{"BUZZHEAVIER text", "BUZZHEAVIER", "", false},
		{"Mega text", "Mega", "", false},
		{"empty text and url", "", "", false},
		{"normal download link", "Download Link", "https://example.com/file.zip", false},
		{"pixeldrain link", "Pixeldrain", "https://pixeldrain.com/u/abc123", false},
		{"regular host", "MEGA", "https://mega.nz/file/abc123", false},
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

func TestIsOnlineOnlyLink_EdgeCases_TUI(t *testing.T) {
	t.Parallel()
	if !isOnlineOnlyLink("play online now", "") {
		t.Error("expected 'play online now' to be online-only")
	}
	if isOnlineOnlyLink("on the go", "") {
		t.Error("expected 'on the go' NOT to be online-only")
	}
	if isOnlineOnlyLink("bone", "") {
		t.Error("expected 'bone' NOT to be online-only")
	}
}

// ---------------------------------------------------------------------------
// sortLinksByPlatform
// ---------------------------------------------------------------------------

func TestSortLinksByPlatform_NilOrEmpty_TUI(t *testing.T) {
	t.Parallel()
	got := sortLinksByPlatform(nil, downloader.PlatformLinux)
	if len(got) != 0 {
		t.Errorf("expected empty for nil input, got %+v", got)
	}

	got = sortLinksByPlatform([]db.DownloadLink{}, downloader.PlatformLinux)
	if len(got) != 0 {
		t.Errorf("expected empty for empty input, got %+v", got)
	}
}

func TestSortLinksByPlatform_NativePlatform_TUI(t *testing.T) {
	t.Parallel()
	links := []db.DownloadLink{
		{Name: "Linux version", Platform: db.PlatformLinux, Host: "pixeldrain"},
		{Name: "Windows version", Platform: db.PlatformWindows, Host: "mega"},
	}
	got := sortLinksByPlatform(links, downloader.PlatformLinux)
	if len(got) == 0 {
		t.Fatal("expected non-empty result")
	}
	if got[0].Name != "Linux version" {
		t.Errorf("expected 'Linux version' first, got %q", got[0].Name)
	}
}

func TestSortLinksByPlatform_WindowsViaWine_TUI(t *testing.T) {
	t.Parallel()
	links := []db.DownloadLink{
		{Name: "Windows version", Platform: db.PlatformWindows, Host: "mega"},     // 70-200=-130
		{Name: "All platforms", Platform: db.PlatformAll, Host: "mediafire"},      // 50+8=58
	}
	got := sortLinksByPlatform(links, downloader.PlatformLinux)
	if len(got) == 0 {
		t.Fatal("expected non-empty result")
	}
	if got[0].Name != "All platforms" {
		t.Errorf("expected 'All platforms' first (Mega -200 penalty), got %q", got[0].Name)
	}
}

func TestSortLinksByPlatform_UnknownLowestOnLinux_TUI(t *testing.T) {
	t.Parallel()
	links := []db.DownloadLink{
		{Name: "Windows version", Platform: db.PlatformWindows, Host: "mega"},      // 70-200=-130
		{Name: "Unknown platform", Platform: db.PlatformUnknown, Host: "unknown"},  // 25+0=25
	}
	got := sortLinksByPlatform(links, downloader.PlatformLinux)
	if len(got) == 0 {
		t.Fatal("expected non-empty result")
	}
	if got[0].Name != "Unknown platform" {
		t.Errorf("expected 'Unknown platform' first (Mega -200 penalty), got %q", got[0].Name)
	}
}

func TestSortLinksByPlatform_SkipsOnlineOnly_TUI(t *testing.T) {
	t.Parallel()
	links := []db.DownloadLink{
		{Name: "Online Version", URL: "", Platform: db.PlatformUnknown, Host: "pixeldrain"},
		{Name: "Linux Download", URL: "", Platform: db.PlatformLinux, Host: "mega"},
	}
	got := sortLinksByPlatform(links, downloader.PlatformLinux)
	if len(got) == 0 {
		t.Fatal("expected non-empty result")
	}
	if got[0].Name != "Linux Download" {
		t.Errorf("expected 'Linux Download' first (online-only skipped), got %q", got[0].Name)
	}
}

func TestSortLinksByPlatform_AllOnlineOnly_TUI(t *testing.T) {
	t.Parallel()
	links := []db.DownloadLink{
		{Name: "Online Version", URL: "https://gamejolt.com/test", Platform: db.PlatformAll, Host: "unknown"},
		{Name: "Browser Play", URL: "https://example.com/online", Platform: db.PlatformAll, Host: "unknown"},
	}
	got := sortLinksByPlatform(links, downloader.PlatformLinux)
	if len(got) != 0 {
		t.Errorf("expected empty when all links are online-only, got %+v", got)
	}
}

func TestSortLinksByPlatform_HostBonus_TUI(t *testing.T) {
	t.Parallel()
	links := []db.DownloadLink{
		{Name: "With mega", Platform: db.PlatformUnknown, Host: "mega"},        // 25-200=-175
		{Name: "With unknown", Platform: db.PlatformUnknown, Host: "randomhost"}, // 25+0=25
	}
	got := sortLinksByPlatform(links, downloader.PlatformLinux)
	if len(got) == 0 {
		t.Fatal("expected non-empty result")
	}
	if got[0].Name != "With unknown" {
		t.Errorf("expected 'With unknown' first (Mega -200 penalty), got %q", got[0].Name)
	}
}

func TestSortLinksByPlatform_AllBonusesBeatNoBonus_TUI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		host   string
		expect string
	}{
		{"vikingfile (+15)", "vikingfile", "bonus link"},
		{"buzzheavier (+15)", "buzzheavier", "bonus link"},
		{"pixeldrain (+15)", "pixeldrain", "bonus link"},
		{"mega (-200 penalty)", "mega", "no bonus link"},
		{"gofile (+15)", "gofile", "bonus link"},
		{"mediafire (+8)", "mediafire", "bonus link"},
		{"workupload (+8)", "workupload", "bonus link"},
		{"krakenfiles (+5)", "krakenfiles", "bonus link"},
		{"googledrive (+5)", "googledrive", "bonus link"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			links := []db.DownloadLink{
				{Name: "bonus link", Platform: db.PlatformUnknown, Host: tt.host},
				{Name: "no bonus link", Platform: db.PlatformUnknown, Host: "rando"},
			}
			got := sortLinksByPlatform(links, downloader.PlatformLinux)
			if len(got) == 0 {
				t.Fatal("expected non-empty result")
			}
			if got[0].Name != tt.expect {
				t.Errorf("expected %q to be first, got %q", tt.expect, got[0].Name)
			}
		})
	}
}

func TestSortLinksByPlatform_PriorityOrdering_TUI(t *testing.T) {
	t.Parallel()
	links := []db.DownloadLink{
		{Name: "Windows only", Platform: db.PlatformWindows, Host: "mega"},
		{Name: "All platforms", Platform: db.PlatformAll, Host: "catbox"},
		{Name: "Unknown plat", Platform: db.PlatformUnknown, Host: "random"},
		{Name: "Linux native", Platform: db.PlatformLinux, Host: "pixeldrain"},
	}
	got := sortLinksByPlatform(links, downloader.PlatformLinux)
	if len(got) == 0 {
		t.Fatal("expected non-empty result")
	}
	if got[0].Name != "Linux native" {
		t.Errorf("expected 'Linux native' first (best match), got %q", got[0].Name)
	}
}

func TestSortLinksByPlatform_HostBonusWithinPlatform_TUI(t *testing.T) {
	t.Parallel()
	links := []db.DownloadLink{
		{Name: "Linux on catbox", Platform: db.PlatformLinux, Host: "catbox"},
		{Name: "Linux on buzzheavier", Platform: db.PlatformLinux, Host: "buzzheavier"},
	}
	got := sortLinksByPlatform(links, downloader.PlatformLinux)
	if len(got) == 0 {
		t.Fatal("expected non-empty result")
	}
	if got[0].Name != "Linux on buzzheavier" {
		t.Errorf("expected 'Linux on buzzheavier' first (+15 host bonus), got %q", got[0].Name)
	}
}

func TestSortLinksByPlatform_ReturnsAllNonOnline_TUI(t *testing.T) {
	t.Parallel()
	links := []db.DownloadLink{
		{Name: "Online", URL: "https://gamejolt.com/test", Platform: db.PlatformAll, Host: "unknown"},
		{Name: "Linux mega", Platform: db.PlatformLinux, Host: "mega"},
		{Name: "Linux pixeldrain", Platform: db.PlatformLinux, Host: "pixeldrain"},
	}
	got := sortLinksByPlatform(links, downloader.PlatformLinux)
	if len(got) != 2 {
		t.Errorf("expected 2 results (online-only skipped), got %d", len(got))
	}
}

func TestSortLinksByPlatform_BestOfMultiple_TUI(t *testing.T) {
	t.Parallel()
	links := []db.DownloadLink{
		{Name: "Online", URL: "https://gamejolt.com/test", Platform: db.PlatformAll, Host: "unknown"},
		{Name: "Windows exe", Platform: db.PlatformWindows, Host: "mega"},
		{Name: "All on gofile", Platform: db.PlatformAll, Host: "gofile"},
		{Name: "Linux tar.gz", Platform: db.PlatformLinux, Host: "pixeldrain"},
		{Name: "All on unknown", Platform: db.PlatformAll, Host: "unknown"},
	}
	got := sortLinksByPlatform(links, downloader.PlatformLinux)
	if len(got) == 0 {
		t.Fatal("expected non-empty result")
	}
	// Linux native (100) + pixeldrain (15) = 115
	if got[0].Name != "Linux tar.gz" {
		t.Errorf("expected 'Linux tar.gz' first (score 115), got %q", got[0].Name)
	}
}
