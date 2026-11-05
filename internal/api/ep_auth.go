package api

import (
	"context"
)

// Meta: GET /v1/meta
func (c *Client) Meta(ctx context.Context) (*MetaInfo, error) {
	// TODO(core): Task 10
	return nil, nil
}

// Health: GET /v1/health
func (c *Client) Health(ctx context.Context) error {
	// TODO(core): Task 10
	return nil
}

type SignupRequest struct {
	Handle, Password                string
	DisplayName, Client, DeviceName string /* omitempty */
}
type LoginRequest struct {
	Handle, Password   string
	Client, DeviceName string /* omitempty */
}

// Signup: POST /v1/auth/signup
func (c *Client) Signup(ctx context.Context, r SignupRequest) (*AuthResult, error) {
	// TODO(core): Task 10
	return nil, nil
}

// Login: POST /v1/auth/login
func (c *Client) Login(ctx context.Context, r LoginRequest) (*AuthResult, error) {
	// TODO(core): Task 10
	return nil, nil
}

// Logout: POST /v1/auth/logout
func (c *Client) Logout(ctx context.Context) error {
	// TODO(core): Task 10
	return nil
}

// ListSessions: GET /v1/auth/sessions
func (c *Client) ListSessions(ctx context.Context) ([]Session, error) {
	// TODO(core): Task 10
	return nil, nil
}

// RevokeSession: DELETE /v1/auth/sessions/{id}
func (c *Client) RevokeSession(ctx context.Context, sessionID string) error {
	// TODO(core): Task 10
	return nil
}

// ChangePassword: POST /v1/auth/password
func (c *Client) ChangePassword(ctx context.Context, current, next string) error {
	// TODO(core): Task 10
	return nil
}
