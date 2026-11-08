package chatstore

import (
	"time"

	"github.com/patrache/masil/internal/realtime"
)

// typingKey scopes typing to a channel and an optional thread ("" = channel itself).
type typingKey struct {
	channelID string
	rootID    string
}

// typers keeps users in first-received order with their local expiry.
type typers struct {
	order   []string
	expires map[string]time.Time
}

func keyOf(channelID string, threadRootID *string) typingKey {
	k := typingKey{channelID: channelID}
	if threadRootID != nil {
		k.rootID = *threadRootID
	}
	return k
}

// applyTyping records a typing frame. Expiry is local receipt time + expires_in_ms;
// the server ev.TS is never used (clock skew, zero TS).
func (c *ChatStore) applyTyping(d realtime.Typing) {
	if d.ChannelID == "" || d.UserID == "" {
		return
	}
	now := c.now()
	k := keyOf(d.ChannelID, d.ThreadRootID)
	t := c.typing[k]
	if t == nil {
		t = &typers{expires: map[string]time.Time{}}
		c.typing[k] = t
	}
	t.prune(now)
	if _, ok := t.expires[d.UserID]; !ok {
		t.order = append(t.order, d.UserID)
	}
	t.expires[d.UserID] = now.Add(time.Duration(d.ExpiresInMS) * time.Millisecond)
}

// prune drops users whose expiry is at or before now.
func (t *typers) prune(now time.Time) {
	kept := t.order[:0]
	for _, u := range t.order {
		if now.Before(t.expires[u]) {
			kept = append(kept, u)
		} else {
			delete(t.expires, u)
		}
	}
	t.order = kept
}

// Typing returns user ids typing in the scope at now, in first-received order,
// excluding me and blocked users.
func (c *ChatStore) Typing(channelID string, threadRootID *string, now time.Time) []string {
	t := c.typing[keyOf(channelID, threadRootID)]
	if t == nil {
		return nil
	}
	var out []string
	for _, u := range t.order {
		if !now.Before(t.expires[u]) || c.s.IsMe(u) || c.s.IsBlocked(u) {
			continue
		}
		out = append(out, u)
	}
	return out
}
