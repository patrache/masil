package api

import (
	"context"
)

type NotificationQuery struct {
	PageOpts
	UnreadOnly bool
}
type MarkNotificationsRead struct {
	IDs    []string
	All    bool
	Before string
} // exactly one set
func (c *Client) ListNotifications(ctx context.Context, q NotificationQuery) (*NotificationPage, error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) NotificationUnreadCount(ctx context.Context) (int, error) {
	// TODO(core): Task 11
	return 0, nil
}

func (c *Client) MarkNotificationsRead(ctx context.Context, r MarkNotificationsRead) (unread int, err error) {
	// TODO(core): Task 11
	return
}
