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
