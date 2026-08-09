package downloader

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/mili/moxie/internal/log"
)

// ErrMegaNeedsMegatools is returned by resolveMega when the megatools binary
// is installed. DownloadWithContext recognizes it and delegates the download
// to the megatools subprocess instead of attempting an HTTP GET — Mega uses an
// encrypted protocol that plain HTTP cannot handle.
var ErrMegaNeedsMegatools = errors.New("mega link requires megatools subprocess")

// findMegatools locates the megatools binary. Package-level var so tests can
// substitute a fake binary without a real megatools installation.
var findMegatools = func() (string, error) {
	return exec.LookPath("megatools")
}

// megatoolsTimeout caps a single megatools subprocess run. Mirrors the HTTP
// path's downloadTimeout runaway-transfer guard. A var so tests can lower it.
var megatoolsTimeout = downloadTimeout

// megaFileIDRe matches the file ID in both Mega share-link styles. New-style
// links are https://mega.nz/file/<ID>#<key>; legacy links are
// https://mega.nz/#!<ID>!<key>.
var megaFileIDRe = regexp.MustCompile(`(?:/file/|#!)([A-Za-z0-9_-]+)`)

// extractMegaFileID returns the file ID from a Mega.nz share URL, or "" when
// the URL does not look like a single-file Mega link. The hostname must be a
// known Mega domain (the resolver only routes host=="mega" here, but the
// check keeps the extractor honest when a URL is mislabeled). Folder links
// (https://mega.nz/folder/<ID>#<key>) are rejected: downloads are sized and
// accounted per file, and megatools handles them differently.
func extractMegaFileID(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	switch strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.") {
	case "mega.nz", "mega.co.nz":
		// known Mega file-link domains
	default:
		return ""
	}
	m := megaFileIDRe.FindStringSubmatch(rawURL)
	if m == nil {
		return ""
	}
	return m[1]
}

// megatoolsOldProgressRe matches the legacy progress lines megatools (< 1.10)
// wrote to stderr. The total and percentage are omitted when the file size is
// unknown:
//
//	Downloaded 123456 bytes
//	Downloaded 123456 bytes of 987654 bytes (12.5%)
var megatoolsOldProgressRe = regexp.MustCompile(`Downloaded (\d+) bytes(?: of (\d+) bytes)?(?: \(([\d.]+)%\))?`)

// megatoolsNewProgressRe matches current megatools progress lines, e.g.
//
//	game.zip: 42.1% - 1.2MiB (1260000 bytes) of 2.9MiB (1.5MiB/s)
//
// Group 2 holds the "done" portion (human size, optionally with an exact
// byte count in parens); group 3/4 are the speed number and unit.
var megatoolsNewProgressRe = regexp.MustCompile(`([\d.]+)% - (.+?) of [^ (]+(?: \(([\d.]+)\s*([KMGT]?i?B)/s\))?`)

// megatoolsByteCountRe extracts the exact byte count megatools prints in
// parens on progress lines.
var megatoolsByteCountRe = regexp.MustCompile(`\((\d+) bytes\)`)

// humanSizeRe matches a megatools human-readable size ("12.4KiB", "2.9MiB").
var humanSizeRe = regexp.MustCompile(`^([\d.]+)\s*([KMGT]?i?B)$`)

// parseHumanSize converts a megatools human-readable size to bytes. Sizes are
// binary (KiB = 1024); unparseable input returns 0.
func parseHumanSize(s string) int64 {
	m := humanSizeRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	var mult float64 = 1
	switch m[2] {
	case "KiB", "K", "KB":
		mult = 1 << 10
	case "MiB", "M", "MB":
		mult = 1 << 20
	case "GiB", "G", "GB":
		mult = 1 << 30
	case "TiB", "T", "TB":
		mult = 1 << 40
	}
	return int64(v * mult)
}

// megatoolsProgress is one parsed progress sample from megatools' stderr.
type megatoolsProgress struct {
	done       int64
	total      int64
	percent    float64
	speedBytes int64
}

// parseMegatoolsProgress extracts a progress sample from one megatools dl
// stderr line. Returns false when the line carries no progress information.
func parseMegatoolsProgress(line string) (megatoolsProgress, bool) {
	if m := megatoolsOldProgressRe.FindStringSubmatch(line); m != nil {
		p := megatoolsProgress{}
		p.done, _ = strconv.ParseInt(m[1], 10, 64)
		if m[2] != "" {
			p.total, _ = strconv.ParseInt(m[2], 10, 64)
		}
		if m[3] != "" {
			p.percent, _ = strconv.ParseFloat(m[3], 64)
		}
		return p, true
	}
	if m := megatoolsNewProgressRe.FindStringSubmatch(line); m != nil {
		p := megatoolsProgress{}
		p.percent, _ = strconv.ParseFloat(m[1], 64)
		if n := megatoolsByteCountRe.FindStringSubmatch(m[2]); n != nil {
			p.done, _ = strconv.ParseInt(n[1], 10, 64)
		} else {
			p.done = parseHumanSize(m[2])
		}
		// Reconstruct the exact total from the reported percentage; the
		// "of <size>" human value is rounded for display.
		if p.percent > 0 {
			p.total = int64(float64(p.done) / p.percent * 100)
		}
		if m[3] != "" {
			p.speedBytes = parseHumanSize(m[3] + m[4])
		}
		return p, true
	}
	return megatoolsProgress{}, false
}

