package realtime

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// events.md 2장 / 4.2 example frame (message.created).
const sampleMessageFrame = `{ "type": "message.created", "seq": 1042, "ts": "2026-11-05T02:05:00.120Z",
  "data": { "message": { "id": "01JC9Z", "channel_id": "01JB", "author": { "id": "01JA", "handle": "sam", "display_name": "sam", "avatar_url": null, "status": null, "is_private": false, "suspended": false },
    "kind": "text", "body": "@minji 점심 먹고 ㄱㄱ", "mentions": [ { "user_id": "01JA2", "handle": "minji" } ], "mention_here": false,
    "thread_root_id": null, "thread": null, "reactions": [], "pinned": false, "poll": null, "roll": null, "system": null,
    "client_nonce": "01JC9X", "created_at": "2026-11-05T02:05:00.100Z", "edited_at": null, "deleted": false } } }`

func TestParseFrame(t *testing.T) {
	ev, err := parseFrame([]byte(sampleMessageFrame))
	if err != nil {
		t.Fatalf("parseFrame: %v", err)
	}
	if ev.Type != "message.created" {
		t.Errorf("Type = %q", ev.Type)
	}
	if ev.Seq == nil || *ev.Seq != 1042 {
		t.Errorf("Seq = %v, want 1042", ev.Seq)
	}
	wantTS := time.Date(2026, 11, 5, 2, 5, 0, 120_000_000, time.UTC)
	if !ev.TS.Equal(wantTS) {
		t.Errorf("TS = %v, want %v", ev.TS, wantTS)
	}
	if ev.Replayed {
		t.Errorf("Replayed must be false from parseFrame")
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(ev.Data, &data); err != nil {
		t.Fatalf("Data not raw JSON object: %v (%s)", err, ev.Data)
	}
	if _, ok := data["message"]; !ok {
		t.Errorf("Data missing message key: %s", ev.Data)
	}

	pong, err := parseFrame([]byte(`{"type":"pong"}`))
	if err != nil {
		t.Fatalf("parseFrame pong: %v", err)
	}
	if pong.Type != "pong" || pong.Seq != nil || !pong.TS.IsZero() {
		t.Errorf("pong = %+v", pong)
	}

	eph, err := parseFrame([]byte(`{"type":"typing","seq":null,"ts":"2026-11-05T02:05:00Z","data":{"channel_id":"C","thread_root_id":null,"user_id":"U","expires_in_ms":6000}}`))
	if err != nil {
		t.Fatalf("parseFrame typing: %v", err)
	}
	if eph.Seq != nil {
		t.Errorf("ephemeral Seq = %v, want nil", *eph.Seq)
	}

	for _, bad := range []string{``, `not json`, `[]`, `{"seq":1}`, `{"type":""}`, `{"type":"x","seq":"1"}`} {
		if _, err := parseFrame([]byte(bad)); err == nil {
			t.Errorf("parseFrame(%q) = nil error, want error", bad)
		}
	}
}

const userJSON = `{"id":"U1","handle":"sam","display_name":"샘","avatar_url":null,"status":null,"is_private":false,"suspended":false}`

func TestDecodeAllTypes(t *testing.T) {
	cases := []struct {
		typ   string
		data  string
		check func(t *testing.T, v any)
	}{
		{"hello", `{"connection_id":"C1","session_id":"S1","user_id":"U1","mode":"full","device_id":"D1","epoch":"E1","seq":1041,"oldest_seq":12,"heartbeat_interval_ms":25000,"heartbeat_timeout_ms":120000,"local_tui_connected":true,"server_time":"2026-11-05T02:03:04.567Z","limits":{"max_frame_bytes":8192,"watch_topics_max":50,"typing_min_interval_ms":3000}}`,
			func(t *testing.T, v any) {
				h := v.(Hello)
				if h.Epoch != "E1" || h.Seq != 1041 || h.OldestSeq != 12 || h.HeartbeatIntervalMS != 25000 || h.HeartbeatTimeoutMS != 120000 || !h.LocalTUIConnected || h.DeviceID != "D1" || h.Mode != "full" {
					t.Errorf("hello = %+v", h)
				}
				if h.Limits != (HelloLimits{MaxFrameBytes: 8192, WatchTopicsMax: 50, TypingMinIntervalMS: 3000}) {
					t.Errorf("hello limits = %+v", h.Limits)
				}
				if h.ServerTime.IsZero() {
					t.Errorf("hello server_time zero")
				}
			}},
		{"ready", `{"resumed":false,"resync_required":true,"replayed":0,"seq":1041,"epoch":"E2"}`,
			func(t *testing.T, v any) {
				r := v.(Ready)
				if r.Resumed || !r.ResyncRequired || r.Seq != 1041 || r.Epoch != "E2" {
					t.Errorf("ready = %+v", r)
				}
			}},
		{"reply", `{"ref":"c-18","ok":false,"error":{"code":"not_found","message":"channel not found"}}`,
			func(t *testing.T, v any) {
				r := v.(Reply)
				if r.Ref != "c-18" || r.OK || r.Error == nil || *r.Error != (ReplyError{Code: "not_found", Message: "channel not found"}) {
					t.Errorf("reply = %+v", r)
				}
			}},
		{"session.revoked", `{"session_id":"S1","reason":"password_changed"}`,
			func(t *testing.T, v any) {
				if s := v.(SessionRevoked); s.SessionID != "S1" || s.Reason != "password_changed" {
					t.Errorf("session.revoked = %+v", s)
				}
			}},
		{"message.created", `{"message":{"id":"M1","channel_id":"C1","author":` + userJSON + `,"kind":"text","body":"안녕"}}`,
			func(t *testing.T, v any) {
				m := v.(MessageEvent)
				if m.Message.ID != "M1" || m.Message.Body != "안녕" || m.Message.Author.ID != "U1" {
					t.Errorf("message.created = %+v", m)
				}
			}},
		{"message.updated", `{"message":{"id":"M2","channel_id":"C1","kind":"text","body":"수정"}}`,
			func(t *testing.T, v any) {
				if m := v.(MessageEvent); m.Message.ID != "M2" {
					t.Errorf("message.updated = %+v", m)
				}
			}},
		{"message.deleted", `{"channel_id":"C1","message_id":"M1","thread_root_id":"R1","tombstone":true}`,
			func(t *testing.T, v any) {
				d := v.(MessageDeleted)
				if d.ChannelID != "C1" || d.MessageID != "M1" || d.ThreadRootID == nil || *d.ThreadRootID != "R1" || !d.Tombstone {
					t.Errorf("message.deleted = %+v", d)
				}
			}},
		{"thread.updated", `{"channel_id":"C1","root_id":"R1","thread":{"reply_count":3,"last_reply_at":null,"participant_ids":["U1"]}}`,
			func(t *testing.T, v any) {
				u := v.(ThreadUpdated)
				if u.RootID != "R1" || u.Thread.ReplyCount != 3 {
					t.Errorf("thread.updated = %+v", u)
				}
			}},
		{"reaction.added", `{"channel_id":"C1","message_id":"M1","thread_root_id":null,"emoji":"👍","user_id":"U2","count":2}`,
			func(t *testing.T, v any) {
				r := v.(ReactionEvent)
				if r.Emoji != "👍" || r.UserID != "U2" || r.Count != 2 || r.ThreadRootID != nil {
					t.Errorf("reaction.added = %+v", r)
				}
			}},
		{"reaction.removed", `{"channel_id":"C1","message_id":"M1","thread_root_id":null,"emoji":"👍","user_id":"U2","count":0}`,
			func(t *testing.T, v any) {
				if r := v.(ReactionEvent); r.Count != 0 || r.MessageID != "M1" {
					t.Errorf("reaction.removed = %+v", r)
				}
			}},
		{"message.pinned", `{"channel_id":"C1","message_id":"M1","pinned_by":"U"}`,
			func(t *testing.T, v any) {
				p := v.(PinEvent)
				if p.By != "U" || p.ChannelID != "C1" || p.MessageID != "M1" {
					t.Errorf("message.pinned = %+v", p)
				}
			}},
		{"message.unpinned", `{"channel_id":"C1","message_id":"M1","unpinned_by":"V"}`,
			func(t *testing.T, v any) {
				if p := v.(PinEvent); p.By != "V" || p.MessageID != "M1" {
					t.Errorf("message.unpinned = %+v", p)
				}
			}},
		{"poll.updated", `{"poll":{"id":"P1","context_type":"post","context_id":"PO1","question":"점심?","options":[],"my_votes":[]}}`,
			func(t *testing.T, v any) {
				if p := v.(PollUpdated); p.Poll.ID != "P1" || p.Poll.ContextType != "post" {
					t.Errorf("poll.updated = %+v", p)
				}
			}},
		{"typing", `{"channel_id":"C1","thread_root_id":null,"user_id":"U2","expires_in_ms":6000}`,
			func(t *testing.T, v any) {
				if ty := v.(Typing); ty.ChannelID != "C1" || ty.UserID != "U2" || ty.ExpiresInMS != 6000 {
					t.Errorf("typing = %+v", ty)
				}
			}},
		{"read_state.updated", `{"state":{"channel_id":"C1","unread_count":0,"mention_count":0,"last_message_id":"M9","last_read_message_id":"M9"}}`,
			func(t *testing.T, v any) {
				if r := v.(ReadStateUpdated); r.State.ChannelID != "C1" || r.State.LastReadMessageID == nil {
					t.Errorf("read_state.updated = %+v", r)
				}
			}},
		{"channel.created", `{"channel":{"id":"C2","kind":"text","space_id":"S1","name":"개발","topic":""}}`,
			func(t *testing.T, v any) {
				if c := v.(ChannelEvent); c.Channel.ID != "C2" {
					t.Errorf("channel.created = %+v", c)
				}
			}},
		{"channel.updated", `{"channel":{"id":"C3","kind":"text","topic":"새 주제"}}`,
			func(t *testing.T, v any) {
				if c := v.(ChannelEvent); c.Channel.ID != "C3" || c.Channel.Topic != "새 주제" {
					t.Errorf("channel.updated = %+v", c)
				}
			}},
		{"channel.deleted", `{"channel_id":"C2","space_id":null,"reason":"removed"}`,
			func(t *testing.T, v any) {
				if c := v.(ChannelDeleted); c.ChannelID != "C2" || c.SpaceID != nil || c.Reason != "removed" {
					t.Errorf("channel.deleted = %+v", c)
				}
			}},
		{"channel.member_added", `{"channel_id":"C2","user":` + userJSON + `}`,
			func(t *testing.T, v any) {
				if c := v.(ChannelMemberAdded); c.ChannelID != "C2" || c.User.Handle != "sam" {
					t.Errorf("channel.member_added = %+v", c)
				}
			}},
		{"channel.member_removed", `{"channel_id":"C2","user_id":"U3"}`,
			func(t *testing.T, v any) {
				if c := v.(ChannelMemberRemoved); c.ChannelID != "C2" || c.UserID != "U3" {
					t.Errorf("channel.member_removed = %+v", c)
				}
			}},
		{"channel.prefs_updated", `{"channel_id":"C2","my":{"notify":"mentions","muted":true,"last_read_message_id":null,"unread_count":4,"mention_count":1}}`,
			func(t *testing.T, v any) {
				c := v.(ChannelPrefsUpdated)
				if c.ChannelID != "C2" || c.My.Notify != "mentions" || !c.My.Muted || c.My.UnreadCount != 4 {
					t.Errorf("channel.prefs_updated = %+v", c)
				}
			}},
		{"space.joined", `{"space":{"id":"S1","name":"노바","owner_id":"U1","my_role":"owner","channels":[{"id":"C1","kind":"text"}]}}`,
			func(t *testing.T, v any) {
				s := v.(SpaceJoined)
				if s.Space.ID != "S1" || s.Space.OwnerID != "U1" || len(s.Space.Channels) != 1 {
					t.Errorf("space.joined = %+v", s)
				}
			}},
		{"space.updated", `{"space":{"id":"S1","name":"노바2"}}`,
			func(t *testing.T, v any) {
				if s := v.(SpaceUpdated); s.Space.ID != "S1" || s.Space.Name != "노바2" {
					t.Errorf("space.updated = %+v", s)
				}
			}},
		{"space.deleted", `{"space_id":"S1"}`,
			func(t *testing.T, v any) {
				if s := v.(SpaceDeleted); s.SpaceID != "S1" {
					t.Errorf("space.deleted = %+v", s)
				}
			}},
		{"space.removed", `{"space_id":"S1","reason":"kicked"}`,
			func(t *testing.T, v any) {
				if s := v.(SpaceRemoved); s.SpaceID != "S1" || s.Reason != "kicked" {
					t.Errorf("space.removed = %+v", s)
				}
			}},
		{"space.member_joined", `{"space_id":"S1","member":{"user":` + userJSON + `,"role":"member","joined_at":"2026-11-05T02:00:00Z"}}`,
			func(t *testing.T, v any) {
				if s := v.(SpaceMemberEvent); s.SpaceID != "S1" || s.Member.User.ID != "U1" {
					t.Errorf("space.member_joined = %+v", s)
				}
			}},
		{"space.member_updated", `{"space_id":"S1","member":{"user":` + userJSON + `,"role":"admin","joined_at":"2026-11-05T02:00:00Z"}}`,
			func(t *testing.T, v any) {
				if s := v.(SpaceMemberEvent); s.Member.Role != "admin" {
					t.Errorf("space.member_updated = %+v", s)
				}
			}},
		{"space.member_left", `{"space_id":"S1","user_id":"U4","reason":"banned"}`,
			func(t *testing.T, v any) {
				if s := v.(SpaceMemberLeft); s.UserID != "U4" || s.Reason != "banned" {
					t.Errorf("space.member_left = %+v", s)
				}
			}},
		{"space.invite_received", `{"invite":{"id":"I1","space":{"id":"S1","name":"노바"},"inviter":` + userJSON + `,"status":"pending"}}`,
			func(t *testing.T, v any) {
				if i := v.(InviteReceived); i.Invite.ID != "I1" || i.Invite.Space.ID != "S1" {
					t.Errorf("space.invite_received = %+v", i)
				}
			}},
		{"space.invite_removed", `{"invite_id":"I1","reason":"declined"}`,
			func(t *testing.T, v any) {
				if i := v.(InviteRemoved); i.InviteID != "I1" || i.Reason != "declined" {
					t.Errorf("space.invite_removed = %+v", i)
				}
			}},
		{"friend.request_received", `{"request":{"id":"FR1","from":` + userJSON + `,"status":"pending"}}`,
			func(t *testing.T, v any) {
				if f := v.(FriendRequestEvent); f.Request.ID != "FR1" || f.Request.From.ID != "U1" {
					t.Errorf("friend.request_received = %+v", f)
				}
			}},
		{"friend.request_accepted", `{"request":{"id":"FR2","status":"accepted"}}`,
			func(t *testing.T, v any) {
				if f := v.(FriendRequestEvent); f.Request.ID != "FR2" || f.Request.Status != "accepted" {
					t.Errorf("friend.request_accepted = %+v", f)
				}
			}},
		{"friend.request_removed", `{"request_id":"FR1","reason":"canceled"}`,
			func(t *testing.T, v any) {
				if f := v.(FriendRequestRemoved); f.RequestID != "FR1" || f.Reason != "canceled" {
					t.Errorf("friend.request_removed = %+v", f)
				}
			}},
		{"friend.added", `{"friend":{"user":` + userJSON + `,"since":"2026-11-05T02:00:00Z","presence":"online"}}`,
			func(t *testing.T, v any) {
				if f := v.(FriendAdded); f.Friend.User.ID != "U1" || f.Friend.Presence != "online" {
					t.Errorf("friend.added = %+v", f)
				}
			}},
		{"friend.removed", `{"user_id":"U5"}`,
			func(t *testing.T, v any) {
				if f := v.(FriendRemoved); f.UserID != "U5" {
					t.Errorf("friend.removed = %+v", f)
				}
			}},
		{"block.updated", `{"user_id":"U6","blocked":true}`,
			func(t *testing.T, v any) {
				if b := v.(BlockUpdated); b.UserID != "U6" || !b.Blocked {
					t.Errorf("block.updated = %+v", b)
				}
			}},
		{"user.updated", `{"user":` + userJSON + `}`,
			func(t *testing.T, v any) {
				if u := v.(UserUpdated); u.User.DisplayName != "샘" {
					t.Errorf("user.updated = %+v", u)
				}
			}},
		{"settings.updated", `{"settings":{"dm_policy":"friends_only","is_private":true,"focus_default_min":50}}`,
			func(t *testing.T, v any) {
				s := v.(SettingsUpdated)
				if s.Settings.DMPolicy != "friends_only" || !s.Settings.IsPrivate || s.Settings.FocusDefaultMin != 50 {
					t.Errorf("settings.updated = %+v", s)
				}
			}},
		{"presence.snapshot", `{"items":[{"user_id":"U1","status":"online","last_seen_at":null},{"user_id":"U2","status":"offline","last_seen_at":"2026-11-05T01:00:00Z"}]}`,
			func(t *testing.T, v any) {
				if p := v.(PresenceSnapshot); len(p.Items) != 2 {
					t.Errorf("presence.snapshot = %+v", p)
				}
			}},
		{"presence.updated", `{"user_id":"U2","status":"away","last_seen_at":"2026-11-05T01:00:00Z"}`,
			func(t *testing.T, v any) {
				p := v.(PresenceUpdated)
				if p.UserID != "U2" || p.Status != "away" || p.LastSeenAt == nil {
					t.Errorf("presence.updated = %+v", p)
				}
			}},
		{"presence.self", `{"presence":{"status":"dnd","mode":"dnd","focus":null}}`,
			func(t *testing.T, v any) {
				if p := v.(PresenceSelf); p.Presence.Status != "dnd" || p.Presence.Mode != "dnd" {
					t.Errorf("presence.self = %+v", p)
				}
			}},
		{"focus.started", `{"focus":{"started_at":"2026-11-05T02:00:00Z","ends_at":"2026-11-05T02:50:00Z"}}`,
			func(t *testing.T, v any) {
				if f := v.(FocusStarted); f.Focus.EndsAt.Sub(f.Focus.StartedAt) != 50*time.Minute {
					t.Errorf("focus.started = %+v", f)
				}
			}},
		{"focus.ended", `{"summary":{"started_at":"2026-11-05T02:00:00Z","ended_at":"2026-11-05T02:50:00Z"}}`,
			func(t *testing.T, v any) {
				if f := v.(FocusEnded); f.Summary.EndedAt.IsZero() {
					t.Errorf("focus.ended = %+v", f)
				}
			}},
		{"notification.created", `{"notification":{"id":"N1","type":"message_mention","preview":"안녕"},"unread_count":7}`,
			func(t *testing.T, v any) {
				n := v.(NotificationCreated)
				if n.Notification.ID != "N1" || n.UnreadCount != 7 {
					t.Errorf("notification.created = %+v", n)
				}
			}},
		{"notifications.read", `{"ids":null,"all":true,"before":null,"unread_count":0}`,
			func(t *testing.T, v any) {
				n := v.(NotificationsRead)
				if n.IDs != nil || !n.All || n.Before != nil || n.UnreadCount != 0 {
					t.Errorf("notifications.read = %+v", n)
				}
			}},
		{"alert", `{"alert":{"id":"A1","kind":"mention","title":"sam · 노바","body":"@minji","actor":` + userJSON + `,"target":{"type":"message","id":"M1","channel_id":"C1","message_id":"M1","thread_root_id":null,"post_id":null,"space_id":"S1"},"created_at":"2026-11-05T02:05:00.100Z","suppressed":false,"suppressed_reason":null}}`,
			func(t *testing.T, v any) {
				a := v.(AlertEvent)
				if a.Alert.ID != "A1" || a.Alert.Kind != "mention" || a.Alert.Target.MessageID == nil || a.Alert.Suppressed {
					t.Errorf("alert = %+v", a)
				}
			}},
		{"timeline.item_created", `{"item":{"id":"P1","type":"post","post":{"id":"P1","body":"첫 글"},"reposted_by":null,"at":"2026-11-05T02:00:00Z"}}`,
			func(t *testing.T, v any) {
				if i := v.(TimelineItemCreated); i.Item.ID != "P1" || i.Item.Post.Body != "첫 글" {
					t.Errorf("timeline.item_created = %+v", i)
				}
			}},
		{"post.deleted", `{"post_id":"P1"}`,
			func(t *testing.T, v any) {
				if p := v.(PostDeleted); p.PostID != "P1" {
					t.Errorf("post.deleted = %+v", p)
				}
			}},
		{"post.stats", `{"post_id":"P1","like_count":1,"repost_count":2,"reply_count":3,"quote_count":4}`,
			func(t *testing.T, v any) {
				if p := v.(PostStats); p != (PostStats{PostID: "P1", LikeCount: 1, RepostCount: 2, ReplyCount: 3, QuoteCount: 4}) {
					t.Errorf("post.stats = %+v", p)
				}
			}},
		{"post.reply_created", `{"parent_id":"P1","post":{"id":"P2","body":"답글"}}`,
			func(t *testing.T, v any) {
				if p := v.(PostReplyCreated); p.ParentID != "P1" || p.Post.ID != "P2" {
					t.Errorf("post.reply_created = %+v", p)
				}
			}},
		{"story.created", `{"user_id":"U1","story_id":"ST1","expires_at":"2026-11-06T02:00:00Z"}`,
			func(t *testing.T, v any) {
				if s := v.(StoryCreated); s.StoryID != "ST1" || s.ExpiresAt.IsZero() {
					t.Errorf("story.created = %+v", s)
				}
			}},
		{"story.deleted", `{"user_id":"U1","story_id":"ST1"}`,
			func(t *testing.T, v any) {
				if s := v.(StoryDeleted); s.UserID != "U1" || s.StoryID != "ST1" {
					t.Errorf("story.deleted = %+v", s)
				}
			}},
		{"device.tui_state", `{"device_id":"D1","tui_connected":true}`,
			func(t *testing.T, v any) {
				if d := v.(DeviceTUIState); d.DeviceID != "D1" || !d.TUIConnected {
					t.Errorf("device.tui_state = %+v", d)
				}
			}},
		{"doc.created", `{"doc":` + docJSON + `}`,
			func(t *testing.T, v any) {
				if d := v.(DocCreatedEvent); d.Doc.ID != "D1" || d.Doc.Type != "arch" || d.Doc.HeadSeq != 4 {
					t.Errorf("doc.created = %+v", d)
				}
			}},
		{"doc.updated", `{"doc":` + docJSON + `,"deleted":true}`,
			func(t *testing.T, v any) {
				if d := v.(DocUpdatedEvent); d.Doc.ID != "D1" || !d.Deleted {
					t.Errorf("doc.updated = %+v", d)
				}
			}},
		{"doc.comment_created", `{"doc_id":"D1","comment":` + commentJSON + `}`,
			func(t *testing.T, v any) {
				if d := v.(DocCommentCreatedEvent); d.DocID != "D1" || d.Comment.ID != "K1" || d.Comment.Anchor != "node:n1" {
					t.Errorf("doc.comment_created = %+v", d)
				}
			}},
		{"workspace_share.invited", `{"share":` + shareJSON + `}`,
			func(t *testing.T, v any) {
				if d := v.(WorkspaceShareInvited); d.Share.ID != "W1" || d.Share.Owner.Handle != "hana" || len(d.Share.Members) != 1 || d.Share.Members[0].Seat != 1 {
					t.Errorf("workspace_share.invited = %+v", d)
				}
			}},
		{"workspace_share.updated", `{"share":` + shareJSON + `,"future":1}`,
			func(t *testing.T, v any) {
				if d := v.(WorkspaceShareUpdated); d.Share.ID != "W1" || d.Share.Status != "live" || d.Share.Members[0].MaxLevel != nil {
					t.Errorf("workspace_share.updated = %+v", d)
				}
			}},
		{"workspace_share.ended", `{"share_id":"W1","reason":"timeout"}`,
			func(t *testing.T, v any) {
				if d := v.(WorkspaceShareEnded); d != (WorkspaceShareEnded{ShareID: "W1", Reason: "timeout"}) {
					t.Errorf("workspace_share.ended = %+v", d)
				}
			}},
	}

	// Every events.md 8장 server type except pong must be covered exactly once.
	covered := map[string]bool{}
	for _, c := range cases {
		if covered[c.typ] {
			t.Fatalf("duplicate case %q", c.typ)
		}
		covered[c.typ] = true
	}
	for _, typ := range serverEventTypes {
		if typ == "pong" {
			continue
		}
		if !covered[typ] {
			t.Errorf("events.md 8장 type %q has no Decode sample", typ)
		}
	}

	for _, c := range cases {
		t.Run(c.typ, func(t *testing.T) {
			frame := `{"type":"` + c.typ + `","seq":7,"ts":"2026-11-05T02:05:00Z","data":` + c.data + `}`
			ev, err := parseFrame([]byte(frame))
			if err != nil {
				t.Fatalf("parseFrame: %v", err)
			}
			v, err := ev.Decode()
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if v == nil {
				t.Fatalf("Decode returned nil for known type %q", c.typ)
			}
			if reflect.TypeOf(v).Kind() == reflect.Pointer {
				t.Fatalf("Decode returned pointer %T; want value type", v)
			}
			c.check(t, v)
		})
	}
}

// serverEventTypes is events.md 8장 (server → client), copied verbatim.
var serverEventTypes = strings.Fields(strings.NewReplacer(",", " ").Replace(`hello, ready, reply, pong, session.revoked,
message.created, message.updated, message.deleted, thread.updated, reaction.added, reaction.removed, message.pinned, message.unpinned, poll.updated,
typing, read_state.updated,
channel.created, channel.updated, channel.deleted, channel.member_added, channel.member_removed, channel.prefs_updated,
space.joined, space.updated, space.deleted, space.removed, space.member_joined, space.member_left, space.member_updated, space.invite_received, space.invite_removed,
friend.request_received, friend.request_accepted, friend.request_removed, friend.added, friend.removed, block.updated, user.updated, settings.updated,
presence.snapshot, presence.updated, presence.self, focus.started, focus.ended,
notification.created, notifications.read, alert,
timeline.item_created, post.deleted, post.stats, post.reply_created, story.created, story.deleted,
device.tui_state,
doc.created, doc.updated, doc.comment_created,
workspace_share.invited, workspace_share.updated, workspace_share.ended`))

const (
	docUserJSON = `{"id":"U1","handle":"hana","display_name":"하나","avatar_url":null,"status":null,"is_private":false,"suspended":false}`
	docJSON     = `{"id":"D1","space_id":"S1","type":"arch","title":"결제","created_by":` + docUserJSON +
		`,"created_at":"2026-11-05T02:00:00Z","updated_at":"2026-11-05T02:05:00Z","head_seq":4,"locked":false}`
	commentJSON = `{"id":"K1","doc_id":"D1","anchor":"node:n1","author":` + docUserJSON +
		`,"body":"@sam 봐 주세요","root_id":null,"resolved_at":null,"created_at":"2026-11-05T02:05:00Z","deleted":false}`
	shareJSON = `{"id":"W1","owner":` + docUserJSON + `,"title":"결제","status":"live","created_at":"2026-11-10T01:00:00Z",` +
		`"ended_at":null,"end_reason":null,"members":[{"user":` + docUserJSON + `,"seat":1,"invited_at":"2026-11-10T01:00:00Z",` +
		`"joined_at":null,"left_at":null,"max_level":null,"verified":false}]}`
)

// TestDecodeDocEvents: the three v0.1.3 doc.* frames (events.md 4.10) decode
// to their types, unknown fields are ignored, and they are logged events.
func TestDecodeDocEvents(t *testing.T) {
	frames := map[string]string{
		"doc.created":         `{"doc":` + docJSON[:len(docJSON)-1] + `,"future":1},"extra":true}`,
		"doc.updated":         `{"doc":` + docJSON + `,"deleted":false,"why":"x"}`,
		"doc.comment_created": `{"doc_id":"D1","comment":` + commentJSON + `,"badge":3}`,
	}
	for typ, data := range frames {
		ev, err := parseFrame([]byte(`{"type":"` + typ + `","seq":9,"ts":"2026-11-05T02:05:00Z","data":` + data + `}`))
		if err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		v, err := ev.Decode()
		if err != nil {
			t.Fatalf("%s: Decode: %v", typ, err)
		}
		switch d := v.(type) {
		case DocCreatedEvent:
			if typ != "doc.created" || d.Doc.Title != "결제" || d.Doc.CreatedBy.Handle != "hana" {
				t.Errorf("%s = %+v", typ, d)
			}
		case DocUpdatedEvent:
			if typ != "doc.updated" || d.Doc.ID != "D1" || d.Deleted {
				t.Errorf("%s = %+v", typ, d)
			}
		case DocCommentCreatedEvent:
			if typ != "doc.comment_created" || d.DocID != "D1" || d.Comment.Body != "@sam 봐 주세요" || d.Comment.RootID != nil {
				t.Errorf("%s = %+v", typ, d)
			}
		default:
			t.Errorf("%s decoded to %T", typ, v)
		}
		if !loggedTypes[typ] {
			t.Errorf("%s is not logged (events.md 4.10: 기록)", typ)
		}
	}
}

func TestDecodeUnknownType(t *testing.T) {
	v, err := Event{Type: "future.thing", Data: json.RawMessage(`{"x":1}`)}.Decode()
	if v != nil || err != nil {
		t.Errorf("Decode(future.thing) = (%v, %v), want (nil, nil)", v, err)
	}
	// pong carries no payload: nothing to decode.
	v, err = Event{Type: "pong"}.Decode()
	if v != nil || err != nil {
		t.Errorf("Decode(pong) = (%v, %v), want (nil, nil)", v, err)
	}
}

func TestDecodeMalformedData(t *testing.T) {
	for _, data := range []string{``, `null`, `[1,2]`, `{"seq":"x"}`} {
		ev := Event{Type: "ready", Data: json.RawMessage(data)}
		if _, err := ev.Decode(); err == nil {
			t.Errorf("Decode(ready, %q) = nil error, want error", data)
		}
	}
}

func TestLoggedTypes(t *testing.T) {
	// events.md 4장 "기록" types.
	logged := []string{
		"message.created", "message.updated", "message.deleted", "thread.updated", "reaction.added", "reaction.removed",
		"message.pinned", "message.unpinned", "poll.updated", "read_state.updated",
		"channel.created", "channel.updated", "channel.deleted", "channel.member_added", "channel.member_removed", "channel.prefs_updated",
		"space.joined", "space.updated", "space.deleted", "space.removed", "space.member_joined", "space.member_left",
		"space.member_updated", "space.invite_received", "space.invite_removed",
		"friend.request_received", "friend.request_accepted", "friend.request_removed", "friend.added", "friend.removed",
		"block.updated", "user.updated", "settings.updated", "focus.started", "focus.ended",
		"notification.created", "notifications.read", "alert",
		"doc.created", "doc.updated", "doc.comment_created",
		"workspace_share.invited", "workspace_share.ended",
	}
	ephemeral := []string{
		"workspace_share.updated",
		"hello", "ready", "reply", "pong", "session.revoked", "typing", "presence.snapshot", "presence.updated", "presence.self",
		"timeline.item_created", "post.deleted", "post.stats", "post.reply_created", "story.created", "story.deleted", "device.tui_state",
	}
	for _, typ := range logged {
		if !loggedTypes[typ] {
			t.Errorf("loggedTypes[%q] = false, want true", typ)
		}
	}
	for _, typ := range ephemeral {
		if loggedTypes[typ] {
			t.Errorf("loggedTypes[%q] = true, want false", typ)
		}
	}
	if got, want := len(logged)+len(ephemeral), len(serverEventTypes); got != want {
		t.Errorf("logged+ephemeral = %d types, events.md 8장 has %d", got, want)
	}
	if len(loggedTypes) != len(logged) {
		t.Errorf("len(loggedTypes) = %d, want %d", len(loggedTypes), len(logged))
	}
}

func TestClientFrames(t *testing.T) {
	if string(pingFrame) != `{"type":"ping"}` || len(pingFrame) != 15 {
		t.Errorf("pingFrame = %q (len %d)", pingFrame, len(pingFrame))
	}
	e := "E"
	var five int64 = 5
	r := "R1"
	cases := []struct {
		name string
		got  []byte
		want string
	}{
		{"resume first", encodeResume(ResumePoint{}), `{"type":"resume","data":{"epoch":null,"last_seq":null}}`},
		{"resume epoch", encodeResume(ResumePoint{Epoch: &e, LastSeq: &five}), `{"type":"resume","data":{"epoch":"E","last_seq":5}}`},
		{"resume device ack", encodeResume(ResumePoint{UseDeviceAck: true}), `{"type":"resume","data":{"use_device_ack":true}}`},
		{"resume device ack wins", encodeResume(ResumePoint{Epoch: &e, LastSeq: &five, UseDeviceAck: true}), `{"type":"resume","data":{"use_device_ack":true}}`},
		{"ack", encodeAck(9), `{"type":"ack","data":{"seq":9}}`},
		{"typing", encodeTyping("C", nil), `{"type":"typing","data":{"channel_id":"C","thread_root_id":null}}`},
		{"typing thread", encodeTyping("C", &r), `{"type":"typing","data":{"channel_id":"C","thread_root_id":"R1"}}`},
		{"typing escapes", encodeTyping(`a"b`, nil), `{"type":"typing","data":{"channel_id":"a\"b","thread_root_id":null}}`},
		{"idle true", encodeIdle(true), `{"type":"idle","data":{"idle":true}}`},
		{"idle false", encodeIdle(false), `{"type":"idle","data":{"idle":false}}`},
		{"watch nil", encodeWatch(nil), `{"type":"watch","data":{"topics":[]}}`},
		{"watch two", encodeWatch([]string{"post:1", "post:2"}), `{"type":"watch","data":{"topics":["post:1","post:2"]}}`},
	}
	for _, c := range cases {
		if string(c.got) != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, c.got, c.want)
		}
	}
}
