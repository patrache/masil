package workspace

import (
	"slices"
	"strings"

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
