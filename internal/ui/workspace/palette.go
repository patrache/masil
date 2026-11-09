package workspace

import (
	"context"
	"log/slog"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/features"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/palette"
)

// The ws.* palette commands (P2 Task 24). Every Available starts with the
// feature flag: while it is off none of them is offered.

// notifyTimeout bounds the settings PATCH of ws.notify.toggle.
const notifyTimeout = 15 * time.Second

func init() {
	on := func() bool { return features.On(ui.ToolWorkspace) }
	folder := func(ctx *ui.Context) bool { return on() && activeSharing(ctx) >= 0 }
	solo := func(ctx *ui.Context) bool { return on() && activeSharing(ctx) == 0 }
	sharing := func(ctx *ui.Context) bool { return on() && activeSharing(ctx) == 1 }
	cmds := []palette.Command{
		{ID: "ws.open", Label: func(t *i18n.T) string { return t.Workspace.PaletteOpen() },
			Keywords:  []string{"workspace", "folder", "open"},
			Available: func(*ui.Context) bool { return on() },
			Run: func(*ui.Context, string) tea.Cmd {
				return ui.OpenTool(ui.ToolWorkspace, pickerID, nil)
			}},
		{ID: "ws.share.start", Label: func(t *i18n.T) string { return t.Workspace.PaletteShareStart() },
			Keywords: []string{"workspace", "share"}, Available: solo, Run: wsCommand("share.start")},
		{ID: "ws.share.end", Label: func(t *i18n.T) string { return t.Workspace.PaletteShareEnd() },
			Keywords: []string{"workspace", "share", "end"}, Available: sharing, Run: wsCommand("share.end")},
		{ID: "ws.people", Label: func(t *i18n.T) string { return t.Workspace.PalettePeople() },
			Keywords: []string{"workspace", "people", "members"}, Available: sharing, Run: wsCommand("people")},
		{ID: "ws.proposals", Label: func(t *i18n.T) string { return t.Workspace.PaletteProposals() },
			Keywords: []string{"workspace", "proposals"}, Available: sharing, Run: wsCommand("proposals")},
		{ID: "ws.join", Label: func(t *i18n.T) string { return t.Workspace.PaletteJoin() },
			Keywords:  []string{"workspace", "join", "invite"},
			Available: func(ctx *ui.Context) bool { return on() && len(invitedShares(ctx)) > 0 },
			Run:       runJoin},
		{ID: "ws.term.new", Label: func(t *i18n.T) string { return t.Workspace.PaletteTermNew() },
			Keywords: []string{"workspace", "terminal", "new"}, Available: folder, Run: wsCommand("term.new")},
		{ID: "ws.autosave.toggle", Label: func(t *i18n.T) string { return t.Workspace.PaletteAutosaveToggle() },
			Keywords: []string{"workspace", "autosave", "save"}, Available: folder, Run: wsCommand("autosave")},
		{ID: "ws.notify.toggle", Label: func(t *i18n.T) string { return t.Workspace.PaletteNotifyToggle() },
			Keywords:  []string{"workspace", "notify", "alert"},
			Available: func(*ui.Context) bool { return on() },
			Run:       runNotifyToggle},
	}
	for _, c := range cmds {
		c.Group = palette.GroupCommand
		palette.Register(c)
	}
}

// runJoin is ws.join: the one invite straight away, else the picker (which
// lists the invites first).
func runJoin(ctx *ui.Context, _ string) tea.Cmd {
	if shares := invitedShares(ctx); len(shares) == 1 {
		return joinCmd(shares[0].ID)
	}
	return ui.OpenTool(ui.ToolWorkspace, pickerID, nil)
}

// runNotifyToggle is ws.notify.toggle: PATCH /v1/me/settings
// {notify:{workspace}}. The store follows the server's settings.updated
// event; the toast says what was asked.
func runNotifyToggle(ctx *ui.Context, _ string) tea.Cmd {
	want := true
	if ctx.Store != nil {
		want = !ctx.Store.Settings.Notify.Workspace
	}
	client, t := ctx.API, ctx.T.Workspace
	return func() tea.Msg {
		if client == nil {
			return ui.ToastMsg{Text: t.NotifyFailed(), Kind: ui.ToastError}
		}
		c, cancel := context.WithTimeout(context.Background(), notifyTimeout)
		defer cancel()
		req := api.UpdateSettingsRequest{Notify: &api.NotifySettingsPatch{Workspace: api.Some(want)}}
		if _, err := client.UpdateSettings(c, req); err != nil {
			slog.Warn("workspace: notify toggle", "err", err)
			return ui.ToastMsg{Text: t.NotifyFailed(), Kind: ui.ToastError}
		}
		if want {
			return ui.ToastMsg{Text: t.NotifyOn(), Kind: ui.ToastInfo}
		}
		return ui.ToastMsg{Text: t.NotifyOff(), Kind: ui.ToastInfo}
	}
}
