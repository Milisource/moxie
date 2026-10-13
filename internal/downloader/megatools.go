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
// wrote. The total and percentage are omitted when the file size is unknown:
//
//	Downloaded 123456 bytes
//	Downloaded 123456 bytes of 987654 bytes (12.5%)
var megatoolsOldProgressRe = regexp.MustCompile(`Downloaded (\d+) bytes(?: of (\d+) bytes)?(?: \(([\d.]+)%\))?`)

// megatoolsNewProgressRe matches current megatools progress lines. GLib's
// g_format_size_full puts a space between the number and the IEC unit, e.g.
//
//	game.zip: 42.10% - 4.0 MiB (4194304 bytes) of 9.5 MiB (1.5 MiB/s)
//
// Group 1 is the percentage, group 2 the "done" portion (human size with an
// exact byte count in parens once the file exceeds 1 KiB), group 3 the total,
// and groups 4/5 the speed number and unit.
var megatoolsNewProgressRe = regexp.MustCompile(`([\d.]+)% - (.*?) of (.*?)(?: \(([\d.]+)\s*([KMGT]i?B)/s\))?\s*$`)

// megatoolsByteCountRe extracts the exact byte count megatools prints in
// parens on progress lines.
var megatoolsByteCountRe = regexp.MustCompile(`\((\d+) bytes\)`)

// humanSizeRe matches a megatools human-readable size. GLib formats sizes
// with a space before the IEC unit ("12.4 KiB", "2.9 MiB") and renders
// sub-1-KiB sizes as "512 bytes".
var humanSizeRe = regexp.MustCompile(`^([\d.]+)\s*([KMGT]?i?B|bytes)$`)

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

// megatoolsProgress is one parsed progress sample from megatools' stdout.
type megatoolsProgress struct {
	done       int64
	total      int64
	percent    float64
	speedBytes int64
}

// parseMegatoolsProgress extracts a progress sample from one megatools dl
// stdout line. Returns false when the line carries no progress information.
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
		if p.total == 0 {
			// At 0% the percentage carries no total information, so fall
			// back to the rounded human size on the first progress line.
			p.total = parseHumanSize(m[3])
		}
		if m[4] != "" {
			p.speedBytes = parseHumanSize(m[4] + m[5])
		}
		return p, true
	}
	return megatoolsProgress{}, false
}

// maxMegatoolsErrTail caps how much of megatools' stderr is retained for
// error messages; warnings scroll past on long downloads.
const maxMegatoolsErrTail = 4096

// streamMegatoolsProgress reads megatools' stdout line by line, forwarding
// parsed progress samples to onProgress and retaining the last sample in
// last. megatools writes its progress bar to stdout via g_print (not stderr),
// so the subprocess must capture stdout for this to fire.
func streamMegatoolsProgress(r io.Reader, expectedTotal int64, onProgress func(Progress), last *megatoolsProgress) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		p, ok := parseMegatoolsProgress(scanner.Text())
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

// drainMegatoolsStderr consumes megatools' stderr, retaining a bounded tail of
// recent output for error reporting. Progress lives on stdout; stderr carries
// ERROR:/WARNING: lines, and megatoolsErrSummary surfaces the last ERROR: line.
func drainMegatoolsStderr(r io.Reader, errTail *strings.Builder) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if errTail.Len() >= maxMegatoolsErrTail {
			// Keep only the most recent output so the final error line
			// survives a long stream of warnings.
			kept := errTail.String()
			kept = kept[len(kept)-maxMegatoolsErrTail/2:]
			errTail.Reset()
			errTail.WriteString(kept)
		}
		errTail.WriteString(scanner.Text())
		errTail.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read megatools stderr: %w", err)
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
// key — is passed through unchanged. megatools emits its progress bar on
// stdout and diagnostics on stderr; both are captured, progress is reported
// through onProgress, and ctx controls cancellation. A megatoolsTimeout cap
// mirrors the HTTP path's runaway-transfer guard.
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
	// megatools writes the progress bar to stdout (g_print) and errors to
	// stderr (g_printerr), so both pipes must be captured: stdout feeds the
	// progress callback, stderr feeds the error summary.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("megatools: create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("megatools: create stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("megatools: start: %w", err)
	}

	// Drain both pipes in goroutines: progress forwarding, error-tail capture,
	// and — because each pipe must be fully read before Wait — avoidance of a
	// blocked child on a full pipe buffer.
	var last megatoolsProgress
	var errTail strings.Builder
	progressErrCh := make(chan error, 1)
	stderrErrCh := make(chan error, 1)
	go func() {
		progressErrCh <- streamMegatoolsProgress(stdout, expectedTotal, onProgress, &last)
	}()
	go func() {
		stderrErrCh <- drainMegatoolsStderr(stderr, &errTail)
	}()

	// Drain both pipes to EOF BEFORE calling Wait: Wait closes the pipes the
	// moment it sees the command exit, so concurrent scanners would hit
	// "file already closed" (visible under -race). The drains are unblocking —
	// the pipes reach EOF when the child exits, and ctx kills the child on
	// timeout.
	progressErr := <-progressErrCh
	stderrErr := <-stderrErrCh
	waitErr := cmd.Wait()

	if dlCtx.Err() != nil {
		return fmt.Errorf("megatools download interrupted: %w", dlCtx.Err())
	}
	if waitErr != nil {
		if tail := megatoolsErrSummary(errTail.String()); tail != "" {
			return fmt.Errorf("megatools dl failed: %s", tail)
		}
		return fmt.Errorf("megatools dl failed: %w", waitErr)
	}
	if progressErr != nil {
		return fmt.Errorf("megatools dl: %w", progressErr)
	}
	if stderrErr != nil {
		return fmt.Errorf("megatools dl: %w", stderrErr)
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
