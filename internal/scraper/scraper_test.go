package scraper

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// ParseCookies
// ---------------------------------------------------------------------------

func TestParseCookies_Empty(t *testing.T) {
	t.Parallel()
	result := ParseCookies("")
	if result != nil {
		t.Fatalf("expected nil for empty input, got %v", result)
	}
}

func TestParseCookies_Single(t *testing.T) {
	t.Parallel()
	result := ParseCookies("xf_session=abc123")
	if len(result) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(result))
	}
	if result[0].Name != "xf_session" || result[0].Value != "abc123" {
		t.Errorf("unexpected cookie: %+v", result[0])
	}
}

func TestParseCookies_Multiple(t *testing.T) {
	t.Parallel()
	input := "xf_session=abc123; cf_clearance=def456; xf_user=ghi789"
	result := ParseCookies(input)
	if len(result) != 3 {
		t.Fatalf("expected 3 cookies, got %d", len(result))
	}
	tests := []struct {
		name, value string
	}{
		{"xf_session", "abc123"},
		{"cf_clearance", "def456"},
		{"xf_user", "ghi789"},
	}
	for i, tt := range tests {
		if result[i].Name != tt.name || result[i].Value != tt.value {
			t.Errorf("cookie %d: expected %s=%s, got %s=%s",
				i, tt.name, tt.value, result[i].Name, result[i].Value)
		}
	}
}

func TestParseCookies_Malformed(t *testing.T) {
	t.Parallel()
	// Missing "=" should be skipped.
	input := "xf_session=abc123; badcookie; xf_user=ghi789"
	result := ParseCookies(input)
	if len(result) != 2 {
		t.Fatalf("expected 2 valid cookies, got %d", len(result))
	}
}

func TestParseCookies_Whitespace(t *testing.T) {
	t.Parallel()
	input := "  xf_session = abc123  ;  xf_user=ghi789  "
	result := ParseCookies(input)
	if len(result) != 2 {
		t.Fatalf("expected 2 cookies, got %d", len(result))
	}
	if result[0].Name != "xf_session" || result[0].Value != "abc123" {
		t.Errorf("first cookie unexpected: %+v", result[0])
	}
	if result[1].Name != "xf_user" || result[1].Value != "ghi789" {
		t.Errorf("second cookie unexpected: %+v", result[1])
	}
}

// ---------------------------------------------------------------------------
// extractThreadID
// ---------------------------------------------------------------------------

func TestExtractThreadID_Valid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		url  string
		want int64
	}{
		{"https://f95zone.to/threads/game-name.12345/", 12345},
		{"https://f95zone.to/threads/some-long-slug.67890/", 67890},
		{"https://f95zone.to/threads/a.b.c.111/", 111},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := extractThreadID(tt.url)
			if got != tt.want {
				t.Errorf("extractThreadID(%q) = %d, want %d", tt.url, got, tt.want)
			}
		})
	}
}

