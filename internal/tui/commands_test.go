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
// selectBestLinkByPlatform
// ---------------------------------------------------------------------------

func TestSelectBestLinkByPlatform_NilOrEmpty_TUI(t *testing.T) {
	t.Parallel()
	got := selectBestLinkByPlatform(nil, downloader.PlatformLinux)
	if got != nil {
		t.Errorf("expected nil for nil input, got %+v", got)
	}

	got = selectBestLinkByPlatform([]db.DownloadLink{}, downloader.PlatformLinux)
	if got != nil {
		t.Errorf("expected nil for empty input, got %+v", got)
	}
}

func TestSelectBestLinkByPlatform_NativePlatform_TUI(t *testing.T) {
	t.Parallel()
	links := []db.DownloadLink{
		{Name: "Linux version", Platform: db.PlatformLinux, Host: "pixeldrain"},
		{Name: "Windows version", Platform: db.PlatformWindows, Host: "mega"},
	}
	got := selectBestLinkByPlatform(links, downloader.PlatformLinux)
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if got.Name != "Linux version" {
		t.Errorf("expected 'Linux version', got %q", got.Name)
	}
}

func TestSelectBestLinkByPlatform_WindowsViaWine_TUI(t *testing.T) {
	t.Parallel()
	// On Linux, Windows via Wine/Proton (70) beats All (50)
	links := []db.DownloadLink{
		{Name: "All platforms", Platform: db.PlatformAll, Host: "mediafire"},    // 50+8=58
		{Name: "Windows version", Platform: db.PlatformWindows, Host: "mega"},   // 70+15=85
	}
	got := selectBestLinkByPlatform(links, downloader.PlatformLinux)
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if got.Name != "Windows version" {
		t.Errorf("expected 'Windows version' (Wine: 70+15 > All 50+8), got %q", got.Name)
	}
}

func TestSelectBestLinkByPlatform_UnknownLowestOnLinux_TUI(t *testing.T) {
	t.Parallel()
	// On Linux: Windows (70) > All (50) > Unknown (25)
	links := []db.DownloadLink{
		{Name: "Unknown platform", Platform: db.PlatformUnknown, Host: "unknown"}, // 25+0=25
		{Name: "Windows version", Platform: db.PlatformWindows, Host: "mega"},     // 70+15=85
	}
	got := selectBestLinkByPlatform(links, downloader.PlatformLinux)
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if got.Name != "Windows version" {
		t.Errorf("expected 'Windows version' (Wine: 70+15 > Unknown 25+0), got %q", got.Name)
	}
}

func TestSelectBestLinkByPlatform_SkipsOnlineOnly_TUI(t *testing.T) {
	t.Parallel()
	links := []db.DownloadLink{
		{Name: "Online Version", URL: "", Platform: db.PlatformUnknown, Host: "pixeldrain"},
		{Name: "Linux Download", URL: "", Platform: db.PlatformLinux, Host: "mega"},
	}
	got := selectBestLinkByPlatform(links, downloader.PlatformLinux)
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if got.Name != "Linux Download" {
		t.Errorf("expected 'Linux Download' (online-only skipped), got %q", got.Name)
	}
}

func TestSelectBestLinkByPlatform_AllOnlineOnly_TUI(t *testing.T) {
	t.Parallel()
	links := []db.DownloadLink{
		{Name: "Online Version", URL: "https://gamejolt.com/test", Platform: db.PlatformAll, Host: "unknown"},
		{Name: "Browser Play", URL: "https://example.com/online", Platform: db.PlatformAll, Host: "unknown"},
	}
	got := selectBestLinkByPlatform(links, downloader.PlatformLinux)
	if got != nil {
		t.Errorf("expected nil when all links are online-only, got %+v", got)
	}
}

func TestSelectBestLinkByPlatform_HostBonus_TUI(t *testing.T) {
	t.Parallel()
	// Both unknown platform, but one has a bonus host
	links := []db.DownloadLink{
		{Name: "With mega", Platform: db.PlatformUnknown, Host: "mega"},
		{Name: "With unknown", Platform: db.PlatformUnknown, Host: "randomhost"},
	}
	got := selectBestLinkByPlatform(links, downloader.PlatformLinux)
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if got.Name != "With mega" {
		t.Errorf("expected 'With mega' (host bonus), got %q", got.Name)
	}
}

func TestSelectBestLinkByPlatform_AllBonusesBeatNoBonus_TUI(t *testing.T) {
	t.Parallel()
	// All bonus hosts beat no-bonus host at the same platform level
	tests := []struct {
		name   string
		host   string
		expect string // the link that should win
	}{
		{"vikingfile (+15)", "vikingfile", "bonus link"},
		{"buzzheavier (+15)", "buzzheavier", "bonus link"},
		{"pixeldrain (+15)", "pixeldrain", "bonus link"},
		{"mega (+15)", "mega", "bonus link"},
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
			got := selectBestLinkByPlatform(links, downloader.PlatformLinux)
			if got == nil {
				t.Fatal("expected non-nil result")
			}
			if got.Name != tt.expect {
				t.Errorf("expected %q to win, got %q", tt.expect, got.Name)
			}
		})
	}
}

func TestSelectBestLinkByPlatform_PriorityOrdering_TUI(t *testing.T) {
	t.Parallel()
	links := []db.DownloadLink{
		{Name: "Windows only", Platform: db.PlatformWindows, Host: "mega"},
		{Name: "All platforms", Platform: db.PlatformAll, Host: "catbox"},
		{Name: "Unknown plat", Platform: db.PlatformUnknown, Host: "random"},
		{Name: "Linux native", Platform: db.PlatformLinux, Host: "pixeldrain"},
	}
	got := selectBestLinkByPlatform(links, downloader.PlatformLinux)
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if got.Name != "Linux native" {
		t.Errorf("expected 'Linux native' (best match), got %q", got.Name)
	}
}

func TestSelectBestLinkByPlatform_HostBonusWithinPlatform_TUI(t *testing.T) {
	t.Parallel()
	links := []db.DownloadLink{
		{Name: "Linux on catbox", Platform: db.PlatformLinux, Host: "catbox"},
		{Name: "Linux on buzzheavier", Platform: db.PlatformLinux, Host: "buzzheavier"},
	}
	got := selectBestLinkByPlatform(links, downloader.PlatformLinux)
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if got.Name != "Linux on buzzheavier" {
		t.Errorf("expected 'Linux on buzzheavier' (+15 host bonus), got %q", got.Name)
	}
}

func TestSelectBestLinkByPlatform_BestOfMultiple_TUI(t *testing.T) {
	t.Parallel()
	links := []db.DownloadLink{
		{Name: "Online", URL: "https://gamejolt.com/test", Platform: db.PlatformAll, Host: "unknown"},
		{Name: "Windows exe", Platform: db.PlatformWindows, Host: "mega"},
		{Name: "All on gofile", Platform: db.PlatformAll, Host: "gofile"},
		{Name: "Linux tar.gz", Platform: db.PlatformLinux, Host: "pixeldrain"},
		{Name: "All on unknown", Platform: db.PlatformAll, Host: "unknown"},
	}
	got := selectBestLinkByPlatform(links, downloader.PlatformLinux)
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	// Linux native (100) + pixeldrain (15) = 115
	if got.Name != "Linux tar.gz" {
		t.Errorf("expected 'Linux tar.gz' (score 115), got %q", got.Name)
	}
}
