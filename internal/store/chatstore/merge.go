package chatstore

import (
	"slices"

	"github.com/patrache/masil/internal/api"
)

// REST results are authoritative for the recipient: unlike realtime fan-out
// copies (mergeLive), they carry the caller's own reactions[].me and
// poll.my_votes, so they replace the stored copies as they are.

// ApplyPage merges a ListMessages page into the channel window: no anchor or
// Around replaces the window, Before prepends an older page and After appends a
// newer one (both merged by ID). An anchored page into a window that holds no
// REST page yet replaces it, so both has-more flags come from the server. The
// window becomes Loaded, and sends whose client_nonce came back leave Pending.
func (c *ChatStore) ApplyPage(channelID string, q api.MessageQuery, w *api.MessageWindow) {
	if w == nil {
		return
	}
	win := c.Window(channelID)
	switch {
	case q.Before != "" && c.loaded[channelID]:
		win.PrependOlder(w.Items, w.HasMoreBefore)
	case q.After != "" && c.loaded[channelID]:
		win.AppendNewer(w.Items, w.HasMoreAfter)
	default:
		win.Replace(w.Items, w.HasMoreBefore, w.HasMoreAfter)
	}
	c.loaded[channelID] = true
	for _, m := range w.Items {
		c.mergePending(m)
		c.refreshCopies(m)
		c.removeMissing(channelID, m.ID)
	}
}

// ApplyThreadPage merges a GetThread page: the root copy is replaced (also in
// the channel window and pin list), replies follow ApplyPage's anchor rules
// (a first page always replaces).
func (c *ChatStore) ApplyThreadPage(rootID string, q api.ThreadQuery, w *api.ThreadWindow) {
	if w == nil {
		return
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
}

// UpsertMessage lands a single REST message (send / reply / edit result,
// GetMessage of a missing id). It replaces the copy we hold unless that copy is
// newer (heldNewer: a tombstone, or a later edit, applied before a slower
// response landed), or is inserted when it belongs in the loaded range (never
// opening a gap past a window that has more after); a reply goes to its
// thread's reply window only. The pending send with its client_nonce is
// dropped either way.
func (c *ChatStore) UpsertMessage(m api.Message) {
	c.mergePending(m)
	defer c.removeMissing(m.ChannelID, m.ID)
	if m.ThreadRootID != nil {
		th := c.threads[*m.ThreadRootID]
		if th == nil {
			return
		}
		if old, ok := th.replies.Get(m.ID); ok || c.replyInRange(*m.ThreadRootID, m.ID) {
			if !ok || !heldNewer(old, m) {
				th.replies.Upsert(m)
			}
			if th.channelID == "" {
				th.channelID = m.ChannelID
			}
		}
		return
	}
	if w := c.windows[m.ChannelID]; w != nil {
		if old, ok := w.Get(m.ID); ok || c.inRange(m.ChannelID, m.ID) {
			if !ok || !heldNewer(old, m) {
				w.Upsert(m)
			}
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
