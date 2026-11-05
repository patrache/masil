package api

import (
	"context"
)

func (c *Client) ListFriends(ctx context.Context, p PageOpts) (*Page[Friend], error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) RemoveFriend(ctx context.Context, u UserRef) error {
	// TODO(core): Task 10
	return nil
}

// ListFriendRequests: incoming|outgoing
func (c *Client) ListFriendRequests(ctx context.Context, direction string) ([]FriendRequest, error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) SendFriendRequest(ctx context.Context, u UserRef) (*FriendRequestResult, error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) AcceptFriendRequest(ctx context.Context, requestID string) (*Friend, error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) DeclineFriendRequest(ctx context.Context, requestID string) error {
	// TODO(core): Task 10
	return nil
}

func (c *Client) CancelFriendRequest(ctx context.Context, requestID string) error {
	// TODO(core): Task 10
	return nil
}

func (c *Client) ListBlocks(ctx context.Context) ([]BlockEntry, error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) Block(ctx context.Context, u UserRef) error {
	// TODO(core): Task 10
	return nil
}

func (c *Client) Unblock(ctx context.Context, u UserRef) error {
	// TODO(core): Task 10
	return nil
}

// Follow: active|pending
func (c *Client) Follow(ctx context.Context, u UserRef) (state string, err error) {
	// TODO(core): Task 10
	return
}

func (c *Client) Unfollow(ctx context.Context, u UserRef) error {
	// TODO(core): Task 10
	return nil
}

func (c *Client) RemoveFollower(ctx context.Context, u UserRef) error {
	// TODO(core): Task 10
	return nil
}

func (c *Client) ListFollowRequests(ctx context.Context, p PageOpts) (*Page[FollowRequestEntry], error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) AcceptFollowRequest(ctx context.Context, u UserRef) error {
	// TODO(core): Task 10
	return nil
}

func (c *Client) DeclineFollowRequest(ctx context.Context, u UserRef) error {
	// TODO(core): Task 10
	return nil
}
