package api

import (
	"encoding/json"
	"time"
)

// Wire types mirroring api.md section 2 one-to-one (CA 4.3.2). Conversion
// rules (CA 4.3.1): ID -> string, Timestamp -> time.Time (nullable ->
// *time.Time), nullable object/string/number -> pointer, arrays -> slices,
// Record<string, unknown> -> json.RawMessage. JSON tags are the api.md field
// names verbatim. TS inheritance is flattened by embedding; JSON stays flat.

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

// NotificationType is the notification inbox item type (api.md 2):
// follow|follow_request|follow_accepted|friend_request|friend_accepted|
// space_invite|post_like|post_repost|post_reply|post_quote|post_mention|
// message_mention|thread_reply|guestbook_entry.
type NotificationType string

// UserStatus is a user's custom status message ("🎧 듣는 중…").
type UserStatus struct {
	Emoji     *string    `json:"emoji"`
	Text      string     `json:"text"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// UserSummary is the user object embedded everywhere.
type UserSummary struct {
	ID          string      `json:"id"`
	Handle      string      `json:"handle"`
	DisplayName string      `json:"display_name"`
	AvatarURL   *string     `json:"avatar_url"` // "/v1/media/<upload_id>" (relative)
	Status      *UserStatus `json:"status"`     // null if unset or expired
	IsPrivate   bool        `json:"is_private"`
	Suspended   bool        `json:"suspended"`
}

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

// FriendRequestRef is Relationship.friend_request (pending only).
type FriendRequestRef struct {
	ID        string `json:"id"`
	Direction string `json:"direction"` // incoming|outgoing
}

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
	Relationship   *Relationship `json:"relationship"` // null when viewing self
}

// Settings are the user's server-side settings.
type Settings struct {
	DMPolicy        string         `json:"dm_policy"` // friends_and_spaces|friends_only
	IsPrivate       bool           `json:"is_private"`
	Notify          NotifySettings `json:"notify"`
	FocusDefaultMin int            `json:"focus_default_min"` // 1..720
}

// NotifySettings are the notify.* desktop alert switches.
type NotifySettings struct {
	DM            bool `json:"dm"`
	Mention       bool `json:"mention"`
	Invite        bool `json:"invite"`
	FriendRequest bool `json:"friend_request"`
	ThreadReply   bool `json:"thread_reply"`
	Social        bool `json:"social"`
}

// Focus is an active focus session.
type Focus struct {
	StartedAt time.Time `json:"started_at"`
	EndsAt    time.Time `json:"ends_at"`
}

// PresenceState is my presence.
type PresenceState struct {
	Status PresenceStatus `json:"status"` // effective
	Mode   string         `json:"mode"`   // auto|away|dnd (manual choice)
	Focus  *Focus         `json:"focus"`
}

// Me is the authenticated user (embeds UserSummary; JSON stays flat).
type Me struct {
	UserSummary
	Bio            string        `json:"bio"`
	CreatedAt      time.Time     `json:"created_at"`
	Role           string        `json:"role"` // user|admin
	FollowerCount  int           `json:"follower_count"`
	FollowingCount int           `json:"following_count"`
	PostCount      int           `json:"post_count"`
	Settings       Settings      `json:"settings"`
	Presence       PresenceState `json:"presence"`
}

// Session is a login session.
type Session struct {
	ID         string    `json:"id"`
	Client     string    `json:"client"` // tui|daemon|admin|other
	DeviceName string    `json:"device_name"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Current    bool      `json:"current"`
}

// Media is an uploaded media object.
type Media struct {
	ID          string `json:"id"`           // upload id
	URL         string `json:"url"`          // "/v1/media/<id>"
	ContentType string `json:"content_type"` // image/png|image/jpeg|image/gif|image/webp
	Size        int    `json:"size"`         // bytes
	Width       *int   `json:"width"`
	Height      *int   `json:"height"`
}

// Upload embeds Media (JSON stays flat).
type Upload struct {
	Media
	Purpose   string    `json:"purpose"` // avatar|post|story
	Status    string    `json:"status"`  // pending|attached|removed
	CreatedAt time.Time `json:"created_at"`
}

// FriendRequest is a friend request.
type FriendRequest struct {
	ID        string      `json:"id"`
	From      UserSummary `json:"from"`
	To        UserSummary `json:"to"`
	Status    string      `json:"status"` // pending|accepted|declined|canceled
	CreatedAt time.Time   `json:"created_at"`
}

// Friend is a friend entry.
type Friend struct {
	User     UserSummary    `json:"user"`
	Since    time.Time      `json:"since"`
	Presence PresenceStatus `json:"presence"`
}

