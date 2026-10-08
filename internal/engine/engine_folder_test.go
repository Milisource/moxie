package engine

import (
	"strings"
	"testing"
)

// TestInstallFolderName pins the engine→folder mapping used when "organize
// installs by engine" is enabled, including the long-standing user conventions
// (Java → JRE, RenPy → REN'PY, UnrealEngine → UNREAL, WolfRPG → WOLF) and the
// OTHER fallback for empty/unknown engines.
func TestInstallFolderName(t *testing.T) {
	cases := []struct {
		engine string
		want   string
	}{
		{"HTML", "HTML"},
		{"Java", "JRE"},
		{"Godot", "GODOT"},
		{"RenPy", "REN'PY"},
		{"RPGM", "RPGM"},
		{"Unity", "UNITY"},
		{"Flash", "FLASH"},
		{"UnrealEngine", "UNREAL"},
		{"WebGL", "WEBGL"},
		{"WolfRPG", "WOLF"},
		{"QSP", "QSP"},
		{"RAGS", "RAGS"},
		{"Tads", "TADS"},
		{"ADRIFT", "ADRIFT"},
		{"Others", "OTHER"},
		{"", "OTHER"},
		{"SomeCustomEngine", "OTHER"},
	}
	for _, tc := range cases {
		if got := InstallFolderName(tc.engine); got != tc.want {
			t.Errorf("InstallFolderName(%q) = %q, want %q", tc.engine, got, tc.want)
		}
	}
}

// TestInstallFolderNameIsPathSafe guards that every mapped folder is a single
// path element, so filepath.Join cannot escape the chosen destination.
func TestInstallFolderNameIsPathSafe(t *testing.T) {
	all := []Engine{
		Others, ADRIFT, Flash, Godot, HTML, Java, QSP, RAGS,
		RPGM, RenPy, Tads, Unity, UnrealEngine, WebGL, WolfRPG,
	}
	for _, e := range all {
		folder := InstallFolderName(string(e))
		if folder == "" {
			t.Errorf("InstallFolderName(%q) returned empty", e)
		}
		if strings.ContainsAny(folder, `/\`) {
			t.Errorf("InstallFolderName(%q) = %q, want a single path element", e, folder)
		}
	}
}
