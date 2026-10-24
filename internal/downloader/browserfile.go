package downloader

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mili/moxie/internal/log"
)

// browserFileScheme marks a URL that names a file a browser session already
// downloaded. browserresolve returns one when the human-assisted browser
// window downloaded the file itself: re-fetching the destination URL would
// either download it twice or fail outright (one-time/session-bound URLs), so
// the finished file is handed over instead.
//
// The token is random and consumed once. A URL scraped from a thread cannot
// forge one, so this cannot be used to read arbitrary local files.
const browserFileScheme = "moxie-browser-file://"

// browserFile is a finished browser download waiting to be adopted.
type browserFile struct {
	path    string
	name    string
	created time.Time
}

// browserFileTTL drops handoffs that were never consumed (e.g. the download
// failed before reaching downloadWithHeaders) so their temp files do not pile
// up for the life of the process.
const browserFileTTL = 6 * time.Hour

var browserFiles = struct {
	sync.Mutex
	byToken map[string]browserFile
}{byToken: map[string]browserFile{}}

// RegisterBrowserFile hands a finished browser download to the downloader.
// It returns the opaque URL the caller (browserresolve) should return in place
// of the destination URL; the downloader adopts the file and removes its temp
// directory. The name is the server-suggested filename and may be empty.
func RegisterBrowserFile(path, name string) string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// crypto/rand does not fail on supported platforms; keep the handoff
		// working anyway instead of dropping a finished download.
		raw = [16]byte{}
		for i := range raw {
			raw[i] = byte(time.Now().UnixNano() >> (8 * i))
		}
	}
	token := browserFileScheme + hex.EncodeToString(raw[:])
	browserFiles.Lock()
	pruneBrowserFilesLocked()
	browserFiles.byToken[token] = browserFile{path: path, name: name, created: time.Now()}
	browserFiles.Unlock()
	log.Debug("browser download handed to the downloader", "path", path, "name", name)
	return token
}

// pruneBrowserFilesLocked drops expired handoffs and their temp dirs. The
// caller holds browserFiles' lock.
func pruneBrowserFilesLocked() {
	cutoff := time.Now().Add(-browserFileTTL)
	for token, bf := range browserFiles.byToken {
		if bf.created.After(cutoff) {
			continue
		}
		delete(browserFiles.byToken, token)
		if dir := browserFileDir(bf.path); dir != "" {
			_ = os.RemoveAll(dir)
		}
	}
}

// takeBrowserFile consumes a handoff token. Unknown or already-consumed
// tokens report false, so the caller falls through to normal URL validation.
func takeBrowserFile(rawURL string) (path, name string, ok bool) {
	if !strings.HasPrefix(rawURL, browserFileScheme) {
		return "", "", false
	}
	browserFiles.Lock()
	defer browserFiles.Unlock()
	bf, ok := browserFiles.byToken[rawURL]
	if !ok {
		return "", "", false
	}
	delete(browserFiles.byToken, rawURL)
	return bf.path, bf.name, true
}

// IsBrowserFileURL reports whether rawURL is a browser handoff token. Used by
// callers that log or classify download URLs.
func IsBrowserFileURL(rawURL string) bool {
	return strings.HasPrefix(rawURL, browserFileScheme)
}

// adoptBrowserFile copies a finished browser download into destDir and
// removes the temp directory that held it. The copy is measured (progress) and
// size-limited exactly like an HTTP download.
func adoptBrowserFile(ctx context.Context, src, name, destDir string, host string, onProgress func(Progress)) error {
	// The browser wrote into a per-session temp dir; remove it once the file
	// is adopted (success or failure) so nothing leaks.
	if dir := browserFileDir(src); dir != "" {
		defer os.RemoveAll(dir)
	}

	fi, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("browser download: %w", err)
	}
	if !fi.Mode().IsRegular() || fi.Size() == 0 {
		return fmt.Errorf("browser download: %q is not a usable file", src)
	}
	if err := checkGlobalSizeLimit(fi.Size()); err != nil {
		return err
	}
	if err := checkHostSizeLimit(fi.Size(), HostSizeLimit(host), host); err != nil {
		return err
	}

	name = browserFileName(name)
	if name == "" {
		name = browserFileName(filepath.Base(src))
	}
	if name == "" {
		name = "download"
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("browser download: creating destination dir: %w", err)
	}
	finalPath := filepath.Join(destDir, name)
	partPath := finalPath + ".part"

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("browser download: %w", err)
	}
	defer in.Close()

	out, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("browser download: %w", err)
	}

	total := fi.Size()
	var written int64
	buf := make([]byte, 64*1024)
	for {
		if cerr := ctx.Err(); cerr != nil {
			out.Close()
			os.Remove(partPath)
			return cerr
		}
		n, readErr := in.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				os.Remove(partPath)
				return fmt.Errorf("browser download: write: %w", werr)
			}
			written += int64(n)
			if onProgress != nil {
				onProgress(Progress{
					BytesDownloaded: written,
					TotalBytes:      total,
					Percent:         float64(written) / float64(total) * 100,
				})
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			out.Close()
			os.Remove(partPath)
			return fmt.Errorf("browser download: read: %w", readErr)
		}
	}
	if err := out.Close(); err != nil {
		os.Remove(partPath)
		return fmt.Errorf("browser download: %w", err)
	}
	if written != total {
		os.Remove(partPath)
		return fmt.Errorf("browser download: short copy (%d of %d bytes)", written, total)
	}
	if err := os.Rename(partPath, finalPath); err != nil {
		os.Remove(partPath)
		return fmt.Errorf("browser download: rename: %w", err)
	}
	if err := os.Chmod(finalPath, 0o600); err != nil {
		return fmt.Errorf("browser download: chmod: %w", err)
	}

	log.Info("browser download adopted", "file", filepath.Base(finalPath), "size_bytes", written)
	if onProgress != nil {
		onProgress(Progress{BytesDownloaded: written, TotalBytes: total, Percent: 100})
	}
	return nil
}

// browserFileDir returns the per-session temp directory that holds a handoff
// file, or "" when the file is not inside one (so an unrelated source file is
// never deleted).
func browserFileDir(path string) string {
	dir := filepath.Dir(path)
	if strings.HasPrefix(filepath.Base(dir), "moxie-browser-handoff-") {
		return dir
	}
	return ""
}

// browserFileName sanitizes a server-suggested filename for use under destDir.
func browserFileName(name string) string {
	name = strings.TrimSpace(name)
	name = filepath.Base(name)
	switch name {
	case "", ".", "..", string(filepath.Separator):
		return ""
	}
	return name
}
