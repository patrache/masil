package layout

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/patrache/masil/internal/config"
)

type sz struct{ w, h int }

func TestAutoTable(t *testing.T) {
	bp := DefaultBreakpoints()
	cases := map[Mode][]sz{
		ModeCommand:  {{200, 60}, {200, 50}},
		ModeStandard: {{200, 49}, {199, 50}, {120, 40}, {100, 40}},
		ModeCompact:  {{120, 30}, {100, 39}, {199, 39}, {160, 16}},
		ModeWidget:   {{80, 24}, {99, 60}, {60, 60}, {44, 24}},
		ModeTiny:     {{50, 20}, {99, 23}, {43, 40}, {120, 15}, {44, 16}},
		ModeTooSmall: {{19, 10}, {30, 5}},
	}
	for want, sizes := range cases {
		for _, s := range sizes {
			if got := Auto(s.w, s.h, bp); got != want {
				t.Errorf("Auto(%d,%d) = %s, want %s", s.w, s.h, got.ID(), want.ID())
			}
		}
	}
	// Negative and zero sizes are too small.
	for _, s := range []sz{{0, 0}, {-5, -5}, {200, -1}, {-1, 60}} {
		if got := Auto(s.w, s.h, bp); got != ModeTooSmall {
			t.Errorf("Auto(%d,%d) = %s, want too_small", s.w, s.h, got.ID())
		}
	}
}

func TestDefaultBreakpointsAndFromConfig(t *testing.T) {
	want := Breakpoints{
		CommandMinCols: 200, CommandMinRows: 50,
		StandardMinCols: 100, StandardMinRows: 40,
		TinyMaxCols: 43, TinyMaxRows: 15,
		WidgetMinRows: 24,
		MinCols:       20, MinRows: 6,
	}
	if got := DefaultBreakpoints(); got != want {
		t.Fatalf("DefaultBreakpoints() = %+v, want %+v", got, want)
	}
	if got := FromConfig(config.Defaults().Layout); got != want {
		t.Fatalf("FromConfig(default) = %+v, want %+v", got, want)
	}
	// Zero/negative values fall back to defaults; MinCols/MinRows are fixed.
	if got := FromConfig(config.LayoutConfig{CommandMinCols: -1}); got != want {
		t.Fatalf("FromConfig(zero) = %+v, want %+v", got, want)
	}
	c := config.LayoutConfig{
		CommandMinCols: 220, CommandMinRows: 55,
		StandardMinCols: 110, StandardMinRows: 42,
		TinyMaxCols: 50, TinyMaxRows: 18, WidgetMinRows: 26,
	}
	got := FromConfig(c)
	want2 := Breakpoints{220, 55, 110, 42, 50, 18, 26, 20, 6}
	if got != want2 {
		t.Fatalf("FromConfig(custom) = %+v, want %+v", got, want2)
	}
	if m := Auto(200, 60, got); m != ModeStandard {
		t.Fatalf("custom Auto(200,60) = %s, want standard", m.ID())
	}
}

func TestAutoNeverBelowPinMin(t *testing.T) {
	// Even with odd custom breakpoints, Auto never picks a mode whose minimum size is not met.
	bp := Breakpoints{
		CommandMinCols: 30, CommandMinRows: 8,
		StandardMinCols: 25, StandardMinRows: 7,
		TinyMaxCols: 21, TinyMaxRows: 6, WidgetMinRows: 7,
		MinCols: 20, MinRows: 6,
	}
	for c := 0; c <= 220; c++ {
		for r := 0; r <= 70; r++ {
			m := Auto(c, r, bp)
			if m == ModeTooSmall {
				continue
			}
			pc, pr := PinMinSize(m)
			if c < pc || r < pr {
				t.Fatalf("Auto(%d,%d) = %s below its minimum %dx%d", c, r, m.ID(), pc, pr)
			}
		}
	}
}

