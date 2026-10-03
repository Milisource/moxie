package scanner

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Most game folders carry no version in their name, so installed versions
// are recovered from engine files instead. These helpers cover the two
// engines that make up most of a typical library and do embed one:
// Ren'Py (config.version) and RPG Maker MV/MZ (System.json gameTitle).

// renpyVerRE matches a Ren'Py config.version assignment in source text or in
// the source strings a compiled .rpyc keeps in its AST pickle. `define` is
// optional: older games assign it inside an init python block.
var renpyVerRE = regexp.MustCompile(`config\.version\s*=\s*["']([^"'\r\n]{1,40})["']`)

// rpaScanLimit caps how large a Ren'Py archive may be before we skip it.
// Scripts usually live in a small scripts.rpa / archive.rpa; multi-GB image
// archives are never worth reading during a scan.
const rpaScanLimit = 64 << 20

// rpycMagic heads every compiled Ren'Py script (Ren'Py 6.18+).
var rpycMagic = []byte("RENPY RPC2")

// renpyVersionFromDir returns the game's config.version, trying in order:
// game/options.rpy, compiled game/options.rpyc, then any .rpa archive under
// game/ small enough to read (scripts are stored inside as .rpy text and/or
// .rpyc blobs).
func renpyVersionFromDir(dir string) string {
	gameDir := filepath.Join(dir, "game")
	if data, err := os.ReadFile(filepath.Join(gameDir, "options.rpy")); err == nil {
		if v := renpyVersionIn(data); v != "" {
			return v
		}
	}
	if data, err := os.ReadFile(filepath.Join(gameDir, "options.rpyc")); err == nil {
		if v := renpyVersionInRpyc(data); v != "" {
			return v
		}
	}

	archives, _ := filepath.Glob(filepath.Join(gameDir, "*.rpa"))
	type sized struct {
		path string
		size int64
	}
	var cands []sized
	for _, a := range archives {
		if fi, err := os.Stat(a); err == nil && fi.Size() <= rpaScanLimit {
			cands = append(cands, sized{a, fi.Size()})
		}
	}
	// Smallest first: scripts.rpa beats a 60 MB audio archive.
	sort.Slice(cands, func(i, j int) bool { return cands[i].size < cands[j].size })
	for _, c := range cands {
		data, err := os.ReadFile(c.path)
		if err != nil {
			continue
		}
		if v := renpyVersionIn(data); v != "" {
			return v
		}
		// Archived .rpyc files: each blob starts with the magic and its slot
		// offsets are relative to that start.
		for off := 0; ; {
			i := bytes.Index(data[off:], rpycMagic)
			if i < 0 {
				break
			}
			if v := renpyVersionInRpyc(data[off+i:]); v != "" {
				return v
			}
			off += i + len(rpycMagic)
		}
	}
	return ""
}

// renpyVersionIn returns the first plausible config.version in data.
func renpyVersionIn(data []byte) string {
	for _, m := range renpyVerRE.FindAllSubmatch(data, -1) {
		if v := cleanRenpyVersion(string(m[1])); v != "" {
			return v
		}
	}
	return ""
}

// cleanRenpyVersion rejects format strings and interpolations
// (`"%s %s" % (...)`, "[patch]") that are not literal versions, and values
// without a digit.
func cleanRenpyVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.ContainsAny(v, "%[{") || !strings.ContainsAny(v, "0123456789") {
		return ""
	}
	return v
}

// renpyVersionInRpyc decompresses the slots of a RPC2 .rpyc and searches
// them for config.version. Layout: magic, then (slot, start, length) uint32
// little-endian triples terminated by slot 0; each slot is zlib-compressed.
func renpyVersionInRpyc(data []byte) string {
	if !bytes.HasPrefix(data, rpycMagic) {
		return ""
	}
	pos := len(rpycMagic)
	for n := 0; n < 8 && pos+12 <= len(data); n++ {
		slot := binary.LittleEndian.Uint32(data[pos:])
		start := int(binary.LittleEndian.Uint32(data[pos+4:]))
		length := int(binary.LittleEndian.Uint32(data[pos+8:]))
		pos += 12
		if slot == 0 {
			break
		}
		if start < 0 || length <= 0 || start+length > len(data) {
			continue
		}
		zr, err := zlib.NewReader(bytes.NewReader(data[start : start+length]))
		if err != nil {
			continue
		}
		// Script ASTs are small; cap the inflate so a corrupt blob cannot
		// balloon memory.
		raw, _ := io.ReadAll(io.LimitReader(zr, 16<<20))
		zr.Close()
		if v := renpyVersionIn(raw); v != "" {
			return v
		}
	}
	return ""
}

// rpgmVersionFromDir extracts a version from an RPG Maker MV/MZ System.json
// gameTitle ("Demons Roots v1.03", "Nymphomania Pardox ver1.10c").
func rpgmVersionFromDir(dir string) string {
	for _, p := range []string{
		filepath.Join(dir, "www", "data", "System.json"),
		filepath.Join(dir, "data", "System.json"),
	} {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var sys struct {
			GameTitle string `json:"gameTitle"`
		}
		// Some exports carry a UTF-8 BOM, which encoding/json rejects.
		if err := json.Unmarshal(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), &sys); err != nil {
			continue
		}
		if v := ExtractVersion(sys.GameTitle); v != "" {
			return v
		}
	}
	return ""
}
