// Package tools is the tool entry of v0.1.3 (spec A8, P0 Task 19): the hub
// (RouteTools), the RouteTool factory that builds the registered tools'
// instances, the hub's one-off open-mode menu and the palette entries
// (tools.* surface commands and one command per tool). Everything is
// invisible while no tool feature flag is on (features.Any).
//
// cmd/masil imports it for its init(); chat never does (/tools reaches the hub
// through ui.RunCommand("tools.hub", "")).
package tools

import (
	"github.com/patrache/masil/internal/ui"
)

func init() {
	ui.RegisterRoute(ui.RouteTools, func(ctx *ui.Context, _ ui.Route) ui.Component { return newHub(ctx) })
	ui.RegisterRoute(ui.RouteTool, newInstance)
	registerCommands()
	registerToolCommands()
}

// newInstance builds a RouteTool instance with its tool's New (nil while the
// tool is unregistered or flagged off: the app shows nothing for it).
func newInstance(ctx *ui.Context, r ui.Route) ui.Component {
	s, ok := ui.ToolByID(r.Arg)
	if !ok || s.New == nil {
		return nil
	}
	return s.New(ctx, r)
}
