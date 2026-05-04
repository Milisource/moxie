package steam

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	vdf "github.com/wakeful-cloud/vdf"
)

// shortcutKnownKeys is the set of VDF keys explicitly handled by ShortcutEntry.
// Any key NOT in this set is preserved in RawFields for round-trip safety.
var shortcutKnownKeys = map[string]bool{
	"appid": true, "AppName": true, "exe": true, "StartDir": true,
	"icon": true, "LaunchOptions": true, "IsHidden": true,
	"AllowDesktopConfig": true, "AllowOverlay": true, "OpenVR": true,
	"Devkit": true, "LastPlayTime": true, "FlatpakAppID": true,
	"sortas": true, "tags": true,
}

// ---------------------------------------------------------------------------
// Read / Write
// ---------------------------------------------------------------------------

// ReadShortcuts reads and parses the binary VDF shortcuts file at the given path.
// Returns a slice of ShortcutEntry values. On file-not-exists, returns an empty slice.
func ReadShortcuts(path string) ([]ShortcutEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // empty — file doesn't exist yet
		}
		return nil, fmt.Errorf("steam: cannot read shortcuts.vdf: %w", err)
	}

	m, err := vdf.ReadVdf(data)
	if err != nil {
		return nil, fmt.Errorf("steam: cannot parse shortcuts.vdf: %w", err)
	}

	return parseShortcutsMap(m), nil
}

