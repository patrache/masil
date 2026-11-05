package store

import (
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
)

// Apply applies a realtime event to core entities, then every registered reducer.
// Unknown types and undecodable payloads are skipped by the core but still passed to reducers.
func (s *Store) Apply(ev realtime.Event) {
	if v, err := ev.Decode(); err == nil && v != nil {
		s.applying = true
		s.applyCore(ev, v)
		s.applying = false
		s.recordEvent(ev, v)
	}
	for _, r := range s.reducerOrder {
		r.Apply(s, ev)
	}
}

// eventTime is the event timestamp, or now when the frame had none.
func (s *Store) eventTime(ev realtime.Event) time.Time {
	if !ev.TS.IsZero() {
		return ev.TS
	}
	return s.now()
}

func (s *Store) applyCore(ev realtime.Event, v any) {
	switch d := v.(type) {
	// ---- messages (channel summary only) ----
	case realtime.MessageEvent:
		s.ObserveUser(d.Message.Author)
		if ev.Type == "message.created" {
			s.messageCreated(d.Message)
		}

	// ---- read state ----
	case realtime.ReadStateUpdated:
		if ch := s.Channels[d.State.ChannelID]; ch != nil {
			ch.My.UnreadCount = d.State.UnreadCount
			ch.My.MentionCount = d.State.MentionCount
			ch.My.LastReadMessageID = d.State.LastReadMessageID
			if d.State.LastMessageID != nil && (ch.LastMessageID == nil || *d.State.LastMessageID > *ch.LastMessageID) {
				ch.LastMessageID = d.State.LastMessageID
			}
			s.syncSpaceChannel(ch)
		}

	// ---- channels ----
	case realtime.ChannelEvent: // channel.created / channel.updated
		s.ObserveChannel(d.Channel)
	case realtime.ChannelDeleted:
		s.dropChannel(d.ChannelID)
	case realtime.ChannelMemberAdded:
		s.ObserveUser(d.User)
		if ch := s.Channels[d.ChannelID]; ch != nil && (isDM(ch) || !s.replaying) {
			if isDM(ch) {
				for _, m := range ch.Members {
					if m.ID == d.User.ID {
						return
					}
				}
				ch.Members = append(ch.Members, d.User)
			}
			ch.MemberCount++
			s.syncSpaceChannel(ch)
		}
	case realtime.ChannelMemberRemoved:
		if ch := s.Channels[d.ChannelID]; ch != nil && (isDM(ch) || !s.replaying) {
			if isDM(ch) {
				found := false
				out := make([]api.UserSummary, 0, len(ch.Members))
				for _, m := range ch.Members {
					if m.ID == d.UserID {
						found = true
						continue
					}
					out = append(out, m)
				}
				ch.Members = out
				if !found {
					return
				}
			}
			if ch.MemberCount > 0 {
				ch.MemberCount--
			}
			s.syncSpaceChannel(ch)
		}
	case realtime.ChannelPrefsUpdated:
		if ch := s.Channels[d.ChannelID]; ch != nil {
			ch.My.Notify = d.My.Notify
			ch.My.Muted = d.My.Muted
			s.syncSpaceChannel(ch)
		}

	// ---- spaces ----
	case realtime.SpaceJoined:
		s.adoptSpace(d.Space)
	case realtime.SpaceUpdated:
		if sd := s.Spaces[d.Space.ID]; sd != nil {
			role := sd.MyRole
			sd.Space = d.Space
			sd.MyRole = role
		}
	case realtime.SpaceDeleted:
		s.dropSpace(d.SpaceID)
	case realtime.SpaceRemoved:
		s.dropSpace(d.SpaceID)
	case realtime.SpaceMemberEvent: // member_joined / member_updated
		s.ObserveUser(d.Member.User)
		sd := s.Spaces[d.SpaceID]
		if sd == nil {
			return
		}
		switch ev.Type {
		case "space.member_joined":
			if !s.IsMe(d.Member.User.ID) && !s.replaying {
				sd.MemberCount++
			}
		case "space.member_updated":
			if s.IsMe(d.Member.User.ID) {
				sd.MyRole = d.Member.Role
			}
		}
	case realtime.SpaceMemberLeft:
		if sd := s.Spaces[d.SpaceID]; sd != nil && sd.MemberCount > 0 && !s.IsMe(d.UserID) && !s.replaying {
			sd.MemberCount--
		}
	case realtime.InviteReceived:
		s.ObserveUser(d.Invite.Inviter)
		s.ObserveUser(d.Invite.Invitee)
		for _, in := range s.DirectInvites {
			if in.ID == d.Invite.ID {
				return
			}
		}
		s.DirectInvites = append(s.DirectInvites, d.Invite)
	case realtime.InviteRemoved:
		out := make([]api.DirectInvite, 0, len(s.DirectInvites))
		for _, in := range s.DirectInvites {
			if in.ID != d.InviteID {
				out = append(out, in)
			}
		}
		s.DirectInvites = out

	// ---- friends, blocks, users ----
	case realtime.FriendRequestEvent:
		s.ObserveUser(d.Request.From)
		s.ObserveUser(d.Request.To)
		if ev.Type != "friend.request_received" {
			return
		}
		for _, r := range s.FriendReqIn {
			if r.ID == d.Request.ID {
				return
			}
		}
		s.FriendReqIn = append(s.FriendReqIn, d.Request)
	case realtime.FriendRequestRemoved:
		s.removeFriendRequest(func(r api.FriendRequest) bool { return r.ID == d.RequestID })
	case realtime.FriendAdded:
		s.ObserveUser(d.Friend.User)
		s.Friends[d.Friend.User.ID] = d.Friend
		s.removeFriendRequest(func(r api.FriendRequest) bool { return r.From.ID == d.Friend.User.ID })
	case realtime.FriendRemoved:
		delete(s.Friends, d.UserID)
	case realtime.BlockUpdated:
		if d.Blocked {
			if _, ok := s.Blocks[d.UserID]; !ok {
				s.Blocks[d.UserID] = s.eventTime(ev)
			}
		} else {
			delete(s.Blocks, d.UserID)
		}
	case realtime.UserUpdated:
		s.ObserveUser(d.User)
		if s.IsMe(d.User.ID) {
			s.Me.UserSummary = d.User
		}
		if f, ok := s.Friends[d.User.ID]; ok {
			f.User = d.User
			s.Friends[d.User.ID] = f
		}
	case realtime.SettingsUpdated:
		s.Settings = d.Settings
		if s.Me != nil {
			s.Me.Settings = d.Settings
		}

	// ---- presence, focus ----
	case realtime.PresenceSnapshot:
		at := s.eventTime(ev)
		for _, it := range d.Items {
			s.setPresence(it.UserID, it.Status, it.LastSeenAt, at)
		}
	case realtime.PresenceUpdated:
		s.setPresence(d.UserID, d.Status, d.LastSeenAt, s.eventTime(ev))
	case realtime.PresenceSelf:
		s.Self = d.Presence
		if s.Me != nil {
			s.Me.Presence = d.Presence
		}
	case realtime.FocusStarted:
		f := d.Focus
		s.Focus = &f
		s.Self.Focus = &f
	case realtime.FocusEnded:
		s.FocusEnded(d.Summary)

	// ---- notifications, alerts ----
	case realtime.NotificationCreated:
		s.NotifUnread = d.UnreadCount
		if d.Notification.Actor != nil {
			s.ObserveUser(*d.Notification.Actor)
		}
		s.hereMention(d.Notification)
	case realtime.NotificationsRead:
		s.NotifUnread = d.UnreadCount
	case realtime.AlertEvent:
		if d.Alert.Actor != nil {
			s.ObserveUser(*d.Alert.Actor)
		}
		s.Alerts.Push(d.Alert)
	}
}

