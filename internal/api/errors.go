package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/patrache/masil/internal/textwidth"
)

// Error is the decoded error envelope (api.md 0.5).
type Error struct {
	Status     int
	Code       string // "invalid_request", "rate_limited", ...
	Message    string
	Details    json.RawMessage // raw "details" object
	RequestID  string          // X-Request-Id
	RetryAfter time.Duration   // 429 only
}

// Error renders the envelope for logs and plain stderr lines. Code, Message
// and RequestID come from the server (body and X-Request-Id header), so each
// is sanitized and length-capped: a caller that prints err.Error() can never
// write an escape sequence to the terminal (pending-fixes 0-b).
func (e *Error) Error() string {
	s := fmt.Sprintf("api: %d %s", e.Status, serverText(e.Code))
	if e.Message != "" {
		s += ": " + strings.TrimRight(textwidth.Truncate(textwidth.Sanitize(e.Message), maxServerMessage, "…"), " ")
	}
	if e.RequestID != "" {
		s += " (request " + serverText(e.RequestID) + ")"
	}
	return s
}

// maxServerMessage caps, in cells, the server's error message in Error().
const maxServerMessage = 200

// details decodes e.Details into v; false if absent or not an object of that shape.
func (e *Error) details(v any) bool {
	if len(e.Details) == 0 {
		return false
	}
	return json.Unmarshal(e.Details, v) == nil
}

// Fields returns details.fields for invalid_request.
func (e *Error) Fields() map[string]string {
	var d struct {
		Fields map[string]string `json:"fields"`
	}
	if !e.details(&d) || len(d.Fields) == 0 {
		return nil
	}
	return d.Fields
}

// UserIDs returns details.user_ids for dm_not_allowed: the users who fail
// can_dm(me, X) in a group DM create/add. Empty for a 1:1 refusal and for a block
// between two would-be members, which names nobody (api.md 3.1).
func (e *Error) UserIDs() []string {
	var d struct {
		UserIDs []string `json:"user_ids"`
	}
	e.details(&d)
	return d.UserIDs
}

// Limiter returns details.limiter for rate_limited.
func (e *Error) Limiter() string {
	var d struct {
		Limiter string `json:"limiter"`
	}
	e.details(&d)
	return d.Limiter
}

// timeoutError (errRequestTimeout) marks the client's own default timeout (15s/120s) firing
// while the caller's ctx is still alive: a slow or unreachable server, so it
// classifies as "network", not "canceled".
type timeoutError struct{}

func (timeoutError) Error() string   { return "request timed out" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

var errRequestTimeout net.Error = timeoutError{}

// CodeOf returns "" if err is not *Error; "network" for transport errors; "canceled" for ctx cancel.
func CodeOf(err error) string {
	if err == nil {
		return ""
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "canceled"
	}
	var ue *url.Error
	var ne net.Error
	if errors.As(err, &ue) || errors.As(err, &ne) {
		return "network"
	}
	return ""
}

// IsCode reports CodeOf(err) == code.
func IsCode(err error, code string) bool {
	return CodeOf(err) == code
}
