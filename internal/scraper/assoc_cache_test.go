package scraper

import (
	"testing"
)

// The association cache is a package-level singleton, so these tests must
// not run in parallel with each other and must restore the global state
// they displace. Other tests in the package never touch globalCache.
func withFreshCache(t *testing.T, fn func()) {
	t.Helper()
	globalCache.mu.Lock()
	origPath, origEntries, origDirty := globalCache.path, globalCache.entries, globalCache.dirty
	globalCache.path = ""
	globalCache.entries = make(map[string]int64)
	globalCache.dirty = false
	globalCache.mu.Unlock()

	t.Cleanup(func() {
		globalCache.mu.Lock()
		globalCache.path = origPath
		globalCache.entries = origEntries
		globalCache.dirty = origDirty
		globalCache.mu.Unlock()
	})

	fn()
}

func TestAssociationCache_SetGetDelete(t *testing.T) {
	withFreshCache(t, func() {
		SetCachedThreadID("Summertime Saga", 12345)
		if got := GetCachedThreadID("Summertime Saga"); got != 12345 {
			t.Errorf("GetCachedThreadID = %d, want 12345", got)
		}
		if !globalCache.dirty {
			t.Error("SetCachedThreadID must mark the cache dirty")
		}

		DeleteCachedThreadID("Summertime Saga")
		if got := GetCachedThreadID("Summertime Saga"); got != 0 {
			t.Errorf("GetCachedThreadID after delete = %d, want 0", got)
		}
	})
}

func TestAssociationCache_GetMissingReturnsZero(t *testing.T) {
	withFreshCache(t, func() {
		if got := GetCachedThreadID("no such game"); got != 0 {
			t.Errorf("GetCachedThreadID(missing) = %d, want 0", got)
		}
	})
}

func TestAssociationCache_DeleteMissingIsNoOp(t *testing.T) {
	withFreshCache(t, func() {
		// Deleting an absent key must not mark the cache dirty (no point
		// rewriting the file for nothing).
		DeleteCachedThreadID("absent")
		if globalCache.dirty {
			t.Error("deleting an absent key must not mark the cache dirty")
		}
		if len(globalCache.entries) != 0 {
			t.Errorf("entries = %v, want empty", globalCache.entries)
		}
	})
}

// Regression: when a previous SaveAssociationCache failed, the in-memory
// entries are newer than the file. LoadAssociationCache must prefer them
// over re-reading the file, which would silently drop the unsaved updates.
func TestLoadAssociationCache_KeepsDirtyInMemory(t *testing.T) {
	withFreshCache(t, func() {
		globalCache.mu.Lock()
		globalCache.path = "/tmp/moxie-test/associations.json" // non-empty → already loaded
		globalCache.entries = map[string]int64{"Game": 42}
		globalCache.dirty = true // previous save failed
		globalCache.mu.Unlock()

		if err := LoadAssociationCache(); err != nil {
			t.Fatalf("LoadAssociationCache: %v", err)
		}

		if got := GetCachedThreadID("Game"); got != 42 {
			t.Errorf("dirty in-memory entry lost on re-load: GetCachedThreadID = %d, want 42", got)
		}
		if !globalCache.dirty {
			t.Error("dirty flag must survive a failed save + re-load")
		}
	})
}

func TestAssociationCache_DeleteByThreadID(t *testing.T) {
	withFreshCache(t, func() {
		SetCachedThreadID("Game One", 777)
		SetCachedThreadID("Game Two", 777)
		SetCachedThreadID("Game Three", 888)

		deleteCachedThreadIDByThread(777)

		if got := GetCachedThreadID("Game One"); got != 0 {
			t.Errorf("Game One still cached as %d, want 0", got)
		}
		if got := GetCachedThreadID("Game Two"); got != 0 {
			t.Errorf("Game Two still cached as %d, want 0", got)
		}
		if got := GetCachedThreadID("Game Three"); got != 888 {
			t.Errorf("Game Three = %d, want 888 (different thread must survive)", got)
		}
		if !globalCache.dirty {
			t.Error("deleteCachedThreadIDByThread must mark the cache dirty")
		}
	})
}
