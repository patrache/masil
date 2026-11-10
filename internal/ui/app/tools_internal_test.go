package app

import (
	"fmt"
	"maps"
	"reflect"
	"testing"

	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
)

// Zero-value new input = old output: with the new NavState fields at their zero
// value (no tools, ToolActive 0, ToolZoom nil) LayoutState and Project give what
// they gave before the tool surface existed, for every mode and many states.
func TestEmptyToolSurfaceIsOldProjection(t *testing.T) {
	states := []NavState{newNavState()}
	n := newNavState()
	for _, r := range []ui.Route{ch("c1"), thr("m1", "c1"), post("p1"), feedR("discover"), ch("c2"), search("x"), NotifRoute} {
		n, _ = n.navigate(ui.NavigateMsg{Route: r}, layout.ModeStandard, nil)
		states = append(states, n)
	}
	for _, s := range states {
		for _, mode := range allModes {
			sz := modeFirstSize[mode]
			st := s.LayoutState(mode)
			if st.ToolOpen || st.Zoomed {
				t.Errorf("%v: LayoutState = %+v", mode, st)
			}
			old := layout.State{ThreadOpen: st.ThreadOpen, FeedSwapped: st.FeedSwapped}
			if st != old {
				t.Errorf("%v: LayoutState %+v has new fields set", mode, st)
			}
			l := layout.Compute(mode, sz[0], sz[1], st)
			got := Project(l, s)
			if _, ok := l.Pane(layout.SlotTool); ok {
				t.Errorf("%v: SlotTool laid out with no tool", mode)
			}
			for slot, r := range got {
				if r.Kind == ui.RouteTool || r.Kind == ui.RouteTools {
					t.Errorf("%v: %s = %+v", mode, slot, r)
				}
			}
			if !maps.Equal(projKeys(got), projKeys(Project(l, s.clone()))) {
				t.Errorf("%v: clone changed the projection", mode)
			}
		}
	}
}

func projKeys(m map[layout.SlotID]ui.Route) map[layout.SlotID]string {
	out := map[layout.SlotID]string{}
	for k, v := range m {
		out[k] = v.Key()
	}
	return out
}

// A FrontTools state without tools (a hand-built or stale state) is repaired to
// the chat surface.
func TestFrontToolsWithoutToolsRepaired(t *testing.T) {
	n := newNavState()
	n.Front = FrontTools
	if c := n.clone(); c.Front != FrontTab {
		t.Errorf("clone front = %v, want FrontTab", c.Front)
	}
	n.Tools = []ui.Route{{Kind: ui.RouteTools}}
	n.ToolActive = 7
	if c := n.clone(); c.Front != FrontTools || c.ToolActive != 0 {
		t.Errorf("clone = front %v active %d, want FrontTools 0", c.Front, c.ToolActive)
	}
}

// NavState.Keys lists the tool surface after everything it listed before.
func TestKeysIncludeTools(t *testing.T) {
	n := newNavState()
	old := n.Keys()
	n.Tools = []ui.Route{{Kind: ui.RouteTools}, {Kind: ui.RouteTool, ID: "g1", Arg: ui.ToolGit}}
	got := n.Keys()
	if len(got) != len(old)+2 || got[len(old)] != "tools//" || got[len(old)+1] != "tool/g1/git" {
		t.Errorf("Keys = %v (before %v)", got, old)
	}
}

// The help lists ctrl+t and ctrl+w z only while a tool flag is on; off, the rows
// are exactly the 0.1.2 help.
func TestHelpToolRowsOnlyWithFlags(t *testing.T) {
	ctx := &ui.Context{Keys: keymap.Default(), T: i18n.KoT()}
	base := helpRows(ctx.Keys)
	t.Setenv("MASIL_FEATURES", "")
	if got := withToolHelp(ctx, base); !reflect.DeepEqual(got, base) {
		t.Fatal("flags off: the help rows changed")
	}
	t.Setenv("MASIL_FEATURES", "git")
	got := withToolHelp(ctx, base)
	if len(got) != len(base)+4 {
		t.Fatalf("flags on: %d rows, want %d", len(got), len(base)+4)
	}
	tail := got[len(base):]
	if tail[1].text != "도구" || tail[2].key != "ctrl+t" || tail[2].text != "도구 허브 ↔ 채팅 · 패널에서 나가기" ||
		tail[3].key != "ctrl+w z" || tail[3].text != "도구 넓게 보기" {
		t.Errorf("tool rows = %+v", tail)
	}
	for i, r := range got {
		if r.id != fmt.Sprint(i+1) {
			t.Fatalf("row %d id = %q", i, r.id)
		}
	}
}
