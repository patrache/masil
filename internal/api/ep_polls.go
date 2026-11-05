package api

import (
	"context"
)

// ---- 5.19 polls ----

// GetPoll: GET /v1/polls/{id}
func (c *Client) GetPoll(ctx context.Context, pollID string) (*Poll, error) {
	v, _, err := one[Poll](ctx, c, request{Method: "GET", Path: escPath("v1", "polls", pollID), Auth: true}, "poll")
	return v, err
}

// Vote: PUT /v1/polls/{id}/vote {"option_idxs"} (replaces my vote; RL_REACT).
func (c *Client) Vote(ctx context.Context, pollID string, optionIdxs []int) (*Poll, error) {
	if optionIdxs == nil {
		optionIdxs = []int{} // an array on the wire, never null (server answers 400)
	}
	body := struct {
		OptionIdxs []int `json:"option_idxs"`
	}{optionIdxs}
	v, _, err := one[Poll](ctx, c, request{Method: "PUT", Path: escPath("v1", "polls", pollID, "vote"), Body: body, Limiter: rlReact, Auth: true}, "poll")
	return v, err
}

// RetractVote: DELETE /v1/polls/{id}/vote (RL_REACT).
func (c *Client) RetractVote(ctx context.Context, pollID string) (*Poll, error) {
	v, _, err := one[Poll](ctx, c, request{Method: "DELETE", Path: escPath("v1", "polls", pollID, "vote"), Limiter: rlReact, Auth: true}, "poll")
	return v, err
}

// ClosePoll: POST /v1/polls/{id}/close (creator).
func (c *Client) ClosePoll(ctx context.Context, pollID string) (*Poll, error) {
	v, _, err := one[Poll](ctx, c, request{Method: "POST", Path: escPath("v1", "polls", pollID, "close"), Auth: true}, "poll")
	return v, err
}
