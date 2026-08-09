package browserresolve

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// errorsIs is a thin alias so table tests read as got/vs want without
// repeating the errors import everywhere.
func errorsIs(err, target error) bool { return errors.Is(err, target) }

// buildProfileFixture creates a fake Chrome profile root mimicking the
// shape of a real one:
//
//	Local State                     (cookie AES key — must be copied)
//	Default/Cookies, Preferences    (cookie store — must be copied)
//	Default/History                 (non-essential — copied)
//	SingletonLock, SingletonSocket  (live-profile locks — skipped)
//	foo.lock                        (OS lock — skipped)
//	Cookies-journal                 (SQLite journal — skipped)
//	Cache/f0/abc                    (cache dir — skipped wholesale)
//	local-state-link -> Local State (symlink — skipped, not followed)
func buildProfileFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	write("Local State", `{"os_crypt":{"encrypted_key":"ZmFrZWtleQ=="}}`)
	write("Default/Cookies", "binary-cookie-store")
	write("Default/Preferences", "{}")
	write("Default/History", "history")
	write("SingletonLock", "pid")
	write("SingletonSocket", "socket")
	write("foo.lock", "x")
	write("Cookies-journal", "journal")
	write("Cache/f0/abc", "cached-bytes")
	// Symlinks need privileges on Windows; skip silently when unsupported.
	if err := os.Symlink(filepath.Join(root, "Local State"), filepath.Join(root, "local-state-link")); err != nil {
		t.Logf("symlink fixture skipped: %v", err)
	}
	return root
}

