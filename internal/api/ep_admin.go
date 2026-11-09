package api

import (
	"context"
	"net/url"
	"strconv"
)

// ---- 5.21 reports ----

// ReportRequest is POST /v1/reports (Note optional).
type ReportRequest struct{ TargetType, TargetID, Reason, Note string }

// reportBody is the POST /v1/reports wire body.
type reportBody struct {
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Reason     string `json:"reason"`
	Note       string `json:"note,omitempty"`
}

// Report: POST /v1/reports (RL_SOCIAL); returns the new report id.
func (c *Client) Report(ctx context.Context, r ReportRequest) (reportID string, err error) {
	var out struct {
		ReportID string `json:"report_id"`
	}
	body := reportBody{TargetType: r.TargetType, TargetID: r.TargetID, Reason: r.Reason, Note: r.Note}
	st, err := c.call(ctx, request{Method: "POST", Path: "/v1/reports", Body: body, Limiter: rlSocial, Auth: true}, &out)
	if err != nil {
		return "", err
	}
	if out.ReportID == "" {
		return "", decodeError(st, "response has no report_id")
	}
	return out.ReportID, nil
}

// ---- 5.22 admin (role=admin only; otherwise 403 not_admin) ----

// AdminReportQuery is GET /v1/admin/reports (Status "" = server default open).
type AdminReportQuery struct {
	Status, TargetType string
	PageOpts
}

// AdminRemoveRequest is POST /v1/admin/content/remove (ReportID optional).
type AdminRemoveRequest struct{ TargetType, TargetID, Reason, ReportID string }

// AdminSuspendRequest is POST /v1/admin/users/{user}/suspend. DurationSec
// omitted or Null = indefinite; ReportID optional.
type AdminSuspendRequest struct {
	Reason      string
	DurationSec Opt[int]
	ReportID    string
}

// AdminUserQuery is GET /v1/admin/users (Suspended nil = both).
type AdminUserQuery struct {
	Q         string
	Suspended *bool
	PageOpts
}

// AdminActionQuery is GET /v1/admin/actions.
type AdminActionQuery struct {
	TargetType, TargetID string
	PageOpts
}

// resolveBody is the POST /v1/admin/reports/{id}/resolve wire body.
type resolveBody struct {
	Resolution string `json:"resolution"`
	Note       string `json:"note,omitempty"`
}

// removeBody is the POST /v1/admin/content/remove wire body.
type removeBody struct {
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Reason     string `json:"reason"`
	ReportID   string `json:"report_id,omitempty"`
}

// suspendBody is the POST /v1/admin/users/{user}/suspend wire body.
type suspendBody struct {
	Reason      string   `json:"reason"`
	DurationSec Opt[int] `json:"duration_sec,omitzero"`
	ReportID    string   `json:"report_id,omitempty"`
}

// AdminListReports: GET /v1/admin/reports?status=&target_type=&cursor=&limit=
func (c *Client) AdminListReports(ctx context.Context, q AdminReportQuery) (*Page[Report], error) {
	query := url.Values{}
	setIf(query, "status", q.Status)
	setIf(query, "target_type", q.TargetType)
	return page[Report](ctx, c, request{Method: "GET", Path: "/v1/admin/reports", Query: q.PageOpts.values(query), Auth: true})
}

// AdminGetReport: GET /v1/admin/reports/{id}
func (c *Client) AdminGetReport(ctx context.Context, reportID string) (*Report, error) {
	v, _, err := one[Report](ctx, c, request{Method: "GET", Path: escPath("v1", "admin", "reports", reportID), Auth: true}, "report")
	return v, err
}

// AdminResolveReport: POST /v1/admin/reports/{id}/resolve
// (resolution content_removed|user_suspended|no_action; note optional).
func (c *Client) AdminResolveReport(ctx context.Context, reportID, resolution, note string) (*Report, error) {
	req := request{Method: "POST", Path: escPath("v1", "admin", "reports", reportID, "resolve"), Body: resolveBody{Resolution: resolution, Note: note}, Auth: true}
	v, _, err := one[Report](ctx, c, req, "report")
	return v, err
}

// AdminRemoveContent: POST /v1/admin/content/remove
func (c *Client) AdminRemoveContent(ctx context.Context, r AdminRemoveRequest) (*AdminAction, error) {
	body := removeBody{TargetType: r.TargetType, TargetID: r.TargetID, Reason: r.Reason, ReportID: r.ReportID}
	v, _, err := one[AdminAction](ctx, c, request{Method: "POST", Path: "/v1/admin/content/remove", Body: body, Auth: true}, "action")
	return v, err
}

// AdminSuspend: POST /v1/admin/users/{user}/suspend
func (c *Client) AdminSuspend(ctx context.Context, u UserRef, r AdminSuspendRequest) (*AdminUserView, error) {
	body := suspendBody{Reason: r.Reason, DurationSec: r.DurationSec, ReportID: r.ReportID}
	v, _, err := one[AdminUserView](ctx, c, request{Method: "POST", Path: escPath("v1", "admin", "users", string(u), "suspend"), Body: body, Auth: true}, "user")
	return v, err
}

// AdminUnsuspend: POST /v1/admin/users/{user}/unsuspend (no body).
func (c *Client) AdminUnsuspend(ctx context.Context, u UserRef) (*AdminUserView, error) {
	v, _, err := one[AdminUserView](ctx, c, request{Method: "POST", Path: escPath("v1", "admin", "users", string(u), "unsuspend"), Auth: true}, "user")
	return v, err
}

// AdminListUsers: GET /v1/admin/users?q=&suspended=&cursor=&limit=
func (c *Client) AdminListUsers(ctx context.Context, q AdminUserQuery) (*Page[AdminUserView], error) {
	query := url.Values{}
	setIf(query, "q", q.Q)
	if q.Suspended != nil {
		query.Set("suspended", strconv.FormatBool(*q.Suspended))
	}
	return page[AdminUserView](ctx, c, request{Method: "GET", Path: "/v1/admin/users", Query: q.PageOpts.values(query), Auth: true})
}

// AdminListActions: GET /v1/admin/actions?target_type=&target_id=&cursor=&limit=
func (c *Client) AdminListActions(ctx context.Context, q AdminActionQuery) (*Page[AdminAction], error) {
	query := url.Values{}
	setIf(query, "target_type", q.TargetType)
	setIf(query, "target_id", q.TargetID)
	return page[AdminAction](ctx, c, request{Method: "GET", Path: "/v1/admin/actions", Query: q.PageOpts.values(query), Auth: true})
}

// AdminSetLeetDaily: PUT /v1/admin/leet/daily/{date} — an operator fixes a
// day's stored LeetCode facts; the server resets mismatch_count and rewrites
// that day's #daily roots (v0.1.3 P1, api.md 5.22).
func (c *Client) AdminSetLeetDaily(ctx context.Context, date string, f LeetDailyFacts) (*LeetDaily, error) {
	if f.Tags == nil {
		f.Tags = []string{}
	}
	v, _, err := one[LeetDaily](ctx, c, request{Method: "PUT", Path: escPath("v1", "admin", "leet", "daily", date), Body: f, Auth: true}, "daily")
	return v, err
}
