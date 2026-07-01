package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/mili/moxie/internal/log"
)

// githubRelease represents the GitHub API response for a release.
type githubRelease struct {
	TagName string       `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Update checks for a new moxie release, downloads it, and replaces the binary.
func Update(version string) {
	fmt.Fprintf(os.Stderr, "  Checking for updates (current: %s)...\n", version)

	release, err := fetchLatestRelease()
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ⚠ Failed to check for updates: %v\n", err)
		os.Exit(1)
	}

	latest := strings.TrimPrefix(release.TagName, "v")
	if !isNewerVersion(latest, version) {
		fmt.Fprintf(os.Stderr, "  ✓ Already up to date (%s)\n", version)
		return
	}

	fmt.Fprintf(os.Stderr, "  New version available: %s → %s\n", version, latest)

	assetName := binaryAssetName()
	var downloadURL string
	for _, a := range release.Assets {
		if a.Name == assetName {
			downloadURL = a.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		fmt.Fprintf(os.Stderr, "  ⚠ No binary found for %s in release %s\n", assetName, release.TagName)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "  Downloading %s...\n", assetName)
	tmpPath, err := downloadBinary(downloadURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ⚠ Download failed: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(tmpPath)

	if err := replaceBinary(tmpPath); err != nil {
		fmt.Fprintf(os.Stderr, "  ⚠ Failed to install update: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "  ✓ Updated to %s\n", latest)
	fmt.Fprintf(os.Stderr, "  Restart moxie to use the new version.\n")
}

// fetchLatestRelease fetches the latest release info from GitHub.
func fetchLatestRelease() (*githubRelease, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/repos/Milisource/moxie/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "moxie-updater")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github API: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("github API: decode: %w", err)
	}
	return &release, nil
}

// binaryAssetName returns the expected asset filename for the current platform.
func binaryAssetName() string {
	arch := runtime.GOARCH
	if arch == "aarch64" {
		arch = "arm64"
	}

	switch runtime.GOOS {
	case "linux":
		return fmt.Sprintf("moxie-linux-%s", arch)
	case "darwin":
		return fmt.Sprintf("moxie-macos-%s", arch)
	case "windows":
		return fmt.Sprintf("moxie-windows-%s.exe", arch)
	default:
		return "moxie"
	}
}

// downloadBinary downloads a file to a temporary path and returns the path.
func downloadBinary(url string) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("User-Agent", "moxie-updater")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}

	tmpPath := filepath.Join(os.TempDir(), "moxie-update-"+binaryAssetName())
	f, err := os.Create(tmpPath)
	if err != nil {
		return "", fmt.Errorf("create temp: %w", err)
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("write temp: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return "", err
	}

	if err := os.Chmod(tmpPath, 0755); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("chmod: %w", err)
	}

	return tmpPath, nil
}

// renameOrCopy attempts an atomic rename across the same filesystem, falling
// back to copy+delete when src and dest are on different mount points.
func renameOrCopy(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	// If the error is a cross-device link (rename across filesystems),
	// fall back to copy + delete.
	if strings.Contains(err.Error(), "invalid cross-device link") ||
		strings.Contains(err.Error(), "The system cannot move the file") {
		log.Debug("rename across filesystems, falling back to copy", "src", src, "dst", dst)
		return copyFile(src, dst)
	}
	return err
}

// copyFile copies a file from src to dst, preserving permissions.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	return nil
}

// replaceBinary atomically replaces the current binary with the downloaded one.
func replaceBinary(tmpPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot find current binary: %w", err)
	}

	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("cannot resolve symlink: %w", err)
	}

	backupPath := exe + ".bak"
	if err := renameOrCopy(exe, backupPath); err != nil {
		return fmt.Errorf("cannot back up current binary: %w", err)
	}

	if err := renameOrCopy(tmpPath, exe); err != nil {
		// Restore backup on failure.
		renameOrCopy(backupPath, exe)
		return fmt.Errorf("cannot install update: %w", err)
	}

	os.Remove(backupPath)
	return nil
}

// isNewerVersion returns true if latest > current using semver-like comparison.
func isNewerVersion(latest, current string) bool {
	// Strip leading 'v' and prerelease suffixes for comparison.
	clean := func(v string) string {
		v = strings.TrimPrefix(v, "v")
		if idx := strings.IndexAny(v, "-+"); idx >= 0 {
			v = v[:idx]
		}
		return v
	}

	latest = clean(latest)
	current = clean(current)

	partsL := strings.Split(latest, ".")
	partsC := strings.Split(current, ".")
	maxLen := len(partsL)
	if len(partsC) > maxLen {
		maxLen = len(partsC)
	}

	for i := 0; i < maxLen; i++ {
		var a, b int
		if i < len(partsL) {
			a, _ = strconv.Atoi(partsL[i])
		}
		if i < len(partsC) {
			b, _ = strconv.Atoi(partsC[i])
		}
		if a != b {
			return a > b
		}
	}
	return false
}
