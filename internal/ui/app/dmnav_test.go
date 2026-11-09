package app

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// DM surface (CA 5.3.3 "DM 표면"): DMs always live there, never as tab roots.

// testDMs: channel ids starting with "D" are DMs.
var testDMs dmPred = func(id string) bool { return strings.HasPrefix(id, "D") }

func navDM(n NavState, r ui.Route) NavState {
	n2, _ := n.navigate(ui.NavigateMsg{Route: r}, layout.ModeWidget, testDMs)
	return n2
}

var dmsR = ui.Route{Kind: ui.RouteDMs}

func TestDMOpensOnDMSurface(t *testing.T) {
	n := navDM(newNavState(), ch("C1")) // tabs [home, C1]
	for _, mode := range allModes {
		got, fi := n.navigate(ui.NavigateMsg{Route: ch("D1")}, mode, testDMs)
		if len(got.Tabs) != 2 || got.Tabs[got.Active].Stack[0].Key() != "channel/C1/" {
			t.Fatalf("%s: tabs changed: %+v", mode.ID(), got.Tabs)
		}
		if !got.InDM || got.Front != FrontTab || !reflect.DeepEqual(keysOf(got.DM.Stack), []string{"dms//", "channel/D1/"}) {
			t.Fatalf("%s: DM surface %+v in DM %v", mode.ID(), got.DM, got.InDM)
		}
		if fi != (FocusIntent{Slot: layout.SlotMain, RouteKey: "channel/D1/"}) {
			t.Fatalf("%s: focus %+v", mode.ID(), fi)
		}
		if mode == layout.ModeTooSmall {
			continue
		}
		if main := Project(layoutFor(mode, got), got)[layout.SlotMain]; main.Key() != "channel/D1/" {
			t.Fatalf("%s: main shows %s", mode.ID(), main.Key())
		}
	}
	// another DM replaces the open one above the list; the same DM keeps its stack
	n = navDM(navDM(n, ch("D1")), search("q"))
	if again := navDM(n, ch("D1")); !reflect.DeepEqual(keysOf(again.DM.Stack), []string{"dms//", "channel/D1/", "search//q"}) {
		t.Fatalf("same DM again: %v", keysOf(again.DM.Stack))
	}
	if other := navDM(n, ch("D2")); !reflect.DeepEqual(keysOf(other.DM.Stack), []string{"dms//", "channel/D2/"}) {
		t.Fatalf("other DM: %v", keysOf(other.DM.Stack))
	}
	// a jump target replaces the open DM so the app can send RouteUpdatedMsg
	a := ch("D1")
	a.Anchor = "M1"
	if j := navDM(n, a); j.DM.Stack[1].Anchor != "M1" {
		t.Fatalf("anchor not kept: %+v", j.DM.Stack[1])
	}
	// a channel that is not a DM still opens as a tab and leaves the DM surface
	c := navDM(n, ch("C2"))
	if c.InDM || c.Tabs[c.Active].Stack[0].Key() != "channel/C2/" || c.DM.Stack[1].Key() != "channel/D1/" {
		t.Fatalf("text channel: in DM %v tabs %+v dm %v", c.InDM, c.Tabs, keysOf(c.DM.Stack))
	}
	// RouteDMs shows the list (the open DM and its thread close)
	l, fi := navDM(n, thr("R", "D1")).navigate(ui.NavigateMsg{Route: dmsR}, layout.ModeCommand, testDMs)
	if !l.InDM || len(l.DM.Stack) != 1 || l.DM.Thread != nil || fi.RouteKey != "dms//" {
		t.Fatalf("DM list: %+v focus %+v", l.DM, fi)
	}
}

