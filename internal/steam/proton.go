package steam

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/andygrunwald/vdf"
)

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// SetProtonVersion writes a Proton compatibility mapping for the given AppID
// into Steam's config.vdf (<steamRoot>/config/config.vdf).
//
// On non-Linux platforms, returns ErrNotLinux.
func SetProtonVersion(steamRoot string, appID uint32, protonVersion string) error {
	if !IsLinux() {
		return ErrNotLinux
	}

	// Validate the proton version against known patterns.
	if !isValidProton(protonVersion) {
		return fmt.Errorf("%w: %s", ErrInvalidProton, protonVersion)
	}

	configPath := filepath.Join(steamRoot, "config", "config.vdf")

	// Read existing config (or start fresh).
	cfg, err := readConfigVDF(configPath)
	if err != nil {
		return fmt.Errorf("steam: cannot read config.vdf: %w", err)
	}
	if cfg == nil {
		cfg = make(map[string]interface{})
	}

	// Ensure the InstallConfigStore → Software → Valve → Steam → CompatToolMapping
	// path exists, creating nested maps as needed.
	ics := getOrCreateMap(cfg, "InstallConfigStore")
	sw := getOrCreateMap(ics, "Software")
	valve := getOrCreateMap(sw, "Valve")
	steam := getOrCreateMap(valve, "Steam")
	ctm := getOrCreateMap(steam, "CompatToolMapping")

	// Add or update the mapping for this AppID.
	appKey := strconv.FormatUint(uint64(appID), 10)
	ctm[appKey] = map[string]interface{}{
		"name":     protonVersion,
		"config":   "",
		"Priority": "250",
	}

	return writeConfigVDF(configPath, cfg)
}

// GetProtonVersion reads the current Proton mapping for the given AppID.
// Returns ("", nil) if no mapping exists.
//
// On non-Linux platforms, returns ErrNotLinux.
func GetProtonVersion(steamRoot string, appID uint32) (string, error) {
	if !IsLinux() {
		return "", ErrNotLinux
	}

	configPath := filepath.Join(steamRoot, "config", "config.vdf")
	cfg, err := readConfigVDF(configPath)
	if err != nil || cfg == nil {
		return "", nil // no config file → no mapping
	}

	// Navigate to CompatToolMapping.
	ctm := getCompatToolMapping(cfg)
	if ctm == nil {
		return "", nil
	}

	appKey := strconv.FormatUint(uint64(appID), 10)
	entry, ok := ctm[appKey].(map[string]interface{})
	if !ok {
		return "", nil
	}

	if name, ok := entry["name"].(string); ok {
		return name, nil
	}
	return "", nil
}

// RemoveProtonVersion removes the Proton mapping for the given AppID.
// Returns nil if no mapping exists (idempotent).
func RemoveProtonVersion(steamRoot string, appID uint32) error {
	if !IsLinux() {
		return ErrNotLinux
	}

	configPath := filepath.Join(steamRoot, "config", "config.vdf")
	cfg, err := readConfigVDF(configPath)
	if err != nil || cfg == nil {
		return nil // no config → nothing to remove
	}

	ctm := getCompatToolMapping(cfg)
	if ctm == nil {
		return nil
	}

	appKey := strconv.FormatUint(uint64(appID), 10)
	delete(ctm, appKey)
	return writeConfigVDF(configPath, cfg)
}

// ListProtonVersions scans Steam's compatibilitytools.d directory and
// steamapps/common/Proton* for installed Proton versions.
// Falls back to KnownProtonVersions if scanning finds nothing.
//
// On non-Linux platforms, returns ErrNotLinux.
func ListProtonVersions(steamRoot string) ([]string, error) {
	if !IsLinux() {
		return nil, ErrNotLinux
	}

	seen := make(map[string]bool)
	var versions []string

	// Scan compatibilitytools.d (GE-Proton, custom tools).
	customDir := filepath.Join(steamRoot, "compatibilitytools.d")
	scanProtonDir(customDir, &versions, seen)

	// Scan official Proton versions from steamapps/common.
	commonDir := filepath.Join(steamRoot, "steamapps", "common")
	entries, err := os.ReadDir(commonDir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(e.Name(), "Proton ") {
				name := strings.TrimPrefix(e.Name(), "Proton ")
				name = strings.ReplaceAll(name, " ", "_")
				name = "proton_" + name
				if !seen[name] {
					seen[name] = true
					versions = append(versions, name)
				}
			}
		}
	}

	// If nothing found, return a copy of the known defaults.
	if len(versions) == 0 {
		cp := make([]string, len(KnownProtonVersions))
		copy(cp, KnownProtonVersions)
		return cp, nil
	}

	sort.Strings(versions)
	return versions, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// readConfigVDF parses a text VDF file into a map.