func TestResolvePin(t *testing.T) {
	bp := DefaultBreakpoints()
	pin := func(m Mode) *Mode { return &m }

	if m, fb := Resolve(160, 40, bp, pin(ModeCommand)); m != ModeCommand || fb {
		t.Errorf("pin command 160x40 = %s,%v", m.ID(), fb)
	}
	if m, fb := Resolve(159, 40, bp, pin(ModeCommand)); m != ModeStandard || !fb {
		t.Errorf("pin command 159x40 = %s,%v, want standard,true", m.ID(), fb)
	}
	if m, fb := Resolve(160, 39, bp, pin(ModeCommand)); m != ModeCompact || !fb {
		t.Errorf("pin command 160x39 = %s,%v, want compact,true", m.ID(), fb)
	}
	if m, fb := Resolve(200, 60, bp, pin(ModeTiny)); m != ModeTiny || fb {
		t.Errorf("pin tiny 200x60 = %s,%v", m.ID(), fb)
	}
	if m, fb := Resolve(19, 10, bp, pin(ModeTiny)); m != ModeTooSmall || !fb {
		t.Errorf("pin tiny 19x10 = %s,%v, want too_small,true", m.ID(), fb)
	}
	if m, fb := Resolve(120, 40, bp, nil); m != ModeStandard || fb {
		t.Errorf("pin nil 120x40 = %s,%v", m.ID(), fb)
	}
	// A too_small or out-of-range pin is ignored (treated as no pin).
	if m, fb := Resolve(120, 40, bp, pin(ModeTooSmall)); m != ModeStandard || fb {
		t.Errorf("pin too_small 120x40 = %s,%v", m.ID(), fb)
	}
	if m, fb := Resolve(120, 40, bp, pin(Mode(99))); m != ModeStandard || fb {
		t.Errorf("pin 99 120x40 = %s,%v", m.ID(), fb)
	}

	mins := map[Mode]sz{
		ModeCommand: {160, 40}, ModeStandard: {80, 24}, ModeCompact: {60, 16},
		ModeWidget: {44, 16}, ModeTiny: {20, 6},
	}
	for m, want := range mins {
		c, r := PinMinSize(m)
		if c != want.w || r != want.h {
			t.Errorf("PinMinSize(%s) = %dx%d, want %dx%d", m.ID(), c, r, want.w, want.h)
		}
		// Exactly at the minimum applies; one below on either axis falls back.
		if got, fb := Resolve(want.w, want.h, bp, pin(m)); got != m || fb {
			t.Errorf("pin %s at min = %s,%v", m.ID(), got.ID(), fb)
		}
		if got, fb := Resolve(want.w-1, want.h, bp, pin(m)); fb != true || got != Auto(want.w-1, want.h, bp) {
			t.Errorf("pin %s below cols = %s,%v", m.ID(), got.ID(), fb)
		}
		if got, fb := Resolve(want.w, want.h-1, bp, pin(m)); fb != true || got != Auto(want.w, want.h-1, bp) {
			t.Errorf("pin %s below rows = %s,%v", m.ID(), got.ID(), fb)
		}
	}
}

func TestLabelsIDs(t *testing.T) {
	cases := []struct {
		m         Mode
		label, id string
	}{
		{ModeCommand, "지휘소", "command"},
		{ModeStandard, "표준", "standard"},
		{ModeCompact, "컴팩트", "compact"},
		{ModeWidget, "위젯", "widget"},
		{ModeTiny, "초소형", "tiny"},
		{ModeTooSmall, "—", "too_small"},
	}
	for _, c := range cases {
		if got := c.m.Label(); got != c.label {
			t.Errorf("%d.Label() = %q, want %q", c.m, got, c.label)
		}
		if got := c.m.ID(); got != c.id {
			t.Errorf("%d.ID() = %q, want %q", c.m, got, c.id)
		}
		if m, ok := ParseMode(c.id); !ok || m != c.m {
			t.Errorf("ParseMode(%q) = %d,%v", c.id, m, ok)
		}
	}
	for _, s := range []string{"auto", "", "bogus", "지휘소"} {
		if m, ok := ParseMode(s); ok || m != 0 {
			t.Errorf("ParseMode(%q) = %d,%v, want 0,false", s, m, ok)
		}
	}
	if got := Mode(42).Label(); got != "—" {
		t.Errorf("unknown Label = %q", got)
	}
}

func TestRect(t *testing.T) {
	r := Rect{2, 3, 4, 5}
	if !r.Contains(2, 3) || !r.Contains(5, 7) || r.Contains(6, 3) || r.Contains(2, 8) || r.Contains(1, 3) {
		t.Fatal("Contains edges wrong")
	}
	if (Rect{0, 0, 0, 0}).Contains(0, 0) {
		t.Fatal("empty rect contains a point")
	}
	if got := r.Inner(); got != (Rect{3, 4, 2, 3}) {
		t.Fatalf("Inner = %+v", got)
	}
	if got := (Rect{1, 1, 1, 1}).Inner(); got.W != 0 || got.H != 0 {
		t.Fatalf("Inner of 1x1 = %+v, want zero size", got)
	}
}

