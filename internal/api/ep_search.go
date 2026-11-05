package api

import (
	"context"
)

type MessageSearchQuery struct {
	Q, ChannelID, SpaceID string
	Author                UserRef
	PageOpts
}
type PostSearchQuery struct {
	Q      string
	Author UserRef
	PageOpts
}

func (c *Client) SearchMessages(ctx context.Context, q MessageSearchQuery) (*Page[MessageSearchHit], error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) SearchPosts(ctx context.Context, q PostSearchQuery) (*Page[PostSearchHit], error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) SearchHashtags(ctx context.Context, q string, limit int) ([]HashtagSuggestion, error) {
	// TODO(core): Task 11
	return nil, nil
}
