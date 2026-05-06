package downloader

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mili/moxie/internal/archive"
	"github.com/mili/moxie/internal/log"
)

var blockedDownloadHosts = []string{
	"169.254.169.254",
	"metadata.google.internal",
	"100.100.100.200",
}

func isValidDownloadURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Scheme != "https" {
		return false
	}
	if u.Host == "" {
		return false
	}
	hostname := u.Hostname()
	if ip := net.ParseIP(hostname); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
			return false
		}
	}
	for _, h := range blockedDownloadHosts {
		if hostname == h {
			return false
		}
	}
	return true
}

type writeCounter struct {
	total     int64
	prevTotal int64
	prevTime  time.Time
	onProgress func(Progress)
}

func (wc *writeCounter) Write(p []byte) (int, error) {
	n := len(p)
	wc.total += int64(n)
	now := time.Now()
	elapsed := now.Sub(wc.prevTime).Seconds()
	if elapsed >= 0.5 {
		speed := float64(wc.total-wc.prevTotal) / elapsed
		wc.prevTotal = wc.total
		wc.prevTime = now
		if wc.onProgress != nil {
			wc.onProgress(Progress{
				BytesDownloaded: wc.total,
				TotalBytes:      0,
				SpeedBytesPerSec: speed,
				Percent:         0,
			})
		}
	}
	return n, nil
}

// DownloadWithHost downloads a file with host-specific URL resolution.
// The host label enables specialized handling for hosts like Buzzheavier, Pixeldrain, etc.
// f95Cookie is an optional F95Zone session cookie used to authenticate masked redirects.
func DownloadWithHost(urlStr, host, destDir string, expectedTotal int64, onProgress func(Progress), f95Cookie string) error {
	log.Debug("download start", "url", urlStr, "host", host, "dest", destDir)
	resolver := NewHostResolver()
	if f95Cookie != "" {
		resolver.SetF95Cookie(f95Cookie)
	}
	resolved, resolveErr := resolver.Resolve(urlStr, host)
	if resolveErr != nil {
		log.Info("download resolve failed", "host", host, "error", resolveErr)
		return fmt.Errorf("resolve %s URL: %w", host, resolveErr)
	}
	log.Debug("download resolving via HTTP", "resolved_url", resolved.URL, "headers", len(resolved.Headers))
	return downloadWithHeaders(resolved.URL, resolved.Headers, destDir, expectedTotal, onProgress)
}

// Download downloads a file using standard HTTP, auto-detecting the host.
// f95Cookie is an optional F95Zone session cookie used to authenticate masked redirects.
func Download(urlStr, destDir string, expectedTotal int64, onProgress func(Progress), f95Cookie string) error {
	host := IdentifyHostInURL(urlStr)
	return DownloadWithHost(urlStr, host, destDir, expectedTotal, onProgress, f95Cookie)
}

func downloadWithHeaders(urlStr string, headers map[string]string, destDir string, expectedTotal int64, onProgress func(Progress)) error {
	if !isValidDownloadURL(urlStr) {
		return fmt.Errorf("invalid or blocked URL: %s", urlStr)
	}

	base := filepath.Base(urlStr)
	if base == "" || base == "." || base == "/" {
		base = "download"
	}
	if idx := strings.Index(base, "?"); idx >= 0 {
		base = base[:idx]
	}

	partPath := filepath.Join(destDir, base+".part")
	finalPath := filepath.Join(destDir, base)

	existingSize := int64(0)
	if fi, err := os.Stat(partPath); err == nil {
		existingSize = fi.Size()
	}

	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0")
	req.Header.Set("Accept", "*/*")

	// Apply host-specific headers (e.g., Referer, HX-Request for Buzzheavier)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	if existingSize > 0 {
		// Don't send Range for host-specific resolvers (they may not support it)
		if _, isResolved := headers["HX-Request"]; !isResolved {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", existingSize))
		}
	}

	client := &http.Client{Timeout: 0, Transport: &http.Transport{
		ResponseHeaderTimeout: 30 * time.Second,
	}}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusMovedPermanently {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var totalBytes int64
	if expectedTotal > 0 {
		totalBytes = expectedTotal
	} else if resp.ContentLength > 0 {
		totalBytes = resp.ContentLength + existingSize
	}

	writeMode := os.O_CREATE | os.O_WRONLY
	if existingSize > 0 && resp.StatusCode == http.StatusPartialContent {
		writeMode |= os.O_APPEND
	} else {
		existingSize = 0
		writeMode |= os.O_TRUNC
	}

	f, err := os.OpenFile(partPath, writeMode, 0644)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	if existingSize == 0 {
		if err := os.Remove(partPath + ".tmp"); err == nil {
		}
	}

	wc := &writeCounter{
		total:     existingSize,
		prevTotal: existingSize,
		prevTime:  time.Now(),
		onProgress: func(p Progress) {
			p.TotalBytes = totalBytes
			if totalBytes > 0 {
				p.Percent = float64(p.BytesDownloaded) / float64(totalBytes) * 100
			}
			if onProgress != nil {
				onProgress(p)
			}
		},
	}

	buf := make([]byte, 32768)
	var finalErr error
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			wn, writeErr := f.Write(buf[:n])
			if writeErr != nil {
				finalErr = fmt.Errorf("write: %w", writeErr)
				break
			}
			if wn != n {
				finalErr = fmt.Errorf("short write: %d != %d", wn, n)
				break
			}
			wc.Write(buf[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			finalErr = fmt.Errorf("read: %w", readErr)
			break
		}
	}
	if finalErr != nil {
		f.Close()
		return finalErr
	}

	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("sync: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}

	if err := os.Rename(partPath, finalPath); err != nil {
		return fmt.Errorf("rename: %w", err)
	}

	log.Info("download complete", "file", filepath.Base(finalPath), "size_bytes", wc.total)

	if onProgress != nil {
		onProgress(Progress{
			BytesDownloaded: wc.total,
			TotalBytes:     totalBytes,
			Percent:        100,
		})
	}

	return nil
}

// IsValidGameFile returns true if the downloaded file appears to be a real game
// file (archive, executable, etc.) rather than an interstitial HTML page or ad.
func IsValidGameFile(path string) bool {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() < 4096 {
		return false
	}
	if archive.IsArchiveFile(path) {
		return true
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".exe", ".sh", ".x86_64", ".bin", ".run", ".AppImage":
		return true
	}
	return false
}
