package steam

import (
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG decoder for HTTP downloads
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/image/draw"
)

// httpClient is the shared HTTP client for all Steam grid artwork downloads.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// ---------------------------------------------------------------------------
// Path helpers
// ---------------------------------------------------------------------------

// GridFilePath builds the absolute path for a grid artwork file.
// Example: GridFilePath(steamRoot, 123456789, 0x81234567, ArtVertical)
//
//	→ "/home/user/.steam/steam/userdata/123456789/config/grid/2166883687p.png"
func GridFilePath(steamRoot string, userID3, appID uint32, artType ArtType) string {
	gridDir := filepath.Join(steamRoot, "userdata", fmt.Sprintf("%d", userID3), "config", "grid")
	filename := fmt.Sprintf("%d%s.png", appID, artType.Suffix())
	return filepath.Join(gridDir, filename)
}

// ---------------------------------------------------------------------------
// High-level per-type download-helpers
// ---------------------------------------------------------------------------

// DownloadAndSetCover downloads an image from coverURL, resizes it to
// 600×900 (Steam vertical grid standard), and writes it to the grid directory.
// If coverURL is empty, returns nil (no-op).
func DownloadAndSetCover(steamRoot string, userID3, appID uint32, coverURL string) error {
	if coverURL == "" {
		return nil
	}
	return downloadAndResize(steamRoot, userID3, appID, coverURL, ArtVertical)
}

// DownloadAndSetHero downloads and resizes to 1920×620 for the hero banner.
func DownloadAndSetHero(steamRoot string, userID3, appID uint32, coverURL string) error {
	if coverURL == "" {
		return nil
	}
	return downloadAndResize(steamRoot, userID3, appID, coverURL, ArtHero)
}

// DownloadAndSetHorizontal downloads and resizes to 460×215 for the horizontal grid.
func DownloadAndSetHorizontal(steamRoot string, userID3, appID uint32, coverURL string) error {
	if coverURL == "" {
		return nil
	}
	return downloadAndResize(steamRoot, userID3, appID, coverURL, ArtHorizontal)
}

// SetAllArtwork downloads the cover image once and resizes it for all three
// grid art types (vertical, horizontal, hero). The vertical grid is mandatory;
// horizontal and hero failures are silently skipped.
//
// If coverURL is empty, returns nil (no-op).
func SetAllArtwork(steamRoot string, userID3, appID uint32, coverURL string) error {
	if coverURL == "" {
		return nil
	}

	// Skip non-downloadable URLs (data: URIs, SVGs).
	if strings.HasPrefix(coverURL, "data:") || strings.Contains(coverURL, "/svg") {
		return ErrUnsupportedFormat
	}

	// 1. Download the image once.
	resp, err := httpClient.Get(coverURL)
	if err != nil {
		return fmt.Errorf("steam: download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("steam: download returned HTTP %d", resp.StatusCode)
	}

	// 2. Decode the source image.
	src, _, err := image.Decode(resp.Body)
	if err != nil {
		return fmt.Errorf("steam: decode failed: %w", err)
	}

	// 3. Resize and save to each artwork type.
	for _, artType := range []ArtType{ArtVertical, ArtHorizontal, ArtHero} {
		if err := resizeAndSave(steamRoot, userID3, appID, src, artType); err != nil {
			if artType == ArtVertical {
				return fmt.Errorf("steam: vertical grid failed: %w", err)
			}
			// Horizontal and hero are best-effort; continue on failure.
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Core resize-and-save / download-and-resize logic
// ---------------------------------------------------------------------------

// resizeAndSave resizes a decoded image to the target artwork dimensions
// and saves it as PNG in the grid directory.
func resizeAndSave(steamRoot string, userID3, appID uint32, src image.Image, artType ArtType) error {
	// 1. Resize to target dimensions using CatmullRom scaler.
	w, h := artType.Dimensions()
	if w == 0 || h == 0 {
		return nil // variable-size types not supported
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)

	// 2. Write the PNG file.
	destPath := GridFilePath(steamRoot, userID3, appID, artType)
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("steam: cannot create grid dir: %w", err)
	}
	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("steam: cannot create file %s: %w", destPath, err)
	}
	defer f.Close()

	if err := png.Encode(f, dst); err != nil {
		return fmt.Errorf("steam: png encode failed: %w", err)
	}

	return nil
}

// downloadAndResize downloads an image, resizes it to the target artwork
// dimensions, and saves it as PNG in the grid directory.
func downloadAndResize(steamRoot string, userID3, appID uint32, url string, artType ArtType) error {
	// Skip non-HTTP URLs (data: URIs, SVGs) — we can't decode these.
	if strings.HasPrefix(url, "data:") || strings.Contains(url, "/svg") {
		return ErrUnsupportedFormat
	}

	// 1. Download the image.
	resp, err := httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("steam: download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("steam: download returned HTTP %d", resp.StatusCode)
	}

	// 2. Decode the image.
	src, _, err := image.Decode(resp.Body)
	if err != nil {
		return fmt.Errorf("steam: decode failed: %w", err)
	}

	// 3. Resize to target dimensions using CatmullRom scaler.
	w, h := artType.Dimensions()
	if w == 0 || h == 0 {
		return nil // variable-size types not supported
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)

	// 4. Write the PNG file.
	destPath := GridFilePath(steamRoot, userID3, appID, artType)
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("steam: cannot create grid dir: %w", err)
	}
	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("steam: cannot create file %s: %w", destPath, err)
	}
	defer f.Close()

	if err := png.Encode(f, dst); err != nil {
		return fmt.Errorf("steam: png encode failed: %w", err)
	}

	return nil
}
