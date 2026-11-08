package api

import (
	"context"
	"net/url"
)

type UpdateProfileRequest struct {
	DisplayName    Opt[string]
	Bio            Opt[string]
	AvatarUploadID Opt[string]
} // json display_name,bio,avatar_upload_id omitzero
// updateProfileBody is the PATCH /v1/me/profile wire body.
type updateProfileBody struct {
	DisplayName    Opt[string] `json:"display_name,omitzero"`
	Bio            Opt[string] `json:"bio,omitzero"`
	AvatarUploadID Opt[string] `json:"avatar_upload_id,omitzero"`
}

// userEnvelope is {"user": Me}.
type userEnvelope struct {
	User Me `json:"user"`
}

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
	var out userEnvelope
	if _, err := c.call(ctx, request{Method: "GET", Path: "/v1/me", Auth: true}, &out); err != nil {
		return nil, err
	}
	return &out.User, nil
}

// UpdateProfile: PATCH /v1/me/profile
func (c *Client) UpdateProfile(ctx context.Context, r UpdateProfileRequest) (*Me, error) {
	var out userEnvelope
	body := updateProfileBody{DisplayName: r.DisplayName, Bio: r.Bio, AvatarUploadID: r.AvatarUploadID}
	if _, err := c.call(ctx, request{Method: "PATCH", Path: "/v1/me/profile", Body: body, Auth: true}, &out); err != nil {
		return nil, err
	}
	return &out.User, nil
}

// GetSettings: GET /v1/me/settings
func (c *Client) GetSettings(ctx context.Context) (*Settings, error) {
	v, _, err := one[Settings](ctx, c, request{Method: "GET", Path: "/v1/me/settings", Auth: true}, "settings")
	return v, err
}

// notifyPatchBody is Partial<Settings["notify"]>.
type notifyPatchBody struct {
	DM            Opt[bool] `json:"dm,omitzero"`
	Mention       Opt[bool] `json:"mention,omitzero"`
	Invite        Opt[bool] `json:"invite,omitzero"`
	FriendRequest Opt[bool] `json:"friend_request,omitzero"`
	ThreadReply   Opt[bool] `json:"thread_reply,omitzero"`
	Social        Opt[bool] `json:"social,omitzero"`
}

// updateSettingsBody is the PATCH /v1/me/settings wire body.
type updateSettingsBody struct {
	DMPolicy        Opt[string]      `json:"dm_policy,omitzero"`
	IsPrivate       Opt[bool]        `json:"is_private,omitzero"`
	Notify          *notifyPatchBody `json:"notify,omitempty"`
	FocusDefaultMin Opt[int]         `json:"focus_default_min,omitzero"`
}

// UpdateSettings: PATCH /v1/me/settings
func (c *Client) UpdateSettings(ctx context.Context, r UpdateSettingsRequest) (*Settings, error) {
	body := updateSettingsBody{DMPolicy: r.DMPolicy, IsPrivate: r.IsPrivate, FocusDefaultMin: r.FocusDefaultMin}
	if n := r.Notify; n != nil {
		body.Notify = &notifyPatchBody{DM: n.DM, Mention: n.Mention, Invite: n.Invite,
			FriendRequest: n.FriendRequest, ThreadReply: n.ThreadReply, Social: n.Social}
	}
	v, _, err := one[Settings](ctx, c, request{Method: "PATCH", Path: "/v1/me/settings", Body: body, Auth: true}, "settings")
	return v, err
}

// setStatusBody is the PUT /v1/me/status wire body (text always sent).
type setStatusBody struct {
	Emoji        Opt[string] `json:"emoji,omitzero"`
	Text         string      `json:"text"`
	ExpiresInSec Opt[int]    `json:"expires_in_sec,omitzero"`
}

// SetStatus: PUT /v1/me/status
func (c *Client) SetStatus(ctx context.Context, r SetStatusRequest) (*UserStatus, error) {
	body := setStatusBody{Emoji: r.Emoji, Text: r.Text, ExpiresInSec: r.ExpiresInSec}
	v, _, err := one[UserStatus](ctx, c, request{Method: "PUT", Path: "/v1/me/status", Body: body, Auth: true}, "status")
	return v, err
}

// ClearStatus: DELETE /v1/me/status
func (c *Client) ClearStatus(ctx context.Context) error {
	_, err := c.call(ctx, request{Method: "DELETE", Path: "/v1/me/status", Auth: true}, nil)
	return err
}

// SetPresenceMode: PUT /v1/me/presence
func (c *Client) SetPresenceMode(ctx context.Context, mode string) (*PresenceState, error) {
	body := struct {
		Mode string `json:"mode"`
	}{mode}
	v, _, err := one[PresenceState](ctx, c, request{Method: "PUT", Path: "/v1/me/presence", Body: body, Auth: true}, "presence")
	return v, err
}

// GetPresence: GET /v1/presence (<=100, caller chunks)
//
// More than 100 IDs is a client-side 400 invalid_request with no request
// sent; an empty list returns an empty result without a request.
func (c *Client) GetPresence(ctx context.Context, userIDs []string) ([]PresenceItem, error) {
	v, err := idList("user_ids", userIDs)
	if err != nil {
		return nil, err
	}
	if len(userIDs) == 0 {
		return []PresenceItem{}, nil
	}
	q := url.Values{"user_ids": {v}}
	return list[PresenceItem](ctx, c, request{Method: "GET", Path: "/v1/presence", Query: q, Auth: true})
}

// StartFocus: POST /v1/me/focus
func (c *Client) StartFocus(ctx context.Context, durationSec Opt[int]) (*Focus, error) {
	body := struct {
		DurationSec Opt[int] `json:"duration_sec,omitzero"`
	}{durationSec}
	v, _, err := one[Focus](ctx, c, request{Method: "POST", Path: "/v1/me/focus", Body: body, Auth: true}, "focus")
	return v, err
}

// GetFocus: GET /v1/me/focus (nil = not focusing)
func (c *Client) GetFocus(ctx context.Context) (*Focus, error) {
	v, _, err := one[Focus](ctx, c, request{Method: "GET", Path: "/v1/me/focus", Auth: true}, "focus")
	return v, err
}

// EndFocus: DELETE /v1/me/focus
func (c *Client) EndFocus(ctx context.Context) (*FocusSummary, error) {
	v, _, err := one[FocusSummary](ctx, c, request{Method: "DELETE", Path: "/v1/me/focus", Auth: true}, "summary")
	return v, err
}

// ListUnreads: GET /v1/me/unreads
func (c *Client) ListUnreads(ctx context.Context) ([]UnreadState, error) {
	return list[UnreadState](ctx, c, request{Method: "GET", Path: "/v1/me/unreads", Auth: true})
}