// WriteShortcuts serializes the shortcuts slice back to binary VDF format and
// writes it to the given path.
//
// SAFETY: Creates a backup of the original file as "<path>.backup" before
// writing. This function refuses to write if Steam is currently running.
func WriteShortcuts(path string, shortcuts []ShortcutEntry) error {
	// Guard: prevent Steam from overwriting moxie's changes on shutdown.
	if running, _ := IsSteamRunning(); running {
		return ErrSteamRunning
	}

	// Backup existing file before overwriting.
	// Uses a fixed name so only one backup is kept; avoids unbounded accumulation.
	if _, err := os.Stat(path); err == nil {
		backup := path + ".backup"
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("steam: cannot backup shortcuts.vdf: %w", err)
		}
		if err := os.WriteFile(backup, data, 0644); err != nil {
			return fmt.Errorf("steam: cannot write backup: %w", err)
		}
	}

	// Serialize to binary VDF.
	m := buildShortcutsMap(shortcuts)
	data, err := vdf.WriteVdf(m)
	if err != nil {
		return fmt.Errorf("steam: cannot encode shortcuts.vdf: %w", err)
	}

	// Write to temp file, then rename atomically.
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "shortcuts-*.vdf")
	if err != nil {
		return fmt.Errorf("steam: cannot create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("steam: cannot write temp file: %w", err)
	}
	// fsync before rename to prevent empty/corrupt file on crash.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	tmp.Close()

	// Atomic rename.
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("steam: cannot rename temp file: %w", err)
	}

	if err := os.Chmod(path, 0644); err != nil {
		return fmt.Errorf("steam: cannot set permissions: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// CRUD operations
// ---------------------------------------------------------------------------

// AddGame appends a shortcut to the list. It generates the AppID via
// GenerateAppID before adding if the AppID is zero.
//
// Returns ErrDuplicate if a shortcut with the same title already exists.
func AddGame(shortcuts *[]ShortcutEntry, entry *ShortcutEntry) error {
	for _, s := range *shortcuts {
		if strings.EqualFold(s.AppName, entry.AppName) {
			return ErrDuplicate
		}
	}

	// Generate AppID if not set.
	if entry.AppID == 0 {
		entry.AppID = GenerateAppID(entry.Exe, entry.AppName)
	}

	// Ensure F95Zone is the first tag.
	tags := []string{"F95Zone"}
	for _, t := range entry.Tags {
		if t != "F95Zone" {
			tags = append(tags, t)
		}
	}
	if len(tags) > 4 {
		tags = tags[:4]
	}
	entry.Tags = tags

	*shortcuts = append(*shortcuts, *entry)
	return nil
}

// RemoveGame removes a shortcut by its AppID. Returns nil if the AppID
// is not found (idempotent).
func RemoveGame(shortcuts *[]ShortcutEntry, appID uint32) error {
	for i, s := range *shortcuts {
		if s.AppID == appID {
			*shortcuts = append((*shortcuts)[:i], (*shortcuts)[i+1:]...)
			return nil
		}
	}
	return nil
}

// FindGame searches for a shortcut by title (case-insensitive, exact match).
// Returns nil if not found.
func FindGame(shortcuts []ShortcutEntry, title string) *ShortcutEntry {
	for i, s := range shortcuts {
		if strings.EqualFold(s.AppName, title) {
			return &shortcuts[i]
		}
	}
	return nil
}

// FindGameByAppID searches for a shortcut by its generated AppID.
func FindGameByAppID(shortcuts []ShortcutEntry, appID uint32) *ShortcutEntry {
	for i, s := range shortcuts {
		if s.AppID == appID {
			return &shortcuts[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Binary VDF conversion helpers
// ---------------------------------------------------------------------------

// parseShortcutsMap converts a vdf.Map into a []ShortcutEntry slice.
func parseShortcutsMap(m vdf.Map) []ShortcutEntry {
	shortcuts, ok := m["shortcuts"].(vdf.Map)
	if !ok {
		return nil
	}

	var entries []ShortcutEntry
	// Collect and sort numeric keys to handle non-contiguous indices.
	var keys []int
	for k := range shortcuts {
		if idx, err := strconv.Atoi(k); err == nil {
			keys = append(keys, idx)
		}
	}
	sort.Ints(keys)
	for _, idx := range keys {
		key := strconv.Itoa(idx)
		if sm, ok := shortcuts[key].(vdf.Map); ok {
			entries = append(entries, parseShortcutEntry(sm))
		}
	}
	return entries
}

// parseShortcutEntry converts a single vdf.Map into a ShortcutEntry.
func parseShortcutEntry(m vdf.Map) ShortcutEntry {
	se := ShortcutEntry{
		AppID:              getUint32(m, "appid"),
		AppName:            getString(m, "AppName"),
		Exe:                stripQuotes(getString(m, "exe")),
		StartDir:           stripQuotes(getString(m, "StartDir")),
		Icon:               getString(m, "icon"),
		LaunchOptions:      getString(m, "LaunchOptions"),
		IsHidden:           getBool(m, "IsHidden"),
		AllowDesktopConfig: getBool(m, "AllowDesktopConfig"),
		AllowOverlay:       getBool(m, "AllowOverlay"),
		OpenVR:             getBool(m, "OpenVR"),
		Devkit:             getBool(m, "Devkit"),
		LastPlayTime:       getUint32(m, "LastPlayTime"),
		FlatpakAppID:       getString(m, "FlatpakAppID"),
		SortAs:             getString(m, "sortas"),
		Tags:               parseTags(m),
	}

	// Preserve unknown keys for round-trip safety.
	for k, v := range m {
		if !shortcutKnownKeys[k] {
			if se.RawFields == nil {
				se.RawFields = make(map[string]interface{})
			}
			se.RawFields[k] = v
		}
	}

	return se
}

// buildShortcutsMap converts a []ShortcutEntry into the vdf.Map format
// expected by vdf.WriteVdf.
func buildShortcutsMap(shortcuts []ShortcutEntry) vdf.Map {
	shortcutsMap := vdf.Map{}
	for i, s := range shortcuts {
		sm := vdf.Map{
			"appid":              s.AppID,
			"AppName":            s.AppName,
			"exe":                quotePath(s.Exe),
			"StartDir":           quotePath(s.StartDir),
			"icon":               s.Icon,
			"LaunchOptions":      s.LaunchOptions,
			"IsHidden":           boolToUint32(s.IsHidden),
			"AllowDesktopConfig": boolToUint32(s.AllowDesktopConfig),
			"AllowOverlay":       boolToUint32(s.AllowOverlay),
			"OpenVR":             boolToUint32(s.OpenVR),
			"Devkit":             boolToUint32(s.Devkit),
			"LastPlayTime":       s.LastPlayTime,
			"FlatpakAppID":       s.FlatpakAppID,
			"sortas":             s.SortAs,
			"tags":               buildTagsMap(s.Tags),
		}
		mergeRawFields(sm, s.RawFields)
		shortcutsMap[fmt.Sprintf("%d", i)] = sm
	}
	return vdf.Map{"shortcuts": shortcutsMap}
}

func buildTagsMap(tags []string) vdf.Map {
	tm := vdf.Map{}
	for i, t := range tags {
		tm[fmt.Sprintf("%d", i)] = vdf.Map{
			fmt.Sprintf("%d", i): t,
		}
	}
	return tm
}

// mergeRawFields copies any preserved unknown fields from RawFields into the
// output map, overriding nothing that's already set.
func mergeRawFields(m vdf.Map, raw map[string]interface{}) {
	for k, v := range raw {
		if _, exists := m[k]; !exists {
			m[k] = v
		}
	}
}

func parseTags(m vdf.Map) []string {
	tags, ok := m["tags"].(vdf.Map)
	if !ok {
		return nil
	}
	var result []string
	var tagKeys []int
	for k := range tags {
		if idx, err := strconv.Atoi(k); err == nil {
			tagKeys = append(tagKeys, idx)
		}
	}
	sort.Ints(tagKeys)
	for _, idx := range tagKeys {
		key := strconv.Itoa(idx)
		item, ok := tags[key].(vdf.Map)
		if !ok {
			continue
		}
		if v, ok := item[key].(string); ok {
			result = append(result, v)
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// Low-level VDF helpers
// ---------------------------------------------------------------------------

func getString(m vdf.Map, key string) string {
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

func getUint32(m vdf.Map, key string) uint32 {
	switch v := m[key].(type) {
	case uint32:
		return v
	case uint64:
		return uint32(v)
	case int32:
		return uint32(v)
	case int:
		return uint32(v)
	case float64:
		return uint32(v)
	}
	return 0
}

func getBool(m vdf.Map, key string) bool {
	return getUint32(m, key) != 0
}

func boolToUint32(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

// quotePath wraps a path in double quotes for the binary VDF format.
// Steam's shortcuts.vdf stores exe/StartDir paths with surrounding quotes.
func quotePath(path string) string {
	if path == "" {
		return ""
	}
	return `"` + path + `"`
}

// stripQuotes removes surrounding double quotes from a path read from VDF.
func stripQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
