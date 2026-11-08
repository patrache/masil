package api

import (
	"context"
)

// ---- 5.20 guestbook ----

// guestbookBody is the POST /v1/users/{user}/guestbook wire body.
type guestbookBody struct {
	Body     string `json:"body"`
	IsSecret bool   `json:"is_secret,omitempty"`
}

// ListGuestbook: GET /v1/users/{user}/guestbook?cursor=&limit= (newest first).
func (c *Client) ListGuestbook(ctx context.Context, owner UserRef, p PageOpts) (*Page[GuestbookEntry], error) {
	return page[GuestbookEntry](ctx, c, request{Method: "GET", Path: escPath("v1", "users", string(owner), "guestbook"), Query: p.values(nil), Auth: true})
}

// WriteGuestbook: POST /v1/users/{user}/guestbook (RL_POST); is_secret only sent when true.
func (c *Client) WriteGuestbook(ctx context.Context, owner UserRef, body string, secret bool) (*GuestbookEntry, error) {
	req := request{Method: "POST", Path: escPath("v1", "users", string(owner), "guestbook"), Body: guestbookBody{Body: body, IsSecret: secret}, Limiter: rlPost, Auth: true}
	v, _, err := one[GuestbookEntry](ctx, c, req, "entry")
	return v, err
}

// DeleteGuestbookEntry: DELETE /v1/guestbook/{id} (author or owner; 204).
func (c *Client) DeleteGuestbookEntry(ctx context.Context, entryID string) error {
	_, err := c.call(ctx, request{Method: "DELETE", Path: escPath("v1", "guestbook", entryID), Auth: true}, nil)
	return err
}
