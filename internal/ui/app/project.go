package app

import (
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// Project maps the logical state onto the panes of l. Pure and deterministic.
// Result: slot -> Route; a slot absent from the map is a pane-factory slot
// (rail, sidebar, members, tabs, context, status) or empty. A slot is only
// filled when l has that pane (CA 5.3.3 Project table, first match per mode).
func Project(l layout.Layout, n NavState) map[layout.SlotID]ui.Route {
	out := map[layout.SlotID]ui.Route{}
	t, ok := n.activeTab()
	if !ok || len(n.Feed) == 0 || l.Mode == layout.ModeTooSmall {
		return out
	}
	has := func(s layout.SlotID) bool { _, ok := l.Pane(s); return ok }
	set := func(s layout.SlotID, r ui.Route) {
		if has(s) {
			out[s] = r
		}
	}
	top := t.Stack[len(t.Stack)-1]
	f := n.Feed[len(n.Feed)-1]
	switch l.Mode {
	case layout.ModeCommand:
		set(layout.SlotMain, top)
		if t.Thread != nil {
			set(layout.SlotThread, *t.Thread)
		}
		set(layout.SlotFeed, f)
		set(layout.SlotNotifications, NotifRoute)
	case layout.ModeStandard:
		main := top
		switch {
		case n.Front == FrontFeed:
			main = f
		case n.Front == FrontNotifications:
			main = NotifRoute
		case !has(layout.SlotThread) && attached(t):
			main = *t.Thread
		}
		set(layout.SlotMain, main)
		if t.Thread != nil {
			set(layout.SlotThread, *t.Thread)
		}
	default: // compact, widget, tiny
		main := top
		switch {
		case n.Front == FrontFeed:
			main = f
		case n.Front == FrontNotifications:
			main = NotifRoute
		case attached(t):
			main = *t.Thread
		}
		set(layout.SlotMain, main)
	}
	return out
}
