package api

import (
	"context"
	"net/url"
	"strings"
)

// ---- 5.26 workspace shares (v0.1.3 B10) ----

// workspaceShareBody is the POST /v1/workspace-shares wire body.
type workspaceShareBody struct {
	Title string    `json:"title"`
	Users []UserRef `json:"users"`
}

// CreateWorkspaceShare: POST /v1/workspace-shares {title, users} (RL_SHARE).
// Seats 1..n follow the order of users.
func (c *Client) CreateWorkspaceShare(ctx context.Context, title string, users []UserRef) (*WorkspaceShare, error) {
	if users == nil {
		users = []UserRef{}
	}
	v, _, err := one[WorkspaceShare](ctx, c, request{Method: "POST", Path: "/v1/workspace-shares", Body: workspaceShareBody{Title: title, Users: users}, Limiter: rlShare, Auth: true}, "share")
	return v, err
}

// ListWorkspaceShares: GET /v1/workspace-shares?role=owner|invited&status=live|ended
// ("" leaves the server default: owner, live). Newest first, at most 50, no pages.
func (c *Client) ListWorkspaceShares(ctx context.Context, role, status string) ([]WorkspaceShare, error) {
	q := url.Values{}
	if role != "" {
		q.Set("role", role)
	}
	if status != "" {
		q.Set("status", status)
	}
	return list[WorkspaceShare](ctx, c, request{Method: "GET", Path: "/v1/workspace-shares", Query: q, Auth: true})
}

// GetWorkspaceShare: GET /v1/workspace-shares/{share_id}
func (c *Client) GetWorkspaceShare(ctx context.Context, id string) (*WorkspaceShare, error) {
	v, _, err := one[WorkspaceShare](ctx, c, request{Method: "GET", Path: escPath("v1", "workspace-shares", id), Auth: true}, "share")
	return v, err
}

// InviteWorkspaceShare: POST /v1/workspace-shares/{share_id}/invites {users} (RL_SHARE).
// People already in the share are skipped.
func (c *Client) InviteWorkspaceShare(ctx context.Context, id string, users []UserRef) (*WorkspaceShare, error) {
	if users == nil {
		users = []UserRef{}
	}
	v, _, err := one[WorkspaceShare](ctx, c, request{Method: "POST", Path: escPath("v1", "workspace-shares", id, "invites"), Body: usersBody{users}, Limiter: rlShare, Auth: true}, "share")
	return v, err
}

// RemoveWorkspaceParticipant: DELETE /v1/workspace-shares/{share_id}/participants/{user};
// the owner removes a member, or a member leaves with u=MeRef.
func (c *Client) RemoveWorkspaceParticipant(ctx context.Context, id string, u UserRef) error {
	_, err := c.call(ctx, request{Method: "DELETE", Path: escPath("v1", "workspace-shares", id, "participants", string(u)), Auth: true}, nil)
	return err
}

// EndWorkspaceShare: DELETE /v1/workspace-shares/{share_id} (the owner only;
// an already ended share is a no-op 204).
func (c *Client) EndWorkspaceShare(ctx context.Context, id string) error {
	_, err := c.call(ctx, request{Method: "DELETE", Path: escPath("v1", "workspace-shares", id), Auth: true}, nil)
	return err
}

// WorkspaceShareWSURL returns ws(s)://.../v1/workspace-shares/<share_id>/ws,
// the share socket (docs/contract/workspace.md 1) that workspace/share dials
// with AuthHeader. The id is escaped as one segment; "", "." and ".." (which
// a URL normaliser would rewrite) give "".
func (c *Client) WorkspaceShareWSURL(id string) string {
	if id == "" || id == "." || id == ".." {
		return ""
	}
	u, err := url.Parse(c.base)
	if err != nil {
		return ""
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	}
	u.RawQuery, u.Fragment = "", ""
	return strings.TrimRight(u.String(), "/") + "/v1/workspace-shares/" + url.PathEscape(id) + "/ws"
}
