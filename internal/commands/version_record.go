package commands

import (
	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/version"
)

// recordInstalledVersion updates game.Version after a successful merge and
// persists it. It returns the version now recorded (unchanged when nothing
// could be determined).
//
// This is the step that takes a game out of the update queue. Without it,
// `download` merged the files but left version != latest_version forever, so
// the next check (and auto-update) reported the same update again.
//
// The payload's own filename version is the ground truth when present: a
// mirror can lag the thread, and recording latest_version while an older
// build is on disk would mark the game up to date and never retry the update.
// Only when the payload carries no version does the thread version stand in.
// A payload older than the recorded version never downgrades it.
func recordInstalledVersion(database *db.Database, game *db.Game, archivePath string) (string, error) {
	current := game.Version
	candidate := current

	payloadVer := ""
	if archivePath != "" {
		payloadVer = versionFromFilename(archivePath)
	}
	if payloadVer != "" {
		if version.IsNewer(payloadVer, current) {
			candidate = payloadVer
			// "1.1.0" and the thread's "1.1" are the same release; keep the
			// thread's spelling so the UI does not show a needless suffix.
			if sameVersionNumbers(payloadVer, game.LatestVersion) {
				candidate = game.LatestVersion
			}
		}
	} else if version.NeedsUpdate(game.LatestVersion, current) {
		candidate = game.LatestVersion
	}

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