// SpacePreview is a space summary.
type SpacePreview struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	IconEmoji   *string `json:"icon_emoji"`
	MemberCount int     `json:"member_count"`
}

// Space embeds SpacePreview (JSON stays flat).
type Space struct {
	SpacePreview
	OwnerID          string    `json:"owner_id"`
	DefaultChannelID string    `json:"default_channel_id"`
	CreatedAt        time.Time `json:"created_at"`
	MyRole           SpaceRole `json:"my_role"`
}

// SpaceDetail embeds Space; Channels are those visible to me, by position.
type SpaceDetail struct {
	Space
	Channels []Channel `json:"channels"`
}

// SpaceMember is a space membership.
type SpaceMember struct {
	User     UserSummary `json:"user"`
	Role     SpaceRole   `json:"role"`
	JoinedAt time.Time   `json:"joined_at"`
}

// Invite is a space invite link.
type Invite struct {
	Code      string       `json:"code"` // 8 chars
	Space     SpacePreview `json:"space"`
	CreatedBy UserSummary  `json:"created_by"`
	MaxUses   *int         `json:"max_uses"`
	UseCount  int          `json:"use_count"`
	ExpiresAt *time.Time   `json:"expires_at"`
	Revoked   bool         `json:"revoked"`
	CreatedAt time.Time    `json:"created_at"`
}

// InvitePreview is what a non-member sees for an invite code.
type InvitePreview struct {
	Code          string       `json:"code"`
	Space         SpacePreview `json:"space"`
	CreatedBy     UserSummary  `json:"created_by"`
	ExpiresAt     *time.Time   `json:"expires_at"`
	AlreadyMember bool         `json:"already_member"`
}

// DirectInvite is a direct space invite.
type DirectInvite struct {
	ID        string       `json:"id"`
	Space     SpacePreview `json:"space"`
	Inviter   UserSummary  `json:"inviter"`
	Invitee   UserSummary  `json:"invitee"`
	Status    string       `json:"status"` // pending|accepted|declined|canceled
	CreatedAt time.Time    `json:"created_at"`
}

// ChannelMyState is the viewer's per-channel state.
type ChannelMyState struct {
	Notify            string  `json:"notify"` // all|mentions|none
	Muted             bool    `json:"muted"`
	LastReadMessageID *string `json:"last_read_message_id"`
	UnreadCount       int     `json:"unread_count"`  // capped at 999
	MentionCount      int     `json:"mention_count"` // top-level only, capped at 999
}

// Channel is a text channel, DM or group DM.
type Channel struct {
	ID             string         `json:"id"`
	Kind           ChannelKind    `json:"kind"`
	SpaceID        *string        `json:"space_id"`
	Name           *string        `json:"name"` // text: name without '#'; group_dm: optional; dm: null
	Topic          string         `json:"topic"`
	IsPrivate      bool           `json:"is_private"`
	Position       int            `json:"position"`
	OwnerID        *string        `json:"owner_id"`
	MemberCount    int            `json:"member_count"`
	Members        []UserSummary  `json:"members"` // dm/group_dm: all members; text: null (nil)
	LastMessageID  *string        `json:"last_message_id"`
	LastMessageAt  *time.Time     `json:"last_message_at"`
	CreatedAt      time.Time      `json:"created_at"`
	ReadOnly       bool           `json:"read_only"`
	ReadOnlyReason *string        `json:"read_only_reason"` // not_eligible|blocked|null
	My             ChannelMyState `json:"my"`
}

// ChannelRef is a channel reference.
type ChannelRef struct {
	ID        string      `json:"id"`
	Kind      ChannelKind `json:"kind"`
	Name      *string     `json:"name"`
	SpaceID   *string     `json:"space_id"`
	SpaceName *string     `json:"space_name"`
}

// UnreadState is a channel's unread counters.
type UnreadState struct {
	ChannelID         string  `json:"channel_id"`
	UnreadCount       int     `json:"unread_count"`
	MentionCount      int     `json:"mention_count"`
	LastMessageID     *string `json:"last_message_id"`
	LastReadMessageID *string `json:"last_read_message_id"`
}

// MentionRef is a resolved mention.
type MentionRef struct {
	UserID string `json:"user_id"`
	Handle string `json:"handle"`
}

// ReactionSummary is one reaction emoji on a message.
type ReactionSummary struct {
	Emoji string `json:"emoji"`
	Count int    `json:"count"`
	Me    bool   `json:"me"`
}

// ThreadSummary summarizes a thread on its root.
type ThreadSummary struct {
	ReplyCount     int        `json:"reply_count"`
	LastReplyAt    *time.Time `json:"last_reply_at"`
	ParticipantIDs []string   `json:"participant_ids"` // most recent first, <= 5
}

