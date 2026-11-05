package api

import (
	"context"
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

func (c *Client) ListMessages(ctx context.Context, channelID string, q MessageQuery) (*MessageWindow, error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) SendMessage(ctx context.Context, channelID string, r SendMessageRequest) (msg *Message, created bool, err error) {
	// TODO(core): Task 11
	return
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

func (c *Client) AddReaction(ctx context.Context, messageID, emoji string) error {
	// TODO(core): Task 11
	return nil
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
