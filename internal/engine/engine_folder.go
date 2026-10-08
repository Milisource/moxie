package engine

// engineFolderNames maps each canonical engine to the subfolder moxie installs
// its games into when "organize installs by engine" is enabled. Names are
// upper-case and, apart from a few long-standing user conventions, mirror the
// category directories the scanner recognizes (see scanner.isEngineName):
// Java → JRE, RenPy → REN'PY, UnrealEngine → UNREAL and WolfRPG → WOLF.
var engineFolderNames = map[Engine]string{
	Others:       "OTHER",
	ADRIFT:       "ADRIFT",
	Flash:        "FLASH",
	Godot:        "GODOT",
	HTML:         "HTML",
	Java:         "JRE",
	QSP:          "QSP",
	RAGS:         "RAGS",
	RPGM:         "RPGM",
	RenPy:        "REN'PY",
	Tads:         "TADS",
	Unity:        "UNITY",
	UnrealEngine: "UNREAL",
	WebGL:        "WEBGL",
	WolfRPG:      "WOLF",
}

// InstallFolderName returns the engine-named subfolder a fresh install of this
// engine belongs in, falling back to "OTHER" for an empty or unrecognized
// engine. The result is a single, path-safe directory name.
func InstallFolderName(engine string) string {
	if name, ok := engineFolderNames[Engine(engine)]; ok {
		return name
	}
	return "OTHER"
}
