package chatstore

import (
	"slices"

	"github.com/patrache/masil/internal/api"
)

// REST results are authoritative for the recipient: unlike realtime fan-out
// copies (mergeLive), they carry the caller's own reactions[].me and
// poll.my_votes, so they replace the stored copies as they are.

// ApplyPage is ApplyPageFor with a ticket taken now (nothing to replay, the
// current window): the page is treated as read at this instant.
func (c *ChatStore) ApplyPage(channelID string, q api.MessageQuery, w *api.MessageWindow) {
	c.ApplyPageFor(channelID, c.Ticket(channelID), q, w)
}

// ApplyPageFor merges a ListMessages page into the channel window: no anchor or
// Around replaces the window, Before prepends an older page and After appends a
// newer one (both merged by ID). An anchored page into a window that holds no
// REST page yet replaces it, so both has-more flags come from the server; an
// anchored page built from a window that was replaced since (t not Current) is
// discarded (false). The window becomes Loaded (and Fresh), sends whose
// client_nonce came back leave Pending, and everything logged since t was taken
// is re-applied over the page (pages.go).
func (c *ChatStore) ApplyPageFor(channelID string, t PageTicket, q api.MessageQuery, w *api.MessageWindow) bool {
	if w == nil {
		return false
	}
	anchored := q.Before != "" || q.After != ""
	if anchored && !c.Current(channelID, t) {
		return false
	}
	win := c.Window(channelID)
	switch {
	case q.Before != "" && c.loaded[channelID]:
		win.PrependOlder(w.Items, w.HasMoreBefore)
	case q.After != "" && c.loaded[channelID]:
		win.AppendNewer(w.Items, w.HasMoreAfter)
	default:
		win.Replace(w.Items, w.HasMoreBefore, w.HasMoreAfter)
		c.wgen[channelID] = c.bump()
		delete(c.stale, channelID)
	}
	c.loaded[channelID] = true
	for _, m := range w.Items {
		c.mergePending(m)
		c.refreshCopies(m)
		c.removeMissing(channelID, m.ID)
	}
	c.replaySince(t.since)
	return true
}

// ApplyThreadPage is ApplyThreadPageFor with a ticket taken now.
func (c *ChatStore) ApplyThreadPage(rootID string, q api.ThreadQuery, w *api.ThreadWindow) {
	c.ApplyThreadPageFor(rootID, c.ThreadTicket(rootID), q, w)
}

// ApplyThreadPageFor merges a GetThread page: the root copy is replaced (also
// in the channel window and pin list), replies follow ApplyPageFor's rules (a
// first page always replaces; a stale anchored page is discarded).
func (c *ChatStore) ApplyThreadPageFor(rootID string, t PageTicket, q api.ThreadQuery, w *api.ThreadWindow) bool {
	if w == nil {
		return false
	}
	anchored := q.Before != "" || q.After != ""
	if anchored && !c.ThreadCurrent(rootID, t) {
		return false
	}
	th := c.thread(rootID)
	had := th.root != nil
	root := w.Root
	th.root = &root
	if root.ChannelID != "" {
		th.channelID = root.ChannelID
	}
	switch {
	case q.Before != "" && had:
		th.replies.PrependOlder(w.Items, w.HasMoreBefore)
	case q.After != "" && had:
		th.replies.AppendNewer(w.Items, w.HasMoreAfter)
	default:
		th.replies.Replace(w.Items, w.HasMoreBefore, w.HasMoreAfter)
		th.gen = c.bump()
	}
	if root.ID != "" {
		if win := c.windows[root.ChannelID]; win != nil {
			win.Update(root.ID, func(p *api.Message) { *p = root })
		}
		c.refreshPin(root)
		c.removeMissing(root.ChannelID, root.ID)
	}
	for _, m := range w.Items {
		c.mergePending(m)
		c.removeMissing(m.ChannelID, m.ID)
	}
	c.replaySince(t.since)
	return true
}

// UpsertMessage lands a single REST message (send / reply / edit result,
// GetMessage of a missing id). It replaces the copy we hold unless that copy is
// newer (heldNewer: a tombstone, or a later edit, applied before a slower
// response landed), or is inserted when it belongs in the loaded range (never
// opening a gap past a window that has more after); a reply goes to its
// thread's reply window only. A message that found no window place is logged
// for page replay, and its pending send (client_nonce) stays while its window
// awaits a page that will show it (pages.go); otherwise the pending send is
// dropped.
func (c *ChatStore) UpsertMessage(m api.Message) {
	placed := c.upsert(m)
	c.settlePending(m, placed)
	if !placed {
		c.recordMessage(m)
	}
}

