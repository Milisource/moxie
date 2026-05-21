package commands

import (
	"testing"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/downloader"
)

// ---------------------------------------------------------------------------
// isOnlineOnlyLink
// ---------------------------------------------------------------------------

func TestIsOnlineOnlyLink_Commands(t *testing.T) {
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

func TestIsOnlineOnlyLink_EdgeCases_Commands(t *testing.T) {
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
// formatSpeed
// ---------------------------------------------------------------------------

func TestFormatSpeed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		bps  float64
		want string
	}{
		{0, "0 B/s"},
		{1, "1 B/s"},
		{512, "512 B/s"},
		{1023, "1023 B/s"},
		{1024, "1.0 KB/s"},
		{1536, "1.5 KB/s"},
		{1024 * 1024, "1.0 MB/s"},
		{5 * 1024 * 1024, "5.0 MB/s"},
		{10.5 * 1024 * 1024, "10.5 MB/s"},
		{999, "999 B/s"},
		{10240, "10.0 KB/s"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatSpeed(tt.bps)
			if got != tt.want {
				t.Errorf("formatSpeed(%f) = %q, want %q", tt.bps, got, tt.want)
			}
		})
	}
}

func TestFormatSpeed_Negative(t *testing.T) {
	t.Parallel()
	got := formatSpeed(-100)
	if got != "-100 B/s" {
		t.Errorf("formatSpeed(-100) = %q, want %q", got, "-100 B/s")
	}
}

// ---------------------------------------------------------------------------
// formatBytes
// ---------------------------------------------------------------------------

func TestFormatBytes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		b    int64
		want string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{10 * 1024 * 1024, "10.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
		{2 * 1024 * 1024 * 1024, "2.0 GB"},
		{1500 * 1024 * 1024, "1.5 GB"}, // rounds up to nearest GB
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatBytes(tt.b)
			if got != tt.want {
				t.Errorf("formatBytes(%d) = %q, want %q", tt.b, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// selectBestLinkByPlatform
// ---------------------------------------------------------------------------

func TestSelectBestLinkByPlatform_NilOrEmpty(t *testing.T) {
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

func TestSelectBestLinkByPlatform_NativePlatform(t *testing.T) {
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

func TestSelectBestLinkByPlatform_WindowsViaWine(t *testing.T) {
	t.Parallel()
	links := []db.DownloadLink{
		{Name: "All platforms", Platform: db.PlatformAll, Host: "mediafire"},
		{Name: "Windows version", Platform: db.PlatformWindows, Host: "mega"},
	}
	got := selectBestLinkByPlatform(links, downloader.PlatformLinux)
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if got.Name != "All platforms" {
		t.Errorf("expected 'All platforms' first (Mega -200 penalty), got %q", got.Name)
	}
}

func TestSelectBestLinkByPlatform_UnknownLowestOnLinux(t *testing.T) {
	t.Parallel()
	links := []db.DownloadLink{
		{Name: "Unknown platform", Platform: db.PlatformUnknown, Host: "unknown"},
		{Name: "Windows version", Platform: db.PlatformWindows, Host: "mega"},
	}
	got := selectBestLinkByPlatform(links, downloader.PlatformLinux)
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if got.Name != "Unknown platform" {
		t.Errorf("expected 'Unknown platform' first (Mega -200 penalty), got %q", got.Name)
	}
}

func TestSelectBestLinkByPlatform_HostBonus(t *testing.T) {
	t.Parallel()
	links := []db.DownloadLink{
		{Name: "With mega", Platform: db.PlatformUnknown, Host: "mega"},
		{Name: "With unknown", Platform: db.PlatformUnknown, Host: "randomhost"},
	}
	got := selectBestLinkByPlatform(links, downloader.PlatformLinux)
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if got.Name != "With unknown" {
		t.Errorf("expected 'With unknown' first (Mega -200 penalty), got %q", got.Name)
	}
}

func TestSelectBestLinkByPlatform_HostBonuses(t *testing.T) {
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

func TestSelectBestLinkByPlatform_SkipsOnlineOnly(t *testing.T) {
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

func TestSelectBestLinkByPlatform_AllOnlineOnly(t *testing.T) {
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

func TestSelectBestLinkByPlatform_PriorityOrdering(t *testing.T) {
	t.Parallel()
	// Native > All > Unknown > Different
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
		t.Errorf("expected 'Linux native' (best platform match + host bonus), got %q", got.Name)
	}
}

func TestSelectBestLinkByPlatform_HostBonusWithinPlatform(t *testing.T) {
	t.Parallel()
	// Both are PlatformLinux; catbox and buzzheavier both +25 → tie → first input wins
	links := []db.DownloadLink{
		{Name: "Linux on catbox", Platform: db.PlatformLinux, Host: "catbox"},
		{Name: "Linux on buzzheavier", Platform: db.PlatformLinux, Host: "buzzheavier"},
	}
	got := selectBestLinkByPlatform(links, downloader.PlatformLinux)
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if got.Name != "Linux on catbox" {
		t.Errorf("expected 'Linux on catbox' (tie, first wins), got %q", got.Name)
	}
}

func TestSelectBestLinkByPlatform_AllWithHostBonus(t *testing.T) {
	t.Parallel()
	// PlatformAll with pixeldrain bonus should beat PlatformUnknown with no bonus
	links := []db.DownloadLink{
		{Name: "All on pixeldrain", Platform: db.PlatformAll, Host: "pixeldrain"},
		{Name: "Unknown on random", Platform: db.PlatformUnknown, Host: "random"},
	}
	got := selectBestLinkByPlatform(links, downloader.PlatformLinux)
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if got.Name != "All on pixeldrain" {
		t.Errorf("expected 'All on pixeldrain' (50+15 > 25+0), got %q", got.Name)
	}
}
