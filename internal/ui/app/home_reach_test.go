package app_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// Widget and tiny have no rail and no sidebar: once a channel is open, the
// palette (ctrl+k, and the context-line ▾ that opens it) must still lead home.
func TestPaletteReachesHomeInWidgetAndTiny(t *testing.T) {
	for _, sz := range []struct{ w, h int }{{80, 24}, {40, 12}} {
		t.Run(fmt.Sprintf("%dx%d", sz.w, sz.h), func(t *testing.T) {
			t.Cleanup(palette.SnapshotForTest())
			h := uitest.New(t)
			h.Resize(sz.w, sz.h)
			h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChDev}})
			nav := h.Nav()
			if root := nav.Tabs[nav.Active].Stack[0]; root.Kind != ui.RouteChannel {
				t.Fatalf("setup: active root %+v, want #개발", root)
			}
			h.Press("ctrl+k")
			h.Type("홈")
			if s := h.Screen(); strings.Contains(s, "찾는 항목이 없어요") {
				t.Fatalf("홈 must be found in the palette:\n%s", s)
			}
			h.Press("enter")
			nav = h.Nav()
			if root := nav.Tabs[nav.Active].Stack[0]; root.Kind != ui.RouteHome || nav.Front != 0 {
				t.Fatalf("after 홈: active root %+v front %v, want the home tab in front", root, nav.Front)
			}
		})
	}
}

// The empty-query palette (what the context-line ▾ shows) lists 홈 too.
func TestPaletteEmptyQueryListsHome(t *testing.T) {
	t.Cleanup(palette.SnapshotForTest())
	h := uitest.New(t)
	h.Resize(80, 24)
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChDev}})
	h.Press("ctrl+k")
	if s := h.Screen(); !strings.Contains(s, "홈") {
		t.Fatalf("empty palette must list 홈:\n%s", s)
	}
}

// Compact has no rail or sidebar either: opening many places never evicts the 홈 tab.
func TestCompactNeverEvictsHomeTab(t *testing.T) {
	h := uitest.New(t)
	h.Resize(120, 30)
	ids := []string{fixtures.ChGeneral, fixtures.ChChat, fixtures.ChDev, fixtures.ChDeploy, fixtures.DMMinji, fixtures.DMJunho}
	for i := len(ids); i < 9; i++ {
		ids = append(ids, fmt.Sprintf("01JB0000000000000000000XC%d", i))
	}
	for _, id := range ids {
		h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteChannel, ID: id}})
	}
	nav := h.Nav()
	if len(nav.Tabs) != 9 {
		t.Fatalf("tabs = %d, want 9", len(nav.Tabs))
	}
	home := false
	for _, tb := range nav.Tabs {
		home = home || tb.Stack[0].Kind == ui.RouteHome
	}
	if !home {
		t.Fatalf("the 홈 tab was evicted: %+v", nav.Tabs)
	}
	if first := strings.SplitN(h.Screen(), "\n", 2)[0]; !strings.Contains(first, "홈") {
		t.Fatalf("tab bar must still show 홈: %q", first)
	}
}
