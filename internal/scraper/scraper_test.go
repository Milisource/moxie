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
// extractOverviewText
// ---------------------------------------------------------------------------

func TestExtractOverviewText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string // substring that should be present in the result
		not   string // substring that should NOT be present
	}{
		{
			name:  "extracts between Overview and Download",
			input: "Overview\nThis is a great game.\nVersion: 1.0\nDeveloper: Foo\nDownload\nmega links here\n",
			want:  "This is a great game.\nVersion: 1.0\nDeveloper: Foo",
			not:   "Download",
		},
		{
			name:  "handles Overview with colon",
			input: "Overview:\nSome description here.\n\nChangelog\nFixed bugs",
			want:  "Some description here.",
			not:   "Changelog",
		},
		{
			name:  "falls back to full text when no Overview heading",
			input: "Just some text without an overview heading",
			want:  "Just some text without an overview heading",
			not:   "",
		},
		{
			name:  "handles Developer Notes as section boundary",
			input: "Overview\nDescription text\nDeveloper Notes\nSome notes",
			want:  "Description text",
			not:   "Developer Notes",
		},
		{
			name:  "handles Installation as section boundary",
			input: "Overview:\nDesc\nInstallation:\nStep 1",
			want:  "Desc",
			not:   "Installation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractOverviewText(tt.input)
			if !strings.Contains(got, tt.want) {
				t.Errorf("extractOverviewText = %q, want it to contain %q", got, tt.want)
			}
			if tt.not != "" && strings.Contains(got, tt.not) {
				t.Errorf("extractOverviewText = %q, should NOT contain %q", got, tt.not)
			}
		})
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
          <script type="text/template">
            <img src="https://attachments.f95zone.to/2025/12/5597256_banner_f95.png" class="bbImage" data-zoom-target="1" alt="banner f95.png" />
          </script>
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

	// --- Overview (should contain just the description text) ---
	if td.Overview == "" {
		t.Error("Overview should not be empty")
	}
	if !strings.Contains(td.Overview, "Welcome to the thread") {
		t.Error("Overview should contain the description text")
	}
	if strings.Contains(td.Overview, "<img src") {
		t.Error("Overview must not contain raw HTML from <script> templates")
	}
	if strings.Contains(td.Overview, "Download") {
		t.Error("Overview should not include the Download section")
	}
	if strings.Contains(td.Overview, "Changelog") {
		t.Error("Overview should not include the Changelog section")
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
// Version source precedence
// ---------------------------------------------------------------------------

// threadHTMLWith builds a minimal XenForo thread page with the given title
// and first-post body.
func threadHTMLWith(title, body string) string {
	return `<html><body>
<h1 class="p-title-value">` + title + `</h1>
<article class="message-content"><div class="bbWrapper">` + body + `</div></article>
</body></html>`
}

// A changelog lists every past release. The title bracket is the version
// F95Zone's posting rules mandate, so it must win over a body scan that
// would otherwise return the longest string in the changelog.
func TestParseThreadHTML_TitleBracketBeatsChangelog(t *testing.T) {
	t.Parallel()

	body := `Overview
A game about things.

Developer: Some Studio

Changelog:
v0.4.10 - added the barn
v0.4.9 - fixed the barn`

	td, err := parseThreadHTML(threadHTMLWith("Barn Game [v0.9] [Some Studio]", body), "https://f95zone.to/threads/barn.999/")
	if err != nil {
		t.Fatalf("parseThreadHTML returned error: %v", err)
	}
	if td.Version != "0.9" {
		t.Errorf("Version = %q, want %q (title bracket must outrank changelog entries)", td.Version, "0.9")
	}
}

// The structured metadata block still outranks the title bracket.
func TestParseThreadHTML_MetaBeatsTitleBracket(t *testing.T) {
	t.Parallel()

	body := `Overview
A game about things.

Version: 1.2.3
Developer: Some Studio`

	td, err := parseThreadHTML(threadHTMLWith("Barn Game [v0.9] [Some Studio]", body), "https://f95zone.to/threads/barn.999/")
	if err != nil {
		t.Fatalf("parseThreadHTML returned error: %v", err)
	}
	if td.Version != "1.2.3" {
		t.Errorf("Version = %q, want %q", td.Version, "1.2.3")
	}
}

func TestExtractVersionFromBody(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "ignores changelog entries after the heading",
			body: "Updated to v0.9 today.\n\nChangelog:\nv0.4.10 - old\nv0.3.12 - older",
			want: "0.9",
		},
		{
			name: "ignores engine strings listed under Installation",
			body: "Now at version 0.9.\n\nInstallation: built with Unity 2019.4.31",
			want: "0.9",
		},
		{
			name: "falls back to full text when the lead region has none",
			body: "Grab it below.\n\nDownload: PC - v1.4.2 here",
			want: "1.4.2",
		},
		{
			name: "no version anywhere",
			body: "Just some prose with no numbers.",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractVersionFromBody(tt.body); got != tt.want {
				t.Errorf("extractVersionFromBody() = %q, want %q", got, tt.want)
			}
		})
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
		// Explicit v/ver/version prefix (always matched first)
		{"Game [v1.31] [BadColor]", "1.31"},
		{"Game [ver 2.0] [Studio]", "2.0"},
		{"Game [version 1.5.2] [Studio]", "1.5.2"},
		{"Game [v0.5] [Studio]", "0.5"},
		{"Game [v1.0]", "1.0"},
		{"[v1.0.0] Game At Start", "1.0.0"},
		{"Multiple [v1.0] [v2.0]", "1.0"},               // first match wins
		{"[v 0.5]", ""},                                  // space between v and digit not handled
		{"Game [v1.0 Alpha] [Dev]", "1.0"},               // prerelease suffix
		{"Game [Ch. 2 v3.0] [Dev]", "3.0"},               // chapter + version

		// Date in brackets (per F95Zone title format rules)
		{"Game [2018-07-18] [Dev]", "2018-07-18"},
		{"Game [2024-02-29] [Dev]", "2024-02-29"},

		// Bare version in brackets (immediate ] after last digit)
		{"Game [1.0] [Dev]", "1.0"},
		{"Game [0.5.2] [Studio]", "0.5.2"},

		// [Final] sentinel for complete games
		{"Game [Final] [Dev]", "Final"},
		{"Game [final] [Dev]", "Final"},

		// Not versions — ranges, days, chapters without embedded v
		{"No Version Brackets", ""},
		{"Game [Ch. 1-5]", ""},  // range, no v
		{"Game [Ep. 1-5]", ""},  // range, no v
		{"Game [Day 3]", ""},    // day marker
		{"Game [Part 1-5]", ""}, // part range

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
	_, err := client.do(req, 0)
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}

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
	_, err := client.do(req, 0)
	if err != nil {
		t.Fatalf("first request: %v", err)
	}

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
	_, err := client.do(req, 0)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

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
	_, err := client.do(req, 0)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if !strings.Contains(receivedCookie, "xf_session=abc123") {
		t.Errorf("expected cookie xf_session, got: %s", receivedCookie)
	}
	if !strings.Contains(receivedCookie, "xf_user=def456") {
		t.Errorf("expected cookie xf_user, got: %s", receivedCookie)
	}
}

