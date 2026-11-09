package app

import (
	"fmt"
	"log/slog"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui"
)

// Delivery queue (design A6.3, CA 5.3.2): the realtime goroutine and ctx.Post
// never wait for the UI. Both only append to eventQueue; one delivery goroutine
// hands the items to tea.Program.Send in the order they were queued. A stuck
// Update therefore never stalls the socket's read loop or heartbeat.

// bridgeQueueLimit bounds each lane of the queue (bridge msgs, posts).
const bridgeQueueLimit = 4096

// resyncNeededMsg is queued instead of the realtime events the bridge lane had
// to drop: Update runs the bootstrap (events.md 5.3) to catch up.
type resyncNeededMsg struct{}

// qLane says which lane an item belongs to.
type qLane uint8

const (
	laneBridge qLane = iota // ui.EventMsg, ui.RealtimeStateMsg, resyncNeededMsg
	lanePost                // ctx.Post / ctx.Coalesce
)

type qItem struct {
	msg  tea.Msg
	lane qLane
	key  string // Coalesce key ("" = plain post)
}

// eventQueue is the non-blocking FIFO in front of tea.Program.Send.
type eventQueue struct {
	send  func(tea.Msg)
	limit int

	mu        sync.Mutex
	cond      *sync.Cond
	items     []qItem // oldest first
	bridge    int     // bridge-lane items in items
	posts     int     // post-lane items in items
	resyncing bool    // a resyncNeededMsg is queued: bridge events are dropped until it is taken
	closed    bool
}

// newEventQueue starts the delivery goroutine; close stops it.
func newEventQueue(send func(tea.Msg), limit int) *eventQueue {
	q := &eventQueue{send: send, limit: max(1, limit)}
	q.cond = sync.NewCond(&q.mu)
	go q.run()
	return q
}

// run delivers items one at a time until close.
func (q *eventQueue) run() {
	for {
		q.mu.Lock()
		for len(q.items) == 0 && !q.closed {
			q.cond.Wait()
		}
		if q.closed {
			q.mu.Unlock()
			return
		}
		it := q.items[0]
		q.items[0] = qItem{}
		q.items = q.items[1:]
		switch it.lane {
		case laneBridge:
			q.bridge--
			if _, ok := it.msg.(resyncNeededMsg); ok {
				q.resyncing = false
			}
		case lanePost:
			q.posts--
		}
		q.mu.Unlock()
		q.send(it.msg)
	}
}

// push queues a bridge msg. When the bridge lane is full (the UI has not taken
// limit msgs) the queued events are dropped, the last RealtimeStateMsg is kept and
// one resyncNeededMsg follows; until that marker is delivered further events are
// dropped too (states still replace the kept one). Never blocks.
func (q *eventQueue) push(msg tea.Msg) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	if q.resyncing {
		if st, ok := msg.(ui.RealtimeStateMsg); ok {
			q.keepState(st)
		}
		return
	}
	if q.bridge < q.limit {
		q.append(qItem{msg: msg, lane: laneBridge})
		return
	}
	// Overflow: drop the bridge lane, keep the newest state, mark the resync.
	var last *ui.RealtimeStateMsg
	kept := q.items[:0]
	for _, it := range q.items {
		if it.lane != laneBridge {
			kept = append(kept, it)
			continue
		}
		if st, ok := it.msg.(ui.RealtimeStateMsg); ok {
			last = &st
		}
	}
	clear(q.items[len(kept):])
	q.items, q.bridge = kept, 0
	if st, ok := msg.(ui.RealtimeStateMsg); ok {
		last = &st
	}
	if last != nil {
		q.append(qItem{msg: *last, lane: laneBridge})
	}
	q.append(qItem{msg: resyncNeededMsg{}, lane: laneBridge})
	q.resyncing = true
	slog.Warn("realtime bridge queue overflowed; resync queued", "limit", q.limit)
}

// keepState replaces the state queued before the pending resync marker.
func (q *eventQueue) keepState(st ui.RealtimeStateMsg) {
	for i := len(q.items) - 1; i >= 0; i-- {
		if q.items[i].lane != laneBridge {
			continue
		}
		if _, ok := q.items[i].msg.(ui.RealtimeStateMsg); ok {
			q.items[i].msg = st
			return
		}
	}
	// No state queued yet: it goes right before the marker.
	for i, it := range q.items {
		if _, ok := it.msg.(resyncNeededMsg); ok && it.lane == laneBridge {
			q.items = append(q.items[:i], append([]qItem{{msg: st, lane: laneBridge}}, q.items[i:]...)...)
			q.bridge++
			return
		}
	}
}

// Post implements ui.Poster.
func (q *eventQueue) Post(msg tea.Msg) { q.post("", msg) }

// Coalesce implements ui.Poster.
func (q *eventQueue) Coalesce(key string, msg tea.Msg) { q.post(key, msg) }

func (q *eventQueue) post(key string, msg tea.Msg) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	if key != "" {
		for i := range q.items {
			if q.items[i].lane == lanePost && q.items[i].key == key {
				q.items[i].msg = msg
				return
			}
		}
	}
	if q.posts >= q.limit {
		q.dropOldestPost()
	}
	q.append(qItem{msg: msg, lane: lanePost, key: key})
}

// dropOldestPost removes the oldest plain post (the oldest coalesced one when
// every queued post has a key).
func (q *eventQueue) dropOldestPost() {
	victim := -1
	for i, it := range q.items {
		if it.lane != lanePost {
			continue
		}
		if victim < 0 {
			victim = i
		}
		if it.key == "" {
			victim = i
			break
		}
	}
	if victim < 0 {
		return
	}
	slog.Warn("ui post queue full; dropped the oldest post", "limit", q.limit)
	q.items = append(q.items[:victim], q.items[victim+1:]...)
	q.posts--
}

// append adds it at the tail and wakes the delivery goroutine (q.mu held).
func (q *eventQueue) append(it qItem) {
	q.items = append(q.items, it)
	switch it.lane {
	case laneBridge:
		q.bridge++
	case lanePost:
		q.posts++
	}
	q.cond.Signal()
}

// len is the number of queued (not yet taken) items.
func (q *eventQueue) len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// close stops delivery; queued items are dropped and later pushes ignored.
func (q *eventQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.items = nil
	q.cond.Broadcast()
	q.mu.Unlock()
}

var _ ui.Poster = (*eventQueue)(nil)

// appPoster is Context.Post: the app's queue once a sender exists (Run), else
// the msg is dropped (no program to deliver to).
type appPoster struct{ m *Model }

func (p appPoster) Post(msg tea.Msg) {
	if q := p.m.q.Load(); q != nil {
		q.Post(msg)
		return
	}
	slog.Debug("ui post without a running program; dropped", "msg_type", fmt.Sprintf("%T", msg))
}

func (p appPoster) Coalesce(key string, msg tea.Msg) {
	if q := p.m.q.Load(); q != nil {
		q.Coalesce(key, msg)
		return
	}
	slog.Debug("ui post without a running program; dropped", "msg_type", fmt.Sprintf("%T", msg))
}

// queue is the delivery queue, built on first use from m.send (Update goroutine
// or runProgram only); nil without a sender.
func (m *Model) queue() *eventQueue {
	if q := m.q.Load(); q != nil {
		return q
	}
	if m.send == nil {
		return nil
	}
	q := newEventQueue(m.send, bridgeQueueLimit)
	m.q.Store(q)
	return q
}

// closeQueue stops the delivery goroutine (end of Run).
func (m *Model) closeQueue() {
	if q := m.q.Swap(nil); q != nil {
		q.close()
	}
}
