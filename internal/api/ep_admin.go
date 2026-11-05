package api

import (
	"context"
)

type ReportRequest struct{ TargetType, TargetID, Reason, Note string }

func (c *Client) Report(ctx context.Context, r ReportRequest) (reportID string, err error) {
	// TODO(core): Task 11
	return
}

type AdminReportQuery struct {
	Status, TargetType string
	PageOpts
}
type AdminRemoveRequest struct{ TargetType, TargetID, Reason, ReportID string }
type AdminSuspendRequest struct {
	Reason      string
	DurationSec Opt[int]
	ReportID    string
}
type AdminUserQuery struct {
	Q         string
	Suspended *bool
	PageOpts
}
type AdminActionQuery struct {
	TargetType, TargetID string
	PageOpts
}

func (c *Client) AdminListReports(ctx context.Context, q AdminReportQuery) (*Page[Report], error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) AdminGetReport(ctx context.Context, reportID string) (*Report, error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) AdminResolveReport(ctx context.Context, reportID, resolution, note string) (*Report, error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) AdminRemoveContent(ctx context.Context, r AdminRemoveRequest) (*AdminAction, error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) AdminSuspend(ctx context.Context, u UserRef, r AdminSuspendRequest) (*AdminUserView, error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) AdminUnsuspend(ctx context.Context, u UserRef) (*AdminUserView, error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) AdminListUsers(ctx context.Context, q AdminUserQuery) (*Page[AdminUserView], error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) AdminListActions(ctx context.Context, q AdminActionQuery) (*Page[AdminAction], error) {
	// TODO(core): Task 11
	return nil, nil
}
