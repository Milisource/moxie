package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
// readConfigFrom / writeConfigTo (test helpers that accept explicit paths)
// ---------------------------------------------------------------------------

func TestReadConfig_FileNotFound(t *testing.T) {
	t.Parallel()
	cfg, err := readConfigFrom("/nonexistent/path/config.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.ScanPaths) != 0 {
		t.Errorf("expected empty ScanPaths, got %v", cfg.ScanPaths)
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
	if cfg.Get("key1") != "val1" || cfg.Get("key2") != "val2" {
		t.Errorf("expected {key1:val1, key2:val2}, got Raw=%v", cfg.Raw)
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
	if cfg == nil {
		t.Fatal("expected non-nil config for corrupt JSON")
	}
	// Should have default values.
	if len(cfg.ScanPaths) != 0 {
		t.Errorf("expected empty ScanPaths for corrupt JSON, got %v", cfg.ScanPaths)
	}
}

func TestWriteConfig_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := defaultConfig()
	cfg.Set("cookie", "xf_session=abc")
	cfg.Set("steamgriddb-key", "my-secret-key")
	cfg.ScanPaths = []string{"/home/user/games", "/media/games"}
	cfg.DefaultDownloadDir = "/home/user/downloads"
	cfg.RateLimitDelay = 3

	if err := writeConfigTo(path, cfg); err != nil {
		t.Fatalf("writeConfigTo: %v", err)
	}

	// Read back.
	got, err := readConfigFrom(path)
	if err != nil {
		t.Fatalf("readConfigFrom: %v", err)
	}
	if got.Get("cookie") != "xf_session=abc" {
		t.Errorf("cookie = %q, want %q", got.Get("cookie"), "xf_session=abc")
	}
	if got.Get("steamgriddb-key") != "my-secret-key" {
		t.Errorf("steamgriddb-key = %q, want %q", got.Get("steamgriddb-key"), "my-secret-key")
	}
	if len(got.ScanPaths) != 2 || got.ScanPaths[0] != "/home/user/games" {
		t.Errorf("ScanPaths = %v, want [%q, %q]", got.ScanPaths, "/home/user/games", "/media/games")
	}
	if got.DefaultDownloadDir != "/home/user/downloads" {
		t.Errorf("DefaultDownloadDir = %q, want %q", got.DefaultDownloadDir, "/home/user/downloads")
	}
	if got.RateLimitDelay != 3 {
		t.Errorf("RateLimitDelay = %d, want 3", got.RateLimitDelay)
	}
}

func TestWriteConfig_Empty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := defaultConfig()
	if err := writeConfigTo(path, cfg); err != nil {
		t.Fatalf("writeConfigTo empty: %v", err)
	}
	got, err := readConfigFrom(path)
	if err != nil {
		t.Fatalf("readConfigFrom: %v", err)
	}
	if len(got.ScanPaths) != 0 {
		t.Errorf("expected empty ScanPaths, got %v", got.ScanPaths)
	}
}

func TestConfig_SetAndGet(t *testing.T) {
	t.Parallel()
	cfg := defaultConfig()

	// Typed fields.
	cfg.Set("scan_paths", "/path/one,/path/two")
	if len(cfg.ScanPaths) != 2 || cfg.ScanPaths[0] != "/path/one" {
		t.Errorf("ScanPaths = %v, want [%q, %q]", cfg.ScanPaths, "/path/one", "/path/two")
	}
	if cfg.Get("scan_paths") != "/path/one,/path/two" {
		t.Errorf("Get(scan_paths) = %q, want %q", cfg.Get("scan_paths"), "/path/one,/path/two")
	}

	cfg.Set("default_download_dir", "/downloads")
	if cfg.DefaultDownloadDir != "/downloads" {
		t.Errorf("DefaultDownloadDir = %q", cfg.DefaultDownloadDir)
	}
	if cfg.Get("default_download_dir") != "/downloads" {
		t.Errorf("Get(default_download_dir) = %q", cfg.Get("default_download_dir"))
	}

	cfg.Set("rate_limit_delay", "5")
	if cfg.RateLimitDelay != 5 {
		t.Errorf("RateLimitDelay = %d, want 5", cfg.RateLimitDelay)
	}
	if cfg.Get("rate_limit_delay") != "5" {
		t.Errorf("Get(rate_limit_delay) = %q", cfg.Get("rate_limit_delay"))
	}

	// Raw field.
	cfg.Set("custom-key", "custom-value")
	if cfg.Raw["custom-key"] != "custom-value" {
		t.Errorf("Raw[custom-key] = %q", cfg.Raw["custom-key"])
	}
	if cfg.Get("custom-key") != "custom-value" {
		t.Errorf("Get(custom-key) = %q", cfg.Get("custom-key"))
	}
}

