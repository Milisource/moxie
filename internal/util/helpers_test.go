package util

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
// ConfigDir / DbPath / ConfigPath
// ---------------------------------------------------------------------------

func TestConfigDir_ReturnsPath(t *testing.T) {
	t.Parallel()
	dir := ConfigDir()
	if dir == "" {
		t.Error("ConfigDir should not be empty")
	}
	// Path should contain "moxie".
	if !strings.Contains(dir, "moxie") {
		t.Errorf("ConfigDir should contain 'moxie', got %q", dir)
	}
}

func TestDbPath_ReturnsPath(t *testing.T) {
	t.Parallel()
	path := DbPath()
	if path == "" {
		t.Error("DbPath should not be empty")
	}
	if !strings.HasSuffix(path, "games.db") {
		t.Errorf("DbPath should end with 'games.db', got %q", path)
	}
}

func TestConfigPath_ReturnsPath(t *testing.T) {
	t.Parallel()
	path := ConfigPath()
	if path == "" {
		t.Error("ConfigPath should not be empty")
	}
	if !strings.HasSuffix(path, "config.json") {
		t.Errorf("ConfigPath should end with 'config.json', got %q", path)
	}
}

// ---------------------------------------------------------------------------
// readConfigFrom / writeConfigTo (exported versions for testing)
// ---------------------------------------------------------------------------

// readConfigFrom reads JSON config from a specific path (for testing).
func readConfigFrom(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, err
	}
	var cfg map[string]string
	if err := json.Unmarshal(data, &cfg); err != nil {
		return make(map[string]string), nil
	}
	if cfg == nil {
		return make(map[string]string), nil
	}
	return cfg, nil
}

// writeConfigTo writes JSON config to a specific path (for testing).
func writeConfigTo(path string, cfg map[string]string) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "config-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	return os.Rename(tmpName, path)
}

func TestReadConfig_FileNotFound(t *testing.T) {
	t.Parallel()
	cfg, err := readConfigFrom("/nonexistent/path/config.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg) != 0 {
		t.Errorf("expected empty map for nonexistent file, got %v", cfg)
	}
}

func TestReadConfig_ValidJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"key1":"val1","key2":"val2"}`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := readConfigFrom(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg["key1"] != "val1" || cfg["key2"] != "val2" {
		t.Errorf("expected {key1:val1, key2:val2}, got %v", cfg)
	}
}

func TestReadConfig_CorruptJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{invalid json`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := readConfigFrom(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg) != 0 {
		t.Errorf("expected empty map for corrupt JSON (fresh start), got %v", cfg)
	}
}

func TestWriteConfig_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	input := map[string]string{
		"cookie": "xf_session=abc",
		"steam":  "/home/user/.steam",
	}
	if err := writeConfigTo(path, input); err != nil {
		t.Fatalf("writeConfigTo: %v", err)
	}

	// Read back.
	cfg, err := readConfigFrom(path)
	if err != nil {
		t.Fatalf("readConfigFrom: %v", err)
	}
	if cfg["cookie"] != "xf_session=abc" {
		t.Errorf("cookie = %q, want %q", cfg["cookie"], "xf_session=abc")
	}
	if cfg["steam"] != "/home/user/.steam" {
		t.Errorf("steam = %q, want %q", cfg["steam"], "/home/user/.steam")
	}
}

func TestWriteConfig_Empty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := writeConfigTo(path, map[string]string{}); err != nil {
		t.Fatalf("writeConfigTo empty: %v", err)
	}
	cfg, err := readConfigFrom(path)
	if err != nil {
		t.Fatalf("readConfigFrom: %v", err)
	}
	if len(cfg) != 0 {
		t.Errorf("expected empty map, got %v", cfg)
	}
}


