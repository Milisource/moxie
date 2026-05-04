package archive

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func extract7z(archivePath, destDir string, opts Options) (*Result, error) {
	// Try 7z command line tool first
	if _, err := exec.LookPath("7z"); err == nil {
		return extract7zWithTool(archivePath, destDir, "7z", opts)
	}
	// Fallback to 7zz (p7zip on some systems)
	if _, err := exec.LookPath("7zz"); err == nil {
		return extract7zWithTool(archivePath, destDir, "7zz", opts)
	}

	return nil, fmt.Errorf("7z extraction requires '7z' or '7zz' command (install p7zip-full)")
}

func extract7zWithTool(archivePath, destDir, tool string, opts Options) (*Result, error) {
	args := []string{"x", archivePath, "-o" + destDir, "-y"}
	if opts.Password != "" {
		args = append(args, "-p"+opts.Password)
	} else {
		args = append(args, "-p-") // No password
	}

	cmd := exec.Command(tool, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("7z extraction failed: %w", err)
	}

	// Count extracted files
	result := &Result{Destination: destDir}
	if err := countExtractedFiles(destDir, result); err != nil {
		return nil, err
	}

	return result, nil
}

func extractRar(archivePath, destDir string, opts Options) (*Result, error) {
	// Try unrar-nonfree or unrar-free
	tools := []string{"unrar", "unrar-nonfree", "rar"}
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err == nil {
			return extractRarWithTool(archivePath, destDir, tool, opts)
		}
	}

	// Fallback to 7z which can also handle rar
	if _, err := exec.LookPath("7z"); err == nil {
		return extract7zWithTool(archivePath, destDir, "7z", opts)
	}

	return nil, fmt.Errorf("rar extraction requires 'unrar' or '7z' command")
}

func extractRarWithTool(archivePath, destDir, tool string, opts Options) (*Result, error) {
	var args []string
	if tool == "rar" || tool == "unrar" || tool == "unrar-nonfree" {
		args = []string{"x", "-o+", archivePath, destDir}
		if opts.Password != "" {
			args = append([]string{"-p" + opts.Password}, args...)
		}
	} else {
		// unrar-free has different syntax
		args = []string{"x", archivePath, destDir}
	}

	cmd := exec.Command(tool, args...)
	cmd.Dir = destDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("unrar extraction failed: %w", err)
	}

	result := &Result{Destination: destDir}
	if err := countExtractedFiles(destDir, result); err != nil {
		return nil, err
	}

	return result, nil
}

func countExtractedFiles(root string, result *Result) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			result.FilesExtracted++
			result.TotalBytes += info.Size()
		}
		return nil
	})
}

// ExtractWithFallback tries multiple methods to extract an archive.
// First tries native Go implementations, then falls back to system tools.
func ExtractWithFallback(archivePath, destDir string, opts Options) (*Result, error) {
	format := DetectFormat(archivePath)
	if format == FormatUnknown {
		return nil, fmt.Errorf("unknown archive format: %s", archivePath)
	}

	// For zip, try unzip command if available (often faster)
	if format == FormatZip {
		if _, err := exec.LookPath("unzip"); err == nil {
			return extractZipWithUnzip(archivePath, destDir, opts)
		}
	}

	// Use native Go implementation
	return Extract(archivePath, destDir, opts)
}

func extractZipWithUnzip(archivePath, destDir string, opts Options) (*Result, error) {
	args := []string{"-o", archivePath, "-d", destDir}
	if opts.Password != "" {
		args = append([]string{"-P", opts.Password}, args...)
	}

	cmd := exec.Command("unzip", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if it's a password error
		if strings.Contains(string(output), "incorrect password") {
			return nil, fmt.Errorf("incorrect password")
		}
		return nil, fmt.Errorf("unzip failed: %w", err)
	}

	result := &Result{Destination: destDir}
	if err := countExtractedFiles(destDir, result); err != nil {
		return nil, err
	}

	return result, nil
}
