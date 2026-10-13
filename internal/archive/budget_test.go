package archive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Native extractor budgets
// ---------------------------------------------------------------------------

func TestExtractZip_TotalBytesBudget(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	zipPath := filepath.Join(src, "big.zip")
	createTestZip(t, zipPath, map[string]string{
		"a.txt": "aaaaaaaaaaaa",
		"b.txt": "bbbbbbbbbbbb",
	})

	dest := t.TempDir()
	_, err := Extract(zipPath, dest, Options{MaxTotalBytes: 5})
	if err == nil {
		t.Fatal("expected a budget error, got nil")
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Errorf("error = %v, want it to mention the limit", err)
	}
	assertNoExtractedFiles(t, dest)
}

func TestExtractZip_EntryBudget(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	zipPath := filepath.Join(src, "many.zip")
	createTestZip(t, zipPath, map[string]string{
		"a.txt": "a",
		"b.txt": "b",
		"c.txt": "c",
	})

	dest := t.TempDir()
	_, err := Extract(zipPath, dest, Options{MaxEntries: 2})
	if err == nil {
		t.Fatal("expected a budget error, got nil")
	}
	assertNoExtractedFiles(t, dest)
}

func TestExtractZip_ZeroBudgetIsUnlimited(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	zipPath := filepath.Join(src, "ok.zip")
	createTestZip(t, zipPath, map[string]string{
		"a.txt": "aaaa",
		"b.txt": "bbbb",
	})

	dest := t.TempDir()
	result, err := Extract(zipPath, dest, Options{MaxTotalBytes: 0, MaxEntries: 0})
	if err != nil {
		t.Fatalf("Extract with zero budget: %v", err)
	}
	if result.FilesExtracted != 2 {
		t.Errorf("FilesExtracted = %d, want 2", result.FilesExtracted)
	}
}

func TestExtractTarGz_TotalBytesBudget(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	tgzPath := filepath.Join(src, "big.tar.gz")
	createTestTarGz(t, tgzPath, map[string]string{
		"a.txt": "aaaaaaaaaaaa",
		"b.txt": "bbbbbbbbbbbb",
	})

	dest := t.TempDir()
	_, err := Extract(tgzPath, dest, Options{MaxTotalBytes: 5})
	if err == nil {
		t.Fatal("expected a budget error, got nil")
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Errorf("error = %v, want it to mention the limit", err)
	}
	assertNoExtractedFiles(t, dest)
}

func TestExtractTarGz_EntryBudget(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	tgzPath := filepath.Join(src, "many.tar.gz")
	createTestTarGz(t, tgzPath, map[string]string{
		"a.txt": "a",
		"b.txt": "b",
		"c.txt": "c",
	})

	dest := t.TempDir()
	_, err := Extract(tgzPath, dest, Options{MaxEntries: 2})
	if err == nil {
		t.Fatal("expected a budget error, got nil")
	}
	assertNoExtractedFiles(t, dest)
}

// assertNoExtractedFiles fails when dir exists and contains any regular file
// (recursively). A missing dir also passes: the extractor may not have created it.
func assertNoExtractedFiles(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return
	}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			t.Errorf("unexpected extracted file %q", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
}

// ---------------------------------------------------------------------------
// Listing parser fixtures
// ---------------------------------------------------------------------------

func TestParseZipListing(t *testing.T) {
	t.Parallel()
	const listing = `Archive:  saves.zip
  Length      Date    Time    Name
---------  ---------- -----   ----
     1284  2024-05-01 12:00   save/persistent
      128  2024-05-01 12:00   save/slot1.save
        0  2024-05-01 12:00   save/empty/
---------                     -------
     1412                     3 files
`
	total, entries, err := parseZipListing(listing)
	if err != nil {
		t.Fatalf("parseZipListing: %v", err)
	}
	if total != 1412 {
		t.Errorf("total = %d, want 1412", total)
	}
	if entries != 2 {
		t.Errorf("entries = %d, want 2 (directory row excluded)", entries)
	}
}

