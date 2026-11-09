package arch

import (
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/features"
	"github.com/patrache/masil/internal/ui"
)

func init() {
	ui.RegisterAlertTarget("doc:arch", openAlert)
}

// openAlert is a doc.comment_mention alert's enter (target {type:"doc",
// doc_type:"arch"}): the room document with its comments panel. The flag off
// (or no document id) opens nothing.
func openAlert(_ *ui.Context, a api.Alert) tea.Cmd {
	id := a.Target.ID
	if !features.On(ui.ToolArch) || id == "" {
		return nil
	}
	inst := keyDoc + id
	return tea.Batch(
		ui.OpenTool(ui.ToolArch, inst, map[string]string{queryDoc: id, queryComments: "1"}),
		// a canvas already open on it keeps its query: open its comments too
		func() tea.Msg { return openCommentsMsg{instance: inst} },
	)
}
