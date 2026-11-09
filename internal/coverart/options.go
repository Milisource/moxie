package coverart

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mili/moxie/internal/config"
)

// Config keys for cover sources.
const (
	KeySteam = "cover-steam" // "false" disables (default on)
	KeyVNDB  = "cover-vndb"  // "true" enables (default off)
	KeySGDB  = "steamgriddb-key"
)

// SGDBKey resolves the SteamGridDB key: STEAMGRIDDB_KEY, then config, then
// the legacy flat file (same order as the CLI's steam artwork commands).
func SGDBKey() string {
	if k := os.Getenv("STEAMGRIDDB_KEY"); k != "" {
		return k
	}
	if cfg, err := config.ReadConfig(); err == nil {
		if k := cfg.Get(KeySGDB); k != "" {
			return k
		}
	}
	if b, err := os.ReadFile(filepath.Join(config.ConfigDir(), "steamgriddb-key")); err == nil {
		return strings.TrimSpace(string(b))
	}
	return ""
}

// OptionsFromConfig reads the enabled sources from config.
func OptionsFromConfig() Options {
	o := Options{Steam: true, SGDBKey: SGDBKey()}
	if cfg, err := config.ReadConfig(); err == nil {
		o.Steam = cfg.Get(KeySteam) != "false"
		o.VNDB = cfg.Get(KeyVNDB) == "true"
	}
	return o
}

// NeedsUpgrade reports whether a cover (w×h, zeros = none) is worth looking
// up portrait art for: missing, landscape, or under 600px on its short edge.
func NeedsUpgrade(w, h int, locked bool) bool {
	if locked {
		return false
	}
	return w == 0 || h == 0 || w >= h || min(w, h) < 600
}
