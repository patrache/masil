// Package api is the REST client for every endpoint in api.md (CA 4.3).
package api

import (
	"net/http"
	"time"
)

// Client talks to the masil REST API.
type Client struct { /* unexported */
}

// Option configures a Client.
type Option func(*Client)

// WithToken sets the bearer token.
func WithToken(token string) Option {
	// TODO(core): Task 8
	return nil
}

// WithHTTPClient overrides the HTTP client (default &http.Client{Timeout: 0}; per-call timeouts via ctx).
func WithHTTPClient(h *http.Client) Option {
	// TODO(core): Task 8
	return nil
}

// WithUserAgent overrides the User-Agent (default version.UserAgent()).
func WithUserAgent(ua string) Option {
	// TODO(core): Task 8
	return nil
}

// WithOnUnauthorized is called (once per token) on 401 unauthorized.
func WithOnUnauthorized(fn func()) Option {
	// TODO(core): Task 8
	return nil
}

// WithOnUpgradeRequired is called on 426 with details.min_client_version.
func WithOnUpgradeRequired(fn func(min string)) Option {
	// TODO(core): Task 8
	return nil
}

// WithClock injects the clock for rate-limit cooldown tests.
func WithClock(now func() time.Time) Option {
	// TODO(core): Task 8
	return nil
}

// New creates a client for baseURL.
func New(baseURL string, opts ...Option) *Client {
	// TODO(core): Task 8
	return nil
}

// BaseURL returns the server base URL.
func (c *Client) BaseURL() string {
	// TODO(core): Task 8
	return ""
}

// SetToken replaces the bearer token.
func (c *Client) SetToken(token string) {
	// TODO(core): Task 8
}

// Token returns the current bearer token.
func (c *Client) Token() string {
	// TODO(core): Task 8
	return ""
}

// MediaURL turns "/v1/media/<id>" into an absolute URL.
func (c *Client) MediaURL(rel string) string {
	// TODO(core): Task 8
	return ""
}

// WebSocketURL returns ws(s)://.../v1/ws?mode=..&device_id=..
func (c *Client) WebSocketURL(mode, deviceID string) string {
	// TODO(core): Task 8
	return ""
}

// AuthHeader returns Authorization + User-Agent, for the realtime dial.
func (c *Client) AuthHeader() http.Header {
	// TODO(core): Task 8
	return nil
}

// Cooldown returns when the limiter's cooldown ends; zero if not rate-limited.
func (c *Client) Cooldown(limiter string) time.Time {
	// TODO(core): Task 8
	return time.Time{}
}
