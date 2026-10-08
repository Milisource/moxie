package main

import (
	"path/filepath"
	"testing"

	"github.com/mili/moxie/internal/config"
)

// TestInstallTargetDir covers the destination chosen for a fresh install:
// flat by default, engine-nested once the setting is enabled.
func TestInstallTargetDir(t *testing.T) {
	config.SetConfigDirForTest(t.TempDir())
	t.Cleanup(func() { config.SetConfigDirForTest("") })

	a := &App{}
	const dest = "/games"

	if got, want := a.installTargetDir(dest, "My Game", "HTML"), filepath.Join(dest, "My Game"); got != want {
		t.Errorf("flat: installTargetDir = %q, want %q", got, want)
	}

	cfg, err := config.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Set("organize-installs-by-engine", "true")
	if err := config.WriteConfig(cfg); err != nil {
		t.Fatal(err)
	}

	if got, want := a.installTargetDir(dest, "My Game", "Java"), filepath.Join(dest, "JRE", "My Game"); got != want {
		t.Errorf("engine folder: installTargetDir = %q, want %q", got, want)
	}
	if got, want := a.installTargetDir(dest, "My Game", ""), filepath.Join(dest, "OTHER", "My Game"); got != want {
		t.Errorf("empty engine: installTargetDir = %q, want %q", got, want)
	}
}

// TestOrganizeInstallsRoundTrip guards the config-backed setting default and
// its enable/disable persistence.
func TestOrganizeInstallsRoundTrip(t *testing.T) {
	config.SetConfigDirForTest(t.TempDir())
	t.Cleanup(func() { config.SetConfigDirForTest("") })

	a := &App{}
	if a.GetOrganizeInstalls() {
		t.Fatal("default should be false")
	}
	if err := a.SetOrganizeInstalls(true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !a.GetOrganizeInstalls() {
		t.Error("setting should be true after enable")
	}
	if err := a.SetOrganizeInstalls(false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if a.GetOrganizeInstalls() {
		t.Error("setting should be false after disable")
	}
}
