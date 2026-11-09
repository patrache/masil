package app_test

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/stub"
)

// dragProbe is a pane that follows drags (ui.DragHandler) and records them.
type dragProbe struct {
	probePane
	drags   []stub.MouseCall
	cleared int
}

func (p *dragProbe) HandleDrag(_ *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	p.drags = append(p.drags, stub.MouseCall{Msg: msg, Local: local})
	return nil
}

func (p *dragProbe) ClearDrag() { p.cleared++ }

// TestDragRouting (CA 5.3.4 mouse): after a left press in a DragHandler pane,
// every motion and the release go to that pane, wherever the pointer is
// (inner-rect coordinates, not clipped); nothing goes anywhere without a
// press, and a press elsewhere clears the pane's selection.
func TestDragRouting(t *testing.T) {
	t.Cleanup(ui.SnapshotRegistryForTest())
	side := &dragProbe{}
	ui.RegisterPane(layout.SlotSidebar, func(*ui.Context) ui.Component { return side })

	h := uitest.New(t, uitest.WithRoute(devRoute))
	h.Resize(200, 60)
	sp, _ := h.Model.Frame().Layout.Pane(layout.SlotSidebar)
	in := sp.Rect.Inner()

	// motion / release without a press: dropped
	h.Send(tea.MouseMotionMsg{X: in.X + 1, Y: in.Y + 1, Button: tea.MouseLeft})
	h.Send(tea.MouseReleaseMsg{X: in.X + 1, Y: in.Y + 1, Button: tea.MouseLeft})
	if len(side.drags) != 0 {
		t.Fatalf("no press, yet HandleDrag got %+v", side.drags)
	}

	h.Send(tea.MouseClickMsg{X: in.X + 2, Y: in.Y + 3, Button: tea.MouseLeft})
	if len(side.mouse) != 1 {
		t.Fatalf("the press goes to HandleMouse: %+v", side.mouse)
	}
	h.Send(tea.MouseMotionMsg{X: in.X + 2, Y: in.Y + 4, Button: tea.MouseLeft})
	h.Send(tea.MouseMotionMsg{X: in.X + in.W + 30, Y: in.Y + in.H + 2, Button: tea.MouseLeft}) // off the pane
	h.Send(tea.MouseMotionMsg{X: in.X - 1, Y: in.Y - 1, Button: tea.MouseLeft})                // on the border
	h.Send(tea.MouseReleaseMsg{X: in.X + 1, Y: in.Y + 1, Button: tea.MouseNone})               // X10 release
	want := []ui.Point{{X: 2, Y: 4}, {X: in.W + 30, Y: in.H + 2}, {X: -1, Y: -1}, {X: 1, Y: 1}}
	if len(side.drags) != len(want) {
		t.Fatalf("HandleDrag got %+v", side.drags)
	}
	for i, w := range want {
		if side.drags[i].Local != w {
			t.Fatalf("drag %d at %+v, want %+v", i, side.drags[i].Local, w)
		}
	}
	if _, ok := side.drags[3].Msg.(tea.MouseReleaseMsg); !ok {
		t.Fatalf("last drag call is %T, want the release", side.drags[3].Msg)
	}
	if len(side.mouse) != 1 {
		t.Fatalf("motion and release never reach HandleMouse: %+v", side.mouse)
	}

	// after the release nothing is grabbed
	h.Send(tea.MouseMotionMsg{X: in.X + 2, Y: in.Y + 5, Button: tea.MouseLeft})
	if len(side.drags) != len(want) {
		t.Fatal("motion after the release")
	}

	// a press in another pane clears the sidebar's selection; one in it does not
	if side.cleared != 0 {
		t.Fatalf("cleared %d times before any press elsewhere", side.cleared)
	}
	h.Click(in.X+1, in.Y+1)
	if side.cleared != 0 {
		t.Fatal("a press in the same pane is the pane's own business")
	}
	mp, _ := h.Model.Frame().Layout.Pane(layout.SlotMain)
	h.Click(mp.Rect.X+5, mp.Rect.Y+5)
	if side.cleared != 1 {
		t.Fatalf("a press elsewhere clears the selection: cleared %d", side.cleared)
	}
	h.Click(mp.Rect.X+6, mp.Rect.Y+5)
	if side.cleared != 1 {
		t.Fatal("cleared once, not on every later press")
	}

	// an overlay opening mid-drag ends the drag
	h.Send(tea.MouseClickMsg{X: in.X + 2, Y: in.Y + 3, Button: tea.MouseLeft})
	n := len(side.drags)
	h.Press("ctrl+k")
	h.Send(tea.MouseMotionMsg{X: in.X + 2, Y: in.Y + 6, Button: tea.MouseLeft})
	if len(side.drags) != n || side.cleared != 2 {
		t.Fatalf("drag continued under an overlay: drags %d->%d cleared %d", n, len(side.drags), side.cleared)
	}
}
