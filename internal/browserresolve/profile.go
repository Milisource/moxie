package browserresolve

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mili/moxie/internal/log"
)

// profileEnvVar overrides automatic profile discovery: point it at a
// profile root ("User Data" dir) when the standard per-OS locations don't
// apply (custom installs, portable Chrome).
const profileEnvVar = "MOXIE_CHROME_PROFILE_DIR"

// skippedProfileDirs are profile subdirectories that are safe to omit from
// a profile copy: Chrome rebuilds them on launch, and they dominate the
// profile's disk footprint (multi-GB caches). Cookie state — the reason the
// profile is copied at all — lives in root files (Cookies, Local State) and
// in Default/, which are always copied.
var skippedProfileDirs = map[string]bool{
	"Cache":                          true,
	"Code Cache":                     true,
	"GPUCache":                       true,
	"DawnCache":                      true,
	"DawnGraphiteCache":              true,
	"GraphiteDawnCache":              true,
	"ShaderCache":                    true,
	"GrShaderCache":                  true,
	"Service Worker":                 true,
	"CacheStorage":                   true,
	"ScriptCache":                    true,
	"Application Cache":              true,
	"Component Updater":              true,
	"GrShaderCache_GL":               true,
	"optimization_guide_model_store": true,
	// "Sync Data" (Chrome/Brave sync LevelDB): never needed for
	// cookie-based sessions, and a Brave sync DB core-dumps vanilla
	// Chromium builds — live-verified 2026-08-09: full Brave profile copy
	// crashed Playwright Chromium 3/3 (2-4s after launch, mid-session),
	// 3/3 OK with Sync Data excluded. This silently killed masked-URL
	// browser sessions all day (the crashes looked like navigation races).
	"Sync Data": true,
	// Firefox rebuilds these multi-GB caches on launch; cookie state
	// (cookies.sqlite + WAL — the reason the profile is copied) is root-level.
	"cache2":        true,
	"startupCache":  true,
	"OfflineCache":  true,
	"minidumps":     true,
	"crashes":       true,
	"datareporting": true,
}

// discoverProfileDir locates the user's browser profile root ("User Data"
// dir containing Local State and Default/), honoring an explicit override
// option first, then the MOXIE_CHROME_PROFILE_DIR environment variable,
// then the standard per-OS locations.
func discoverProfileDir(override string) (string, error) {
	var candidates []string
	if override != "" {
		candidates = append(candidates, override)
	}
	if env := os.Getenv(profileEnvVar); env != "" && override == "" {
		candidates = append(candidates, env)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		// Discovery can still proceed from the environment-based
		// candidates below; only the home-dependent defaults need it.
		home = ""
	}
	switch runtime.GOOS {
	case "windows":
		if appData := os.Getenv("LOCALAPPDATA"); appData != "" {
			candidates = append(candidates,
				filepath.Join(appData, "Google", "Chrome", "User Data"),
				filepath.Join(appData, "Microsoft", "Edge", "User Data"),
				filepath.Join(appData, "BraveSoftware", "Brave-Browser", "User Data"),
			)
		}
	case "darwin":
		if home != "" {
			candidates = append(candidates,
				filepath.Join(home, "Library", "Application Support", "Google", "Chrome"),
				filepath.Join(home, "Library", "Application Support", "Microsoft Edge"),
				filepath.Join(home, "Library", "Application Support", "BraveSoftware", "Brave-Browser"),
			)
		}
	default:
		if home != "" {
			candidates = append(candidates,
				filepath.Join(home, ".config", "google-chrome"),
				filepath.Join(home, ".config", "chromium"),
				filepath.Join(home, ".config", "microsoft-edge"),
				filepath.Join(home, ".config", "BraveSoftware", "Brave-Browser"),
			)
		}
	}

	for _, c := range candidates {
		if isProfileRoot(c) {
			return c, nil
		}
	}
	if override != "" {
		return "", fmt.Errorf("%w: %q is not an existing profile dir", ErrNoProfile, override)
	}
	return "", fmt.Errorf("%w: looked in %s", ErrNoProfile, strings.Join(candidates, ", "))
}

// isProfileRoot reports whether dir looks like a Chrome profile root: it
// must exist and contain either Local State (the cookie encryption key the
// copy needs) or a Default/ profile directory.
func isProfileRoot(dir string) bool {
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "Local State")); err == nil {
		return true
	}
	_, err = os.Stat(filepath.Join(dir, "Default"))
	return err == nil
}

// copyProfile copies a live browser profile directory into dst, skipping
// lock files and cache-heavy directories (see skipProfileEntry). Symlinks
// are not followed — Chrome rebuilds them. The copy is what headless Chrome
// launches on: the live profile is hard-locked against a second process
// (SingletonLock) and Chrome >= 136 ignores --remote-debugging-port
// against the real profile dir.
func copyProfile(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat profile %q: %w", src, err)
	}
	if !srcInfo.IsDir() {
		return fmt.Errorf("profile %q is not a directory", src)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("create profile copy dir: %w", err)
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)

		if d.Type()&fs.ModeSymlink != 0 {
			log.Debug("browserresolve: skipping symlink in profile copy", "path", rel)
			return nil
		}
		if skipProfileEntry(d) {
			log.Debug("browserresolve: skipping profile entry", "path", rel)
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

// skipProfileEntry reports whether a directory entry of a live profile must
// not be copied.
func skipProfileEntry(d fs.DirEntry) bool {
	if d.IsDir() {
		return skippedProfileDirs[d.Name()]
	}
	return skipProfileFile(d.Name())
}

// skipProfileFile mirrors the browsers' own lock-file naming: Chrome's
// Singleton* files (SingletonLock, SingletonSocket, SingletonCookie) guard
// the live profile against a second process, *.lock are OS-level locks
// (incl. Firefox's parent.lock), -journal is a SQLite rollback journal, and
// .parentlock is Firefox's own instance lock (Linux/Windows). All are safe
// to omit — the browsers regenerate them.
//
// SQLite -wal/-shm files ARE copied: per-frame checksums let SQLite
// truncate a torn tail safely, and skipping them would drop cookies written
// since the last checkpoint (e.g. a just-issued cf_clearance).
func skipProfileFile(name string) bool {
	return strings.HasPrefix(name, "Singleton") ||
		name == ".parentlock" ||
		strings.HasSuffix(name, ".lock") ||
		strings.HasSuffix(name, "-journal")
}

// copyFile copies a single profile file, preserving its permission bits.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %q: %w", src, err)
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return fmt.Errorf("stat %q: %w", src, err)
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("create %q: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("copy %q: %w", src, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close %q: %w", dst, err)
	}
	return nil
}
