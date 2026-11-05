package store

import (
	"context"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
)

// BootstrapMsg carries the events.md 5.3 step-1 REST results.
// A nil slice / nil Me means that request failed (Err is then set); an empty slice is a real empty result.
type BootstrapMsg struct {
	Me            *api.Me
	Spaces        []api.SpaceDetail
	DMs           []api.Channel
	Unreads       []api.UnreadState
	NotifUnread   int
	Friends       []api.Friend
	FriendReqIn   []api.FriendRequest
	DirectInvites []api.DirectInvite
	Blocks        []api.BlockEntry
	Err           error

	client *api.Client // the client Bootstrap used; handed to every Resyncer by ApplyBootstrap
}

const (
	bootstrapPageLimit = 100 // api.md 0: cursor page max
	bootstrapMaxPages  = 20  // safety cap: 2,000 items
)

// Bootstrap fetches core entities and emits BootstrapMsg. Requests run concurrently;
// the first error is reported in Err while every successful result is still delivered.
func Bootstrap(c *api.Client) tea.Cmd {
	return func() tea.Msg {
		return fetchBootstrap(context.Background(), c)
	}
}

func fetchBootstrap(ctx context.Context, c *api.Client) BootstrapMsg {
	m := BootstrapMsg{client: c}
	var (
		wg    sync.WaitGroup
		errMu sync.Mutex
	)
	fail := func(err error) {
		errMu.Lock()
		defer errMu.Unlock()
		if m.Err == nil {
			m.Err = err
		}
	}
	// Each goroutine writes only its own field of m.
	run := func(fn func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn()
		}()
	}

	run(func() {
		me, err := c.GetMe(ctx)
		if err != nil {
			fail(err)
			return
		}
		m.Me = me
	})
	run(func() {
		spaces, err := c.ListSpaces(ctx)
		if err != nil {
			fail(err)
			return
		}
		details := make([]*api.SpaceDetail, len(spaces))
		var swg sync.WaitGroup
		for i, sp := range spaces {
			swg.Add(1)
			go func() {
				defer swg.Done()
				d, err := c.GetSpace(ctx, sp.ID)
				if err != nil {
					fail(err)
					return
				}
				details[i] = d
			}()
		}
		swg.Wait()
		out := make([]api.SpaceDetail, 0, len(spaces))
		for _, d := range details {
			if d == nil {
				// One failed detail fails the whole result: a partial list would
				// drop that space and its channels from the store.
				return
			}
			out = append(out, *d)
		}
		m.Spaces = out
	})
	run(func() {
		items, err := allPages(ctx, c.ListDMs)
		if err != nil {
			fail(err)
		}
		m.DMs = items
	})
	run(func() {
		v, err := c.ListUnreads(ctx)
		if err != nil {
			fail(err)
			return
		}
		m.Unreads = v
	})
	run(func() {
		n, err := c.NotificationUnreadCount(ctx)
		if err != nil {
			fail(err)
			return
		}
		m.NotifUnread = n
	})
	run(func() {
		items, err := allPages(ctx, c.ListFriends)
		if err != nil {
			fail(err)
		}
		m.Friends = items
	})
	run(func() {
		v, err := c.ListFriendRequests(ctx, "incoming")
		if err != nil {
			fail(err)
			return
		}
		m.FriendReqIn = v
	})
	run(func() {
		v, err := c.ListMyDirectInvites(ctx)
		if err != nil {
			fail(err)
			return
		}
		m.DirectInvites = v
	})
	run(func() {
		v, err := c.ListBlocks(ctx)
		if err != nil {
			fail(err)
			return
		}
		m.Blocks = v
	})
	wg.Wait()
	return m
}

// allPages follows next_cursor from a limit=100 first page, up to bootstrapMaxPages pages
// (hitting the cap is not an error). Any failed page returns nil items with the error, so
// a partial list never replaces the store (BootstrapMsg: nil means the request failed).
func allPages[T any](ctx context.Context, list func(context.Context, api.PageOpts) (*api.Page[T], error)) ([]T, error) {
	var out []T
	p := api.PageOpts{Limit: bootstrapPageLimit}
	for i := 0; i < bootstrapMaxPages; i++ {
		pg, err := list(ctx, p)
		if err != nil {
			return nil, err
		}
		if out == nil {
			out = make([]T, 0, len(pg.Items))
		}
		out = append(out, pg.Items...)
		if pg.Done() || *pg.NextCursor == "" {
			break
		}
		p.Cursor = *pg.NextCursor
	}
	return out, nil
}

