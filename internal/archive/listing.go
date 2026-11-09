package archive

import (
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/mili/moxie/internal/log"
)

// maxListingBytes bounds how much of a tool's archive listing is buffered
// before a budget pre-scan is abandoned. A save archive's listing is kilobytes,
// so anything approaching this is already far past any sane budget.
const maxListingBytes = 16 << 20

// precheckExtractBudget inspects an archive with the listing command of the
// shell-out tool that will extract it and errors when the archive already
// exceeds opts' budget. Unlike the native extractors, 7z/unzip/unrar write
// entries verbatim and cannot be bounded mid-write, so the sizes are checked up
// front.
//
// The check is best-effort: when the listing cannot be produced or parsed (a
// tool quirk, a localized build, an encrypted header), a warning is logged and
// extraction proceeds rather than refusing a legitimate download. The residual
// risk is that a hostile archive slips past the size pre-check; such an archive
// still cannot escape its destination (verifyExtractedTree), but it could
// consume disk.
func precheckExtractBudget(archivePath, tool string, opts Options) error {
	if opts.MaxTotalBytes <= 0 && opts.MaxEntries <= 0 {
		return nil
	}

	var (
		listing   []byte
		truncated bool
		runErr    error
		parse     func(string) (int64, int, error)
	)
	switch tool {
	case "7z", "7zz":
		listing, truncated, runErr = scanListing(tool, "l", "-slt", archivePath)
		parse = parse7zListing
	case "unzip":
		listing, truncated, runErr = scanListing(tool, "-l", archivePath)
		parse = parseZipListing
	case "unrar", "unrar-nonfree", "rar":
		listing, truncated, runErr = scanListing(tool, "l", archivePath)
		parse = parseRarListing
	default:
		return nil
	}

	if truncated {
		return fmt.Errorf("%w: %s listing exceeds %d bytes", errExtractBudget, tool, maxListingBytes)
	}
	if runErr != nil {
		log.Warn("archive: listing failed; skipping size pre-check",
			"tool", tool, "archive", archivePath, "error", runErr)
		return nil
	}
	total, entries, err := parse(string(listing))
	if err != nil {
		log.Warn("archive: could not parse listing; skipping size pre-check",
			"tool", tool, "archive", archivePath, "error", err)
		return nil
	}
	return checkListingBudget(total, entries, opts)
}

// scanListing runs a listing command and returns its stdout, buffered up to
// maxListingBytes. truncated reports that the cap was hit, in which case the
// command is killed and the caller should treat the archive as over budget. A
// non-nil err means the command could not be run or exited non-zero.
func scanListing(name string, args ...string) (out []byte, truncated bool, err error) {
	cmd := exec.Command(name, args...)
	cmd.Stderr = io.Discard
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, err
	}
	if err := cmd.Start(); err != nil {
		return nil, false, err
	}

	out, readErr := io.ReadAll(io.LimitReader(stdout, maxListingBytes+1))
	if int64(len(out)) > maxListingBytes {
		// Stop the producer: it is blocked writing once we stop reading.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return out[:maxListingBytes], true, nil
	}
	if waitErr := cmd.Wait(); waitErr != nil {
		return out, false, waitErr
	}
	if readErr != nil {
		return out, false, readErr
	}
	return out, false, nil
}

// ---------------------------------------------------------------------------
// Listing parsers (pure: output text in, totals out)
// ---------------------------------------------------------------------------

// zipListingRow matches an "unzip -l" data row: a size, a date, a time, then the
// entry name. The date is ISO (YYYY-MM-DD) or the legacy two-digit form. The
// name is lazy so trailing padding is trimmed by the caller.
var zipListingRow = regexp.MustCompile(
	`^\s*(\d+)\s+(?:\d{4}-\d{2}-\d{2}|\d{2}-\d{2}-\d{2})\s+\d{2}:\d{2}\s+(.*?)\s*$`)

