package downloader

import "testing"

func TestClassifyLink(t *testing.T) {
	cases := []struct {
		name     string
		kind     LinkKind
		from, to string
	}{
		{"DOWNLOAD · Win", LinkFull, "", ""},
		{"Win/Linux", LinkFull, "", ""},
		{"PIXELDRAIN", LinkFull, "", ""},
		{"LonaRPG.Beta.0.9.8.1.3 · Win", LinkFull, "", ""},
		{"v2.0.0 · Linux", LinkFull, "", ""},
		{"Update Only (v0.17 -> v0.18) · Win", LinkPatch, "0.17", "0.18"},
		{"Update-only", LinkPatch, "", ""},
		{"Patch v0.4.1 to v0.4.2", LinkPatch, "0.4.1", "0.4.2"},
		{"Update (0.5 → 0.6) · Win", LinkPatch, "0.5", "0.6"},
		{"ENG Patch v1.02: MEGA -", LinkExtra, "", ""},
		{"Chapter 1 - Russian Translation · Win", LinkExtra, "", ""},
		{"v3.1.5 with Cheats · Win", LinkExtra, "", ""},
		{"Old Ren'py Version · Win", LinkExtra, "", ""},
		{"DOWNLOAD Part 2 · Win", LinkExtra, "", ""},
		{"Walkthrough Mod · Win", LinkExtra, "", ""},
		{"Decensor", LinkExtra, "", ""},
		{"DLC", LinkExtra, "", ""},
		{"Unofficial Mod", LinkExtra, "", ""},
	}
	for _, c := range cases {
		got := ClassifyLink(c.name)
		if got.Kind != c.kind || got.FromVersion != c.from || got.ToVersion != c.to {
			t.Errorf("ClassifyLink(%q) = %+v, want %s %q→%q", c.name, got, c.kind, c.from, c.to)
		}
	}
}

func TestDetectPlatformLabels(t *testing.T) {
	cases := map[string]Platform{
		"DOWNLOAD · Win":         PlatformWindows,
		"DOWNLOAD Win · Mac":     PlatformMacOS,
		"DOWNLOAD Win · Android": PlatformAndroid,
		"DOWNLOAD · Linux":       PlatformLinux,
		"Win/Linux":              PlatformAll,
		"Win/Linux · Mac":        PlatformMacOS,
		"DOWNLOAD Win 64":        PlatformWindows,
		"Android(v1.11)":         PlatformAndroid,
		"research edition":       PlatformUnknown,
		"DOWNLOAD":               PlatformUnknown,
	}
	for name, want := range cases {
		if got := DetectPlatform(name, "https://f95zone.to/masked/pixeldrain.com/1/2/x"); got != want {
			t.Errorf("DetectPlatform(%q) = %s, want %s", name, got, want)
		}
	}
}

func TestInstalledPlatform(t *testing.T) {
	cases := map[string]Platform{
		"/games/MHF/My_hentai_fantasy.exe": PlatformWindows,
		"/games/X/X.sh":                    PlatformLinux,
		"/games/Y/Game.x86_64":             PlatformLinux,
		"/games/Z/nw":                      PlatformLinux,
		"/games/M/Game.app":                PlatformMacOS,
		"":                                 PlatformUnknown,
		"/games/H/index.html":              PlatformUnknown,
	}
	for p, want := range cases {
		if got := InstalledPlatform(p); got != want {
			t.Errorf("InstalledPlatform(%q) = %s, want %s", p, got, want)
		}
	}
	if InstallPlatformPriority(PlatformLinux, PlatformWindows) >= 0 {
		t.Error("Linux build must be rejected for a Windows install")
	}
}
