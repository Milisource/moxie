package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

// backfillCoverThumbs walks the cover cache and generates .thumb files for
// any full-size cover that predates the thumbnailing change (or whose
// thumbnail is missing for any reason). Local-only, no network. Returns the
// number of thumbnails written. Covers whose thumbnail already exists, and
// covers too small to need one, are skipped.
func backfillCoverThumbs() int {
	start := time.Now()
	dir := config.CoverDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		slog.Debug("cover thumbnail backfill: cannot read cover dir", "dir", dir, "error", err)
		return 0
	}
	count := 0
	for _, e := range entries {
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
		writeCoverThumb(full)
		// writeCoverThumb is best-effort (too-small images and decode
		// failures write nothing) — count only what actually appeared.
		if _, err := os.Stat(full + ".thumb"); err == nil {
			count++
		}
	}
	slog.Info("cover thumbnails backfilled", "count", count, "elapsed", time.Since(start))
	return count
}

// handleCover serves /cover/<gameID> (full image) and
// /cover/<gameID>/thumb (downscaled list thumbnail, falling back to the
// full image when no thumbnail exists). Only numeric game IDs are accepted,
// so the path can never escape the cover directory.
func (cs *coverServer) handleCover(w http.ResponseWriter, r *http.Request) {
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

	// Serve via ServeFile: it streams with Content-Length and caps memory
	// use, unlike ReadFile which loads a potentially huge file whole.
	w.Header().Set("Content-Type", "image/"+imageMimeFromFile(path))
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	http.ServeFile(w, r, path)
}

// imageMimeFromFile sniffs the MIME type of an image file for the
// Content-Type header.
func imageMimeFromFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return "unknown"
	}
	defer f.Close()
	buf := make([]byte, 12)
	n, _ := io.ReadFull(f, buf)
	return imageMimeFromPrefix(buf[:n])
}