func p(s SlotID, x, y, w, h int, b bool) Pane {
	return Pane{Slot: s, Rect: Rect{x, y, w, h}, Bordered: b}
}

func mustPanes(t *testing.T, l Layout, want []Pane) {
	t.Helper()
	if !reflect.DeepEqual(l.Panes, want) {
		t.Fatalf("panes:\n got %+v\nwant %+v", l.Panes, want)
	}
}

func TestComputeCommand200x60(t *testing.T) {
	l := Compute(ModeCommand, 200, 60, State{})
	if l.Mode != ModeCommand || l.Cols != 200 || l.Rows != 60 {
		t.Fatalf("header = %+v", l)
	}
	mustPanes(t, l, []Pane{
		p(SlotRail, 0, 0, 6, 59, true),
		p(SlotSidebar, 6, 0, 26, 59, true),
		p(SlotMain, 32, 0, 78, 59, true),
		p(SlotFeed, 110, 0, 56, 59, true),
		p(SlotMembers, 166, 0, 34, 32, true),
		p(SlotNotifications, 166, 32, 34, 27, true),
		p(SlotStatus, 0, 59, 200, 1, false),
	})
	l = Compute(ModeCommand, 200, 60, State{ThreadOpen: true})
	if _, ok := l.Pane(SlotMembers); ok {
		t.Fatal("members shown while thread open")
	}
	th, ok := l.Pane(SlotThread)
	if !ok || th.Rect != (Rect{166, 0, 34, 32}) || !th.Bordered {
		t.Fatalf("thread = %+v,%v", th, ok)
	}
	if pn, ok := l.At(170, 40); !ok || pn.Slot != SlotNotifications {
		t.Fatalf("At(170,40) = %+v,%v", pn, ok)
	}
	if pn, ok := l.At(0, 59); !ok || pn.Slot != SlotStatus {
		t.Fatalf("At(0,59) = %+v,%v", pn, ok)
	}
	if _, ok := l.At(200, 0); ok {
		t.Fatal("At outside screen found a pane")
	}
}

func TestComputeStandard120x40(t *testing.T) {
	mustPanes(t, Compute(ModeStandard, 120, 40, State{}), []Pane{
		p(SlotSidebar, 0, 0, 26, 39, true),
		p(SlotMain, 26, 0, 94, 39, true),
		p(SlotStatus, 0, 39, 120, 1, false),
	})
	mustPanes(t, Compute(ModeStandard, 120, 40, State{ThreadOpen: true}), []Pane{
		p(SlotSidebar, 0, 0, 26, 39, true),
		p(SlotMain, 26, 0, 94, 23, true),
		p(SlotThread, 26, 23, 94, 16, true),
		p(SlotStatus, 0, 39, 120, 1, false),
	})
	mustPanes(t, Compute(ModeStandard, 120, 40, State{ThreadOpen: true, FeedSwapped: true}), []Pane{
		p(SlotSidebar, 0, 0, 26, 39, true),
		p(SlotMain, 26, 0, 94, 39, true),
		p(SlotStatus, 0, 39, 120, 1, false),
	})
}

func TestComputeStandardShortThreadFolds(t *testing.T) {
	l := Compute(ModeStandard, 80, 24, State{ThreadOpen: true})
	mustPanes(t, l, []Pane{
		p(SlotSidebar, 0, 0, 24, 23, true),
		p(SlotMain, 24, 0, 56, 13, true),
		p(SlotThread, 24, 13, 56, 10, true),
		p(SlotStatus, 0, 23, 80, 1, false),
	})
	l = Compute(ModeStandard, 100, 12, State{ThreadOpen: true})
	mustPanes(t, l, []Pane{
		p(SlotSidebar, 0, 0, 24, 11, true),
		p(SlotMain, 24, 0, 76, 11, true),
		p(SlotStatus, 0, 11, 100, 1, false),
	})
}

func TestComputeCompactWidgetTiny(t *testing.T) {
	mustPanes(t, Compute(ModeCompact, 120, 30, State{ThreadOpen: true}), []Pane{
		p(SlotTabs, 0, 0, 120, 1, false),
		p(SlotMain, 0, 1, 120, 28, true),
		p(SlotStatus, 0, 29, 120, 1, false),
	})
	mustPanes(t, Compute(ModeWidget, 80, 24, State{}), []Pane{
		p(SlotTabs, 0, 0, 80, 1, false),
		p(SlotContext, 0, 1, 80, 1, false),
		p(SlotMain, 0, 2, 80, 21, true),
		p(SlotStatus, 0, 23, 80, 1, false),
	})
	mustPanes(t, Compute(ModeTiny, 50, 20, State{}), []Pane{
		p(SlotTabs, 0, 0, 50, 1, false),
		p(SlotMain, 0, 1, 50, 18, false),
		p(SlotStatus, 0, 19, 50, 1, false),
	})
}

