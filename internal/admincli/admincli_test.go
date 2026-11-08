package admincli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
)

var clock = time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC) // 11:05 in Seoul

func seoul(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// run executes admincli.Run against srv with the fixed test clock.
func run(t *testing.T, srv *apitest.Server, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errb bytes.Buffer
	err = Run(t.Context(), srv.Client(), args, IO{Stdout: &out, Stderr: &errb, Now: clock, Loc: seoul(t)})
	return out.String(), errb.String(), err
}

func user(id, handle, name string) api.UserSummary {
	return api.UserSummary{ID: id, Handle: handle, DisplayName: name}
}

func ptr[T any](v T) *T { return &v }

var (
	hana   = user("01JC0HANA00000000000000000", "hana", "하나")
	minji  = user("01JC0MINJI0000000000000000", "minji", "민지")
	junho  = user("01JC0JUNH00000000000000000", "junho", "준호")
	devKim = user("01JC0DEVK1M000000000000000", "dev_kim", "김개발")
	sam    = user("01JC0SAM000000000000000000", "sam", "샘")
)

const (
	r1ID = "01JC1REP0RT000000000000001"
	r2ID = "01JC1REP0RT000000000000002"
	p1ID = "01JC1P0ST00000000000000001"
)

// reportFix: R1 open post report (hana's post), R2 user report on a deleted
// target with an emoji note (wide-char alignment).
func reportFix() []api.Report {
	return []api.Report{
		{
			ID: r1ID, Reporter: minji, TargetType: "post", TargetID: p1ID, TargetUser: ptr(hana),
			Reason: "spam", TargetExists: true, OpenReportsOnTarget: 2, Status: "open",
			CreatedAt: time.Date(2026, 11, 5, 2, 4, 0, 0, time.UTC),
		},
		{
			ID: r2ID, Reporter: junho, TargetType: "user", TargetID: devKim.ID, TargetUser: ptr(devKim),
			Reason: "harassment", Note: "😡 욕설", TargetExists: false, OpenReportsOnTarget: 1, Status: "open",
			CreatedAt: time.Date(2026, 11, 4, 9, 30, 0, 0, time.UTC),
		},
	}
}

// userFix: hana normal, junho suspended for a week, sam admin, minji back
// from a timed suspension that ended (the server keeps suspended_at/until but
// reports suspended=false).
func userFix() []api.AdminUserView {
	j := junho
	j.Suspended = true
	j.DisplayName = "준호 Junho Park the Long Name"
	return []api.AdminUserView{
		{UserSummary: hana, Role: "user", CreatedAt: time.Date(2026, 3, 1, 1, 0, 0, 0, time.UTC), OpenReportCount: 2},
		{UserSummary: j, Role: "user", CreatedAt: time.Date(2026, 11, 4, 1, 0, 0, 0, time.UTC),
			SuspendedAt: ptr(time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)), SuspendedUntil: ptr(time.Date(2026, 11, 12, 2, 5, 0, 0, time.UTC)),
			SuspendReason: ptr("spam")},
		{UserSummary: sam, Role: "admin", CreatedAt: time.Date(2025, 12, 31, 1, 0, 0, 0, time.UTC)},
		{UserSummary: minji, Role: "user", CreatedAt: time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC),
			SuspendedAt: ptr(time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC)), SuspendedUntil: ptr(time.Date(2026, 10, 27, 1, 0, 0, 0, time.UTC)),
			SuspendReason: ptr("spam")},
	}
}

// actionFix: a content removal with a long reason and a suspension.
func actionFix() []api.AdminAction {
	return []api.AdminAction{
		{ID: "01JC1ACT10N000000000000001", Admin: sam, Action: "remove_content", TargetType: "post", TargetID: p1ID,
			ReportID: ptr(r1ID), Reason: "illegal content 불법 촬영물 링크 공유로 즉시 삭제함", CreatedAt: time.Date(2026, 11, 5, 2, 0, 0, 0, time.UTC)},
		{ID: "01JC1ACT10N000000000000002", Admin: sam, Action: "suspend_user", TargetType: "user", TargetID: junho.ID,
			Reason: "스팸 반복", CreatedAt: time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)},
	}
}

