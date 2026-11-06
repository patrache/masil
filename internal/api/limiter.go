package api

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// Limiter names, exactly the binding names of api.md 0.7. Every endpoint
// method sets request.Limiter to its dedicated limiter (api.md 0.7 table and
// the 6-chapter endpoint summary); authenticated requests additionally fall
// under rlAPI. contract_limiters_test.go checks every harness row against the
// api.md 6 table, and checkLimiter proves each method uses its row.
const (
	rlSignup  = "RL_SIGNUP"  // POST /v1/auth/signup
	rlLogin   = "RL_LOGIN"   // POST /v1/auth/login, POST /v1/auth/password (same bucket)
	rlMessage = "RL_MESSAGE" // message send, thread reply, message edit
	rlPost    = "RL_POST"    // post/reply/quote, story, guestbook write
	rlReact   = "RL_REACT"   // reactions, like, repost, vote, pin
	rlUpload  = "RL_UPLOAD"  // POST /v1/uploads
	rlSocial  = "RL_SOCIAL"  // friend request, follow, block, DM create, channel member add, invites, space create, report
	rlSearch  = "RL_SEARCH"  // /v1/search/*, /v1/users/search, /v1/hashtags/search
	rlWS      = "RL_WS"      // GET /v1/ws (used by realtime, listed for completeness)
	rlAPI     = "RL_API"     // every authenticated request
)

// noteCooldown records a 429 cooldown for limiter until now+d (never shortens an existing one).
func (c *Client) noteCooldown(limiter string, d time.Duration) {
	if limiter == "" || d <= 0 {
		return
	}
	until := c.now().Add(d)
	c.mu.Lock()
	defer c.mu.Unlock()
	if cur, ok := c.cooldown[limiter]; !ok || until.After(cur) {
		c.cooldown[limiter] = until
	}
}

// Cooldown returns when the limiter's cooldown ends; zero if not rate-limited.
func (c *Client) Cooldown(limiter string) time.Time {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	until, ok := c.cooldown[limiter]
	if !ok {
		return time.Time{}
	}
	if !until.After(now) {
		delete(c.cooldown, limiter)
		return time.Time{}
	}
	return until
}

// cooldownError returns the local 429 for the first limiter still cooling down, or nil.
func (c *Client) cooldownError(limiters ...string) *Error {
	now := c.now()
	for _, l := range limiters {
		if l == "" {
			continue
		}
		until := c.Cooldown(l)
		if until.IsZero() {
			continue
		}
		left := until.Sub(now)
		details, _ := json.Marshal(struct {
			Limiter       string `json:"limiter"`
			RetryAfterSec int    `json:"retry_after_sec"`
		}{l, int(math.Ceil(left.Seconds()))})
		return &Error{
			Status:     429,
			Code:       "rate_limited",
			Message:    fmt.Sprintf("%s cooling down (client-side)", l),
			Details:    details,
			RetryAfter: left,
		}
	}
	return nil
}
