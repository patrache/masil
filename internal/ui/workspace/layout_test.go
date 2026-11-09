package workspace

import (
	"testing"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

func TestPlanLayout(t *testing.T) {
	p := planLayout(layout.ModeStandard, ui.Size{W: 120, H: 38}, true, false, false, PaneCode)
	if p.Tree.W != 26 || p.Code.W != 94 || p.Band.H != 1 || p.Tiny {
		t.Fatalf("standard zoomed = %+v", p)
	}
	if want := (38 - 1) * 55 / 100; p.Code.H < want-1 || p.Code.H > want+1 {
		t.Errorf("code height %d, want about %d", p.Code.H, want)
	}
	if p.Term.Y != p.Code.Y+p.Code.H || p.Term.X != p.Code.X || p.Term.Y+p.Term.H != 38 {
		t.Errorf("terminal not below code: %+v / %+v", p.Code, p.Term)
	}

	p = planLayout(layout.ModeStandard, ui.Size{W: 94, H: 38}, false, false, false, PaneCode)
	if p.Tree.W != 22 || p.Tree.W+p.Code.W != 94 {
		t.Errorf("standard = %+v", p)
	}
	p = planLayout(layout.ModeCompact, ui.Size{W: 94, H: 28}, false, false, false, PaneTree)
	if p.Tree.W != 22 || p.Tree.W+p.Code.W != 94 {
		t.Errorf("compact = %+v", p)
	}

	p = planLayout(layout.ModeWidget, ui.Size{W: 80, H: 20}, true, false, false, PaneTerm)
	if p.SubTabs.H != 1 || p.Single != PaneTerm || p.Term.W != 80 || p.Code.W != 0 || p.Tree.W != 0 {
		t.Errorf("widget = %+v", p)
	}
	if p := planLayout(layout.ModeTiny, ui.Size{W: 40, H: 12}, true, true, false, PaneCode); !p.Tiny {
		t.Errorf("tiny = %+v", p)
	}

	p = planLayout(layout.ModeCommand, ui.Size{W: 90, H: 58}, false, true, false, PaneCode)
	if p.People.W != 24 || p.Tree.W+p.Code.W+p.People.W != 90 {
		t.Errorf("command sharing = %+v", p)
	}
	p = planLayout(layout.ModeCommand, ui.Size{W: 200, H: 58}, true, true, false, PaneCode)
	if p.People.W != 24 || p.Code.Y != p.Term.Y || p.Code.X+p.Code.W != p.Term.X {
		t.Errorf("command wide sharing = %+v (tree | code | terminal side by side)", p)
	}
	if p := planLayout(layout.ModeCommand, ui.Size{W: 200, H: 58}, true, false, false, PaneCode); p.People.W != 0 {
		t.Errorf("command solo has a people cell: %+v", p)
	}
}
