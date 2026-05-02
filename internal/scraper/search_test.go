package scraper

import (
	"fmt"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// SanitizeTitle
// ---------------------------------------------------------------------------

func TestSanitizeTitle_VariousInputs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		// Basic: version + tags stripped
		{"My Game v0.5 [Adult] [RPGMaker]", "My Game"},
		{"Summer's Gone [v0.5.2] [Ocean]", "Summer's Gone"},
		{"Test Game v1.0", "Test Game"},
		{"Simple Game", "Simple Game"},
		{"HH TRAP v1.2.3", "HH TRAP"},
		{"Acting Lessons [v1.0] [Dr PinkCake]", "Acting Lessons"},
		{"", ""},
		{"No tags or version", "No tags or version"},

		// Platform suffixes
		{"Game Name - PC", "Game Name"},
		{"Another Game - win", "Another Game"},
		{"Trailing - linux", "Trailing"},
		{"Trailing - mac", "Trailing"},
		{"Trailing - MAC", "Trailing"},

		// Hyphenated version → stripped
		{"Game-v0.5", "Game"},
		{"BootyHunter-0.8-pc", "BootyHunter"},
		{"SummertimeSaga-0-20-16-pc", "SummertimeSaga"},
		{"CoC II-0.7.18-win", "CoC II"},

		// Underscored version → stripped
		{"SiNiSistar2_v1_0_6-WIN", "SiNiSistar2"},
		{"Latex_Dungeon_V1.5.7-WIN", "Latex Dungeon"},

		// HG prefix stripped
		{"HG755_Inn, Tavern and Halberd", "Inn, Tavern and Halberd"},
		{"HG2496_UnHolY JaiL TRPG v1.30b", "UnHolY JaiL TRPG"},

		// Bracketed content stripped (keeps surrounding words)
		{"Game with (platform) tags", "Game with tags"},
		{"[Only Tag] Title", "Title"},

		// Whitespace handling
		{"   Spaced Out v2.0 [Final]   ", "Spaced Out"},

		// Parenthetical in middle — content removed, words preserved
		{"My (adult) Game v0.5", "My Game"},

		// Version with letter suffix
		{"My_hentai_fantasy-0.11-pc", "My hentai fantasy"},
		{"succumama-1.1.6-pc", "succumama"},
		{"Training_The_Demon-0.1.2-pc", "Training The Demon"},

		// Numbers not preceded by version marker
		{"WelcomeToErosland-0.5-pc", "WelcomeToErosland"},
		{"seeds-of-chaos-0.4.10-pc", "seeds of chaos"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.input), func(t *testing.T) {
			got := SanitizeTitle(tt.input)
			if got != tt.want {
				t.Errorf("SanitizeTitle(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ComputeMatchScore
// ---------------------------------------------------------------------------

func TestComputeMatchScore_Exact(t *testing.T) {
	t.Parallel()
	score := ComputeMatchScore("My Game", "My Game")
	if score != 1.0 {
		t.Errorf("exact match expected 1.0, got %f", score)
	}
}

func TestComputeMatchScore_ExactAfterSanitize(t *testing.T) {
	t.Parallel()
	score := ComputeMatchScore("HH TRAP", "HH TRAP [v1.0] [Studio]")
	// SanitizeTitle("HH TRAP") = "HH TRAP"; SanitizeTitle("HH TRAP [v1.0] [Studio]") = "HH TRAP"
	// So they are equal after sanitize.
	if score == 1.0 {
		// Exact after sanitize is acceptable too.
		return
	}
	if score < 0.85 {
		t.Errorf("sanitize-equal match expected >= 0.85, got %f", score)
	}
}

func TestComputeMatchScore_Contains(t *testing.T) {
	t.Parallel()
	score := ComputeMatchScore("Summer's Gone", "Summer's Gone [v0.5.2] [Ocean]")
	if score < 0.85 {
		t.Errorf("contains match expected >= 0.85, got %f", score)
	}
}

func TestComputeMatchScore_WordOverlap(t *testing.T) {
	t.Parallel()

	// Exact match after sanitize: both sanitize to "acting lessons".
	score1 := ComputeMatchScore("Acting Lessons", "Acting Lessons [v1.0] [Dr PinkCake]")
	if score1 < 0.85 {
		t.Errorf("exact-after-sanitize match expected >= 0.85, got %f", score1)
	}

	score2 := ComputeMatchScore("Game of Thrones", "Game of Thrones Adventure")
	// sanitize both: "game of thrones" vs "game of thrones adventure"
	// contains -> 0.85

	score3 := ComputeMatchScore("Life is Strange", "Strange Life")
	// "life is strange" vs "strange life"
	// wordsA = [life, is, strange], wordsB = [strange, life]
	// shared = 2 (life, strange), max(3,2) = 3
	// score = 2/3 ≈ 0.667

	if score2 < 0.85 {
		t.Errorf("contains match expected >= 0.85, got %f", score2)
	}
	if score3 < 0.6 || score3 > 0.7 {
		t.Errorf("word overlap expected ~0.667, got %f", score3)
	}
}

func TestComputeMatchScore_NoMatch(t *testing.T) {
	t.Parallel()
	score := ComputeMatchScore("Totally Different", "Unrelated Game")
	if score != 0.0 {
		t.Errorf("no-match expected 0.0, got %f", score)
	}
}

func TestComputeMatchScore_CaseInsensitive(t *testing.T) {
	t.Parallel()
	score := ComputeMatchScore("MY GAME", "my game")
	if score != 1.0 {
		t.Errorf("case-insensitive exact expected 1.0, got %f", score)
	}
}

func TestComputeMatchScore_Empty(t *testing.T) {
	t.Parallel()
	score := ComputeMatchScore("", "")
	if score != 0.0 {
		t.Errorf("empty strings expected 0.0, got %f", score)
	}
}

// ---------------------------------------------------------------------------
// parseSearchResults
// ---------------------------------------------------------------------------

func TestParseSearchResults_Success(t *testing.T) {
	t.Parallel()

	const searchHTML = `<!DOCTYPE html>
<html>
<body>
<div class="block">
  <div class="contentRow">
    <h3 class="contentRow-title">
      <a href="https://f95zone.to/threads/my-awesome-game.12345/">My Awesome Game [v0.5.2] [Some Studio]</a>
    </h3>
    <div class="contentRow-snippet">Welcome to the thread for My Awesome Game!</div>
  </div>
  <div class="contentRow">
    <h3 class="contentRow-title">
      <a href="https://f95zone.to/threads/another-game.67890/">Another Game [v1.0] [DevName]</a>
    </h3>
    <div class="contentRow-snippet">Another great game thread.</div>
  </div>
  <div class="contentRow">
    <h3 class="contentRow-title">
      <a href="https://f95zone.to/threads/third-entry.11111/">Third Entry [v0.3] [Studio X]</a>
    </h3>
    <div class="contentRow-snippet">Third game description.</div>
  </div>
</div>
</body>
</html>`

	results := parseSearchResults(searchHTML)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	tests := []struct {
		idx       int
		wantTitle string
		wantURL   string
		wantSnip  string
	}{
		{
			idx:       0,
			wantTitle: "My Awesome Game [v0.5.2] [Some Studio]",
			wantURL:   "https://f95zone.to/threads/my-awesome-game.12345/",
			wantSnip:  "Welcome to the thread for My Awesome Game!",
		},
		{
			idx:       1,
			wantTitle: "Another Game [v1.0] [DevName]",
			wantURL:   "https://f95zone.to/threads/another-game.67890/",
			wantSnip:  "Another great game thread.",
		},
		{
			idx:       2,
			wantTitle: "Third Entry [v0.3] [Studio X]",
			wantURL:   "https://f95zone.to/threads/third-entry.11111/",
			wantSnip:  "Third game description.",
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("result[%d]", tt.idx), func(t *testing.T) {
			r := results[tt.idx]
			if r.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", r.Title, tt.wantTitle)
			}
			if r.URL != tt.wantURL {
				t.Errorf("URL = %q, want %q", r.URL, tt.wantURL)
			}
			if r.Snippet != tt.wantSnip {
				t.Errorf("Snippet = %q, want %q", r.Snippet, tt.wantSnip)
			}
		})
	}
}

func TestParseSearchResults_EmptyResults(t *testing.T) {
	t.Parallel()

	const emptyHTML = `<!DOCTYPE html>
<html>
<body>
<div class="block">
  <p>No results found.</p>
</div>
</body>
</html>`

	results := parseSearchResults(emptyHTML)
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestParseSearchResults_RelativeURLs(t *testing.T) {
	t.Parallel()

	const html = `<!DOCTYPE html>
<html>
<body>
<div class="block">
  <div class="contentRow">
    <h3 class="contentRow-title">
      <a href="/threads/relative-game.99999/">Relative Game</a>
    </h3>
  </div>
</div>
</body>
</html>`

	results := parseSearchResults(html)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].URL != "https://f95zone.to/threads/relative-game.99999/" {
		t.Errorf("URL should be absolute, got %q", results[0].URL)
	}
}

func TestParseSearchResults_MaxFiveResults(t *testing.T) {
	t.Parallel()

	// Build HTML with 7 results.
	var buf strings.Builder
	buf.WriteString(`<!DOCTYPE html><html><body><div class="block">`)
	for i := 1; i <= 7; i++ {
		buf.WriteString(fmt.Sprintf(`<div class="contentRow">
    <h3 class="contentRow-title"><a href="https://f95zone.to/threads/game-%d.%d/">Game %d</a></h3>
  </div>`, i, i, i))
	}
	buf.WriteString(`</div></body></html>`)

	results := parseSearchResults(buf.String())
	if len(results) > 5 {
		t.Fatalf("expected at most 5 results, got %d", len(results))
	}
	if len(results) != 5 {
		t.Errorf("expected exactly 5 results (capped), got %d", len(results))
	}
}

func TestParseSearchResults_MissingHref(t *testing.T) {
	t.Parallel()

	const html = `<!DOCTYPE html>
<html>
<body>
<div class="block">
  <div class="contentRow">
    <h3 class="contentRow-title"><a>No Href</a></h3>
  </div>
</div>
</body>
</html>`

	results := parseSearchResults(html)
	if len(results) != 0 {
		t.Fatalf("expected 0 results for missing href, got %d", len(results))
	}
}

func TestParseSearchResults_InvalidHTML(t *testing.T) {
	t.Parallel()

	results := parseSearchResults("<<<invalid html>>>")
	if results != nil {
		t.Fatalf("expected nil for invalid HTML, got %v", results)
	}
}

func TestParseSearchResults_NoSnippetField(t *testing.T) {
	t.Parallel()

	const html = `<!DOCTYPE html>
<html>
<body>
<div class="block">
  <div class="contentRow">
    <h3 class="contentRow-title"><a href="https://f95zone.to/threads/game.1/">Title Only</a></h3>
  </div>
</div>
</body>
</html>`

	results := parseSearchResults(html)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Snippet != "" {
		t.Errorf("expected empty snippet, got %q", results[0].Snippet)
	}
}
