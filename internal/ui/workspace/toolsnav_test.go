package workspace

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/uitest"
)

// 0.1.3 patch (tools-nav fix 2): ctrl+t — also ctrl+ㅅ on a Korean layout —
// leaves a workspace terminal that holds the keyboard and goes back to the
// chat; the terminal keeps running and never sees the key.
func TestCtrlTLeavesWorkspaceTerminal(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{{Code: 't', Mod: tea.ModCtrl}, {Code: 'ㅅ', Mod: tea.ModCtrl}} {
		t.Run(key.String(), func(t *testing.T) {
			fake := setup(t)
			h := uitest.New(t)
			s := openFolder(t, h, "workspace:test", copyFixture(t))
			h.Press("ctrl+\\", "3", "n", "enter") // a terminal (the login shell)
			if !s.GrabsKeys() {
				t.Fatal("the terminal does not hold the keyboard")
			}
			started := fake.Started()
			h.Press("a")
			h.Send(key)
			if h.Nav().Front == app.FrontTools {
				t.Fatalf("%s did not leave the workspace terminal", key.String())
			}
			if len(fake.Started()) != len(started) {
				t.Fatal("ctrl+t started something")
			}
			ids := fake.IDs()
			if len(ids) != 1 {
				t.Fatalf("panes = %v, want the one terminal", ids)
			}
			if keys := fake.Keys(ids[0]); !slices.Equal(keys, []string{"a"}) {
				t.Fatalf("the terminal got %q, want only a", keys)
			}
			if !s.GrabsKeys() {
				t.Fatal("the terminal stopped (or gave up the keyboard)")
			}
		})
	}
}