func TestDMSurfaceKeepsPlaceAcrossTabs(t *testing.T) {
	n := navDM(navDM(newNavState(), ch("C1")), ch("D1")) // 채팅 = C1, DM = D1
	for _, mode := range []layout.Mode{layout.ModeWidget, layout.ModeTiny} {
		c := n.cycleTab(-1, mode) // DM → 채팅
		if c.InDM || c.Front != FrontTab || c.Tabs[c.Active].Stack[0].Key() != "channel/C1/" {
			t.Fatalf("%s: 채팅 lost its place: %+v", mode.ID(), c)
		}
		c = c.cycleTab(1, mode) // 채팅 → DM
		if !c.InDM || c.DM.Stack[len(c.DM.Stack)-1].Key() != "channel/D1/" {
			t.Fatalf("%s: DM lost its place: %+v", mode.ID(), c.DM)
		}
		if c = c.selectTab(0, mode); c.InDM {
			t.Fatalf("%s: click 채팅", mode.ID())
		}
		if c = c.selectTab(1, mode); !c.InDM || c.Front != FrontTab {
			t.Fatalf("%s: click DM", mode.ID())
		}
		if c = c.selectTab(2, mode); c.Front != FrontFeed || !c.InDM {
			t.Fatalf("%s: click 피드 must keep the chat surface (%v)", mode.ID(), c.InDM)
		}
		if c = c.selectTab(4, mode); c.Front != FrontFeed {
			t.Fatalf("%s: out of range", mode.ID())
		}
	}
	// compact: tab roots, then DM, 피드, 알림
	c := n.selectTab(len(n.Tabs), layout.ModeCompact)
	if !c.InDM || c.barPos() != len(n.Tabs) {
		t.Fatalf("compact DM entry: %+v", c)
	}
	if c = c.selectTab(len(n.Tabs)+1, layout.ModeCompact); c.Front != FrontFeed {
		t.Fatalf("compact 피드 entry")
	}
	if c = c.selectTab(1, layout.ModeCompact); c.InDM || c.Active != 1 {
		t.Fatalf("compact C1 entry")
	}
	// toggles return to the chat surface they left
	f, _ := n.toggleFeed(layout.ModeWidget)
	if f, _ = f.toggleFeed(layout.ModeWidget); !f.InDM || f.Front != FrontTab {
		t.Fatal("f f must return to the DM tab")
	}
}

func TestDMThreadStaysOnDMSurface(t *testing.T) {
	n := navDM(navDM(newNavState(), ch("C1")), search("q")) // the 채팅 tab has a stack
	for _, mode := range allModes {
		got, fi := n.navigate(ui.NavigateMsg{Route: thr("R", "D1")}, mode, testDMs)
		if !got.InDM || got.DM.Thread == nil || got.DM.Thread.Key() != "thread/R/D1" ||
			!reflect.DeepEqual(keysOf(got.DM.Stack), []string{"dms//", "channel/D1/"}) {
			t.Fatalf("%s: %+v", mode.ID(), got.DM)
		}
		if !reflect.DeepEqual(activeStack(got), []string{"channel/C1/", "search//q"}) {
			t.Fatalf("%s: the 채팅 tab changed: %v", mode.ID(), activeStack(got))
		}
		if fi.RouteKey != "thread/R/D1" || fi.Slot != threadSlot(mode) {
			t.Fatalf("%s: focus %+v", mode.ID(), fi)
		}
		if !got.LayoutState(mode).ThreadOpen && mode != layout.ModeTooSmall {
			t.Fatalf("%s: thread not open", mode.ID())
		}
	}
	// command: Main = the DM, the thread pane = its thread
	got := navDM(n, thr("R", "D1"))
	pr := Project(layoutFor(layout.ModeCommand, got), got)
	if pr[layout.SlotMain].Key() != "channel/D1/" || pr[layout.SlotThread].Key() != "thread/R/D1" {
		t.Fatalf("command projection %v", pr)
	}
	// single pane: esc = the DM, esc = the list (no thread left behind)
	l := layoutFor(layout.ModeWidget, got)
	if Project(l, got)[layout.SlotMain].Key() != "thread/R/D1" {
		t.Fatal("widget main must show the thread")
	}
	b := got.back(layout.SlotMain, layout.ModeWidget, l)
	if b.DM.Thread != nil || Project(layoutFor(layout.ModeWidget, b), b)[layout.SlotMain].Key() != "channel/D1/" {
		t.Fatalf("esc from the thread: %+v", b.DM)
	}
	b = navDM(b, thr("R", "D1"))
	b.DM.Stack = append(b.DM.Stack, search("x"))
	// standard: back on Main pops the DM to the list and closes its thread
	b = b.back(layout.SlotMain, layout.ModeStandard, layoutFor(layout.ModeStandard, b))
	b = b.back(layout.SlotMain, layout.ModeStandard, layoutFor(layout.ModeStandard, b))
	if len(b.DM.Stack) != 1 || b.DM.Thread != nil {
		t.Fatalf("back to the list: %+v", b.DM)
	}
	// Replace never replaces the open DM
	r, _ := navDM(n, ch("D1")).navigate(ui.NavigateMsg{Route: search("z"), Replace: true}, layout.ModeWidget, testDMs)
	if !reflect.DeepEqual(keysOf(r.DM.Stack), []string{"dms//", "channel/D1/", "search//z"}) {
		t.Fatalf("replace: %v", keysOf(r.DM.Stack))
	}
}

