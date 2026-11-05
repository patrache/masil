package api

import (
	"context"
)

func (c *Client) ListGuestbook(ctx context.Context, owner UserRef, p PageOpts) (*Page[GuestbookEntry], error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) WriteGuestbook(ctx context.Context, owner UserRef, body string, secret bool) (*GuestbookEntry, error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) DeleteGuestbookEntry(ctx context.Context, entryID string) error {
	// TODO(core): Task 11
	return nil
}
