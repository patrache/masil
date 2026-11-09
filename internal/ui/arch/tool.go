// Package arch is the architecture design tool (v0.1.3 spec C2, tool id
// "arch"): a canvas instance on the tool surface that draws a design
// (archdraw), analyses it on every edit (archsim) and saves it to a personal
// file, a card copy or a room document. Every screen string comes from the
// archcanvas catalog area; the tool is invisible while features.On("arch") is off.
package arch

import (
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/features"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ids"
	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/palette"
)

// Instance query keys (ui.Route.Query of a RouteTool instance).
const (
	queryFile   = "file"   // a personal design name
	queryDoc    = "doc"    // a room document id (P3 Task 22)
	queryMsg    = "msg"    // a design card's message id: opened as a copy (P3 Task 26)
	queryDialog = "dialog" // "open": show the open dialog first (P3 Task 20)
)

// Instance id prefixes (the instance id doubles as the OpenTool key).
const (
	keyFile = "arch:file:"
	keyDoc  = "arch:doc:"
	keyMsg  = "arch:msg:"
	keyNew  = "arch:new:"
)

func init() {
	ui.RegisterTool(ui.ToolSpec{
		ID:          ui.ToolArch,
		Order:       3,
		Icon:        "🏗",
		Name:        func(t *i18n.T) string { return t.Archcanvas.ToolName() },
		Available:   func(*ui.Context) bool { return features.On(ui.ToolArch) },
		Status:      hubStatus,
		New:         newCanvas,
		Open:        openCanvas,
		WideDefault: true,
	})
	registerCommands()
}

// newInstanceID is a fresh new-design instance id.
func newInstanceID() string { return keyNew + ids.New() }

// openFile opens personal design name (the same file twice shows one instance).
func openFile(name string) tea.Cmd {
	return ui.OpenTool(ui.ToolArch, keyFile+name, map[string]string{queryFile: name})
}

// openNew opens a new, unsaved design.
func openNew(q map[string]string) tea.Cmd {
	return ui.OpenTool(ui.ToolArch, newInstanceID(), q)
}

// openCanvas is the hub's enter: show the open canvas in front (or the first
// open one), else start a new design.
func openCanvas(ctx *ui.Context, _ termhost.Mode) tea.Cmd {
	if r, ok := openInstance(ctx, false); ok {
		return ui.OpenTool(ui.ToolArch, r.ID, r.Query)
	}
	return openNew(nil)
}

// openInstance finds an open arch instance: the active one, or (any) the first.
func openInstance(ctx *ui.Context, activeOnly bool) (ui.Route, bool) {
	if ctx == nil || ctx.OpenTools == nil {
		return ui.Route{}, false
	}
	var first *ui.Route
	for _, o := range ctx.OpenTools() {
		if o.Route.Arg != ui.ToolArch {
			continue
		}
		if o.Active {
			return o.Route, true
		}
		if first == nil {
			r := o.Route
			first = &r
		}
	}
	if first != nil && !activeOnly {
		return *first, true
	}
	return ui.Route{}, false
}

// hubStatus is the hub row's status: the open design's (active first), else "새 설계".
func hubStatus(ctx *ui.Context) string {
	if ctx.OpenTools != nil {
		r, ok := openInstance(ctx, false)
		for _, o := range ctx.OpenTools() {
			if ok && o.Route.Key() == r.Key() && o.Status != "" {
				return o.Status
			}
		}
	}
	return ctx.T.Archcanvas.StatusNew()
}

// commandMsg runs palette command id on canvas instance.
type commandMsg struct {
	instance string
	id       string
}

// commandIDs are the arch.* palette commands (CA 5.6).
var commandIDs = []string{"arch.new", "arch.open", "arch.share", "arch.export", "arch.versions", "arch.send_card", "arch.run", "arch.save_copy", "arch.challenge"}

func registerCommands() {
	flag := func(*ui.Context) bool { return features.On(ui.ToolArch) }
	front := func(ctx *ui.Context) bool {
		_, ok := openInstance(ctx, true)
		return features.On(ui.ToolArch) && ok
	}
	// a room document in front (versions exist only there)
	frontDoc := func(ctx *ui.Context) bool {
		r, ok := openInstance(ctx, true)
		return features.On(ui.ToolArch) && ok && r.Query[queryDoc] != ""
	}
	toFront := func(id string) func(*ui.Context, string) tea.Cmd {
		return func(ctx *ui.Context, _ string) tea.Cmd {
			r, ok := openInstance(ctx, true)
			if !ok {
				return nil
			}
			return func() tea.Msg { return commandMsg{instance: r.ID, id: id} }
		}
	}
	cmds := []palette.Command{
		{ID: "arch.new", Label: func(t *i18n.T) string { return t.Archcanvas.PaletteNew() }, Keywords: []string{"arch", "architecture", "new"}, Available: flag,
			Run: func(*ui.Context, string) tea.Cmd { return openNew(nil) }},
		{ID: "arch.open", Label: func(t *i18n.T) string { return t.Archcanvas.PaletteOpen() }, Keywords: []string{"arch", "architecture", "open"}, Available: flag,
			Run: func(*ui.Context, string) tea.Cmd { return ui.OpenOverlay(newFilesDialog()) }},
		{ID: "arch.share", Label: func(t *i18n.T) string { return t.Archcanvas.PaletteShare() }, Keywords: []string{"arch", "share"}, Available: front, Run: toFront("arch.share")},
		{ID: "arch.export", Label: func(t *i18n.T) string { return t.Archcanvas.PaletteExport() }, Keywords: []string{"arch", "export", "mermaid", "terraform"}, Available: front, Run: toFront("arch.export")},
		{ID: "arch.versions", Label: func(t *i18n.T) string { return t.Archcanvas.PaletteVersions() }, Keywords: []string{"arch", "versions"}, Available: frontDoc, Run: toFront("arch.versions")},
		{ID: "arch.send_card", Label: func(t *i18n.T) string { return t.Archcanvas.PaletteSendCard() }, Keywords: []string{"arch", "card", "send"}, Available: front, Run: toFront("arch.send_card")},
		{ID: "arch.save_copy", Label: func(t *i18n.T) string { return t.Archcanvas.PaletteSaveCopy() }, Keywords: []string{"arch", "copy", "save"}, Available: front, Run: toFront("arch.save_copy")},
		{ID: "arch.run", Label: func(t *i18n.T) string { return t.Archcanvas.PaletteRun() }, Keywords: []string{"arch", "run", "simulate"}, Available: front, Run: toFront("arch.run")},
		// the optional daily challenge (Task 28): its own command, no hub row (JX12)
		{ID: "arch.challenge", Label: func(t *i18n.T) string { return t.Archcanvas.PaletteChallenge() }, Keywords: []string{"arch", "challenge", "daily"}, Available: flag,
			Run: func(ctx *ui.Context, _ string) tea.Cmd { return openChallenge(ctx) }},
	}
	for _, c := range cmds {
		c.Group = palette.GroupCommand
		palette.Register(c)
	}
}
