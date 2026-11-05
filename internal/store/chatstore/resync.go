package chatstore

import (
	"context"
	"maps"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
)

const (
	resyncLimit   = 50               // page size of every refetch (same as the conversation view)
	resyncTimeout = 15 * time.Second // whole refetch command
	maxFlight     = 4096             // journaled events while a refetch is in flight
)

// ResyncResultMsg carries the windows refetched by Resync. Channels and Errs
// are keyed by channel id; a thread's window is in Threads by root id and its
// error in Errs under threadErrKey(root). Any chat component that receives it
// calls ApplyResync.
type ResyncResultMsg struct {
	Gen      uint64
	Channels map[string]*api.MessageWindow
	Threads  map[string]*api.ThreadWindow
	Errs     map[string]error
}

// threadErrKey is the ResyncResultMsg.Errs key of a thread refetch failure.
func threadErrKey(rootID string) string { return "thread:" + rootID }

// chatFlight journals realtime events applied while a refetch is in flight,
// so ApplyResync can re-apply them over the (older) REST snapshot
// (events.md 5.3 step 5). Typing is not journaled (it is never refetched).
type chatFlight struct {
	events   []realtime.Event
	overflow bool
}

func (c *ChatStore) record(ev realtime.Event) {
	f := c.flight
	if f == nil || c.replaying || ev.Type == "typing" || f.overflow {
		return
	}
	if len(f.events) >= maxFlight {
		f.events, f.overflow = nil, true
		return
	}
	f.events = append(f.events, ev)
}

type channelRefetch struct {
	id string
	q  api.MessageQuery
}

// Resync implements store.Resyncer (events.md 5.3 steps 2-3). On the Update
// goroutine it drops every cache of a channel or thread that is not open and
// every pin list (all are message caches; the pin overlay refetches), then
// returns one command that refetches each open channel window (latest page when
// at the bottom or without an anchor, else ?around=<anchor>) and each open
// thread, and yields a ResyncResultMsg. Optimistic sends are kept. It returns
// nil, using no generation, when there is nothing to fetch or no client.
func (c *ChatStore) Resync(s *store.Store, cl *api.Client) tea.Cmd {
	c.dropClosed()

	var chans []channelRefetch
	for _, id := range slices.Sorted(maps.Keys(c.open)) {
		o := c.open[id]
		q := api.MessageQuery{Limit: resyncLimit}
		if !o.atBottom && o.anchor != "" {
			q.Around = o.anchor
		}
		chans = append(chans, channelRefetch{id: id, q: q})
	}
	roots := slices.Sorted(maps.Keys(c.threads)) // only open threads are left
	if cl == nil || (len(chans) == 0 && len(roots) == 0) {
		return nil
	}

	c.gen++
	gen := c.gen
	if c.flight == nil {
		c.flight = &chatFlight{}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), resyncTimeout)
		defer cancel()
		out := ResyncResultMsg{
			Gen:      gen,
			Channels: map[string]*api.MessageWindow{},
			Threads:  map[string]*api.ThreadWindow{},
			Errs:     map[string]error{},
		}
		for _, ch := range chans {
			w, err := cl.ListMessages(ctx, ch.id, ch.q)
			if err != nil {
				out.Errs[ch.id] = err
				continue
			}
			out.Channels[ch.id] = w
		}
		for _, root := range roots {
			w, err := cl.GetThread(ctx, root, api.ThreadQuery{Limit: resyncLimit})
			if err != nil {
				out.Errs[threadErrKey(root)] = err
				continue
			}
			out.Threads[root] = w
		}
		return out
	}
}

// dropClosed forgets the message caches resync does not refetch.
func (c *ChatStore) dropClosed() {
	for ch := range c.loaded {
		if _, ok := c.open[ch]; !ok {
			delete(c.loaded, ch)
		}
	}
	for ch := range c.windows {
		if _, ok := c.open[ch]; !ok {
			delete(c.windows, ch)
		}
	}
	for ch := range c.missing {
		if _, ok := c.open[ch]; !ok {
			delete(c.missing, ch)
		}
	}
	for id, th := range c.threads {
		if !th.open {
			delete(c.threads, id)
		}
	}
	c.pins = map[string][]api.Message{}
	c.pinsLoaded = map[string]bool{}
}

// ApplyResync replaces the refetched windows (channels first, then threads;
// failed ones are kept as they are) and re-applies the realtime events
// journaled since the refetch started. A result whose generation was already
// applied, is older than an applied one, or predates Reset is ignored.
func (c *ChatStore) ApplyResync(m ResyncResultMsg) {
	if m.Gen <= c.applied {
		return
	}
	c.applied = m.Gen
	if m.Gen > c.gen {
		c.gen = m.Gen // a result not produced by Resync (tests) still advances the generation
	}
	for _, ch := range slices.Sorted(maps.Keys(m.Channels)) {
		c.ApplyPage(ch, api.MessageQuery{}, m.Channels[ch])
	}
	for _, root := range slices.Sorted(maps.Keys(m.Threads)) {
		c.ApplyThreadPage(root, api.ThreadQuery{}, m.Threads[root])
	}
	c.replayFlight(m.Gen == c.gen)
}

// replayFlight re-applies the journal; it is closed once the latest refetch
// landed (an older one keeps it for the newer snapshot). Every chat rule merges
// by ID with absolute values, so replaying events already applied converges.
func (c *ChatStore) replayFlight(closeIt bool) {
	f := c.flight
	if f == nil {
		return
	}
	if closeIt {
		c.flight = nil
	}
	if f.overflow {
		return
	}
	c.replaying = true
	defer func() { c.replaying = false }()
	for _, ev := range f.events {
		c.apply(c.s, ev)
	}
}
