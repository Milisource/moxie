package downloader

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mili/moxie/internal/log"
)

// downloadWithExternal runs a host-specific external downloader (currently
// megatools) and adapts it to the Progress callback. The external tool is
// responsible for the protocol details (MEGA key exchange + decryption).
func downloadWithExternal(binary, url, host, destDir string, expectedTotal int64, onProgress func(Progress)) error {
	if err := os.MkdirAll(destDir, 0700); err != nil {
		return fmt.Errorf("create download directory: %w", err)
	}
	before := snapshotFiles(destDir)

	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()

	var args []string
	switch strings.ToLower(filepath.Base(binary)) {
	case "megatools":
		args = []string{"dl", "--path", destDir, "--no-progress", "--no-ask-password", url}
	default:
		return fmt.Errorf("unsupported external downloader: %s", binary)
	}

	cmd := exec.CommandContext(ctx, binary, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	log.Info("external download start", "host", host, "tool", filepath.Base(binary))
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", filepath.Base(binary), err)
	}

	// Poll the growing temp/final file so the TUI/CLI still shows movement.
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if onProgress == nil {
					continue
				}
				size := largestFileSize(destDir)
				if size > 0 {
					p := Progress{BytesDownloaded: size, TotalBytes: expectedTotal}
					if expectedTotal > 0 {
						p.Percent = float64(size) / float64(expectedTotal) * 100
					}
					onProgress(p)
				}
			}
		}
	}()

	err := cmd.Wait()
	close(done)

	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("%s timed out after %s", filepath.Base(binary), downloadTimeout)
	}
	if err != nil {
		msg := strings.TrimSpace(out.String())
		if len(msg) > 300 {
			msg = msg[len(msg)-300:]
		}
		return fmt.Errorf("%s failed: %w: %s", filepath.Base(binary), err, msg)
	}

	file := newestNewFile(destDir, before)
	if file == "" {
		// The tool may have overwritten an existing file with the same name.
		file = FindMostRecentFile(destDir)
	}
	if file == "" {
		return fmt.Errorf("%s finished but no downloaded file was found in %s", filepath.Base(binary), destDir)
	}
	fi, err := os.Stat(file)
	if err != nil {
		return fmt.Errorf("stat downloaded file: %w", err)
	}
	if err := os.Chmod(file, 0600); err != nil {
		log.Warn("could not restrict downloaded file permissions", "file", file, "error", err)
	}

	log.Info("external download complete", "file", filepath.Base(file), "size_bytes", fi.Size())
	if onProgress != nil {
		onProgress(Progress{BytesDownloaded: fi.Size(), TotalBytes: fi.Size(), Percent: 100})
	}
	return nil
}

// snapshotFiles records the names present in dir before a download starts.
func snapshotFiles(dir string) map[string]bool {
	seen := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return seen
	}
	for _, e := range entries {
		seen[e.Name()] = true
	}
	return seen
}

// newestNewFile returns the newest regular file in dir that was not present in
// before. Megatools writes to a dot-prefixed temp file and renames it at the end.
func newestNewFile(dir string, before map[string]bool) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var best string
	var bestTime time.Time
	for _, e := range entries {
		if e.IsDir() || before[e.Name()] {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if info.ModTime().After(bestTime) {
			best = filepath.Join(dir, e.Name())
			bestTime = info.ModTime()
		}
	}
	return best
}

func largestFileSize(dir string) int64 {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var largest int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.Size() > largest {
			largest = info.Size()
		}
	}
	return largest
}