func TestExtractThreadID_NoMatch(t *testing.T) {
	t.Parallel()
	tests := []string{
		"https://f95zone.to/threads/no-id-here/",
		"https://f95zone.to/forums/general.5/",
		"not-a-url",
		"",
	}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			got := extractThreadID(url)
			if got != 0 {
				t.Errorf("extractThreadID(%q) = %d, want 0", url, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// extractVersion
// ---------------------------------------------------------------------------

func TestExtractVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		text string
		want string
	}{
		{"Version: 1.2.3", "1.2.3"},
		{"Version 1.2.3", "1.2.3"},
		{"Version: v0.5", "0.5"},
		{"v0.5", "0.5"},
		{"Ver. 1.0", "1.0"},
		{"Updated to v1.5.2", "1.5.2"},
		{"The game is now at version 2.0.1!", "2.0.1"}, // matches "version 2.0.1"
		{"Current version: 3.0", "3.0"},
		{"No version here", ""},
		{"", ""},
		{"Ver 0.9", "0.9"},
		{"ver 4.5.6", "4.5.6"},
		{"Developer: SomeStudio", ""},         // no version
		{"v 1.0", ""},                          // "v " not "v" directly followed by digit
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.text), func(t *testing.T) {
			got := extractVersion(tt.text)
			if got != tt.want {
				t.Errorf("extractVersion(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// extractDeveloper
// ---------------------------------------------------------------------------

func TestExtractDeveloper(t *testing.T) {
	t.Parallel()
	tests := []struct {
		text string
		want string
	}{
		{"Developer: SomeStudio", "SomeStudio"},
		{"Developer: Cool Dev Team", "Cool Dev Team"},
		{"Publisher: BigPublisher", "BigPublisher"},
		{"Developer/Publisher: DevName", "DevName"}, // Publisher pattern captures "DevName"
		{"No developer info here", ""},     // "developer" mid-sentence should NOT match
		{"", ""},
		{"Developer:   Lots of   spaces  ", "Lots of   spaces"},
		{"Developer: Studio\nOther text", "Studio"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.text), func(t *testing.T) {
			got := extractDeveloper(tt.text)
			if got != tt.want {
				t.Errorf("extractDeveloper(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// identifyHost
// ---------------------------------------------------------------------------

func TestIdentifyHost(t *testing.T) {
	t.Parallel()
	tests := []struct {
		url  string
		text string
		want string
	}{
		{"https://mega.nz/file/abc123", "MEGA", "mega"},
		{"https://mega.co.nz/file/xyz", "", "mega"},
		{"https://keep2share.cc/file/abc", "K2S", "keep2share"},
		{"https://k2s.cc/file/abc", "", "keep2share"},
		{"https://uploaded.net/file/abc", "Uploaded", "uploaded"},
		{"http://ul.to/abc", "", "uploaded"},
		{"https://www.mediafire.com/file/xyz", "MediaFire", "mediafire"},
		{"https://workupload.com/file/abc", "", "workupload"},
		{"https://drive.google.com/file/d/abc", "Google Drive", "googledrive"},
		{"https://www.dropbox.com/s/abc", "Dropbox", "dropbox"},
		{"https://example.com/file.zip", "Random Link", "other"},
		{"", "", "other"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s|%s", tt.url, tt.text), func(t *testing.T) {
			got := identifyHost(tt.url, tt.text)
			if got != tt.want {
				t.Errorf("identifyHost(%q, %q) = %q, want %q", tt.url, tt.text, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// extractMetadata
// ---------------------------------------------------------------------------

func TestExtractMetadata_Empty(t *testing.T) {
	t.Parallel()
	meta := extractMetadata("")
	if len(meta) != 0 {
		t.Errorf("expected empty map, got %v", meta)
	}
}

func TestExtractMetadata_NoOverview(t *testing.T) {
	t.Parallel()
	meta := extractMetadata("just some random text\nwith no overview")
	if len(meta) != 0 {
		t.Errorf("expected empty map, got %v", meta)
	}
}

func TestExtractMetadata_WithVersionAndDeveloper(t *testing.T) {
	t.Parallel()
	body := `Overview
Welcome to the thread for My Awesome Game!
This is an adult RPG with lots of content.
Version: 0.5.2
Developer: Some Studio

Download
MEGA
MediaFire

Changelog
Updated to v0.5.2 with many fixes.`

	meta := extractMetadata(body)
	if len(meta) != 2 {
		t.Fatalf("expected 2 metadata fields, got %d: %v", len(meta), meta)
	}

	if meta["version"] != "0.5.2" {
		t.Errorf(`meta["version"] = %q, want "0.5.2"`, meta["version"])
	}
	if meta["developer"] != "Some Studio" {
		t.Errorf(`meta["developer"] = %q, want "Some Studio"`, meta["developer"])
	}
}

func TestExtractMetadata_FullBlock(t *testing.T) {
	t.Parallel()
	body := `Overview:
A simple point and click game with nice art.

Thread Updated: 2026-04-23
Release Date: 2026-04-23
Developer: BadColor - Steam - Itch.io
Censored: No
Version: 1.31 Steam
OS: Windows
Language: English
Other Games: Link
Store: Steam
Genre:

Installation:
1. Download
2. Extract
3. Run`

	meta := extractMetadata(body)

	tests := []struct {
		key  string
		want string
	}{
		{"thread_updated", "2026-04-23"},
		{"release_date", "2026-04-23"},
		{"developer", "BadColor"}, // " - " stripped
		{"censored", "No"},
		{"version", "1.31 Steam"}, // version keeps full value
		{"os", "Windows"},
		{"language", "English"},
		{"other_games", "Link"},
		{"store", "Steam"},
		{"genre", ""}, // empty value preserved
	}
	for _, tt := range tests {
		got := meta[tt.key]
		if got != tt.want {
			t.Errorf("meta[%q] = %q, want %q", tt.key, got, tt.want)
		}
	}
}

func TestExtractMetadata_ChangelogFirst(t *testing.T) {
	t.Parallel()
	// No Installation section — Changelog is the first boundary.
	body := `Overview:
A short description.

Version: 2.0.0
Developer: GameDev

Changelog:
Version 2.0.0 - big update`

	meta := extractMetadata(body)
	if len(meta) != 2 {
		t.Fatalf("expected 2 fields, got %d: %v", len(meta), meta)
	}
	if meta["version"] != "2.0.0" {
		t.Errorf(`meta["version"] = %q, want "2.0.0"`, meta["version"])
	}
	if meta["developer"] != "GameDev" {
		t.Errorf(`meta["developer"] = %q, want "GameDev"`, meta["developer"])
	}
}

// extractMetadata only returns key-value metadata pairs, not the description
// text. The description is extracted separately via extractBodyText/bbWrapper.
func TestExtractMetadata_OnlyDescription(t *testing.T) {
	t.Parallel()
	// No metadata lines in the overview block — only description.
	body := `Overview:
This is just a description with no metadata.

It has multiple paragraphs.

Installation:
Do stuff.`

	meta := extractMetadata(body)
	if len(meta) != 0 {
		t.Errorf("expected empty map for description-only overview, got %v", meta)
	}
}

// ---------------------------------------------------------------------------
// extractVersionFromMeta
// ---------------------------------------------------------------------------

func TestExtractVersionFromMeta_MetadataWins(t *testing.T) {
	t.Parallel()
	meta := map[string]string{"version": "1.31 Steam"}
	fullText := "Changelog:\nVersion 0.5.2 - old version"

	got := extractVersionFromMeta(meta, fullText)
	if got != "1.31" {
		t.Errorf("extractVersionFromMeta = %q, want %q", got, "1.31")
	}
}

func TestExtractVersionFromMeta_Fallback(t *testing.T) {
	t.Parallel()
	meta := map[string]string{} // no version in meta
	fullText := "Overall, the best game ever.\nCurrent version: 3.0.1"

	got := extractVersionFromMeta(meta, fullText)
	if got != "3.0.1" {
		t.Errorf("extractVersionFromMeta = %q, want %q", got, "3.0.1")
	}
}

func TestExtractVersionFromMeta_EmptyMetaValue(t *testing.T) {
	t.Parallel()
	meta := map[string]string{"version": ""}
	fullText := "Version: 0.9"

	got := extractVersionFromMeta(meta, fullText)
	if got != "0.9" {
		t.Errorf("extractVersionFromMeta = %q, want %q", got, "0.9")
	}
}

// ---------------------------------------------------------------------------
// extractDeveloperFromMeta
// ---------------------------------------------------------------------------

func TestExtractDeveloperFromMeta_MetadataWins(t *testing.T) {
	t.Parallel()
	meta := map[string]string{"developer": "BadColor"}
	fullText := "Developer: Some Other Studio\nPublisher: Yet Another"

	got := extractDeveloperFromMeta(meta, fullText)
	if got != "BadColor" {
		t.Errorf("extractDeveloperFromMeta = %q, want %q", got, "BadColor")
	}
}

func TestExtractDeveloperFromMeta_Fallback(t *testing.T) {
	t.Parallel()
	meta := map[string]string{}
	fullText := "Developer: Actual Dev"

	got := extractDeveloperFromMeta(meta, fullText)
	if got != "Actual Dev" {
		t.Errorf("extractDeveloperFromMeta = %q, want %q", got, "Actual Dev")
	}
}

// ---------------------------------------------------------------------------
// parseThreadHTML — realistic XenForo HTML
// ---------------------------------------------------------------------------

const testXenForoHTML = `<!DOCTYPE html>
<html>
<head>
  <title>My Awesome Game [v0.5.2] [Some Studio] | F95Zone</title>
</head>
<body>
<div class="p-body">
  <div class="p-title">
    <h1 class="p-title-value">My Awesome Game [v0.5.2] [Some Studio]</h1>
  </div>

  <div class="tagList">
    <ul class="tagList">
      <li><a href="/tags/adult/" class="tagItem">Adult</a></li>
      <li><a href="/tags/rpg/" class="tagItem">RPG</a></li>
      <li><a href="/tags/parody/" class="tagItem">Parody</a></li>
    </ul>
  </div>

  <div class="messageList">
    <article class="message message--post" data-author="SomeStudio">
      <div class="message-content">
        <div class="bbWrapper">
          <h2>Overview</h2>
          <p>Welcome to the thread for My Awesome Game!</p>
          <p>This is an adult RPG with lots of content.</p>
          <p>Version: 0.5.2</p>
          <p>Developer: Some Studio</p>

          <img src="https://f95zone.to/attachments/cover_jpg.12345/" class="bbImage" width="500" alt="Cover" />

          <h3>Download</h3>
          <ul>
            <li><a href="https://mega.nz/file/abc123">MEGA</a></li>
            <li><a href="https://www.mediafire.com/file/xyz">MediaFire</a></li>
            <li><a href="https://www.pixeldrain.com/file/xyz">PixelDrain</a></li>
          </ul>

          <h3>Changelog</h3>
          <p>Updated to v0.5.2 with many fixes.</p>
        </div>
      </div>
    </article>

    <!-- Second post — should be ignored -->
    <article class="message message--post" data-author="User2">
      <div class="message-content">
        <div class="bbWrapper">
          <p>Great game, thanks!</p>
        </div>
      </div>
    </article>
  </div>
</div>
</body>
</html>`

func TestParseThreadHTML(t *testing.T) {
	t.Parallel()

	threadURL := "https://f95zone.to/threads/my-awesome-game.12345/"
	td, err := parseThreadHTML(testXenForoHTML, threadURL)
	if err != nil {
		t.Fatalf("parseThreadHTML returned error: %v", err)
	}

	// --- Title ---
	if td.Title != "My Awesome Game" {
		t.Errorf("Title = %q, want %q", td.Title, "My Awesome Game")
	}

	// --- ThreadID ---
	if td.ThreadID != 12345 {
		t.Errorf("ThreadID = %d, want 12345", td.ThreadID)
	}

	// --- Version ---
	if td.Version != "0.5.2" {
		t.Errorf("Version = %q, want %q", td.Version, "0.5.2")
	}

	// --- Developer ---
	if td.Developer != "Some Studio" {
		t.Errorf("Developer = %q, want %q", td.Developer, "Some Studio")
	}

	// --- Tags ---
	if len(td.Tags) != 3 {
		t.Fatalf("expected 3 tags, got %d: %v", len(td.Tags), td.Tags)
	}
	expectedTags := []string{"Adult", "RPG", "Parody"}
	for i, tag := range expectedTags {
		if td.Tags[i] != tag {
			t.Errorf("Tag[%d] = %q, want %q", i, td.Tags[i], tag)
		}
	}

	// --- CoverURL ---
	expectedCover := "https://f95zone.to/attachments/cover_jpg.12345/"
	if td.CoverURL != expectedCover {
		t.Errorf("CoverURL = %q, want %q", td.CoverURL, expectedCover)
	}

	// --- Overview (should contain first post body text) ---
	if td.Overview == "" {
		t.Error("Overview should not be empty")
	}
	if !strings.Contains(td.Overview, "Welcome to the thread") {
		t.Error("Overview should contain the first post's text")
	}

	// --- DownloadLinks ---
	if len(td.DownloadLinks) != 3 {
		t.Fatalf("expected 3 download links (mega + mediafire + pixeldrain), got %d", len(td.DownloadLinks))
	}
	if td.DownloadLinks[0].Host != "mega" {
		t.Errorf("DownloadLink[0].Host = %q, want %q", td.DownloadLinks[0].Host, "mega")
	}
	if td.DownloadLinks[1].Host != "mediafire" {
		t.Errorf("DownloadLink[1].Host = %q, want %q", td.DownloadLinks[1].Host, "mediafire")
	}
	if td.DownloadLinks[2].Host != "pixeldrain" {
		t.Errorf("DownloadLink[2].Host = %q, want %q", td.DownloadLinks[2].Host, "pixeldrain")
	}
}

// ---------------------------------------------------------------------------
// stripBracketed
// ---------------------------------------------------------------------------

func TestStripBracketed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"Goblin Nest [v1.31] [BadColor]", "Goblin Nest"},
		{"My Awesome Game [v0.5.2] [Some Studio]", "My Awesome Game"},
		{"Game (Steam)", "Game"},
		{"No Brackets Here", "No Brackets Here"},
		{"[Only Brackets]", ""},
		{"[v1.0] [Tag1] [Tag2]", ""},
		{"  [Leading] Spaced [Tags]", "Spaced"},
		{"Plain Title", "Plain Title"},
		{"", ""},
		{"  ", ""},
		{"(parentheses) and [brackets]", "and"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := stripBracketed(tt.input)
			if got != tt.want {
				t.Errorf("stripBracketed(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// extractVersionFromBrackets
// ---------------------------------------------------------------------------

func TestExtractVersionFromBrackets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"Game [v1.31] [BadColor]", "1.31"},
		{"Game [ver 2.0] [Studio]", "2.0"},
		{"Game [version 1.5.2] [Studio]", "1.5.2"},
		{"Game [v0.5] [Studio]", "0.5"},
		{"No Version Brackets", ""},
		{"Game [1.0] without v prefix", ""}, // must have v/ver prefix
		{"Game [v1.0]", "1.0"},
		{"[v1.0.0] Game At Start", "1.0.0"},
		{"Multiple [v1.0] [v2.0]", "1.0"}, // first match wins
		{"[v 0.5]", ""},                   // space between v and digit not handled
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := extractVersionFromBrackets(tt.input)
			if got != tt.want {
				t.Errorf("extractVersionFromBrackets(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// NewClient
// ---------------------------------------------------------------------------

func TestNewClient(t *testing.T) {
	t.Parallel()

	t.Run("empty cookie", func(t *testing.T) {
		c := NewClient("")
		if c == nil {
			t.Fatal("NewClient returned nil")
		}
		if c.Delay() <= 0 {
			t.Error("NewClient should have positive delay with rate limiting enabled")
		}
	})

	t.Run("with cookies", func(t *testing.T) {
		c := NewClient("xf_session=abc123; cf_clearance=def456; xf_user=ghi789")
		if c == nil {
			t.Fatal("NewClient with cookies returned nil")
		}
		if c.Delay() <= 0 {
			t.Error("NewClient should have positive delay")
		}
	})

	t.Run("unsafe client has zero delay", func(t *testing.T) {
		c := NewUnsafeClient("test=1")
		if c == nil {
			t.Fatal("NewUnsafeClient returned nil")
		}
		if c.Delay() != 0 {
			t.Errorf("NewUnsafeClient should have zero delay, got %v", c.Delay())
		}
	})
}

// ---------------------------------------------------------------------------
// ScrapeThread error paths
// ---------------------------------------------------------------------------

func TestScrapeThread_EmptyURL(t *testing.T) {
	t.Parallel()
	c := NewClient("")
	_, err := c.ScrapeThread("")
	if err == nil {
		t.Fatal("expected error for empty URL, got nil")
	}
}

func TestScrapeThread_InvalidURL(t *testing.T) {
	t.Parallel()
	c := NewClient("")
	_, err := c.ScrapeThread("://invalid")
	if err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}
}

// ---------------------------------------------------------------------------
// Client.do() rate-limiting tests via httptest server
// ---------------------------------------------------------------------------

func TestClientDo_RateLimitBackoff(t *testing.T) {
	t.Parallel()

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusTooManyRequests) // 429
		}
	}))
	defer srv.Close()

	client := NewClientWithHTTP("test=1", srv.Client())

	// First request should succeed.
	req, _ := http.NewRequest("GET", srv.URL, nil)
	resp, err := client.do(req, 0)
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// After 429, delay should increase.
	if client.Delay() <= 0 {
		t.Error("expected delay to increase after 429 response")
	}
}

func TestClientDo_ContextCancellation(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewClientWithHTTP("test=1", srv.Client())

	// Fire one request to set lastRequest.
	req, _ := http.NewRequest("GET", srv.URL, nil)
	resp, err := client.do(req, 0)
	if err != nil {
		t.Fatalf("first request: %v", err)
	}
	resp.Body.Close()

	// Second request with cancelled context — should respect context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	req, _ = http.NewRequestWithContext(ctx, "GET", srv.URL, nil)
	req.Header.Set("User-Agent", "test")
	_, err = client.do(req, 0)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestClientDo_Cooldown(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewClientWithHTTP("test=1", srv.Client())
	// Force reqCount to cooldownInterval so the check
	// cooldownInterval % cooldownInterval == 0 triggers on first call.
	client.reqCount = cooldownInterval

	req, _ := http.NewRequest("GET", srv.URL, nil)
	req.Header.Set("User-Agent", "test")
	start := time.Now()
	resp, err := client.do(req, 0)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	// Cooldown should have added significant delay.
	if elapsed < 1*time.Second {
		t.Errorf("expected at least 1s cooldown delay, got %v", elapsed)
	}
}

func TestClientDo_ForbiddenDetection(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden) // 403
	}))
	defer srv.Close()

	client := NewClientWithHTTP("test=1", srv.Client())
	req, _ := http.NewRequest("GET", srv.URL, nil)
	req.Header.Set("User-Agent", "test")
	_, err := client.do(req, 0)
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("expected 403 in error, got: %v", err)
	}
}

func TestClientDo_CloudflareBlocked(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable) // 503
		w.Write([]byte(`<html>Cloudflare</html>`))
	}))
	defer srv.Close()

	client := NewClientWithHTTP("test=1", srv.Client())
	req, _ := http.NewRequest("GET", srv.URL, nil)
	req.Header.Set("User-Agent", "test")
	_, err := client.do(req, 0)
	if err == nil {
		t.Fatal("expected error for Cloudflare-blocked response")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("expected 'blocked' in error, got: %v", err)
	}
}

func TestNewClientWithHTTP_PreservesCookieInjection(t *testing.T) {
	t.Parallel()

	var receivedCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedCookie = r.Header.Get("Cookie")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewClientWithHTTP("xf_session=abc123; xf_user=def456", srv.Client())
	req, _ := http.NewRequest("GET", srv.URL, nil)
	req.Header.Set("User-Agent", "test")
	resp, err := client.do(req, 0)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	if !strings.Contains(receivedCookie, "xf_session=abc123") {
		t.Errorf("expected cookie xf_session, got: %s", receivedCookie)
	}
	if !strings.Contains(receivedCookie, "xf_user=def456") {
		t.Errorf("expected cookie xf_user, got: %s", receivedCookie)
	}
}
