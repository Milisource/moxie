package main

import (
	"bytes"
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
	"github.com/mili/moxie/internal/coverart"
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

	// Wide-view rendition (`.wide`). The wide card is landscape and tops out
	// around 960 CSS px on a large window, so 960 is the cap. It is built from
	// the retained landscape banner (coverart.BannerPath) when there is one,
	// else from the primary cover.
	coverWideMaxDim = 960
	coverWideQ      = 85

	// coverThumbFormat versions the thumbnail geometry. Part of the backfill
	// marker name, so changing it regenerates every thumb once.
	coverThumbFormat = "t3"
)

// coverMeta is the <id>.meta.json sidecar (format shared with the CLI).
type coverMeta = coverart.Meta

func coverPathFor(id int64) string {
	return filepath.Join(config.CoverDir(), strconv.FormatInt(id, 10))
}

func readCoverMeta(coverPath string) (coverMeta, bool) { return coverart.ReadMeta(coverPath) }

func writeCoverMeta(coverPath string, m coverMeta) error { return coverart.WriteMeta(coverPath, m) }

// coverPinned reports whether the cached cover must not be replaced by the
// F95 thread's cover: the user locked it, or it came from another source
// (an upgrade). Without this the next sync would re-fetch the banner.
func coverPinned(coverPath string) bool {
	m, ok := readCoverMeta(coverPath)
	return ok && (m.Locked || (m.Source != "" && m.Source != "f95"))
}

// coverNeedsBanner reports whether the game has a cover but no retained
// landscape banner, so the wide view would fall back to a crop of a portrait
// primary. Games with no cover are excluded — there is nothing to derive a
// banner from, and their grid card is empty too.
func coverNeedsBanner(coverPath string) bool {
	w, h := coverart.CurrentSize(coverPath)
	if w <= 0 || h <= 0 || isLandscape(w, h) {
		return false
	}
	return !coverart.HasBanner(coverPath)
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

// isLandscape reports whether w×h is wide enough to serve as a banner.
func isLandscape(w, h int) bool {
	return h > 0 && float64(w)/float64(h) >= coverart.BannerAspect
}

// wideGeometry returns the source crop rectangle and output size for a w×h
// cover rendered for the wide (landscape) card. Landscape banners keep their
// whole frame and are scaled to coverWideMaxDim wide; portrait or square art
// is centre-cropped to 16:9. Never upscales.
func wideGeometry(w, h int) (crop image.Rectangle, outW, outH int) {
	if w <= 0 || h <= 0 {
		return image.Rectangle{}, 0, 0
	}
	if isLandscape(w, h) {
		outW = min(w, coverWideMaxDim)
		outH = max(1, int(math.Round(float64(h)*float64(outW)/float64(w))))
		return image.Rect(0, 0, w, h), outW, outH
	}
	cw, ch := w, h
	if w*9 > h*16 { // wider than 16:9
		cw = h * 16 / 9
	} else {
		ch = w * 9 / 16
	}
	x0, y0 := (w-cw)/2, (h-ch)/2
	crop = image.Rect(x0, y0, x0+cw, y0+ch)
	outW, outH = cw, ch
	if outW > coverWideMaxDim {
		outW = coverWideMaxDim
		outH = max(1, int(math.Round(float64(ch)*float64(outW)/float64(cw))))
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

// writeCoverWide renders the wide-view rendition to coverPath+".wide" from the
// retained landscape banner when there is one, else from the primary cover.
// Landscape sources keep their whole frame; portrait art is centre-cropped to
// 16:9. Records the rendition size in the sidecar. On failure the cover server
// falls back to the primary image.
func writeCoverWide(coverPath string) thumbResult {
	src := coverPath
	if _, err := os.Stat(coverart.BannerPath(coverPath)); err == nil {
		src = coverart.BannerPath(coverPath)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return thumbDecodeFailed
	}
	img, err := decodeCoverImage(data)
	if err != nil {
		slog.Warn("failed to decode cover for wide rendition", "path", src, "error", err)
		return thumbDecodeFailed
	}
	b := img.Bounds()
	tone := averageTone(img)
	crop, w, h := wideGeometry(b.Dx(), b.Dy())
	crop = crop.Add(b.Min)

	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{tone}, image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, crop, draw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: coverWideQ}); err != nil {
		return thumbDecodeFailed
	}
	if err := os.WriteFile(coverPath+".wide", buf.Bytes(), 0o644); err != nil {
		slog.Warn("failed to write wide cover rendition", "path", coverPath, "error", err)
		return thumbDecodeFailed
	}
	updateCoverMeta(coverPath, func(m *coverMeta) { m.WideW, m.WideH = w, h })
	return thumbWritten
}

// writeCoverRenditions rebuilds both the grid (.thumb) and wide (.wide)
// renditions and returns the grid write result. The wide pass is best-effort.
func writeCoverRenditions(coverPath string) thumbResult {
	r := writeCoverThumb(coverPath)
	writeCoverWide(coverPath)
	return r
}

// captureCoverBanner preserves coverPath's current image as the landscape
// banner before it is replaced, when it is landscape and no banner is already
// retained. Returns true when a banner was stored.
func captureCoverBanner(coverPath string) bool {
	if coverart.HasBanner(coverPath) {
		return false
	}
	data, err := os.ReadFile(coverPath)
	if err != nil {
		return false
	}
	m, _ := readCoverMeta(coverPath)
	ok, err := coverart.StoreBanner(coverPath, data, m.Source, m.URL)
	return err == nil && ok
}

// migrateCoverBanner retains the one-step-undo cover (.prev) as the landscape
// banner when the current cover is portrait and no banner exists yet. Games
// upgraded to portrait art before banners were kept still have their old
// landscape banner sitting in .prev, so the wide view can use it without a
// re-fetch. Returns true when a banner was retained.
func migrateCoverBanner(coverPath string) bool {
	if coverart.HasBanner(coverPath) {
		return false
	}
	if w, h := coverart.CurrentSize(coverPath); w > 0 && h > 0 && isLandscape(w, h) {
		return false
	}
	data, err := os.ReadFile(coverPath + ".prev")
	if err != nil {
		return false
	}
	source, url := "f95", ""
	if m, ok := readCoverMeta(coverPath + ".prev"); ok {
		source, url = m.Source, m.URL
	}
	ok, err := coverart.StoreBanner(coverPath, data, source, url)
	if err != nil || !ok {
		return false
	}
	writeCoverWide(coverPath)
	return true
}

// coverBannerCurrent reports whether coverPath's sidecar already records a
// banner fetched from url. The F95 sync path uses it to avoid re-downloading
// an unchanged banner for a game whose primary cover is pinned.
func coverBannerCurrent(coverPath, url string) bool {
	if url == "" {
		return false
	}
	m, _ := readCoverMeta(coverPath)
	return m.BannerURL == url
}

// retainCoverBannerData stores data as coverPath's landscape banner and
// rebuilds the wide rendition. Returns true when data was landscape and got
// stored. A non-landscape image records the attempted URL (when no banner
// exists yet) so the sync path does not refetch it every run.
func retainCoverBannerData(coverPath string, data []byte, source, url string) bool {
	ok, err := coverart.StoreBanner(coverPath, data, source, url)
	if err != nil {
		return false
	}
	if !ok {
		if url != "" && !coverart.HasBanner(coverPath) {
			updateCoverMeta(coverPath, func(m *coverMeta) { m.BannerURL = url })
		}
		return false
	}
	writeCoverWide(coverPath)
	return true
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
