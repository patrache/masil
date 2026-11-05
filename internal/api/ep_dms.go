package api

import (
	"context"
)

type OpenDMRequest struct {
	Users []UserRef
	Name  Opt[string]
}

func (c *Client) ListDMs(ctx context.Context, p PageOpts) (*Page[Channel], error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) OpenDM(ctx context.Context, r OpenDMRequest) (ch *Channel, created bool, err error) {
	// TODO(core): Task 10
	return
}

func (c *Client) AddChannelMembers(ctx context.Context, channelID string, users []UserRef) (*Channel, error) {
	// TODO(core): Task 10
	return nil, nil
}

// RemoveChannelMember: u=Me => leave
func (c *Client) RemoveChannelMember(ctx context.Context, channelID string, u UserRef) error {
	// TODO(core): Task 10
	return nil
}
