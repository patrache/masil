package realtime

import (
	"encoding/json"
	"time"

	"github.com/patrache/masil/internal/api"
)

// Typed event payloads returned by Event.Decode (CA 4.4 table; JSON is events.md 4장).

// Hello is `hello` (events.md 3.2).
type Hello struct {
	ConnectionID        string      `json:"connection_id"`
	SessionID           string      `json:"session_id"`
	UserID              string      `json:"user_id"`
	Mode                string      `json:"mode"`
	DeviceID            string      `json:"device_id"`
	Epoch               string      `json:"epoch"`
	Seq                 int64       `json:"seq"`
	OldestSeq           int64       `json:"oldest_seq"`
	HeartbeatIntervalMS int         `json:"heartbeat_interval_ms"`
	HeartbeatTimeoutMS  int         `json:"heartbeat_timeout_ms"`
	LocalTUIConnected   bool        `json:"local_tui_connected"`
	ServerTime          time.Time   `json:"server_time"`
	Limits              HelloLimits `json:"limits"`
}

// HelloLimits is hello.limits.
type HelloLimits struct {
	MaxFrameBytes       int `json:"max_frame_bytes"`
	WatchTopicsMax      int `json:"watch_topics_max"`
	TypingMinIntervalMS int `json:"typing_min_interval_ms"`
}

// Ready is `ready` (events.md 3.3).
type Ready struct {
	Resumed        bool   `json:"resumed"`
	ResyncRequired bool   `json:"resync_required"`
	Replayed       int    `json:"replayed"`
	Seq            int64  `json:"seq"`
	Epoch          string `json:"epoch"`
}

// Reply is `reply` (events.md 2).
type Reply struct {
	Ref   string      `json:"ref"`
	OK    bool        `json:"ok"`
	Error *ReplyError `json:"error"`
}

// ReplyError is reply.error.
type ReplyError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// SessionRevoked is `session.revoked`.
type SessionRevoked struct {
	SessionID string `json:"session_id"`
	Reason    string `json:"reason"`
}

// MessageEvent is `message.created` / `message.updated`.
type MessageEvent struct {
	Message api.Message `json:"message"`
}

// MessageDeleted is `message.deleted`.
type MessageDeleted struct {
	ChannelID    string  `json:"channel_id"`
	MessageID    string  `json:"message_id"`
	ThreadRootID *string `json:"thread_root_id"`
	Tombstone    bool    `json:"tombstone"`
}

// ThreadUpdated is `thread.updated`.
type ThreadUpdated struct {
	ChannelID string            `json:"channel_id"`
	RootID    string            `json:"root_id"`
	Thread    api.ThreadSummary `json:"thread"`
}

// ReactionEvent is `reaction.added` / `reaction.removed`.
type ReactionEvent struct {
	ChannelID    string  `json:"channel_id"`
	MessageID    string  `json:"message_id"`
	ThreadRootID *string `json:"thread_root_id"`
	Emoji        string  `json:"emoji"`
	UserID       string  `json:"user_id"`
	Count        int     `json:"count"`
}

// PinEvent is `message.pinned` / `message.unpinned`. By is decoded from
// `pinned_by` / `unpinned_by` by a custom UnmarshalJSON (Task 12).
type PinEvent struct {
	ChannelID string `json:"channel_id"`
	MessageID string `json:"message_id"`
	By        string `json:"-"` // pinned_by | unpinned_by
}

