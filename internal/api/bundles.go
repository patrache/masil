package api

import "time"

// Endpoint-specific response bundles (CA 4.3.2). JSON tags (snake_case) are
// finalized in Task 9 against api.md.

// AuthResult is the signup/login response.
type AuthResult struct {
	Token   string  `json:"token"`
	Session Session `json:"session"`
	User    Me      `json:"user"`
}

// MetaInfo is GET /v1/meta.
type MetaInfo struct {
	APIVersion       int
	ServerTime       time.Time
	MinClientVersion string
	Limits           MetaLimits
}

// MetaLimits is MetaInfo.limits.
type MetaLimits struct {
	MessageMaxChars, PostMaxChars, PostMaxMedia, UploadMaxBytes, GroupDMMaxMembers, StoryTTLSec, PollMaxOptions, ReactionsPerMessage, WatchTopicsMax int
}

// PresenceItem is one GET /v1/presence item.
type PresenceItem struct {
	UserID     string
	Status     PresenceStatus
	LastSeenAt *time.Time
}

// BlockEntry is one GET /v1/blocks item.
type BlockEntry struct {
	User      UserSummary
	CreatedAt time.Time
}

// FollowRequestEntry is one follow request.
type FollowRequestEntry struct {
	User      UserSummary
	CreatedAt time.Time
}

// FriendRequestResult is POST /v1/friends/requests; AutoAccepted = HTTP 200.
type FriendRequestResult struct {
	Request      FriendRequest
	Friend       *Friend
	AutoAccepted bool
}

// ChannelMember is one channel member.
type ChannelMember struct {
	User     UserSummary
	JoinedAt time.Time
}

// SpaceBan is one space ban.
type SpaceBan struct {
	User      UserSummary
	BannedBy  UserSummary
	Reason    string
	CreatedAt time.Time
}

// MessageWindow is a page of channel history.
type MessageWindow struct {
	Items                       []Message
	HasMoreBefore, HasMoreAfter bool
}

// ThreadWindow is a page of thread replies.
type ThreadWindow struct {
	Root                        Message
	Items                       []Message
	HasMoreBefore, HasMoreAfter bool
}

// MessageSearchHit is one message search result.
type MessageSearchHit struct {
	Message    Message
	Channel    ChannelRef
	Highlights []SearchHighlight
}

// PostSearchHit is one post search result.
type PostSearchHit struct {
	Post       Post
	Highlights []SearchHighlight
}

// HashtagSuggestion is one hashtag search result.
type HashtagSuggestion struct {
	Tag          string
	PostCount24h int `json:"post_count_24h"`
}

// TimelinePage is a timeline page.
type TimelinePage struct {
	Items      []TimelineItem
	NextCursor *string
	Gap        bool
}

// PostContext is a post with its ancestors.
type PostContext struct {
	Ancestors []Post
	Post      Post
}

// LikeResult is the like/unlike response.
type LikeResult struct {
	PostID    string
	Liked     bool
	LikeCount int
}

// RepostResult is the repost/unrepost response.
type RepostResult struct {
	PostID      string
	Reposted    bool
	RepostCount int
}

// Trend is one trending hashtag.
type Trend struct {
	Rank                   int
	Tag                    string
	PostCount, AuthorCount int
}

// Trends is GET /v1/trends.
type Trends struct {
	Items      []Trend
	ComputedAt *time.Time
}

// StoryViewer is one story viewer.
type StoryViewer struct {
	User     UserSummary
	ViewedAt time.Time
}

// NotificationPage is a notification inbox page.
type NotificationPage struct {
	Items       []Notification
	NextCursor  *string
	UnreadCount int
}

// MediaBlob is a fetched media body.
type MediaBlob struct {
	Data              []byte
	ContentType, ETag string
	NotModified       bool
}
