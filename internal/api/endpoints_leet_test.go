package api_test

import (
	"testing"

	"github.com/patrache/masil/internal/api"
)

// leetEndpoints is every api.md 5.25 LeetCode method (P1 Task 6).
var leetEndpoints = []string{
	"SetSpaceLeet", "ReportLeetDaily", "PutLeetSolve", "DeleteLeetSolve",
	"LeetMe", "LeetSpaceBoard", "LeetFriendsBoard",
}

func leetEndpointCases(t *testing.T) []epCase {
	ch := "01JB0000000000000000000001"
	pct := 12.5
	sub := "1234567890"
	code := "func f() {}"
	facts := api.LeetDailyFacts{
		FrontendID: "1606", Slug: "find-servers-that-handled-most-number-of-requests",
		Title: "Find Servers That Handled Most Number of Requests", Difficulty: "Hard",
		Tags: []string{"Array", "Ordered Set"}, IsPaidOnly: false,
	}
	return []epCase{
		{name: "SetSpaceLeet", route: "PUT /v1/spaces/{id}/leet", wantPath: "/v1/spaces/S/leet", resp: env(t, "settings", "LeetSpaceSettings"), limiter: "RL_SOCIAL",
			call:     func(c *api.Client) (any, error) { return c.SetSpaceLeet(ctx, "S", true, &ch) },
			wantBody: `{"enabled":true,"channel_id":"01JB0000000000000000000001"}`},
		{name: "SetSpaceLeet/off", route: "PUT /v1/spaces/{id}/leet", wantPath: "/v1/spaces/S/leet", resp: env(t, "settings", "LeetSpaceSettings"), limiter: "RL_SOCIAL",
			call:     func(c *api.Client) (any, error) { return c.SetSpaceLeet(ctx, "S", false, nil) },
			wantBody: `{"enabled":false}`},
		{name: "ReportLeetDaily", route: "PUT /v1/leet/daily/{date}", wantPath: "/v1/leet/daily/2026-11-05", resp: sample(t, "LeetDailyReport"), limiter: "RL_SOCIAL",
			call: func(c *api.Client) (any, error) { return c.ReportLeetDaily(ctx, "2026-11-05", facts) },
			wantBody: `{"frontend_id":"1606","slug":"find-servers-that-handled-most-number-of-requests",` +
				`"title":"Find Servers That Handled Most Number of Requests","difficulty":"Hard","tags":["Array","Ordered Set"],"is_paid_only":false}`,
			check: func(t *testing.T, got any) {
				r := got.(*api.LeetDailyReport)
				if r.Daily.Date != "2026-11-09" || r.Daily.FrontendID != "1606" || r.Daily.FirstReporter != nil || len(r.Roots) != 1 || r.Roots[0].MessageID == "" {
					t.Errorf("report = %+v", r)
				}
			}},
		{name: "ReportLeetDaily/noTags", route: "PUT /v1/leet/daily/{date}", wantPath: "/v1/leet/daily/2026-11-05", resp: sample(t, "LeetDailyReport"), limiter: "RL_SOCIAL",
			call: func(c *api.Client) (any, error) {
				return c.ReportLeetDaily(ctx, "2026-11-05", api.LeetDailyFacts{FrontendID: "1", Slug: "two-sum", Title: "Two Sum", Difficulty: "Easy", IsPaidOnly: true})
			},
			wantBody: `{"frontend_id":"1","slug":"two-sum","title":"Two Sum","difficulty":"Easy","tags":[],"is_paid_only":true}`},
		{name: "PutLeetSolve", route: "PUT /v1/leet/solves/{date}", wantPath: "/v1/leet/solves/2026-11-05", resp: sample(t, "LeetSolveResult"), limiter: "RL_SOCIAL",
			call: func(c *api.Client) (any, error) {
				return c.PutLeetSolve(ctx, "2026-11-05", api.LeetSolveRequest{
					Slug: "two-sum", Lang: "golang", FrontendID: "1", RuntimePercentile: &pct, SubmissionID: &sub,
					ShareSpaceIDs: []string{"S1", "S2"}, Code: &code,
				})
			},
			wantBody: `{"status":"accepted","slug":"two-sum","lang":"golang","frontend_id":"1","runtime_percentile":12.5,` +
				`"submission_id":"1234567890","share_space_ids":["S1","S2"],"code":"func f() {}"}`,
			check: func(t *testing.T, got any) {
				r := got.(*api.LeetSolveResult)
				if !r.Solve.CountsForStreak || r.Solve.RuntimePercentile == nil || *r.Solve.RuntimePercentile != 12.5 || len(r.Cards) != 1 || r.Cards[0].SpaceID == "" {
					t.Errorf("result = %+v", r)
				}
			}},
		{name: "PutLeetSolve/minimal", route: "PUT /v1/leet/solves/{date}", wantPath: "/v1/leet/solves/2026-11-05", resp: sample(t, "LeetSolveResult"), limiter: "RL_SOCIAL",
			call: func(c *api.Client) (any, error) {
				return c.PutLeetSolve(ctx, "2026-11-05", api.LeetSolveRequest{Slug: "two-sum", Lang: "python3", FrontendID: "1"})
			},
			wantBody: `{"status":"accepted","slug":"two-sum","lang":"python3","frontend_id":"1"}`},
		{name: "DeleteLeetSolve", route: "DELETE /v1/leet/solves/{date}", wantPath: "/v1/leet/solves/2026-11-05", limiter: "RL_SOCIAL",
			call: func(c *api.Client) (any, error) { return nil, c.DeleteLeetSolve(ctx, "2026-11-05") }},
		{name: "LeetMe", route: "GET /v1/leet/me", resp: sample(t, "LeetMe"),
			call: func(c *api.Client) (any, error) { return c.LeetMe(ctx) },
			check: func(t *testing.T, got any) {
				m := got.(*api.LeetMe)
				if m.Current != 3 || m.Best != 7 || len(m.Recent) != 1 || len(m.DailySpaces) != 1 || m.DailySpaces[0].ChannelID == "" {
					t.Errorf("me = %+v", m)
				}
			}},
		{name: "LeetSpaceBoard", route: "GET /v1/spaces/{id}/leet/board", wantPath: "/v1/spaces/S/leet/board", resp: sample(t, "LeetBoard"),
			call:      func(c *api.Client) (any, error) { return c.LeetSpaceBoard(ctx, "S", "month") },
			wantQuery: "period=month",
			check: func(t *testing.T, got any) {
				b := got.(*api.LeetBoard)
				if b.Period != "week" || len(b.Items) != 1 || b.Items[0].Solved != 4 || b.Items[0].Streak != 3 || b.Items[0].User.Handle != "minji" {
					t.Errorf("board = %+v", b)
				}
			}},
		{name: "LeetSpaceBoard/default", route: "GET /v1/spaces/{id}/leet/board", wantPath: "/v1/spaces/S/leet/board", resp: sample(t, "LeetBoard"),
			call: func(c *api.Client) (any, error) { return c.LeetSpaceBoard(ctx, "S", "") }},
		{name: "LeetFriendsBoard", route: "GET /v1/leet/friends/board", resp: sample(t, "LeetFriendsBoard"),
			call: func(c *api.Client) (any, error) { return c.LeetFriendsBoard(ctx) }},
	}
}

