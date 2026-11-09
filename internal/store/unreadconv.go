package store

import (
	"sort"
	"time"

	"github.com/patrache/masil/internal/api"
)

// UnreadConv is one conversation with unread messages that the 알림함 lists in
// its "안 읽은 대화" section (client-architecture.md, 알림함): plain DM and
// channel messages make no inbox rows (api.md 5.18), so the client derives
// them from the live read state.
type UnreadConv struct {
	ChannelID string
	Kind      api.ChannelKind
	Unread    int       // My.UnreadCount (top-level only)
	Mentions  int       // My.MentionCount
	LastAt    time.Time // last_message_at (created_at when none)
	Anchor    string    // My.LastReadMessageID: the jump lands just after it ("" = never read)
	// Badged: the conversation adds one to InboxBadge. One listed only for
	// its mentions is left out while an unread message_mention row for it
	// counts it already (MentionCoverer).
	Badged bool
}

// MentionCoverer is optionally implemented by the Reducer that holds the
// notification inbox (feedstore).
type MentionCoverer interface {
	// CoversMentions reports whether an unread message_mention notification
	// for the channel is counted in NotifUnread: one is loaded, or the inbox
	// was never fetched (the server's row is assumed).
	CoversMentions(channelID string) bool
}

// mentionsCovered reports whether the channel's unread mentions already count
// in NotifUnread (nothing does once NotifUnread is 0, e.g. after x).
func (s *Store) mentionsCovered(channelID string) bool {
	if s.NotifUnread <= 0 {
		return false
	}
	for _, r := range s.reducerOrder {
		if c, ok := r.(MentionCoverer); ok {
			return c.CoversMentions(channelID)
		}
	}
	return true // no inbox holder: trust the server's message_mention row
}

// Quiet reports whether the channel is muted or its notify pref is "none":
// the 알림함's 안 읽은 대화 section leaves such conversations (and their
// threads) out.
func Quiet(ch *api.Channel) bool {
	return ch.My.Muted || notifyOf(ch) == "none"
}

// MessageDeleted reports whether message.deleted removed this message this
// session (a stored alert about it must not be previewed).
func (s *Store) MessageDeleted(id string) bool { return s.deleted.Has(id) }

// notifyOf is the channel's notify pref, with the server default when unset
// (dm / group_dm "all", text "mentions"; 0001_init.sql channel_prefs).
func notifyOf(ch *api.Channel) string {
	if ch.My.Notify != "" {
		return ch.My.Notify
	}
	if isDM(ch) {
		return "all"
	}
	return "mentions"
}

// unreadConv reports whether ch belongs in the section, and how.
func (s *Store) unreadConv(ch *api.Channel) (UnreadConv, bool) {
	my := ch.My
	notify := notifyOf(ch)
	if Quiet(ch) || (my.UnreadCount <= 0 && my.MentionCount <= 0) {
		return UnreadConv{}, false
	}
	c := UnreadConv{ChannelID: ch.ID, Kind: ch.Kind, Unread: my.UnreadCount, Mentions: my.MentionCount, LastAt: dmKey(ch)}
	if my.LastReadMessageID != nil {
		c.Anchor = *my.LastReadMessageID
	}
	switch ch.Kind {
	case api.ChannelDM:
		if p := s.dmPeer(ch); p != "" && s.IsBlocked(p) {
			return UnreadConv{}, false // hidden on the blocker's side (api.md 3.2), as the sidebar does
		}
		c.Badged = true // a 1:1 dm has no mention rows
	case api.ChannelGroupDM:
		c.Badged = my.UnreadCount > my.MentionCount || !s.mentionsCovered(ch.ID)
	case api.ChannelText:
		all := notify == "all" && my.UnreadCount > 0
		if !all && my.MentionCount <= 0 {
			return UnreadConv{}, false
		}
		c.Badged = (all && my.UnreadCount > my.MentionCount) || (my.MentionCount > 0 && !s.mentionsCovered(ch.ID))
	default:
		return UnreadConv{}, false
	}
	return c, true
}

// dmPeer is the id of the other member of a 1:1 DM ("" for a DM with myself).
func (s *Store) dmPeer(ch *api.Channel) string {
	for _, m := range ch.Members {
		if !s.IsMe(m.ID) {
			return m.ID
		}
	}
	return ""
}

// UnreadConversations lists, newest first, the conversations with unread
// messages worth a look: DMs and group DMs with unread messages, text channels
// with unread mentions, and text channels whose notify pref is "all" with any
// unread. Muted channels, notify "none" and 1:1 DMs with a blocked peer are
// left out. Reading a conversation (read_state.updated, a local read) takes it
// out at once.
func (s *Store) UnreadConversations() []UnreadConv {
	var out []UnreadConv
	for _, ch := range s.Channels {
		if c, ok := s.unreadConv(ch); ok {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].LastAt.Equal(out[j].LastAt) {
			return out[i].LastAt.After(out[j].LastAt)
		}
		return out[i].ChannelID > out[j].ChannelID
	})
	return out
}

// InboxBadge is the 알림함 badge: unread notifications plus the unread
// conversations no notification row already covers (UnreadConv.Badged).
func (s *Store) InboxBadge() int {
	n := s.NotifUnread
	for _, ch := range s.Channels {
		if c, ok := s.unreadConv(ch); ok && c.Badged {
			n++
		}
	}
	return n
}

// LastMessage is the newest top-level message this session saw created in the
// channel (the "안 읽은 대화" preview), kept current by edits and deletes.
// Messages from before the session (bootstrap counts only) are not known.
func (s *Store) LastMessage(channelID string) (api.Message, bool) {
	m, ok := s.lastMsg[channelID]
	return m, ok
}

// noteMessage keeps m as its channel's last message when it is top level and newest.
func (s *Store) noteMessage(m api.Message) {
	if m.ThreadRootID != nil || m.Kind == "system" || m.Deleted {
		return
	}
	if cur, ok := s.lastMsg[m.ChannelID]; ok && cur.ID > m.ID {
		return
	}
	s.lastMsg[m.ChannelID] = m
}

// updateLastMessage applies an edit to the kept last message.
func (s *Store) updateLastMessage(m api.Message) {
	if cur, ok := s.lastMsg[m.ChannelID]; ok && cur.ID == m.ID {
		if m.Deleted {
			delete(s.lastMsg, m.ChannelID)
			return
		}
		s.lastMsg[m.ChannelID] = m
	}
}

// forgetLastMessage drops a deleted last message.
func (s *Store) forgetLastMessage(channelID, messageID string) {
	if cur, ok := s.lastMsg[channelID]; ok && cur.ID == messageID {
		delete(s.lastMsg, channelID)
	}
}
