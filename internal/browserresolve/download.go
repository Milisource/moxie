package browserresolve

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// downloadState is the phase of a browser download, tracked from CDP
// download events.
type downloadState int

const (
	// downloadIdle: no download seen yet.
	downloadIdle downloadState = iota
	// downloadWillBegin: Browser/Page.downloadWillBegin fired (GUID known).
	downloadWillBegin
	// downloadInProgress: progress events flowing (may skip WillBegin —
	// see rod#971).
	downloadInProgress
	// downloadCompleted: terminal, file fully written.
	downloadCompleted
	// downloadCanceled: terminal, no usable file.
	downloadCanceled
)

// downloadSnapshot is a consistent view of a downloadTracker.
type downloadSnapshot struct {
	state     downloadState
	guid      string // Chrome's GUID for the download (allowAndName names the file by it)
	suggested string // server-suggested filename, may be empty
	received  int64  // bytes received so far
	total     int64  // expected total (0 = unknown)
	err       error  // set when canceled
}

// completed reports whether the download reached a terminal success state.
func (s downloadSnapshot) completed() bool { return s.state == downloadCompleted }

// downloadTracker accumulates CDP download events (both the deprecated
// Page.* and current Browser.* families) into a single state machine. It is
// safe for concurrent use: events arrive from rod's event loop while the
// caller waits on the tracker.
type downloadTracker struct {
	mu        sync.Mutex
	state     downloadState
	guid      string
	suggested string
	received  int64
	total     int64
	err       error
}

func newDownloadTracker() *downloadTracker {
	return &downloadTracker{state: downloadIdle}
}

// onWillBegin records Browser/Page.downloadWillBegin. The first event wins;
// later events (a second download racing the first) are ignored.
func (t *downloadTracker) onWillBegin(guid, suggested string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.state != downloadIdle {
		return
	}
	t.state = downloadWillBegin
	t.guid = guid
	t.suggested = suggested
}

// onProgress records Browser/Page.downloadProgress. Progress may arrive
// before downloadWillBegin (rod#971: some flows never emit it) — the GUID is
// adopted so completion can still be signaled and the file located.
func (t *downloadTracker) onProgress(guid string, st downloadState, received, total float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.guid == "" {
		t.guid = guid
	} else if guid != t.guid {
		// Events from another download racing the tracked one must not
		// affect it (first-wins GUID).
		return
	}
	if t.state < downloadInProgress {
		t.state = downloadInProgress
	}
	t.received = int64(received)
	t.total = int64(total)
	switch st {
	case downloadCompleted:
		t.state = downloadCompleted
	case downloadCanceled:
		t.state = downloadCanceled
		if t.err == nil {
			t.err = errors.New("download canceled by the server or browser")
		}
	}
}

// finished reports whether the tracked download reached a terminal state.
func (t *downloadTracker) finished() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.state == downloadCompleted || t.state == downloadCanceled
}

// snapshot returns a consistent copy of the tracker state.
func (t *downloadTracker) snapshot() downloadSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	return downloadSnapshot{
		state:     t.state,
		guid:      t.guid,
		suggested: t.suggested,
		received:  t.received,
		total:     t.total,
		err:       t.err,
	}
}

// verifyDownloadedFile sanity-checks a finished browser download before it
// is moved into the destination game directory: the file must exist, be
// non-empty, not look like an HTML challenge page, and match the size the
// browser reported when one was reported.
func verifyDownloadedFile(path string, minSize, expectedSize int64) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDownload, err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("%w: file %q is empty", ErrDownload, path)
	}
	if minSize > 0 && info.Size() < minSize {
		return fmt.Errorf("%w: file %q is %d bytes, below the %d-byte minimum", ErrDownload, path, info.Size(), minSize)
	}
	if expectedSize > 0 && info.Size() != expectedSize {
		return fmt.Errorf("%w: size mismatch for %q: %d bytes on disk, browser reported %d", ErrDownload, path, info.Size(), expectedSize)
	}
	if looksLikeHTML(path) {
		return fmt.Errorf("%w: downloaded content of %q looks like an HTML page (%d bytes), not a file", ErrChallengePage, path, info.Size())
	}
	return nil
}

// looksLikeHTML sniffs the head of a file for the signature of an HTML
// challenge page (leading BOM/whitespace tolerated). A server that answers a
// download URL with HTML is presenting a CAPTCHA or bot check the automated
// session could not clear.
func looksLikeHTML(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := io.ReadFull(f, buf) // short files return n bytes + ErrUnexpectedEOF; n is authoritative
	head := strings.TrimPrefix(string(buf[:n]), "\xef\xbb\xbf")
	head = strings.ToLower(strings.TrimSpace(head))
	return strings.HasPrefix(head, "<!doctype html") || strings.HasPrefix(head, "<html")
}

// sanitizeFilename turns a server-suggested filename into a safe basename:
// directory components, control characters, and leading dots are stripped.
// Returns "" when nothing usable remains (callers fall back to the on-disk
// name).
func sanitizeFilename(name string) string {
	// Normalize backslashes first so Windows-style paths are handled the
	// same on every OS, then keep only the final path element.
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.TrimSpace(name)
	name = strings.Map(func(r rune) rune {
		switch {
		case r < 0x20 || r == 0x7f:
			return -1
		case r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' ||
			r == '"' || r == '<' || r == '>' || r == '|':
			return -1
		}
		return r
	}, name)
	name = strings.TrimLeft(name, ".")
	if name == "" || name == "." {
		return ""
	}
	return name
}

// moveFile moves src into destDir under name, creating destDir if needed
// and resolving name collisions with "name (1).ext" style suffixes. When a
// rename is impossible (cross-device temp dir, Windows sharing violation)
// the file is copied and the source removed. Returns the final path.
func moveFile(src, destDir, name string) (string, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("browserresolve: creating destination dir: %w", err)
	}
	dest := filepath.Join(destDir, name)
	used := false
	for i := 1; i < 1000; i++ {
		if _, err := os.Lstat(dest); errors.Is(err, fs.ErrNotExist) {
			used = true
			break
		}
		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		dest = filepath.Join(destDir, fmt.Sprintf("%s (%d)%s", base, i, ext))
	}
	if !used {
		return "", fmt.Errorf("browserresolve: no free name for %q in %q", name, destDir)
	}

	if err := os.Rename(src, dest); err == nil {
		return dest, nil
	}

	if err := copyFile(src, dest); err != nil {
		return "", fmt.Errorf("browserresolve: moving download into %q: %w", destDir, err)
	}
	_ = os.Remove(src)
	return dest, nil
}
