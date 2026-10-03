package coverart

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"  // DecodeConfig
	_ "image/jpeg" // DecodeConfig
	_ "image/png"  // DecodeConfig
	"io"
	"net/http"
	"os"
	"time"

	_ "github.com/gen2brain/avif" // DecodeConfig (most F95 covers)
	_ "golang.org/x/image/webp"   // DecodeConfig
)

// Meta is the <id>.meta.json sidecar next to a cached cover (written by the
// desktop app's cover cache and by Store).
type Meta struct {
	W      int    `json:"w"`                // original width
	H      int    `json:"h"`                // original height
	Tone   string `json:"tone,omitempty"`   // average colour, "#rrggbb"
	Source string `json:"source,omitempty"` // f95, steam, steamgriddb, vndb, manual
	URL    string `json:"url,omitempty"`
	Locked bool   `json:"locked,omitempty"` // never auto-replaced
}

// ReadMeta loads coverPath's sidecar; ok is false when missing/corrupt.
func ReadMeta(coverPath string) (Meta, bool) {
	var m Meta
	b, err := os.ReadFile(coverPath + ".meta.json")
	if err != nil || json.Unmarshal(b, &m) != nil {
		return Meta{}, false
	}
	return m, true
}

// WriteMeta writes coverPath's sidecar.
func WriteMeta(coverPath string, m Meta) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(coverPath+".meta.json", b, 0o644)
}

// CurrentSize returns the cached cover's pixel size from its sidecar, or by
// reading the image header. Zeros when there is no (decodable) cover.
func CurrentSize(coverPath string) (w, h int) {
	if m, ok := ReadMeta(coverPath); ok && m.W > 0 && m.H > 0 {
		return m.W, m.H
	}
	f, err := os.Open(coverPath)
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

// MaxImageBytes caps a cover download.
const MaxImageBytes = 16 << 20

// Fetch downloads an image URL (capped, must decode as an image header).
func Fetch(ctx context.Context, url string) ([]byte, image.Config, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, image.Config{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; moxie cover art)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, image.Config{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, image.Config{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxImageBytes+1))
	if err != nil {
		return nil, image.Config{}, err
	}
	if len(data) > MaxImageBytes {
		return nil, image.Config{}, fmt.Errorf("image exceeds %d bytes", MaxImageBytes)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, image.Config{}, fmt.Errorf("not a decodable image: %w", err)
	}
	return data, cfg, nil
}

// Store replaces the cover at coverPath with data from candidate c: writes
// the image, the .url marker and the sidecar (keeping Locked), and removes
// derived .thumb/.large files so they are regenerated. The previous cover is
// kept as coverPath+".prev" for one-step undo.
func Store(coverPath string, data []byte, cfg image.Config, c Candidate, locked bool) error {
	if _, err := os.Stat(coverPath); err == nil {
		_ = os.Remove(coverPath + ".prev")
		if err := os.Rename(coverPath, coverPath+".prev"); err != nil {
			return err
		}
		if b, err := os.ReadFile(coverPath + ".meta.json"); err == nil {
			_ = os.WriteFile(coverPath+".prev.meta.json", b, 0o644)
		}
	}
	tmp := coverPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, coverPath); err != nil {
		os.Remove(tmp)
		return err
	}
	_ = os.WriteFile(coverPath+".url", []byte(c.URL), 0o644)
	_ = os.Remove(coverPath + ".thumb")
	_ = os.Remove(coverPath + ".large")
	return WriteMeta(coverPath, Meta{W: cfg.Width, H: cfg.Height, Source: c.Source, URL: c.URL, Locked: locked})
}
