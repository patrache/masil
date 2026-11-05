package api

import "time"

// Wire types mirroring api.md section 2 (CA 4.3.2). Fields that CA 4.3.2
// spells out are complete here; the remaining fields arrive in Task 9.

// PresenceStatus is online|away|dnd|offline.
type PresenceStatus string

// PresenceStatus values.
const (
	PresenceOnline  PresenceStatus = "online"
	PresenceAway    PresenceStatus = "away"
	PresenceDND     PresenceStatus = "dnd"
	PresenceOffline PresenceStatus = "offline"
)

// SpaceRole is owner|admin|member.
type SpaceRole string

// SpaceRole values.
const (
	RoleOwner  SpaceRole = "owner"
	RoleAdmin  SpaceRole = "admin"
	RoleMember SpaceRole = "member"
)

// ChannelKind is text|dm|group_dm.
type ChannelKind string

// ChannelKind values.
const (
	ChannelText    ChannelKind = "text"
	ChannelDM      ChannelKind = "dm"
	ChannelGroupDM ChannelKind = "group_dm"
)

// AlertKind is dm|mention|thread_reply|invite|friend_request|channel_message|social|focus_summary.
type AlertKind string

// AlertKind values.
const (
	AlertDM             AlertKind = "dm"
	AlertMention        AlertKind = "mention"
	AlertThreadReply    AlertKind = "thread_reply"
	AlertInvite         AlertKind = "invite"
	AlertFriendRequest  AlertKind = "friend_request"
	AlertChannelMessage AlertKind = "channel_message"
	AlertSocial         AlertKind = "social"
	AlertFocusSummary   AlertKind = "focus_summary"
)

// NotificationType is the notification inbox item type (api.md 2).
type NotificationType string

// UserStatus is a user's status message. TODO(core): fields in Task 9.
type UserStatus struct{}

// UserSummary is the embedded user object. TODO(core): fields in Task 9.
type UserSummary struct{}

// Relationship describes how the viewer relates to a user.
type Relationship struct {
	IsFriend       bool              `json:"is_friend"`
	FriendRequest  *FriendRequestRef `json:"friend_request"` // {id, direction}
	Following      string            `json:"following"`      // none|pending|active
	FollowedBy     string            `json:"followed_by"`
	Blocked        bool              `json:"blocked"`
	CanDM          bool              `json:"can_dm"`
	CanViewPosts   bool              `json:"can_view_posts"`
	SharedSpaceIDs []string          `json:"shared_space_ids"`
}

// FriendRequestRef is {id, direction}. TODO(core): fields in Task 9.
type FriendRequestRef struct{}

// Profile is a full user profile. TS inheritance is flattened by embedding; JSON stays flat.
type Profile struct {
	UserSummary
	Bio            string        `json:"bio"`
	CreatedAt      time.Time     `json:"created_at"`
	FollowerCount  int           `json:"follower_count"`
	FollowingCount int           `json:"following_count"`
	PostCount      int           `json:"post_count"`
	Presence       *string       `json:"presence"` // PresenceStatus or null
	LastSeenAt     *time.Time    `json:"last_seen_at"`
	Relationship   *Relationship `json:"relationship"`
}

// Settings are the user's server-side settings. TODO(core): fields in Task 9.
type Settings struct{}

// NotifySettings are the notify.* switches. TODO(core): fields in Task 9.
type NotifySettings struct{}

// Focus is an active focus session. TODO(core): fields in Task 9.
type Focus struct{}

// PresenceState is my presence. TODO(core): fields in Task 9.
type PresenceState struct{}

// Me is the authenticated user (embeds UserSummary). TODO(core): fields in Task 9.
type Me struct {
	UserSummary
}

// Session is a login session. TODO(core): fields in Task 9.
type Session struct{}

// Media is an uploaded media object. TODO(core): fields in Task 9.
type Media struct{}

// Upload embeds Media. TODO(core): fields in Task 9.
type Upload struct {
	Media
}

// FriendRequest is a friend request. TODO(core): fields in Task 9.
type FriendRequest struct{}

// Friend is a friend entry. TODO(core): fields in Task 9.
type Friend struct{}

// SpacePreview is a space summary. TODO(core): fields in Task 9.
type SpacePreview struct{}

// Space embeds SpacePreview. TODO(core): fields in Task 9.
type Space struct {
	SpacePreview
}

// SpaceDetail embeds Space. TODO(core): fields in Task 9.
type SpaceDetail struct {
	Space
}

// SpaceMember is a space membership. TODO(core): fields in Task 9.
type SpaceMember struct{}

// Invite is a space invite link. TODO(core): fields in Task 9.
type Invite struct{}

