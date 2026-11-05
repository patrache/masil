package api

import (
	"context"
)

func (c *Client) GetPoll(ctx context.Context, pollID string) (*Poll, error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) Vote(ctx context.Context, pollID string, optionIdxs []int) (*Poll, error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) RetractVote(ctx context.Context, pollID string) (*Poll, error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) ClosePoll(ctx context.Context, pollID string) (*Poll, error) {
	// TODO(core): Task 11
	return nil, nil
}