// TestLeetEndpoints: every LeetCode method's method, path, query, body and
// RL_SOCIAL limiter (api.md 5.25 / 6).
func TestLeetEndpoints(t *testing.T) {
	cases := leetEndpointCases(t)
	checkCoverage(t, leetEndpoints, cases)
	runCases(t, cases)
}

// TestLeetCardData: the card data types decode the leet.* card samples and the
// card type constants match api.md 2.
func TestLeetCardData(t *testing.T) {
	if api.CardLeetDaily != "leet.daily" || api.CardLeetSolve != "leet.solve" {
		t.Fatalf("card types = %q, %q", api.CardLeetDaily, api.CardLeetSolve)
	}
	var d api.LeetDailyCard
	if err := strictDecode(mustRead(t, "testdata/types/LeetDailyCard.json"), &d); err != nil {
		t.Fatal(err)
	}
	if d.Date != "2026-11-09" || d.Slug == "" || d.Difficulty != "Hard" || len(d.Tags) != 3 {
		t.Errorf("daily card = %+v", d)
	}
	var s api.LeetSolveCard
	if err := strictDecode(mustRead(t, "testdata/types/LeetSolveCard.json"), &s); err != nil {
		t.Fatal(err)
	}
	if !s.Spoiler || s.Code == nil || s.Code.Lang != "golang" || s.RuntimePercentile == nil {
		t.Errorf("solve card = %+v", s)
	}
	var none api.LeetSolveCard
	if err := strictDecode([]byte(`{"date":"2026-11-09","frontend_id":"1","slug":"two-sum","lang":"golang","runtime_percentile":null,"code":null,"spoiler":true}`), &none); err != nil {
		t.Fatal(err)
	}
	if none.Code != nil || none.RuntimePercentile != nil {
		t.Errorf("null fields = %+v", none)
	}
}

// TestUpdateSettingsLeetShare: the friends-board switch goes out only when set.
func TestUpdateSettingsLeetShare(t *testing.T) {
	runCases(t, []epCase{
		{name: "UpdateSettings/leetShare", route: "PATCH /v1/me/settings", resp: env(t, "settings", "Settings"),
			call: func(c *api.Client) (any, error) {
				return c.UpdateSettings(ctx, api.UpdateSettingsRequest{LeetShareWithFriends: api.Some(false)})
			},
			wantBody: `{"leet_share_with_friends":false}`,
			check: func(t *testing.T, got any) {
				if s := got.(*api.Settings); !s.LeetShareWithFriends {
					t.Errorf("settings = %+v, want leet_share_with_friends true from the sample", s)
				}
			}},
	})
}
