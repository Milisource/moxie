package scraper

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// associationCache maps sanitized game titles to F95Zone thread IDs.
// This provides a reliable shortcut for auto-association: once a thread is
// correctly identified (via search or manual URL), future auto-runs use the
// cached ID instead of re-searching, which can return wrong threads.
//
// The cache is stored as a JSON file at AssociationCachePath() in the config
// directory. Keys are SanitizeTitle(title), values are thread IDs (int64).
type associationCache struct {
	mu      sync.RWMutex
	path    string
	entries map[string]int64
	dirty   bool
}

var globalCache = &associationCache{
	entries: make(map[string]int64),
}

// AssociationCachePath returns the path to the persistent association cache file.
func AssociationCachePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "moxie", "associations.json")
}

// LoadAssociationCache reads the persistent cache from disk.
// Safe to call multiple times — subsequent calls are no-ops if already loaded.
func LoadAssociationCache() error {
	globalCache.mu.Lock()
	defer globalCache.mu.Unlock()

	if globalCache.path != "" {
		// Already loaded. When dirty, a previous SaveAssociationCache failed
		// and the in-memory entries are newer than the file — keep them
		// instead of re-reading the file, which would drop the unsaved
		// updates.
		return nil
	}

	path := AssociationCachePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			globalCache.path = path
			globalCache.entries = make(map[string]int64)
			return nil
		}
		return err
	}

	var entries map[string]int64
	if err := json.Unmarshal(data, &entries); err != nil {
		// Corrupted cache — start fresh.
		globalCache.entries = make(map[string]int64)
		globalCache.path = path
		return nil
	}
	if entries == nil {
		entries = make(map[string]int64)
	}
	globalCache.entries = entries
	globalCache.path = path
	globalCache.dirty = false
	return nil
}

// SaveAssociationCache writes the cache to disk if it has been modified.
func SaveAssociationCache() error {
	globalCache.mu.Lock()
	defer globalCache.mu.Unlock()

	if !globalCache.dirty {
		return nil
	}

	if globalCache.path == "" {
		globalCache.path = AssociationCachePath()
	}

	// Ensure the config directory exists.
	dir := filepath.Dir(globalCache.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(globalCache.entries, "", "  ")
	if err != nil {
		return err
	}

	// Atomic write via temp file + rename.
	tmp, err := os.CreateTemp(dir, "associations-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	// fsync before the rename so a crash cannot leave a truncated cache at
	// the final path, and restrict permissions before the file is visible
	// under its final name (the cache contains game/thread associations).
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	if err := os.Rename(tmpName, globalCache.path); err != nil {
		return err
	}

	globalCache.dirty = false
	return nil
}

// GetCachedThreadID returns the cached thread ID for a sanitized game title.
// Returns 0 if no cached entry exists. LoadAssociationCache must be called
// before this function.
func GetCachedThreadID(sanitizedTitle string) int64 {
	globalCache.mu.RLock()
	defer globalCache.mu.RUnlock()
	return globalCache.entries[sanitizedTitle]
}

// SetCachedThreadID stores a thread ID for a sanitized game title in the
// cache. The cache is marked dirty and will be saved on the next call to
// SaveAssociationCache.
func SetCachedThreadID(sanitizedTitle string, threadID int64) {
	globalCache.mu.Lock()
	defer globalCache.mu.Unlock()
	globalCache.entries[sanitizedTitle] = threadID
	globalCache.dirty = true
}

// DeleteCachedThreadID removes the cached thread ID for a sanitized game
// title. Use it when a cached association turns out to be stale — the
// thread was privated, moved, or deleted (CacheFullThread returns
// ErrThreadNotFound) — so a wrong mapping cannot persist across runs. The
// cache is marked dirty and will be saved on the next call to
// SaveAssociationCache. Deleting a key that is not present is a no-op.
func DeleteCachedThreadID(sanitizedTitle string) {
	globalCache.mu.Lock()
	defer globalCache.mu.Unlock()
	if _, ok := globalCache.entries[sanitizedTitle]; !ok {
		return
	}
	delete(globalCache.entries, sanitizedTitle)
	globalCache.dirty = true
}

// deleteCachedThreadIDByThread removes every cached entry that maps to the
// given (now dead) thread ID. CacheFullThread calls this when the cache API
// reports a thread as gone, so every game associated with that thread is
// re-associated on the next sync instead of being pinned to a stale mapping.
// The cache is marked dirty and will be saved on the next call to
// SaveAssociationCache. Deleting nothing is a no-op.
func deleteCachedThreadIDByThread(threadID int64) {
	globalCache.mu.Lock()
	defer globalCache.mu.Unlock()
	for title, id := range globalCache.entries {
		if id == threadID {
			delete(globalCache.entries, title)
			globalCache.dirty = true
		}
	}
}
