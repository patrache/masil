package api

import (
	"context"
	"net/url"
)

// ---- 5.25 LeetCode daily problem (v0.1.3 P1) ----

// setSpaceLeetBody is the PUT /v1/spaces/{space_id}/leet wire body.
type setSpaceLeetBody struct {
	Enabled   bool    `json:"enabled"`
	ChannelID *string `json:"channel_id,omitempty"`
}

// leetSolveBody is the PUT /v1/leet/solves/{date} wire body.
type leetSolveBody struct {
	Status            string   `json:"status"`
	Slug              string   `json:"slug"`
	Lang              string   `json:"lang"`
	FrontendID        string   `json:"frontend_id"`
	RuntimePercentile *float64 `json:"runtime_percentile,omitempty"`
	SubmissionID      *string  `json:"submission_id,omitempty"`
	ShareSpaceIDs     []string `json:"share_space_ids,omitempty"`
	Code              *string  `json:"code,omitempty"`
}

// SetSpaceLeet: PUT /v1/spaces/{space_id}/leet {enabled, channel_id?}
// (owner/admin; RL_SOCIAL). channelID nil = the remembered or a new `daily`.
func (c *Client) SetSpaceLeet(ctx context.Context, spaceID string, enabled bool, channelID *string) (*LeetSpaceSettings, error) {
	body := setSpaceLeetBody{Enabled: enabled, ChannelID: channelID}
	v, _, err := one[LeetSpaceSettings](ctx, c, request{Method: "PUT", Path: escPath("v1", "spaces", spaceID, "leet"), Body: body, Limiter: rlSocial, Auth: true}, "settings")
	return v, err
}

// ReportLeetDaily: PUT /v1/leet/daily/{date} — the facts this client fetched
// for a UTC day (today or yesterday); the server makes missing #daily roots.
// RL_SOCIAL (the server also counts RL_MESSAGE when it makes a root).
func (c *Client) ReportLeetDaily(ctx context.Context, date string, f LeetDailyFacts) (*LeetDailyReport, error) {
	if f.Tags == nil {
		f.Tags = []string{}
	}
	var out LeetDailyReport
	if _, err := c.call(ctx, request{Method: "PUT", Path: escPath("v1", "leet", "daily", date), Body: f, Limiter: rlSocial, Auth: true}, &out); err != nil {
		return nil, err
	}
	if out.Roots == nil {
		out.Roots = []LeetDailyRoot{}
	}
	return &out, nil
}

// PutLeetSolve: PUT /v1/leet/solves/{date} — my accepted solve and, per
// shared space, my solve card in that day's #daily thread (RL_SOCIAL).
func (c *Client) PutLeetSolve(ctx context.Context, date string, r LeetSolveRequest) (*LeetSolveResult, error) {
	body := leetSolveBody{
		Status: "accepted", Slug: r.Slug, Lang: r.Lang, FrontendID: r.FrontendID,
		RuntimePercentile: r.RuntimePercentile, SubmissionID: r.SubmissionID, ShareSpaceIDs: r.ShareSpaceIDs, Code: r.Code,
	}
	var out LeetSolveResult
	if _, err := c.call(ctx, request{Method: "PUT", Path: escPath("v1", "leet", "solves", date), Body: body, Limiter: rlSocial, Auth: true}, &out); err != nil {
		return nil, err
	}
	if out.Cards == nil {
		out.Cards = []LeetSolveCardRef{}
	}
	return &out, nil
}

// DeleteLeetSolve: DELETE /v1/leet/solves/{date} — my record and my solve
// cards of that day (RL_SOCIAL).
func (c *Client) DeleteLeetSolve(ctx context.Context, date string) error {
	_, err := c.call(ctx, request{Method: "DELETE", Path: escPath("v1", "leet", "solves", date), Limiter: rlSocial, Auth: true}, nil)
	return err
}

// LeetMe: GET /v1/leet/me — my streaks, last 30 days and #daily spaces.
func (c *Client) LeetMe(ctx context.Context) (*LeetMe, error) {
	var out LeetMe
	if _, err := c.call(ctx, request{Method: "GET", Path: "/v1/leet/me", Auth: true}, &out); err != nil {
		return nil, err
	}
	if out.Recent == nil {
		out.Recent = []LeetSolve{}
	}
	if out.DailySpaces == nil {
		out.DailySpaces = []LeetDailySpace{}
	}
	return &out, nil
}

// LeetSpaceBoard: GET /v1/spaces/{space_id}/leet/board?period=week|month|all
// (period "" = the server default, week).
func (c *Client) LeetSpaceBoard(ctx context.Context, spaceID, period string) (*LeetBoard, error) {
	q := url.Values{}
	if period != "" {
		q.Set("period", period)
	}
	var out LeetBoard
	if _, err := c.call(ctx, request{Method: "GET", Path: escPath("v1", "spaces", spaceID, "leet", "board"), Query: q, Auth: true}, &out); err != nil {
		return nil, err
	}
	if out.Items == nil {
		out.Items = []LeetBoardEntry{}
	}
	return &out, nil
}

// LeetFriendsBoard: GET /v1/leet/friends/board — streaks of me and the
// friends who share them.
func (c *Client) LeetFriendsBoard(ctx context.Context) (*LeetFriendsBoard, error) {
	var out LeetFriendsBoard
	if _, err := c.call(ctx, request{Method: "GET", Path: "/v1/leet/friends/board", Auth: true}, &out); err != nil {
		return nil, err
	}
	if out.Items == nil {
		out.Items = []LeetFriendStreak{}
	}
	return &out, nil
}
