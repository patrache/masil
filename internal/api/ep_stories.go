package api

import (
	"context"
)

// ---- 5.17 stories ----

// CreateStoryRequest is POST /v1/stories (body and/or media; BG
// accent|accent2|accent3|accent4|plain). Empty fields are omitted.
type CreateStoryRequest struct{ Body, MediaUploadID, BG string } // omitempty

// createStoryBody is the POST /v1/stories wire body.
type createStoryBody struct {
	Body          string `json:"body,omitempty"`
	MediaUploadID string `json:"media_upload_id,omitempty"`
	BG            string `json:"bg,omitempty"`
}

// CreateStory: POST /v1/stories (RL_POST).
func (c *Client) CreateStory(ctx context.Context, r CreateStoryRequest) (*Story, error) {
	body := createStoryBody{Body: r.Body, MediaUploadID: r.MediaUploadID, BG: r.BG}
	v, _, err := one[Story](ctx, c, request{Method: "POST", Path: "/v1/stories", Body: body, Limiter: rlPost, Auth: true}, "story")
	return v, err
}

// StoryFeed: GET /v1/stories/feed
func (c *Client) StoryFeed(ctx context.Context) ([]StoryGroup, error) {
	return list[StoryGroup](ctx, c, request{Method: "GET", Path: "/v1/stories/feed", Auth: true})
}

// ListUserStories: GET /v1/users/{user}/stories
func (c *Client) ListUserStories(ctx context.Context, u UserRef) ([]Story, error) {
	return list[Story](ctx, c, request{Method: "GET", Path: escPath("v1", "users", string(u), "stories"), Auth: true})
}

// ViewStory: POST /v1/stories/{id}/view (idempotent, 204).
func (c *Client) ViewStory(ctx context.Context, storyID string) error {
	_, err := c.call(ctx, request{Method: "POST", Path: escPath("v1", "stories", storyID, "view"), Auth: true}, nil)
	return err
}

// ListStoryViewers: GET /v1/stories/{id}/viewers?cursor=&limit= (author only).
func (c *Client) ListStoryViewers(ctx context.Context, storyID string, p PageOpts) (*Page[StoryViewer], error) {
	return page[StoryViewer](ctx, c, request{Method: "GET", Path: escPath("v1", "stories", storyID, "viewers"), Query: p.values(nil), Auth: true})
}

// DeleteStory: DELETE /v1/stories/{id} (204).
func (c *Client) DeleteStory(ctx context.Context, storyID string) error {
	_, err := c.call(ctx, request{Method: "DELETE", Path: escPath("v1", "stories", storyID), Auth: true}, nil)
	return err
}
