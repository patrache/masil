package api

import (
	"context"
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

func (c *Client) ListSpaces(ctx context.Context) ([]Space, error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) CreateSpace(ctx context.Context, r CreateSpaceRequest) (*SpaceDetail, error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) GetSpace(ctx context.Context, spaceID string) (*SpaceDetail, error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) UpdateSpace(ctx context.Context, spaceID string, r UpdateSpaceRequest) (*Space, error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) DeleteSpace(ctx context.Context, spaceID string) error {
	// TODO(core): Task 10
	return nil
}

func (c *Client) TransferSpace(ctx context.Context, spaceID string, to UserRef) (*Space, error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) ListSpaceMembers(ctx context.Context, spaceID string, q SpaceMembersQuery) (*Page[SpaceMember], error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) SetSpaceMemberRole(ctx context.Context, spaceID string, u UserRef, role SpaceRole) (*SpaceMember, error) {
	// TODO(core): Task 10
	return nil, nil
}

// RemoveSpaceMember: u=Me => leave
func (c *Client) RemoveSpaceMember(ctx context.Context, spaceID string, u UserRef, o RemoveMemberOpts) error {
	// TODO(core): Task 10
	return nil
}

func (c *Client) ListSpaceBans(ctx context.Context, spaceID string) ([]SpaceBan, error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) Unban(ctx context.Context, spaceID string, u UserRef) error {
	// TODO(core): Task 10
	return nil
}

type CreateInviteRequest struct {
	MaxUses      Opt[int]
	ExpiresInSec Opt[int]
} // omit ExpiresInSec => server default 7d
func (c *Client) ListInvites(ctx context.Context, spaceID string) ([]Invite, error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) CreateInvite(ctx context.Context, spaceID string, r CreateInviteRequest) (*Invite, error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) RevokeInvite(ctx context.Context, code string) error {
	// TODO(core): Task 10
	return nil
}

func (c *Client) GetInvite(ctx context.Context, code string) (*InvitePreview, error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) AcceptInvite(ctx context.Context, code string) (*SpaceDetail, error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) SendDirectInvite(ctx context.Context, spaceID string, u UserRef) (*DirectInvite, error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) ListMyDirectInvites(ctx context.Context) ([]DirectInvite, error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) AcceptDirectInvite(ctx context.Context, inviteID string) (*SpaceDetail, error) {
	// TODO(core): Task 10
	return nil, nil
}

func (c *Client) DeclineDirectInvite(ctx context.Context, inviteID string) error {
	// TODO(core): Task 10
	return nil
}

func (c *Client) CancelDirectInvite(ctx context.Context, inviteID string) error {
	// TODO(core): Task 10
	return nil
}
