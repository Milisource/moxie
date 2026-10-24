package commands

import (
	"path/filepath"

	"github.com/mili/moxie/internal/db"
	"github.com/mili/moxie/internal/scanner"
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
// The thread's latest version is used when it is genuinely newer; an archive
// filename that parses to an even newer version wins because it describes the
// bytes actually installed. Older candidates are never written, so a thread
// that reverts cannot downgrade the library's recorded version.
func recordInstalledVersion(database *db.Database, game *db.Game, archivePath string) (string, error) {
	current := game.Version
	candidate := ""
	if version.NeedsUpdate(game.LatestVersion, current) {
		candidate = game.LatestVersion
	}
	if archivePath != "" {
		if archiveVer := scanner.ExtractVersion(filepath.Base(archivePath)); archiveVer != "" &&
			version.IsNewer(archiveVer, current) {
			if candidate == "" || version.IsNewer(archiveVer, candidate) {
				candidate = archiveVer
			}
		}
	}
	if candidate == "" || version.Compare(candidate, current) == version.Same {
		return current, nil
	}

	game.Version = candidate
	if err := database.UpdateGame(game); err != nil {
		return current, err
	}
	return candidate, nil
}