func checkTableLines(t *testing.T, out string) {
	t.Helper()
	for i, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if ansi.StringWidth(line) != ansi.StringWidthWc(line) {
			t.Errorf("line %d: StringWidth %d != StringWidthWc %d: %q", i, ansi.StringWidth(line), ansi.StringWidthWc(line), line)
		}
		if strings.TrimRight(line, " ") != line {
			t.Errorf("line %d has trailing spaces: %q", i, line)
		}
	}
	if strings.Contains(out, "msl_test") {
		t.Errorf("output contains the token: %q", out)
	}
}

func TestAdminOutput(t *testing.T) {
	c2 := "c2"
	cases := []struct {
		name  string
		setup func(srv *apitest.Server)
		args  []string
	}{
		{"reports", func(srv *apitest.Server) {
			srv.JSON("GET /v1/admin/reports", 200, api.Page[api.Report]{Items: reportFix(), NextCursor: &c2})
		}, []string{"reports"}},
		{"reports_empty", func(srv *apitest.Server) {
			srv.JSON("GET /v1/admin/reports", 200, api.Page[api.Report]{Items: []api.Report{}})
		}, []string{"reports"}},
		{"report_detail", func(srv *apitest.Server) {
			r := reportFix()[0]
			r.Snapshot = json.RawMessage(`{"body":"광고 링크 \u001b[31m여기\u001b[0m\n클릭하세요 👉 https://spam.example","author_id":"x"}`)
			srv.JSON("GET /v1/admin/reports/{id}", 200, map[string]any{"report": r})
		}, []string{"report", r1ID}},
		{"report_resolved", func(srv *apitest.Server) {
			r := reportFix()[1]
			r.Status, r.Resolution, r.ResolutionNote = "dismissed", ptr("no_action"), ptr("증거 부족")
			r.ResolvedBy, r.ResolvedAt = ptr(sam), ptr(time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC))
			r.Snapshot = json.RawMessage(`{"handle":"dev_kim","display_name":"김개발","bio":"안녕"}`)
			srv.JSON("GET /v1/admin/reports/{id}", 200, map[string]any{"report": r})
		}, []string{"report", r2ID}},
		{"users", func(srv *apitest.Server) {
			srv.JSON("GET /v1/admin/users", 200, api.Page[api.AdminUserView]{Items: userFix()})
		}, []string{"users"}},
		{"actions", func(srv *apitest.Server) {
			srv.JSON("GET /v1/admin/actions", 200, api.Page[api.AdminAction]{Items: actionFix()})
		}, []string{"actions"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := apitest.New(t)
			tc.setup(srv)
			out, errOut, err := run(t, srv, tc.args...)
			if err != nil {
				t.Fatalf("Run: %v (stderr %q)", err, errOut)
			}
			if errOut != "" {
				t.Fatalf("stderr = %q", errOut)
			}
			checkTableLines(t, out)
			golden.RequireEqual(t, out)
		})
	}
}

// spoof holds bidi controls, invisible format characters, line separators
// and escapes another user can put in a name, a report note or reported
// content: "\u202e"+"txt.exe" reads as "exe.txt", U+2028 starts a fake line
// (pending-fixes 1).
const spoof = "\u202etxt.exe\u202c \u2067역\u2069 \u200e\u200f\u061c ad\u200bmin \u2060\ufeff\u00ad" +
	"가짜\u2028[관리자]\u2029 \x1b[2J\x1b]52;c;ZXZpbA==\x07\u009b31m tag\U000e0041"