func TestDMHistory(t *testing.T) {
	n := navDM(navDM(newNavState(), ch("C1")), thr("R", "D1"))
	l := n.loc()
	if l.Front != FrontTab || l.Root.Key() != "dms//" || l.Route.Key() != "thread/R/D1" {
		t.Fatalf("loc %+v", l)
	}
	list := navDM(n, dmsR).loc()
	if list.Root.Key() != "dms//" || list.Route.Key() != "dms//" {
		t.Fatalf("list loc %+v", list)
	}
	chat := navDM(n, ch("C1"))
	for _, mode := range allModes {
		r, fi := chat.restore(l, mode)
		if !r.InDM || r.DM.Thread == nil || r.DM.Thread.Key() != "thread/R/D1" || fi.Slot != threadSlot(mode) {
			t.Fatalf("%s: restore thread: %+v %+v", mode.ID(), r.DM, fi)
		}
		r, _ = r.restore(tabLoc(dmsR, ch("D2")), mode)
		if !reflect.DeepEqual(keysOf(r.DM.Stack), []string{"dms//", "channel/D2/"}) || r.DM.Thread != nil {
			t.Fatalf("%s: restore DM: %+v", mode.ID(), r.DM)
		}
		r, _ = r.restore(list, mode)
		if len(r.DM.Stack) != 1 || !r.InDM {
			t.Fatalf("%s: restore list: %+v", mode.ID(), r.DM)
		}
		r, _ = r.restore(tabLoc(ch("C1"), ch("C1")), mode)
		if r.InDM || r.Tabs[r.Active].Stack[0].Key() != "channel/C1/" {
			t.Fatalf("%s: restore the 채팅 tab: %+v", mode.ID(), r)
		}
	}
}

func TestLiftDMs(t *testing.T) {
	// tabs restored from state.json before the Store knew D1 / D2 were DMs
	n := nav(nav(nav(nav(newNavState(), ch("D2")), ch("C1")), ch("D1")), thr("R", "D1"))
	n = nav(n, search("q")) // on D1's tab
	got := n.liftDMs(testDMs)
	for _, tb := range got.Tabs {
		if strings.HasPrefix(tb.Stack[0].ID, "D") {
			t.Fatalf("DM tab left: %+v", got.Tabs)
		}
	}
	if len(got.Tabs) != 2 || !got.InDM || got.DM.Thread == nil || got.DM.Thread.Key() != "thread/R/D1" ||
		!reflect.DeepEqual(keysOf(got.DM.Stack), []string{"dms//", "channel/D1/", "search//q"}) {
		t.Fatalf("lift: %+v", got)
	}
	if got.Tabs[got.Active].Stack[0].Key() != "channel/C1/" {
		t.Fatalf("active tab = most recently seen: %+v", got.Tabs[got.Active])
	}
	// an inactive DM tab just closes
	m := nav(nav(newNavState(), ch("D1")), ch("C1")).liftDMs(testDMs)
	if m.InDM || len(m.Tabs) != 2 || m.Tabs[m.Active].Stack[0].Key() != "channel/C1/" {
		t.Fatalf("inactive lift: %+v", m)
	}
	if !reflect.DeepEqual(nav(newNavState(), ch("C1")).liftDMs(nil), nav(newNavState(), ch("C1"))) {
		t.Fatal("nothing to lift must change nothing")
	}
}

