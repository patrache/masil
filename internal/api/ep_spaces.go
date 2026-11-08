package api

import (
	"context"
	"net/url"
)

type CreateSpaceRequest struct {
	Name                   string
	Description, IconEmoji string /* omitempty */
}
type UpdateSpaceRequest struct {
	Name, Description Opt[string]
	IconEmoji         Opt[string]
}
type SpaceMembersQuery struct {
	Q, Role string
	PageOpts
}
type RemoveMemberOpts struct {
	Ban    bool
	Reason string
}

// createSpaceBody is the POST /v1/spaces wire body.
type createSpaceBody struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	IconEmoji   string `json:"icon_emoji,omitempty"`
}

// updateSpaceBody is the PATCH /v1/spaces/{space_id} wire body.
type updateSpaceBody struct {
	Name        Opt[string] `json:"name,omitzero"`
	Description Opt[string] `json:"description,omitzero"`
	IconEmoji   Opt[string] `json:"icon_emoji,omitzero"`
}

// ---- 5.7 spaces ----

// ListSpaces: GET /v1/spaces
func (c *Client) ListSpaces(ctx context.Context) ([]Space, error) {
	return list[Space](ctx, c, request{Method: "GET", Path: "/v1/spaces", Auth: true})
}

// CreateSpace: POST /v1/spaces
func (c *Client) CreateSpace(ctx context.Context, r CreateSpaceRequest) (*SpaceDetail, error) {
	body := createSpaceBody{Name: r.Name, Description: r.Description, IconEmoji: r.IconEmoji}
	v, _, err := one[SpaceDetail](ctx, c, request{Method: "POST", Path: "/v1/spaces", Body: body, Limiter: rlSocial, Auth: true}, "space")
	return v, err
}

// GetSpace: GET /v1/spaces/{space_id}
func (c *Client) GetSpace(ctx context.Context, spaceID string) (*SpaceDetail, error) {
	v, _, err := one[SpaceDetail](ctx, c, request{Method: "GET", Path: escPath("v1", "spaces", spaceID), Auth: true}, "space")
	return v, err
}

// UpdateSpace: PATCH /v1/spaces/{space_id}
func (c *Client) UpdateSpace(ctx context.Context, spaceID string, r UpdateSpaceRequest) (*Space, error) {
	body := updateSpaceBody{Name: r.Name, Description: r.Description, IconEmoji: r.IconEmoji}
	v, _, err := one[Space](ctx, c, request{Method: "PATCH", Path: escPath("v1", "spaces", spaceID), Body: body, Auth: true}, "space")
	return v, err
}

// DeleteSpace: DELETE /v1/spaces/{space_id}
func (c *Client) DeleteSpace(ctx context.Context, spaceID string) error {
	_, err := c.call(ctx, request{Method: "DELETE", Path: escPath("v1", "spaces", spaceID), Auth: true}, nil)
	return err
}

// TransferSpace: POST /v1/spaces/{space_id}/transfer
func (c *Client) TransferSpace(ctx context.Context, spaceID string, to UserRef) (*Space, error) {
	v, _, err := one[Space](ctx, c, request{Method: "POST", Path: escPath("v1", "spaces", spaceID, "transfer"), Body: userBody{to}, Auth: true}, "space")
	return v, err
}

// ListSpaceMembers: GET /v1/spaces/{space_id}/members?q=&role=&cursor=&limit=
func (c *Client) ListSpaceMembers(ctx context.Context, spaceID string, q SpaceMembersQuery) (*Page[SpaceMember], error) {
	query := url.Values{}
	setIf(query, "q", q.Q)
	setIf(query, "role", q.Role)
	return page[SpaceMember](ctx, c, request{Method: "GET", Path: escPath("v1", "spaces", spaceID, "members"), Query: q.PageOpts.values(query), Auth: true})
}

// SetSpaceMemberRole: PATCH /v1/spaces/{space_id}/members/{user}
func (c *Client) SetSpaceMemberRole(ctx context.Context, spaceID string, u UserRef, role SpaceRole) (*SpaceMember, error) {
	body := struct {
		Role SpaceRole `json:"role"`
	}{role}
	v, _, err := one[SpaceMember](ctx, c, request{Method: "PATCH", Path: escPath("v1", "spaces", spaceID, "members", string(u)), Body: body, Auth: true}, "member")
	return v, err
}

// RemoveSpaceMember: DELETE /v1/spaces/{space_id}/members/{user}?ban=&reason=;
// u=MeRef => leave. ban is sent only when true, reason only when non-empty.
func (c *Client) RemoveSpaceMember(ctx context.Context, spaceID string, u UserRef, o RemoveMemberOpts) error {
	q := url.Values{}
	if o.Ban {
		q.Set("ban", "true")
	}
	setIf(q, "reason", o.Reason)
	_, err := c.call(ctx, request{Method: "DELETE", Path: escPath("v1", "spaces", spaceID, "members", string(u)), Query: q, Auth: true}, nil)
	return err
}

