package api

import (
	"context"
)

// GetUser: GET /v1/users/{user}
func (c *Client) GetUser(ctx context.Context, u UserRef) (*Profile, error) {
	// TODO(core): Task 10
	return nil, nil
}

// GetUsers: GET /v1/users?ids=
func (c *Client) GetUsers(ctx context.Context, ids []string) ([]UserSummary, error) {
	// TODO(core): Task 10
	return nil, nil
}

// SearchUsers: GET /v1/users/search
func (c *Client) SearchUsers(ctx context.Context, q string, limit int) ([]UserSummary, error) {
	// TODO(core): Task 10
	return nil, nil
}

// ListUserPosts: filter posts|replies|media
func (c *Client) ListUserPosts(ctx context.Context, u UserRef, filter string, p PageOpts) (*Page[TimelineItem], error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) ListFollowers(ctx context.Context, u UserRef, p PageOpts) (*Page[UserSummary], error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) ListFollowing(ctx context.Context, u UserRef, p PageOpts) (*Page[UserSummary], error) {
	// TODO(core): Task 10
	return nil, nil
}
