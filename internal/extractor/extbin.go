package extractor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// extract7z extracts a .7z archive using the 7z binary (p7zip).
func extract7z(ctx context.Context, archivePath, destDir string) error {
	if _, err := exec.LookPath("7z"); err != nil {
		return fmt.Errorf(
			"%w: 7z is not installed; install p7zip-full (apt: sudo apt install p7zip-full, brew: brew install p7zip)",
			ErrBinNotInstalled,
		)
	}

	args := []string{"x", archivePath, fmt.Sprintf("-o%s", destDir), "-y"}
	return runExtCmd(ctx, "7z", args)
}

// extractRar extracts a .rar archive using unrar (preferred) or 7z as fallback.
func extractRar(ctx context.Context, archivePath, destDir string) error {
	// Try unrar first (native RAR support).
	if _, err := exec.LookPath("unrar"); err == nil {
		args := []string{"x", "-y", archivePath, destDir + "/"}
		return runExtCmd(ctx, "unrar", args)
	}

	// Fall back to 7z which also handles RAR.
	if _, err := exec.LookPath("7z"); err == nil {
		args := []string{"x", archivePath, fmt.Sprintf("-o%s", destDir), "-y"}
		return runExtCmd(ctx, "7z", args)
	}

	return fmt.Errorf(
		"%w: neither unrar nor 7z is installed; install one of: unrar (apt: sudo apt install unrar, brew: brew install unrar), p7zip-full (apt: sudo apt install p7zip-full, brew: brew install p7zip)",
		ErrBinNotInstalled,
	)
}

// runExtCmd runs an external extraction command with context support.
func runExtCmd(ctx context.Context, name string, args []string) error {
	cmd := exec.CommandContext(ctx, name, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return fmt.Errorf("%s extraction failed: %s", name, errMsg)
		}
		return fmt.Errorf("%s extraction failed: %w", name, err)
	}

	return nil
}