// upsert is UpsertMessage without the pending / log rules; it reports whether
// the window (or reply window) now holds m's id.
func (c *ChatStore) upsert(m api.Message) bool {
	defer c.removeMissing(m.ChannelID, m.ID)
	if m.ThreadRootID != nil {
		th := c.threads[*m.ThreadRootID]
		if th == nil {
			return false
		}
		if old, ok := th.replies.Get(m.ID); ok || c.replyInRange(*m.ThreadRootID, m.ID) {
			if !ok || !heldNewer(old, m) {
				th.replies.Upsert(m)
			}
			if th.channelID == "" {
				th.channelID = m.ChannelID
			}
			return true
		}
		return false
	}
	placed := false
	if w := c.windows[m.ChannelID]; w != nil {
		if old, ok := w.Get(m.ID); ok || c.inRange(m.ChannelID, m.ID) {
			if !ok || !heldNewer(old, m) {
				w.Upsert(m)
			}
			placed = true
		}
	}
	if th := c.threads[m.ID]; th != nil && th.root != nil && !heldNewer(*th.root, m) {
		r := m
		th.root = &r
	}
	for i := range c.pins[m.ChannelID] {
		if p := &c.pins[m.ChannelID][i]; p.ID == m.ID && !heldNewer(*p, m) {
			*p = m
		}
	}
	return placed
}

// heldNewer reports whether the stored copy old is newer than the incoming m
// (events.md 6.4: responses and events of different requests can cross): old
// is a tombstone, or carries a later edited_at.
func heldNewer(old, m api.Message) bool {
	if old.Deleted {
		return true
	}
	return old.EditedAt != nil && (m.EditedAt == nil || old.EditedAt.After(*m.EditedAt))
}

// mergePending drops the optimistic send whose client_nonce m carries.
func (c *ChatStore) mergePending(m api.Message) {
	if m.ClientNonce != nil && *m.ClientNonce != "" && c.pendingIndex(m.ChannelID, *m.ClientNonce) >= 0 {
		c.DropPending(m.ChannelID, *m.ClientNonce)
	}
}

// refreshCopies overwrites the thread root copy and the pin list copy of a
// top-level REST message (the window item is the caller's).
func (c *ChatStore) refreshCopies(m api.Message) {
	if m.ThreadRootID != nil {
		return
	}
	if th := c.threads[m.ID]; th != nil && th.root != nil {
		r := m
		th.root = &r
	}
	c.refreshPin(m)
}

func (c *ChatStore) refreshPin(m api.Message) {
	for i := range c.pins[m.ChannelID] {
		if c.pins[m.ChannelID][i].ID == m.ID {
			c.pins[m.ChannelID][i] = m
		}
	}
}

// locateAny is locate that also finds messages held only in a pin list.
func (c *ChatStore) locateAny(id string) (channelID string, rootID *string, ok bool) {
	if ch, root, ok := c.locate(id); ok {
		return ch, root, true
	}
	for ch, ps := range c.pins {
		if slices.ContainsFunc(ps, func(m api.Message) bool { return m.ID == id }) {
			return ch, nil, true
		}
	}
	return "", nil, false
}

// SetMyReaction applies my reaction optimistically to every copy of a message:
// on adds 1 and sets me (a new chip goes last), off subtracts 1 and clears me
// (the chip goes at 0). Repeating the current state is a no-op. It reports
// false when no copy of the message is held. The UI reverts a failed request
// with the opposite call.
func (c *ChatStore) SetMyReaction(messageID, emoji string, on bool) bool {
	ch, root, ok := c.locateAny(messageID)
	if !ok {
		return false
	}
	c.mutate(ch, root, messageID, func(p *api.Message) { toggleMine(p, emoji, on) })
	return true
}

func toggleMine(p *api.Message, emoji string, on bool) {
	i := slices.IndexFunc(p.Reactions, func(r api.ReactionSummary) bool { return r.Emoji == emoji })
	if on {
		if i >= 0 && p.Reactions[i].Me {
			return
		}
		rs := slices.Clone(p.Reactions)
		if i < 0 {
			rs = append(rs, api.ReactionSummary{Emoji: emoji})
			i = len(rs) - 1
		}
		rs[i].Count++
		rs[i].Me = true
		p.Reactions = rs
		return
	}
	if i < 0 || !p.Reactions[i].Me {
		return
	}
	rs := slices.Clone(p.Reactions)
	rs[i].Count--
	rs[i].Me = false
	if rs[i].Count <= 0 {
		rs = slices.Delete(rs, i, i+1)
	}
	p.Reactions = rs
}

// SetPoll stores a chat poll on every copy of its message. authoritative (the
// REST Vote / RetractVote / ClosePoll result) replaces my_votes too; otherwise
// the local my_votes of the same poll are kept, like poll.updated.
func (c *ChatStore) SetPoll(p api.Poll, authoritative bool) {
	if p.ContextType != "message" {
		return
	}
	ch, root, ok := c.locateAny(p.ContextID)
	if !ok {
		return
	}
	c.mutate(ch, root, p.ContextID, func(m *api.Message) {
		np := p
		np.Options = slices.Clone(p.Options)
		np.MyVotes = slices.Clone(p.MyVotes)
		if !authoritative && m.Poll != nil && m.Poll.ID == p.ID {
			np.MyVotes = m.Poll.MyVotes
		}
		m.Poll = &np
	})
}
