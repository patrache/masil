package chatstore

import (
	"time"

	"github.com/patrache/masil/internal/api"
)

// PendingMessage is an optimistic send keyed by client_nonce (CA 6.1).
type PendingMessage struct {
	Nonce, Body, Kind string
	ThreadRootID      *string
	Failed            error
	SentAt            time.Time
}

// Pending returns the channel's optimistic sends in add order (a copy).
func (c *ChatStore) Pending(channelID string) []PendingMessage {
	p := c.pending[channelID]
	if len(p) == 0 {
		return nil
	}
	out := make([]PendingMessage, len(p))
	copy(out, p)
	return out
}

func (c *ChatStore) pendingIndex(channelID, nonce string) int {
	for i, p := range c.pending[channelID] {
		if p.Nonce == nonce {
			return i
		}
	}
	return -1
}

// AddPending appends an optimistic send; an item with the same nonce is replaced in place.
func (c *ChatStore) AddPending(channelID string, p PendingMessage) {
	if i := c.pendingIndex(channelID, p.Nonce); i >= 0 {
		c.pending[channelID][i] = p
		return
	}
	c.pending[channelID] = append(c.pending[channelID], p)
}

// FailPending marks a pending send failed (it stays listed for retry).
func (c *ChatStore) FailPending(channelID, nonce string, err error) {
	if i := c.pendingIndex(channelID, nonce); i >= 0 {
		c.pending[channelID][i].Failed = err
	}
}

// RetryPending clears the failure and restamps SentAt (the resend reuses the same nonce).
func (c *ChatStore) RetryPending(channelID, nonce string, at time.Time) {
	if i := c.pendingIndex(channelID, nonce); i >= 0 {
		c.pending[channelID][i].Failed = nil
		c.pending[channelID][i].SentAt = at
	}
}

// DropPending removes a pending send and its poll input.
func (c *ChatStore) DropPending(channelID, nonce string) {
	if i := c.pendingIndex(channelID, nonce); i >= 0 {
		p := c.pending[channelID]
		p = append(p[:i:i], p[i+1:]...)
		if len(p) == 0 {
			delete(c.pending, channelID)
		} else {
			c.pending[channelID] = p
		}
	}
	if m := c.polls[channelID]; m != nil {
		delete(m, nonce)
		if len(m) == 0 {
			delete(c.polls, channelID)
		}
	}
}

// SetPendingPoll keeps the poll input of a pending /poll send for resend.
func (c *ChatStore) SetPendingPoll(channelID, nonce string, poll *api.PollInput) {
	if poll == nil {
		if m := c.polls[channelID]; m != nil {
			delete(m, nonce)
		}
		return
	}
	m := c.polls[channelID]
	if m == nil {
		m = map[string]*api.PollInput{}
		c.polls[channelID] = m
	}
	m[nonce] = poll
}

// PendingPoll returns the poll input kept for a pending send, or nil.
func (c *ChatStore) PendingPoll(channelID, nonce string) *api.PollInput {
	return c.polls[channelID][nonce]
}
