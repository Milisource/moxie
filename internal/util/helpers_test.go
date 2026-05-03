package util

import (
	"fmt"
	"testing"
	"time"
)

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
			got := FormatSize(tt.input)
			if got != tt.want {
				t.Errorf("FormatSize(%d) = %q, want %q", tt.input, got, tt.want)
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
			got := FormatDuration(tt.input)
			if got != tt.want {
				t.Errorf("FormatDuration(%v) = %q, want %q", tt.input, got, tt.want)
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
			got := Truncate(tt.input, tt.max)
			if got != tt.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
			}
		})
	}
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
			got := WrapText(tt.input, tt.width)
			if got != tt.want {
				t.Errorf("WrapText(%q, %d) = %q, want %q", tt.input, tt.width, got, tt.want)
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
			got := IsBlocked(tt.err)
			if got != tt.want {
				t.Errorf("IsBlocked(%v) = %v, want %v", tt.err, got, tt.want)
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
			got := TruncateVer(tt.input)
			if got != tt.want {
				t.Errorf("TruncateVer(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