// messageCreated applies the channel-summary part of message.created (events.md 4.2).
func (s *Store) messageCreated(m api.Message) {
	ch := s.Channels[m.ChannelID]
	if ch == nil {
		return
	}
	topLevel := m.ThreadRootID == nil
	if topLevel {
		if ch.LastMessageID == nil || m.ID >= *ch.LastMessageID {
			id, at := m.ID, m.CreatedAt
			ch.LastMessageID, ch.LastMessageAt = &id, &at
		}
		if isDM(ch) {
			s.moveDMFront(ch.ID)
		}
	}
	defer s.syncSpaceChannel(ch)
	if s.replaying && s.inSnapshot(ch.ID, m.ID) {
		return // the snapshot's counts already include it
	}
	if !s.counted.Add(m.ID) {
		return // duplicate delivery
	}
	if !topLevel || s.IsMe(m.Author.ID) || s.Viewing(ch.ID) {
		return
	}
	if ch.My.LastReadMessageID != nil && m.ID <= *ch.My.LastReadMessageID {
		return // already read elsewhere
	}
	if m.Kind != "system" && ch.My.UnreadCount < maxCount {
		ch.My.UnreadCount++
	}
	for _, mr := range m.Mentions {
		if s.IsMe(mr.UserID) {
			s.countMention(ch, m.ID)
			break
		}
	}
}

