package api

import (
	"context"
	"net/http"
)

type OpenDMRequest struct {
	Users []UserRef
	Name  Opt[string]
}

// openDMBody is the POST /v1/dms wire body.
type openDMBody struct {
	Users []UserRef   `json:"users"`
	Name  Opt[string] `json:"name,omitzero"`
}

// ListDMs: GET /v1/dms
func (c *Client) ListDMs(ctx context.Context, p PageOpts) (*Page[Channel], error) {
	return page[Channel](ctx, c, request{Method: "GET", Path: "/v1/dms", Query: p.values(nil), Auth: true})
}

// OpenDM: POST /v1/dms. created is true on 201 (a new DM or group DM) and
// false on 200 (the existing 1:1 DM).
func (c *Client) OpenDM(ctx context.Context, r OpenDMRequest) (ch *Channel, created bool, err error) {
	users := r.Users
	if users == nil {
		users = []UserRef{}
	}
	ch, st, err := one[Channel](ctx, c, request{Method: "POST", Path: "/v1/dms", Body: openDMBody{Users: users, Name: r.Name}, Limiter: rlSocial, Auth: true}, "channel")
	if err != nil {
		return nil, false, err
	}
	return ch, st == http.StatusCreated, nil
}

// AddChannelMembers: POST /v1/channels/{channel_id}/members
func (c *Client) AddChannelMembers(ctx context.Context, channelID string, users []UserRef) (*Channel, error) {
	if users == nil {
		users = []UserRef{}
	}
	v, _, err := one[Channel](ctx, c, request{Method: "POST", Path: escPath("v1", "channels", channelID, "members"), Body: usersBody{users}, Limiter: rlSocial, Auth: true}, "channel")
	return v, err
}

// RemoveChannelMember: DELETE /v1/channels/{channel_id}/members/{user}; u=MeRef => leave
func (c *Client) RemoveChannelMember(ctx context.Context, channelID string, u UserRef) error {
	_, err := c.call(ctx, request{Method: "DELETE", Path: escPath("v1", "channels", channelID, "members", string(u)), Auth: true}, nil)
	return err
}