// TestAdminOutputDropsSpoofing: every command that prints user-supplied text
// (names, notes, snapshots, reasons) prints none of spoof's invisible runes.
func TestAdminOutputDropsSpoofing(t *testing.T) {
	evil := func(u api.UserSummary) api.UserSummary { u.DisplayName += spoof; return u }
	rep := reportFix()[1]
	rep.Reporter, rep.TargetUser = evil(rep.Reporter), ptr(evil(*rep.TargetUser))
	rep.Note = spoof
	rep.ResolutionNote = ptr(spoof)
	snap, _ := json.Marshal(map[string]string{"body": spoof + "\n" + spoof})
	keyed, _ := json.Marshal(map[string]string{spoof: spoof})
	users := userFix()
	for i := range users {
		users[i].UserSummary = evil(users[i].UserSummary)
		users[i].SuspendReason = ptr(spoof)
	}
	acts := actionFix()
	for i := range acts {
		acts[i].Admin = evil(acts[i].Admin)
		acts[i].Reason = spoof
	}
	for name, tc := range map[string]struct {
		setup func(srv *apitest.Server)
		args  []string
		shown bool // the command prints the spoofed text (the reports list prints no names or notes)
	}{
		"reports": {func(srv *apitest.Server) {
			srv.JSON("GET /v1/admin/reports", 200, api.Page[api.Report]{Items: []api.Report{rep}})
		}, []string{"reports"}, false},
		"report body": {func(srv *apitest.Server) {
			r := rep
			r.Snapshot = snap
			srv.JSON("GET /v1/admin/reports/{id}", 200, map[string]any{"report": r})
		}, []string{"report", r2ID}, true},
		"report keys": {func(srv *apitest.Server) {
			r := rep
			r.Snapshot = keyed
			srv.JSON("GET /v1/admin/reports/{id}", 200, map[string]any{"report": r})
		}, []string{"report", r2ID}, true},
		"users": {func(srv *apitest.Server) {
			srv.JSON("GET /v1/admin/users", 200, api.Page[api.AdminUserView]{Items: users})
		}, []string{"users"}, true},
		"actions": {func(srv *apitest.Server) {
			srv.JSON("GET /v1/admin/actions", 200, api.Page[api.AdminAction]{Items: acts})
		}, []string{"actions"}, true},
	} {
		t.Run(name, func(t *testing.T) {
			srv := apitest.New(t)
			tc.setup(srv)
			out, errOut, err := run(t, srv, tc.args...)
			if err != nil {
				t.Fatalf("Run: %v (stderr %q)", err, errOut)
			}
			if tc.shown && !strings.Contains(out, "txt.exe") {
				t.Fatalf("spoofed text not printed at all: %q", out)
			}
			for _, r := range out {
				if r != '\n' && (r < 0x20 || r >= 0x7f && r <= 0x9f || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)) {
					t.Fatalf("output carries %U: %q", r, out)
				}
			}
		})
	}
}

func TestReportsNextPageKeepsFilters(t *testing.T) {
	srv := apitest.New(t)
	c := "c9"
	srv.JSON("GET /v1/admin/reports", 200, api.Page[api.Report]{Items: reportFix()[:1], NextCursor: &c})
	out, _, err := run(t, srv, "reports", "--status", "resolved", "--type", "post")
	if err != nil {
		t.Fatal(err)
	}
	want := "다음 페이지: masil admin reports --status resolved --type post --cursor c9\n"
	if !strings.HasSuffix(out, want) {
		t.Fatalf("last line: got %q, want suffix %q", out, want)
	}
}

func TestTableAlignsWideChars(t *testing.T) {
	out := Table([]string{"이름", "ID"}, [][]string{{"하나", "A"}, {"hana", "B"}, {"👍x", "C"}})
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("no trailing newline: %q", out)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("got %d lines: %q", len(lines), out)
	}
	want := -1
	for i, line := range lines {
		col2 := strings.LastIndex(line, "  ") + 2
		w := ansi.StringWidth(line[:col2])
		if want == -1 {
			want = w
		}
		if w != want {
			t.Errorf("line %d: column 2 starts at %d, want %d (%q)", i, w, want, line)
		}
		if strings.TrimRight(line, " ") != line {
			t.Errorf("line %d has trailing spaces: %q", i, line)
		}
	}
	if want != 6 { // widest first cell "hana"/"하나"/"👍x" = 4, + 2-space gap
		t.Errorf("column 2 at %d, want 6", want)
	}
}

