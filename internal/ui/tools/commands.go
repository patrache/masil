package tools

import (
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/features"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/palette"
)

// hubRoute is the hub (one per tool surface).
var hubRoute = ui.Route{Kind: ui.RouteTools}

// toolsOn is every tools.* command's Available: a tool feature flag is on.
func toolsOn(*ui.Context) bool { return features.Any() }

// surfaceAction is a tools.* command that runs a tool-surface action in the shell.
func surfaceAction(a ui.ToolSurfaceAction) func(*ui.Context, string) tea.Cmd {
	return func(*ui.Context, string) tea.Cmd {
		return func() tea.Msg { return ui.ToolSurfaceMsg{Action: a} }
	}
}

// registerCommands registers the tool-surface palette commands (A8.2):
// tools.hub, tools.toggle, tools.next, tools.prev, tools.zoom. Their Korean
// names are found through the catalog labels ("도구 허브" for 도구); the
// keywords are the English aliases.
func registerCommands() {
	cmds := []palette.Command{
		{
			ID: "tools.hub", Label: func(t *i18n.T) string { return t.Tools.PaletteHub() },
			Keywords: []string{"tools", "hub"},
			Run:      func(*ui.Context, string) tea.Cmd { return ui.Navigate(hubRoute) },
		},
		{
			ID: "tools.toggle", Label: func(t *i18n.T) string { return t.Tools.PaletteToggle() },
			Keywords: []string{"tools", "toggle"}, KeyHint: "ctrl+t",
			Run: surfaceAction(ui.ToolSurfaceToggle),
		},
		{
			ID: "tools.next", Label: func(t *i18n.T) string { return t.Tools.PaletteNext() },
			Keywords: []string{"tools", "next"},
			Run:      surfaceAction(ui.ToolSurfaceNext),
		},
		{
			ID: "tools.prev", Label: func(t *i18n.T) string { return t.Tools.PalettePrev() },
			Keywords: []string{"tools", "previous"},
			Run:      surfaceAction(ui.ToolSurfacePrev),
		},
		{
			ID: "tools.zoom", Label: func(t *i18n.T) string { return t.Tools.PaletteZoom() },
			Keywords: []string{"tools", "zoom", "wide"}, KeyHint: "ctrl+w z",
			Run: surfaceAction(ui.ToolSurfaceZoom),
		},
	}
	for _, c := range cmds {
		c.Group = palette.GroupNav
		c.Available = toolsOn
		palette.Register(c)
	}
}