// maxMegatoolsErrTail caps how much of megatools' stderr is retained for
// error messages; progress lines scroll past quickly on long downloads.
const maxMegatoolsErrTail = 4096

// streamMegatoolsOutput reads megatools' stderr line by line, forwarding
// progress samples to onProgress and retaining a bounded tail of recent
// output for error reporting. The last progress sample is stored in last.
func streamMegatoolsOutput(r io.Reader, expectedTotal int64, onProgress func(Progress), last *megatoolsProgress, errTail *strings.Builder) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if errTail.Len() >= maxMegatoolsErrTail {
			// Progress lines scroll by; keep only the most recent output so
			// the final error line survives long downloads.
			kept := errTail.String()
			kept = kept[len(kept)-maxMegatoolsErrTail/2:]
			errTail.Reset()
			errTail.WriteString(kept)
		}
		errTail.WriteString(line)
		errTail.WriteByte('\n')

		p, ok := parseMegatoolsProgress(line)
		if !ok {
			continue
		}
		*last = p
		if onProgress == nil {
			continue
		}
		total := p.total
		if total <= 0 && expectedTotal > 0 {
			total = expectedTotal
		}
		percent := p.percent
		if p.total > 0 {
			percent = float64(p.done) / float64(p.total) * 100
		}
		onProgress(Progress{
			BytesDownloaded:  p.done,
			TotalBytes:       total,
			SpeedBytesPerSec: float64(p.speedBytes),
			Percent:          percent,
		})
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read megatools output: %w", err)
	}
	return nil
}

// megatoolsErrSummary picks the most useful line from megatools' stderr for an
// error message: the last "ERROR:" line when present, otherwise the last
// non-empty line.
func megatoolsErrSummary(tail string) string {
	lines := strings.Split(strings.TrimSpace(tail), "\n")
	lastErr := ""
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "ERROR:") {
			lastErr = ln
		}
	}
	if lastErr != "" {
		return lastErr
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if ln := strings.TrimSpace(lines[i]); ln != "" {
			return ln
		}
	}
	return ""
}

// runMegatoolsDownload downloads a Mega link by running the megatools binary
// as a subprocess:
//
//	megatools dl --path <destDir> <url>
//
// The full URL — including the #key fragment that carries the decryption
// key — is passed through unchanged. Progress lines are streamed from stderr
// and reported through onProgress; ctx controls cancellation and a
// megatoolsTimeout cap mirrors the HTTP path's runaway-transfer guard.
func runMegatoolsDownload(ctx context.Context, url, destDir string, expectedTotal int64, onProgress func(Progress)) error {
	if extractMegaFileID(url) == "" {
		return fmt.Errorf("could not extract Mega file ID from URL: %s", redactedURL(url))
	}
	bin, err := findMegatools()
	if err != nil {
		return fmt.Errorf(
			"Mega uses encrypted protocol - use megatools CLI:\n"+
				"  megatools dl --path <dest> '%s'\n"+
				"  Install: brew install megatools / apt install megatools",
			url,
		)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("megatools: create destination dir: %w", err)
	}

	// megatools owns the transfer; give it the same ceiling as the HTTP path.
	// A caller ctx with an earlier deadline wins.
	dlCtx, cancel := context.WithTimeout(ctx, megatoolsTimeout)
	defer cancel()

	cmd := exec.CommandContext(dlCtx, bin, "dl", "--path", destDir, url)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("megatools: create stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("megatools: start: %w", err)
	}

	// Drain stderr in a goroutine: progress forwarding, error-tail capture,
	// and — because the pipe must be fully read before Wait — avoidance of
	// a blocked child on a full pipe buffer.
	var last megatoolsProgress
	var errTail strings.Builder
	streamErrCh := make(chan error, 1)
	go func() {
		streamErrCh <- streamMegatoolsOutput(stderr, expectedTotal, onProgress, &last, &errTail)
	}()

	waitErr := cmd.Wait()
	streamErr := <-streamErrCh

	if dlCtx.Err() != nil {
		return fmt.Errorf("megatools download interrupted: %w", dlCtx.Err())
	}
	if waitErr != nil {
		if tail := megatoolsErrSummary(errTail.String()); tail != "" {
			return fmt.Errorf("megatools dl failed: %s", tail)
		}
		return fmt.Errorf("megatools dl failed: %w", waitErr)
	}
	if streamErr != nil {
		return fmt.Errorf("megatools dl: %w", streamErr)
	}

	if onProgress != nil {
		total := last.total
		if total <= 0 && expectedTotal > 0 {
			total = expectedTotal
		}
		done := last.done
		if total > 0 {
			done = total
		}
		onProgress(Progress{BytesDownloaded: done, TotalBytes: total, Percent: 100})
	}
	log.Info("mega download complete", "url", redactedURL(url), "dest", destDir, "bytes", last.done)
	return nil
}