func TestParseZipListing_LegacyDateFormat(t *testing.T) {
	t.Parallel()
	const listing = `  Length      Date    Time    Name
---------  ---------- -----   ----
      100  05-01-24 12:00   a.txt
---------                     -------
      100                     1 file
`
	total, entries, err := parseZipListing(listing)
	if err != nil {
		t.Fatalf("parseZipListing: %v", err)
	}
	if total != 100 || entries != 1 {
		t.Errorf("total, entries = %d, %d; want 100, 1", total, entries)
	}
}

func TestParseZipListing_Unrecognized(t *testing.T) {
	t.Parallel()
	if _, _, err := parseZipListing("this is not a listing\n"); err == nil {
		t.Fatal("expected an error for unrecognized output")
	}
}

func TestParse7zListing(t *testing.T) {
	t.Parallel()
	const listing = `7-Zip (z) 24.09 : Copyright (c) 1999-2024 Igor Pavlov : 2024-11-29
64-bit locale=C ...

Scanning the drive for archives:
1 file, 4096 bytes (4 KiB)

Listing archive: saves.7z

--
Path = saves.7z
Type = 7z
Physical Size = 4096
Headers Size = 234
Method = LZMA2:12
Solid = -
Blocks = 1

----------
Path = save/persistent
Size = 1284
Packed Size = 400
Modified = 2024-05-01 12:00:00
Attributes = A
CRC = 12345678
Encrypted = -

Path = save/slot1.save
Size = 128
Packed Size = 40
Modified = 2024-05-01 12:00:00
Attributes = A
CRC = 87654321
Encrypted = -

Path = save/empty
Size = 0
Packed Size = 0
Modified = 2024-05-01 12:00:00
Attributes = D
CRC =
Encrypted = -

`
	total, entries, err := parse7zListing(listing)
	if err != nil {
		t.Fatalf("parse7zListing: %v", err)
	}
	if total != 1412 {
		t.Errorf("total = %d, want 1412", total)
	}
	if entries != 2 {
		t.Errorf("entries = %d, want 2 (archive-level and directory blocks excluded)", entries)
	}
}

func TestParse7zListing_Unrecognized(t *testing.T) {
	t.Parallel()
	if _, _, err := parse7zListing("nothing useful here\n"); err == nil {
		t.Fatal("expected an error for unrecognized output")
	}
}

func TestParseRarListing(t *testing.T) {
	t.Parallel()
	const listing = `UNRAR 6.24 freeware      Copyright (c) 1993-2024 Alexander Roshal

Archive: saves.rar
Details: RAR 5

 Attributes      Size     Date    Time   Name
----------- ---------  ---------- -----  ----
    ..A....       1284  2024-05-01 12:00  save/persistent
    ..A....        128  2024-05-01 12:00  save/slot1.save
    ..D....          0  2024-05-01 12:00  save/empty
----------- ---------  ---------- -----  ----
                 1412                     2
`
	total, entries, err := parseRarListing(listing)
	if err != nil {
		t.Fatalf("parseRarListing: %v", err)
	}
	if total != 1412 {
		t.Errorf("total = %d, want 1412", total)
	}
	if entries != 2 {
		t.Errorf("entries = %d, want 2 (directory row excluded)", entries)
	}
}

func TestParseRarListing_FooterFallback(t *testing.T) {
	t.Parallel()
	const listing = `UNRAR freeware

Archive: saves.rar

----------- ---------  ---------- -----  ----
                 1412                     2
`
	total, entries, err := parseRarListing(listing)
	if err != nil {
		t.Fatalf("parseRarListing: %v", err)
	}
	if total != 1412 || entries != 2 {
		t.Errorf("total, entries = %d, %d; want 1412, 2", total, entries)
	}
}

func TestParseRarListing_Unrecognized(t *testing.T) {
	t.Parallel()
	if _, _, err := parseRarListing("nothing useful here\n"); err == nil {
		t.Fatal("expected an error for unrecognized output")
	}
}

// ---------------------------------------------------------------------------
// Pre-check gating
// ---------------------------------------------------------------------------

func TestPrecheckExtractBudget_ZeroBudgetSkipsListing(t *testing.T) {
	t.Parallel()
	// With no budget set no listing command is run, even for a path no tool
	// could list — proving the zero-value Options keep the old behavior.
	if err := precheckExtractBudget("/nonexistent-save.7z", "7z", Options{}); err != nil {
		t.Errorf("precheckExtractBudget(zero budget) = %v, want nil", err)
	}
}
