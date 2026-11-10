package chat

import (
	"cmp"
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/errtext"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/palette"
)

func init() {
	ui.RegisterOverlay(overlayDMNew, newDMDialog)
	palette.Register(palette.Command{
		ID: "dm.new", Title: "새 대화 (DM·그룹 DM)", Keywords: []string{"dm", "direct message", "group", "대화", "그룹 DM", "메시지 보내기"}, Group: palette.GroupCommand,
		Run: func(_ *ui.Context, args string) tea.Cmd { return ui.OpenRegisteredOverlay(overlayDMNew, args) },
	})
}

// overlayDMNew is the registered dm.new picker (args = the first query).
const overlayDMNew = "chat.dm_new"

// DM dialog texts and limits.
const (
	dmNewTitle   = "새 대화"
	dmNewSubmit  = "대화 시작"
	dmMaxPeers   = 9  // POST /v1/dms: 1..9 users besides me (api.md 0.3)
	groupDMLimit = 10 // group DM members, me included (api.md 3.1)
	tagDMOpen    = "chat.dm_open:"

	// groupDMRefusedText is a group DM create/add refused over a block between two
	// would-be members: 403 dm_not_allowed with no details.user_ids (api.md 3.1, D2).
	groupDMRefusedText = "함께 대화할 수 없는 참여자가 있어요"
)

// dmRequest is the POST /v1/dms body: one user = the 1:1 DM (never named),
// several = a new group DM named name ("" = no name).
func dmRequest(users []api.UserRef, name string) api.OpenDMRequest {
	req := api.OpenDMRequest{Users: users}
	if len(users) > 1 && name != "" {
		req.Name = api.Some(name)
	}
	return req
}

// friendCandidates are my friends (never a blocked user), for user pickers.
func friendCandidates(ctx *ui.Context) []api.UserSummary {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	out := make([]api.UserSummary, 0, len(ctx.Store.Friends))
	for id, f := range ctx.Store.Friends {
		if !ctx.Store.IsBlocked(id) {
			out = append(out, f.User)
		}
	}
	return out
}

// byPresence orders users online first (presenceRank).
func byPresence(ctx *ui.Context, a, b api.UserSummary) int {
	if ctx == nil || ctx.Store == nil {
		return 0
	}
	return cmp.Compare(presenceRank(userPresence(ctx, a.ID)), presenceRank(userPresence(ctx, b.ID)))
}

// blockedUser is a picker Exclude: users I blocked.
func blockedUser(ctx *ui.Context, id string) bool {
	return ctx != nil && ctx.Store != nil && ctx.Store.IsBlocked(id)
}

// userRefs are the {user} references of users.
func userRefs(users []api.UserSummary) []api.UserRef {
	refs := make([]api.UserRef, len(users))
	for i, u := range users {
		refs[i] = userRef(u)
	}
	return refs
}

// describeGroupDMErr words a failed group DM create/add: dm_not_allowed without
// details.user_ids is a block between two would-be members, which the server never
// names (api.md 3.1, D2); everything else (can_dm failures included) is errtext.Describe.
func describeGroupDMErr(t *i18n.T, err error, _ []api.UserSummary) string {
	var e *api.Error
	if errors.As(err, &e) && e.Code == "dm_not_allowed" && len(e.UserIDs()) == 0 {
		return groupDMRefusedText
	}
	return errtext.Describe(t, err)
}

// describeDMErr is dm.new's DescribeErr: two picks or more are a group DM.
func describeDMErr(t *i18n.T, err error, picked []api.UserSummary) string {
	if len(picked) >= 2 {
		return describeGroupDMErr(t, err, picked)
	}
	return errtext.Describe(t, err)
}

// newDMDialog is the dm.new picker: friends (online first) plus a server search,
// blocked users hidden, at most 9 people; from 2 picks on the "대화 이름(선택)"
// field appears. Submitting opens (or creates) the DM and navigates to it; an
// error (e.g. 403 dm_not_allowed) stays in the dialog.
func newDMDialog(_ *ui.Context, args string) ui.Overlay {
	return newUserPicker(pickerSpec{
		Title: dmNewTitle, Submit: dmNewSubmit, Max: dmMaxPeers, Search: true, Query: args,
		Candidates: friendCandidates, Exclude: blockedUser, Order: byPresence,
		NameField: true, NameFrom: 2, DescribeErr: describeDMErr,
		OnPick: func(ctx *ui.Context, users []api.UserSummary, name string) (string, tea.Cmd) {
			if len(users) == 0 {
				return "", nil
			}
			refs := userRefs(users)
			req := dmRequest(refs, name)
			tag := tagDMOpen + string(refs[0])
			return tag, ui.Do(ctx, tag, func(c context.Context, cl *api.Client) (*api.Channel, error) {
				ch, _, err := cl.OpenDM(c, req)
				return ch, err
			})
		},
		OnDone: func(ctx *ui.Context, result tea.Msg) (tea.Cmd, ui.Overlay) {
			m, ok := result.(ui.ResultMsg[*api.Channel])
			if !ok || m.Value == nil {
				return nil, nil
			}
			if ctx.Store != nil {
				ctx.Store.ObserveChannel(*m.Value)
			}
			return ui.Navigate(ui.Route{Kind: ui.RouteChannel, ID: m.Value.ID}), nil
		},
	})
}
