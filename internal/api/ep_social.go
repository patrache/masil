package api

import (
	"context"
	"net/http"
	"net/url"
)

// ---- 5.3 friends ----

// ListFriends: GET /v1/friends
func (c *Client) ListFriends(ctx context.Context, p PageOpts) (*Page[Friend], error) {
	return page[Friend](ctx, c, request{Method: "GET", Path: "/v1/friends", Query: p.values(nil), Auth: true})
}

// RemoveFriend: DELETE /v1/friends/{user}
func (c *Client) RemoveFriend(ctx context.Context, u UserRef) error {
	_, err := c.call(ctx, request{Method: "DELETE", Path: escPath("v1", "friends", string(u)), Auth: true}, nil)
	return err
}

// ListFriendRequests: incoming|outgoing ("" = no filter sent)
func (c *Client) ListFriendRequests(ctx context.Context, direction string) ([]FriendRequest, error) {
	q := url.Values{}
	setIf(q, "direction", direction)
	return list[FriendRequest](ctx, c, request{Method: "GET", Path: "/v1/friends/requests", Query: q, Auth: true})
}

// SendFriendRequest: POST /v1/friends/requests. A 200 answer means the
// other side had already asked me and the request was auto-accepted
// (AutoAccepted, Friend set); 201 is a new pending request.
func (c *Client) SendFriendRequest(ctx context.Context, u UserRef) (*FriendRequestResult, error) {
	var out FriendRequestResult
	st, err := c.call(ctx, request{Method: "POST", Path: "/v1/friends/requests", Body: userBody{u}, Limiter: rlSocial, Auth: true}, &out)
	if err != nil {
		return nil, err
	}
	out.AutoAccepted = st == http.StatusOK
	return &out, nil
}

// AcceptFriendRequest: POST /v1/friends/requests/{request_id}/accept
func (c *Client) AcceptFriendRequest(ctx context.Context, requestID string) (*Friend, error) {
	v, _, err := one[Friend](ctx, c, request{Method: "POST", Path: escPath("v1", "friends", "requests", requestID, "accept"), Auth: true}, "friend")
	return v, err
}

// DeclineFriendRequest: POST /v1/friends/requests/{request_id}/decline
func (c *Client) DeclineFriendRequest(ctx context.Context, requestID string) error {
	_, err := c.call(ctx, request{Method: "POST", Path: escPath("v1", "friends", "requests", requestID, "decline"), Auth: true}, nil)
	return err
}

// CancelFriendRequest: DELETE /v1/friends/requests/{request_id}
func (c *Client) CancelFriendRequest(ctx context.Context, requestID string) error {
	_, err := c.call(ctx, request{Method: "DELETE", Path: escPath("v1", "friends", "requests", requestID), Auth: true}, nil)
	return err
}

// ---- 5.4 blocks ----

// ListBlocks: GET /v1/blocks
func (c *Client) ListBlocks(ctx context.Context) ([]BlockEntry, error) {
	return list[BlockEntry](ctx, c, request{Method: "GET", Path: "/v1/blocks", Auth: true})
}

// Block: PUT /v1/blocks/{user}
func (c *Client) Block(ctx context.Context, u UserRef) error {
	_, err := c.call(ctx, request{Method: "PUT", Path: escPath("v1", "blocks", string(u)), Limiter: rlSocial, Auth: true}, nil)
	return err
}

// Unblock: DELETE /v1/blocks/{user}
func (c *Client) Unblock(ctx context.Context, u UserRef) error {
	_, err := c.call(ctx, request{Method: "DELETE", Path: escPath("v1", "blocks", string(u)), Auth: true}, nil)
	return err
}

// ---- 5.5 follow ----

// Follow: PUT /v1/users/{user}/follow => active|pending
func (c *Client) Follow(ctx context.Context, u UserRef) (state string, err error) {
	var out struct {
		State string `json:"state"`
	}
	if _, err := c.call(ctx, request{Method: "PUT", Path: escPath("v1", "users", string(u), "follow"), Limiter: rlSocial, Auth: true}, &out); err != nil {
		return "", err
	}
	return out.State, nil
}

// Unfollow: DELETE /v1/users/{user}/follow (also cancels a pending request)
func (c *Client) Unfollow(ctx context.Context, u UserRef) error {
	_, err := c.call(ctx, request{Method: "DELETE", Path: escPath("v1", "users", string(u), "follow"), Auth: true}, nil)
	return err
}

// RemoveFollower: DELETE /v1/users/{user}/follower
func (c *Client) RemoveFollower(ctx context.Context, u UserRef) error {
	_, err := c.call(ctx, request{Method: "DELETE", Path: escPath("v1", "users", string(u), "follower"), Auth: true}, nil)
	return err
}

// ListFollowRequests: GET /v1/me/follow-requests
func (c *Client) ListFollowRequests(ctx context.Context, p PageOpts) (*Page[FollowRequestEntry], error) {
	return page[FollowRequestEntry](ctx, c, request{Method: "GET", Path: "/v1/me/follow-requests", Query: p.values(nil), Auth: true})
}

// AcceptFollowRequest: POST /v1/me/follow-requests/{user}/accept
func (c *Client) AcceptFollowRequest(ctx context.Context, u UserRef) error {
	_, err := c.call(ctx, request{Method: "POST", Path: escPath("v1", "me", "follow-requests", string(u), "accept"), Auth: true}, nil)
	return err
}

// DeclineFollowRequest: POST /v1/me/follow-requests/{user}/decline
func (c *Client) DeclineFollowRequest(ctx context.Context, u UserRef) error {
	_, err := c.call(ctx, request{Method: "POST", Path: escPath("v1", "me", "follow-requests", string(u), "decline"), Auth: true}, nil)
	return err
}