func TestComputeTooSmall(t *testing.T) {
	for _, m := range []Mode{ModeTooSmall, ModeCommand, ModeTiny} {
		l := Compute(m, 19, 10, State{})
		if l.Mode != ModeTooSmall {
			t.Errorf("Compute(%s,19,10).Mode = %s", m.ID(), l.Mode.ID())
		}
		mustPanes(t, l, []Pane{p(SlotMain, 0, 0, 19, 10, false)})
	}
	mustPanes(t, Compute(ModeTooSmall, 200, 60, State{}), []Pane{p(SlotMain, 0, 0, 200, 60, false)})
	for _, s := range []sz{{0, 0}, {-5, -5}, {0, 10}, {10, 0}, {-3, 40}} {
		l := Compute(ModeTooSmall, s.w, s.h, State{})
		if len(l.Panes) != 0 {
			t.Errorf("Compute(%dx%d) panes = %+v, want none", s.w, s.h, l.Panes)
		}
		if l.Cols < 0 || l.Rows < 0 {
			t.Errorf("negative size kept: %+v", l)
		}
	}
}

func TestComputeAnyModeAnySizeNoPanic(t *testing.T) {
	// Out-of-domain combos (mode below its minimum size, bogus modes) must not panic
	// and must still tile the screen without overlaps.
	for m := Mode(-1); m <= ModeCommand+1; m++ {
		for c := -2; c <= 70; c++ {
			for r := -2; r <= 30; r++ {
				for _, st := range allStates {
					l := Compute(m, c, r, st)
					if err := checkTiling(l, false); err != nil {
						t.Fatalf("Compute(%d,%d,%d,%+v): %v", m, c, r, st, err)
					}
				}
			}
		}
	}
}

var allStates = []State{{}, {ThreadOpen: true}, {FeedSwapped: true}, {ThreadOpen: true, FeedSwapped: true}}

// checkTiling verifies panes are disjoint, non-empty, inside the screen and cover it exactly.
// When strict, bordered panes must have a >=1x1 inner rect.
func checkTiling(l Layout, strict bool) error {
	cols, rows := max(l.Cols, 0), max(l.Rows, 0)
	if cols == 0 || rows == 0 {
		if len(l.Panes) != 0 {
			return fmt.Errorf("empty screen has panes %+v", l.Panes)
		}
		return nil
	}
	area := 0
	for i, a := range l.Panes {
		r := a.Rect
		if r.W < 1 || r.H < 1 {
			return fmt.Errorf("pane %s empty: %+v", a.Slot, r)
		}
		if r.X < 0 || r.Y < 0 || r.X+r.W > cols || r.Y+r.H > rows {
			return fmt.Errorf("pane %s outside screen: %+v", a.Slot, r)
		}
		if strict && a.Bordered {
			in := r.Inner()
			if in.W < 1 || in.H < 1 {
				return fmt.Errorf("pane %s bordered with inner %+v", a.Slot, in)
			}
		}
		for _, b := range l.Panes[i+1:] {
			if overlaps(r, b.Rect) {
				return fmt.Errorf("panes %s %+v and %s %+v overlap", a.Slot, r, b.Slot, b.Rect)
			}
			if a.Slot == b.Slot {
				return fmt.Errorf("slot %s placed twice", a.Slot)
			}
		}
		area += r.W * r.H
	}
	if area != cols*rows {
		return fmt.Errorf("union area %d != %d", area, cols*rows)
	}
	return nil
}

