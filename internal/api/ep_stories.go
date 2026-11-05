package api

import (
	"context"
)

type CreateStoryRequest struct{ Body, MediaUploadID, BG string } // omitempty
func (c *Client) CreateStory(ctx context.Context, r CreateStoryRequest) (*Story, error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) StoryFeed(ctx context.Context) ([]StoryGroup, error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) ListUserStories(ctx context.Context, u UserRef) ([]Story, error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) ViewStory(ctx context.Context, storyID string) error {
	// TODO(core): Task 11
	return nil
}

func (c *Client) ListStoryViewers(ctx context.Context, storyID string, p PageOpts) (*Page[StoryViewer], error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) DeleteStory(ctx context.Context, storyID string) error {
	// TODO(core): Task 11
	return nil
}
