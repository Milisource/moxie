package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mili/moxie/internal/config"
)

// coverServer serves cached cover art to the webview over loopback HTTP.
//
// The old GetCachedCovers path base64-encoded every visible cover through
// the Wails IPC bridge on every list render — 50 images at up to 16 MiB each
// is hundreds of megabytes of base64 churn per view. Serving files over
// 127.0.0.1 lets the webview's own HTTP cache handle repeat loads and keeps
// the JS side holding nothing more than an <img src>.
type coverServer struct {
	ln   net.Listener
	srv  *http.Server
	base string
}

// startCoverServer binds a loopback listener on a random port and serves
// cover files from config.CoverDir(). It returns nil when binding fails.
func startCoverServer() *coverServer {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		slog.Error("failed to bind cover server", "error", err)
		return nil
	}

	// The cover cache holds nothing secret, but owner-only permissions keep
	// other local users from planting files (e.g. symlinks) that the webview
	// would then load.
	if err := os.MkdirAll(config.CoverDir(), 0o700); err != nil {
		slog.Warn("could not create cover directory", "error", err)
	}

	cs := &coverServer{
		ln:   ln,
		base: "http://" + ln.Addr().String(),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/cover/", cs.handleCover)
	cs.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		if err := cs.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Warn("cover server stopped unexpectedly", "error", err)
		}
	}()
	return cs
}

// BaseURL returns the loopback origin frontend <img> tags should use.
func (cs *coverServer) BaseURL() string {
	if cs == nil {
		return ""
	}
	return cs.base
}

// Close shuts the server down, releasing the loopback port.
func (cs *coverServer) Close() {
	if cs == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := cs.srv.Shutdown(ctx); err != nil {
		slog.Warn("cover server shutdown", "error", err)
	}
}

// backfillMarkerName is the name of the marker file recording a completed
// full thumbnail-backfill pass. Version-scoped: a new app version that
// changes thumbnailing invalidates the previous pass, so the marker does not
// suppress the walk after an upgrade.
func backfillMarkerName() string {
	v := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, appVersion)
	if v == "" {
		v = "unknown"
	}
	return ".backfill-complete-" + v
}

var backfillMu sync.Mutex

// backfillCoverThumbs walks the cover cache and generates .thumb files for
// any full-size cover that predates the thumbnailing change (or whose
// thumbnail is missing for any reason). Local-only, no network. Returns the
// number of thumbnails written. Covers whose thumbnail already exists, and
// covers too small to need one, are skipped. AVIF covers (no pure-Go
// decoder) and corrupt files are counted and reported in a single summary
// line instead of a per-cover Warn storm.
//
// The optional context argument cancels the walk (aborting leaves no
// completion marker, so the next launch retries); callers that predate the
// parameter may call it with no argument, in which case the walk is
// uncancellable — exactly as before. The pass is serialized against any
// other concurrent backfill (notably the one inside FetchCovers) and skipped
// entirely once a "backfill complete" marker exists for this app version,
// so the decode-everything walk runs once per version instead of on every
// startup. Covers cached after the marker was written get their thumbnails
// from cacheCover's own writeCoverThumb call.
func backfillCoverThumbs(ctxs ...context.Context) int {
	backfillMu.Lock()
	defer backfillMu.Unlock()

	ctx := context.Background()
	if len(ctxs) > 0 && ctxs[0] != nil {
		ctx = ctxs[0]
	}

	dir := config.CoverDir()
	if _, err := os.Stat(filepath.Join(dir, backfillMarkerName())); err == nil {
		slog.Debug("cover thumbnail backfill already complete; skipping", "dir", dir)
		return 0
	}

	start := time.Now()
	entries, err := os.ReadDir(dir)
	if err != nil {
		slog.Debug("cover thumbnail backfill: cannot read cover dir", "dir", dir, "error", err)
		return 0
	}
	count := 0
	skippedAVIF := 0
	skippedOther := 0
	for _, e := range entries {
		if ctx.Err() != nil {
			// Interrupted (e.g. shutdown): no completion marker, so the next
			// launch retries the remaining covers.
			slog.Info("cover thumbnail backfill cancelled", "written", count, "elapsed", time.Since(start))
			return count
		}
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".thumb") {
			continue
		}
		if _, err := strconv.ParseInt(name, 10, 64); err != nil {
			continue // not a cover file
		}
		full := filepath.Join(dir, name)
		if _, err := os.Stat(full + ".thumb"); err == nil {
			continue
		}
		switch writeCoverThumb(full) {
		case thumbWritten:
			count++
		case thumbSkipAVIF:
			// Expected: webview renders AVIF from the full image; there is
			// no pure-Go AVIF decoder, so no thumbnail can be generated.
			skippedAVIF++
		case thumbDecodeFailed:
			skippedOther++
		}
	}
	slog.Info("cover thumbnails backfilled",
		"count", count,
		"skippedAVIF", skippedAVIF, "skippedOther", skippedOther,
		"elapsed", time.Since(start))

	// Full pass completed: mark it done so later startups skip the walk.
	if err := os.WriteFile(filepath.Join(dir, backfillMarkerName()), []byte(appVersion+"\n"), 0o644); err != nil {
		slog.Warn("could not write backfill completion marker", "error", err)
	}
	return count
}

