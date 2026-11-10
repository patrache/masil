package api

import (
	"context"
	"net/http"
	"net/url"

	"github.com/patrache/masil/internal/ids"
)

// ---- 5.10 messages ----

// MessageQuery selects a window of channel history (api.md 5.10). At most one
// anchor (Before/After/Around) may be set; Limit 0 = server default (50).
type MessageQuery struct {
	Before, After, Around string
	Limit                 int
} // at most one anchor

// SendMessageRequest is POST /v1/channels/{id}/messages. Kind "" = text.
type SendMessageRequest struct {
	ClientNonce string
	Kind        string
	Body        string
	Poll        *PollInput
	Card        *Card // with Kind MessageKindCard (Body = the fallback summary)
}

// sendMessageBody is the POST /v1/channels/{id}/messages wire body (api.md 5.10).
type sendMessageBody struct {
	ClientNonce string     `json:"client_nonce"`
	Kind        string     `json:"kind,omitempty"`
	Body        string     `json:"body"`
	Poll        *PollInput `json:"poll,omitempty"`
	Card        *Card      `json:"card,omitempty"`
}

// messageEnvelope is {"message": Message}.
type messageEnvelope struct {
	Message Message `json:"message"`
}

// anchors encodes the before/after/around anchors into q, rejecting more than
// one locally (api.md 5.10/5.11: "at most one").
func anchors(q url.Values, kv ...string) error {
	n := 0
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] != "" {
			n++
			q.Set(kv[i], kv[i+1])
		}
	}
	if n > 1 {
		return invalidRequest("anchor", "invalid", "at most one of before/after/around may be set")
	}
	return nil
}

// ListMessages: GET /v1/channels/{id}/messages?before=|after=|around=&limit=
// (items always ID ascending). Two or more anchors is a client-side
// invalid_request with no request sent.
func (c *Client) ListMessages(ctx context.Context, channelID string, q MessageQuery) (*MessageWindow, error) {
	query := url.Values{}
	if err := anchors(query, "before", q.Before, "after", q.After, "around", q.Around); err != nil {
		return nil, err
	}
	setIntIf(query, "limit", q.Limit)
	var out MessageWindow
	if _, err := c.call(ctx, request{Method: "GET", Path: escPath("v1", "channels", channelID, "messages"), Query: query, Auth: true}, &out); err != nil {
		return nil, err
	}
	if out.Items == nil {
		out.Items = []Message{}
	}
	return &out, nil
}

// SendMessage: POST /v1/channels/{id}/messages. created = 201 (200 = duplicate
// nonce). An empty ClientNonce gets a fresh ULID; a rate_limited answer with
// RetryAfter <= 10s is retried once with the same nonce.
func (c *Client) SendMessage(ctx context.Context, channelID string, r SendMessageRequest) (msg *Message, created bool, err error) {
	if r.ClientNonce == "" {
		r.ClientNonce = ids.New()
	}
	var out messageEnvelope
	body := sendMessageBody{ClientNonce: r.ClientNonce, Kind: r.Kind, Body: r.Body, Poll: r.Poll, Card: r.Card}
	st, err := c.callRetryOnce(ctx, request{Method: "POST", Path: escPath("v1", "channels", channelID, "messages"), Body: body, Limiter: rlMessage, Auth: true}, &out)
	if err != nil {
		return nil, false, err
	}
	return &out.Message, st == http.StatusCreated, nil
}

// GetMessage: GET /v1/messages/{id} (thread replies too).
func (c *Client) GetMessage(ctx context.Context, messageID string) (*Message, error) {
	v, _, err := one[Message](ctx, c, request{Method: "GET", Path: escPath("v1", "messages", messageID), Auth: true}, "message")
	return v, err
}

// EditMessage: PATCH /v1/messages/{id} {"body"} (RL_MESSAGE, no auto-retry).
func (c *Client) EditMessage(ctx context.Context, messageID, body string) (*Message, error) {
	req := request{Method: "PATCH", Path: escPath("v1", "messages", messageID), Body: struct {
		Body string `json:"body"`
	}{body}, Limiter: rlMessage, Auth: true}
	v, _, err := one[Message](ctx, c, req, "message")
	return v, err
}

// DeleteMessage: DELETE /v1/messages/{id} (204).
func (c *Client) DeleteMessage(ctx context.Context, messageID string) error {
	_, err := c.call(ctx, request{Method: "DELETE", Path: escPath("v1", "messages", messageID), Auth: true}, nil)
	return err
}

// ---- 5.11 threads ----

