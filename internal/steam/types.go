package steam

import (
	"errors"
	"hash/crc32"
	"runtime"
)

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

var (
	ErrSteamNotFound  = errors.New("steam: installation not found")
	ErrSteamRunning   = errors.New("steam: Steam is running — close it before modifying shortcuts")
	ErrNoUsers        = errors.New("steam: no Steam users found under userdata/")
	ErrDuplicate      = errors.New("steam: a shortcut with this title already exists")
	ErrNotLinux       = errors.New("steam: Proton configuration is only available on Linux")
	ErrNotImplemented = errors.New("steam: feature not yet implemented")
	ErrInvalidProton   = errors.New("steam: unknown Proton version")
	ErrUnsupportedFormat = errors.New("steam: unsupported image format")
	ErrInvalidURL        = errors.New("steam: invalid or unsafe download URL")
)

// IsLinux returns true when the current platform is Linux.
func IsLinux() bool { return runtime.GOOS == "linux" }

// ---------------------------------------------------------------------------
// Artwork types
// ---------------------------------------------------------------------------

// ArtType represents a Steam grid artwork type.
type ArtType int

const (
	ArtVertical   ArtType = iota // 600x900 portrait grid  (file suffix "p")
	ArtHorizontal                // 460x215 landscape grid (no suffix)
	ArtHero                      // 1920x620 hero banner   (suffix "_hero")
	ArtLogo                      // logo                    (suffix "_logo")
	ArtIcon                      // icon                    (suffix "_icon")
)

// Suffix returns the filename suffix for this artwork type
// (e.g. "p" for vertical grid, "_hero" for hero banner).
func (a ArtType) Suffix() string {
	switch a {
	case ArtVertical:
		return "p"
	case ArtHorizontal:
		return ""
	case ArtHero:
		return "_hero"
	case ArtLogo:
		return "_logo"
	case ArtIcon:
		return "_icon"
	}
	return ""
}

// Dimensions returns the standard width and height for this artwork type.
// Returns (0,0) for variable-size types (Logo, Icon).
func (a ArtType) Dimensions() (int, int) {
	switch a {
	case ArtVertical:
		return 600, 900
	case ArtHorizontal:
		return 460, 215
	case ArtHero:
		return 1920, 620
	default:
		return 0, 0
	}
}

// ---------------------------------------------------------------------------
// Shortcuts
// ---------------------------------------------------------------------------

// ShortcutEntry represents a single non-Steam game shortcut in shortcuts.vdf.
// Field names match the binary VDF keys (case-sensitive).
type ShortcutEntry struct {
	AppID              uint32   `vdf:"appid"`
	AppName            string   `vdf:"AppName"`
	Exe                string   `vdf:"exe"`
	StartDir           string   `vdf:"StartDir"`
	Icon               string   `vdf:"icon"`
	LaunchOptions      string   `vdf:"LaunchOptions"`
	IsHidden           bool     `vdf:"IsHidden"`
	AllowDesktopConfig bool     `vdf:"AllowDesktopConfig"`
	AllowOverlay       bool     `vdf:"AllowOverlay"`
	OpenVR             bool     `vdf:"OpenVR"`
	Devkit             bool     `vdf:"Devkit"`
	LastPlayTime       uint32   `vdf:"LastPlayTime"`
	FlatpakAppID       string   `vdf:"FlatpakAppID"`
	SortAs             string   `vdf:"sortas"`
	Tags               []string                 `vdf:"tags"`
	// RawFields preserves any VDF keys not explicitly handled above.
	// This ensures round-trip safety when Steam or other tools add new fields.
	RawFields map[string]interface{} `vdf:"-"`
}

// SteamPaths holds resolved file paths for a single Steam user.
type SteamPaths struct {
	SteamRoot    string // Steam installation root
	UserID3      uint32 // Steam ID3 (numeric userdata folder name)
	ShortcutsVDF string // <UserDataDir>/config/shortcuts.vdf
	GridDir      string // <UserDataDir>/config/grid/
	ConfigVDF    string // <SteamRoot>/config/config.vdf
}

// ---------------------------------------------------------------------------
// AppID generation
// ---------------------------------------------------------------------------

// GenerateAppID computes a deterministic Steam AppID for a non-Steam game.
// Algorithm: uint32(crc32(append(exe, appName...))) | 0x80000000
//
// The 0x80000000 mask ensures the ID falls in the non-Steam game range
// (high bit set), avoiding collisions with real Steam App IDs.
func GenerateAppID(exe, appName string) uint32 {
	input := []byte(exe)
	input = append(input, []byte(appName)...)
	crc := crc32.ChecksumIEEE(input)
	return crc | 0x80000000
}

// ---------------------------------------------------------------------------
// Proton
// ---------------------------------------------------------------------------

// KnownProtonVersions is the default set of Proton version identifiers
// used as a fallback when ListProtonVersions cannot scan the filesystem.
var KnownProtonVersions = []string{
	"proton_experimental",
	"proton_9",
	"proton_hotfix",
	"proton_8",
	"proton_7",
}

// ---------------------------------------------------------------------------
// SteamGridDB (deferred to Phase 2)
// ---------------------------------------------------------------------------

// SGDBGameResult is a search result from SteamGridDB.
type SGDBGameResult struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// SGDBImageResult is an artwork result from SteamGridDB.
type SGDBImageResult struct {
	URL   string `json:"url"`
	Thumb string `json:"thumb"`
	Width int    `json:"width"`
	Score int    `json:"score"`
}
