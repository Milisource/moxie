package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

// The relaunched process must never inherit the swap-agent env vars — a
// stale MOXIE_UPDATE_SWAP=1 would turn the fresh instance into an agent.
func TestWithoutUpdateEnv(t *testing.T) {
	env := []string{
		"HOME=/home/user",
		"MOXIE_UPDATE_SWAP=1",
		"PATH=/usr/bin",
		"MOXIE_UPDATE_MARKER=/x/pending-update.json",
		"MOXIE_UPDATE_PARENT=1234",
	}
	got := withoutUpdateEnv(env)
	want := []string{"HOME=/home/user", "PATH=/usr/bin"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("withoutUpdateEnv = %v, want %v", got, want)
	}
}

func TestWithoutUpdateEnvKeepsUnrelatedMoxieVars(t *testing.T) {
	env := []string{"MOXIE_FOO=bar", "MOXIE_UPDATE_SWAP=1"}
	got := withoutUpdateEnv(env)
	if len(got) != 1 || got[0] != "MOXIE_FOO=bar" {
		t.Errorf("withoutUpdateEnv = %v, want only MOXIE_FOO kept", got)
	}
}

// The marker must round-trip the staged path, exe path, and the original
// command-line arguments so the swap agent can replay them on relaunch.
func TestPendingUpdateMarkerRoundTrip(t *testing.T) {
	m := pendingUpdateMarker{
		Staged: `C:\Users\mili\AppData\Roaming\moxie\updates\moxie-desktop-windows-amd64.exe`,
		Exe:    `C:\Program Files\Moxie\moxie-desktop-windows-amd64.exe`,
		Args:   []string{"--flag", "value with spaces"},
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back pendingUpdateMarker
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Staged != m.Staged || back.Exe != m.Exe || !reflect.DeepEqual(back.Args, m.Args) {
		t.Errorf("round trip = %+v, want %+v", back, m)
	}
}
