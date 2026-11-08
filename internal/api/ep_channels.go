package api

import (
	"context"
)

type CreateChannelRequest struct {
	Name      string
	Topic     string
	IsPrivate bool
	Members   []UserRef
} // omitempty
type UpdateChannelRequest struct {
	Name, Topic Opt[string]
	Position    Opt[int]
}
type ChannelPrefsRequest struct {
	Notify Opt[string]
	Muted  Opt[bool]
}

// createChannelBody is the POST /v1/spaces/{space_id}/channels wire body.
type createChannelBody struct {
	Name      string    `json:"name"`
	Topic     string    `json:"topic,omitempty"`
	IsPrivate bool      `json:"is_private,omitempty"`
	Members   []UserRef `json:"members,omitempty"`
}

// updateChannelBody is the PATCH /v1/channels/{channel_id} wire body.
type updateChannelBody struct {
	Name     Opt[string] `json:"name,omitzero"`
	Topic    Opt[string] `json:"topic,omitzero"`
	Position Opt[int]    `json:"position,omitzero"`
}

// channelPrefsBody is the PUT /v1/channels/{channel_id}/prefs wire body.
type channelPrefsBody struct {
	Notify Opt[string] `json:"notify,omitzero"`
	Muted  Opt[bool]   `json:"muted,omitzero"`
}

// ListChannels: GET /v1/spaces/{space_id}/channels
func (c *Client) ListChannels(ctx context.Context, spaceID string) ([]Channel, error) {
	return list[Channel](ctx, c, request{Method: "GET", Path: escPath("v1", "spaces", spaceID, "channels"), Auth: true})
}

// CreateChannel: POST /v1/spaces/{space_id}/channels
func (c *Client) CreateChannel(ctx context.Context, spaceID string, r CreateChannelRequest) (*Channel, error) {
	body := createChannelBody{Name: r.Name, Topic: r.Topic, IsPrivate: r.IsPrivate, Members: r.Members}
	v, _, err := one[Channel](ctx, c, request{Method: "POST", Path: escPath("v1", "spaces", spaceID, "channels"), Body: body, Auth: true}, "channel")
	return v, err
}

// GetChannel: GET /v1/channels/{channel_id}
func (c *Client) GetChannel(ctx context.Context, channelID string) (*Channel, error) {
	v, _, err := one[Channel](ctx, c, request{Method: "GET", Path: escPath("v1", "channels", channelID), Auth: true}, "channel")
	return v, err
}

// UpdateChannel: PATCH /v1/channels/{channel_id}
func (c *Client) UpdateChannel(ctx context.Context, channelID string, r UpdateChannelRequest) (*Channel, error) {
	body := updateChannelBody{Name: r.Name, Topic: r.Topic, Position: r.Position}
	v, _, err := one[Channel](ctx, c, request{Method: "PATCH", Path: escPath("v1", "channels", channelID), Body: body, Auth: true}, "channel")
	return v, err
}

// DeleteChannel: DELETE /v1/channels/{channel_id}
func (c *Client) DeleteChannel(ctx context.Context, channelID string) error {
	_, err := c.call(ctx, request{Method: "DELETE", Path: escPath("v1", "channels", channelID), Auth: true}, nil)
	return err
}

// ListChannelMembers: GET /v1/channels/{channel_id}/members
func (c *Client) ListChannelMembers(ctx context.Context, channelID string, p PageOpts) (*Page[ChannelMember], error) {
	return page[ChannelMember](ctx, c, request{Method: "GET", Path: escPath("v1", "channels", channelID, "members"), Query: p.values(nil), Auth: true})
}

// SetChannelPrefs: PUT /v1/channels/{channel_id}/prefs
func (c *Client) SetChannelPrefs(ctx context.Context, channelID string, r ChannelPrefsRequest) (*ChannelMyState, error) {
	body := channelPrefsBody{Notify: r.Notify, Muted: r.Muted}
	v, _, err := one[ChannelMyState](ctx, c, request{Method: "PUT", Path: escPath("v1", "channels", channelID, "prefs"), Body: body, Auth: true}, "my")
	return v, err
}

// MarkRead: PUT /v1/channels/{channel_id}/read
func (c *Client) MarkRead(ctx context.Context, channelID, messageID string) (*UnreadState, error) {
	body := struct {
		MessageID string `json:"message_id"`
	}{messageID}
	v, _, err := one[UnreadState](ctx, c, request{Method: "PUT", Path: escPath("v1", "channels", channelID, "read"), Body: body, Auth: true}, "state")
	return v, err
}
