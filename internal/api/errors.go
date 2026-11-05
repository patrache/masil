package api

import (
	"encoding/json"
	"time"
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

func (e *Error) Error() string {
	// TODO(core): Task 8
	return ""
}

// Fields returns details.fields for invalid_request.
func (e *Error) Fields() map[string]string {
	// TODO(core): Task 8
	return nil
}

// Limiter returns details.limiter for rate_limited.
func (e *Error) Limiter() string {
	// TODO(core): Task 8
	return ""
}

// CodeOf returns "" if err is not *Error; "network" for transport errors; "canceled" for ctx cancel.
func CodeOf(err error) string {
	// TODO(core): Task 8
	return ""
}

// IsCode reports CodeOf(err) == code.
func IsCode(err error, code string) bool {
	// TODO(core): Task 8
	return false
}
