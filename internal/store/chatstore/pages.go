package chatstore

import (
	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
)

// Pages that replace a window are snapshots read by the server at some point
// while the request was in flight, so realtime changes committed meanwhile may
// be missing from them (and were dropped on arrival when the window did not
// hold their range yet). Every applied realtime event and every REST message
// that found no place is therefore kept in a bounded log; a page carries the
// log position its request was built at (PageTicket) and, once applied,
// re-applies everything logged since. Every chat rule merges by ID with
// absolute values, so re-applying converges.
//
// A window also has a generation that every Replace (and drop) changes: a
// Before / After page built from one window is discarded when the window was
// replaced while it was in flight, instead of being merged into the other
// window with a silent gap.

// maxLog bounds the log; a request in flight for longer than maxLog events is
// replayed from the oldest entry still held.
const maxLog = 4096

// logEntry is one logged realtime event (msg == nil) or unplaced REST message.
type logEntry struct {
	seq uint64
	ev  realtime.Event
	msg *api.Message
}

// PageTicket ties a history request to the log position and window generation
// it was built from; take it on the Update goroutine when the request is built.
type PageTicket struct {
	gen, since uint64
}

// record logs a realtime event for page replay (typing is never refetched).
func (c *ChatStore) record(ev realtime.Event) {
	if c.replaying || ev.Type == "typing" {
		return
	}
	c.push(logEntry{ev: ev})
}

// recordMessage logs a REST message that found no window to land in.
func (c *ChatStore) recordMessage(m api.Message) {
	if c.replaying {
		return
	}
	c.push(logEntry{msg: &m})
}

func (c *ChatStore) push(e logEntry) {
	c.logSeq++
	e.seq = c.logSeq
	c.log = append(c.log, e)
	if len(c.log) >= 2*maxLog {
		c.log = append([]logEntry(nil), c.log[len(c.log)-maxLog:]...)
	}
}

// replaySince re-applies everything logged after since.
func (c *ChatStore) replaySince(since uint64) {
	if since >= c.logSeq || c.replaying {
		return
	}
	c.replaying = true
	defer func() { c.replaying = false }()
	start := max(0, len(c.log)-maxLog)
	for _, e := range c.log[start:] {
		if e.seq <= since {
			continue
		}
		if e.msg != nil {
			c.UpsertMessage(*e.msg)
		} else {
			c.apply(c.s, e.ev)
		}
	}
}

// replayPinsSince re-applies, over a pin list snapshot of channelID, the
// channel's pin events and deletions logged after since.
func (c *ChatStore) replayPinsSince(channelID string, since uint64) {
	if since >= c.logSeq || c.replaying {
		return
	}
	c.replaying = true
	defer func() { c.replaying = false }()
	start := max(0, len(c.log)-maxLog)
	for _, e := range c.log[start:] {
		if e.seq <= since || e.msg != nil {
			continue
		}
		switch e.ev.Type {
		case "message.pinned", "message.unpinned", "message.deleted":
		default:
			continue
		}
		v, err := e.ev.Decode()
		if err != nil {
			continue
		}
		switch d := v.(type) {
		case realtime.PinEvent:
			if d.ChannelID == channelID {
				c.pinned(d, e.ev.Type == "message.pinned")
			}
		case realtime.MessageDeleted:
			if d.ChannelID == channelID {
				c.removePin(d.ChannelID, d.MessageID)
			}
		}
	}
}

// bump gives a window a new generation.
func (c *ChatStore) bump() uint64 {
	c.genSeq++
	return c.genSeq
}

// Ticket is the ticket of a channel history request built now.
func (c *ChatStore) Ticket(channelID string) PageTicket {
	return PageTicket{gen: c.wgen[channelID], since: c.logSeq}
}

// ThreadTicket is the ticket of a thread page request built now.
func (c *ChatStore) ThreadTicket(rootID string) PageTicket {
	t := PageTicket{since: c.logSeq}
	if th := c.threads[rootID]; th != nil {
		t.gen = th.gen
	}
	return t
}

// Current reports whether the channel window is still the one t was built from.
func (c *ChatStore) Current(channelID string, t PageTicket) bool {
	return c.wgen[channelID] == t.gen
}

// ThreadCurrent reports whether the reply window is still the one t was built from.
func (c *ChatStore) ThreadCurrent(rootID string, t PageTicket) bool {
	var g uint64
	if th := c.threads[rootID]; th != nil {
		g = th.gen
	}
	return g == t.gen
}

// Fresh reports whether the channel window holds a REST page that no refetch
// has superseded since (a resync result that was never applied leaves it
// stale: still shown, but the next conversation view refetches it).
func (c *ChatStore) Fresh(channelID string) bool {
	return c.loaded[channelID] && !c.stale[channelID]
}

// IsOpen reports whether the channel window is marked open (SetOpen).
func (c *ChatStore) IsOpen(channelID string) bool {
	_, ok := c.open[channelID]
	return ok
}

// awaitsPage reports whether m's window may still take m from a later page (its
// first page has not landed, or it is not the latest page): an optimistic send
// that found no place keeps its pending row until then.
func (c *ChatStore) awaitsPage(m api.Message) bool {
	if m.ThreadRootID != nil {
		th := c.threads[*m.ThreadRootID]
		return th != nil && th.open && (th.root == nil || th.replies.HasMoreAfter())
	}
	w := c.windows[m.ChannelID]
	return w != nil && (!c.loaded[m.ChannelID] || w.HasMoreAfter())
}

// settlePending drops the optimistic send m carries the nonce of, unless m
// found no place and its window still awaits a page that will show it.
func (c *ChatStore) settlePending(m api.Message, placed bool) {
	if placed || !c.awaitsPage(m) {
		c.mergePending(m)
	}
}

// dropWindow forgets a channel window (not its pending sends or pins).
func (c *ChatStore) dropWindow(channelID string) {
	delete(c.windows, channelID)
	delete(c.loaded, channelID)
	delete(c.stale, channelID)
	delete(c.missing, channelID)
	c.wgen[channelID] = c.bump()
}
