package api_test

import (
	"testing"

	"github.com/patrache/masil/internal/api"
)

// workspaceEndpoints is every 5.25 workspace-share method (P2 Task 14).
var workspaceEndpoints = []string{
	"CreateWorkspaceShare", "ListWorkspaceShares", "GetWorkspaceShare",
	"InviteWorkspaceShare", "RemoveWorkspaceParticipant", "EndWorkspaceShare",
}

func workspaceEndpointCases(t *testing.T) []epCase {
	shareEnv := env(t, "share", "WorkspaceShare")
	return []epCase{
		{name: "CreateWorkspaceShare", route: "POST /v1/workspace-shares", status: 201, resp: shareEnv, limiter: "RL_SHARE",
			call: func(c *api.Client) (any, error) {
				return c.CreateWorkspaceShare(ctx, "결제 서버", []api.UserRef{"@minji", "01JA0000000000000000000002"})
			},
			wantBody: `{"title":"결제 서버","users":["@minji","01JA0000000000000000000002"]}`},
		{name: "CreateWorkspaceShare/nilUsers", route: "POST /v1/workspace-shares", status: 201, resp: shareEnv, limiter: "RL_SHARE",
			call:     func(c *api.Client) (any, error) { return c.CreateWorkspaceShare(ctx, "t", nil) },
			wantBody: `{"title":"t","users":[]}`},
		{name: "ListWorkspaceShares", route: "GET /v1/workspace-shares", resp: itemsOf(t, "WorkspaceShare"),
			call:      func(c *api.Client) (any, error) { return c.ListWorkspaceShares(ctx, "invited", "live") },
			wantQuery: "role=invited&status=live"},
		{name: "ListWorkspaceShares/defaults", route: "GET /v1/workspace-shares", resp: `{"items":[]}`,
			call: func(c *api.Client) (any, error) { return c.ListWorkspaceShares(ctx, "", "") },
			check: func(t *testing.T, got any) {
				if items := got.([]api.WorkspaceShare); items == nil || len(items) != 0 {
					t.Errorf("items = %#v, want empty non-nil", items)
				}
			}},
		{name: "GetWorkspaceShare", route: "GET /v1/workspace-shares/{id}", wantPath: "/v1/workspace-shares/W1", resp: shareEnv,
			call: func(c *api.Client) (any, error) { return c.GetWorkspaceShare(ctx, "W1") },
			check: func(t *testing.T, got any) {
				s := got.(*api.WorkspaceShare)
				if s.ID == "" || s.Owner.Handle != "minji" || len(s.Members) != 1 || s.Members[0].Seat != 1 ||
					s.Members[0].MaxLevel == nil || *s.Members[0].MaxLevel != "editor" || !s.Members[0].Verified ||
					s.EndReason == nil || *s.EndReason != "timeout" || s.EndedAt == nil {
					t.Errorf("share = %+v", s)
				}
			}},
		{name: "InviteWorkspaceShare", route: "POST /v1/workspace-shares/{id}/invites", wantPath: "/v1/workspace-shares/W1/invites", resp: shareEnv, limiter: "RL_SHARE",
			call:     func(c *api.Client) (any, error) { return c.InviteWorkspaceShare(ctx, "W1", []api.UserRef{"@hana"}) },
			wantBody: `{"users":["@hana"]}`},
		{name: "RemoveWorkspaceParticipant", route: "DELETE /v1/workspace-shares/{id}/participants/{user}",
			wantPath: "/v1/workspace-shares/W1/participants/@hana",
			call:     func(c *api.Client) (any, error) { return nil, c.RemoveWorkspaceParticipant(ctx, "W1", "@hana") }},
		{name: "RemoveWorkspaceParticipant/me", route: "DELETE /v1/workspace-shares/{id}/participants/{user}",
			wantPath: "/v1/workspace-shares/W1/participants/@me",
			call:     func(c *api.Client) (any, error) { return nil, c.RemoveWorkspaceParticipant(ctx, "W1", api.MeRef) }},
		{name: "EndWorkspaceShare", route: "DELETE /v1/workspace-shares/{id}", wantPath: "/v1/workspace-shares/W1",
			call: func(c *api.Client) (any, error) { return nil, c.EndWorkspaceShare(ctx, "W1") }},
	}
}

// TestWorkspaceEndpoints: every workspace-share method's method, path, query,
// body and RL_SHARE limiter (api.md 5.25 / 6).
func TestWorkspaceEndpoints(t *testing.T) {
	cases := workspaceEndpointCases(t)
	checkCoverage(t, workspaceEndpoints, cases)
	runCases(t, cases)
}

// TestWorkspaceShareWSURL: ws(s) scheme, the base path kept, the share id
// escaped as one segment, and "", "." and ".." refused.
func TestWorkspaceShareWSURL(t *testing.T) {
	for base, want := range map[string]string{
		"https://h":             "wss://h/v1/workspace-shares/W1/ws",
		"http://localhost:8787": "ws://localhost:8787/v1/workspace-shares/W1/ws",
		"https://h/api/":        "wss://h/api/v1/workspace-shares/W1/ws",
	} {
		if got := api.New(base).WorkspaceShareWSURL("W1"); got != want {
			t.Errorf("WorkspaceShareWSURL(%s) = %q, want %q", base, got, want)
		}
	}
	if got := api.New("https://h").WorkspaceShareWSURL("a/b"); got != "wss://h/v1/workspace-shares/a%2Fb/ws" {
		t.Errorf("escaped = %q", got)
	}
	for _, bad := range []string{"", ".", ".."} {
		if got := api.New("https://h").WorkspaceShareWSURL(bad); got != "" {
			t.Errorf("WorkspaceShareWSURL(%q) = %q, want \"\"", bad, got)
		}
	}
}

// TestWorkspaceAlertKinds: the two v0.1.3 alert kinds keep their wire names.
func TestWorkspaceAlertKinds(t *testing.T) {
	if api.AlertWorkspaceInvite != "workspace_invite" || api.AlertWorkspaceAttention != "workspace_attention" {
		t.Errorf("alert kinds = %q, %q", api.AlertWorkspaceInvite, api.AlertWorkspaceAttention)
	}
}
