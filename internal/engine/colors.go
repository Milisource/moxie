package engine

import (
	_ "embed"
	"encoding/json"
)

// Canonical engine color map, shared by the TUI (via go:embed) and the
// desktop frontend (which imports engine-colors.json directly).
//
// Colors mirror F95Zone's "Latest Updates" page. Engine→class mapping comes
// from the site's latestUpdates prefix data; class→color from its stylesheet:
//
//	pre-java/pre-renpy/pre-unity/pre-webgl  engine-specific pill colors
//	label--blue        RPGM, ADRIFT, Tads
//	label--royalBlue   UnrealEngine
//	label--skyBlue     Godot
//	label--olive       HTML
//	label--gray        Flash
//	label--red         QSP
//	label--orange      RAGS
//	label--green       WolfRPG
//	label--lightGreen  Others
//
// Keep this file in sync with engine-colors.json — that file is the single
// source of truth.
//
//go:embed engine-colors.json
var colorsJSON []byte

var engineColors map[string]string

func init() {
	if err := json.Unmarshal(colorsJSON, &engineColors); err != nil {
		// Embedded data is static; failure means the file is malformed.
		panic("engine: invalid embedded engine-colors.json: " + err.Error())
	}
}

// EngineColor returns the canonical hex color (e.g. "#ea5201") for the given
// engine name, or "" if the engine is not in the palette.
func EngineColor(name string) string {
	return engineColors[name]
}