func jsonEqual(t *testing.T, got []byte, want string) bool {
	t.Helper()
	if want == "" {
		return len(bytes.TrimSpace(got)) == 0
	}
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Errorf("body %q: %v", got, err)
		return false
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(g, w)
}

func TestAdminRequests(t *testing.T) {
	report := map[string]any{"report": reportFix()[0]}
	view := map[string]any{"user": userFix()[0]}
	action := map[string]any{"action": actionFix()[0]}
	cases := []struct {
		args             string
		method, path, qs string
		body             string
	}{
		{"reports --status resolved --type post", "GET", "/v1/admin/reports", "status=resolved&target_type=post", ""},
		{"reports --cursor c2 --limit 5", "GET", "/v1/admin/reports", "cursor=c2&limit=5", ""},
		{"report R1", "GET", "/v1/admin/reports/R1", "", ""},
		{"resolve R1 no_action --note 중복", "POST", "/v1/admin/reports/R1/resolve", "", `{"resolution":"no_action","note":"중복"}`},
		{"resolve R1 content_removed", "POST", "/v1/admin/reports/R1/resolve", "", `{"resolution":"content_removed"}`},
		{"remove post P1 --reason illegal --report R1", "POST", "/v1/admin/content/remove", "", `{"target_type":"post","target_id":"P1","reason":"illegal","report_id":"R1"}`},
		{"remove upload U1 --reason illegal", "POST", "/v1/admin/content/remove", "", `{"target_type":"upload","target_id":"U1","reason":"illegal"}`},
		{"suspend @hana --for 7d --reason spam", "POST", "/v1/admin/users/@hana/suspend", "", `{"reason":"spam","duration_sec":604800}`},
		{"suspend @hana --reason spam", "POST", "/v1/admin/users/@hana/suspend", "", `{"reason":"spam"}`},
		{"suspend @Hana --reason spam --report R1 --for 1h30m", "POST", "/v1/admin/users/@hana/suspend", "", `{"reason":"spam","duration_sec":5400,"report_id":"R1"}`},
		{"suspend " + hana.ID + " --reason spam", "POST", "/v1/admin/users/" + hana.ID + "/suspend", "", `{"reason":"spam"}`},
		{"unsuspend @hana", "POST", "/v1/admin/users/@hana/unsuspend", "", ""},
		{"users --q ha --suspended true", "GET", "/v1/admin/users", "q=ha&suspended=true", ""},
		{"users --suspended false", "GET", "/v1/admin/users", "suspended=false", ""},
		{"actions --target-type post --target P1", "GET", "/v1/admin/actions", "target_type=post&target_id=P1", ""},
	}
	for _, tc := range cases {
		t.Run(tc.args, func(t *testing.T) {
			srv := apitest.New(t)
			srv.JSON("GET /v1/admin/reports", 200, api.Page[api.Report]{Items: []api.Report{}})
			srv.JSON("GET /v1/admin/reports/{id}", 200, report)
			srv.JSON("POST /v1/admin/reports/{id}/resolve", 200, report)
			srv.JSON("POST /v1/admin/content/remove", 200, action)
			srv.JSON("POST /v1/admin/users/{user}/suspend", 200, view)
			srv.JSON("POST /v1/admin/users/{user}/unsuspend", 200, view)
			srv.JSON("GET /v1/admin/users", 200, api.Page[api.AdminUserView]{Items: []api.AdminUserView{}})
			srv.JSON("GET /v1/admin/actions", 200, api.Page[api.AdminAction]{Items: []api.AdminAction{}})
			_, errOut, err := run(t, srv, strings.Fields(tc.args)...)
			if err != nil {
				t.Fatalf("Run: %v (stderr %q)", err, errOut)
			}
			reqs := srv.Requests()
			if len(reqs) != 1 {
				t.Fatalf("got %d requests, want 1: %+v", len(reqs), reqs)
			}
			r := reqs[0]
			path, _ := url.PathUnescape(r.Path)
			if r.Method != tc.method || path != tc.path {
				t.Errorf("request %s %s, want %s %s", r.Method, path, tc.method, tc.path)
			}
			gq, _ := url.ParseQuery(r.Query)
			wq, _ := url.ParseQuery(tc.qs)
			if !reflect.DeepEqual(gq, wq) {
				t.Errorf("query %q, want %q", r.Query, tc.qs)
			}
			if !jsonEqual(t, r.Body, tc.body) {
				t.Errorf("body %s, want %s", r.Body, tc.body)
			}
		})
	}
}