// UnmarshalJSON reads `pinned_by` (message.pinned) or `unpinned_by`
// (message.unpinned) into By.
func (p *PinEvent) UnmarshalJSON(b []byte) error {
	var w struct {
		ChannelID  string  `json:"channel_id"`
		MessageID  string  `json:"message_id"`
		PinnedBy   *string `json:"pinned_by"`
		UnpinnedBy *string `json:"unpinned_by"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*p = PinEvent{ChannelID: w.ChannelID, MessageID: w.MessageID}
	switch {
	case w.PinnedBy != nil:
		p.By = *w.PinnedBy
	case w.UnpinnedBy != nil:
		p.By = *w.UnpinnedBy
	}
	return nil
}

// PollUpdated is `poll.updated` (message and post polls; Poll.ContextType tells them apart).
type PollUpdated struct {
	Poll api.Poll `json:"poll"`
}

// Typing is `typing`.
type Typing struct {
	ChannelID    string  `json:"channel_id"`
	ThreadRootID *string `json:"thread_root_id"`
	UserID       string  `json:"user_id"`
	ExpiresInMS  int     `json:"expires_in_ms"`
}

// ReadStateUpdated is `read_state.updated`.
type ReadStateUpdated struct {
	State api.UnreadState `json:"state"`
}

// ChannelEvent is `channel.created` / `channel.updated`.
type ChannelEvent struct {
	Channel api.Channel `json:"channel"`
}

// ChannelDeleted is `channel.deleted`.
type ChannelDeleted struct {
	ChannelID string  `json:"channel_id"`
	SpaceID   *string `json:"space_id"`
	Reason    string  `json:"reason"`
}

// ChannelMemberAdded is `channel.member_added`.
type ChannelMemberAdded struct {
	ChannelID string          `json:"channel_id"`
	User      api.UserSummary `json:"user"`
}

// ChannelMemberRemoved is `channel.member_removed`.
type ChannelMemberRemoved struct {
	ChannelID string `json:"channel_id"`
	UserID    string `json:"user_id"`
}

// ChannelPrefsUpdated is `channel.prefs_updated`.
type ChannelPrefsUpdated struct {
	ChannelID string             `json:"channel_id"`
	My        api.ChannelMyState `json:"my"`
}

// SpaceJoined is `space.joined`.
type SpaceJoined struct {
	Space api.SpaceDetail `json:"space"`
}

// SpaceUpdated is `space.updated`.
type SpaceUpdated struct {
	Space api.Space `json:"space"`
}

// SpaceDeleted is `space.deleted`.
type SpaceDeleted struct {
	SpaceID string `json:"space_id"`
}

// SpaceRemoved is `space.removed`.
type SpaceRemoved struct {
	SpaceID string `json:"space_id"`
	Reason  string `json:"reason"`
}

// SpaceMemberEvent is `space.member_joined` / `space.member_updated`.
type SpaceMemberEvent struct {
	SpaceID string          `json:"space_id"`
	Member  api.SpaceMember `json:"member"`
}

// SpaceMemberLeft is `space.member_left`.
type SpaceMemberLeft struct {
	SpaceID string `json:"space_id"`
	UserID  string `json:"user_id"`
	Reason  string `json:"reason"`
}

// InviteReceived is `space.invite_received`.
type InviteReceived struct {
	Invite api.DirectInvite `json:"invite"`
}

// InviteRemoved is `space.invite_removed`.
type InviteRemoved struct {
	InviteID string `json:"invite_id"`
	Reason   string `json:"reason"`
}

// FriendRequestEvent is `friend.request_received` / `friend.request_accepted`.
type FriendRequestEvent struct {
	Request api.FriendRequest `json:"request"`
}

// FriendRequestRemoved is `friend.request_removed`.
type FriendRequestRemoved struct {
	RequestID string `json:"request_id"`
	Reason    string `json:"reason"`
}

// FriendAdded is `friend.added`.
type FriendAdded struct {
	Friend api.Friend `json:"friend"`
}

// FriendRemoved is `friend.removed`.
type FriendRemoved struct {
	UserID string `json:"user_id"`
}

// BlockUpdated is `block.updated`.
type BlockUpdated struct {
	UserID  string `json:"user_id"`
	Blocked bool   `json:"blocked"`
}

// UserUpdated is `user.updated`.
type UserUpdated struct {
	User api.UserSummary `json:"user"`
}

// SettingsUpdated is `settings.updated`.
type SettingsUpdated struct {
	Settings api.Settings `json:"settings"`
}

// PresenceSnapshot is `presence.snapshot`.
type PresenceSnapshot struct {
	Items []api.PresenceItem `json:"items"`
}

// PresenceUpdated is `presence.updated`.
type PresenceUpdated struct {
	UserID     string             `json:"user_id"`
	Status     api.PresenceStatus `json:"status"`
	LastSeenAt *time.Time         `json:"last_seen_at"`
}

// PresenceSelf is `presence.self`.
type PresenceSelf struct {
	Presence api.PresenceState `json:"presence"`
}

// FocusStarted is `focus.started`.
type FocusStarted struct {
	Focus api.Focus `json:"focus"`
}

// FocusEnded is `focus.ended`.
type FocusEnded struct {
	Summary api.FocusSummary `json:"summary"`
}

// NotificationCreated is `notification.created`.
type NotificationCreated struct {
	Notification api.Notification `json:"notification"`
	UnreadCount  int              `json:"unread_count"`
}

// NotificationsRead is `notifications.read`.
type NotificationsRead struct {
	IDs         []string `json:"ids"`
	All         bool     `json:"all"`
	Before      *string  `json:"before"`
	UnreadCount int      `json:"unread_count"`
}

// AlertEvent is `alert`.
type AlertEvent struct {
	Alert api.Alert `json:"alert"`
}

// TimelineItemCreated is `timeline.item_created`.
type TimelineItemCreated struct {
	Item api.TimelineItem `json:"item"`
}

// PostDeleted is `post.deleted`.
type PostDeleted struct {
	PostID string `json:"post_id"`
}

// PostStats is `post.stats`.
type PostStats struct {
	PostID      string `json:"post_id"`
	LikeCount   int    `json:"like_count"`
	RepostCount int    `json:"repost_count"`
	ReplyCount  int    `json:"reply_count"`
	QuoteCount  int    `json:"quote_count"`
}

// PostReplyCreated is `post.reply_created`.
type PostReplyCreated struct {
	ParentID string   `json:"parent_id"`
	Post     api.Post `json:"post"`
}

// StoryCreated is `story.created`.
type StoryCreated struct {
	UserID    string    `json:"user_id"`
	StoryID   string    `json:"story_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

// StoryDeleted is `story.deleted`.
type StoryDeleted struct {
	UserID  string `json:"user_id"`
	StoryID string `json:"story_id"`
}

// DeviceTUIState is `device.tui_state`.
type DeviceTUIState struct {
	DeviceID     string `json:"device_id"`
	TUIConnected bool   `json:"tui_connected"`
}

// DocCreatedEvent is `doc.created` (events.md 4.10, v0.1.3).
type DocCreatedEvent struct {
	Doc api.Doc `json:"doc"`
}

// DocUpdatedEvent is `doc.updated`: title / lock changes, or a delete
// (Deleted, Doc = its last value).
type DocUpdatedEvent struct {
	Doc     api.Doc `json:"doc"`
	Deleted bool    `json:"deleted"`
}

// DocCommentCreatedEvent is `doc.comment_created`.
type DocCommentCreatedEvent struct {
	DocID   string         `json:"doc_id"`
	Comment api.DocComment `json:"comment"`
}

// WorkspaceShareInvited is `workspace_share.invited` (events.md 4.11, v0.1.3):
// I was invited to a share.
type WorkspaceShareInvited struct {
	Share api.WorkspaceShare `json:"share"`
}

// WorkspaceShareUpdated is `workspace_share.updated` (ephemeral): invites,
// removals, leaving and level changes. A removed member also gets it, with a
// Share whose Members no longer list them.
type WorkspaceShareUpdated struct {
	Share api.WorkspaceShare `json:"share"`
}

// WorkspaceShareEnded is `workspace_share.ended`; Reason is ended|timeout.
type WorkspaceShareEnded struct {
	ShareID string `json:"share_id"`
	Reason  string `json:"reason"`
}

// decodeAs unmarshals raw into a T and returns it as a value.
func decodeAs[T any](raw json.RawMessage) (any, error) {
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return v, nil
}

// decoders maps every events.md 8장 server type (except payload-less `pong`)
// to its CA 4.4 Go type.
var decoders = map[string]func(json.RawMessage) (any, error){
	"hello":           decodeAs[Hello],
	"ready":           decodeAs[Ready],
	"reply":           decodeAs[Reply],
	"session.revoked": decodeAs[SessionRevoked],

	"message.created":  decodeAs[MessageEvent],
	"message.updated":  decodeAs[MessageEvent],
	"message.deleted":  decodeAs[MessageDeleted],
	"thread.updated":   decodeAs[ThreadUpdated],
	"reaction.added":   decodeAs[ReactionEvent],
	"reaction.removed": decodeAs[ReactionEvent],
	"message.pinned":   decodeAs[PinEvent],
	"message.unpinned": decodeAs[PinEvent],
	"poll.updated":     decodeAs[PollUpdated],

	"typing":             decodeAs[Typing],
	"read_state.updated": decodeAs[ReadStateUpdated],

	"channel.created":        decodeAs[ChannelEvent],
	"channel.updated":        decodeAs[ChannelEvent],
	"channel.deleted":        decodeAs[ChannelDeleted],
	"channel.member_added":   decodeAs[ChannelMemberAdded],
	"channel.member_removed": decodeAs[ChannelMemberRemoved],
	"channel.prefs_updated":  decodeAs[ChannelPrefsUpdated],

	"space.joined":          decodeAs[SpaceJoined],
	"space.updated":         decodeAs[SpaceUpdated],
	"space.deleted":         decodeAs[SpaceDeleted],
	"space.removed":         decodeAs[SpaceRemoved],
	"space.member_joined":   decodeAs[SpaceMemberEvent],
	"space.member_left":     decodeAs[SpaceMemberLeft],
	"space.member_updated":  decodeAs[SpaceMemberEvent],
	"space.invite_received": decodeAs[InviteReceived],
	"space.invite_removed":  decodeAs[InviteRemoved],

	"friend.request_received": decodeAs[FriendRequestEvent],
	"friend.request_accepted": decodeAs[FriendRequestEvent],
	"friend.request_removed":  decodeAs[FriendRequestRemoved],
	"friend.added":            decodeAs[FriendAdded],
	"friend.removed":          decodeAs[FriendRemoved],
	"block.updated":           decodeAs[BlockUpdated],
	"user.updated":            decodeAs[UserUpdated],
	"settings.updated":        decodeAs[SettingsUpdated],

	"presence.snapshot": decodeAs[PresenceSnapshot],
	"presence.updated":  decodeAs[PresenceUpdated],
	"presence.self":     decodeAs[PresenceSelf],
	"focus.started":     decodeAs[FocusStarted],
	"focus.ended":       decodeAs[FocusEnded],

	"notification.created": decodeAs[NotificationCreated],
	"notifications.read":   decodeAs[NotificationsRead],
	"alert":                decodeAs[AlertEvent],

	"timeline.item_created": decodeAs[TimelineItemCreated],
	"post.deleted":          decodeAs[PostDeleted],
	"post.stats":            decodeAs[PostStats],
	"post.reply_created":    decodeAs[PostReplyCreated],
	"story.created":         decodeAs[StoryCreated],
	"story.deleted":         decodeAs[StoryDeleted],

	"device.tui_state": decodeAs[DeviceTUIState],

	"doc.created":         decodeAs[DocCreatedEvent],
	"doc.updated":         decodeAs[DocUpdatedEvent],
	"doc.comment_created": decodeAs[DocCommentCreatedEvent],

	"workspace_share.invited": decodeAs[WorkspaceShareInvited],
	"workspace_share.updated": decodeAs[WorkspaceShareUpdated],
	"workspace_share.ended":   decodeAs[WorkspaceShareEnded],
}

// loggedTypes are the types events.md 4장 marks 기록 (logged, carry a seq).
// poll.updated is logged for message polls (4.2) but ephemeral for post polls
// (4.8); the frame's own seq (nil or not) is authoritative per event.
var loggedTypes = map[string]bool{
	"message.created": true, "message.updated": true, "message.deleted": true, "thread.updated": true,
	"reaction.added": true, "reaction.removed": true, "message.pinned": true, "message.unpinned": true,
	"poll.updated": true, "read_state.updated": true,

	"channel.created": true, "channel.updated": true, "channel.deleted": true, "channel.member_added": true,
	"channel.member_removed": true, "channel.prefs_updated": true,

	"space.joined": true, "space.updated": true, "space.deleted": true, "space.removed": true,
	"space.member_joined": true, "space.member_left": true, "space.member_updated": true,
	"space.invite_received": true, "space.invite_removed": true,

	"friend.request_received": true, "friend.request_accepted": true, "friend.request_removed": true,
	"friend.added": true, "friend.removed": true, "block.updated": true, "user.updated": true, "settings.updated": true,

	"focus.started": true, "focus.ended": true,

	"notification.created": true, "notifications.read": true, "alert": true,

	"doc.created": true, "doc.updated": true, "doc.comment_created": true,

	"workspace_share.invited": true, "workspace_share.ended": true, // updated is ephemeral (events.md 4.11)
}
