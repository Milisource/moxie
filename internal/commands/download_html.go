package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/engine"
	"github.com/mili/moxie/internal/scraper"
	"github.com/mili/moxie/internal/version"
)

// filenameVersionRe extracts a version token from a download filename:
// "Ashfall_RPG_v0.4.0.html" → "0.4.0", "Game-v1.2.zip" → "1.2".
var filenameVersionRe = regexp.MustCompile(`(?i)(?:^|[_\-\s])v?(\d+(?:\.\d+){1,3}[a-z]?)(?:[_\-\s.]|$)`)

// versionFromFilename returns the version declared by a download filename, or
// "" when the name carries none.
func versionFromFilename(name string) string {
	m := filenameVersionRe.FindStringSubmatch(filepath.Base(name))
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// sameVersionNumbers reports whether two version strings denote the same
// release, tolerating "0.5" vs "0.5.0" and a missing/trailing v.
func sameVersionNumbers(a, b string) bool {
	na := scraper.StripVersionQualifier(a)
	nb := scraper.StripVersionQualifier(b)
	if na == "" || nb == "" {
		return false
	}
	return version.Compare(na, nb) == version.Same
}

// installHTMLGameFile installs a single-file HTML game payload into the
// game's directory. HTML games are often distributed as one .html page (no
// archive), so there is nothing to extract or directory-merge: the new page is
// copied in, the previous entry page is retired (renamed, never deleted so a
// bad update stays recoverable), and the game record points at the new page.
//
// Returns the installed path and the retired path (empty when none).
func installHTMLGameFile(gameDir, oldExePath, src string) (installed, retired string, err error) {
	info, err := os.Stat(gameDir)
	if err != nil {
		return "", "", fmt.Errorf("game directory: %w", err)
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("game path %s is not a directory", gameDir)
	}

	// Resolve the previous entry BEFORE copying: after the copy
	// FindHTMLEntry could pick either page.
	oldEntry := ""
	if isHTMLFile(oldExePath) && filepath.Dir(filepath.Clean(oldExePath)) == filepath.Clean(gameDir) {
		oldEntry = oldExePath
	} else {
		oldEntry = engine.FindHTMLEntry(gameDir)
	}

	dst := filepath.Join(gameDir, filepath.Base(src))
	if err := copyFileContents(src, dst); err != nil {
		return "", "", err
	}

	if oldEntry != "" && filepath.Clean(oldEntry) != filepath.Clean(dst) {
		retiredPath := oldEntry + ".pre-update"
		_ = os.Remove(retiredPath)
		if err := os.Rename(oldEntry, retiredPath); err != nil {
			// The new page is in place; a failed retire only leaves the old
			// page behind (the entry scan prefers index.html, then the
			// shortest name), so warn rather than fail the install.
			return dst, "", fmt.Errorf("installed %s but could not retire %s: %w", dst, oldEntry, err)
		}
		retired = retiredPath
	}
	return dst, retired, nil
}

func isHTMLFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".html" || ext == ".htm"
}

// copyFileContents copies src to dst with 0644 permissions.
func copyFileContents(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("copy to %s: %w", dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", dst, err)
	}
	return nil
}

// applyHTMLGameInstall installs a downloaded HTML payload and records it on
// the game (entry page + installed version). Returns false when the payload is
// not an HTML page.
func applyHTMLGameInstall(database *db.Database, game *db.Game, downloadedFile string) bool {
	if !isHTMLFile(downloadedFile) {
		return false
	}
	fmt.Fprintf(os.Stderr, "  Installing HTML game file...\n")
	installed, retired, err := installHTMLGameFile(game.Path, game.ExePath, downloadedFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "    Warning: %v\n", err)
		if installed == "" {
			return true
		}
	}
	if retired != "" {
		fmt.Fprintf(os.Stderr, "    Previous entry kept as %s\n", retired)
	}
	game.ExePath = installed
	if game.LatestVersion != "" {
		installedVersion := game.LatestVersion
		// A mirror that lags the thread (or a stale stored link) delivers an
		// older file than LatestVersion. Record the version the payload
		// actually declares, otherwise the game looks up to date and the
		// update never retries.
		if fv := versionFromFilename(downloadedFile); fv != "" && !sameVersionNumbers(fv, game.LatestVersion) {
			fmt.Fprintf(os.Stderr, "    Warning: mirror delivered %s while the thread lists %s; recording %s so the update is retried.\n", fv, game.LatestVersion, fv)
			installedVersion = fv
		}
		game.Version = installedVersion
		game.VersionSource = "f95"
	}
	if err := database.UpdateGame(game); err != nil {
		fmt.Fprintf(os.Stderr, "    Warning: could not record the install: %v\n", err)
	}
	fmt.Fprintf(os.Stderr, "    Installed: %s\n", installed)
	_ = os.Remove(downloadedFile)
	return true
}