func TestConfig_LegacyMigration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Write config in legacy map[string]string format.
	legacy := map[string]string{
		"cookie":         "xf_session=abc",
		"steamgriddb-key": "some-key",
	}
	data, _ := json.MarshalIndent(legacy, "", "  ")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := readConfigFrom(path)
	if err != nil {
		t.Fatalf("readConfigFrom: %v", err)
	}
	if cfg.Get("cookie") != "xf_session=abc" {
		t.Errorf("cookie = %q", cfg.Get("cookie"))
	}
	if cfg.Get("steamgriddb-key") != "some-key" {
		t.Errorf("steamgriddb-key = %q", cfg.Get("steamgriddb-key"))
	}
}

func TestConfig_Keys(t *testing.T) {
	t.Parallel()
	cfg := defaultConfig()
	cfg.Set("key1", "val1")
	cfg.Set("key2", "val2")
	cfg.ScanPaths = []string{"/path"}
	cfg.RateLimitDelay = 3

	keys := cfg.Keys()
	found := make(map[string]bool)
	for _, k := range keys {
		found[k] = true
	}
	if !found["key1"] {
		t.Error("expected key1 in Keys()")
	}
	if !found["key2"] {
		t.Error("expected key2 in Keys()")
	}
	if !found["scan_paths"] {
		t.Error("expected scan_paths in Keys()")
	}
	if !found["rate_limit_delay"] {
		t.Error("expected rate_limit_delay in Keys()")
	}
}

func TestConfig_Validate(t *testing.T) {
	t.Parallel()
	// Empty config should be valid.
	cfg := defaultConfig()
	if err := validate(cfg); err != nil {
		t.Errorf("expected nil error for empty config, got %v", err)
	}

	// Config with valid scan paths.
	cfg.ScanPaths = []string{"/path/one", "/path/two"}
	if err := validate(cfg); err != nil {
		t.Errorf("expected nil error for valid config, got %v", err)
	}

	// Config with empty scan path entry.
	cfg.ScanPaths = []string{""}
	if err := validate(cfg); err == nil {
		t.Error("expected error for empty scan path")
	}
}

func TestReadConfig_WithTypedFields(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	typed := map[string]any{
		"scan_paths":           []string{"/games/a", "/games/b"},
		"default_download_dir": "/downloads",
		"rate_limit_delay":     3,
		"cookie":               "abc123",
	}
	data, _ := json.MarshalIndent(typed, "", "  ")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := readConfigFrom(path)
	if err != nil {
		t.Fatalf("readConfigFrom: %v", err)
	}
	if len(cfg.ScanPaths) != 2 || cfg.ScanPaths[0] != "/games/a" {
		t.Errorf("ScanPaths = %v", cfg.ScanPaths)
	}
	if cfg.DefaultDownloadDir != "/downloads" {
		t.Errorf("DefaultDownloadDir = %q", cfg.DefaultDownloadDir)
	}
	if cfg.RateLimitDelay != 3 {
		t.Errorf("RateLimitDelay = %d", cfg.RateLimitDelay)
	}
	if cfg.Get("cookie") != "abc123" {
		t.Errorf("cookie = %q", cfg.Get("cookie"))
	}
}

func TestMarshalUnmarshal_RoundTrip(t *testing.T) {
	t.Parallel()
	orig := defaultConfig()
	orig.ScanPaths = []string{"/a", "/b"}
	orig.DefaultDownloadDir = "/dl"
	orig.RateLimitDelay = 7
	orig.Set("custom", "value")

	data, err := marshalConfig(orig)
	if err != nil {
		t.Fatalf("marshalConfig: %v", err)
	}

	got, err := unmarshalConfig(data)
	if err != nil {
		t.Fatalf("unmarshalConfig: %v", err)
	}

	if len(got.ScanPaths) != 2 || got.ScanPaths[0] != "/a" {
		t.Errorf("ScanPaths = %v", got.ScanPaths)
	}
	if got.DefaultDownloadDir != "/dl" {
		t.Errorf("DefaultDownloadDir = %q", got.DefaultDownloadDir)
	}
	if got.RateLimitDelay != 7 {
		t.Errorf("RateLimitDelay = %d", got.RateLimitDelay)
	}
	if got.Get("custom") != "value" {
		t.Errorf("custom = %q", got.Get("custom"))
	}
}
