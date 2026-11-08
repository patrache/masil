package chatstore

import (
	"slices"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
)

// apply is the realtime rule table (events.md 4.2 / 4.4). Messages are merged by
// ID, never by arrival order; events outside the loaded range are ignored, and an
// unknown message inside it is recorded for TakeMissing (the UI fetches it).
func (c *ChatStore) apply(s *store.Store, ev realtime.Event) {
	v, err := ev.Decode()
	if err != nil || v == nil {
		return
	}
	switch d := v.(type) {
	case realtime.Typing:
		c.applyTyping(d)
	case realtime.MessageEvent:
		switch ev.Type {
		case "message.created":
			c.messageCreated(d.Message)
		case "message.updated":
			c.messageUpdated(d.Message)
		}
	case realtime.MessageDeleted:
		c.messageDeleted(d)
	case realtime.ThreadUpdated:
		c.threadUpdated(d)
	case realtime.ReactionEvent:
		c.reaction(d, ev.Type == "reaction.added")
	case realtime.PinEvent:
		c.pinned(d, ev.Type == "message.pinned")
	case realtime.PollUpdated:
		c.pollUpdated(d.Poll)
	case realtime.ChannelDeleted:
		if _, ok := s.Channels[d.ChannelID]; !ok {
			c.dropChannel(d.ChannelID)
		}
	case realtime.SpaceDeleted, realtime.SpaceRemoved:
		c.dropGone(s)
	}
}

// inRange reports whether a top-level message id belongs in the channel window:
// the window holds a REST page and either covers id, or is an empty latest page.
func (c *ChatStore) inRange(channelID, id string) bool {
	w := c.windows[channelID]
	if w == nil || !c.loaded[channelID] {
		return false
	}
	return w.Covers(id) || (w.Len() == 0 && !w.HasMoreAfter())
}

// replyInRange is inRange for an open thread's reply window.
func (c *ChatStore) replyInRange(rootID, id string) bool {
	th := c.threads[rootID]
	if th == nil || !th.open {
		return false
	}
	return th.replies.Covers(id) || (th.replies.Len() == 0 && !th.replies.HasMoreAfter())
}

func (c *ChatStore) inRangeFor(channelID string, rootID *string, id string) bool {
	if rootID != nil {
		return c.replyInRange(*rootID, id)
	}
	return c.inRange(channelID, id)
}

// mutate applies fn to every copy of a message: the channel window item (or the
// reply window item when rootID is set), the thread root copy and the pin list
// copy. It reports whether the primary copy (window / reply window) was found.
func (c *ChatStore) mutate(channelID string, rootID *string, id string, fn func(*api.Message)) bool {
	found := false
	if rootID != nil {
		if th := c.threads[*rootID]; th != nil {
			found = th.replies.Update(id, fn)
		}
		return found
	}
	if w := c.windows[channelID]; w != nil {
		found = w.Update(id, fn)
	}
	if th := c.threads[id]; th != nil && th.root != nil {
		fn(th.root)
	}
	for i := range c.pins[channelID] {
		if c.pins[channelID][i].ID == id {
			fn(&c.pins[channelID][i])
		}
	}
	return found
}

// mergeLive replaces old with an event copy, keeping the per-recipient fields
// that fan-out fills with defaults (events.md 4.4): reactions[].me per emoji
// (false for emoji we did not have) and poll.my_votes.
func mergeLive(old, ev api.Message) api.Message {
	out := ev
	if len(ev.Reactions) > 0 {
		out.Reactions = make([]api.ReactionSummary, len(ev.Reactions))
		for i, r := range ev.Reactions {
			r.Me = false
			for _, o := range old.Reactions {
				if o.Emoji == r.Emoji {
					r.Me = o.Me
					break
				}
			}
			out.Reactions[i] = r
		}
	}
	if ev.Poll != nil {
		p := *ev.Poll
		if old.Poll != nil && old.Poll.ID == p.ID {
			p.MyVotes = old.Poll.MyVotes
		}
		out.Poll = &p
	}
	return out
}

