package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/image/draw"

	"github.com/mili/moxie/internal/config"
)

// Cover cache layout (config.CoverDir()):
//
//	<id>            original image as downloaded
//	<id>.url        source URL (refresh detection, see cacheCoverCtx)
//	<id>.thumb      grid/list thumbnail (writeCoverThumb)
//	<id>.large      detail-view variant, long edge ≤ coverLargeMaxDim, made
//	                on first request (ensureCoverLarge)
//	<id>.meta.json  coverMeta: source size, tone, provenance, lock
//
// Thumbnails are shaped for the 3:4 grid card. Covers close to portrait are
// centre-cropped to 3:4 and scaled to fit coverThumbW×coverThumbH, so the
// card shows the crop it would show anyway at full card resolution — the old
// "long edge ≤ 480" thumb left a 16:9 banner 270px tall, which the card then
// upscaled 2–3×. Wide banners (aspect > coverWideAspect) are not cropped:
// the card letterboxes them (object-fit: contain over the cover's tone), so
// their thumb only needs the card's width.
const (
	coverThumbW      = 600
	coverThumbH      = 800
	coverWideAspect  = 1.6
	coverWideThumbW  = 720
	coverLargeMaxDim = 1400
	coverThumbQ      = 85

	// coverThumbFormat versions the thumbnail geometry. Part of the backfill
	// marker name, so changing it regenerates every thumb once.
	coverThumbFormat = "t2"
)

// coverMeta is the <id>.meta.json sidecar.
type coverMeta struct {
	W      int    `json:"w"`                // original width
	H      int    `json:"h"`                // original height
	Tone   string `json:"tone,omitempty"`   // average colour, "#rrggbb"
	Source string `json:"source,omitempty"` // f95, steam, steamgriddb, vndb, manual
	URL    string `json:"url,omitempty"`
	Locked bool   `json:"locked,omitempty"` // never auto-replaced
}

func coverPathFor(id int64) string {
	return filepath.Join(config.CoverDir(), strconv.FormatInt(id, 10))
}

// readCoverMeta loads coverPath's sidecar; ok is false when missing/corrupt.
func readCoverMeta(coverPath string) (coverMeta, bool) {
	var m coverMeta
	b, err := os.ReadFile(coverPath + ".meta.json")
	if err != nil || json.Unmarshal(b, &m) != nil {
		return coverMeta{}, false
	}
	return m, true
}

func writeCoverMeta(coverPath string, m coverMeta) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(coverPath+".meta.json", b, 0o644)
}

// updateCoverMeta read-modify-writes the sidecar.
func updateCoverMeta(coverPath string, fn func(*coverMeta)) {
	m, _ := readCoverMeta(coverPath)
	fn(&m)
	if err := writeCoverMeta(coverPath, m); err != nil {
		slog.Warn("failed to write cover meta", "path", coverPath, "error", err)
	}
}

// thumbResult classifies what writeCoverThumb did, so callers can count
// genuine decode failures (corrupt files) and log one summary instead of
// per-cover Warns.
type thumbResult int

const (
	thumbWritten      thumbResult = iota // .thumb written
	thumbNotNeeded                       // kept for callers; no longer produced
	thumbDecodeFailed                    // corrupt/undecodable data
)

func (r thumbResult) String() string {
	switch r {
	case thumbWritten:
		return "written"
	case thumbNotNeeded:
		return "not-needed"
	case thumbDecodeFailed:
		return "decode-failed"
	default:
		return "unknown"
	}
}

// thumbGeometry returns the source crop rectangle and output size for a
// w×h cover. Never upscales.
func thumbGeometry(w, h int) (crop image.Rectangle, outW, outH int) {
	if w <= 0 || h <= 0 {
		return image.Rectangle{}, 0, 0
	}
	if float64(w)/float64(h) > coverWideAspect {
		outW = min(w, coverWideThumbW)
		outH = max(1, int(math.Round(float64(h)*float64(outW)/float64(w))))
		return image.Rect(0, 0, w, h), outW, outH
	}
	// Centre-crop to 3:4.
	cw, ch := w, h
	if w*coverThumbH > h*coverThumbW { // wider than 3:4
		cw = h * coverThumbW / coverThumbH
	} else {
		ch = w * coverThumbH / coverThumbW
	}
	x0, y0 := (w-cw)/2, (h-ch)/2
	crop = image.Rect(x0, y0, x0+cw, y0+ch)
	outW, outH = cw, ch
	if outW > coverThumbW {
		outW, outH = coverThumbW, coverThumbH
	}
	return crop, max(1, outW), max(1, outH)
}