// ---------------------------------------------------------------------------
// extractStoreLinks (tested through parseThreadHTML)
// ---------------------------------------------------------------------------

// storeLinkHTML is a minimal XenForo HTML template that wraps store links
// inside a bbWrapper so parseThreadHTML can find them.
const storeLinkHTML = `<!DOCTYPE html>
<html><body>
<div class="p-body">
  <div class="p-title">
    <h1 class="p-title-value">Store Link Test</h1>
  </div>
  <div class="messageList">
    <article class="message message--post">
      <div class="message-content">
        <div class="bbWrapper">
          <h2>Overview</h2>
          <p>Version: 1.0</p>
          <p>Developer: TestDev</p>
          <ul>
            %s
          </ul>
          <h3>Download</h3>
          <ul>
            <li><a href="https://mega.nz/file/abc">MEGA</a></li>
          </ul>
        </div>
      </div>
    </article>
  </div>
</div>
</body></html>`

func TestExtractStoreLinks_SteamAppURL(t *testing.T) {
	t.Parallel()

	links := `<li><a href="https://store.steampowered.com/app/12345/GameName/">Steam</a></li>`
	html := fmt.Sprintf(storeLinkHTML, links)
	td, err := parseThreadHTML(html, "https://f95zone.to/threads/test.1/")
	if err != nil {
		t.Fatalf("parseThreadHTML failed: %v", err)
	}

	v, ok := td.StoreLinks["steam"]
	if !ok {
		t.Fatal("expected steam store link, got none")
	}
	if v != "https://store.steampowered.com/app/12345/GameName/" {
		t.Errorf("steam link = %q, want %q", v, "https://store.steampowered.com/app/12345/GameName/")
	}
}