// messageCreated inserts a new message in range. Its pending send (client_nonce)
// is dropped, unless the message found no place and its window still awaits a
// page that will show it (pages.go: the event is logged and re-applied then).
func (c *ChatStore) messageCreated(m api.Message) {
	c.clearTyper(m.ChannelID, m.ThreadRootID, m.Author.ID)

	var w *store.Window[api.Message]
	if m.ThreadRootID != nil {
		if !c.replyInRange(*m.ThreadRootID, m.ID) {
			c.settlePending(m, false)
			return
		}
		th := c.threads[*m.ThreadRootID]
		if th.channelID == "" {
			th.channelID = m.ChannelID
		}
		w = th.replies
	} else {
		if !c.inRange(m.ChannelID, m.ID) {
			c.settlePending(m, false)
			return
		}
		w = c.windows[m.ChannelID]
	}
	c.settlePending(m, true)
	c.removeMissing(m.ChannelID, m.ID)
	// Any copy we already hold (the REST send result, a later edit or a
	// tombstone, a resync snapshot) is at least as new as its creation, and
	// events from different requests can arrive out of order (events.md 6.4):
	// a late creation payload must never regress it, nor bring back a message
	// whose deletion arrived first.
	if _, ok := w.Get(m.ID); ok {
		return
	}
	if m, keep, _ := c.deletedCopy(m); keep {
		w.Upsert(m)
	}
}

// messageUpdated replaces every held copy with the event copy (mergeLive),
// except a copy that is newer (heldNewer: a tombstone or a later edit, events.md
// 6.4). An update of a message known deleted changes nothing.
func (c *ChatStore) messageUpdated(m api.Message) {
	if _, _, gone := c.deletedCopy(m); gone {
		return
	}
	found := c.mutate(m.ChannelID, m.ThreadRootID, m.ID, func(p *api.Message) {
		if !heldNewer(*p, m) {
			*p = mergeLive(*p, m)
		}
	})
	if !found && c.inRangeFor(m.ChannelID, m.ThreadRootID, m.ID) {
		c.addMissing(m.ChannelID, m.ID)
	}
}

func tombstone(p *api.Message) {
	p.Deleted = true
	p.Body = ""
	p.Mentions = nil
	p.MentionHere = false
	p.Reactions = nil
	p.Pinned = false
	p.Poll = nil
	p.Roll = nil
}

func (c *ChatStore) messageDeleted(d realtime.MessageDeleted) {
	c.noteDeleted(d.ChannelID, d.MessageID, d.Tombstone)
	var w *store.Window[api.Message]
	if d.ThreadRootID != nil {
		if th := c.threads[*d.ThreadRootID]; th != nil {
			w = th.replies
		}
	} else {
		w = c.windows[d.ChannelID]
		// A thread root keeps its thread; its copy becomes the placeholder either way.
		if th := c.threads[d.MessageID]; th != nil && th.root != nil {
			tombstone(th.root)
		}
	}
	if w != nil {
		if d.Tombstone {
			w.Update(d.MessageID, tombstone)
		} else {
			w.Remove(d.MessageID)
		}
	}
	c.removePin(d.ChannelID, d.MessageID)
	c.removeMissing(d.ChannelID, d.MessageID)
}

// threadUpdated sets the root's thread summary, unless the held summary is
// newer (a later last_reply_at: summaries of different requests can cross).
func (c *ChatStore) threadUpdated(d realtime.ThreadUpdated) {
	set := func(p *api.Message) {
		if old := p.Thread; old != nil && old.LastReplyAt != nil && d.Thread.LastReplyAt != nil &&
			old.LastReplyAt.After(*d.Thread.LastReplyAt) {
			return
		}
		t := d.Thread
		t.ParticipantIDs = slices.Clone(d.Thread.ParticipantIDs)
		p.Thread = &t
	}
	if w := c.windows[d.ChannelID]; w != nil {
		w.Update(d.RootID, set)
	}
	if th := c.threads[d.RootID]; th != nil && th.root != nil {
		set(th.root)
	}
}

