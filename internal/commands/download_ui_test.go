package commands

import (
	"os"
	"testing"

	"github.com/mili/moxie/internal/downloader"
)

// TestRenderProgressBarSuppressed covers both no-op conditions: the package
// switch being off, and stderr not being a character device (a temp file
// here, which is what redirected logs look like).
func TestRenderProgressBarSuppressed(t *testing.T) {
	prev := progressBarEnabled.Load()
	t.Cleanup(func() { progressBarEnabled.Store(prev) })

	f, err := os.CreateTemp(t.TempDir(), "stderr-*")
	if err != nil {
		t.Fatal(err)
	}
	oldStderr := os.Stderr
	os.Stderr = f
	t.Cleanup(func() {
		os.Stderr = oldStderr
		f.Close()
	})

	progress := downloader.Progress{Percent: 50, BytesDownloaded: 1 << 20, TotalBytes: 2 << 20}

	progressBarEnabled.Store(false)
	renderProgressBar(progress) // switch off

	progressBarEnabled.Store(true)
	renderProgressBar(progress) // stderr is a regular file, not a char device

	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Errorf("renderProgressBar wrote %q, want no output", data)
	}
}