// Returns (nil, nil) if the file does not exist.
func readConfigVDF(path string) (map[string]interface{}, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	return vdf.NewParser(f).Parse()
}

// writeConfigVDF serializes and writes a text VDF config file using a
// backup-then-atomic-write pattern to prevent corruption.
func writeConfigVDF(path string, cfg map[string]interface{}) error {
	// Backup existing file.
	if existing, err := os.ReadFile(path); err == nil {
		backup := path + ".backup-" + time.Now().UTC().Format("20060102T150405Z")
		if err := os.WriteFile(backup, existing, 0644); err != nil {
			return fmt.Errorf("steam: cannot backup config.vdf: %w", err)
		}
	}

	// Serialize to temp file.
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "config-*.vdf")
	if err != nil {
		return fmt.Errorf("steam: cannot create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := encodeVDF(tmp, cfg); err != nil {
		tmp.Close()
		return fmt.Errorf("steam: cannot encode config.vdf: %w", err)
	}
	tmp.Close()

	// Atomic rename.
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("steam: cannot rename temp file: %w", err)
	}

	// Match original permissions.
	if err := os.Chmod(path, 0644); err != nil {
		return fmt.Errorf("steam: cannot set permissions: %w", err)
	}

	return nil
}

// encodeVDF serializes a map[string]interface{} as text VDF to w.
func encodeVDF(w io.Writer, m map[string]interface{}) error {
	buf := new(strings.Builder)
	writeVDFMap(buf, m, 0)
	_, err := io.WriteString(w, buf.String())
	return err
}

// writeVDFMap recursively writes a VDF map with the given indentation level.
// Keys are sorted for deterministic output.
func writeVDFMap(buf *strings.Builder, m map[string]interface{}, indent int) {
	tab := strings.Repeat("\t", indent)

	// Sort keys for deterministic output.
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := m[k]
		switch val := v.(type) {
		case map[string]interface{}:
			// Nested map: output as block.
			buf.WriteString(fmt.Sprintf("%s\"%s\"\n%s{\n", tab, vdfEscape(k), tab))
			writeVDFMap(buf, val, indent+1)
			buf.WriteString(fmt.Sprintf("%s}\n", tab))
		case string:
			// Simple key-value pair.
			buf.WriteString(fmt.Sprintf("%s\"%s\"\t\t\"%s\"\n", tab, vdfEscape(k), vdfEscape(val)))
		}
	}
}

// vdfEscape escapes special characters (backslash and double-quote)
// for use inside a VDF quoted string.
func vdfEscape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

// getOrCreateMap returns the existing sub-map at key, or creates a new
// empty map and stores it at key before returning it.
func getOrCreateMap(m map[string]interface{}, key string) map[string]interface{} {
	if existing, ok := m[key].(map[string]interface{}); ok {
		return existing
	}
	newMap := make(map[string]interface{})
	m[key] = newMap
	return newMap
}

// getCompatToolMapping navigates to the CompatToolMapping block in config.vdf.
// Returns nil if any segment of the path is missing.
func getCompatToolMapping(cfg map[string]interface{}) map[string]interface{} {
	ics, _ := cfg["InstallConfigStore"].(map[string]interface{})
	if ics == nil {
		return nil
	}
	sw, _ := ics["Software"].(map[string]interface{})
	if sw == nil {
		return nil
	}
	valve, _ := sw["Valve"].(map[string]interface{})
	if valve == nil {
		return nil
	}
	steamMap, _ := valve["Steam"].(map[string]interface{})
	if steamMap == nil {
		return nil
	}
	ctm, _ := steamMap["CompatToolMapping"].(map[string]interface{})
	return ctm
}

// isValidProton checks if the given version string matches a known pattern.
// Accepts "proton_*", "GE-Proton*", and "none".
// Rejects version strings containing whitespace — actual Proton identifiers
// never include spaces (e.g. "Proton 9.0" with a space is not valid).
func isValidProton(version string) bool {
	if version == "none" {
		return true
	}
	// Proton identifiers never contain whitespace.
	if strings.ContainsAny(version, " \t\r\n") {
		return false
	}
	lower := strings.ToLower(version)
	if strings.HasPrefix(lower, "proton") {
		return true
	}
	if strings.HasPrefix(lower, "ge-proton") {
		return true
	}
	return false
}

// scanProtonDir scans a directory for subdirectories that contain a
// compatibilitytool.vdf file, adding the tool directory name to the
// version list.
func scanProtonDir(dir string, versions *[]string, seen map[string]bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		tf := filepath.Join(dir, e.Name(), "compatibilitytool.vdf")
		if _, err := os.Stat(tf); err != nil {
			continue
		}
		name := e.Name()
		if !seen[name] {
			seen[name] = true
			*versions = append(*versions, name)
		}
	}
}
