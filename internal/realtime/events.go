package realtime

import (
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
