package ui_test

import (
	"testing"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

func mustPanic(t *testing.T, what string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: expected panic", what)
		}
	}()
	fn()
}

func mustNotPanic(t *testing.T, what string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s: unexpected panic: %v", what, r)
		}
	}()
	fn()
}

func routeFactory(*ui.Context, ui.Route) ui.Component { return nil }
func paneFactory(*ui.Context) ui.Component            { return nil }
func overlayFactory(*ui.Context, string) ui.Overlay   { return nil }

func TestRegisterRouteDuplicatePanics(t *testing.T) {
	t.Cleanup(ui.SnapshotRegistryForTest())
	ui.ResetRegistryForTest()
	ui.RegisterRoute(ui.RouteSettings, routeFactory)
	mustPanic(t, "duplicate route", func() { ui.RegisterRoute(ui.RouteSettings, routeFactory) })
	if ui.Registered().Routes[ui.RouteSettings] == nil {
		t.Fatal("RouteSettings not registered")
	}
}

func TestRegisterPaneRejectsRouteSlots(t *testing.T) {
	t.Cleanup(ui.SnapshotRegistryForTest())
	ui.ResetRegistryForTest()
	for _, s := range []layout.SlotID{layout.SlotMain, layout.SlotThread, layout.SlotFeed, layout.SlotNotifications, layout.SlotTabs, layout.SlotContext, layout.SlotStatus} {
		mustPanic(t, string(s), func() { ui.RegisterPane(s, paneFactory) })
	}
	for _, s := range []layout.SlotID{layout.SlotRail, layout.SlotSidebar, layout.SlotMembers} {
		mustNotPanic(t, string(s), func() { ui.RegisterPane(s, paneFactory) })
		if ui.Registered().Panes[s] == nil {
			t.Fatalf("pane %s not registered", s)
		}
	}
	mustPanic(t, "duplicate pane", func() { ui.RegisterPane(layout.SlotRail, paneFactory) })
}

func TestRegisterOverlayDuplicatePanics(t *testing.T) {
	t.Cleanup(ui.SnapshotRegistryForTest())
	ui.ResetRegistryForTest()
	ui.RegisterOverlay("fun.x", overlayFactory)
	mustPanic(t, "duplicate overlay", func() { ui.RegisterOverlay("fun.x", overlayFactory) })
	if ui.Registered().Overlays["fun.x"] == nil {
		t.Fatal("overlay fun.x not registered")
	}
}

func TestRegisteredIsCopy(t *testing.T) {
	t.Cleanup(ui.SnapshotRegistryForTest())
	ui.ResetRegistryForTest()
	ui.RegisterRoute(ui.RouteHelp, routeFactory)
	r := ui.Registered()
	delete(r.Routes, ui.RouteHelp)
	r.Routes[ui.RouteLogin] = routeFactory
	r.Panes[layout.SlotRail] = paneFactory
	r.Overlays["x.y"] = overlayFactory
	got := ui.Registered()
	if got.Routes[ui.RouteHelp] == nil || got.Routes[ui.RouteLogin] != nil || got.Panes[layout.SlotRail] != nil || got.Overlays["x.y"] != nil {
		t.Fatal("Registered() must return a copy")
	}
}

func TestSnapshotRestoresRegistry(t *testing.T) {
	t.Cleanup(ui.SnapshotRegistryForTest())
	ui.ResetRegistryForTest()
	ui.RegisterRoute(ui.RouteHelp, routeFactory)
	restore := ui.SnapshotRegistryForTest()
	ui.ResetRegistryForTest()
	ui.RegisterRoute(ui.RouteSettings, routeFactory)
	ui.RegisterPane(layout.SlotRail, paneFactory)
	restore()
	got := ui.Registered()
	if len(got.Routes) != 1 || got.Routes[ui.RouteHelp] == nil {
		t.Fatalf("routes after restore = %v, want only RouteHelp", got.Routes)
	}
	if len(got.Panes) != 0 {
		t.Fatalf("panes after restore = %v, want none", got.Panes)
	}
	mustPanic(t, "RouteHelp duplicate after restore", func() { ui.RegisterRoute(ui.RouteHelp, routeFactory) })
	mustNotPanic(t, "RouteSettings free after restore", func() { ui.RegisterRoute(ui.RouteSettings, routeFactory) })
}
