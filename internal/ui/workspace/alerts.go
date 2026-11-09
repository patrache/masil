package workspace

import (
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/features"
	"github.com/patrache/masil/internal/ui"
)

// The workspace alerts (spec B6.9): workspace.invite and workspace.attention
// carry target workspace_share. Their words come from alerts.toml through
// notify.Render; here is only where enter on one goes.

func init() {
	ui.RegisterAlertTarget("workspace_share", openShareAlert)
}

// openShareAlert opens share id: my own share's folder instance when it is
// one of mine, else the participant screen. With the flag off it returns nil
// (the alert stays in the inbox).
func openShareAlert(_ *ui.Context, a api.Alert) tea.Cmd {
	id := a.Target.ID
	if !features.On(ui.ToolWorkspace) || id == "" {
		return nil
	}
	liveMu.Lock()
	var mine *ui.Route
	for _, s := range live {
		if s.own != nil && s.own.id == id {
			r := s.route
			mine = &r
			break
		}
	}
	liveMu.Unlock()
	if mine != nil {
		r := *mine
		return func() tea.Msg { return ui.NavigateMsg{Route: r} }
	}
	return joinCmd(id)
}