func TestDMSavedFront(t *testing.T) {
	m := &Model{nav: navDM(newNavState(), ch("D1"))}
	m.ctx = &ui.Context{Store: store.New()}
	s := m.savedState()
	if s.Front != "dm" {
		t.Fatalf("front %q", s.Front)
	}
	for _, tb := range s.OpenTabs {
		if tb.ID == "D1" {
			t.Fatal("a DM must never be saved as a tab")
		}
	}
	m.restore(s)
	if !m.nav.InDM || m.nav.Front != FrontTab {
		t.Fatalf("restored %+v", m.nav)
	}
}

// TestDMInvariantRandomWalk is TestInvariantRandomWalk with DMs: one key, one place,
// and a DM is never a tab root; the DM surface is the list, then at most one DM.
func TestDMInvariantRandomWalk(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 3))
	ids := []string{"C1", "C2", "C3", "D1", "D2", "D3"}
	kinds := []ui.RouteKind{ui.RouteHome, ui.RouteChannel, ui.RouteThread, ui.RouteSearch, ui.RouteDMs,
		ui.RouteFeed, ui.RoutePost, ui.RouteNotifications, ui.RouteSettings}
	n := newNavState()
	var h history
	for step := range 2000 {
		mode := allModes[rng.IntN(len(allModes))]
		var desc string
		switch op := rng.IntN(12); {
		case op < 6:
			r := ui.Route{Kind: kinds[rng.IntN(len(kinds))], ID: ids[rng.IntN(len(ids))]}
			switch r.Kind {
			case ui.RouteThread:
				r.Arg, r.ID = r.ID, "R"+ids[rng.IntN(len(ids))]
			case ui.RouteFeed:
				r.ID, r.Arg = "", "following"
			case ui.RouteHome, ui.RouteNotifications, ui.RouteDMs, ui.RouteSettings:
				r.ID = ""
			}
			m := ui.NavigateMsg{Route: r, Target: ui.Target(rng.IntN(3)), Replace: rng.IntN(4) == 0}
			desc = fmt.Sprintf("navigate %+v", m)
			n, _ = n.navigate(m, mode, testDMs)
		case op < 8:
			s := []layout.SlotID{layout.SlotMain, layout.SlotThread, layout.SlotFeed}[rng.IntN(3)]
			desc = "back " + string(s)
			n = n.back(s, mode, layoutFor(mode, n))
		case op < 10:
			d := rng.IntN(5) - 2
			desc = fmt.Sprintf("cycleTab %d", d)
			n = n.cycleTab(d, mode)
		default:
			if i := h.target(-1, n.loc(), always); i >= 0 {
				h.cur = i
				desc = fmt.Sprintf("history %+v", h.locs[i])
				n, _ = n.restore(h.locs[i], mode)
			}
		}
		h.note(n.loc())
		where := fmt.Sprintf("step %d (%s in %s)", step, desc, mode.ID())
		checkInvariants(t, where, n)
		for i, tb := range n.Tabs {
			if r := tb.Stack[0]; r.Kind == ui.RouteChannel && testDMs.is(r.ID) {
				t.Errorf("%s: tab %d is DM %s", where, i, r.Key())
			}
		}
		d := n.DM
		if d.Stack[0].Kind != ui.RouteDMs {
			t.Errorf("%s: DM root %v", where, d.Stack)
		}
		for j, r := range d.Stack[1:] {
			switch r.Kind {
			case ui.RouteChannel:
				if j != 0 {
					t.Errorf("%s: a DM above the open one %v", where, keysOf(d.Stack))
				}
			case ui.RouteHome, ui.RouteDMs, ui.RouteThread, ui.RouteFeed, ui.RouteNotifications:
				t.Errorf("%s: DM stack holds %s", where, r.Key())
			}
			if r.Kind == ui.RouteChannel && !testDMs.is(r.ID) {
				t.Errorf("%s: text channel on the DM surface %v", where, keysOf(d.Stack))
			}
		}
		if d.Thread != nil && (len(d.Stack) < 2 || d.Stack[1].ID != d.Thread.Arg) {
			t.Errorf("%s: DM thread %v stack %v", where, d.Thread, keysOf(d.Stack))
		}
		if t.Failed() {
			t.Fatalf("state: %+v", n)
		}
	}
}