// ApplyBootstrap replaces core entities, then batches every Resyncer.
// Fields whose request failed (nil) keep their current value; NotifUnread is replaced only when Err is nil.
// Users is filled from every embedded UserSummary.
func (s *Store) ApplyBootstrap(m BootstrapMsg) tea.Cmd {
	if m.Me != nil {
		me := *m.Me
		s.Me = &me
		s.Settings = me.Settings
		s.Self = me.Presence
		if me.Presence.Focus != nil {
			f := *me.Presence.Focus
			s.Focus = &f
		} else {
			s.Focus = nil
		}
		s.ObserveUser(me.UserSummary)
	}

	if m.Spaces != nil || m.DMs != nil {
		// Rebuild channels: keep the half that did not arrive.
		old := s.Channels
		s.Channels = map[string]*api.Channel{}
		if m.Spaces == nil {
			for id, ch := range old {
				if !isDM(ch) {
					s.Channels[id] = ch
				}
			}
		}
		if m.DMs == nil {
			for id, ch := range old {
				if isDM(ch) {
					s.Channels[id] = ch
				}
			}
		}
	}
	if m.Spaces != nil {
		s.Spaces = map[string]*api.SpaceDetail{}
		s.SpaceOrder = nil
		for _, d := range m.Spaces {
			s.adoptSpace(d)
		}
	}
	if m.DMs != nil {
		order := make([]string, 0, len(m.DMs))
		for _, ch := range m.DMs {
			if _, dup := s.Channels[ch.ID]; dup {
				continue
			}
			s.putChannel(ch)
			order = append(order, ch.ID)
		}
		s.DMOrder = order // server order (last_message_at desc)
	}
	for _, u := range m.Unreads {
		ch := s.Channels[u.ChannelID]
		if ch == nil {
			continue
		}
		ch.My.UnreadCount = u.UnreadCount
		ch.My.MentionCount = u.MentionCount
		ch.My.LastReadMessageID = u.LastReadMessageID
		if u.LastMessageID != nil && (ch.LastMessageID == nil || *u.LastMessageID > *ch.LastMessageID) {
			ch.LastMessageID = u.LastMessageID
		}
		s.syncSpaceChannel(ch)
	}
	if m.Err == nil {
		s.NotifUnread = m.NotifUnread
	}
	if m.Friends != nil {
		s.Friends = make(map[string]api.Friend, len(m.Friends))
		for _, f := range m.Friends {
			s.ObserveUser(f.User)
			s.Friends[f.User.ID] = f
		}
	}
	if m.FriendReqIn != nil {
		s.FriendReqIn = append([]api.FriendRequest(nil), m.FriendReqIn...)
		for _, r := range m.FriendReqIn {
			s.ObserveUser(r.From)
			s.ObserveUser(r.To)
		}
	}
	if m.DirectInvites != nil {
		s.DirectInvites = append([]api.DirectInvite(nil), m.DirectInvites...)
		for _, in := range m.DirectInvites {
			s.ObserveUser(in.Inviter)
			s.ObserveUser(in.Invitee)
		}
	}
	if m.Blocks != nil {
		s.Blocks = make(map[string]time.Time, len(m.Blocks))
		for _, b := range m.Blocks {
			s.ObserveUser(b.User)
			s.Blocks[b.User.ID] = b.CreatedAt
		}
	}

	s.Sync.Err = m.Err
	if m.Err == nil {
		s.Sync.Bootstrapped = true
	}
	s.Sync.LastResync = s.now()

	var cmds []tea.Cmd
	for _, r := range s.reducerOrder {
		if rs, ok := r.(Resyncer); ok {
			cmds = append(cmds, rs.Resync(s, m.client))
		}
	}
	return tea.Batch(cmds...)
}
