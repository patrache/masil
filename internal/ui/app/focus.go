package app

import (
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// resolveFocus picks this frame's focus slot from the remembered intent (CA 5.3.5,
// first match): (1) the slot showing the intent's route key, (2) the intent's slot
// when the layout has it, (3) Main. The intent itself is never changed here, so a
// route/pane that comes back is focused again. RouteKey is the resolved slot's route
// key ("" for pane-factory slots).
func resolveFocus(intent FocusIntent, l layout.Layout, routes map[layout.SlotID]ui.Route) FocusIntent {
	keyOf := func(s layout.SlotID) string {
		if r, ok := routes[s]; ok {
			return r.Key()
		}
		return ""
	}
	if intent.RouteKey != "" {
		for _, s := range focusOrder {
			if r, ok := routes[s]; ok && r.Key() == intent.RouteKey {
				return FocusIntent{Slot: s, RouteKey: intent.RouteKey}
			}
		}
	}
	if intent.Slot != "" {
		if _, ok := l.Pane(intent.Slot); ok {
			return FocusIntent{Slot: intent.Slot, RouteKey: keyOf(intent.Slot)}
		}
	}
	return FocusIntent{Slot: layout.SlotMain, RouteKey: keyOf(layout.SlotMain)}
}

// focusOrder is the focus ring order (CA 5.3.5).
var focusOrder = []layout.SlotID{
	layout.SlotRail, layout.SlotSidebar, layout.SlotMain, layout.SlotThread,
	layout.SlotFeed, layout.SlotMembers, layout.SlotNotifications,
}
