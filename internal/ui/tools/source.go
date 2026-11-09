package tools

import (
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/palette"
)

// The palette's tool entries (A8.1 ③: "ctrl+k → 각 도구 이름으로 바로"): one
// command per tool id, tools.open.<id>, titled "<icon> <name>" at draw time,
// found by its name and by "tools" and the tool id; it runs
// spec.Open(ctx, ModeAuto). They are commands rather than a palette.Source:
// the palette runs a chosen command item by id (ui.RunCommand), so an item
// must be a registered command. A tool that is not registered or is flagged
// off is not Available, so it is not listed.

// toolCommandID is the palette command of tool id.
func toolCommandID(id string) string { return "tools.open." + id }

// toolIDs are the five tool ids (ui.RegisterTool accepts no other).
var toolIDs = []string{ui.ToolLeet, ui.ToolWorkspace, ui.ToolArch, ui.ToolGit, ui.ToolAPI}

func registerToolCommands() {
	for _, id := range toolIDs {
		palette.Register(palette.Command{
			ID:       toolCommandID(id),
			Group:    palette.GroupNav,
			Keywords: []string{"tools", id},
			Label: func(t *i18n.T) string {
				s, ok := ui.ToolByID(id)
				if !ok {
					return id
				}
				return toolLabel(t, s)
			},
			Available: func(*ui.Context) bool {
				_, ok := ui.ToolByID(id)
				return ok
			},
			Run: func(ctx *ui.Context, _ string) tea.Cmd {
				s, ok := ui.ToolByID(id)
				if !ok || s.Open == nil {
					return nil
				}
				return s.Open(ctx, termhost.ModeAuto)
			},
		})
	}
}

// toolLabel is "<icon> <name>" of s in t's language.
func toolLabel(t *i18n.T, s ui.ToolSpec) string {
	name := s.ID
	if s.Name != nil {
		name = s.Name(t)
	}
	if s.Icon == "" {
		return name
	}
	return s.Icon + " " + name
}
