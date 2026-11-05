package api

import (
	"context"
	"net/http"

	"github.com/patrache/masil/internal/ids"
)

type MessageQuery struct {
	Before, After, Around string
	Limit                 int
} // at most one anchor
type SendMessageRequest struct {
	ClientNonce string
	Kind        string
	Body        string
	Poll        *PollInput
}

// sendMessageBody is the POST /v1/channels/{id}/messages wire body (api.md 5.10).
type sendMessageBody struct {
	ClientNonce string     `json:"client_nonce"`
	Kind        string     `json:"kind,omitempty"`
	Body        string     `json:"body"`
	Poll        *PollInput `json:"poll,omitempty"`
}

// messageEnvelope is {"message": Message}.
type messageEnvelope struct {
	Message Message `json:"message"`
}

func (c *Client) ListMessages(ctx context.Context, channelID string, q MessageQuery) (*MessageWindow, error) {
	// TODO(core): Task 11
	return nil, nil
}

// SendMessage: POST /v1/channels/{id}/messages. created = 201 (200 = duplicate
// nonce). An empty ClientNonce gets a fresh ULID; a rate_limited answer with
// RetryAfter <= 10s is retried once with the same nonce.
func (c *Client) SendMessage(ctx context.Context, channelID string, r SendMessageRequest) (msg *Message, created bool, err error) {
	if r.ClientNonce == "" {
		r.ClientNonce = ids.New()
	}
	var out messageEnvelope
	body := sendMessageBody{ClientNonce: r.ClientNonce, Kind: r.Kind, Body: r.Body, Poll: r.Poll}
	st, err := c.callRetryOnce(ctx, request{Method: "POST", Path: escPath("v1", "channels", channelID, "messages"), Body: body, Limiter: rlMessage, Auth: true}, &out)
	if err != nil {
		return nil, false, err
	}
	return &out.Message, st == http.StatusCreated, nil
}

func (c *Client) GetMessage(ctx context.Context, messageID string) (*Message, error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) EditMessage(ctx context.Context, messageID, body string) (*Message, error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) DeleteMessage(ctx context.Context, messageID string) error {
	// TODO(core): Task 11
	return nil
}

type ThreadQuery struct {
	Before, After string
	Limit         int
}
type ThreadReplyRequest struct{ ClientNonce, Kind, Body string }

func (c *Client) GetThread(ctx context.Context, rootID string, q ThreadQuery) (*ThreadWindow, error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) ReplyThread(ctx context.Context, rootID string, r ThreadReplyRequest) (msg *Message, created bool, err error) {
	// TODO(core): Task 11
	return
}

// AddReaction: PUT /v1/messages/{id}/reactions/{emoji} (emoji path-escaped).
func (c *Client) AddReaction(ctx context.Context, messageID, emoji string) error {
	_, err := c.call(ctx, request{Method: "PUT", Path: escPath("v1", "messages", messageID, "reactions", emoji), Limiter: rlReact, Auth: true}, nil)
	return err
}

func (c *Client) RemoveReaction(ctx context.Context, messageID, emoji string) error {
	// TODO(core): Task 11
	return nil
}

func (c *Client) ListReactionUsers(ctx context.Context, messageID, emoji string, p PageOpts) (*Page[UserSummary], error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) Pin(ctx context.Context, messageID string) error {
	// TODO(core): Task 11
	return nil
}

func (c *Client) Unpin(ctx context.Context, messageID string) error {
	// TODO(core): Task 11
	return nil
}

func (c *Client) ListPins(ctx context.Context, channelID string) ([]Message, error) {
	// TODO(core): Task 11
	return nil, nil
}
