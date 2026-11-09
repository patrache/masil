// Package wsstore is the workspace share list store (v0.1.3 B10): the live
// shares I was invited to and the live shares I own, kept from the
// workspace_share.* events (events.md 4.11) and refetched on resync. It is a
// store.Reducer named "workspace" and, like the rest of the Store, is
// mutated only on the Bubble Tea Update goroutine.
package wsstore

import (
	"context"
	"sort"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/features"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
)

// Name is the reducer name (store.Reducer).
const Name = "workspace"

// feature is the tool flag that gates the fetches (internal/features).
const feature = "workspace"

func init() {
	store.RegisterReducer(func(s *store.Store) store.Reducer { return newState(s) })
}

// change is what an event did to one share since the last Resync: put it
// (share), took it off my lists (removed: I was removed or left) or ended it
// while it was not known yet (ended).
type change struct {
	share   api.WorkspaceShare
	removed bool
	ended   *realtime.WorkspaceShareEnded
}

// State holds the shares by id.
type State struct {
	s      *store.Store // for Me: a share is mine when I own it
	shares map[string]api.WorkspaceShare

	// Resync bookkeeping: gen changes on Reset so an older result is
	// dropped; touched holds what events changed since the last Resync, laid
	// over its result.
	gen     uint64
	touched map[string]change
}

func newState(s *store.Store) *State {
	return &State{s: s, shares: map[string]api.WorkspaceShare{}, touched: map[string]change{}}
}

// Get returns the store's workspace reducer (nil if not registered).
func Get(s *store.Store) *State {
	st, _ := s.Reducer(Name).(*State)
	return st
}

// Name implements store.Reducer.
func (st *State) Name() string { return Name }

// Reset implements store.Reducer (logout / user switch).
func (st *State) Reset() {
	st.shares = map[string]api.WorkspaceShare{}
	st.touched = map[string]change{}
	st.gen++
}

// Apply implements store.Reducer for workspace_share.invited / updated / ended.
func (st *State) Apply(s *store.Store, ev realtime.Event) {
	v, err := ev.Decode()
	if err != nil || v == nil {
		return
	}
	switch d := v.(type) {
	case realtime.WorkspaceShareInvited:
		st.put(d.Share)
	case realtime.WorkspaceShareUpdated:
		if s.IsMe(d.Share.Owner.ID) || listsMe(s, d.Share) {
			st.put(d.Share)
		} else {
			st.drop(d.Share.ID)
		}
	case realtime.WorkspaceShareEnded:
		sh, ok := st.shares[d.ShareID]
		if !ok {
			st.touched[d.ShareID] = change{ended: &d}
			return
		}
		st.put(ended(sh, d))
	}
}

// ended is sh after workspace_share.ended. The event carries no time, so
// EndedAt stays as it was (the next fetch fills it).
func ended(sh api.WorkspaceShare, d realtime.WorkspaceShareEnded) api.WorkspaceShare {
	sh.Status = "ended"
	reason := d.Reason
	sh.EndReason = &reason
	return sh
}

// listsMe reports whether I am one of sh's (not removed) members.
func listsMe(s *store.Store, sh api.WorkspaceShare) bool {
	for _, m := range sh.Members {
		if s.IsMe(m.User.ID) {
			return true
		}
	}
	return false
}

func (st *State) put(sh api.WorkspaceShare) {
	st.shares[sh.ID] = sh
	st.touched[sh.ID] = change{share: sh}
}

func (st *State) drop(id string) {
	delete(st.shares, id)
	st.touched[id] = change{removed: true}
}

// Share returns the share with that id (live or ended) if known.
func (st *State) Share(id string) (api.WorkspaceShare, bool) {
	sh, ok := st.shares[id]
	return sh, ok
}

// Invited is the live shares someone else owns that I am in, newest first.
// The owner is told apart by the Store's Me (set by bootstrap).
func (st *State) Invited() []api.WorkspaceShare { return st.live(false) }

// Mine is the live shares I own, newest first.
func (st *State) Mine() []api.WorkspaceShare { return st.live(true) }

func (st *State) live(mine bool) []api.WorkspaceShare {
	out := []api.WorkspaceShare{}
	for _, sh := range st.shares {
		if sh.Status == "live" && st.s.IsMe(sh.Owner.ID) == mine {
			out = append(out, sh)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})
	return out
}

// ResyncMsg is the result of Resync / Backfill: my live shares and the live
// shares I am invited to. The app hands it to ApplyResync.
type ResyncMsg struct {
	Gen     uint64
	Mine    []api.WorkspaceShare
	Invited []api.WorkspaceShare
	Err     error
}

// Resync implements store.Resyncer: GET /v1/workspace-shares (owner, live)
// and (invited, live). Nothing is fetched while the workspace tool is off or
// without a client. It runs on the Update goroutine; the command only calls
// the API.
func (st *State) Resync(_ *store.Store, c *api.Client) tea.Cmd {
	if c == nil || !features.On(feature) {
		return nil
	}
	st.touched = map[string]change{}
	gen := st.gen
	return func() tea.Msg {
		ctx := context.Background()
		m := ResyncMsg{Gen: gen}
		if m.Mine, m.Err = c.ListWorkspaceShares(ctx, "owner", "live"); m.Err != nil {
			return m
		}
		m.Invited, m.Err = c.ListWorkspaceShares(ctx, "invited", "live")
		return m
	}
}

// Backfill implements store.Backfiller: workspace_share.updated is
// ephemeral, so a resumed socket refetches the lists like Resync.
func (st *State) Backfill(s *store.Store, c *api.Client) tea.Cmd { return st.Resync(s, c) }

// ApplyResync replaces the lists with m, then lays over what events changed
// since the Resync that made m. A result from before a Reset, or a failed
// one, changes nothing.
func (st *State) ApplyResync(m ResyncMsg) {
	if m.Gen != st.gen || m.Err != nil {
		return
	}
	shares := make(map[string]api.WorkspaceShare, len(m.Mine)+len(m.Invited))
	for _, sh := range m.Mine {
		shares[sh.ID] = sh
	}
	for _, sh := range m.Invited {
		shares[sh.ID] = sh
	}
	for id, ch := range st.touched {
		switch {
		case ch.removed:
			delete(shares, id)
		case ch.ended != nil:
			if sh, ok := shares[id]; ok {
				shares[id] = ended(sh, *ch.ended)
			}
		default:
			shares[id] = ch.share
		}
	}
	st.shares = shares
}
