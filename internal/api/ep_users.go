package api

import (
	"context"
	"net/url"
)

// GetUser: GET /v1/users/{user}
func (c *Client) GetUser(ctx context.Context, u UserRef) (*Profile, error) {
	v, _, err := one[Profile](ctx, c, request{Method: "GET", Path: escPath("v1", "users", string(u)), Auth: true}, "user")
	return v, err
}

// GetUsers: GET /v1/users?ids=
//
// At most 100 IDs (api.md 5.2): more is a client-side 400 invalid_request
// with no request sent; an empty list returns an empty result without a request.
func (c *Client) GetUsers(ctx context.Context, ids []string) ([]UserSummary, error) {
	v, err := idList("ids", ids)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []UserSummary{}, nil
	}
	return list[UserSummary](ctx, c, request{Method: "GET", Path: "/v1/users", Query: url.Values{"ids": {v}}, Auth: true})
}

// SearchUsers: GET /v1/users/search (limit 0 = server default)
func (c *Client) SearchUsers(ctx context.Context, q string, limit int) ([]UserSummary, error) {
	query := url.Values{"q": {q}}
	setIntIf(query, "limit", limit)
	return list[UserSummary](ctx, c, request{Method: "GET", Path: "/v1/users/search", Query: query, Limiter: rlSearch, Auth: true})
}

// ListUserPosts: filter posts|replies|media ("" = server default posts)
func (c *Client) ListUserPosts(ctx context.Context, u UserRef, filter string, p PageOpts) (*Page[TimelineItem], error) {
	q := url.Values{}
	setIf(q, "filter", filter)
	return page[TimelineItem](ctx, c, request{Method: "GET", Path: escPath("v1", "users", string(u), "posts"), Query: p.values(q), Auth: true})
}

// ListFollowers: GET /v1/users/{user}/followers
func (c *Client) ListFollowers(ctx context.Context, u UserRef, p PageOpts) (*Page[UserSummary], error) {
	return page[UserSummary](ctx, c, request{Method: "GET", Path: escPath("v1", "users", string(u), "followers"), Query: p.values(nil), Auth: true})
}

// ListFollowing: GET /v1/users/{user}/following
func (c *Client) ListFollowing(ctx context.Context, u UserRef, p PageOpts) (*Page[UserSummary], error) {
	return page[UserSummary](ctx, c, request{Method: "GET", Path: escPath("v1", "users", string(u), "following"), Query: p.values(nil), Auth: true})
}
