package downloader

import (
	"sync"
	"time"
)

// ResolutionStore persists successful masked-link unmaskings between runs.
// The db.Database type satisfies this interface; the downloader package keeps
// it abstract to avoid coupling the resolver to the SQLite layer.
type ResolutionStore interface {
	GetResolvedURL(maskedURL string, maxAge time.Duration) (resolvedURL, resolvedHost string, ok bool)
	SaveResolvedURL(maskedURL, resolvedURL, resolvedHost string) error
}

var (
	resolutionStoreMu sync.RWMutex
	resolutionStore   ResolutionStore
)

// SetResolutionStore installs the persistent cache used for masked links.
func SetResolutionStore(store ResolutionStore) {
	resolutionStoreMu.Lock()
	resolutionStore = store
	resolutionStoreMu.Unlock()
}

func getResolutionStore() ResolutionStore {
	resolutionStoreMu.RLock()
	defer resolutionStoreMu.RUnlock()
	return resolutionStore
}