// InvitePreview is an invite link preview. TODO(core): fields in Task 9.
type InvitePreview struct{}

// DirectInvite is a direct space invite. TODO(core): fields in Task 9.
type DirectInvite struct{}

// ChannelMyState is the viewer's per-channel state. TODO(core): fields in Task 9.
type ChannelMyState struct{}

// Channel is a text channel, DM or group DM. TODO(core): fields in Task 9.
type Channel struct{}

// ChannelRef is a channel reference. TODO(core): fields in Task 9.
type ChannelRef struct{}

// UnreadState is a channel's unread counters. TODO(core): fields in Task 9.
type UnreadState struct{}

// MentionRef is a resolved mention. TODO(core): fields in Task 9.
type MentionRef struct{}

// ReactionSummary is one reaction emoji on a message. TODO(core): fields in Task 9.
type ReactionSummary struct{}

// ThreadSummary summarizes a thread on its root. TODO(core): fields in Task 9.
type ThreadSummary struct{}

// RollResult is a /roll result. TODO(core): fields in Task 9.
type RollResult struct{}

// SystemEvent is a system message payload. TODO(core): fields in Task 9.
type SystemEvent struct{}

// SystemEventData is SystemEvent.data. TODO(core): fields in Task 9.
type SystemEventData struct{}

// PollOption is one poll option. TODO(core): fields in Task 9.
type PollOption struct{}

// Poll is a message or post poll. TODO(core): fields in Task 9.
type Poll struct{}

// PollInput creates a poll (messages and posts).
type PollInput struct {
	Question    string   `json:"question"`
	Options     []string `json:"options"`
	Multi       bool     `json:"multi,omitempty"`
	DurationSec *int     `json:"duration_sec,omitempty"`
}

// Message is a chat message.
type Message struct {
	ID           string            `json:"id"`
	ChannelID    string            `json:"channel_id"`
	Author       UserSummary       `json:"author"`
	Kind         string            `json:"kind"` // text|me|roll|poll|system
	Body         string            `json:"body"`
	Mentions     []MentionRef      `json:"mentions"`
	MentionHere  bool              `json:"mention_here"`
	ThreadRootID *string           `json:"thread_root_id"`
	Thread       *ThreadSummary    `json:"thread"`
	Reactions    []ReactionSummary `json:"reactions"`
	Pinned       bool              `json:"pinned"`
	Poll         *Poll             `json:"poll"`
	Roll         *RollResult       `json:"roll"`
	System       *SystemEvent      `json:"system"`
	ClientNonce  *string           `json:"client_nonce"`
	CreatedAt    time.Time         `json:"created_at"`
	EditedAt     *time.Time        `json:"edited_at"`
	Deleted      bool              `json:"deleted"`
}

// QuotedPost is a union discriminated by "available".
type QuotedPost struct {
	Available bool   `json:"available"`
	Post      *Post  `json:"post,omitempty"`   // when available
	ID        string `json:"id,omitempty"`     // when !available
	Reason    string `json:"reason,omitempty"` // deleted | not_visible
}

// Post is a feed post. TODO(core): fields in Task 9.
type Post struct{}

// PostViewer is Post.viewer. TODO(core): fields in Task 9.
type PostViewer struct{}

// TimelineItem is a timeline entry. TODO(core): fields in Task 9.
type TimelineItem struct{}

// Story is a story. TODO(core): fields in Task 9.
type Story struct{}

// StoryGroup is one user's stories. TODO(core): fields in Task 9.
type StoryGroup struct{}

// Notification is a notification inbox item. TODO(core): fields in Task 9.
type Notification struct{}

// NotificationSubject is Notification.subject. TODO(core): fields in Task 9.
type NotificationSubject struct{}

// NotificationContext is Notification.context. TODO(core): fields in Task 9.
type NotificationContext struct{}

// Alert is a desktop notification candidate. TODO(core): fields in Task 9.
type Alert struct{}

// AlertTarget is Alert.target. TODO(core): fields in Task 9.
type AlertTarget struct{}

// FocusSummary summarizes a finished focus session. TODO(core): fields in Task 9.
type FocusSummary struct{}

// FocusCounts is FocusSummary.counts. TODO(core): fields in Task 9.
type FocusCounts struct{}

// GuestbookEntry is a guestbook entry. TODO(core): fields in Task 9.
type GuestbookEntry struct{}

// SearchHighlight is a search match range. TODO(core): fields in Task 9.
type SearchHighlight struct{}

// AdminUserView is the operator's user view. TODO(core): fields in Task 9.
type AdminUserView struct{}

// Report is a moderation report. TODO(core): fields in Task 9.
type Report struct{}

// AdminAction is an operator action log entry. TODO(core): fields in Task 9.
type AdminAction struct{}
