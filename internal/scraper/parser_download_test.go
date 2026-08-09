package scraper

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestParseSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		text string
		want int64
	}{
		{"[228.3 MB]", 2283 * 1024 * 1024 / 10},
		{"Game_v1.0.rar [1.2 GB]", 12 * 1024 * 1024 * 1024 / 10},
		{"512 KB", 512 * 1024},
		{"3.5 TB", 35 * 1024 * 1024 * 1024 * 1024 / 10},
		{"228.3 MiB", 2283 * 1024 * 1024 / 10},
		{"228.3 MB [Win]", 2283 * 1024 * 1024 / 10},
		{"WIN: MEGA - GOFILE [346.6 MB]", 3466 * 1024 * 1024 / 10},
		{"8 GB of RAM needed", 8 * 1024 * 1024 * 1024},
		{"no size here", 0},
		{"", 0},
		{"v1.2b", 0},
		{"100", 0},
		{"needs 8 GB of RAM", 8 * 1024 * 1024 * 1024},
	}
	for _, tt := range tests {
		if got := parseSize(tt.text); got != tt.want {
			t.Errorf("parseSize(%q) = %d, want %d", tt.text, got, tt.want)
		}
	}
}

// docFromHTML parses an HTML fragment into a goquery document.
func docFromHTML(t *testing.T, html string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return doc
}

func TestExtractLinkSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		html string
		want int64
	}{
		{
			"anchor text bracket size",
			`<a href="https://pixeldrain.com/u/abc">Game_v1.0.zip [228.3 MB]</a>`,
			2283 * 1024 * 1024 / 10,
		},
		{
			"title attribute",
			`<a href="https://pixeldrain.com/u/abc" title="[1.2 GB]">Game_v1.0.zip</a>`,
			12 * 1024 * 1024 * 1024 / 10,
		},
		{
			"aria-label attribute",
			`<a href="https://pixeldrain.com/u/abc" aria-label="Game 1.0 — 512 MB">dl</a>`,
			512 * 1024 * 1024,
		},
		{
			"row trailing sibling",
			`<div class="dl"><a href="https://mega.nz/file/x">MEGA</a>: <b>Win</b> [346.6 MB]</div>`,
			3466 * 1024 * 1024 / 10,
		},
		{
			"prose outside the row is ignored",
			`<div><p>This game needs 8 GB of RAM.</p><div class="dl"><a href="https://pixeldrain.com/u/abc">dl</a></div></div>`,
			0,
		},
		{
			"no size anywhere",
			`<a href="https://pixeldrain.com/u/abc">dl</a>`,
			0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := docFromHTML(t, tt.html)
			a := doc.Find("a").First()
			if a.Length() == 0 {
				t.Fatal("fixture has no anchor")
			}
			if got := extractLinkSize(a, a.Text()); got != tt.want {
				t.Errorf("extractLinkSize = %d, want %d", got, tt.want)
			}
		})
	}
}
