package scanner

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractVersionSpelledPrefixes(t *testing.T) {
	tests := map[string]string{
		"War Demon Kirsten ver1.11":                     "1.11",
		"Nymphomania Pardox ver1.10c -English Edition-": "1.10c",
		"Nymphomania Priestess Alpha Ver0.80":           "0.80",
		"Battle Demon Kirsten ov1.0.3":                  "1.0.3",
		"Boneka Ascension v.0.1":                        "0.1",
		"Some Game Version 2":                           "2",
		"Silver Dawn":                                   "",
		"Overture":                                      "",
	}
	for in, want := range tests {
		if got := ExtractVersion(in); got != want {
			t.Errorf("ExtractVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRPGMSystemJSONVersion(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "www", "data", "System.json"),
		[]byte("\xef\xbb\xbf{\"gameTitle\":\"Demons Roots v1.03\",\"locale\":\"en_US\"}"))
	if got := ExtractVersionFromDir(dir); got != "1.03" {
		t.Errorf("ExtractVersionFromDir = %q, want 1.03", got)
	}

	plain := t.TempDir()
	writeFile(t, filepath.Join(plain, "data", "System.json"), []byte(`{"gameTitle":"Below Sunshade"}`))
	if got := ExtractVersionFromDir(plain); got != "" {
		t.Errorf("versionless title gave %q, want empty", got)
	}
}

// makeRpyc builds a minimal RPC2 .rpyc whose slot 1 holds payload.
func makeRpyc(t *testing.T, payload string) []byte {
	t.Helper()
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	zw.Write([]byte(payload))
	zw.Close()
	var b bytes.Buffer
	b.Write(rpycMagic)
	header := len(rpycMagic) + 24 // one slot triple + terminator
	for _, v := range []uint32{1, uint32(header), uint32(z.Len()), 0, 0, 0} {
		binary.Write(&b, binary.LittleEndian, v)
	}
	b.Write(z.Bytes())
	return b.Bytes()
}

func TestRenpyVersionSources(t *testing.T) {
	t.Run("options.rpy", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "game", "options.rpy"), []byte("define config.version = \"0.9.1\"\n"))
		if got := ExtractVersionFromDir(dir); got != "0.9.1" {
			t.Errorf("got %q, want 0.9.1", got)
		}
	})
	t.Run("compiled options.rpyc", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "game", "options.rpyc"),
			makeRpyc(t, "name = \"Seeds of Chaos\"\nconfig.version = \"0.4.10\"\n"))
		if got := ExtractVersionFromDir(dir); got != "0.4.10" {
			t.Errorf("got %q, want 0.4.10", got)
		}
	})
	t.Run("rpa with source text", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "game", "scripts.rpa"),
			[]byte("RPA-3.0 0000000000000010 42424242\nMade with Ren'Py.define config.version = \"0.5\"\n"))
		if got := ExtractVersionFromDir(dir); got != "0.5" {
			t.Errorf("got %q, want 0.5", got)
		}
	})
	t.Run("rpa with archived rpyc", func(t *testing.T) {
		dir := t.TempDir()
		blob := append([]byte("RPA-3.0 0000000000000010 42424242\nMade with Ren'Py."), makeRpyc(t, `config.version = "1.2"`)...)
		writeFile(t, filepath.Join(dir, "game", "archive.rpa"), blob)
		if got := ExtractVersionFromDir(dir); got != "1.2" {
			t.Errorf("got %q, want 1.2", got)
		}
	})
	t.Run("format strings rejected", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "game", "options.rpy"),
			[]byte("    config.version = \"%s %s\" % (BK_DIST, patch_version)\n"))
		if got := ExtractVersionFromDir(dir); got != "" {
			t.Errorf("got %q, want empty for a format string", got)
		}
	})
}
