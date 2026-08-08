// Package version compares game version strings scraped from F95Zone
// against locally-known versions.
//
// F95Zone versions are not semver. Real-world values include "v0.12.0",
// "0.8.1b", "Ch.4 Free", "Ep.6", "2018-07-18" (the date form mandated for
// games without a version number), and "Final". Comparison therefore has
// to answer three questions, not one: are they the same, is the remote one
// newer, or are the two simply not comparable?
package version

import (
	"regexp"
	"strconv"
	"strings"
)

// Diff is the result of comparing a remote version against a known one.
type Diff int

const (
	// Same means the two versions are equivalent after normalization.
	Same Diff = iota
	// Newer means the remote version is unambiguously ahead — a real update.
	Newer
	// Older means the remote version is behind the known one. This usually
	// signals a parse regression or an edited thread, not a downgrade.
	Older
	// Changed means the versions differ but no ordering can be established
	// (e.g. a date-form version replacing a numeric one).
	Changed
)

func (d Diff) String() string {
	switch d {
	case Same:
		return "same"
	case Newer:
		return "newer"
	case Older:
		return "older"
	default:
		return "changed"
	}
}

var (
	// digitSepRe matches a separator between two digits, so directory-derived
	// versions like "0_8_1" and "0-8-1" collapse to the canonical "0.8.1".
	digitSepRe = regexp.MustCompile(`(\d)[ _-]+(\d)`)

	// dateRe matches the ISO date form F95Zone mandates for games with no
	// version number, e.g. "2018-07-18".
	dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

	// numRe extracts the digit runs used for element-wise comparison.
	numRe = regexp.MustCompile(`\d+`)

	// buildLetterRe matches a build letter attached to a number, as in
	// "1.5b". The trailing letter orders builds within a release.
	buildLetterRe = regexp.MustCompile(`\d([a-z])(?:[^a-z0-9]|$)`)
)

// Normalize reduces a version string to a canonical comparable form: it
// trims whitespace, drops a leading "v"/"V", lowercases, collapses digit
// separators to dots, and strips trailing ".0" segments so "1.0.0" and "1"
// compare equal. Date-form versions are returned with their dashes intact.
func Normalize(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	v = strings.ToLower(strings.TrimSpace(v))

	// Dates are canonical as-is; collapsing their dashes would make them
	// indistinguishable from a three-segment version number.
	if dateRe.MatchString(v) {
		return v
	}

	// Repeat until stable: each pass consumes the digit that would start
	// the next match, so "0_8_1" needs two passes.
	for i := 0; i < 8; i++ {
		out := digitSepRe.ReplaceAllString(v, "$1.$2")
		if out == v {
			break
		}
		v = out
	}

	for strings.HasSuffix(v, ".0") {
		v = strings.TrimSuffix(v, ".0")
	}
	return v
}

type kind int

const (
	kindEmpty kind = iota
	kindDate
	kindFinal
	kindNumeric
	kindOther
)

func classify(v string) kind {
	switch {
	case v == "":
		return kindEmpty
	case dateRe.MatchString(v):
		return kindDate
	case v == "final":
		return kindFinal
	case strings.ContainsAny(v, "0123456789"):
		return kindNumeric
	default:
		return kindOther
	}
}

// Compare reports how the remote version relates to the known one.
//
// Only a Newer result should be treated as an update available without
// qualification. Callers that surface Changed to users should mark it as
// uncertain, and Older is a signal that something upstream went wrong.
func Compare(remote, known string) Diff {
	r, k := Normalize(remote), Normalize(known)
	if r == k {
		return Same
	}

	rk, kk := classify(r), classify(k)
	if rk == kindEmpty || kk == kindEmpty {
		// Nothing to compare against — a missing side is not an update.
		return Changed
	}

	// A game reaching "Final" is a genuine release event; the reverse
	// (Final regressing to a number) is not something we can order.
	if rk == kindFinal {
		return Newer
	}
	if kk == kindFinal {
		return Changed
	}

	if rk != kk {
		return Changed
	}

	switch rk {
	case kindDate:
		// ISO dates order lexicographically.
		if r > k {
			return Newer
		}
		return Older
	case kindNumeric:
		return compareNumeric(r, k)
	default:
		return Changed
	}
}

// IsNewer reports whether remote is unambiguously ahead of known.
func IsNewer(remote, known string) bool {
	return Compare(remote, known) == Newer
}

// compareNumeric compares the digit runs of two versions element-wise,
// treating a missing segment as zero so "0.8" < "0.8.1". When every
// segment matches it falls back to the build letter ("1.5a" < "1.5b").
func compareNumeric(r, k string) Diff {
	rn := numRe.FindAllString(r, -1)
	kn := numRe.FindAllString(k, -1)

	n := len(rn)
	if len(kn) > n {
		n = len(kn)
	}
	for i := 0; i < n; i++ {
		a := segment(rn, i)
		b := segment(kn, i)
		if a != b {
			if a > b {
				return Newer
			}
			return Older
		}
	}

	// Numbers are identical — the strings differ only in their text, e.g.
	// "0.8 extra" vs "0.8", or "1.5a" vs "1.5b".
	rl, rok := buildLetter(r)
	kl, kok := buildLetter(k)
	if rok && kok && rl != kl {
		if rl > kl {
			return Newer
		}
		return Older
	}
	return Changed
}

func segment(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	// Digit runs are bounded in practice; an overflowing run compares as 0
	// rather than crashing the update check.
	n, err := strconv.Atoi(parts[i])
	if err != nil {
		return 0
	}
	return n
}

func buildLetter(v string) (string, bool) {
	m := buildLetterRe.FindAllStringSubmatch(v, -1)
	if len(m) == 0 {
		return "", false
	}
	return m[len(m)-1][1], true
}
