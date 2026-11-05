package api

import (
	"context"
)

type UpdateProfileRequest struct {
	DisplayName    Opt[string]
	Bio            Opt[string]
	AvatarUploadID Opt[string]
} // json display_name,bio,avatar_upload_id omitzero
type UpdateSettingsRequest struct {
	DMPolicy        Opt[string]
	IsPrivate       Opt[bool]
	Notify          *NotifySettingsPatch
	FocusDefaultMin Opt[int]
}
type NotifySettingsPatch struct{ DM, Mention, Invite, FriendRequest, ThreadReply, Social Opt[bool] }
type SetStatusRequest struct {
	Emoji        Opt[string]
	Text         string
	ExpiresInSec Opt[int]
}

// GetMe: GET /v1/me
func (c *Client) GetMe(ctx context.Context) (*Me, error) {
	// TODO(core): Task 10
	return nil, nil
}

// UpdateProfile: PATCH /v1/me/profile
func (c *Client) UpdateProfile(ctx context.Context, r UpdateProfileRequest) (*Me, error) {
	// TODO(core): Task 10
	return nil, nil
}

// GetSettings: GET /v1/me/settings
func (c *Client) GetSettings(ctx context.Context) (*Settings, error) {
	// TODO(core): Task 10
	return nil, nil
}

// UpdateSettings: PATCH /v1/me/settings
func (c *Client) UpdateSettings(ctx context.Context, r UpdateSettingsRequest) (*Settings, error) {
	// TODO(core): Task 10
	return nil, nil
}

// SetStatus: PUT /v1/me/status
func (c *Client) SetStatus(ctx context.Context, r SetStatusRequest) (*UserStatus, error) {
	// TODO(core): Task 10
	return nil, nil
}

// ClearStatus: DELETE /v1/me/status
func (c *Client) ClearStatus(ctx context.Context) error {
	// TODO(core): Task 10
	return nil
}

// SetPresenceMode: PUT /v1/me/presence
func (c *Client) SetPresenceMode(ctx context.Context, mode string) (*PresenceState, error) {
	// TODO(core): Task 10
	return nil, nil
}

// GetPresence: GET /v1/presence (<=100, caller chunks)
func (c *Client) GetPresence(ctx context.Context, userIDs []string) ([]PresenceItem, error) {
	// TODO(core): Task 10
	return nil, nil
}

// StartFocus: POST /v1/me/focus
func (c *Client) StartFocus(ctx context.Context, durationSec Opt[int]) (*Focus, error) {
	// TODO(core): Task 10
	return nil, nil
}

// GetFocus: GET /v1/me/focus (nil = not focusing)
func (c *Client) GetFocus(ctx context.Context) (*Focus, error) {
	// TODO(core): Task 10
	return nil, nil
}

// EndFocus: DELETE /v1/me/focus
func (c *Client) EndFocus(ctx context.Context) (*FocusSummary, error) {
	// TODO(core): Task 10
	return nil, nil
}

// ListUnreads: GET /v1/me/unreads
func (c *Client) ListUnreads(ctx context.Context) ([]UnreadState, error) {
	// TODO(core): Task 10
	return nil, nil
}