// hereMention counts an @here expansion (or any top-level mention) announced by notification.created.
func (s *Store) hereMention(n api.Notification) {
	if n.Type != "message_mention" || n.Context.MessageID == nil || n.Context.ChannelID == nil || n.Context.ThreadRootID != nil {
		return
	}
	ch := s.Channels[*n.Context.ChannelID]
	if ch == nil || s.Viewing(ch.ID) || s.replaying && s.inSnapshot(ch.ID, *n.Context.MessageID) {
		return
	}
	s.countMention(ch, *n.Context.MessageID)
	s.syncSpaceChannel(ch)
}

// countMention adds one mention for messageID, at most once per message.
func (s *Store) countMention(ch *api.Channel, messageID string) {
	if !s.mentioned.Add(messageID) {
		return
	}
	if ch.My.MentionCount < maxCount {
		ch.My.MentionCount++
	}
}

func (s *Store) setPresence(userID string, st api.PresenceStatus, lastSeen *time.Time, at time.Time) {
	s.Presence[userID] = PresenceEntry{Status: st, LastSeenAt: lastSeen, FetchedAt: at}
	if f, ok := s.Friends[userID]; ok {
		f.Presence = st
		s.Friends[userID] = f
	}
}

func (s *Store) removeFriendRequest(match func(api.FriendRequest) bool) {
	out := make([]api.FriendRequest, 0, len(s.FriendReqIn))
	for _, r := range s.FriendReqIn {
		if !match(r) {
			out = append(out, r)
		}
	}
	s.FriendReqIn = out
}

// adoptSpace stores a space addressed to me (space.joined, bootstrap): exact values, idempotent order.
func (s *Store) adoptSpace(d api.SpaceDetail) {
	sd := d
	sd.Channels = append([]api.Channel(nil), d.Channels...)
	if old := s.Spaces[d.ID]; old != nil {
		// channels no longer listed are gone
		keep := map[string]bool{}
		for _, c := range sd.Channels {
			keep[c.ID] = true
		}
		for cid, ch := range s.Channels {
			if ch.SpaceID != nil && *ch.SpaceID == d.ID && !keep[cid] {
				delete(s.Channels, cid)
			}
		}
	}
	s.Spaces[d.ID] = &sd
	found := false
	for _, id := range s.SpaceOrder {
		if id == d.ID {
			found = true
			break
		}
	}
	if !found {
		s.SpaceOrder = append(s.SpaceOrder, d.ID)
	}
	for _, c := range sd.Channels {
		s.putChannel(c)
	}
}