func overlaps(a, b Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

func TestComputeExhaustive(t *testing.T) {
	bp := DefaultBreakpoints()
	step := 1
	if testing.Short() {
		step = 3
	}
	for _, m := range []Mode{ModeTooSmall, ModeTiny, ModeWidget, ModeCompact, ModeStandard, ModeCommand} {
		t.Run(m.ID(), func(t *testing.T) {
			t.Parallel()
			pinM := m
			pc, pr := PinMinSize(m)
			checked := 0
			for c := 0; c <= 320; c += step {
				for r := 0; r <= 100; r += step {
					auto := Auto(c, r, bp)
					pinned := m != ModeTooSmall && c >= pc && r >= pr
					if auto != m && !pinned {
						continue
					}
					if pinned {
						if got, fb := Resolve(c, r, bp, &pinM); got != m || fb {
							t.Fatalf("Resolve pin %s at %dx%d = %s,%v", m.ID(), c, r, got.ID(), fb)
						}
					}
					for _, st := range allStates {
						l := Compute(m, c, r, st)
						checked++
						if err := checkTiling(l, true); err != nil {
							t.Fatalf("Compute(%s,%d,%d,%+v): %v", m.ID(), c, r, st, err)
						}
						if c > 0 && r > 0 {
							checkFormula(t, m, c, r, st, l)
						}
					}
				}
			}
			if checked == 0 {
				t.Fatal("no sizes checked")
			}
		})
	}
}

// checkFormula checks mode-specific shape facts beyond tiling.
func checkFormula(t *testing.T, m Mode, c, r int, st State, l Layout) {
	t.Helper()
	need := func(s SlotID) Pane {
		t.Helper()
		pn, ok := l.Pane(s)
		if !ok {
			t.Fatalf("Compute(%s,%d,%d,%+v): no %s pane", m.ID(), c, r, st, s)
		}
		return pn
	}
	if l.Mode != m {
		t.Fatalf("Compute(%s,%d,%d).Mode = %s", m.ID(), c, r, l.Mode.ID())
	}
	if m == ModeTooSmall {
		mn := need(SlotMain)
		if len(l.Panes) != 1 || mn.Bordered {
			t.Fatalf("too_small %dx%d panes = %+v", c, r, l.Panes)
		}
		return
	}
	stp := need(SlotStatus)
	if stp.Rect != (Rect{0, r - 1, c, 1}) || stp.Bordered {
		t.Fatalf("%s %dx%d status = %+v", m.ID(), c, r, stp)
	}
	switch m {
	case ModeCommand:
		if need(SlotMain).Rect.W <= 0 || need(SlotFeed).Rect.W <= 0 {
			t.Fatalf("command %dx%d chat/feed width zero", c, r)
		}
		need(SlotRail)
		need(SlotSidebar)
		need(SlotNotifications)
		if st.ThreadOpen {
			need(SlotThread)
		} else {
			need(SlotMembers)
		}
	case ModeStandard:
		need(SlotSidebar)
		if need(SlotMain).Rect.W <= 0 {
			t.Fatalf("standard %dx%d main width zero", c, r)
		}
		if _, ok := l.Pane(SlotThread); ok && (st.FeedSwapped || !st.ThreadOpen) {
			t.Fatalf("standard %dx%d %+v has thread pane", c, r, st)
		}
	case ModeCompact, ModeWidget, ModeTiny:
		need(SlotTabs)
		mn := need(SlotMain)
		if mn.Bordered == (m == ModeTiny) {
			t.Fatalf("%s main bordered = %v", m.ID(), mn.Bordered)
		}
		if m == ModeWidget {
			need(SlotContext)
		}
	}
	for _, pn := range l.Panes {
		if pn.Rect.H > 0 && pn.Rect.W > 0 {
			if got, ok := l.At(pn.Rect.X, pn.Rect.Y); !ok || got.Slot != pn.Slot {
				t.Fatalf("At(%d,%d) = %+v, want %s", pn.Rect.X, pn.Rect.Y, got, pn.Slot)
			}
		}
	}
}

func TestComputeToolOpenCommand(t *testing.T) {
	l := Compute(ModeCommand, 200, 60, State{ToolOpen: true})
	mustPanes(t, l, []Pane{
		p(SlotRail, 0, 0, 6, 59, true),
		p(SlotSidebar, 6, 0, 26, 59, true),
		p(SlotMain, 32, 0, 78, 59, true),
		p(SlotTool, 110, 0, 90, 59, true),
		p(SlotStatus, 0, 59, 200, 1, false),
	})
	for _, s := range []SlotID{SlotFeed, SlotThread, SlotMembers, SlotNotifications} {
		if _, ok := l.Pane(s); ok {
			t.Fatalf("%s shown while the tool surface is open", s)
		}
	}
	// An open thread does not bring the right column back.
	l = Compute(ModeCommand, 200, 60, State{ToolOpen: true, ThreadOpen: true})
	if _, ok := l.Pane(SlotThread); ok {
		t.Fatal("thread shown next to the tool surface")
	}
	if err := checkTiling(l, true); err != nil {
		t.Fatal(err)
	}
}

func TestComputeZoomed(t *testing.T) {
	// Command: the tool covers chat + feed + right column.
	l := Compute(ModeCommand, 200, 60, State{ToolOpen: true, Zoomed: true})
	mustPanes(t, l, []Pane{
		p(SlotRail, 0, 0, 6, 59, true),
		p(SlotSidebar, 6, 0, 26, 59, true),
		p(SlotTool, 32, 0, 168, 59, true),
		p(SlotStatus, 0, 59, 200, 1, false),
	})
	if _, ok := l.Pane(SlotMain); ok {
		t.Fatal("main shown while zoomed")
	}

	// Standard: Main covers the sidebar.
	mustPanes(t, Compute(ModeStandard, 120, 40, State{Zoomed: true}), []Pane{
		p(SlotMain, 0, 0, 120, 39, true),
		p(SlotStatus, 0, 39, 120, 1, false),
	})
	if l := Compute(ModeStandard, 120, 40, State{Zoomed: true, ThreadOpen: true}); checkTiling(l, true) != nil {
		t.Fatalf("standard zoomed with thread does not tile: %+v", l.Panes)
	} else if _, ok := l.Pane(SlotSidebar); ok {
		t.Fatal("sidebar shown while zoomed")
	}

	// Standard ToolOpen alone changes nothing (Main is the tool).
	if a, b := Compute(ModeStandard, 120, 40, State{ToolOpen: true}), Compute(ModeStandard, 120, 40, State{}); !samePanes(a, b) {
		t.Fatalf("standard ToolOpen changed the layout:\n%+v\n%+v", a.Panes, b.Panes)
	}

	// Compact, widget, tiny: Main is already full width.
	for _, c := range []struct {
		m    Mode
		w, h int
	}{{ModeCompact, 120, 30}, {ModeWidget, 80, 24}, {ModeTiny, 40, 14}, {ModeTooSmall, 19, 10}} {
		for _, st := range []State{{Zoomed: true}, {ToolOpen: true}, {ToolOpen: true, Zoomed: true}} {
			if a, b := Compute(c.m, c.w, c.h, st), Compute(c.m, c.w, c.h, State{}); !samePanes(a, b) {
				t.Fatalf("%s %+v changed the layout", c.m.ID(), st)
			}
		}
	}
}

func samePanes(a, b Layout) bool {
	if len(a.Panes) != len(b.Panes) {
		return false
	}
	for i := range a.Panes {
		if a.Panes[i] != b.Panes[i] {
			return false
		}
	}
	return true
}

// TestComputeDefaultUnchanged: with ToolOpen and Zoomed false no tool slot
// appears anywhere; the byte-for-byte proof that the old geometry is kept is
// the unchanged TestDiagramGolden set.
func TestComputeDefaultUnchanged(t *testing.T) {
	for m := ModeTooSmall; m <= ModeCommand; m++ {
		for c := 0; c <= 320; c += 7 {
			for r := 0; r <= 100; r += 5 {
				for _, st := range allStates {
					if _, ok := Compute(m, c, r, st).Pane(SlotTool); ok {
						t.Fatalf("Compute(%s,%d,%d,%+v) has a tool slot", m.ID(), c, r, st)
					}
				}
			}
		}
	}
}

var toolStates = []State{
	{ToolOpen: true}, {Zoomed: true}, {ToolOpen: true, Zoomed: true},
	{ToolOpen: true, ThreadOpen: true}, {ToolOpen: true, Zoomed: true, ThreadOpen: true},
	{Zoomed: true, ThreadOpen: true}, {Zoomed: true, FeedSwapped: true},
}

func TestComputeToolStatesTile(t *testing.T) {
	for m := Mode(-1); m <= ModeCommand+1; m++ {
		for c := -2; c <= 330; c += 3 {
			for r := -2; r <= 100; r += 3 {
				for _, st := range toolStates {
					l := Compute(m, c, r, st)
					if err := checkTiling(l, false); err != nil {
						t.Fatalf("Compute(%d,%d,%d,%+v): %v", m, c, r, st, err)
					}
					if pc, pr := PinMinSize(ModeCommand); l.Mode == ModeCommand && c >= pc && r >= pr {
						if _, ok := l.Pane(SlotTool); !ok {
							t.Fatalf("command %dx%d %+v has no tool slot", c, r, st)
						}
					}
				}
			}
		}
	}
}
