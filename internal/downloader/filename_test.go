package downloader

import (
	"path/filepath"
	"testing"
)

func TestSafeDestName(t *testing.T) {
	for _, tc := range []struct {
		raw, cd, want string
		reject        bool
	}{
		{"https://example.com/%2e%2e%2f%2e%2e%2fpasswd", "", "passwd", false},
		{"https://example.com/%2F", "", "", true},
		{"https://example.com/..", "", "", true},
		{"https://example.com/.hidden", "", "", true},
		{"https://example.com/id", `attachment; filename="/etc/passwd"`, "passwd", false},
		{"https://example.com/", `attachment; filename="game.zip"`, "game.zip", false},
		{"https://example.com/file%20name.zip", "", "file name.zip", false},
		{"https://example.com/id", `attachment; filename="100% Orange Juice.zip"`, "100% Orange Juice.zip", false},
		{"https://example.com/id", `attachment; filename*=UTF-8''100%2525.zip`, "100%25.zip", false},
		{"https://example.com/CON.zip", "", "", true},
		{"https://example.com/LPT1.zip", "", "", true},
		{"https://example.com/file%00.zip", "", "", true},
		{"https://example.com/Game:%20Remake.zip", "", "Game_ Remake.zip", false},
		{"https://example.com/..%5Cgame.zip", "", "game.zip", false},
	} {
		t.Run(tc.raw+tc.cd, func(t *testing.T) {
			name, err := safeDestName(tc.raw, tc.cd)
			if (err != nil) != tc.reject {
				t.Fatalf("name=%q err=%v", name, err)
			}
			if tc.reject {
				return
			}
			if name != tc.want {
				t.Fatalf("name=%q want %q", name, tc.want)
			}
			dir := t.TempDir()
			dest, err := safeDestPath(dir, name)
			if err != nil {
				t.Fatal(err)
			}
			rel, err := filepath.Rel(dir, dest)
			if err != nil || rel != name {
				t.Fatalf("escaped: %q %v", rel, err)
			}
		})
	}
}