func TestExtractStoreLinks_SteamCuratorURL(t *testing.T) {
	t.Parallel()

	// Curator URLs should NOT match — they don't have /app/\d+.
	links := `<li><a href="https://store.steampowered.com/curator/12345-Some-Curator/">Curator</a></li>`
	html := fmt.Sprintf(storeLinkHTML, links)
	td, err := parseThreadHTML(html, "https://f95zone.to/threads/test.1/")
	if err != nil {
		t.Fatalf("parseThreadHTML failed: %v", err)
	}

	if _, ok := td.StoreLinks["steam"]; ok {
		t.Error("steam curator URL should NOT match as a store link")
	}
}

func TestExtractStoreLinks_ItchIOWithPath(t *testing.T) {
	t.Parallel()

	// itch.io URL with a game path should match.
	links := `<li><a href="https://some-creator.itch.io/game-name">Itch</a></li>`
	html := fmt.Sprintf(storeLinkHTML, links)
	td, err := parseThreadHTML(html, "https://f95zone.to/threads/test.1/")
	if err != nil {
		t.Fatalf("parseThreadHTML failed: %v", err)
	}

	v, ok := td.StoreLinks["itch"]
	if !ok {
		t.Fatal("expected itch store link, got none")
	}
	if v != "https://some-creator.itch.io/game-name" {
		t.Errorf("itch link = %q, want %q", v, "https://some-creator.itch.io/game-name")
	}
}

func TestExtractStoreLinks_ItchIOBarePublisher(t *testing.T) {
	t.Parallel()

	// Bare publisher page (no path) should NOT match.
	links := `<li><a href="https://some-creator.itch.io/">Itch</a></li>`
	html := fmt.Sprintf(storeLinkHTML, links)
	td, err := parseThreadHTML(html, "https://f95zone.to/threads/test.1/")
	if err != nil {
		t.Fatalf("parseThreadHTML failed: %v", err)
	}

	if _, ok := td.StoreLinks["itch"]; ok {
		t.Error("bare itch.io publisher page should NOT match as a store link")
	}
}

func TestExtractStoreLinks_ItchIOBarePublisherNoTrailingSlash(t *testing.T) {
	t.Parallel()

	// Bare publisher page without trailing slash should NOT match.
	links := `<li><a href="https://some-creator.itch.io">Itch</a></li>`
	html := fmt.Sprintf(storeLinkHTML, links)
	td, err := parseThreadHTML(html, "https://f95zone.to/threads/test.1/")
	if err != nil {
		t.Fatalf("parseThreadHTML failed: %v", err)
	}

	if _, ok := td.StoreLinks["itch"]; ok {
		t.Error("bare itch.io publisher page (no trailing slash) should NOT match")
	}
}

func TestExtractStoreLinks_DLSiteGamePage(t *testing.T) {
	t.Parallel()

	// DL-Site game/work page should match.
	links := `<li><a href="https://www.dlsite.com/maniax/work/=/product_id/abc123.html">DLsite</a></li>`
	html := fmt.Sprintf(storeLinkHTML, links)
	td, err := parseThreadHTML(html, "https://f95zone.to/threads/test.1/")
	if err != nil {
		t.Fatalf("parseThreadHTML failed: %v", err)
	}

	v, ok := td.StoreLinks["dlsite"]
	if !ok {
		t.Fatal("expected dlsite store link, got none")
	}
	if v != "https://www.dlsite.com/maniax/work/=/product_id/abc123.html" {
		t.Errorf("dlsite link = %q, want %q", v, "https://www.dlsite.com/maniax/work/=/product_id/abc123.html")
	}
}

