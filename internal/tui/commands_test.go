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
			got := downloader.IsOnlineOnly(tt.text, tt.url)
			if got != tt.want {
				t.Errorf("downloader.IsOnlineOnly(%q, %q) = %v, want %v", tt.text, tt.url, got, tt.want)
			}
		})
	}
}

func TestIsOnlineOnlyLink_EdgeCases_TUI(t *testing.T) {
	t.Parallel()
	if !downloader.IsOnlineOnly("play online now", "") {
		t.Error("expected 'play online now' to be online-only")
	}
	if downloader.IsOnlineOnly("on the go", "") {
		t.Error("expected 'on the go' NOT to be online-only")
	}
	if downloader.IsOnlineOnly("bone", "") {
		t.Error("expected 'bone' NOT to be online-only")
	}
}

// ---------------------------------------------------------------------------
// sortLinksByPlatform
// ---------------------------------------------------------------------------

func TestSortLinksByPlatform_NilOrEmpty_TUI(t *testing.T) {
	t.Parallel()
	got := sortLinksByPlatform(nil, downloader.PlatformLinux)
	if got != nil {
		t.Errorf("expected nil for nil input, got %+v", got)
	}

	got = sortLinksByPlatform([]db.DownloadLink{}, downloader.PlatformLinux)
	if got != nil {
		t.Errorf("expected nil for empty input, got %+v", got)
	}
}

func TestSortLinksByPlatform_NativePlatform_TUI(t *testing.T) {
	t.Parallel()
	links := []db.DownloadLink{
		{Name: "Linux version", Platform: db.PlatformLinux, Host: "pixeldrain"},
		{Name: "Windows version", Platform: db.PlatformWindows, Host: "mega"},
	}
	got := sortLinksByPlatform(links, downloader.PlatformLinux)
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if got[0].Name != "Linux version" {
		t.Errorf("expected 'Linux version' first, got %q", got[0].Name)
	}
}

func TestSortLinksByPlatform_WindowsViaWine_TUI(t *testing.T) {
	t.Parallel()
	links := []db.DownloadLink{
		{Name: "All platforms", Platform: db.PlatformAll, Host: "mediafire"},
		{Name: "Windows version", Platform: db.PlatformWindows, Host: "mega"},
	}
	got := sortLinksByPlatform(links, downloader.PlatformLinux)
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if got[0].Name != "All platforms" {
		t.Errorf("expected 'All platforms' first (Mega -200 penalty), got %q", got[0].Name)
	}
}

func TestSortLinksByPlatform_UnknownLowestOnLinux_TUI(t *testing.T) {
	t.Parallel()
	links := []db.DownloadLink{
		{Name: "Unknown platform", Platform: db.PlatformUnknown, Host: "unknown"},
		{Name: "Windows version", Platform: db.PlatformWindows, Host: "mega"},
	}
	got := sortLinksByPlatform(links, downloader.PlatformLinux)
	if got == nil {
		t.Fatal("expected non-nil result")
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
	if got == nil {
		t.Fatal("expected non-nil result")
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
	if got != nil {
		t.Errorf("expected nil when all links are online-only, got %+v", got)
	}
}

func TestSortLinksByPlatform_HostBonus_TUI(t *testing.T) {
	t.Parallel()
	links := []db.DownloadLink{
		{Name: "With mega", Platform: db.PlatformUnknown, Host: "mega"},
		{Name: "With unknown", Platform: db.PlatformUnknown, Host: "randomhost"},
	}
	got := sortLinksByPlatform(links, downloader.PlatformLinux)
	if got == nil {
		t.Fatal("expected non-nil result")
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
		{"vikingfile loses (-200)", "vikingfile", "no bonus link"},
		{"buzzheavier wins (+25)", "buzzheavier", "bonus link"},
		{"pixeldrain wins (+25)", "pixeldrain", "bonus link"},
		{"mega loses (-200)", "mega", "no bonus link"},
		{"gofile wins (+25)", "gofile", "bonus link"},
		{"mediafire (tie, stable sort)", "mediafire", "bonus link"},
		{"workupload loses (-200)", "workupload", "no bonus link"},
		{"krakenfiles loses (-200)", "krakenfiles", "no bonus link"},
		{"googledrive (tie, stable sort)", "googledrive", "bonus link"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			links := []db.DownloadLink{
				{Name: "bonus link", Platform: db.PlatformUnknown, Host: tt.host},
				{Name: "no bonus link", Platform: db.PlatformUnknown, Host: "rando"},
			}
			got := sortLinksByPlatform(links, downloader.PlatformLinux)
			if got == nil {
				t.Fatal("expected non-nil result")
			}
			if got[0].Name != tt.expect {
				t.Errorf("expected %q to win, got %q", tt.expect, got[0].Name)
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
	if got == nil {
		t.Fatal("expected non-nil result")
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
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if got[0].Name != "Linux on catbox" {
		t.Errorf("expected 'Linux on catbox' (tie, stable sort), got %q", got[0].Name)
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
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if got[0].Name != "Linux tar.gz" {
		t.Errorf("expected 'Linux tar.gz' first (best score), got %q", got[0].Name)
	}
}