// ThreadQuery selects a window of thread replies; at most one of Before/After.
type ThreadQuery struct {
	Before, After string
	Limit         int
}

// ThreadReplyRequest is POST /v1/messages/{id}/thread. Kind "" = text (no
// polls); Card goes with Kind MessageKindCard.
type ThreadReplyRequest struct {
	ClientNonce, Kind, Body string
	Card                    *Card
}

// threadReplyBody is the POST /v1/messages/{id}/thread wire body (api.md 5.11).
type threadReplyBody struct {
	ClientNonce string `json:"client_nonce"`
	Kind        string `json:"kind,omitempty"`
	Body        string `json:"body"`
	Card        *Card  `json:"card,omitempty"`
}

// GetThread: GET /v1/messages/{id}/thread?before=|after=&limit= (rootID may be
// a reply; the server resolves its root). Both anchors set is a client-side
// invalid_request with no request sent.
func (c *Client) GetThread(ctx context.Context, rootID string, q ThreadQuery) (*ThreadWindow, error) {
	query := url.Values{}
	if err := anchors(query, "before", q.Before, "after", q.After); err != nil {
		return nil, err
	}
	setIntIf(query, "limit", q.Limit)
	var out ThreadWindow
	if _, err := c.call(ctx, request{Method: "GET", Path: escPath("v1", "messages", rootID, "thread"), Query: query, Auth: true}, &out); err != nil {
		return nil, err
	}
	if out.Items == nil {
		out.Items = []Message{}
	}
	return &out, nil
}

// ReplyThread: POST /v1/messages/{id}/thread. created = 201 (200 = duplicate
// nonce). Same nonce and single auto-retry rules as SendMessage.
func (c *Client) ReplyThread(ctx context.Context, rootID string, r ThreadReplyRequest) (msg *Message, created bool, err error) {
	if r.ClientNonce == "" {
		r.ClientNonce = ids.New()
	}
	var out messageEnvelope
	body := threadReplyBody{ClientNonce: r.ClientNonce, Kind: r.Kind, Body: r.Body, Card: r.Card}
	st, err := c.callRetryOnce(ctx, request{Method: "POST", Path: escPath("v1", "messages", rootID, "thread"), Body: body, Limiter: rlMessage, Auth: true}, &out)
	if err != nil {
		return nil, false, err
	}
	return &out.Message, st == http.StatusCreated, nil
}

// ---- 5.12 reactions / pins ----

// AddReaction: PUT /v1/messages/{id}/reactions/{emoji} (emoji path-escaped).
func (c *Client) AddReaction(ctx context.Context, messageID, emoji string) error {
	_, err := c.call(ctx, request{Method: "PUT", Path: escPath("v1", "messages", messageID, "reactions", emoji), Limiter: rlReact, Auth: true}, nil)
	return err
}

// RemoveReaction: DELETE /v1/messages/{id}/reactions/{emoji}.
func (c *Client) RemoveReaction(ctx context.Context, messageID, emoji string) error {
	_, err := c.call(ctx, request{Method: "DELETE", Path: escPath("v1", "messages", messageID, "reactions", emoji), Limiter: rlReact, Auth: true}, nil)
	return err
}

// ListReactionUsers: GET /v1/messages/{id}/reactions/{emoji}?cursor=&limit=
func (c *Client) ListReactionUsers(ctx context.Context, messageID, emoji string, p PageOpts) (*Page[UserSummary], error) {
	return page[UserSummary](ctx, c, request{Method: "GET", Path: escPath("v1", "messages", messageID, "reactions", emoji), Query: p.values(nil), Auth: true})
}

// Pin: PUT /v1/messages/{id}/pin.
func (c *Client) Pin(ctx context.Context, messageID string) error {
	_, err := c.call(ctx, request{Method: "PUT", Path: escPath("v1", "messages", messageID, "pin"), Limiter: rlReact, Auth: true}, nil)
	return err
}

// Unpin: DELETE /v1/messages/{id}/pin.
func (c *Client) Unpin(ctx context.Context, messageID string) error {
	_, err := c.call(ctx, request{Method: "DELETE", Path: escPath("v1", "messages", messageID, "pin"), Limiter: rlReact, Auth: true}, nil)
	return err
}

// ListPins: GET /v1/channels/{id}/pins (newest pin first).
func (c *Client) ListPins(ctx context.Context, channelID string) ([]Message, error) {
	return list[Message](ctx, c, request{Method: "GET", Path: escPath("v1", "channels", channelID, "pins"), Auth: true})
}
