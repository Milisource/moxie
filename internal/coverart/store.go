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

	// The landscape banner kept alongside the primary cover, used by the wide
	// library view. It is stored as the cover's `.banner` file; these fields
	// record its geometry and provenance. Empty when no banner is retained
	// (the wide view then falls back to a crop of the primary).
	BannerW      int    `json:"banW,omitempty"`
	BannerH      int    `json:"banH,omitempty"`
	BannerSource string `json:"banSource,omitempty"`
	BannerURL    string `json:"banURL,omitempty"`

	// WideW/WideH is the rendered `.wide` rendition's geometry. Part of the
	// cache-busting token, so a regenerated wide image is re-fetched.
	WideW int `json:"wideW,omitempty"`
	WideH int `json:"wideH,omitempty"`
}

// BannerAspect is the minimum width/height ratio for an image to count as a
// landscape banner worth keeping for the wide view (4:3 and wider).
const BannerAspect = 1.3

// BannerPath returns coverPath's retained landscape banner file.
func BannerPath(coverPath string) string { return coverPath + ".banner" }

// StoreBanner writes data as coverPath's landscape banner when it is a
// landscape image (aspect ≥ BannerAspect), recording its size/source/url in
// the sidecar. Returns false (and leaves any existing banner untouched) when
// data is portrait or square. A missing sidecar is created.
func StoreBanner(coverPath string, data []byte, source, url string) (bool, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return false, err
	}
	if cfg.Height <= 0 || float64(cfg.Width)/float64(cfg.Height) < BannerAspect {
		return false, nil
	}
	if err := os.WriteFile(BannerPath(coverPath), data, 0o644); err != nil {
		return false, err
	}
	m, _ := ReadMeta(coverPath)
	m.BannerW, m.BannerH, m.BannerSource, m.BannerURL = cfg.Width, cfg.Height, source, url
	if err := WriteMeta(coverPath, m); err != nil {
		return false, err
	}
	return true, nil
}

// HasBanner reports whether a landscape banner is retained for coverPath.
func HasBanner(coverPath string) bool {
	if _, err := os.Stat(BannerPath(coverPath)); err != nil {
		return false
	}
	m, ok := ReadMeta(coverPath)
	return ok && m.BannerW > 0
}

// RemoveBanner deletes coverPath's retained banner and clears its sidecar
// fields. Used when the primary cover itself becomes landscape (it is then its
// own banner, so a stale retained one would shadow it).
func RemoveBanner(coverPath string) {
	_ = os.Remove(BannerPath(coverPath))
	m, ok := ReadMeta(coverPath)
	if !ok || (m.BannerW == 0 && m.BannerSource == "" && m.BannerURL == "") {
		return
	}
	m.BannerW, m.BannerH, m.BannerSource, m.BannerURL = 0, 0, "", ""
	_ = WriteMeta(coverPath, m)
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
// derived .thumb/.large/.wide files so they are regenerated. The previous
// cover is kept as coverPath+".prev" for one-step undo. A retained landscape
// banner (.banner) and its sidecar fields are carried across, so replacing the
// primary cover does not lose the wide-view art.
func Store(coverPath string, data []byte, cfg image.Config, c Candidate, locked bool) error {
	prev, _ := ReadMeta(coverPath)
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
	_ = os.Remove(coverPath + ".wide")
	return WriteMeta(coverPath, Meta{
		W: cfg.Width, H: cfg.Height, Source: c.Source, URL: c.URL, Locked: locked,
		BannerW: prev.BannerW, BannerH: prev.BannerH,
		BannerSource: prev.BannerSource, BannerURL: prev.BannerURL,
	})
}
