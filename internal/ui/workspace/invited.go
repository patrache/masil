package workspace

import (
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store/wsstore"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
)

// The shares I was invited to (spec B6.3 #1): the folder picker lists them
// above the recent folders — the workspace tool's own list stands in for a
// "sharing now" mark on friends' profiles. Enter joins.

// invitedShares are the live shares I was invited to, newest first.
func invitedShares(ctx *ui.Context) []api.WorkspaceShare {
	st := wsstore.Get(ctx.Store)
	if st == nil {
		return nil
	}
	return st.Invited()
}

// invitedRows is the picker's "초대받은 공유" section (none when empty).
func invitedRows(ctx *ui.Context, s *Surface) []row {
	shares := invitedShares(ctx)
	if len(shares) == 0 {
		return nil
	}
	t := ctx.T.Workspace
	rows := []row{{kind: rowHead, text: t.InvitedTitle()}}
	now := nowClock(ctx).Now()
	for _, sh := range shares {
		owner := textwidth.Sanitize(sh.Owner.DisplayName)
		if owner == "" {
			owner = textwidth.Sanitize(sh.Owner.Handle)
		}
		id := sh.ID
		rows = append(rows, row{kind: rowOption, text: t.InvitedRow(owner, textwidth.Sanitize(sh.Title), ago(ctx, now.Sub(sh.CreatedAt))),
			act: func(*ui.Context) tea.Cmd { return tea.Batch(joinCmd(id), ui.CloseTool(s.id)) }})
	}
	return append(rows, row{kind: rowText})
}
