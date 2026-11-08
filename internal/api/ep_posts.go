package api

import (
	"context"
	"net/http"
	"net/url"
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

// GetPost: GET /v1/posts/{id}
func (c *Client) GetPost(ctx context.Context, postID string) (*Post, error) {
	v, _, err := one[Post](ctx, c, request{Method: "GET", Path: escPath("v1", "posts", postID), Auth: true}, "post")
	return v, err
}

// GetPostContext: GET /v1/posts/{id}/context (ancestors from the root, max 20).
func (c *Client) GetPostContext(ctx context.Context, postID string) (*PostContext, error) {
	var out PostContext
	if _, err := c.call(ctx, request{Method: "GET", Path: escPath("v1", "posts", postID, "context"), Auth: true}, &out); err != nil {
		return nil, err
	}
	if out.Ancestors == nil {
		out.Ancestors = []Post{}
	}
	return &out, nil
}

// ListReplies: GET /v1/posts/{id}/replies?cursor=&limit= (direct replies, ID ascending).
func (c *Client) ListReplies(ctx context.Context, postID string, p PageOpts) (*Page[Post], error) {
	return page[Post](ctx, c, request{Method: "GET", Path: escPath("v1", "posts", postID, "replies"), Query: p.values(nil), Auth: true})
}

// DeletePost: DELETE /v1/posts/{id} (204).
func (c *Client) DeletePost(ctx context.Context, postID string) error {
	_, err := c.call(ctx, request{Method: "DELETE", Path: escPath("v1", "posts", postID), Auth: true}, nil)
	return err
}

// likeOrRepost performs PUT/DELETE /v1/posts/{id}/<what> (RL_REACT) into T.
func likeOrRepost[T any](ctx context.Context, c *Client, method, postID, what string) (*T, error) {
	var out T
	if _, err := c.call(ctx, request{Method: method, Path: escPath("v1", "posts", postID, what), Limiter: rlReact, Auth: true}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Like: PUT /v1/posts/{id}/like.
func (c *Client) Like(ctx context.Context, postID string) (*LikeResult, error) {
	return likeOrRepost[LikeResult](ctx, c, "PUT", postID, "like")
}

// Unlike: DELETE /v1/posts/{id}/like.
func (c *Client) Unlike(ctx context.Context, postID string) (*LikeResult, error) {
	return likeOrRepost[LikeResult](ctx, c, "DELETE", postID, "like")
}

// ListLikes: GET /v1/posts/{id}/likes?cursor=&limit=
func (c *Client) ListLikes(ctx context.Context, postID string, p PageOpts) (*Page[UserSummary], error) {
	return page[UserSummary](ctx, c, request{Method: "GET", Path: escPath("v1", "posts", postID, "likes"), Query: p.values(nil), Auth: true})
}

// Repost: PUT /v1/posts/{id}/repost.
func (c *Client) Repost(ctx context.Context, postID string) (*RepostResult, error) {
	return likeOrRepost[RepostResult](ctx, c, "PUT", postID, "repost")
}

// Unrepost: DELETE /v1/posts/{id}/repost.
func (c *Client) Unrepost(ctx context.Context, postID string) (*RepostResult, error) {
	return likeOrRepost[RepostResult](ctx, c, "DELETE", postID, "repost")
}

// ListReposts: GET /v1/posts/{id}/reposts?cursor=&limit=
func (c *Client) ListReposts(ctx context.Context, postID string, p PageOpts) (*Page[UserSummary], error) {
	return page[UserSummary](ctx, c, request{Method: "GET", Path: escPath("v1", "posts", postID, "reposts"), Query: p.values(nil), Auth: true})
}

// ListQuotes: GET /v1/posts/{id}/quotes?cursor=&limit=
func (c *Client) ListQuotes(ctx context.Context, postID string, p PageOpts) (*Page[Post], error) {
	return page[Post](ctx, c, request{Method: "GET", Path: escPath("v1", "posts", postID, "quotes"), Query: p.values(nil), Auth: true})
}

// ---- 5.16 timelines / hashtags / trends ----

// TimelineQuery is the shared timeline query (api.md 5.16): cursor/limit, and
// Since = a TimelineItem.id to fetch only newer items (Gap when more than limit).
type TimelineQuery struct {
	PageOpts
	Since string
}

// timeline performs GET <path>?cursor=&limit=&since=.
func (c *Client) timeline(ctx context.Context, path string, q TimelineQuery) (*TimelinePage, error) {
	query := url.Values{}
	setIf(query, "since", q.Since)
	var out TimelinePage
	if _, err := c.call(ctx, request{Method: "GET", Path: path, Query: q.PageOpts.values(query), Auth: true}, &out); err != nil {
		return nil, err
	}
	if out.Items == nil {
		out.Items = []TimelineItem{}
	}
	return &out, nil
}

// FollowingTimeline: GET /v1/timeline/following
func (c *Client) FollowingTimeline(ctx context.Context, q TimelineQuery) (*TimelinePage, error) {
	return c.timeline(ctx, "/v1/timeline/following", q)
}

// DiscoverTimeline: GET /v1/timeline/discover
func (c *Client) DiscoverTimeline(ctx context.Context, q TimelineQuery) (*TimelinePage, error) {
	return c.timeline(ctx, "/v1/timeline/discover", q)
}

// HashtagPosts: GET /v1/hashtags/{tag}/posts; tag without "#" (a leading "#" is dropped), path-escaped.
func (c *Client) HashtagPosts(ctx context.Context, tag string, p PageOpts) (*Page[Post], error) {
	path := escPath("v1", "hashtags", strings.TrimPrefix(tag, "#"), "posts")
	return page[Post](ctx, c, request{Method: "GET", Path: path, Query: p.values(nil), Auth: true})
}

// Trends: GET /v1/trends
func (c *Client) Trends(ctx context.Context) (*Trends, error) {
	var out Trends
	if _, err := c.call(ctx, request{Method: "GET", Path: "/v1/trends", Auth: true}, &out); err != nil {
		return nil, err
	}
	if out.Items == nil {
		out.Items = []Trend{}
	}
	return &out, nil
}
