package admincli

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/i18n"
)

func leetDailyResp(date, slug string) map[string]any {
	return map[string]any{"daily": map[string]any{
		"date": date, "frontend_id": "1606", "slug": slug, "title": "Find Servers", "difficulty": "Hard",
		"tags": []string{"Array"}, "is_paid_only": false, "first_reporter": nil,
		"first_reported_at": "2026-11-05T00:01:00Z", "mismatch_count": 0,
	}}
}

func TestAdminLeetDailySet(t *testing.T) {
	const date, slug = "2026-11-05", "find-servers-that-handled-most-number-of-requests"
	srv := apitest.New(t)
	var body []byte
	srv.Handle("PUT /v1/admin/leet/daily/{date}", func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(leetDailyResp(date, slug))
	})
	out, errOut, err := run(t, srv, "leet-daily", "set", date, "--id", "1606", "--slug", slug,
		"--title", "Find Servers That Handled Most Number of Requests", "--difficulty", "Hard",
		"--tags", "Array, Ordered Set,", "--paid")
	if err != nil {
		t.Fatalf("err %v stderr %q", err, errOut)
	}
	reqs := srv.Requests()
	if len(reqs) != 1 || reqs[0].Method != "PUT" || reqs[0].Path != "/v1/admin/leet/daily/"+date {
		t.Fatalf("requests = %+v", reqs)
	}
	var got api.LeetDailyFacts
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	want := api.LeetDailyFacts{FrontendID: "1606", Slug: slug, Title: "Find Servers That Handled Most Number of Requests",
		Difficulty: "Hard", Tags: []string{"Array", "Ordered Set"}, IsPaidOnly: true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("body = %+v\nwant %+v", got, want)
	}
	if wantOut := i18n.For(i18n.Ko).Leetcode.AdminDailySet(date, slug) + "\n"; out != wantOut || errOut != "" {
		t.Fatalf("stdout %q (want %q) stderr %q", out, wantOut, errOut)
	}
}

func TestAdminLeetDailyNoTagsSendsEmptyList(t *testing.T) {
	srv := apitest.New(t)
	var body []byte
	srv.Handle("PUT /v1/admin/leet/daily/{date}", func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(leetDailyResp("2026-11-05", "two-sum"))
	})
	if _, errOut, err := run(t, srv, "leet-daily", "set", "2026-11-05", "--id", "1", "--slug", "two-sum", "--title", "Two Sum", "--difficulty", "Easy"); err != nil {
		t.Fatalf("err %v stderr %q", err, errOut)
	}
	if !strings.Contains(string(body), `"tags":[]`) || !strings.Contains(string(body), `"is_paid_only":false`) {
		t.Fatalf("body = %s", body)
	}
}

func TestAdminLeetDailyUsage(t *testing.T) {
	ok := []string{"--id", "1606", "--slug", "two-sum", "--title", "Two Sum", "--difficulty", "Easy"}
	with := func(date string, flags ...string) []string { return append([]string{"leet-daily", "set", date}, flags...) }
	replace := func(name, v string) []string {
		out := append([]string(nil), ok...)
		for i := 0; i+1 < len(out); i += 2 {
			if out[i] == name {
				out[i+1] = v
			}
		}
		return out
	}
	usageLine := i18n.For(i18n.Ko).Leetcode.AdminUsage()
	for _, args := range [][]string{
		{"leet-daily"},
		{"leet-daily", "get", "2026-11-05"},
		{"leet-daily", "set"},
		{"leet-daily", "set", "--id", "1"},
		with("2026-13-01", ok...),
		with("20261105", ok...),
		with("2026-02-30", ok...),
		with("2026-11-05", replace("--slug", "../x")...),
		with("2026-11-05", replace("--slug", "Two-Sum")...),
		with("2026-11-05", replace("--slug", "a b")...),
		with("2026-11-05", replace("--slug", strings.Repeat("a", 101))...),
		with("2026-11-05", replace("--id", "12a")...),
		with("2026-11-05", replace("--id", "1234567")...),
		with("2026-11-05", replace("--difficulty", "Insane")...),
		with("2026-11-05", replace("--title", "  ")...),
		with("2026-11-05", "--slug", "two-sum", "--title", "Two Sum", "--difficulty", "Easy"),
		with("2026-11-05", append(ok, "extra")...),
		with("2026-11-05", append(ok, "--bogus")...),
	} {
		srv := apitest.New(t)
		_, errOut, err := run(t, srv, args...)
		if !errors.Is(err, ErrUsage) {
			t.Errorf("%q: err = %v, want ErrUsage", args, err)
		}
		if !strings.Contains(errOut, usageLine) {
			t.Errorf("%q: stderr %q has no leet-daily usage", args, errOut)
		}
		if n := len(srv.Requests()); n != 0 {
			t.Errorf("%q: %d requests sent", args, n)
		}
	}
}

func TestAdminLeetDailyHelpAndNotAdmin(t *testing.T) {
	srv := apitest.New(t)
	out, errOut, err := run(t, srv, "leet-daily", "-h")
	if err != nil || out != i18n.For(i18n.Ko).Leetcode.AdminUsage()+"\n" || errOut != "" {
		t.Fatalf("help: err %v stdout %q stderr %q", err, out, errOut)
	}
	srv.Error("PUT /v1/admin/leet/daily/{date}", 403, "not_admin")
	_, errOut, err = run(t, srv, "leet-daily", "set", "2026-11-05", "--id", "1", "--slug", "two-sum", "--title", "Two Sum", "--difficulty", "Easy")
	if err == nil || errOut != notAdminMessage+"\n" {
		t.Fatalf("not admin: err %v stderr %q", err, errOut)
	}
}
