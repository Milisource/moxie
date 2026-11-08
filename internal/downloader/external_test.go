package downloader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadWithExternal_Megatools(t *testing.T) {
	t.Parallel()
	binDir := t.TempDir()
	dest := t.TempDir()
	script := filepath.Join(binDir, "megatools")
	body := "#!/bin/sh\n" +
		"while [ $# -gt 0 ]; do\n" +
		"  if [ \"$1\" = \"--path\" ]; then shift; d=\"$1\"; fi\n" +
		"  shift\n" +
		"done\n" +
		"printf 'fake mega bytes' > \"$d/game.zip\"\n"
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}

	var last Progress
	err := downloadWithExternal(script, "https://mega.nz/file/abc#key", "mega", dest, 16, func(p Progress) {
		last = p
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dest, "game.zip"))
	if err != nil {
		t.Fatalf("downloaded file missing: %v", err)
	}
	if string(data) != "fake mega bytes" {
		t.Errorf("unexpected content: %q", data)
	}
	if last.Percent != 100 {
		t.Errorf("expected final progress 100, got %+v", last)
	}
}

func TestDownloadWithExternal_Unsupported(t *testing.T) {
	t.Parallel()
	err := downloadWithExternal("curl", "https://example.com/f", "unknown", t.TempDir(), 0, nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported error, got %v", err)
	}
}