func TestAdminMutationOutput(t *testing.T) {
	resolved := reportFix()[0]
	resolved.Status, resolved.Resolution = "resolved", ptr("content_removed")
	suspended := userFix()[1]
	indefinite := userFix()[1]
	indefinite.SuspendedUntil = nil
	cases := []struct {
		args  string
		setup func(srv *apitest.Server)
		want  string
	}{
		{"resolve " + r1ID + " content_removed", func(srv *apitest.Server) {
			srv.JSON("POST /v1/admin/reports/{id}/resolve", 200, map[string]any{"report": resolved})
		}, "신고 " + r1ID + ": 처리됨 (콘텐츠 삭제)\n"},
		{"remove post " + p1ID + " --reason illegal", func(srv *apitest.Server) {
			srv.JSON("POST /v1/admin/content/remove", 200, map[string]any{"action": actionFix()[0]})
		}, "삭제했습니다: post " + p1ID + " (조치 01JC1ACT10N000000000000001)\n"},
		{"suspend @junho --for 7d --reason spam", func(srv *apitest.Server) {
			srv.JSON("POST /v1/admin/users/{user}/suspend", 200, map[string]any{"user": suspended})
		}, "정지했습니다: @junho — 11월 12일 11:05까지\n"},
		{"suspend @junho --reason spam", func(srv *apitest.Server) {
			srv.JSON("POST /v1/admin/users/{user}/suspend", 200, map[string]any{"user": indefinite})
		}, "정지했습니다: @junho — 무기한\n"},
		{"unsuspend @junho", func(srv *apitest.Server) {
			srv.JSON("POST /v1/admin/users/{user}/unsuspend", 200, map[string]any{"user": userFix()[0]})
		}, "정지를 풀었습니다: @junho\n"},
	}
	for _, tc := range cases {
		t.Run(tc.args, func(t *testing.T) {
			srv := apitest.New(t)
			tc.setup(srv)
			out, errOut, err := run(t, srv, strings.Fields(tc.args)...)
			if err != nil {
				t.Fatalf("Run: %v (stderr %q)", err, errOut)
			}
			if out != tc.want {
				t.Fatalf("stdout = %q, want %q", out, tc.want)
			}
		})
	}
}

func TestAdminUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"bogus"},
		{"report"},
		{"reports", "extra"},
		{"reports", "--status", "maybe"},
		{"reports", "--bogus"},
		{"resolve", "R1", "maybe"},
		{"resolve", "R1"},
		{"remove", "post", "P1"},
		{"remove", "bogus", "P1", "--reason", "x"},
		{"suspend", "hana", "--reason", "x"},
		{"suspend", "@hana", "--for", "30s", "--reason", "x"},
		{"suspend", "@hana"},
		{"unsuspend"},
		{"users", "--suspended", "maybe"},
		{"actions", "--limit", "-1"},
	} {
		srv := apitest.New(t)
		_, errOut, err := run(t, srv, args...)
		if !errors.Is(err, ErrUsage) {
			t.Errorf("%q: err = %v, want ErrUsage", args, err)
		}
		if !strings.Contains(errOut, "사용법: masil admin") {
			t.Errorf("%q: stderr %q has no usage", args, errOut)
		}
		if n := len(srv.Requests()); n != 0 {
			t.Errorf("%q: %d requests sent", args, n)
		}
	}
}

