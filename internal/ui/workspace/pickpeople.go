package workspace

import (
	"context"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui"
)

// maxInvite is the share's seat count (seats 1..8).
const maxInvite = 8

// inviteCandidates are the friends one can invite, by handle.
func inviteCandidates(ctx *ui.Context) []api.UserSummary {
	var out []api.UserSummary
	if ctx.Store != nil {
		for _, f := range ctx.Store.Friends {
			if f.User.Handle != "" && !f.User.Suspended {
				out = append(out, f.User)
			}
		}
	}
	slices.SortFunc(out, func(a, b api.UserSummary) int { return strings.Compare(a.Handle, b.Handle) })
	return out
}

// invitePick is the toggled friends, in the order they were picked (seat order).
type invitePick struct{ handles []string }

func (p *invitePick) has(h string) bool { return slices.Contains(p.handles, h) }

// toggle adds or drops h; false when the share is full.
func (p *invitePick) toggle(h string) bool {
	if i := slices.Index(p.handles, h); i >= 0 {
		p.handles = slices.Delete(p.handles, i, i+1)
		return true
	}
	if len(p.handles) >= maxInvite {
		return false
	}
	p.handles = append(p.handles, h)
	return true
}

// spaceChoices are the spaces I am in, in join order.
func spaceChoices(ctx *ui.Context) []*api.SpaceDetail {
	var out []*api.SpaceDetail
	if ctx.Store == nil {
		return nil
	}
	for _, id := range ctx.Store.SpaceOrder {
		if sd := ctx.Store.Spaces[id]; sd != nil {
			out = append(out, sd)
		}
	}
	return out
}

// spaceMemberLimit is how many members of a space the dialog fetches (the
// server's page maximum).
const spaceMemberLimit = 100

// spacePick is the share dialog's chosen space and its members once fetched.
type spacePick struct {
	id      string
	loading bool
	failed  bool
	members []api.UserSummary
}

// spaceMembersMsg brings a space's members to the share dialog.
type spaceMembersMsg struct {
	surface, space string
	members        []api.UserSummary
	err            error
}

// choose picks space id (again: unpick) and fetches its members.
func (p *spacePick) choose(ctx *ui.Context, surface, id string) tea.Cmd {
	if p.id == id {
		*p = spacePick{}
		return nil
	}
	*p = spacePick{id: id, loading: true}
	client := ctx.API
	if client == nil {
		p.loading, p.failed = false, true
		return nil
	}
	return func() tea.Msg {
		page, err := client.ListSpaceMembers(context.Background(), id, api.SpaceMembersQuery{PageOpts: api.PageOpts{Limit: spaceMemberLimit}})
		m := spaceMembersMsg{surface: surface, space: id, err: err}
		if err == nil {
			for _, sm := range page.Items {
				m.members = append(m.members, sm.User)
			}
		}
		return m
	}
}

// got takes a fetch result for the chosen space (a late one is dropped).
func (p *spacePick) got(m spaceMembersMsg) {
	if p == nil || m.space != p.id {
		return
	}
	p.loading = false
	p.failed = m.err != nil
	p.members = m.members
}

// spaceInvitees are a space's members one can invite: not me, not
// suspended, with a handle, and not already listed as a friend.
func spaceInvitees(ctx *ui.Context, members, friends []api.UserSummary) []api.UserSummary {
	me := ""
	if u := ctx.Me(); u != nil {
		me = u.ID
	}
	var out []api.UserSummary
	for _, u := range members {
		if u.ID == me || u.Handle == "" || u.Suspended || slices.ContainsFunc(friends, func(f api.UserSummary) bool { return f.ID == u.ID }) {
			continue
		}
		out = append(out, u)
	}
	slices.SortFunc(out, func(a, b api.UserSummary) int { return strings.Compare(a.Handle, b.Handle) })
	return out
}
