//go:build !windows

package browserresolve

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestCopyProfileUnreadableFile proves copyProfile surfaces a permission
// error (the analog of a Windows exclusive lock) instead of silently
// producing a partial profile. Skipped when running as root, where mode
// bits are not enforced.
func TestCopyProfileUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits are not enforced")
	}
	src := buildProfileFixture(t)
	locked := filepath.Join(src, "Default", "Cookies")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o600) })

	err := copyProfile(src, filepath.Join(t.TempDir(), "copy"))
	if err == nil {
		t.Fatal("expected copyProfile to fail on an unreadable file")
	}
}

// TestResolver_ProfileLockedOnCopy drives the full resolver and asserts the
// copy failure surfaces as ErrProfileLocked (the "quit Chrome and retry"
// hint), before the engine is ever called.
func TestResolver_ProfileLockedOnCopy(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits are not enforced")
	}
	profile := buildProfileFixture(t)
	locked := filepath.Join(profile, "Default", "Cookies")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o600) })

	f := &fakeEngine{}
	r := newTestResolver(t, f)
	if r.opts.ProfileDir == "" {
		r.opts.ProfileDir = profile
	}

	_, err := r.resolve(t.Context(), "https://cdn.example.com/f/abc", t.TempDir())
	if !errors.Is(err, ErrProfileLocked) {
		t.Fatalf("expected ErrProfileLocked, got %v", err)
	}
	if reqs := f.requests(); len(reqs) != 0 {
		t.Errorf("engine must not run when the profile copy fails, got %d calls", len(reqs))
	}
}
