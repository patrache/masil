package api

import "time"

// Endpoint-specific response bundles (CA 4.3.2). JSON tags are the response
// keys of the matching endpoint in api.md sections 1, 4 and 5 (snake_case).

// AuthResult is the signup/login response.
type AuthResult struct {
	Token   string  `json:"token"`
	Session Session `json:"session"`
	User    Me      `json:"user"`
}

// MetaInfo is GET /v1/meta.
type MetaInfo struct {
	APIVersion       int        `json:"api_version"`
	ServerTime       time.Time  `json:"server_time"`
	MinClientVersion string     `json:"min_client_version"`
	Limits           MetaLimits `json:"limits"`
}

// MetaLimits is MetaInfo.limits.
type MetaLimits struct {
	MessageMaxChars     int `json:"message_max_chars"`
	PostMaxChars        int `json:"post_max_chars"`
	PostMaxMedia        int `json:"post_max_media"`
	UploadMaxBytes      int `json:"upload_max_bytes"`
	GroupDMMaxMembers   int `json:"group_dm_max_members"`
	StoryTTLSec         int `json:"story_ttl_sec"`
	PollMaxOptions      int `json:"poll_max_options"`
	ReactionsPerMessage int `json:"reactions_per_message"`
	WatchTopicsMax      int `json:"watch_topics_max"`
}

// PresenceItem is one GET /v1/presence item.
type PresenceItem struct {
	UserID     string         `json:"user_id"`
	Status     PresenceStatus `json:"status"`
	LastSeenAt *time.Time     `json:"last_seen_at"`
}

// BlockEntry is one GET /v1/blocks item.
type BlockEntry struct {
	User      UserSummary `json:"user"`
	CreatedAt time.Time   `json:"created_at"`
}

// FollowRequestEntry is one GET /v1/me/follow-requests item.
type FollowRequestEntry struct {
	User      UserSummary `json:"user"`
	CreatedAt time.Time   `json:"created_at"`
}

// FriendRequestResult is POST /v1/friends/requests. AutoAccepted is not on
// the wire: it is true when the server answered 200 (auto-accepted) instead
// of 201.
type FriendRequestResult struct {
	Request      FriendRequest `json:"request"`
	Friend       *Friend       `json:"friend"`
	AutoAccepted bool          `json:"-"`
}

// ChannelMember is one GET /v1/channels/{id}/members item.
type ChannelMember struct {
	User     UserSummary `json:"user"`
	JoinedAt time.Time   `json:"joined_at"`
}

// SpaceBan is one GET /v1/spaces/{id}/bans item.
type SpaceBan struct {
	User      UserSummary `json:"user"`
	BannedBy  UserSummary `json:"banned_by"`
	Reason    string      `json:"reason"`
	CreatedAt time.Time   `json:"created_at"`
}

// MessageWindow is a page of channel history (always ID ascending).
type MessageWindow struct {
	Items         []Message `json:"items"`
	HasMoreBefore bool      `json:"has_more_before"`
	HasMoreAfter  bool      `json:"has_more_after"`
}

// ThreadWindow is a page of thread replies.
type ThreadWindow struct {
	Root          Message   `json:"root"`
	Items         []Message `json:"items"`
	HasMoreBefore bool      `json:"has_more_before"`
	HasMoreAfter  bool      `json:"has_more_after"`
}

// MessageSearchHit is one message search result.
type MessageSearchHit struct {
	Message    Message           `json:"message"`
	Channel    ChannelRef        `json:"channel"`
	Highlights []SearchHighlight `json:"highlights"`
}

// PostSearchHit is one post search result.
type PostSearchHit struct {
	Post       Post              `json:"post"`
	Highlights []SearchHighlight `json:"highlights"`
}

// HashtagSuggestion is one hashtag search result.
type HashtagSuggestion struct {
	Tag          string `json:"tag"`
	PostCount24h int    `json:"post_count_24h"`
}

// TimelinePage is a timeline page.
type TimelinePage struct {
	Items      []TimelineItem `json:"items"`
	NextCursor *string        `json:"next_cursor"`
	Gap        bool           `json:"gap"`
}

// PostContext is a post with its ancestors.
type PostContext struct {
	Ancestors []Post `json:"ancestors"`
	Post      Post   `json:"post"`
}

// LikeResult is the like/unlike response.
type LikeResult struct {
	PostID    string `json:"post_id"`
	Liked     bool   `json:"liked"`
	LikeCount int    `json:"like_count"`
}

// RepostResult is the repost/unrepost response.
type RepostResult struct {
	PostID      string `json:"post_id"`
	Reposted    bool   `json:"reposted"`
	RepostCount int    `json:"repost_count"`
}

// Trend is one trending hashtag.
type Trend struct {
	Rank        int    `json:"rank"`
	Tag         string `json:"tag"`
	PostCount   int    `json:"post_count"`
	AuthorCount int    `json:"author_count"`
}

// Trends is GET /v1/trends.
type Trends struct {
	Items      []Trend    `json:"items"`
	ComputedAt *time.Time `json:"computed_at"`
}

// StoryViewer is one story viewer.
type StoryViewer struct {
	User     UserSummary `json:"user"`
	ViewedAt time.Time   `json:"viewed_at"`
}

// NotificationPage is a notification inbox page.
type NotificationPage struct {
	Items       []Notification `json:"items"`
	NextCursor  *string        `json:"next_cursor"`
	UnreadCount int            `json:"unread_count"`
}

// MediaBlob is a fetched media body (not JSON: GET /v1/media/{id} bytes).
type MediaBlob struct {
	Data        []byte
	ContentType string
	ETag        string
	NotModified bool
}
