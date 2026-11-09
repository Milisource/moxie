package saves

import "github.com/mili/moxie/internal/version"

// VersionsDiffer reports whether the version a set was captured for differs from
// the game's current version, comparing light-normalized forms (see
// version.Normalize). An empty version on either side means "unknown" and never
// counts as a mismatch.
//
// It is the single comparator behind the CLI's apply/restore warning and the
// desktop app's, so the two surfaces cannot drift apart: a set moxie warns about
// in the terminal is the same set the GUI warns about.
func VersionsDiffer(setVersion, gameVersion string) bool {
	if setVersion == "" || gameVersion == "" {
		return false
	}
	return version.Normalize(setVersion) != version.Normalize(gameVersion)
}
