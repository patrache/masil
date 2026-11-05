package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/patrache/masil/internal/ids"
)

type CreatePostRequest struct {
	ClientNonce, Body    string
	MediaUploadIDs       []string
	ReplyToID, QuoteOfID string
	Poll                 *PollInput
} // omitempty

// createPostBody is the POST /v1/posts wire body (api.md 5.15).
type createPostBody struct {
	ClientNonce    string     `json:"client_nonce"`
	Body           string     `json:"body"`
	MediaUploadIDs []string   `json:"media_upload_ids,omitempty"`
	ReplyToID      string     `json:"reply_to_id,omitempty"`
	QuoteOfID      string     `json:"quote_of_id,omitempty"`
	Poll           *PollInput `json:"poll,omitempty"`
}

// postEnvelope is {"post": Post}.
type postEnvelope struct {
	Post Post `json:"post"`
}

// CreatePost: POST /v1/posts. created = 201 (200 = duplicate nonce). An empty
// ClientNonce gets a fresh ULID. No auto-retry (CA 4.3.1: messages only).
func (c *Client) CreatePost(ctx context.Context, r CreatePostRequest) (p *Post, created bool, err error) {
	if r.ClientNonce == "" {
		r.ClientNonce = ids.New()
	}
	var out postEnvelope
	body := createPostBody{ClientNonce: r.ClientNonce, Body: r.Body, MediaUploadIDs: r.MediaUploadIDs, ReplyToID: r.ReplyToID, QuoteOfID: r.QuoteOfID, Poll: r.Poll}
	st, err := c.call(ctx, request{Method: "POST", Path: "/v1/posts", Body: body, Limiter: rlPost, Auth: true}, &out)
	if err != nil {
		return nil, false, err
	}
	return &out.Post, st == http.StatusCreated, nil
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

// Like: PUT /v1/posts/{id}/like.
func (c *Client) Like(ctx context.Context, postID string) (*LikeResult, error) {
	var out LikeResult
	if _, err := c.call(ctx, request{Method: "PUT", Path: escPath("v1", "posts", postID, "like"), Limiter: rlReact, Auth: true}, &out); err != nil {
		return nil, err
	}
	return &out, nil
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

// HashtagPosts: GET /v1/hashtags/{tag}/posts; tag without "#" (a leading "#" is dropped), path-escaped.
func (c *Client) HashtagPosts(ctx context.Context, tag string, p PageOpts) (*Page[Post], error) {
	var out Page[Post]
	path := escPath("v1", "hashtags", strings.TrimPrefix(tag, "#"), "posts")
	if _, err := c.call(ctx, request{Method: "GET", Path: path, Query: p.values(nil), Auth: true}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Trends(ctx context.Context) (*Trends, error) {
	// TODO(core): Task 11
	return nil, nil
}
