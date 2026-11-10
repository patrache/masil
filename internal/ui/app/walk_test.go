package app_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
)

// 0.1.3 patch (tools-nav item 7): the top tab bar lists exactly what [ / ]
// walk now, in every mode — the chat side's places, or the open tools while
// the tool surface has the focus — and a click on an entry goes there.

var walkSizes = []struct {
	mode layout.Mode
	size uitest.Size
}{
	{layout.ModeCommand, uitest.Size{W: 200, H: 60}},
	{layout.ModeStandard, uitest.Size{W: 120, H: 40}},
	{layout.ModeCompact, uitest.Size{W: 120, H: 30}},
	{layout.ModeWidget, uitest.Size{W: 80, H: 24}},
	{layout.ModeTiny, uitest.Size{W: 50, H: 20}},
}

// walkHarness opens the hub and two tools (git in front) at size, then
// leaves the tool surface when chat is set.
func walkHarness(t *testing.T, size uitest.Size, chat bool) *uitest.Harness {
	t.Helper()
	h := uitest.New(t)
	h.Resize(size.W, size.H)
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteTools}})
	openTool(h, ui.ToolLeet, "l1")
	openTool(h, ui.ToolGit, "g1")
	if chat {
		h.Press("ctrl+t")
		if h.Model.Nav().Front == app.FrontTools && h.Model.Frame().Layout.Mode != layout.ModeCommand {
			t.Fatal("ctrl+t did not leave the tool surface")
		}
		if h.Model.Frame().Layout.Mode == layout.ModeCommand {
			h.Click(40, 10) // command keeps the tool column: the chat column takes the focus
		}
	}
	return h
}

func bar(h *uitest.Harness) ([]string, int) {
	h.Screen() // draw: Hit reads the last drawn bar
	return app.TabBarForTest(h.Model)
}

// The bar is on the top row in every mode, every entry on it.
func TestWalkBarOnTopEveryMode(t *testing.T) {
	installStubTools(t, "all")
	for _, c := range walkSizes {
		for _, chat := range []bool{false, true} {
			h := walkHarness(t, c.size, chat)
			p, ok := h.Model.Frame().Layout.Pane(layout.SlotTabs)
			if !ok || p.Rect.Y != 0 || p.Rect.W != c.size.W {
				t.Fatalf("%s chat=%v: tab bar pane %+v, %v", c.mode.Label(), chat, p, ok)
			}
			labels, _ := bar(h)
			for i := range labels {
				if !slices.ContainsFunc(columns(h), func(e int) bool { return e == i }) {
					t.Errorf("%s chat=%v: entry %d (%q) has no cell on %q", c.mode.Label(), chat, i, labels[i], tabRow(h))
				}
			}
			if !strings.Contains(tabRow(h), "[") || !strings.Contains(tabRow(h), "]") {
				t.Errorf("%s chat=%v: no [ ] marks: %q", c.mode.Label(), chat, tabRow(h))
			}
			h.Check()
		}
	}
}

// columns is the entry each column of the bar maps to (-1: none).
func columns(h *uitest.Harness) []int {
	h.Screen()
	w := h.Model.Frame().Layout.Cols
	out := make([]int, w)
	for x := range w {
		out[x] = -1
		if i, ok := app.TabHitForTest(h.Model, x); ok {
			out[x] = i
		}
	}
	return out
}

// Pressing ] (and [) lands on the bar's next (previous) entry, all the way
// round; landing on 도구 from the chat side brings the tool surface (whose bar
// then lists the tools).
func TestWalkBarAgreesWithKeys(t *testing.T) {
	installStubTools(t, "all")
	for _, c := range walkSizes {
		for _, chat := range []bool{false, true} {
			for _, key := range []string{"]", "["} {
				h := walkHarness(t, c.size, chat)
				delta := 1
				if key == "[" {
					delta = -1
				}
				labels, cur := bar(h)
				for range len(labels) {
					want := ((cur+delta)%len(labels) + len(labels)) % len(labels)
					h.Press(key)
					got, now := bar(h)
					if !slices.Equal(got, labels) {
						if labels[want] != h.Context().T.Tools.TabLabel() {
							t.Fatalf("%s chat=%v %s: the bar changed %q -> %q", c.mode.Label(), chat, key, labels, got)
						}
						if _, ok := shownTool(h); !ok {
							t.Fatalf("%s: 도구 entry did not show the tool surface", c.mode.Label())
						}
						break
					}
					if now != want {
						t.Fatalf("%s chat=%v %s: from %d (%q) the key went to %d, the bar marks %d (%q)",
							c.mode.Label(), chat, key, cur, labels, now, want, labels[want])
					}
					checkShown(t, h, got[now])
					cur = now
				}
			}
		}
	}
}

