package workspace

import (
	"testing"

	"github.com/patrache/masil/internal/ui/uitest"
)

func TestGoldenSolo(t *testing.T) {
	setup(t)
	uitest.ForEachSize(t, append(append([]uitest.Size(nil), uitest.Required...), uitest.Tiny), func(t *testing.T, h *uitest.Harness) {
		s := openFolder(t, h, "workspace:golden", copyFixture(t))
		s.openDoc(h.Context(), hubTS)
		h.Golden("solo")
	})
	t.Run("widget", func(t *testing.T) {
		h := uitest.New(t)
		h.Resize(80, 24)
		s := openFolder(t, h, "workspace:golden", copyFixture(t))
		s.openDoc(h.Context(), hubTS)
		for _, tab := range []struct {
			name string
			pane int
		}{{"tree", PaneTree}, {"code", PaneCode}, {"term", PaneTerm}} {
			s.setFocus(h.Context(), tab.pane)
			h.Golden("solo_widget_" + tab.name)
		}
	})
	// The Windows screen, pinned on every OS: no pane, the full-screen
	// notice in the terminal cell (the other goldens run with panes on).
	t.Run("windows", func(t *testing.T) {
		t.Cleanup(SetPaneSupportedForTest(false))
		h := uitest.New(t)
		h.Resize(120, 40)
		s := openFolder(t, h, "workspace:golden", copyFixture(t))
		s.openDoc(h.Context(), hubTS)
		h.Golden("solo_windows")
	})
}