// writeCoverThumb decodes the cover at coverPath, writes the grid thumbnail
// to coverPath+".thumb" and records size and tone in the meta sidecar
// (keeping its source/lock fields). A stale .large is removed so the detail
// view regenerates it. Decode failures are logged as Warn and leave the
// cover server falling back to the full image.
func writeCoverThumb(coverPath string) thumbResult {
	data, err := os.ReadFile(coverPath)
	if err != nil {
		return thumbDecodeFailed
	}
	img, err := decodeCoverImage(data)
	if err != nil {
		slog.Warn("failed to decode cover for thumbnail", "path", coverPath, "error", err)
		return thumbDecodeFailed
	}
	b := img.Bounds()
	tone := averageTone(img)
	crop, w, h := thumbGeometry(b.Dx(), b.Dy())
	crop = crop.Add(b.Min)

	// Fill with the tone first so transparent PNG/WebP covers don't turn
	// black in the JPEG. CatmullRom: generated once and cached, so the
	// sharper filter is worth its cost.
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{tone}, image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, crop, draw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: coverThumbQ}); err != nil {
		return thumbDecodeFailed
	}
	if err := os.WriteFile(coverPath+".thumb", buf.Bytes(), 0o644); err != nil {
		slog.Warn("failed to write cover thumbnail", "path", coverPath, "error", err)
		return thumbDecodeFailed
	}
	_ = os.Remove(coverPath + ".large")
	updateCoverMeta(coverPath, func(m *coverMeta) {
		m.W, m.H = b.Dx(), b.Dy()
		m.Tone = hexColor(tone)
	})
	return thumbWritten
}

// ensureCoverLarge returns the path of the detail-view variant: the
// original when its long edge is already ≤ coverLargeMaxDim (and it's a
// format the webview decodes natively), else a downscaled JPEG generated on
// first request and cached as .large. Decoding a 3840px cover in the
// webview costs ~60 MB of bitmap per view; 1400px is ~8 MB.
func ensureCoverLarge(coverPath string) string {
	large := coverPath + ".large"
	if fi, err := os.Stat(large); err == nil {
		if ofi, oerr := os.Stat(coverPath); oerr == nil && !fi.ModTime().Before(ofi.ModTime()) {
			return large
		}
	}
	if m, ok := readCoverMeta(coverPath); ok && max(m.W, m.H) > 0 && max(m.W, m.H) <= coverLargeMaxDim {
		return coverPath
	}
	data, err := os.ReadFile(coverPath)
	if err != nil {
		return coverPath
	}
	img, err := decodeCoverImage(data)
	if err != nil {
		return coverPath
	}
	b := img.Bounds()
	long := max(b.Dx(), b.Dy())
	if long <= coverLargeMaxDim {
		return coverPath
	}
	r := float64(coverLargeMaxDim) / float64(long)
	w, h := max(1, int(float64(b.Dx())*r)), max(1, int(float64(b.Dy())*r))
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{averageTone(img)}, image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 88}); err != nil {
		return coverPath
	}
	if err := os.WriteFile(large, buf.Bytes(), 0o644); err != nil {
		return coverPath
	}
	return large
}

// averageTone is the mean colour of img, sampled on a coarse grid (≤ 64×64
// points) — enough for a letterbox backdrop.
func averageTone(img image.Image) color.RGBA {
	b := img.Bounds()
	if b.Empty() {
		return color.RGBA{0x22, 0x22, 0x2e, 0xff}
	}
	stepX, stepY := max(1, b.Dx()/64), max(1, b.Dy()/64)
	var r, g, bl, n uint64
	for y := b.Min.Y; y < b.Max.Y; y += stepY {
		for x := b.Min.X; x < b.Max.X; x += stepX {
			cr, cg, cb, ca := img.At(x, y).RGBA()
			if ca == 0 {
				continue
			}
			r += uint64(cr >> 8)
			g += uint64(cg >> 8)
			bl += uint64(cb >> 8)
			n++
		}
	}
	if n == 0 {
		return color.RGBA{0x22, 0x22, 0x2e, 0xff}
	}
	return color.RGBA{uint8(r / n), uint8(g / n), uint8(bl / n), 0xff}
}

func hexColor(c color.RGBA) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

// coverMetaFromDir loads every meta sidecar in the cover directory, keyed by
// game ID. Shares coverSetFromDir's cache window.
func coverMetaFromDir() map[int64]coverMeta {
	coverSetFromDir() // refreshes coverSetCache (and its meta) when stale
	coverSetCache.mu.Lock()
	defer coverSetCache.mu.Unlock()
	return coverSetCache.meta
}

// loadCoverMetas reads all <id>.meta.json files listed in entries.
func loadCoverMetas(dir string, entries []os.DirEntry) map[int64]coverMeta {
	out := make(map[int64]coverMeta)
	for _, e := range entries {
		name := e.Name()
		idStr, ok := strings.CutSuffix(name, ".meta.json")
		if !ok {
			continue
		}
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			continue
		}
		if m, ok := readCoverMeta(filepath.Join(dir, idStr)); ok {
			out[id] = m
		}
	}
	return out
}

// coverSourceForURL names the provider a cover URL came from.
func coverSourceForURL(u string) string {
	switch {
	case strings.Contains(u, "steamstatic.com"), strings.Contains(u, "steampowered.com"):
		return "steam"
	case strings.Contains(u, "steamgriddb.com"):
		return "steamgriddb"
	case strings.Contains(u, "vndb.org"):
		return "vndb"
	case strings.Contains(u, "dlsite"):
		return "dlsite"
	default:
		return "f95"
	}
}
