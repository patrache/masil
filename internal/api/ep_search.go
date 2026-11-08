package api

import (
	"context"
	"net/url"
)

// ---- 5.13 search ----

// MessageSearchQuery is GET /v1/search/messages. Scope: ChannelID > SpaceID >
// everything readable. Author is a {user} reference ("" = anyone).
type MessageSearchQuery struct {
	Q, ChannelID, SpaceID string
	Author                UserRef
	PageOpts
}

// PostSearchQuery is GET /v1/search/posts.
type PostSearchQuery struct {
	Q      string
	Author UserRef
	PageOpts
}

// SearchMessages: GET /v1/search/messages?q=&channel_id=&space_id=&author=&cursor=&limit=
func (c *Client) SearchMessages(ctx context.Context, q MessageSearchQuery) (*Page[MessageSearchHit], error) {
	query := url.Values{"q": {q.Q}}
	setIf(query, "channel_id", q.ChannelID)
	setIf(query, "space_id", q.SpaceID)
	setIf(query, "author", string(q.Author))
	return page[MessageSearchHit](ctx, c, request{Method: "GET", Path: "/v1/search/messages", Query: q.PageOpts.values(query), Limiter: rlSearch, Auth: true})
}

// SearchPosts: GET /v1/search/posts?q=&author=&cursor=&limit=
func (c *Client) SearchPosts(ctx context.Context, q PostSearchQuery) (*Page[PostSearchHit], error) {
	query := url.Values{"q": {q.Q}}
	setIf(query, "author", string(q.Author))
	return page[PostSearchHit](ctx, c, request{Method: "GET", Path: "/v1/search/posts", Query: q.PageOpts.values(query), Limiter: rlSearch, Auth: true})
}

// SearchHashtags: GET /v1/hashtags/search?q=&limit= (prefix match; limit 0 = server default).
func (c *Client) SearchHashtags(ctx context.Context, q string, limit int) ([]HashtagSuggestion, error) {
	query := url.Values{"q": {q}}
	setIntIf(query, "limit", limit)
	return list[HashtagSuggestion](ctx, c, request{Method: "GET", Path: "/v1/hashtags/search", Query: query, Limiter: rlSearch, Auth: true})
}
