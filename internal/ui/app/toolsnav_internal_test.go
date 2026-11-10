package app

import (
	"testing"

	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
)

// 0.1.3 patch (tools-nav fix 1): ctrl+t from the chat always brings the hub
// to the front — never straight back into the last tool — and remembers the
// tool instance that was active (ToolLast) so the hub's cursor starts on it.

var (
	navGit  = ui.Route{Kind: ui.RouteTool, ID: "g1", Arg: ui.ToolGit}
	navLeet = ui.Route{Kind: ui.RouteTool, ID: "l1", Arg: ui.ToolLeet}
)

func TestToggleToolsFromChatShowsHub(t *testing.T) {
	for _, mode := range []layout.Mode{layout.ModeCommand, layout.ModeStandard, layout.ModeCompact, layout.ModeWidget, layout.ModeTiny} {
		t.Run(mode.Label(), func(t *testing.T) {
			// The hub is open, a tool instance was active, the chat is in front.
			n := newNavState()
			n.Tools = []ui.Route{hubRoute, navLeet, navGit}
			n.ToolActive = 2
			n.Front = FrontTab
			c, intent := n.toggleTools(mode)
			if c.Front != FrontTools {
				t.Fatalf("front = %v, want FrontTools", c.Front)
			}
			if r, _ := c.activeTool(); r.Kind != ui.RouteTools {
				t.Fatalf("active = %+v, want the hub", r)
			}
			if len(c.Tools) != 3 {
				t.Fatalf("tools = %v: an open tool was closed", c.Tools)
			}
			if c.ToolLast != navGit.Key() {
				t.Errorf("ToolLast = %q, want %q", c.ToolLast, navGit.Key())
			}
			if intent.RouteKey != hubRoute.Key() || intent.Slot != toolSlotFor(mode) {
				t.Errorf("intent = %+v, want the hub in %s", intent, toolSlotFor(mode))
			}
			// ctrl+t again from the hub: back to the chat, the tools kept.
			b, intent := c.toggleTools(mode)
			if b.Front != FrontTab || len(b.Tools) != 3 || intent.Slot != layout.SlotMain {
				t.Errorf("hub -> ctrl+t: front %v tools %v intent %+v, want the chat", b.Front, b.Tools, intent)
			}
		})
	}
}

// A tool opened without the hub (palette, /tools …): ctrl+t adds the hub in
// front of it, the instance kept.
func TestToggleToolsAddsHubBeforeInstance(t *testing.T) {
	n := newNavState()
	n.Tools = []ui.Route{navGit}
	n.Front = FrontTab
	c, _ := n.toggleTools(layout.ModeStandard)
	if len(c.Tools) != 2 || c.Tools[0].Kind != ui.RouteTools || c.Tools[1].Key() != navGit.Key() {
		t.Fatalf("tools = %v, want [hub git]", c.Tools)
	}
	if c.ToolActive != 0 || c.Front != FrontTools || c.ToolLast != navGit.Key() {
		t.Errorf("active %d front %v last %q, want the hub in front, last = git", c.ToolActive, c.Front, c.ToolLast)
	}
}

// The hub itself was active (left with esc or ctrl+t): ToolLast is cleared
// (review fix), so the hub keeps the cursor where the user left it instead
// of jumping back to an older instance.
func TestToggleToolsFromHubClearsLast(t *testing.T) {
	n := newNavState()
	n.Tools = []ui.Route{hubRoute, navGit}
	n.ToolLast = navGit.Key()
	n.Front = FrontTab
	c, _ := n.toggleTools(layout.ModeStandard)
	if c.ToolLast != "" {
		t.Errorf("ToolLast = %q, want cleared (the hub was the last entry)", c.ToolLast)
	}
	// hub -> chat -> hub: still cleared.
	b, _ := c.toggleTools(layout.ModeStandard)
	if b, _ = b.toggleTools(layout.ModeStandard); b.ToolLast != "" {
		t.Errorf("hub -> chat -> hub: ToolLast = %q, want cleared", b.ToolLast)
	}
}

// The first ctrl+t (no tool open) still opens the hub alone.
func TestToggleToolsFirstOpensHub(t *testing.T) {
	c, _ := newNavState().toggleTools(layout.ModeStandard)
	if len(c.Tools) != 1 || c.Tools[0].Kind != ui.RouteTools || c.Front != FrontTools || c.ToolLast != "" {
		t.Errorf("tools %v front %v last %q, want the hub alone", c.Tools, c.Front, c.ToolLast)
	}
}

// Closing the remembered instance forgets it; a stale ToolLast is repaired.
func TestToolLastForgottenOnClose(t *testing.T) {
	n := newNavState()
	n.Tools = []ui.Route{hubRoute, navLeet, navGit}
	n.ToolLast = navGit.Key()
	c, ok := n.closeToolEntry(navGit.Key())
	if !ok || c.ToolLast != "" {
		t.Errorf("after closing git: ToolLast = %q, want empty", c.ToolLast)
	}
	n.ToolLast = "tool/gone/git"
	if c := n.clone(); c.ToolLast != "" {
		t.Errorf("clone kept a ToolLast not on the surface: %q", c.ToolLast)
	}
	n.ToolLast = navLeet.Key()
	if c := n.clone(); c.ToolLast != navLeet.Key() {
		t.Errorf("clone lost ToolLast: %q", c.ToolLast)
	}
}

// The context's open-tool list marks the remembered instance (the hub reads it).
func TestOpenToolsMarksLast(t *testing.T) {
	m := &Model{}
	m.nav = newNavState()
	m.nav.Tools = []ui.Route{hubRoute, navLeet, navGit}
	m.nav.ToolLast = navGit.Key()
	m.ctx = &ui.Context{Keys: keymap.Default(), T: i18n.KoT()}
	m.cache = newInstanceCache()
	got := m.openTools()
	if len(got) != 2 || got[0].Last || !got[1].Last {
		t.Errorf("OpenTools = %+v, want git marked Last", got)
	}
}