// parseZipListing extracts the total uncompressed size and non-directory entry
// count from `unzip -l` output. Directory rows (names ending in "/") are not
// counted.
func parseZipListing(out string) (int64, int, error) {
	var total int64
	var entries int
	sawFormat := false
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(strings.TrimSpace(line), "Length") {
			sawFormat = true
			continue
		}
		if isDashLine(strings.TrimSpace(line)) {
			sawFormat = true
			continue
		}
		m := zipListingRow.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		sawFormat = true
		name := strings.TrimSpace(m[2])
		if name == "" || strings.HasSuffix(name, "/") {
			continue
		}
		size, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			continue
		}
		total += size
		entries++
	}
	if !sawFormat {
		return 0, 0, fmt.Errorf("unrecognized unzip -l output")
	}
	return total, entries, nil
}

// parse7zListing extracts the total uncompressed size and non-directory entry
// count from `7z l -slt` output. The output is Key = Value blocks separated by
// blank lines; entry blocks carry both Path and Size, while the archive-level
// block carries "Physical Size" instead of "Size". Directory entries
// (Attributes starting with D, or Folder = +) are skipped.
func parse7zListing(out string) (int64, int, error) {
	var total int64
	var entries int
	sawSeparator := false

	block := map[string]string{}
	flush := func() {
		defer func() { block = map[string]string{} }()
		if len(block) == 0 {
			return
		}
		path, hasPath := block["Path"]
		sizeStr, hasSize := block["Size"]
		if !hasPath || !hasSize {
			return
		}
		if block["Folder"] == "+" || strings.HasPrefix(block["Attributes"], "D") {
			return
		}
		if strings.TrimSpace(path) == "" {
			return
		}
		size, err := strconv.ParseInt(strings.TrimSpace(sizeStr), 10, 64)
		if err != nil {
			return
		}
		total += size
		entries++
	}

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			flush()
			continue
		}
		if isDashLine(trimmed) {
			sawSeparator = true
			flush()
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		block[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	flush()

	if !sawSeparator {
		return 0, 0, fmt.Errorf("unrecognized 7z -slt output")
	}
	return total, entries, nil
}

// rarListingRow matches an "unrar l" data row: attribute flags, a size, a date,
// a time, then the entry name.
var rarListingRow = regexp.MustCompile(
	`^\s*(\S+)\s+(\d+)\s+\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}\s+(.+?)\s*$`)

// rarListingFooter matches the "<total size> <count>" totals line some unrar
// builds print below the entry table.
var rarListingFooter = regexp.MustCompile(`^\s*(\d+)\s+(\d+)\s*$`)

// parseRarListing extracts the total uncompressed size and non-directory entry
// count from `unrar l` output. Directory rows (names ending in a slash) are not
// counted. When the entry table header is missing (unrar-free variants), the
// footer totals line is used instead.
func parseRarListing(out string) (int64, int, error) {
	var total int64
	var entries int
	sawHeader := false
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Attributes") && strings.Contains(trimmed, "Name") {
			sawHeader = true
			continue
		}
		m := rarListingRow.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		// The attribute column carries a D flag for directory entries (unrar
		// does not always suffix the name with a slash).
		if strings.Contains(m[1], "D") {
			continue
		}
		name := strings.TrimSpace(m[3])
		if name == "" || strings.HasSuffix(name, "/") || strings.HasSuffix(name, `\`) {
			continue
		}
		size, err := strconv.ParseInt(m[2], 10, 64)
		if err != nil {
			continue
		}
		total += size
		entries++
	}
	if sawHeader {
		return total, entries, nil
	}
	if size, count, ok := parseRarFooter(out); ok {
		return size, count, nil
	}
	return 0, 0, fmt.Errorf("unrecognized unrar l output")
}

// parseRarFooter returns the last "<size> <count>" totals line, if any.
func parseRarFooter(out string) (int64, int, bool) {
	lines := strings.Split(out, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		m := rarListingFooter.FindStringSubmatch(strings.TrimRight(lines[i], "\r"))
		if m == nil {
			continue
		}
		size, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			continue
		}
		count, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		return size, count, true
	}
	return 0, 0, false
}

// isDashLine reports whether s is a separator line consisting only of dashes
// (at least four, so the two-dash " -- " archive separator 7z prints above the
// archive-level block does not count).
func isDashLine(s string) bool {
	if len(s) < 4 {
		return false
	}
	for _, r := range s {
		if r != '-' {
			return false
		}
	}
	return true
}