func (c *ChatStore) reaction(d realtime.ReactionEvent, added bool) {
	mine := c.s.IsMe(d.UserID)
	found := c.mutate(d.ChannelID, d.ThreadRootID, d.MessageID, func(p *api.Message) {
		i := slices.IndexFunc(p.Reactions, func(r api.ReactionSummary) bool { return r.Emoji == d.Emoji })
		if d.Count <= 0 {
			if i >= 0 {
				p.Reactions = slices.Delete(slices.Clone(p.Reactions), i, i+1)
			}
			return
		}
		rs := slices.Clone(p.Reactions)
		if i < 0 {
			rs = append(rs, api.ReactionSummary{Emoji: d.Emoji})
			i = len(rs) - 1
		}
		rs[i].Count = d.Count
		if mine {
			rs[i].Me = added
		}
		p.Reactions = rs
	})
	if !found && c.inRangeFor(d.ChannelID, d.ThreadRootID, d.MessageID) {
		c.addMissing(d.ChannelID, d.MessageID)
	}
}

func (c *ChatStore) pinned(d realtime.PinEvent, on bool) {
	c.mutate(d.ChannelID, nil, d.MessageID, func(p *api.Message) { p.Pinned = on })
	if !on {
		c.removePin(d.ChannelID, d.MessageID)
		return
	}
	if !c.pinsLoaded[d.ChannelID] {
		return
	}
	var m api.Message
	ok := false
	if w := c.windows[d.ChannelID]; w != nil {
		m, ok = w.Get(d.MessageID)
	}
	if !ok {
		if th := c.threads[d.MessageID]; th != nil && th.root != nil {
			m, ok = *th.root, true
		}
	}
	if !ok {
		// held only in the pin list (a snapshot replaying the event): move it up
		if i := slices.IndexFunc(c.pins[d.ChannelID], func(p api.Message) bool { return p.ID == d.MessageID }); i >= 0 {
			m, ok = c.pins[d.ChannelID][i], true
		}
	}
	if !ok {
		// We cannot show a pin we hold no copy of: refetch the list next time.
		delete(c.pins, d.ChannelID)
		delete(c.pinsLoaded, d.ChannelID)
		return
	}
	m.Pinned = true
	c.removePin(d.ChannelID, d.MessageID)
	c.pins[d.ChannelID] = append([]api.Message{m}, c.pins[d.ChannelID]...)
}

func (c *ChatStore) pollUpdated(p api.Poll) {
	if p.ContextType != "message" {
		return
	}
	ch, root, ok := c.locate(p.ContextID)
	if !ok {
		return
	}
	c.mutate(ch, root, p.ContextID, func(m *api.Message) {
		np := p
		np.Options = slices.Clone(p.Options)
		if m.Poll != nil && m.Poll.ID == p.ID {
			np.MyVotes = m.Poll.MyVotes
		}
		m.Poll = &np
	})
}

// locate finds the channel (and thread root, for replies) holding a message id.
func (c *ChatStore) locate(id string) (channelID string, rootID *string, ok bool) {
	for ch, w := range c.windows {
		if _, ok := w.Get(id); ok {
			return ch, nil, true
		}
	}
	if th := c.threads[id]; th != nil && th.root != nil {
		return th.root.ChannelID, nil, true
	}
	for r, th := range c.threads {
		if m, ok := th.replies.Get(id); ok {
			return m.ChannelID, &r, true
		}
	}
	return "", nil, false
}

// FindMessage returns a message from any channel window, thread root or reply window.
func (c *ChatStore) FindMessage(id string) (api.Message, bool) {
	for _, w := range c.windows {
		if m, ok := w.Get(id); ok {
			return m, true
		}
	}
	if th := c.threads[id]; th != nil && th.root != nil {
		return *th.root, true
	}
	for _, th := range c.threads {
		if m, ok := th.replies.Get(id); ok {
			return m, true
		}
	}
	return api.Message{}, false
}

// clearTyper drops a user's typing indicator in one (channel, thread) scope.
func (c *ChatStore) clearTyper(channelID string, threadRootID *string, userID string) {
	t := c.typing[keyOf(channelID, threadRootID)]
	if t == nil {
		return
	}
	if _, ok := t.expires[userID]; !ok {
		return
	}
	delete(t.expires, userID)
	t.order = slices.DeleteFunc(t.order, func(u string) bool { return u == userID })
}

// ---- missing ----

func (c *ChatStore) addMissing(channelID, id string) {
	if !slices.Contains(c.missing[channelID], id) {
		c.missing[channelID] = append(c.missing[channelID], id)
	}
}

