package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mili/moxie/internal/log"
)

// ConfigDir returns the platform-standard configuration directory for moxie.
func ConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
		log.Warn("cannot determine home directory, using temp dir", "error", err, "fallback", home)
	}
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "moxie")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "moxie")
	default:
		return filepath.Join(home, ".config", "moxie")
	}
}

// LogDir returns the directory for per-day log files.
func LogDir() string {
	return filepath.Join(ConfigDir(), "logs")
}

// DbPath returns the path to the games database.
func DbPath() string {
	dir := ConfigDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Error("cannot create config directory for database", "dir", dir, "error", err)
	}
	return filepath.Join(dir, "games.db")
}

// ConfigPath returns the path to the JSON config file.
func ConfigPath() string {
	dir := ConfigDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Error("cannot create config directory", "dir", dir, "error", err)
	}
	return filepath.Join(dir, "config.json")
}

// EngineProfilesDir returns the path to custom engine detection profiles.
func EngineProfilesDir() string {
	return filepath.Join(ConfigDir(), "engines")
}

// Config holds typed configuration values alongside a generic key-value store
// for backward compatibility with arbitrary string-based keys.
type Config struct {
	// Typed fields (known schema).
	ScanPaths         []string `json:"scan_paths,omitempty"`
	DefaultDownloadDir string  `json:"default_download_dir,omitempty"`
	RateLimitDelay    int      `json:"rate_limit_delay,omitempty"` // seconds between requests

	// Raw holds arbitrary string key-value pairs for settings not covered by
	// typed fields (e.g., steamgriddb-key, cookie). These are serialized
	// alongside typed fields at the top level of the JSON document.
	Raw map[string]string `json:"-"`
}

// Get returns a config value by key. It checks typed fields first, then the
// Raw map. Returns empty string if not found.
func (c *Config) Get(key string) string {
	if c == nil {
		return ""
	}
	switch key {
	case "default_download_dir":
		return c.DefaultDownloadDir
	case "rate_limit_delay":
		if c.RateLimitDelay > 0 {
			return fmt.Sprintf("%d", c.RateLimitDelay)
		}
		return ""
	case "scan_paths":
		return strings.Join(c.ScanPaths, ",")
	default:
		if c.Raw != nil {
			return c.Raw[key]
		}
		return ""
	}
}

// Set sets a config value by key. It routes known typed fields to the struct
// and stores everything else in Raw.
func (c *Config) Set(key, value string) {
	if c == nil {
		return
	}
	switch key {
	case "default_download_dir":
		c.DefaultDownloadDir = value
	case "rate_limit_delay":
		if value == "" {
			c.RateLimitDelay = 0
		} else {
			if v, err := fmt.Sscanf(value, "%d", &c.RateLimitDelay); err != nil || v != 1 {
				c.RateLimitDelay = 0
			}
		}
	case "scan_paths":
		if value == "" {
			c.ScanPaths = nil
		} else {
			parts := strings.Split(value, ",")
			c.ScanPaths = make([]string, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					c.ScanPaths = append(c.ScanPaths, p)
				}
			}
		}
	default:
		if c.Raw == nil {
			c.Raw = make(map[string]string)
		}
		if value == "" {
			delete(c.Raw, key)
		} else {
			c.Raw[key] = value
		}
	}
}

// Keys returns all config keys (typed + raw).
func (c *Config) Keys() []string {
	var keys []string
	if c.DefaultDownloadDir != "" {
		keys = append(keys, "default_download_dir")
	}
	if c.RateLimitDelay > 0 {
		keys = append(keys, "rate_limit_delay")
	}
	if len(c.ScanPaths) > 0 {
		keys = append(keys, "scan_paths")
	}
	for k := range c.Raw {
		keys = append(keys, k)
	}
	return keys
}

// validate checks the config for obvious problems and returns any errors.
func validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config: config is nil")
	}
	for _, p := range cfg.ScanPaths {
		if strings.TrimSpace(p) == "" {
			return fmt.Errorf("config: scan path cannot be empty")
		}
	}
	return nil
}

// defaultConfig returns a Config with sensible defaults.
func defaultConfig() *Config {
	return &Config{
		Raw: make(map[string]string),
	}
}

// readConfigFrom reads JSON config from a specific path (for testing).
func readConfigFrom(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultConfig(), nil
		}
		return nil, err
	}
	return unmarshalConfig(data)
}

// writeConfigTo writes JSON config to a specific path (for testing).
func writeConfigTo(path string, cfg *Config) error {
	if err := validate(cfg); err != nil {
		return err
	}
	data, err := marshalConfig(cfg)
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

// marshalConfig serializes a Config to JSON. Typed fields appear at the top
// level alongside Raw entries. Empty/default values are omitted.
func marshalConfig(cfg *Config) ([]byte, error) {
	// Start with a map of typed fields.
	m := make(map[string]any)
	if len(cfg.ScanPaths) > 0 {
		m["scan_paths"] = cfg.ScanPaths
	}
	if cfg.DefaultDownloadDir != "" {
		m["default_download_dir"] = cfg.DefaultDownloadDir
	}
	if cfg.RateLimitDelay > 0 {
		m["rate_limit_delay"] = cfg.RateLimitDelay
	}
	// Add raw entries.
	for k, v := range cfg.Raw {
		m[k] = v
	}
	return json.MarshalIndent(m, "", "  ")
}

// unmarshalConfig deserializes JSON into a Config. Supports both the new typed
// format and the legacy map[string]string format for backward compatibility.
func unmarshalConfig(data []byte) (*Config, error) {
	// Try typed format first: unmarshal into a raw map, then extract known keys.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		// Corrupted JSON — return default.
		return defaultConfig(), nil
	}
	if raw == nil {
		return defaultConfig(), nil
	}

	cfg := defaultConfig()

	// Extract typed fields.
	if v, ok := raw["scan_paths"]; ok {
		switch arr := v.(type) {
		case []any:
			for _, item := range arr {
				if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
					cfg.ScanPaths = append(cfg.ScanPaths, s)
				}
			}
		case string:
			// Legacy format: comma-separated string.
			for _, p := range strings.Split(arr, ",") {
				p = strings.TrimSpace(p)
				if p != "" {
					cfg.ScanPaths = append(cfg.ScanPaths, p)
				}
			}
		}
		delete(raw, "scan_paths")
	}
	if v, ok := raw["default_download_dir"]; ok {
		if s, ok := v.(string); ok {
			cfg.DefaultDownloadDir = s
		}
		delete(raw, "default_download_dir")
	}
	if v, ok := raw["rate_limit_delay"]; ok {
		switch n := v.(type) {
		case float64:
			cfg.RateLimitDelay = int(n)
		case string:
			if n != "" {
				fmt.Sscanf(n, "%d", &cfg.RateLimitDelay)
			}
		}
		delete(raw, "rate_limit_delay")
	}

	// Remaining keys go into Raw.
	for k, v := range raw {
		if s, ok := v.(string); ok {
			cfg.Raw[k] = s
		}
	}

	return cfg, nil
}

// ReadConfig reads the JSON config file. Returns default config if the file
// does not exist or is corrupted. The returned Config is always valid.
func ReadConfig() (*Config, error) {
	return readConfigFrom(ConfigPath())
}

// WriteConfig serializes the config as JSON and writes it to the config file
// atomically. Validates the config before writing.
func WriteConfig(cfg *Config) error {
	if err := validate(cfg); err != nil {
		return err
	}
	return writeConfigTo(ConfigPath(), cfg)
}
