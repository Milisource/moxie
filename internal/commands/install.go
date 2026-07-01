package commands

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mili/moxie/internal/archive"
	"github.com/mili/moxie/internal/log"
	"github.com/mili/moxie/internal/updater"
)

// Install installs a downloaded game archive into the game directory.
// Usage: moxie install <id|name> <archive-path>
func Install(args []string) {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() < 2 {
		fmt.Fprintf(os.Stderr, "Usage: moxie install <id|name> <archive-path>\n")
		fmt.Fprintf(os.Stderr, "\nFlags:\n")
		fs.PrintDefaults()
		os.Exit(1)
	}

	archivePath := fs.Arg(1)

	// Validate archive exists and is a known format
	if _, err := os.Stat(archivePath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Archive not found: %s\n", archivePath)
		os.Exit(1)
	}
	if !archive.IsArchiveFile(archivePath) {
		fmt.Fprintf(os.Stderr, "Not a recognized archive format: %s\n", archivePath)
		fmt.Fprintf(os.Stderr, "Supported formats: zip, 7z, rar, tar.gz\n")
		os.Exit(1)
	}

	database := OpenDB()
	defer database.Close()

	game := ResolveFirstArg(database, fs.Arg(0))
	if game == nil {
		fmt.Fprintf(os.Stderr, "Cancelled.\n")
		os.Exit(1)
	}

	// Determine destination extraction directory
	destDir := filepath.Join(filepath.Dir(game.Path), "downloads")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating extraction directory: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Extracting archive...\n")

	// Extract the archive
	result, err := archive.Extract(archivePath, destDir, archive.Options{
		OnProgress: func(totalFiles, extractedFiles int, currentFile string, bytesProcessed, bytesTotal int64) {
			if totalFiles > 0 {
				percent := float64(extractedFiles) / float64(totalFiles) * 100
				fmt.Fprintf(os.Stderr, "\r  Extracting: %d/%d files (%.1f%%) - %s", extractedFiles, totalFiles, percent, currentFile)
			}
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nExtraction failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "\n  Extracted %d files to: %s\n", result.FilesExtracted, result.Destination)

	// Merge extracted files into game directory, preserving saves/configs
	fmt.Fprintf(os.Stderr, "\nMerging update into %s...\n", game.Path)
	mergeResult, mergeErr := updater.Merge(game.Path, string(game.Engine), result.Destination, true)
	filesCopied, filesPreserved := 0, 0
	if mergeErr != nil {
		fmt.Fprintf(os.Stderr, "  Merge warning: %v\n", mergeErr)
	} else {
		filesCopied = mergeResult.FilesCopied
		filesPreserved = mergeResult.FilesPreserved
		fmt.Fprintf(os.Stderr, "  Files updated: %d  |  User files preserved: %d\n", filesCopied, filesPreserved)
		if mergeResult.BackupPath != "" {
			fmt.Fprintf(os.Stderr, "  Backup saved to: %s\n", mergeResult.BackupPath)
		}
	}

	// Optionally update the game's version fields
	fmt.Fprintf(os.Stderr, "\n  Current version: %s", game.Version)
	if game.LatestVersion != "" {
		fmt.Fprintf(os.Stderr, " (latest: %s)", game.LatestVersion)
	}
	fmt.Fprintf(os.Stderr, "\n")

	// Update version in DB — if the game has a latest_version from F95Zone, use it;
	// otherwise prompt the user to enter a version (non-interactive, just note it).
	if game.LatestVersion != "" && game.LatestVersion != game.Version {
		game.Version = game.LatestVersion
		if err := database.UpdateGame(game); err != nil {
			log.Warn("failed to update game version", "game_id", game.ID, "error", err)
		} else {
			fmt.Fprintf(os.Stderr, "  Version updated to: %s\n", game.Version)
		}
	} else {
		fmt.Fprintf(os.Stderr, "  Run 'moxie info %d' to see details, or set version with 'moxie config'\n", game.ID)
	}

	// Print summary
	fmt.Fprintf(os.Stderr, "\nInstallation complete!\n")
	fmt.Fprintf(os.Stderr, "  Game:   %s\n", game.Title)
	fmt.Fprintf(os.Stderr, "  Path:   %s\n", game.Path)
	fmt.Fprintf(os.Stderr, "  Files:  %d updated, %d preserved\n", filesCopied, filesPreserved)
}
