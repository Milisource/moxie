package main

import (
	"fmt"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// normalizeVersion
// ---------------------------------------------------------------------------

func TestNormalizeVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"v1.0", "1"},
		{"V1.0.0", "1"},
		{"1.0.3", "1.0.3"},
		{"0.13.4", "0.13.4"},
		{"1.0", "1"},
		{"", ""},
		{"v0.5", "0.5"},
		{"V2.0", "2"},
		{"1.0.0.0", "1"},
		{"v1.2.3", "1.2.3"},
		{"  1.0  ", "1"},
		{"v1", "1"},
		{"0.0.0", "0"},
		{"V1.0.0.0.0", "1"},
		{"  v1.0  ", "1"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeVersion(tt.input)
			if got != tt.want {
				t.Errorf("normalizeVersion(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// formatSize
// ---------------------------------------------------------------------------

func TestFormatSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
		{1099511627776, "1.0 TB"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatSize(tt.input)
			if got != tt.want {
				t.Errorf("formatSize(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// formatDuration
// ---------------------------------------------------------------------------

func TestFormatDuration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input time.Duration
		want  string
	}{
		{30 * time.Second, "30s"},
		{60 * time.Second, "1m"},
		{90 * time.Second, "1m30s"},
		{61 * time.Second, "1m1s"},
		{3600 * time.Second, "1h0m"},
		{3661 * time.Second, "1h1m"},
		{0, "0s"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatDuration(tt.input)
			if got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// truncate
// ---------------------------------------------------------------------------

func TestTruncate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello", 4, "h..."},
		{"hello", 3, "hel"},
		{"hello", 2, "he"},
		{"hello", 1, "h"},
		{"hello world", 10, "hello w..."},
		{"hello\nworld", 15, "hello world"},
		{"", 5, ""},
		{"", 0, ""},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%d", tt.input, tt.max), func(t *testing.T) {
			got := truncate(tt.input, tt.max)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// extractEngineFromTitle
// ---------------------------------------------------------------------------
//
// extractEngineFromTitle checks only the FIRST word of the title against
// engine tag variant values.  If the first word doesn't exactly match a
// variant, no engine is returned.  This differs from findF95Engine which
// also checks HasPrefix on the full lowercased title.

func TestExtractEngineFromTitle(t *testing.T) {
	t.Parallel()
	tests := []struct {
		title string
		want  string
		note  string // optional context
	}{
		{"RPGM Completed Game Name", "RPGM", ""},
		{"Ren'Py Abandoned My Game [v0.5]", "RenPy", ""},
		{"Unity Game Title", "Unity", ""},
		{"Completed Visual Novel", "", "no engine prefix — first word is 'completed'"},
		{"Wolf RPG Something", "", "first word is 'Wolf', variants are 'wolf rpg'/'wolfrpg' — no exact match"},
		{"HTML5 Web Game", "HTML", "first word 'html5' matches HTML variant"},
		{"Flash Game Title", "Flash", ""},
		{"", "", "empty title"},
	}
	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			got := extractEngineFromTitle(tt.title)
			if got != tt.want {
				t.Errorf("extractEngineFromTitle(%q) = %q, want %q%s",
					tt.title, got, tt.want, optionalNote(tt.note))
			}
		})
	}
}

// optionalNote returns a colon-prefixed note if s is non-empty.
func optionalNote(s string) string {
	if s == "" {
		return ""
	}
	return " // " + s
}

// ---------------------------------------------------------------------------
// wrapText
// ---------------------------------------------------------------------------

func TestWrapText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		width int
		want  string
	}{
		{"", 10, ""},
		{"hello", 10, "hello"},
		{"hello world", 5, "hello\nworld"},
		{"hello beautiful world", 10, "hello\nbeautiful\nworld"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%d", tt.input, tt.width), func(t *testing.T) {
			got := wrapText(tt.input, tt.width)
			if got != tt.want {
				t.Errorf("wrapText(%q, %d) = %q, want %q", tt.input, tt.width, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// isBlocked
// ---------------------------------------------------------------------------

type testError struct{ msg string }

func (e testError) Error() string { return e.msg }

func TestIsBlocked(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"blocked string", testError{"blocked"}, true},
		{"BlockedError type", testError{"BlockedError"}, true},
		{"Cloudflare challenge", testError{"Cloudflare challenge"}, true},
		{"unrelated error", testError{"something else"}, false},
		{"empty string", testError{""}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isBlocked(tt.err)
			if got != tt.want {
				t.Errorf("isBlocked(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// truncateVer
// ---------------------------------------------------------------------------

func TestTruncateVer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"", "(none)"},
		{"1.0", "1.0"},
		{"v0.5.2", "v0.5.2"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := truncateVer(tt.input)
			if got != tt.want {
				t.Errorf("truncateVer(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
