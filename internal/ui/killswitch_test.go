package ui_test

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui"
)

func TestKillSwitchRegistry(t *testing.T) {
	t.Cleanup(ui.SnapshotRegistryForTest())
	ui.ResetRegistryForTest()
	if ui.KillSwitchActive() || ui.KillAll(nil) != nil {
		t.Fatal("an empty registry is active")
	}
	a, b := false, true
	var killed []string
	ui.RegisterKillSwitch(ui.KillSwitch{Active: func() bool { return a }, Kill: func(*ui.Context) tea.Cmd { killed = append(killed, "a"); return nil }})
	ui.RegisterKillSwitch(ui.KillSwitch{Active: func() bool { return b }, Kill: func(*ui.Context) tea.Cmd { killed = append(killed, "b"); return nil }})
	if !ui.KillSwitchActive() {
		t.Fatal("not active")
	}
	ui.KillAll(nil)
	if len(killed) != 1 || killed[0] != "b" {
		t.Fatalf("killed = %v, want only the active b", killed)
	}
	if n := len(ui.Registered().KillSwitches); n != 2 {
		t.Fatalf("Registry.KillSwitches = %d", n)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("a nil Kill did not panic")
		}
	}()
	ui.RegisterKillSwitch(ui.KillSwitch{Active: func() bool { return true }})
}
