package app

import (
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// Front selects which surface single-pane modes show in Main.
type Front int

// Fronts.
const (
	FrontTab           Front = iota // single-pane modes show the active tab (its stack top, or its thread)
	FrontFeed                       // ... show the feed surface (standard: FeedSwapped)
	FrontNotifications              // ... show the notifications route
)

// TabState is one open place.
type TabState struct {
	Stack  []ui.Route // Stack[0] = tab root (RouteHome | RouteChannel); never RouteThread/RouteFeed/RouteNotifications
	Thread *ui.Route  // this tab's open thread (Kind=RouteThread, Arg=root channel id), nil = closed
}

// NavState is the mode-independent logical navigation state.
type NavState struct {
	Tabs   []TabState // open places, max 9 (compact tab bar; command/standard: sidebar-selection history; widget/tiny: the "채팅" tab)
	Active int        // index into Tabs
	Feed   []ui.Route // feed surface stack; Feed[0].Kind == RouteFeed (default {feed, Arg:"following"}); never empty
	Front  Front      // which surface single-pane modes (and standard's FeedSwapped) show in Main
}

// NotifRoute is the single notifications route (key "notifications//").
var NotifRoute = ui.Route{Kind: ui.RouteNotifications}

// LayoutState derives layout.State for mode.
func (n NavState) LayoutState(mode layout.Mode) layout.State {
	// TODO(core): Task 24
	return layout.State{}
}

// Keys returns every route key held anywhere (instance-cache pinning).
func (n NavState) Keys() []string {
	// TODO(core): Task 24
	return nil
}