// A click on an entry shows it.
func TestWalkBarClick(t *testing.T) {
	installStubTools(t, "all")
	for _, c := range walkSizes {
		for _, chat := range []bool{false, true} {
			labels, _ := bar(walkHarness(t, c.size, chat))
			for i := range labels {
				h := walkHarness(t, c.size, chat)
				x := slices.Index(columns(h), i)
				if x < 0 {
					t.Fatalf("%s: entry %d not drawn", c.mode.Label(), i)
				}
				h.Click(x, 0)
				got, now := bar(h)
				if !slices.Equal(got, labels) {
					if labels[i] != h.Context().T.Tools.TabLabel() {
						t.Fatalf("%s chat=%v: click %d (%q) changed the bar to %q", c.mode.Label(), chat, i, labels[i], got)
					}
					if _, ok := shownTool(h); !ok {
						t.Fatalf("%s: a click on 도구 did not show the tool surface", c.mode.Label())
					}
					continue
				}
				checkShown(t, h, got[now])
				if now != i {
					t.Errorf("%s chat=%v: click on %d (%q) shows %d", c.mode.Label(), chat, i, labels[i], now)
				}
				if !chat && c.mode != layout.ModeWidget && c.mode != layout.ModeTiny { // the tools walk
					if r, ok := shownTool(h); !ok || r.Key() != h.Model.Nav().Tools[i].Key() {
						t.Errorf("%s: click on tool %d shows %+v", c.mode.Label(), i, r)
					}
				}
			}
		}
	}
}

// Standard mode, the tool surface with three entries: the bar's look.
func TestWalkBarStandardGolden(t *testing.T) {
	installStubTools(t, "all")
	h := walkHarness(t, uitest.Size{W: 120, H: 40}, false)
	h.Golden("standard_tools")
	h = walkHarness(t, uitest.Size{W: 120, H: 40}, true)
	h.Golden("standard_chat")
}

// checkShown checks that the place shown is the bar entry label marks as
// current (the fixed entries; the tab roots are checked by their index).
func checkShown(t *testing.T, h *uitest.Harness, label string) {
	t.Helper()
	n := h.Model.Nav()
	mode := h.Model.Frame().Layout.Mode
	side := mode != layout.ModeCommand // command shows 피드/알림 in their own columns
	switch label {
	case "피드", "피":
		if side && n.Front != app.FrontFeed {
			t.Errorf("bar marks %q, front is %v", label, n.Front)
		}
	case "알림", "알":
		if side && n.Front != app.FrontNotifications {
			t.Errorf("bar marks %q, front is %v", label, n.Front)
		}
	case "DM":
		if !n.InDM {
			t.Errorf("bar marks DM, the DM surface is not shown")
		}
	}
}

// Review fix: on a one-pane screen tab / shift+tab walk the same sequence as
// ] / [ — on the compact tool surface the open tools, not the hidden chat walk.
func TestWalkBarAgreesWithTabKeyCompact(t *testing.T) {
	installStubTools(t, "all")
	for _, chat := range []bool{false, true} {
		for _, key := range []string{"tab", "shift+tab"} {
			h := walkHarness(t, uitest.Size{W: 120, H: 30}, chat)
			delta := 1
			if key == "shift+tab" {
				delta = -1
			}
			labels, cur := bar(h)
			for range len(labels) {
				want := ((cur+delta)%len(labels) + len(labels)) % len(labels)
				h.Press(key)
				got, now := bar(h)
				if !slices.Equal(got, labels) {
					if labels[want] != h.Context().T.Tools.TabLabel() {
						t.Fatalf("chat=%v %s: the bar changed %q -> %q", chat, key, labels, got)
					}
					break
				}
				if now != want {
					t.Fatalf("chat=%v %s: from %d (%q) the key went to %d, the bar marks %d", chat, key, cur, labels, now, want)
				}
				cur = now
			}
		}
	}
}
