package downloader

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/log"
)

// --- Multi-part archive support ---
//
// F95Zone threads routinely split one archive across several download
// links (Game.part1.rar + Game.part2.rar, or Game.7z.001 + Game.7z.002).
// GroupMultiPartLinks detects such link sets, and DownloadMultiPart fetches
// every part (with per-part host fallback), assembling split-archive parts
// into the final file.

// partSuffixRe matches "Game.part1.rar" / "Game.part01.zip" /
// "Game part 1.7z" / "Game [part 2].zip" — the dominant volume style.
var partSuffixRe = regexp.MustCompile(`(?i)^(.+?)[.\s_\-\[\]]+part[\s.\-_\[\]]*(\d{1,3})[.\s_\-\[\]]*\.(rar|zip|7z)$`)

// splitSuffixRe matches "Game.7z.001" / "Game.001" — split-archive style.
var splitSuffixRe = regexp.MustCompile(`^(.+)\.(\d{3})$`)

// numberedSuffixRe matches "Game_1.rar" / "Game-2.zip" (small indices only;
// a prefix ending in "v" is a version number, not a part).
var numberedSuffixRe = regexp.MustCompile(`(?i)^(.+?)[_\-](\d{1,2})\.(rar|zip|7z)$`)

// parsePartName extracts the archive prefix and part index from a link
// name. ok is false when the name is not a part of a multi-part set.
func parsePartName(name string) (prefix string, index int, ok bool) {
	base := name
	if i := strings.LastIndexByte(base, '?'); i >= 0 {
		base = base[:i]
	}
	if m := partSuffixRe.FindStringSubmatch(base); m != nil {
		idx, err := strconv.Atoi(m[2])
		if err != nil {
			return "", 0, false
		}
		return strings.TrimRight(m[1], " ._-[("), idx, true
	}
	if m := splitSuffixRe.FindStringSubmatch(base); m != nil {
		idx, err := strconv.Atoi(m[2])
		if err != nil || idx == 0 {
			return "", 0, false
		}
		return m[1], idx, true
	}
	if m := numberedSuffixRe.FindStringSubmatch(base); m != nil {
		idx, err := strconv.Atoi(m[2])
		if err != nil || idx == 0 || idx > 30 {
			return "", 0, false
		}
		if strings.HasSuffix(strings.ToLower(m[1]), "v") {
			return "", 0, false
		}
		return m[1], idx, true
	}
	return "", 0, false
}

// PartGroup is one multi-part archive: the shared prefix and, per part
// index, the candidate links ordered by host score (best first).
type PartGroup struct {
	Prefix string
	Parts  map[int][]db.DownloadLink
}

// MaxIndex returns the highest part index in the group.
func (g PartGroup) MaxIndex() int {
	max := 0
	for idx := range g.Parts {
		if idx > max {
			max = idx
		}
	}
	return max
}

// IsSplitStyle reports whether the group uses split-archive naming
// (Game.7z.001 style), which requires concatenation after download.
func (g PartGroup) IsSplitStyle() bool {
	for _, candidates := range g.Parts {
		for _, link := range candidates {
			if splitSuffixRe.MatchString(strings.SplitN(link.Name, "?", 2)[0]) {
				return true
			}
		}
	}
	return false
}

// bestScore is the group's best single-link host score, used to order
// groups so the most reliable host set is attempted first.
func (g PartGroup) bestScore() int {
	best := -1000
	for _, candidates := range g.Parts {
		for _, link := range candidates {
			if s := ScoreLinkHost(link.Host); s > best {
				best = s
			}
		}
	}
	return best
}

// GroupMultiPartLinks splits a game's links into multi-part archive groups.
// Links whose names are not part-style (or whose part set has only one
// member) are ignored — they remain regular single-link downloads. Groups
// are ordered by their best host score.
func GroupMultiPartLinks(links []db.DownloadLink) []PartGroup {
	byPrefix := map[string]map[int][]db.DownloadLink{}
	for _, link := range links {
		prefix, idx, ok := parsePartName(link.Name)
		if !ok {
			continue
		}
		if byPrefix[prefix] == nil {
			byPrefix[prefix] = map[int][]db.DownloadLink{}
		}
		byPrefix[prefix][idx] = append(byPrefix[prefix][idx], link)
	}
	var groups []PartGroup
	for prefix, parts := range byPrefix {
		if len(parts) < 2 {
			continue
		}
		g := PartGroup{Prefix: prefix, Parts: parts}
		for idx := range g.Parts {
			sort.SliceStable(g.Parts[idx], func(i, j int) bool {
				return ScoreLinkHost(g.Parts[idx][i].Host) > ScoreLinkHost(g.Parts[idx][j].Host)
			})
		}
		groups = append(groups, g)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		return groups[i].bestScore() > groups[j].bestScore()
	})
	return groups
}

