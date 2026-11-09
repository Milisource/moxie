package tui

import (
	"archive/zip"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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
// Download flow: double-start guard and reservation lifecycle
// ---------------------------------------------------------------------------

func TestHandleDownloadKey_DoubleInvokeStartsOneDownload(t *testing.T) {
	f, err := os.CreateTemp("", "tui-download-*.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	f.Close()
	database, err := db.Open(path)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() {
		database.Close()
		os.Remove(path)
	})

	id, err := database.InsertGame(&db.Game{
		Title:  "Test Game",
		Path:   filepath.Join(t.TempDir(), "game"),
		Engine: "Unknown",
	})
	if err != nil {
		t.Fatalf("InsertGame: %v", err)
	}

	m := model{
		db:                database,
		selectedID:        id,
		activeDownloadsMu: &sync.Mutex{},
		activeDownloads:   make(map[int64]*activeDownload),
	}

	// First keypress: must reserve a Pending entry synchronously and
	// return a resolveLinksCmd.
	m1, cmd1 := m.handleDownloadKey()
	if cmd1 == nil {
		t.Fatal("expected a resolveLinksCmd from the first keypress")
	}
	updated := m1.(model)

	updated.activeDownloadsMu.Lock()
	ad, ok := updated.activeDownloads[id]
	updated.activeDownloadsMu.Unlock()
	if !ok {
		t.Fatal("expected a Pending reservation immediately after the first keypress")
	}
	ad.mu.Lock()
	status := ad.status
	ad.mu.Unlock()
	if status != db.DownloadStatusPending {
		t.Errorf("expected Pending reservation, got %q", status)
	}
	if !updated.hasActiveDownloads() {
		t.Error("expected the Pending reservation to count as an active download")
	}

	// Second keypress while resolution is still in flight: must be
	// rejected before any async work is started.
	m2, cmd2 := updated.handleDownloadKey()
	if cmd2 != nil {
		t.Fatal("second keypress must not start another link resolution")
	}
	if m2.(model).err == nil {
		t.Error("expected 'already in progress' error on second keypress")
	}

	// Exactly one entry in the map — the reservation.
	updated.activeDownloadsMu.Lock()
	count := len(updated.activeDownloads)
	updated.activeDownloadsMu.Unlock()
	if count != 1 {
		t.Errorf("expected exactly 1 activeDownloads entry, got %d", count)
	}

	// Run the resolveLinksCmd: no links in the DB and no scraper client,
	// so resolution fails.
	msg := cmd1()
	linksMsg, ok := msg.(downloadLinksMsg)
	if !ok {
		t.Fatalf("expected downloadLinksMsg, got %T", msg)
	}
	if linksMsg.err == nil {
		t.Fatal("expected link resolution to fail (no links, no scraper)")
	}

	// Feeding the failure back through Update must release the
	// reservation so the game can be retried.
	m3, _ := updated.Update(linksMsg)
	released := m3.(model)
	released.activeDownloadsMu.Lock()
	_, stillReserved := released.activeDownloads[id]
	released.activeDownloadsMu.Unlock()
	if stillReserved {
		t.Error("expected reservation to be released after failed link resolution")
	}
	if released.hasActiveDownloads() {
		t.Error("expected no active downloads after failed link resolution")
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
		{"workupload wins (+10)", "workupload", "bonus link"},
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

// ---------------------------------------------------------------------------
// Browser fallback: all download links failed → open in real browser +
// download-dir watcher pickup
// ---------------------------------------------------------------------------

// makeTestZip creates a zip archive at path whose payload exceeds the
// 4096-byte minimum IsValidGameFile enforces.
func makeTestZip(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("data.txt")
	if err != nil {
		t.Fatal(err)
	}
	// Random payload: 8192 sequential bytes would still deflate to ~470B,
	// tripping downloader.IsValidGameFile's 4096-byte minimum.
	rng := rand.New(rand.NewSource(42))
	buf := make([]byte, 8192)
	if _, err := rng.Read(buf); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(buf); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// runCmd runs a Cmd and returns the first message it produces, unwrapping
// tea.BatchMsg the same way the bubbletea program does.
func runCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("nil command")
	}
	msg := cmd()
	if bm, ok := msg.(tea.BatchMsg); ok {
		for _, sub := range bm {
			if m := runCmd(t, sub); m != nil {
				return m
			}
		}
		return nil
	}
	return msg
}

// newFallbackTestModel returns a model with the download maps initialized
// and a stubbed browser opener recording the URL it was asked to open.
func newFallbackTestModel(t *testing.T, gameID int64) (model, *string) {
	t.Helper()
	opened := ""
	old := openBrowser
	openBrowser = func(url string) error {
		opened = url
		return nil
	}
	t.Cleanup(func() { openBrowser = old })

	return model{
		selectedID:        gameID,
		activeDownloadsMu: &sync.Mutex{},
		activeDownloads:   make(map[int64]*activeDownload),
		browserFallbacks:  make(map[int64]*browserFallback),
		gameWatchers:      make(map[int64]*downloader.ArchiveWatcher),
		watcherMsgCh:      make(chan tea.Msg, 16),
		pumpArmed:         &atomic.Bool{},
	}, &opened
}

func TestDownloadFinishedMsg_SetsBrowserFallback(t *testing.T) {
	m, _ := newFallbackTestModel(t, 42)

	msg := downloadFinishedMsg{
		gameID: 42, url: "https://example.com/file.zip",
		summary: "All 2 download links failed:\n  [vikingfile] ✗ boom",
		destDir: "/tmp/downloads", gamePath: "/tmp/game", engine: "RenPy",
	}
	updated, _ := m.Update(msg)
	m2 := updated.(model)

	fb := m2.browserFallbacks[42]
	if fb == nil {
		t.Fatal("expected browserFallback to be set after downloadFinishedMsg")
	}
	if fb.url != msg.url || fb.destDir != msg.destDir || fb.gamePath != msg.gamePath || fb.engine != msg.engine {
		t.Errorf("fallback payload mismatch: %+v", fb)
	}
	if fb.watching {
		t.Error("fallback must start as not-watching")
	}

	// A success (empty summary) must not create a fallback.
	updated, _ = m2.Update(downloadFinishedMsg{gameID: 43})
	m3 := updated.(model)
	if m3.browserFallbacks[43] != nil {
		t.Error("successful download must not create a browser fallback")
	}
}

func TestBrowserFallback_Dismiss(t *testing.T) {
	m, _ := newFallbackTestModel(t, 7)
	m.browserFallbacks[7] = &browserFallback{
		gameID: 7, url: "https://example.com/x.zip", destDir: "/tmp/dl",
		gamePath: "/tmp/g", engine: "RPGM",
	}
	m.viewMode = DetailView

	// 'n' dismisses the prompt; the failed download state itself is kept.
	updated, cmd := m.handleDetailKey("n")
	if cmd != nil {
		t.Error("dismiss must not return a command")
	}
	m2 := updated.(model)
	if m2.browserFallbacks[7] != nil {
		t.Error("expected fallback to be removed after dismiss")
	}

	// Esc dismisses too.
	m.browserFallbacks[7] = &browserFallback{gameID: 7}
	updated, _ = m.handleDetailKey("esc")
	if updated.(model).browserFallbacks[7] != nil {
		t.Error("expected fallback to be removed after esc")
	}

	// Without a fallback entry, y/n must fall through to normal keys.
	m3, _ := m.handleDetailKey("y")
	if m3.(model).err != nil {
		t.Error("unexpected error from plain 'y' key")
	}
}

func TestBrowserFallback_ConfirmOpensBrowserAndStartsWatcher(t *testing.T) {
	gameID := int64(11)
	destDir := t.TempDir()
	m, opened := newFallbackTestModel(t, gameID)
	m.browserFallbacks[gameID] = &browserFallback{
		gameID: gameID, url: "https://example.com/best.zip",
		destDir: destDir, gamePath: "/tmp/game", engine: "RenPy",
	}
	m.viewMode = DetailView

	updated, cmd := m.handleDetailKey("y")
	m2 := updated.(model)

	if *opened != "https://example.com/best.zip" {
		t.Errorf("browser opened with %q, want the best link", *opened)
	}
	if cmd == nil {
		t.Fatal("expected the watcher message pump to be armed")
	}
	fb := m2.browserFallbacks[gameID]
	if fb == nil || !fb.watching {
		t.Fatal("expected fallback to be marked watching after confirmation")
	}
	if fb.watcher == nil {
		t.Fatal("expected a download-dir watcher to be started")
	}
	if m2.gameWatchers[gameID] != fb.watcher {
		t.Error("expected the watcher to be registered in gameWatchers")
	}

	// A browser-saved archive landing in the download dir must produce a
	// watcherFoundMsg carrying the install payload.
	archivePath := filepath.Join(destDir, "Game_Update.zip")
	makeTestZip(t, archivePath)

	select {
	case msg := <-m2.watcherMsgCh:
		wf, ok := msg.(watcherFoundMsg)
		if !ok {
			t.Fatalf("expected watcherFoundMsg, got %T", msg)
		}
		if wf.path != archivePath {
			t.Errorf("watcher path = %q, want %q", wf.path, archivePath)
		}
		if wf.gameID != gameID || wf.destDir != destDir || wf.engine != "RenPy" || wf.gamePath != "/tmp/game" {
			t.Errorf("watcher payload mismatch: %+v", wf)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for watcherFoundMsg")
	}

	// Clean up the watcher so its goroutines stop before the test ends.
	if err := m2.gameWatchers[gameID].Stop(); err != nil {
		t.Errorf("watcher Stop: %v", err)
	}
}

func TestBrowserFallback_ConfirmBrowserError(t *testing.T) {
	gameID := int64(12)
	m, _ := newFallbackTestModel(t, gameID)
	m.browserFallbacks[gameID] = &browserFallback{gameID: gameID, url: "https://x/"}

	old := openBrowser
	openBrowser = func(string) error { return os.ErrPermission }
	t.Cleanup(func() { openBrowser = old })

	updated, cmd := m.handleDetailKey("y")
	m2 := updated.(model)
	if cmd != nil {
		t.Error("browser failure must not arm the pump")
	}
	if m2.err == nil {
		t.Error("expected an error when the browser cannot be opened")
	}
	if m2.browserFallbacks[gameID].watching {
		t.Error("fallback must remain not-watching when the browser fails")
	}
	if len(m2.gameWatchers) != 0 {
		t.Error("no watcher must be started when the browser fails")
	}
}

// TestWatcherFoundMsg_InstallsArchive exercises the full watcher pickup
// pipeline: Update receives watcherFoundMsg and the returned command runs
// validate → extract → merge, ending with a successful watcherInstalledMsg.
func TestWatcherFoundMsg_InstallsArchive(t *testing.T) {
	gameID := int64(21)
	destDir := t.TempDir()
	gamePath := filepath.Join(t.TempDir(), "game")
	if err := os.MkdirAll(gamePath, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-existing game file that must survive the merge. RPGM pattern
	// "save/*" is what actually exercises the preserve path.
	if err := os.MkdirAll(filepath.Join(gamePath, "save"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gamePath, "save", "slot1.rpgsave"), []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	m, _ := newFallbackTestModel(t, gameID)
	archivePath := filepath.Join(destDir, "Game_Update.zip")
	makeTestZip(t, archivePath)

	updated, cmd := m.Update(watcherFoundMsg{
		gameID: gameID, path: archivePath,
		destDir: destDir, gamePath: gamePath, engine: "RPGM",
	})
	if cmd == nil {
		t.Fatal("Update(watcherFoundMsg) must return an install command")
	}

	// Run the batch: the install cmd completes, the pump times out idle.
	msg := runCmd(t, cmd)
	wi, ok := msg.(watcherInstalledMsg)
	if !ok {
		t.Fatalf("expected watcherInstalledMsg, got %T", msg)
	}
	if wi.err != nil {
		t.Fatalf("install failed: %v", wi.err)
	}
	_ = updated

	// The merged file landed in the game directory and the archive was
	// consumed; the pre-existing save survived the backup dance.
	if _, err := os.Stat(filepath.Join(gamePath, "data.txt")); err != nil {
		t.Errorf("merged file missing: %v", err)
	}
	if _, err := os.Stat(archivePath); !os.IsNotExist(err) {
		t.Error("expected the archive to be removed after install")
	}
	if b, err := os.ReadFile(filepath.Join(gamePath, filepath.Join("save", "slot1.rpgsave"))); err != nil || string(b) != "keep me" {
		t.Errorf("pre-existing save was not preserved: %v %q", err, b)
	}

	// The active download entry reflects the install.
	m2 := updated.(model)
	m2.activeDownloadsMu.Lock()
	ad := m2.activeDownloads[gameID]
	m2.activeDownloadsMu.Unlock()
	if ad == nil {
		t.Fatal("expected an active download entry for the watcher install")
	}
	ad.mu.Lock()
	status := ad.status
	ad.mu.Unlock()
	if status != db.DownloadStatusCompleted {
		t.Errorf("expected completed status, got %q", status)
	}
}

func TestWatcherFoundMsg_RejectsInvalidFile(t *testing.T) {
	gameID := int64(22)
	destDir := t.TempDir()
	m, _ := newFallbackTestModel(t, gameID)

	// A file that is not a valid game archive (too small, not an archive).
	badPath := filepath.Join(destDir, "junk.zip")
	if err := os.WriteFile(badPath, []byte("tiny"), 0o644); err != nil {
		t.Fatal(err)
	}

	updated, cmd := m.Update(watcherFoundMsg{
		gameID: gameID, path: badPath, destDir: destDir,
		gamePath: "/tmp/nope", engine: "Unknown",
	})
	msg := runCmd(t, cmd)
	wi, ok := msg.(watcherInstalledMsg)
	if !ok {
		t.Fatalf("expected watcherInstalledMsg, got %T", msg)
	}
	if wi.err == nil {
		t.Error("expected an error for an invalid picked-up file")
	}
	_ = updated
}

// TestBrowserFallback_PromptRenders verifies the y/n prompt appears in the
// detail view while a fallback is pending, and switches to the "watching"
// hint after confirmation.
func TestBrowserFallback_PromptRenders(t *testing.T) {
	initStyles()

	gameID := int64(31)
	m, _ := newFallbackTestModel(t, gameID)
	m.width = 100
	m.detailGame = &db.Game{ID: gameID, Title: "Fallback Game", Path: "/tmp/game", Status: "active"}
	m.activeDownloads[gameID] = &activeDownload{
		gameID: gameID, status: db.DownloadStatusFailed,
		err: "All 1 download links failed:\n  [vikingfile] ✗ boom",
	}

	// Pending fallback → prompt with the y/n choice.
	m.browserFallbacks[gameID] = &browserFallback{
		gameID: gameID, url: "https://x/", destDir: "/tmp/downloads",
	}
	content := m.buildDetailContent()
	if !strings.Contains(content, "[y] Open") {
		t.Error("expected the browser-open prompt in the detail view")
	}
	if !strings.Contains(content, "/tmp/downloads") {
		t.Error("expected the download dir hint in the detail view")
	}

	// Confirmed → watching hint, no prompt.
	m.browserFallbacks[gameID].watching = true
	content = m.buildDetailContent()
	if strings.Contains(content, "[y] Open") {
		t.Error("prompt must disappear once watching")
	}
	if !strings.Contains(content, "Watching /tmp/downloads") {
		t.Error("expected the watching hint in the detail view")
	}
}