func (c *ChatStore) removeMissing(channelID, id string) {
	m := slices.DeleteFunc(c.missing[channelID], func(x string) bool { return x == id })
	if len(m) == 0 {
		delete(c.missing, channelID)
	} else {
		c.missing[channelID] = m
	}
}

// TakeMissing returns (and forgets) in-range message ids an event referred to
// that we hold no copy of; the caller fetches them with GetMessage.
func (c *ChatStore) TakeMissing(channelID string) []string {
	out := c.missing[channelID]
	delete(c.missing, channelID)
	return out
}

// ---- pins ----

// Pins returns the channel's pinned messages (a copy) and whether the list was loaded.
func (c *ChatStore) Pins(channelID string) (pins []api.Message, loaded bool) {
	return slices.Clone(c.pins[channelID]), c.pinsLoaded[channelID]
}

// PinsTicket is the ticket of a ListPins request built now (take it on the
// Update goroutine when the request is built; SetPinsFor replays from it).
func (c *ChatStore) PinsTicket() PageTicket { return PageTicket{since: c.logSeq} }

// SetPins stores a pin list known now (a local edit of the list): SetPinsFor
// with a ticket taken now, so nothing is replayed.
func (c *ChatStore) SetPins(channelID string, pins []api.Message) {
	c.SetPinsFor(channelID, c.PinsTicket(), pins)
}

// SetPinsFor stores the REST pin list (ListPins order) read with ticket t and
// keeps it live by pin events. The list is a snapshot read while the request
// was in flight, so the channel's pin, unpin and delete events logged since t
// are re-applied over it, like a history page (pages.go).
func (c *ChatStore) SetPinsFor(channelID string, t PageTicket, pins []api.Message) {
	c.observeAuthors(pins...)
	c.pins[channelID] = slices.Clone(pins)
	c.pinsLoaded[channelID] = true
	c.replayPinsSince(channelID, t.since)
}

func (c *ChatStore) removePin(channelID, id string) {
	if p, ok := c.pins[channelID]; ok {
		c.pins[channelID] = slices.DeleteFunc(p, func(m api.Message) bool { return m.ID == id })
	}
}

// ---- channel cleanup ----

func (t *threadState) channel() string {
	if t.channelID != "" {
		return t.channelID
	}
	if t.root != nil {
		return t.root.ChannelID
	}
	if t.replies.Len() > 0 {
		return t.replies.Items()[0].ChannelID
	}
	return ""
}

// dropChannel forgets every cache of a channel: window, pending sends, pins,
// typing, open state, missing ids and its threads. The active channel id is
// kept so the conversation view can tell the user the channel is gone.
func (c *ChatStore) dropChannel(channelID string) {
	c.dropWindow(channelID)
	delete(c.open, channelID)
	delete(c.pending, channelID)
	delete(c.polls, channelID)
	delete(c.pins, channelID)
	delete(c.pinsLoaded, channelID)
	delete(c.deleted, channelID)
	for k := range c.typing {
		if k.channelID == channelID {
			delete(c.typing, k)
		}
	}
	for id, th := range c.threads {
		if th.channel() == channelID {
			delete(c.threads, id)
		}
	}
}

// dropGone drops caches of every channel the core store no longer has
// (space.deleted / space.removed: the core already removed the space's channels).
func (c *ChatStore) dropGone(s *store.Store) {
	seen := map[string]bool{}
	note := func(ch string) {
		if ch != "" {
			seen[ch] = true
		}
	}
	for ch := range c.windows {
		note(ch)
	}
	for ch := range c.loaded {
		note(ch)
	}
	for ch := range c.open {
		note(ch)
	}
	for ch := range c.pending {
		note(ch)
	}
	for ch := range c.polls {
		note(ch)
	}
	for ch := range c.pinsLoaded {
		note(ch)
	}
	for ch := range c.missing {
		note(ch)
	}
	for ch := range c.deleted {
		note(ch)
	}
	for k := range c.typing {
		note(k.channelID)
	}
	for _, th := range c.threads {
		note(th.channel())
	}
	for ch := range seen {
		if _, ok := s.Channels[ch]; !ok {
			c.dropChannel(ch)
		}
	}
}
