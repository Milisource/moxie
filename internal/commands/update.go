package commands

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/mili/moxie/internal/config"
	"github.com/mili/moxie/internal/log"
)

// githubRelease represents the GitHub API response for a release.
type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	// Digest is "sha256:<hex>" when GitHub has computed one. It is absent for
	// older assets, so verification is best-effort.
	Digest string `json:"digest"`
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
	var downloadURL, expectedDigest string
	for _, a := range release.Assets {
		if a.Name == assetName {
			downloadURL = a.BrowserDownloadURL
			expectedDigest = a.Digest
			break
		}
	}
	if downloadURL == "" {
		fmt.Fprintf(os.Stderr, "  ⚠ No binary found for %s in release %s\n", assetName, release.TagName)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "  Downloading %s...\n", assetName)
	tmpPath, err := downloadBinary(downloadURL, expectedDigest)
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
	if runtime.GOOS == "windows" {
		fmt.Fprintf(os.Stderr, "  The new binary is staged and will replace this one when moxie exits.\n")
	}
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

// updateStageDir returns a private staging directory for update binaries.
// A predictable name in world-writable /tmp lets any other local user
// pre-create or symlink the path and choose what the app runs after an
// update, so staging lives in the user's private config dir instead.
func updateStageDir() (string, error) {
	dir := filepath.Join(config.ConfigDir(), "updates")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create update staging directory: %w", err)
	}
	return dir, nil
}

// downloadBinary downloads a file to the private staging directory and
// returns its path. expectedDigest is "sha256:<hex>" when the release
// provides one; a mismatch discards the download.
func downloadBinary(url, expectedDigest string) (string, error) {
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

	stageDir, err := updateStageDir()
	if err != nil {
		return "", err
	}
	tmpPath := filepath.Join(stageDir, binaryAssetName())
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0700)
	if err != nil {
		return "", fmt.Errorf("create temp: %w", err)
	}

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, hasher), resp.Body); err != nil {
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

	// Verify against the digest GitHub publishes for the asset.
	if expectedDigest != "" {
		got := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
		if !strings.EqualFold(got, expectedDigest) {
			os.Remove(tmpPath)
			return "", fmt.Errorf("update integrity check failed: expected %s, got %s", expectedDigest, got)
		}
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

// replaceBinary installs the downloaded binary over the current executable.
// On Windows the running exe is locked and cannot be renamed or replaced in
// place (ERROR_ACCESS_DENIED), so the update is staged instead: the new
// binary is copied next to the exe and a small detached .bat helper is
// launched which waits for this process to exit (releasing the lock), moves
// the new binary over the exe, and deletes itself. On other platforms the
// current binary is renamed aside and the new one moved into place atomically.
func replaceBinary(tmpPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot find current binary: %w", err)
	}

	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("cannot resolve symlink: %w", err)
	}

	if runtime.GOOS == "windows" {
		return stageWindowsReplace(tmpPath, exe)
	}

	backupPath := exe + ".bak"
	if err := renameOrCopy(exe, backupPath); err != nil {
		return fmt.Errorf("cannot back up current binary: %w", err)
	}

	if err := renameOrCopy(tmpPath, exe); err != nil {
		// Restore backup on failure — a failed restore leaves no working
		// binary, so the error must be surfaced loudly.
		if rbErr := renameOrCopy(backupPath, exe); rbErr != nil {
			return fmt.Errorf("cannot install update: %w (and restoring backup failed: %v)", err, rbErr)
		}
		return fmt.Errorf("cannot install update: %w", err)
	}

	os.Remove(backupPath)
	return nil
}

// stageWindowsReplace stages a Windows self-update. The running exe is locked
// for as long as this process lives, so the swap must happen after exit: the
// downloaded binary is copied to <exe>.new (same directory, so the helper's
// move is a same-volume rename), a detached helper .bat is written next to
// it, and launched via `cmd /c start /min`. The helper loops `move /y` until
// the lock releases, swaps the binary, then deletes itself and the .new file.
// Any setup failure cleans up after itself and returns an actionable error —
// the installed exe is never touched on this path.
func stageWindowsReplace(tmpPath, exe string) error {
	newPath := exe + ".new"

	// The helper references files by base name via %~dp0, so the .new copy
	// must live next to the exe. Clear a stale copy from a previous attempt
	// first (copyFile opens the destination with O_EXCL).
	os.Remove(newPath)
	if err := copyFile(tmpPath, newPath); err != nil {
		return fmt.Errorf("cannot stage new binary: %w", err)
	}

	batPath := exe + ".update.bat"
	batContent := windowsSwapBat(filepath.Base(newPath), filepath.Base(exe))
	if err := os.WriteFile(batPath, []byte(batContent), 0700); err != nil {
		os.Remove(newPath)
		return fmt.Errorf("cannot write update helper: %w", err)
	}

	// Detached launch: cmd's `start` builtin returns immediately, running the
	// helper in its own minimized console window that survives this process's
	// exit — only then is the exe unlocked for the move.
	cmd := exec.Command("cmd", "/c", "start", "", "/min", batPath)
	if err := cmd.Start(); err != nil {
		os.Remove(newPath)
		os.Remove(batPath)
		return fmt.Errorf("cannot start update helper: %w", err)
	}
	return nil
}

// windowsSwapBat renders the self-deleting helper .bat that completes a
// staged Windows update. newName and exeName are the base filenames of the
// new binary and the installed executable; the bat resolves them next to
// itself via %~dp0, so it must be written into the exe's directory. It loops
// `move /y` until the running exe's lock is released, then removes itself.
func windowsSwapBat(newName, exeName string) string {
	newName = escapeBatch(newName)
	exeName = escapeBatch(exeName)
	return "@echo off\r\n" +
		"rem moxie self-update helper - swaps the new binary over the running exe.\r\n" +
		"set \"SRC=%~dp0" + newName + "\"\r\n" +
		"set \"DST=%~dp0" + exeName + "\"\r\n" +
		":retry\r\n" +
		"move /y \"%SRC%\" \"%DST%\" >nul 2>&1\r\n" +
		"if exist \"%SRC%\" (\r\n" +
		"  ping -n 3 127.0.0.1 >nul 2>&1\r\n" +
		"  goto retry\r\n" +
		")\r\n" +
		"del \"%SRC%\" >nul 2>&1\r\n" +
		"(goto) 2>nul & del \"%~f0\"\r\n"
}

// escapeBatch doubles '%' characters so batch-file expansion cannot mangle
// filenames that contain them.
func escapeBatch(s string) string {
	return strings.ReplaceAll(s, "%", "%%")
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
