package browserresolve

import (
	"os"
	"path/filepath"
	"testing"
)

// buildFirefoxFixture creates a Firefox-style profile root: profiles.ini
// with a Default=1 profile, and that profile dir with cookies.sqlite.
//
// It lives in its own file with no build tag because the live Firefox test
// (firefox_live_test.go) is cross-platform and calls it; the Unix-only
// firefox_test.go uses it too.
func buildFirefoxFixture(t *testing.T) (root, profileDir string) {
	t.Helper()
	root = t.TempDir()
	profileDir = filepath.Join(root, "abcdefgh.default")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "cookies.sqlite"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	ini := `[Profile1]
Name=secondary
IsRelative=1
Path=secondary.default

[Profile0]
Name=default
IsRelative=1
Path=abcdefgh.default
Default=1
`
	if err := os.WriteFile(filepath.Join(root, "profiles.ini"), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, profileDir
}