// partDestName derives the on-disk file name a download of link will
// produce — mirroring downloadWithHeaders' base-name derivation.
func partDestName(link db.DownloadLink) string {
	base := filepath.Base(strings.SplitN(link.URL, "?", 2)[0])
	if base == "" || base == "." || base == "/" {
		base = "download"
	}
	if decoded, err := url.PathUnescape(base); err == nil && decoded != "" {
		base = decoded
	}
	return base
}

// DownloadMultiPart downloads every part of a group into destDir, trying
// each part's candidate links in order and skipping parts already present
// with the expected size. Split-style groups are concatenated into the
// final archive (parts removed afterwards); volume-style groups return the
// first part (the extractor handles the set). Returns the assembled file.
func DownloadMultiPart(group PartGroup, destDir string, onProgress func(Progress), f95Cookie string) (string, error) {
	maxIdx := group.MaxIndex()
	files := make([]string, maxIdx+1)
	for idx := 1; idx <= maxIdx; idx++ {
		candidates := group.Parts[idx]
		if len(candidates) == 0 {
			return "", fmt.Errorf("multi-part download missing part %d of %d", idx, maxIdx)
		}
		var got string
		var lastErr error
		for _, link := range candidates {
			name := partDestName(link)
			present := filepath.Join(destDir, name)
			if fi, err := os.Stat(present); err == nil && link.Size > 0 && fi.Size() == link.Size {
				got = present
				log.Info("multi-part: part already present, skipping", "part", idx, "file", name)
				break
			}
			err := DownloadWithHost(link.URL, link.Host, destDir, link.Size, onProgress, f95Cookie)
			if err != nil {
				lastErr = err
				log.Warn("multi-part: part download failed", "part", idx, "host", link.Host, "error", err)
				continue
			}
			got = present
			break
		}
		if got == "" {
			if lastErr == nil {
				lastErr = fmt.Errorf("no candidate links for part %d", idx)
			}
			return "", fmt.Errorf("multi-part download failed at part %d/%d: %w", idx, maxIdx, lastErr)
		}
		files[idx] = got
	}

	if group.IsSplitStyle() {
		final := strings.TrimSuffix(files[1], filepath.Ext(files[1]))
		if err := ConcatSplitParts(files[1:], final); err != nil {
			return "", err
		}
		for _, p := range files[1:] {
			os.Remove(p)
		}
		return final, nil
	}
	return files[1], nil
}

// ConcatSplitParts appends parts in order into destPath. Any pre-existing
// destPath is replaced; on error the partial target is removed. The source
// parts are left untouched.
func ConcatSplitParts(parts []string, destPath string) error {
	if len(parts) < 2 {
		return fmt.Errorf("concat needs at least 2 parts, got %d", len(parts))
	}
	if err := os.Remove(destPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("concat remove stale target %s: %w", destPath, err)
	}
	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("concat create %s: %w", destPath, err)
	}
	for _, p := range parts {
		in, err := os.Open(p)
		if err != nil {
			out.Close()
			os.Remove(destPath)
			return fmt.Errorf("concat open %s: %w", p, err)
		}
		_, err = io.Copy(out, in)
		in.Close()
		if err != nil {
			out.Close()
			os.Remove(destPath)
			return fmt.Errorf("concat copy %s: %w", p, err)
		}
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(destPath)
		return fmt.Errorf("concat sync: %w", err)
	}
	if err := out.Close(); err != nil {
		os.Remove(destPath)
		return fmt.Errorf("concat close: %w", err)
	}
	log.Info("concat split parts", "target", destPath, "parts", len(parts))
	return nil
}
