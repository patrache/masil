package api

import (
	"context"
)

type CreatePostRequest struct {
	ClientNonce, Body    string
	MediaUploadIDs       []string
	ReplyToID, QuoteOfID string
	Poll                 *PollInput
} // omitempty
func (c *Client) CreatePost(ctx context.Context, r CreatePostRequest) (p *Post, created bool, err error) {
	// TODO(core): Task 11
	return
}

func (c *Client) GetPost(ctx context.Context, postID string) (*Post, error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) GetPostContext(ctx context.Context, postID string) (*PostContext, error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) ListReplies(ctx context.Context, postID string, p PageOpts) (*Page[Post], error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) DeletePost(ctx context.Context, postID string) error {
	// TODO(core): Task 11
	return nil
}

func (c *Client) Like(ctx context.Context, postID string) (*LikeResult, error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) Unlike(ctx context.Context, postID string) (*LikeResult, error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) ListLikes(ctx context.Context, postID string, p PageOpts) (*Page[UserSummary], error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) Repost(ctx context.Context, postID string) (*RepostResult, error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) Unrepost(ctx context.Context, postID string) (*RepostResult, error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) ListReposts(ctx context.Context, postID string, p PageOpts) (*Page[UserSummary], error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) ListQuotes(ctx context.Context, postID string, p PageOpts) (*Page[Post], error) {
	// TODO(core): Task 11
	return nil, nil
}

type TimelineQuery struct {
	PageOpts
	Since string
}

func (c *Client) FollowingTimeline(ctx context.Context, q TimelineQuery) (*TimelinePage, error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) DiscoverTimeline(ctx context.Context, q TimelineQuery) (*TimelinePage, error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) HashtagPosts(ctx context.Context, tag string, p PageOpts) (*Page[Post], error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) Trends(ctx context.Context) (*Trends, error) {
	// TODO(core): Task 11
	return nil, nil
}
