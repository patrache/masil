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

func (c *Client) ListChannels(ctx context.Context, spaceID string) ([]Channel, error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) CreateChannel(ctx context.Context, spaceID string, r CreateChannelRequest) (*Channel, error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) GetChannel(ctx context.Context, channelID string) (*Channel, error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) UpdateChannel(ctx context.Context, channelID string, r UpdateChannelRequest) (*Channel, error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) DeleteChannel(ctx context.Context, channelID string) error {
	// TODO(core): Task 10
	return nil
}

func (c *Client) ListChannelMembers(ctx context.Context, channelID string, p PageOpts) (*Page[ChannelMember], error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) SetChannelPrefs(ctx context.Context, channelID string, r ChannelPrefsRequest) (*ChannelMyState, error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) MarkRead(ctx context.Context, channelID, messageID string) (*UnreadState, error) {
	// TODO(core): Task 10
	return nil, nil
}