// handleCover serves /cover/<gameID> (full image) and
// /cover/<gameID>/thumb (downscaled list thumbnail, falling back to the
// full image when no thumbnail exists). Only numeric game IDs are accepted,
// so the path can never escape the cover directory by construction.
func (cs *coverServer) handleCover(w http.ResponseWriter, r *http.Request) {
	// DNS-rebinding defense: the browser's Host header names the host it
	// actually connected to. A rebinding attack resolves an attacker domain
	// to 127.0.0.1, so the Host would be the attacker's domain — refuse it.
	if !cs.isLoopbackHost(r.Host) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/cover/")
	rest = strings.TrimSuffix(rest, "/")

	thumb := false
	if strings.HasSuffix(rest, "/thumb") {
		thumb = true
		rest = strings.TrimSuffix(rest, "/thumb")
	}

	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}

	path := filepath.Join(config.CoverDir(), strconv.FormatInt(id, 10))
	if thumb {
		if _, err := os.Stat(path + ".thumb"); err == nil {
			path += ".thumb"
		}
	}

	// Resolve symlinks and refuse anything that escapes the cover directory:
	// http.ServeFile happily follows a locally-planted symlink, which would
	// expose arbitrary readable files to the webview. EvalSymlinks returns
	// the final non-symlink path, so the ServeFile below has nothing left to
	// follow.
	served, err := resolveUnderCoverDir(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Serve via ServeFile: it streams with Content-Length and caps memory
	// use, unlike ReadFile which loads a potentially huge file whole.
	w.Header().Set("Content-Type", coverContentType(served))
	w.Header().Set("Cache-Control", "public, max-age=3600")
	// No Access-Control-Allow-Origin: a wildcard on a loopback server would
	// let any webpage (or DNS-rebinding domain) read which cover IDs exist,
	// leaking which games the user owns. <img> loads are no-cors and need no
	// CORS header.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, served)
}

// isLoopbackHost reports whether a Host header names a loopback address.
// Used to refuse requests whose Host does not match the loopback origin the
// frontend was told to use — the DNS-rebinding defense.
func (cs *coverServer) isLoopbackHost(host string) bool {
	h := host
	if i := strings.LastIndex(h, ":"); i >= 0 {
		// Strip the port. IPv6 literals are bracketed, so the last colon is
		// the port separator.
		h = h[:i]
	}
	h = strings.Trim(h, "[]")
	switch h {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

// resolveUnderCoverDir resolves path under the cover directory, following
// symlinks, and returns the fully-resolved path only when it still lives
// inside the cover directory. A locally-planted symlink pointing elsewhere
// (or a dangling one) yields an error, so the server never serves through it.
func resolveUnderCoverDir(path string) (string, error) {
	coverDir, err := filepath.EvalSymlinks(config.CoverDir())
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if !isPathUnder(coverDir, resolved) {
		return "", fmt.Errorf("path escapes cover directory")
	}
	return resolved, nil
}

// coverContentType returns the Content-Type header value for a cover file.
// Unknown magic bytes are served as application/octet-stream — never guessed
// as "image/png", which would label arbitrary (possibly attacker-placed)
// content as an image.
func coverContentType(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer f.Close()
	buf := make([]byte, 12)
	n, _ := io.ReadFull(f, buf)
	mime := imageMimeFromPrefix(buf[:n])
	if mime == "png" && !bytes.HasPrefix(buf[:n], []byte{0x89, 0x50, 0x4E, 0x47}) {
		// imageMimeFromPrefix falls back to "png" for anything unrecognised;
		// only a real PNG signature earns the image/png type.
		return "application/octet-stream"
	}
	return "image/" + mime
}
