package app_test

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
)

// While a kill switch is active, ctrl+\ x cuts it from any screen (A7.5 JX6);
// otherwise the keys are masil's as before.
func TestGlobalKillWhileSharing(t *testing.T) {
	t.Cleanup(ui.SnapshotRegistryForTest())
	active, killed := false, 0
	ui.RegisterKillSwitch(ui.KillSwitch{
		Active: func() bool { return active },
		Kill:   func(*ui.Context) tea.Cmd { killed++; return nil },
	})
	h := uitest.New(t)
	h.Press("ctrl+\\", "x")
	if killed != 0 {
		t.Fatalf("Kill ran while not sharing")
	}
	active = true
	h.Press("ctrl+\\", "x")
	if killed != 1 {
		t.Fatalf("Kill calls = %d, want 1", killed)
	}
	h.Press("ctrl+\\")
	h.Advance(2e9) // the prefix waits 2s
	h.Press("x")
	if killed != 1 {
		t.Fatalf("x after the prefix timed out still killed (%d)", killed)
	}
	h.Press("ctrl+\\", "q", "x")
	if killed != 1 {
		t.Fatalf("x after a cancelled prefix killed (%d)", killed)
	}
}
