package api

import (
	"context"
	"net/url"
)

// ---- 5.18 notifications ----

// NotificationQuery is GET /v1/notifications.
type NotificationQuery struct {
	PageOpts
	UnreadOnly bool
}

// MarkNotificationsRead is POST /v1/notifications/read: exactly one of IDs
// (1..100), All, or Before (that ID and older) must be set.
type MarkNotificationsRead struct {
	IDs    []string
	All    bool
	Before string
} // exactly one set

// markReadBody is the POST /v1/notifications/read wire body.
type markReadBody struct {
	IDs    []string `json:"ids,omitempty"`
	All    bool     `json:"all,omitempty"`
	Before string   `json:"before,omitempty"`
}

// unreadCount is {"unread_count": n}.
type unreadCount struct {
	UnreadCount int `json:"unread_count"`
}

// ListNotifications: GET /v1/notifications?cursor=&limit=&unread_only=
func (c *Client) ListNotifications(ctx context.Context, q NotificationQuery) (*NotificationPage, error) {
	query := url.Values{}
	if q.UnreadOnly {
		query.Set("unread_only", "true")
	}
	var out NotificationPage
	if _, err := c.call(ctx, request{Method: "GET", Path: "/v1/notifications", Query: q.PageOpts.values(query), Auth: true}, &out); err != nil {
		return nil, err
	}
	if out.Items == nil {
		out.Items = []Notification{}
	}
	return &out, nil
}

// NotificationUnreadCount: GET /v1/notifications/unread-count
func (c *Client) NotificationUnreadCount(ctx context.Context) (int, error) {
	var out unreadCount
	if _, err := c.call(ctx, request{Method: "GET", Path: "/v1/notifications/unread-count", Auth: true}, &out); err != nil {
		return 0, err
	}
	return out.UnreadCount, nil
}

// MarkNotificationsRead: POST /v1/notifications/read; returns the new unread
// count. Zero or several selectors, or more than 100 IDs, is a client-side
// invalid_request with no request sent.
func (c *Client) MarkNotificationsRead(ctx context.Context, r MarkNotificationsRead) (unread int, err error) {
	set := 0
	if len(r.IDs) > 0 {
		set++
	}
	if r.All {
		set++
	}
	if r.Before != "" {
		set++
	}
	if set != 1 {
		return 0, invalidRequest("ids", "invalid", "exactly one of ids/all/before must be set")
	}
	if len(r.IDs) > maxIDList {
		return 0, invalidRequest("ids", "invalid", "at most 100 ids")
	}
	var out unreadCount
	body := markReadBody{IDs: r.IDs, All: r.All, Before: r.Before}
	if _, err := c.call(ctx, request{Method: "POST", Path: "/v1/notifications/read", Body: body, Auth: true}, &out); err != nil {
		return 0, err
	}
	return out.UnreadCount, nil
}
