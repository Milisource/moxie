package downloader

import (
	"regexp"
	"strings"
)

// LinkKind classifies what a thread download link delivers, from its
// section label (link names are "section · platform").
type LinkKind string

const (
	// LinkFull is a complete game build — safe to replace an install with.
	LinkFull LinkKind = "full"
	// LinkPatch is an "Update only" archive holding just the files that
	// changed between two versions; it must be overlaid, never used to
	// replace an install, and only applies to its FromVersion.
	LinkPatch LinkKind = "patch"
	// LinkExtra is anything that isn't the game itself (mods, translations,
	// walkthroughs, DLC, multi-part pieces, old builds, soundtracks).
	LinkExtra LinkKind = "extra"
)

// LinkClass is the result of ClassifyLink.
type LinkClass struct {
	Kind        LinkKind
	FromVersion string // patch only; "" when the label carries no range
	ToVersion   string
}

var (
	// "Update Only (v0.17 -> v0.18)", "Update-only", "Patch v0.17 to v0.18",
	// "Update (0.5 → 0.6)".
	updateOnlyRE = regexp.MustCompile(`(?i)\bupdate[\s-]*only\b|\bonly[\s-]*update\b|\bupdate\s*patch\b`)
	versionRange = regexp.MustCompile(`(?i)v?(\d[\w.]*?)\s*(?:->|→|=>|\bto\b|~)\s*v?(\d[\w.]*)`)
	patchRangeRE = regexp.MustCompile(`(?i)\b(?:patch|update)\b[^·]*?v?\d[\w.]*\s*(?:->|→|=>|\bto\b)\s*v?\d`)

	extraRE = regexp.MustCompile(`(?i)\b(` + strings.Join([]string{
		`mods?`, `modded`, `walkthrough`, `cheats?`, `cheat\s*menu`, `translations?`,
		`translated`, `decensor(ed)?`, `uncensor(ed)?`, `censor\s*patch`, `dlc`,
		`extras?`, `bonus`, `soundtrack`, `ost`, `artbook`, `art\s*book`, `gallery`,
		`torrent`, `part\s*\d+`, `old\s+.*version`, `previous\s+version`,
		`legacy`, `saves?`, `save\s*file`, `guide`, `patch(ed)?`, `pre-?patched`,
		`fix`, `hotfix`, `russian`, `french`, `spanish`, `german`, `chinese`,
		`portuguese`, `japanese`, `italian`,
	}, `|`) + `)\b`)
)

// ClassifyLink reports whether a link is a full build, an "Update only"
// patch (with its version range when stated) or an extra. Unlabelled links
// ("DOWNLOAD · Win", a bare host name) are full builds.
func ClassifyLink(name string) LinkClass {
	section := name
	if i := strings.LastIndex(section, "·"); i >= 0 {
		section = section[:i]
	}
	if updateOnlyRE.MatchString(section) || patchRangeRE.MatchString(section) {
		c := LinkClass{Kind: LinkPatch}
		if m := versionRange.FindStringSubmatch(section); m != nil {
			c.FromVersion, c.ToVersion = m[1], m[2]
		}
		return c
	}
	if extraRE.MatchString(section) {
		return LinkClass{Kind: LinkExtra}
	}
	return LinkClass{Kind: LinkFull}
}
