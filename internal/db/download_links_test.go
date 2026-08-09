package db

import (
	"path/filepath"
	"testing"
)

// columnTypeDefault reads the dflt_value of a column from PRAGMA table_info.
func columnHasSize(t *testing.T, d *Database) bool {
	t.Helper()
	rows, err := d.conn.Query("PRAGMA table_info(download_links)")
	if err != nil {
		t.Fatalf("table_info(download_links): %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dfltValue *string
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan table_info row: %v", err)
		}
		if name == "size" {
			return true
		}
	}
	return false
}

// Open() on a pre-v9 database must add download_links.size (defaulting to 0
// for existing rows) and keep the stored links readable afterwards. The v7
// fixture from migrate_test.go exercises the full v8 → v9 upgrade path.
func TestMigrateV9AddsDownloadLinkSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v7.sqlite")
	makeV7DB(t, path)

	d, err := Open(path)
	if err != nil {
		t.Fatalf("Open on v7 db: %v", err)
	}
	defer d.Close()

	var userVersion int
	if err := d.conn.QueryRow("PRAGMA user_version").Scan(&userVersion); err != nil {
		t.Fatal(err)
	}
	if userVersion != currentSchemaVersion {
		t.Fatalf("user_version = %d, want %d", userVersion, currentSchemaVersion)
	}
	if !columnHasSize(t, d) {
		t.Fatal("download_links.size column missing after v9 migration")
	}

	// Existing rows get the default: 0 (size unknown).
	links, err := d.ListDownloadLinks(1, "", true)
	if err != nil || len(links) != 1 {
		t.Fatalf("ListDownloadLinks after migration = %+v, want 1 link", links)
	}
	if links[0].Host != "pixeldrain" || links[0].Size != 0 {
		t.Errorf("migrated link = {%q size=%d}, want pixeldrain with size 0", links[0].Host, links[0].Size)
	}

	// New rows carry the scraped size through every read path.
	link := &DownloadLink{
		GameID:   1,
		URL:      "https://catbox.moe/x",
		Host:     "catbox",
		Name:     "Game_v1.0.rar",
		Size:     239494758,
		Platform: PlatformAll,
	}
	id, err := d.CreateDownloadLink(link)
	if err != nil {
		t.Fatalf("CreateDownloadLink with size: %v", err)
	}

	got, err := d.GetDownloadLink(id)
	if err != nil || got == nil {
		t.Fatalf("GetDownloadLink(%d): %v", id, err)
	}
	if got.Size != 239494758 {
		t.Errorf("GetDownloadLink.Size = %d, want 239494758", got.Size)
	}

	byURL, err := d.GetDownloadLinkByURL(1, "https://catbox.moe/x")
	if err != nil || byURL == nil || byURL.Size != 239494758 {
		t.Errorf("GetDownloadLinkByURL = %+v (err %v), want size 239494758", byURL, err)
	}

	list, err := d.ListDownloadLinks(1, "", true)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListDownloadLinks = %+v, want 2 links", list)
	}
	var sized bool
	for _, l := range list {
		if l.Size == 239494758 {
			sized = true
		}
	}
	if !sized {
		t.Errorf("ListDownloadLinks lost the size: %+v", list)
	}

	all, err := d.AllDownloadLinks(true)
	if err != nil {
		t.Fatalf("AllDownloadLinks: %v", err)
	}
	var allSized bool
	for _, l := range all {
		if l.Size == 239494758 {
			allSized = true
		}
	}
	if !allSized {
		t.Errorf("AllDownloadLinks lost the size: %+v", all)
	}

	// UPDATE path preserves and changes the size.
	got.Size = 999
	if err := d.UpdateDownloadLink(got); err != nil {
		t.Fatalf("UpdateDownloadLink: %v", err)
	}
	refetched, err := d.GetDownloadLink(id)
	if err != nil || refetched == nil || refetched.Size != 999 {
		t.Errorf("updated link = %+v (err %v), want size 999", refetched, err)
	}
}

// Fresh databases (userVersion 0) create download_links with the size column
// from the start; the round-trip must not depend on the migration path.
func TestDownloadLinkSizeRoundTripFreshDB(t *testing.T) {
	d := setupTestDB(t)
	defer d.Close()

	gameID, err := d.InsertGame(&Game{Title: "Size Game", Path: "/games/size", Engine: "RenPy", Status: "active"})
	if err != nil {
		t.Fatalf("InsertGame: %v", err)
	}

	// 346.6 MiB = 363436441.6 → truncated to 363436441.
	const size int64 = 363436441

	link := &DownloadLink{
		GameID:   gameID,
		URL:      "https://pixeldrain.com/u/abc",
		Host:     "pixeldrain",
		Name:     "Size_Game_v1.0.zip [346.6 MB]",
		Size:     size,
		Platform: PlatformWindows,
	}
	id, err := d.CreateDownloadLink(link)
	if err != nil {
		t.Fatalf("CreateDownloadLink: %v", err)
	}

	got, err := d.GetDownloadLink(id)
	if err != nil || got == nil {
		t.Fatalf("GetDownloadLink(%d): %v", id, err)
	}
	want := size
	if got.Size != want {
		t.Errorf("Size = %d, want %d", got.Size, want)
	}

	// A link created without a size defaults to 0 (unknown).
	zero := &DownloadLink{GameID: gameID, URL: "https://mega.nz/file/xyz", Host: "mega"}
	zeroID, err := d.CreateDownloadLink(zero)
	if err != nil {
		t.Fatalf("CreateDownloadLink without size: %v", err)
	}
	gotZero, err := d.GetDownloadLink(zeroID)
	if err != nil || gotZero == nil || gotZero.Size != 0 {
		t.Errorf("size-less link = %+v (err %v), want size 0", gotZero, err)
	}
}