// RollResult is a /roll result.
type RollResult struct {
	Expr     string `json:"expr"`
	Rolls    []int  `json:"rolls"`
	Modifier int    `json:"modifier"`
	Total    int    `json:"total"`
}

// SystemEvent is a system message payload.
type SystemEvent struct {
	Event        string          `json:"event"` // member_joined|member_left|member_kicked|channel_created|channel_renamed|channel_topic_changed|message_pinned|group_renamed|group_member_added|group_member_removed
	ActorID      string          `json:"actor_id"`
	TargetUserID *string         `json:"target_user_id"`
	Data         SystemEventData `json:"data"`
}

// SystemEventData is SystemEvent.data; every key is optional on the wire.
type SystemEventData struct {
	Name      string `json:"name,omitempty"`
	Topic     string `json:"topic,omitempty"`
	MessageID string `json:"message_id,omitempty"`
}

// PollOption is one poll option.
type PollOption struct {
	Idx       int    `json:"idx"`
	Text      string `json:"text"`
	VoteCount int    `json:"vote_count"`
}

// Poll is a message or post poll.
type Poll struct {
	ID          string       `json:"id"`
	ContextType string       `json:"context_type"` // message|post
	ContextID   string       `json:"context_id"`
	CreatorID   string       `json:"creator_id"`
	Question    string       `json:"question"`
	Multi       bool         `json:"multi"`
	Options     []PollOption `json:"options"`
	VoterCount  int          `json:"voter_count"`
	ClosesAt    *time.Time   `json:"closes_at"`
	Closed      bool         `json:"closed"`
	MyVotes     []int        `json:"my_votes"` // option idx list
}

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

// Post is a feed post.
type Post struct {
	ID            string       `json:"id"`
	Author        UserSummary  `json:"author"`
	Body          string       `json:"body"`  // "" if deleted
	Media         []Media      `json:"media"` // 0..4, ordered
	Poll          *Poll        `json:"poll"`
	ReplyToID     *string      `json:"reply_to_id"`
	RootID        *string      `json:"root_id"`
	ReplyToAuthor *UserSummary `json:"reply_to_author"`
	Quote         *QuotedPost  `json:"quote"`
	Hashtags      []string     `json:"hashtags"` // normalized, no '#'
	Mentions      []MentionRef `json:"mentions"`
	LikeCount     int          `json:"like_count"`
	RepostCount   int          `json:"repost_count"`
	ReplyCount    int          `json:"reply_count"`
	QuoteCount    int          `json:"quote_count"`
	Viewer        PostViewer   `json:"viewer"`
	CreatedAt     time.Time    `json:"created_at"`
	Deleted       bool         `json:"deleted"`
}

// PostViewer is Post.viewer.
type PostViewer struct {
	Liked    bool `json:"liked"`
	Reposted bool `json:"reposted"`
}

// TimelineItem is a timeline entry.
type TimelineItem struct {
	ID         string       `json:"id"`   // post id (post) or repost id (repost); cursor anchor
	Type       string       `json:"type"` // post|repost
	Post       Post         `json:"post"`
	RepostedBy *UserSummary `json:"reposted_by"`
	At         time.Time    `json:"at"`
}

// Story is a story.
type Story struct {
	ID        string      `json:"id"`
	Author    UserSummary `json:"author"`
	Body      string      `json:"body"`
	Media     *Media      `json:"media"`
	BG        string      `json:"bg"` // accent|accent2|accent3|accent4|plain
	CreatedAt time.Time   `json:"created_at"`
	ExpiresAt time.Time   `json:"expires_at"`
	Seen      bool        `json:"seen"`
	ViewCount *int        `json:"view_count"` // author only, else null
}

// StoryGroup is one user's stories.
type StoryGroup struct {
	User      UserSummary `json:"user"`
	Stories   []Story     `json:"stories"`
	HasUnseen bool        `json:"has_unseen"`
	LatestAt  time.Time   `json:"latest_at"`
}

// Notification is a notification inbox item.
type Notification struct {
	ID        string              `json:"id"`
	Type      NotificationType    `json:"type"`
	Actor     *UserSummary        `json:"actor"`
	Subject   NotificationSubject `json:"subject"`
	Preview   string              `json:"preview"` // <= 100 code points; "" if none
	Context   NotificationContext `json:"context"`
	CreatedAt time.Time           `json:"created_at"`
	Read      bool                `json:"read"`
}

// NotificationSubject is Notification.subject.
type NotificationSubject struct {
	Type string `json:"type"` // user|post|message|space|friend_request|space_direct_invite|guestbook_entry
	ID   string `json:"id"`
}

