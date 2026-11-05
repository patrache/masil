package chatstore

import (
	"context"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store"
)

const (
	resyncLimit   = 50 // page size of every refetch (same as the conversation view)
	resyncWorkers = 4  // refetch requests run side by side
)

// resyncRequestTimeout is the deadline of each refetch request (a var for tests).
var resyncRequestTimeout = 10 * time.Second

// ResyncResultMsg carries the windows refetched by Resync. Channels and Errs
// are keyed by channel id; a thread's window is in Threads by root id and its
// error in Errs under threadErrKey(root). Any chat component that receives it
// calls ApplyResync (a result nobody applies leaves the windows stale, see
// Fresh). Since is the replay log position the refetch started at.
type ResyncResultMsg struct {
	Gen      uint64
	Since    uint64
	Channels map[string]*api.MessageWindow
	Threads  map[string]*api.ThreadWindow
	Errs     map[string]error
}

// threadErrKey is the ResyncResultMsg.Errs key of a thread refetch failure.
func threadErrKey(rootID string) string { return "thread:" + rootID }

// ChannelErr is the refetch error of a channel window (nil = none).
func (m ResyncResultMsg) ChannelErr(channelID string) error { return m.Errs[channelID] }

// ThreadErr is the refetch error of a thread (nil = none).
func (m ResyncResultMsg) ThreadErr(rootID string) error { return m.Errs[threadErrKey(rootID)] }

type channelRefetch struct {
	id string
	q  api.MessageQuery
}

// Resync implements store.Resyncer (events.md 5.3 steps 2-3). On the Update
// goroutine it drops every cache of a channel or thread that is not open and
// every pin list (all are message caches; the pin overlay refetches), marks
// each open channel window stale (Fresh false) until its refetch lands, then
// returns one command that refetches each open channel window (latest page when
// at the bottom or without an anchor, else ?around=<anchor>) and each open
// thread — resyncWorkers requests side by side, each with its own deadline —
// and yields a ResyncResultMsg. Optimistic sends are kept. It returns nil,
// using no generation, when there is nothing to fetch or no client.
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
	gen, since := c.gen, c.logSeq
	for _, ch := range chans {
		if c.loaded[ch.id] {
			c.stale[ch.id] = true
		}
	}
	return func() tea.Msg {
		out := ResyncResultMsg{
			Gen:      gen,
			Since:    since,
			Channels: map[string]*api.MessageWindow{},
			Threads:  map[string]*api.ThreadWindow{},
			Errs:     map[string]error{},
		}
		var mu sync.Mutex
		var wg sync.WaitGroup
		jobs := make(chan func(), len(chans)+len(roots))
		for _, ch := range chans {
			jobs <- func() {
				ctx, cancel := context.WithTimeout(context.Background(), resyncRequestTimeout)
				defer cancel()
				w, err := cl.ListMessages(ctx, ch.id, ch.q)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					out.Errs[ch.id] = err
					return
				}
				out.Channels[ch.id] = w
			}
		}
		for _, root := range roots {
			jobs <- func() {
				ctx, cancel := context.WithTimeout(context.Background(), resyncRequestTimeout)
				defer cancel()
				w, err := cl.GetThread(ctx, root, api.ThreadQuery{Limit: resyncLimit})
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					out.Errs[threadErrKey(root)] = err
					return
				}
				out.Threads[root] = w
			}
		}
		close(jobs)
		for range min(resyncWorkers, len(chans)+len(roots)) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for job := range jobs {
					job()
				}
			}()
		}
		wg.Wait()
		return out
	}
}

// dropClosed forgets the message caches resync does not refetch.
func (c *ChatStore) dropClosed() {
	for ch := range c.windows {
		if _, ok := c.open[ch]; !ok {
			c.dropWindow(ch)
		}
	}
	for ch := range c.loaded {
		if _, ok := c.open[ch]; !ok {
			c.dropWindow(ch)
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

// ApplyResync replaces the refetched windows (channels first, then threads)
// and re-applies everything logged since the refetch started (pages.go). A
// window whose refetch failed is dropped (events.md 5.3: message caches are
// never patched across a gap), so its conversation view refetches it. A
// result whose generation was already applied, is older than an applied one,
// or predates Reset is ignored.
func (c *ChatStore) ApplyResync(m ResyncResultMsg) {
	if m.Gen <= c.applied {
		return
	}
	c.applied = m.Gen
	if m.Gen > c.gen {
		c.gen = m.Gen // a result not produced by Resync (tests) still advances the generation
	}
	now := PageTicket{since: c.logSeq}
	for _, ch := range slices.Sorted(maps.Keys(m.Channels)) {
		c.ApplyPageFor(ch, now, api.MessageQuery{}, m.Channels[ch])
	}
	for _, root := range slices.Sorted(maps.Keys(m.Threads)) {
		c.ApplyThreadPageFor(root, now, api.ThreadQuery{}, m.Threads[root])
	}
	for _, key := range slices.Sorted(maps.Keys(m.Errs)) {
		if root, ok := strings.CutPrefix(key, threadErrKey("")); ok {
			if th := c.threads[root]; th != nil {
				th.root = nil
				th.replies.Clear()
				th.gen = c.bump()
			}
			continue
		}
		c.dropWindow(key)
	}
	c.replaySince(m.Since)
}
