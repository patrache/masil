package ui_test

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui"
)

type targetMsg struct{ key string }

func targetCmd(key string) func(*ui.Context, api.Alert) tea.Cmd {
	return func(*ui.Context, api.Alert) tea.Cmd {
		return func() tea.Msg { return targetMsg{key} }
	}
}

func runTarget(t *testing.T, a api.Alert) (string, bool) {
	t.Helper()
	cmd, ok := ui.AlertTargetCmd(&ui.Context{}, a)
	if !ok {
		return "", false
	}
	if cmd == nil {
		return "", true
	}
	m, _ := cmd().(targetMsg)
	return m.key, true
}

func TestAlertTargetRegistry(t *testing.T) {
	t.Cleanup(ui.SnapshotRegistryForTest())
	ui.ResetRegistryForTest()

	doc := api.Alert{ID: "A", Target: api.AlertTarget{Type: "doc", ID: "D", DocType: "arch"}}
	if _, ok := ui.AlertTargetCmd(&ui.Context{}, doc); ok {
		t.Fatal("empty registry must report false")
	}

	ui.RegisterAlertTarget("doc:arch", targetCmd("doc:arch"))
	ui.RegisterAlertTarget("doc", targetCmd("doc"))
	ui.RegisterAlertTarget("workspace_share", targetCmd("ws"))

	if got, ok := runTarget(t, doc); !ok || got != "doc:arch" {
		t.Fatalf("doc_type arch => %q %v, want doc:arch", got, ok)
	}
	doc.Target.DocType = "apicol"
	if got, ok := runTarget(t, doc); !ok || got != "doc" {
		t.Fatalf("doc_type apicol => %q %v, want doc", got, ok)
	}
	doc.Target.DocType = ""
	if got, ok := runTarget(t, doc); !ok || got != "doc" {
		t.Fatalf("no doc_type => %q %v, want doc", got, ok)
	}
	share := api.Alert{Target: api.AlertTarget{Type: "workspace_share", ID: "S"}}
	if got, ok := runTarget(t, share); !ok || got != "ws" {
		t.Fatalf("workspace_share => %q %v", got, ok)
	}
	if _, ok := ui.AlertTargetCmd(&ui.Context{}, api.Alert{Target: api.AlertTarget{Type: "weird"}}); ok {
		t.Fatal("unregistered type must report false")
	}

	mustPanic(t, "duplicate alert target", func() { ui.RegisterAlertTarget("doc", targetCmd("again")) })
	mustPanic(t, "empty alert target", func() { ui.RegisterAlertTarget("", targetCmd("x")) })
	mustPanic(t, "nil alert target func", func() { ui.RegisterAlertTarget("other", nil) })

	// Registered() copies; the snapshot restores it.
	if len(ui.Registered().AlertTargets) != 3 {
		t.Fatalf("Registered().AlertTargets = %d, want 3", len(ui.Registered().AlertTargets))
	}
	restore := ui.SnapshotRegistryForTest()
	ui.ResetRegistryForTest()
	if _, ok := runTarget(t, share); ok {
		t.Fatal("reset registry still resolves")
	}
	restore()
	if _, ok := runTarget(t, share); !ok {
		t.Fatal("restored registry lost the target")
	}
}

// A registered func that returns nil (its tool's flag is off) still reports ok
// with a nil Cmd; the caller falls back.
func TestAlertTargetNilCmd(t *testing.T) {
	t.Cleanup(ui.SnapshotRegistryForTest())
	ui.ResetRegistryForTest()
	ui.RegisterAlertTarget("doc", func(*ui.Context, api.Alert) tea.Cmd { return nil })
	cmd, ok := ui.AlertTargetCmd(&ui.Context{}, api.Alert{Target: api.AlertTarget{Type: "doc"}})
	if !ok || cmd != nil {
		t.Fatalf("nil-returning target: cmd=%v ok=%v", cmd != nil, ok)
	}
}
