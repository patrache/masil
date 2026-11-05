package app

import (
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// Project maps the logical state onto the panes of l. Pure and deterministic.
// Result: slot -> Route; a slot absent from the map is a pane-factory slot
// (rail, sidebar, members, tabs, context, status) or empty.
func Project(l layout.Layout, n NavState) map[layout.SlotID]ui.Route {
	// TODO(core): Task 24
	return nil
}
