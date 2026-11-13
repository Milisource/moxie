package commands

import (
	"strings"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/log"
	"github.com/mili/moxie/internal/version"
)

// RecordedVersion chooses the installed version from the payload and thread,
// retaining the current version when the payload would downgrade it. patch
// means the payload was an "Update only" overlay, which may be named after the
// version it upgrades from.
func RecordedVersion(current, latest, archivePath string, patch bool) string {
	payload := ""
	if archivePath != "" {
		payload = versionFromFilename(archivePath)
	}
	if strings.TrimSpace(current) == "" {
		// Nothing is recorded to compare against (first install): the applied
		// payload is the evidence of what is on disk, falling back to the
		// thread release when the filename carries no version.
		if payload != "" {
			if latest != "" && version.SameCore(payload, latest) {
				return latest
			}
			return payload
		}
		return latest
	}
	candidate := current
	if payload != "" {
		switch version.Compare(payload, current) {
		case version.Newer:
			candidate = payload
			if latest != "" && version.SameCore(payload, latest) {
				candidate = latest
			}
		case version.Same:
			// A full build named after the installed release is not evidence
			// that the thread's newer build is on disk. Only a qualifier-only
			// difference ("1.0" vs "1.0 Final") or an overlay patch may adopt
			// the thread version.
			if latest != "" && version.NeedsUpdate(latest, current) &&
				(patch || version.SameCore(payload, latest)) {
				candidate = latest
			}
		case version.Changed:
			// An unorderable payload is not evidence of an installed upgrade.
			return current
		case version.Older:
			log.Warn("mirror served an older build than installed", "payload", payload, "current", current, "archivePath", archivePath)
		}
	} else if version.NeedsUpdate(latest, current) {
		candidate = latest
	}
	if candidate == "" {
		return current
	}
	return candidate
}

// RecordInstalledVersion persists the shared recording decision after a
// successful install, marking changed versions as F95Zone-authoritative.
func RecordInstalledVersion(database *db.Database, game *db.Game, archivePath string, patch bool) (string, error) {
	current := game.Version
	candidate := RecordedVersion(current, game.LatestVersion, archivePath, patch)

	if candidate == "" || version.Compare(candidate, current) == version.Same {
		return current, nil
	}

	game.Version = candidate
	game.VersionSource = "f95"
	if err := database.UpdateGame(game); err != nil {
		return current, err
	}
	return candidate, nil
}