// NotificationContext is Notification.context.
type NotificationContext struct {
	PostID       *string `json:"post_id"`
	ChannelID    *string `json:"channel_id"`
	MessageID    *string `json:"message_id"`
	ThreadRootID *string `json:"thread_root_id"`
	SpaceID      *string `json:"space_id"`
	SpaceName    *string `json:"space_name"`
}

// Alert is a desktop notification candidate (events.md 4.7).
type Alert struct {
	ID               string       `json:"id"`
	Kind             AlertKind    `json:"kind"`
	Title            string       `json:"title"` // <= 80 code points
	Body             string       `json:"body"`  // <= 200 code points, plain text
	Actor            *UserSummary `json:"actor"`
	Target           AlertTarget  `json:"target"`
	CreatedAt        time.Time    `json:"created_at"`
	Suppressed       bool         `json:"suppressed"`        // focus/dnd: no OS notification
	SuppressedReason *string      `json:"suppressed_reason"` // focus|dnd|null
}

// AlertTarget is Alert.target.
type AlertTarget struct {
	Type         string  `json:"type"` // channel|message|post|space|user|notification
	ID           string  `json:"id"`
	ChannelID    *string `json:"channel_id"`
	MessageID    *string `json:"message_id"`
	ThreadRootID *string `json:"thread_root_id"`
	PostID       *string `json:"post_id"`
	SpaceID      *string `json:"space_id"`
}

// FocusSummary summarizes a finished focus session.
type FocusSummary struct {
	StartedAt time.Time   `json:"started_at"`
	EndedAt   time.Time   `json:"ended_at"`
	Counts    FocusCounts `json:"counts"`
	Items     []Alert     `json:"items"` // suppressed alerts, newest first, <= 20
}

// FocusCounts is FocusSummary.counts.
type FocusCounts struct {
	DM            int `json:"dm"`
	Mention       int `json:"mention"`
	ThreadReply   int `json:"thread_reply"`
	Invite        int `json:"invite"`
	FriendRequest int `json:"friend_request"`
	Other         int `json:"other"`
}

// GuestbookEntry is a guestbook entry.
type GuestbookEntry struct {
	ID        string       `json:"id"`
	OwnerID   string       `json:"owner_id"`
	Author    *UserSummary `json:"author"` // null when hidden
	Body      string       `json:"body"`   // "" when hidden or deleted
	IsSecret  bool         `json:"is_secret"`
	Hidden    bool         `json:"hidden"`
	CreatedAt time.Time    `json:"created_at"`
	Deleted   bool         `json:"deleted"`
}

// SearchHighlight is a search match range in code points of the body.
type SearchHighlight struct {
	Start  int `json:"start"`
	Length int `json:"length"`
}

// AdminUserView is the operator's user view (embeds UserSummary; JSON stays flat).
type AdminUserView struct {
	UserSummary
	Role            string     `json:"role"` // user|admin
	CreatedAt       time.Time  `json:"created_at"`
	SuspendedAt     *time.Time `json:"suspended_at"`
	SuspendedUntil  *time.Time `json:"suspended_until"`
	SuspendReason   *string    `json:"suspend_reason"`
	OpenReportCount int        `json:"open_report_count"`
}

// Report is a moderation report.
type Report struct {
	ID                  string          `json:"id"`
	Reporter            UserSummary     `json:"reporter"`
	TargetType          string          `json:"target_type"` // user|message|post|story|guestbook_entry|space
	TargetID            string          `json:"target_id"`
	TargetUser          *UserSummary    `json:"target_user"`
	Reason              string          `json:"reason"` // spam|harassment|illegal|sexual|violence|other
	Note                string          `json:"note"`
	Snapshot            json.RawMessage `json:"snapshot"` // copy at report time
	TargetExists        bool            `json:"target_exists"`
	OpenReportsOnTarget int             `json:"open_reports_on_target"`
	Status              string          `json:"status"`     // open|resolved|dismissed
	Resolution          *string         `json:"resolution"` // content_removed|user_suspended|no_action|null
	ResolutionNote      *string         `json:"resolution_note"`
	ResolvedBy          *UserSummary    `json:"resolved_by"`
	ResolvedAt          *time.Time      `json:"resolved_at"`
	CreatedAt           time.Time       `json:"created_at"`
}

// AdminAction is an operator action log entry.
type AdminAction struct {
	ID         string      `json:"id"`
	Admin      UserSummary `json:"admin"`
	Action     string      `json:"action"` // remove_content|suspend_user|unsuspend_user|resolve_report|dismiss_report
	TargetType string      `json:"target_type"`
	TargetID   string      `json:"target_id"`
	ReportID   *string     `json:"report_id"`
	Reason     string      `json:"reason"`
	CreatedAt  time.Time   `json:"created_at"`
}