// readAllProfile returns the relative paths of every file under root.
func readAllProfile(t *testing.T, root string) map[string]bool {
	t.Helper()
	files := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if !d.IsDir() {
			files[filepath.ToSlash(rel)] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return files
}

func TestSkipProfileFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want bool
	}{
		{"SingletonLock", true},
		{"SingletonSocket", true},
		{"SingletonCookie", true},
		{"foo.lock", true},
		{"Cookies-journal", true},
		{"Local State", false},
		{"Cookies", false},
		{"Cookies-wal", false}, // copied: WAL holds cookies newer than the last checkpoint
		{"Cookies-shm", false}, // copied: SQLite rebuilds shm from wal on mismatch
		{"Preferences", false},
		{"Default", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := skipProfileFile(tt.name); got != tt.want {
				t.Errorf("skipProfileFile(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestCopyProfileSkipsLocksJournalsCachesAndSymlinks(t *testing.T) {
	t.Parallel()
	src := buildProfileFixture(t)
	dst := filepath.Join(t.TempDir(), "profile-copy")
	if err := copyProfile(src, dst); err != nil {
		t.Fatalf("copyProfile: %v", err)
	}

	files := readAllProfile(t, dst)
	for _, mustHave := range []string{
		"Local State", // cookie AES key — the whole point of the copy
		"Default/Cookies",
		"Default/Preferences",
		"Default/History",
	} {
		if !files[mustHave] {
			t.Errorf("copy missing %q (got %v)", mustHave, files)
		}
	}
	for _, mustNotHave := range []string{
		"SingletonLock",
		"SingletonSocket",
		"foo.lock",
		"Cookies-journal",
		"Cache/f0/abc",
		"local-state-link",
	} {
		if files[mustNotHave] {
			t.Errorf("copy must not contain %q", mustNotHave)
		}
	}
}

func TestCopyProfileLocalStateContentPreserved(t *testing.T) {
	t.Parallel()
	src := buildProfileFixture(t)
	dst := filepath.Join(t.TempDir(), "profile-copy")
	if err := copyProfile(src, dst); err != nil {
		t.Fatalf("copyProfile: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "Local State"))
	if err != nil {
		t.Fatalf("read copied Local State: %v", err)
	}
	want := `{"os_crypt":{"encrypted_key":"ZmFrZWtleQ=="}}`
	if string(got) != want {
		t.Errorf("Local State content = %q, want %q", got, want)
	}
}

func TestCopyProfileSkipsWholeCacheDirs(t *testing.T) {
	t.Parallel()
	src := filepath.Join(t.TempDir(), "profile")
	for _, dir := range []string{"Cache", "Code Cache", "GPUCache", "Service Worker", "CacheStorage", "ShaderCache"} {
		if err := os.MkdirAll(filepath.Join(src, dir, "nested"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, dir, "nested", "blob"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(src, "Local State"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "copy")
	if err := copyProfile(src, dst); err != nil {
		t.Fatalf("copyProfile: %v", err)
	}
	files := readAllProfile(t, dst)
	if len(files) != 1 || !files["Local State"] {
		t.Errorf("expected only Local State copied, got %v", files)
	}
}

func TestCopyProfileErrorPaths(t *testing.T) {
	t.Parallel()
	t.Run("source missing", func(t *testing.T) {
		err := copyProfile(filepath.Join(t.TempDir(), "nope"), t.TempDir())
		if err == nil {
			t.Fatal("expected error for missing source")
		}
	})
	t.Run("source is a file", func(t *testing.T) {
		src := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		err := copyProfile(src, t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "not a directory") {
			t.Fatalf("expected not-a-directory error, got %v", err)
		}
	})
	t.Run("destination blocked by a file", func(t *testing.T) {
		src := buildProfileFixture(t)
		dst := filepath.Join(t.TempDir(), "copy")
		if err := os.MkdirAll(dst, 0o755); err != nil {
			t.Fatal(err)
		}
		// A regular file where copyProfile must create a directory.
		if err := os.WriteFile(filepath.Join(dst, "Default"), []byte("block"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := copyProfile(src, dst); err == nil {
			t.Fatal("expected error when destination is blocked by a file")
		}
	})
}

func TestDiscoverProfileDir(t *testing.T) {
	// No t.Parallel: t.Setenv mutates the process environment.
	// Isolate HOME so real browser profiles on this machine cannot satisfy
	// the "no profile" cases.
	t.Setenv("HOME", t.TempDir())
	t.Run("override missing", func(t *testing.T) {
		_, err := discoverProfileDir(filepath.Join(t.TempDir(), "missing"))
		if !errorsIs(err, ErrNoProfile) {
			t.Fatalf("expected ErrNoProfile, got %v", err)
		}
	})
	t.Run("override valid", func(t *testing.T) {
		profile := buildProfileFixture(t)
		got, err := discoverProfileDir(profile)
		if err != nil || got != profile {
			t.Fatalf("discoverProfileDir = %q, %v; want %q", got, err, profile)
		}
	})
	t.Run("env var", func(t *testing.T) {
		profile := buildProfileFixture(t)
		t.Setenv(profileEnvVar, profile)
		got, err := discoverProfileDir("")
		if err != nil || got != profile {
			t.Fatalf("env discovery = %q, %v; want %q", got, err, profile)
		}
	})
	t.Run("env var invalid", func(t *testing.T) {
		t.Setenv(profileEnvVar, filepath.Join(t.TempDir(), "nope"))
		if _, err := discoverProfileDir(""); !errorsIs(err, ErrNoProfile) {
			t.Fatalf("expected ErrNoProfile, got %v", err)
		}
	})
	t.Run("override beats env", func(t *testing.T) {
		envProfile := buildProfileFixture(t)
		override := buildProfileFixture(t)
		t.Setenv(profileEnvVar, envProfile)
		got, err := discoverProfileDir(override)
		if err != nil || got != override {
			t.Fatalf("override discovery = %q, %v; want %q", got, err, override)
		}
	})
	t.Run("profile root without Local State but with Default", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "Default"), 0o755); err != nil {
			t.Fatal(err)
		}
		got, err := discoverProfileDir(root)
		if err != nil || got != root {
			t.Fatalf("discoverProfileDir = %q, %v; want %q", got, err, root)
		}
	})
	t.Run("empty dir is not a profile", func(t *testing.T) {
		if _, err := discoverProfileDir(t.TempDir()); !errorsIs(err, ErrNoProfile) {
			t.Fatalf("expected ErrNoProfile, got %v", err)
		}
	})
}