// ListSpaceBans: GET /v1/spaces/{space_id}/bans
func (c *Client) ListSpaceBans(ctx context.Context, spaceID string) ([]SpaceBan, error) {
	return list[SpaceBan](ctx, c, request{Method: "GET", Path: escPath("v1", "spaces", spaceID, "bans"), Auth: true})
}

// Unban: DELETE /v1/spaces/{space_id}/bans/{user}
func (c *Client) Unban(ctx context.Context, spaceID string, u UserRef) error {
	_, err := c.call(ctx, request{Method: "DELETE", Path: escPath("v1", "spaces", spaceID, "bans", string(u)), Auth: true}, nil)
	return err
}

// ---- 5.8 invites ----

type CreateInviteRequest struct {
	MaxUses      Opt[int]
	ExpiresInSec Opt[int]
} // omit ExpiresInSec => server default 7d

// createInviteBody is the POST /v1/spaces/{space_id}/invites wire body.
type createInviteBody struct {
	MaxUses      Opt[int] `json:"max_uses,omitzero"`
	ExpiresInSec Opt[int] `json:"expires_in_sec,omitzero"`
}

// ListInvites: GET /v1/spaces/{space_id}/invites
func (c *Client) ListInvites(ctx context.Context, spaceID string) ([]Invite, error) {
	return list[Invite](ctx, c, request{Method: "GET", Path: escPath("v1", "spaces", spaceID, "invites"), Auth: true})
}

// CreateInvite: POST /v1/spaces/{space_id}/invites
func (c *Client) CreateInvite(ctx context.Context, spaceID string, r CreateInviteRequest) (*Invite, error) {
	body := createInviteBody{MaxUses: r.MaxUses, ExpiresInSec: r.ExpiresInSec}
	v, _, err := one[Invite](ctx, c, request{Method: "POST", Path: escPath("v1", "spaces", spaceID, "invites"), Body: body, Limiter: rlSocial, Auth: true}, "invite")
	return v, err
}

// RevokeInvite: DELETE /v1/invites/{code}
func (c *Client) RevokeInvite(ctx context.Context, code string) error {
	_, err := c.call(ctx, request{Method: "DELETE", Path: escPath("v1", "invites", code), Auth: true}, nil)
	return err
}

// GetInvite: GET /v1/invites/{code}
func (c *Client) GetInvite(ctx context.Context, code string) (*InvitePreview, error) {
	v, _, err := one[InvitePreview](ctx, c, request{Method: "GET", Path: escPath("v1", "invites", code), Auth: true}, "invite")
	return v, err
}

// AcceptInvite: POST /v1/invites/{code}/accept
func (c *Client) AcceptInvite(ctx context.Context, code string) (*SpaceDetail, error) {
	v, _, err := one[SpaceDetail](ctx, c, request{Method: "POST", Path: escPath("v1", "invites", code, "accept"), Auth: true}, "space")
	return v, err
}

// SendDirectInvite: POST /v1/spaces/{space_id}/direct-invites
func (c *Client) SendDirectInvite(ctx context.Context, spaceID string, u UserRef) (*DirectInvite, error) {
	v, _, err := one[DirectInvite](ctx, c, request{Method: "POST", Path: escPath("v1", "spaces", spaceID, "direct-invites"), Body: userBody{u}, Limiter: rlSocial, Auth: true}, "invite")
	return v, err
}

// ListMyDirectInvites: GET /v1/me/direct-invites
func (c *Client) ListMyDirectInvites(ctx context.Context) ([]DirectInvite, error) {
	return list[DirectInvite](ctx, c, request{Method: "GET", Path: "/v1/me/direct-invites", Auth: true})
}

// AcceptDirectInvite: POST /v1/direct-invites/{invite_id}/accept
func (c *Client) AcceptDirectInvite(ctx context.Context, inviteID string) (*SpaceDetail, error) {
	v, _, err := one[SpaceDetail](ctx, c, request{Method: "POST", Path: escPath("v1", "direct-invites", inviteID, "accept"), Auth: true}, "space")
	return v, err
}

// DeclineDirectInvite: POST /v1/direct-invites/{invite_id}/decline
func (c *Client) DeclineDirectInvite(ctx context.Context, inviteID string) error {
	_, err := c.call(ctx, request{Method: "POST", Path: escPath("v1", "direct-invites", inviteID, "decline"), Auth: true}, nil)
	return err
}

// CancelDirectInvite: DELETE /v1/direct-invites/{invite_id}
func (c *Client) CancelDirectInvite(ctx context.Context, inviteID string) error {
	_, err := c.call(ctx, request{Method: "DELETE", Path: escPath("v1", "direct-invites", inviteID), Auth: true}, nil)
	return err
}
