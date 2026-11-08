package api

import (
	"context"
)

// Meta: GET /v1/meta
func (c *Client) Meta(ctx context.Context) (*MetaInfo, error) {
	var out MetaInfo
	if _, err := c.call(ctx, request{Method: "GET", Path: "/v1/meta"}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Health: GET /v1/health
func (c *Client) Health(ctx context.Context) error {
	_, err := c.call(ctx, request{Method: "GET", Path: "/v1/health"}, nil)
	return err
}

type SignupRequest struct {
	Handle, Password                string
	DisplayName, Client, DeviceName string /* omitempty */
}
type LoginRequest struct {
	Handle, Password   string
	Client, DeviceName string /* omitempty */
}

// authBody is the signup/login wire body (api.md 1.4).
type authBody struct {
	Handle      string `json:"handle"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name,omitempty"`
	Client      string `json:"client,omitempty"`
	DeviceName  string `json:"device_name,omitempty"`
}

// Signup: POST /v1/auth/signup
func (c *Client) Signup(ctx context.Context, r SignupRequest) (*AuthResult, error) {
	var out AuthResult
	body := authBody{Handle: r.Handle, Password: r.Password, DisplayName: r.DisplayName, Client: r.Client, DeviceName: r.DeviceName}
	if _, err := c.call(ctx, request{Method: "POST", Path: "/v1/auth/signup", Body: body, Limiter: rlSignup}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Login: POST /v1/auth/login
func (c *Client) Login(ctx context.Context, r LoginRequest) (*AuthResult, error) {
	var out AuthResult
	body := authBody{Handle: r.Handle, Password: r.Password, Client: r.Client, DeviceName: r.DeviceName}
	if _, err := c.call(ctx, request{Method: "POST", Path: "/v1/auth/login", Body: body, Limiter: rlLogin}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Logout: POST /v1/auth/logout
func (c *Client) Logout(ctx context.Context) error {
	_, err := c.call(ctx, request{Method: "POST", Path: "/v1/auth/logout", Auth: true}, nil)
	return err
}

// ListSessions: GET /v1/auth/sessions
func (c *Client) ListSessions(ctx context.Context) ([]Session, error) {
	return list[Session](ctx, c, request{Method: "GET", Path: "/v1/auth/sessions", Auth: true})
}

// RevokeSession: DELETE /v1/auth/sessions/{id}
func (c *Client) RevokeSession(ctx context.Context, sessionID string) error {
	_, err := c.call(ctx, request{Method: "DELETE", Path: escPath("v1", "auth", "sessions", sessionID), Auth: true}, nil)
	return err
}

// ChangePassword: POST /v1/auth/password
func (c *Client) ChangePassword(ctx context.Context, current, next string) error {
	body := struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}{current, next}
	_, err := c.call(ctx, request{Method: "POST", Path: "/v1/auth/password", Body: body, Limiter: rlLogin, Auth: true}, nil)
	return err
}