func TestExtractStoreLinks_DLSiteHelpArticle(t *testing.T) {
	t.Parallel()

	// DL-Site help center article (/hc/) should NOT match.
	links := `<li><a href="https://www.dlsite.com/maniax/hc/faq/123/">Help</a></li>`
	html := fmt.Sprintf(storeLinkHTML, links)
	td, err := parseThreadHTML(html, "https://f95zone.to/threads/test.1/")
	if err != nil {
		t.Fatalf("parseThreadHTML failed: %v", err)
	}

	if _, ok := td.StoreLinks["dlsite"]; ok {
		t.Error("DLsite help article should NOT match as a store link")
	}
}

func TestExtractStoreLinks_DLSiteHomePage(t *testing.T) {
	t.Parallel()

	// DL-Site home page should NOT match.
	links := `<li><a href="https://www.dlsite.com/home/">DLsite Home</a></li>`
	html := fmt.Sprintf(storeLinkHTML, links)
	td, err := parseThreadHTML(html, "https://f95zone.to/threads/test.1/")
	if err != nil {
		t.Fatalf("parseThreadHTML failed: %v", err)
	}

	if _, ok := td.StoreLinks["dlsite"]; ok {
		t.Error("DLsite home page should NOT match as a store link")
	}
}

func TestExtractStoreLinks_MultipleStores(t *testing.T) {
	t.Parallel()

	links := `<li><a href="https://store.steampowered.com/app/555/">Steam</a></li>
<li><a href="https://dev.itch.io/my-game">Itch</a></li>`
	html := fmt.Sprintf(storeLinkHTML, links)
	td, err := parseThreadHTML(html, "https://f95zone.to/threads/test.1/")
	if err != nil {
		t.Fatalf("parseThreadHTML failed: %v", err)
	}

	if len(td.StoreLinks) != 2 {
		t.Fatalf("expected 2 store links, got %d: %v", len(td.StoreLinks), td.StoreLinks)
	}

	if v, ok := td.StoreLinks["steam"]; !ok || v != "https://store.steampowered.com/app/555/" {
		t.Errorf("steam: got %q, want %q", v, "https://store.steampowered.com/app/555/")
	}
	if v, ok := td.StoreLinks["itch"]; !ok || v != "https://dev.itch.io/my-game" {
		t.Errorf("itch: got %q, want %q", v, "https://dev.itch.io/my-game")
	}
}

func TestExtractStoreLinks_NoStoreLinks(t *testing.T) {
	t.Parallel()

	// No store links at all — only download links.
	links := `<!-- no store links here -->`
	html := fmt.Sprintf(storeLinkHTML, links)
	td, err := parseThreadHTML(html, "https://f95zone.to/threads/test.1/")
	if err != nil {
		t.Fatalf("parseThreadHTML failed: %v", err)
	}

	if len(td.StoreLinks) != 0 {
		t.Errorf("expected 0 store links, got %d: %v", len(td.StoreLinks), td.StoreLinks)
	}
}

func TestExtractStoreLinks_FirstMatchWins(t *testing.T) {
	t.Parallel()

	// When there are duplicate store links, the first one wins.
	links := `<li><a href="https://store.steampowered.com/app/111/">Steam First</a></li>
<li><a href="https://store.steampowered.com/app/222/">Steam Second</a></li>`
	html := fmt.Sprintf(storeLinkHTML, links)
	td, err := parseThreadHTML(html, "https://f95zone.to/threads/test.1/")
	if err != nil {
		t.Fatalf("parseThreadHTML failed: %v", err)
	}

	v, ok := td.StoreLinks["steam"]
	if !ok {
		t.Fatal("expected steam store link")
	}
	// First link should be preserved.
	if v != "https://store.steampowered.com/app/111/" {
		t.Errorf("steam link = %q, want first link %q", v, "https://store.steampowered.com/app/111/")
	}
}
