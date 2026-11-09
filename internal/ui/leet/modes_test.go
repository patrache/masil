package leet_test

import (
	"strings"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
)

// modeWalk is the resize walk of the mode-switch test (P1 Task 24): every
// layout mode and back.
var modeWalk = []uitest.Size{{W: 200, H: 60}, {W: 120, H: 40}, {W: 80, H: 24}, {W: 40, H: 14}, {W: 120, H: 40}}

// TestModeSwitchKeepsInstances: today's problem with nvim in a pane stays the
// same instance and the same nvim through every layout mode, each mode shows
// the tool again, and the frame (the tab row's tool cell at 80x24 included)
// holds at every size.
func TestModeSwitchKeepsInstances(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withNvim(t, true)
	h := uitest.New(t)
	h.Resize(200, 60)
	leetMe(h, api.LeetMe{Current: 3, Best: 5})
	dailyReport(h, "2026-11-05", slug1606)
	openToday(t, h)
	h.Press("e")
	if n := len(e.pane.Started()); n != 1 {
		t.Fatalf("Started = %d, want 1 (nvim)", n)
	}
	inst := todayInstance(t, h)
	for _, sz := range modeWalk {
		h.Resize(sz.W, sz.H)
		h.Check()
		if n := len(e.pane.Started()); n != 1 {
			t.Fatalf("%dx%d: Started = %d, want 1 (a resize restarted nvim)", sz.W, sz.H, n)
		}
		if c := todayInstance(t, h); c != inst {
			t.Fatalf("%dx%d: leet:today is a new instance", sz.W, sz.H)
		}
		if s := h.Screen(); !strings.Contains(s, "nvim pane") {
			t.Fatalf("%dx%d (%v): the tool's editor is not shown:\n%s", sz.W, sz.H, h.Context().Mode, s)
		}
	}
	// Leaving for chat and coming back shows the same tool.
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteHome}})
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteTool, ID: "leet:today", Arg: ui.ToolLeet}})
	h.Check()
	if s := h.Screen(); !strings.Contains(s, "nvim pane") || len(e.pane.Started()) != 1 || todayInstance(t, h) != inst {
		t.Fatalf("back to the tool: Started %d\n%s", len(e.pane.Started()), s)
	}
}

// TestTinyOpensExec: in the tiny mode no pane opens — e edits in place
// (goeditor) and E opens nvim as a screen switch (P0 Choose's tiny rule) —
// and the frame holds before and after it returns.
func TestTinyOpensExec(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withNvim(t, true)
	h := uitest.New(t)
	h.Resize(40, 14)
	if h.Context().Mode != layout.ModeTiny {
		t.Fatalf("40x14 is %v, want tiny", h.Context().Mode)
	}
	openToday(t, h)
	h.Press("e")
	if n := len(e.pane.Started()); n != 0 {
		t.Fatalf("tiny e started %+v", e.pane.Started())
	}
	h.Check()
	h.Press("esc", "E")
	st := e.pane.Started()
	if len(st) != 1 || st[0].Program != "/opt/bin/nvim" || st[0].Mode != termhost.ModeExec {
		t.Fatalf("Started = %+v:\n%s", st, h.Screen())
	}
	if s := h.Screen(); strings.Contains(s, "nvim pane") {
		t.Fatalf("tiny opened a pane:\n%s", s)
	}
	h.Check()
	h.Send(e.pane.Exit("leet:today#exec", 0)) // back from the screen switch
	h.Check()
	h.Resize(120, 40)
	h.Check()
	if s := h.Screen(); !strings.Contains(s, "1606") || strings.Contains(s, "nvim pane") {
		t.Fatalf("after the switch back:\n%s", s)
	}
}