func TestAdminSuspendForMessage(t *testing.T) {
	srv := apitest.New(t)
	_, errOut, err := run(t, srv, "suspend", "@hana", "--for", "30s", "--reason", "x")
	if !errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(errOut, "정지 기간은 7d, 12h, 30m 처럼 1분 이상으로 적어 주세요") {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestAdminHelp(t *testing.T) {
	srv := apitest.New(t)
	for _, a := range []string{"help", "-h", "--help"} {
		out, errOut, err := run(t, srv, a)
		if err != nil || out != Usage+"\n" || errOut != "" {
			t.Errorf("%s: err %v stdout %q stderr %q", a, err, out, errOut)
		}
	}
}

func TestAdminNotAdmin(t *testing.T) {
	srv := apitest.New(t)
	srv.Error("GET /v1/admin/reports", 403, "not_admin")
	out, errOut, err := run(t, srv, "reports")
	if err == nil {
		t.Fatal("want error")
	}
	if errOut != "운영자 권한이 없습니다.\n" || out != "" {
		t.Fatalf("stdout %q stderr %q", out, errOut)
	}
}

func TestAdminSuspendAdminConflict(t *testing.T) {
	srv := apitest.New(t)
	srv.Error("POST /v1/admin/users/{user}/suspend", 409, "conflict")
	_, errOut, err := run(t, srv, "suspend", "@sam", "--reason", "x")
	if err == nil {
		t.Fatal("want error")
	}
	if errOut != "운영자는 정지할 수 없습니다.\n" {
		t.Fatalf("stderr %q", errOut)
	}
}

func TestAdminOtherError(t *testing.T) {
	srv := apitest.New(t)
	srv.Error("GET /v1/admin/reports/{id}", 404, "not_found")
	_, errOut, err := run(t, srv, "report", "R9")
	if err == nil || errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v", err)
	}
	if want := "오류: " + api.Describe(err) + "\n"; errOut != want {
		t.Fatalf("stderr %q, want %q", errOut, want)
	}
}

func TestParseFor(t *testing.T) {
	for s, want := range map[string]int{"7d": 604800, "1d": 86400, "12h": 43200, "1h30m": 5400, "90s": 90, "30m": 1800, "60s": 60} {
		got, err := ParseFor(s)
		if err != nil || got != want {
			t.Errorf("ParseFor(%q) = %d, %v; want %d", s, got, err, want)
		}
	}
	for _, s := range []string{"30s", "", "0d", "-1h", "7x", "07d", "1.5s", "61500ms", "99999999999999999999d", "d"} {
		if got, err := ParseFor(s); err == nil {
			t.Errorf("ParseFor(%q) = %d, want error", s, got)
		}
	}
}

// RunLazy builds the client only after the arguments are valid.
func TestRunLazySkipsClientOnHelpAndUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"--help"}, {"bogus"}, {"users", "-h"}, {"users", "--x"}, {"suspend", "nope"}} {
		called := false
		mk := func() (*api.Client, error) { called = true; return nil, errors.New("no client") }
		var out, errb bytes.Buffer
		_ = RunLazy(context.Background(), mk, args, IO{Stdout: &out, Stderr: &errb})
		if called {
			t.Errorf("%v: client constructor called", args)
		}
	}
	sentinel := errors.New("no creds")
	var out, errb bytes.Buffer
	err := RunLazy(context.Background(), func() (*api.Client, error) { return nil, sentinel }, []string{"users"}, IO{Stdout: &out, Stderr: &errb})
	if !errors.Is(err, sentinel) || errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v, want the constructor error", err)
	}
}
