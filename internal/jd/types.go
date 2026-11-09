package jd

import "github.com/rkosegi/jdownloader-go/jdownloader"

// DeviceInfo is a client-side projection of the device descriptor returned
// by MyJDownloader's /my/listdevices endpoint. The underlying library type
// is not re-exported so the package surface stays stable even if the
// dependency's JSON tags change.
type DeviceInfo struct {
	ID     string
	Type   string
	Name   string
	Status string
}

// Download is a projection of jdownloader.DownloadLink with all pointer
// fields flattened to zero values when the API omits them. It describes a
// single link inside the Downloads list (downloadsV2/queryLinks).
type Download struct {
	UUID             int64
	PackageUUID      int64
	Name             string
	URL              string
	Host             string
	Status           string
	Comment          string
	BytesTotal       int64
	BytesLoaded      int64
	Speed            float64 // bytes per second
	ETA              int64   // remaining seconds, best effort
	Finished         bool
	FinishedDate     int64 // unix seconds
	Skipped          bool
	Enabled          bool
	ExtractionStatus string
	AddedDate        int64 // unix seconds
}

// Running reports whether the link is currently transferring data. The JD
// API does not expose an explicit running flag on links, so it is inferred
// from Speed > 0 (a link parked behind a captcha sits at 0 B/s).
func (d Download) Running() bool { return d.Speed > 0 }

// Progress returns the fraction [0,1] of bytes downloaded, or 0 when the
// total size is unknown (JD reports 0 for hosts it cannot size in advance).
func (d Download) Progress() float64 {
	if d.BytesTotal <= 0 {
		return 0
	}
	if d.BytesLoaded >= d.BytesTotal {
		return 1
	}
	return float64(d.BytesLoaded) / float64(d.BytesTotal)
}

// Progress is a snapshot of an entire download batch: aggregates across all
// links plus the raw per-link state. Passed to the callback registered via
// WithProgress so the eventual CLI/TUI integration can render one progress
// line per batch.
type Progress struct {
	Downloads   []Download
	BytesLoaded int64
	BytesTotal  int64
	Speed       float64
	Finished    bool
}

// Running reports whether any link in the batch is currently transferring
// data (see Download.Running for the inference caveat).
func (p Progress) Running() bool { return p.Speed > 0 }

// Fraction returns the aggregated fraction [0,1] of the batch downloaded.
func (p Progress) Fraction() float64 {
	if p.BytesTotal <= 0 {
		return 0
	}
	if p.BytesLoaded >= p.BytesTotal {
		return 1
	}
	return float64(p.BytesLoaded) / float64(p.BytesTotal)
}

// batchDone reports whether every link in the batch has reached a terminal
// state: finished, skipped, or disabled. An empty batch is never "done" —
// immediately after LinkGrabber.Add(autostart=true) the links may still be
// inside the linkgrabber for seconds, so the caller must keep polling until
// they appear in the Downloads list (or the stall timeout fires).
func batchDone(downloads []Download) bool {
	if len(downloads) == 0 {
		return false
	}
	for _, d := range downloads {
		if !d.Finished && !d.Skipped && d.Enabled {
			return false
		}
	}
	return true
}

// batchProgress aggregates a Downloads snapshot into a single Progress.
func batchProgress(downloads []Download) Progress {
	p := Progress{Downloads: downloads}
	for _, d := range downloads {
		if d.BytesLoaded > 0 {
			p.BytesLoaded += d.BytesLoaded
		}
		if d.BytesTotal > 0 {
			p.BytesTotal += d.BytesTotal
		}
		if d.Speed > 0 {
			p.Speed += d.Speed
		}
	}
	p.Finished = batchDone(downloads)
	return p
}

// toDownloads converts the library's pointer-heavy link list into the
// package's flat projection.
func toDownloads(list *[]jdownloader.DownloadLink) []Download {
	if list == nil {
		return nil
	}
	out := make([]Download, 0, len(*list))
	for _, dl := range *list {
		out = append(out, Download{
			UUID:             i64(dl.Uuid),
			PackageUUID:      i64(dl.PackageUuid),
			Name:             str(dl.Name),
			URL:              str(dl.Url),
			Host:             str(dl.Host),
			Status:           str(dl.Status),
			Comment:          str(dl.Comment),
			BytesTotal:       i64(dl.BytesTotal),
			BytesLoaded:      i64(dl.BytesLoaded),
			Speed:            f64(dl.Speed),
			ETA:              i64(dl.Eta),
			Finished:         bl(dl.Finished),
			FinishedDate:     i64(dl.FinishedDate),
			Skipped:          bl(dl.Skipped),
			Enabled:          bl(dl.Enabled),
			ExtractionStatus: str(dl.ExtractionStatus),
			AddedDate:        i64(dl.AddedDate),
		})
	}
	return out
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func i64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func f64(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func bl(p *bool) bool {
	if p == nil {
		return false
	}
	return *p
}
