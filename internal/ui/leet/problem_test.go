package leet_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/leetcode"
	"github.com/patrache/masil/internal/leetcode/leetcodetest"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/leet"
	"github.com/patrache/masil/internal/ui/uitest"
)

// openToday opens today's problem from the hub (the first-run notice done).
func openToday(t *testing.T, h *uitest.Harness) {
	t.Helper()
	firstRunDone(t, h)
	h.Press("ctrl+t", "1")
}

// reopen closes the tool and opens it again from the hub.
func reopen(h *uitest.Harness) {
	h.Send(ui.CloseToolMsg{InstanceID: "leet:today"})
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteTools}})
	h.Press("1")
}

// dailyReport answers PUT /v1/leet/daily/{date} with slug as masil's fact.
func dailyReport(h *uitest.Harness, date, slug string) {
	h.API.JSON("PUT /v1/leet/daily/{date}", http.StatusOK, api.LeetDailyReport{
		Daily: api.LeetDaily{Date: date, LeetDailyFacts: api.LeetDailyFacts{FrontendID: "1606", Slug: slug, Title: "x", Difficulty: "Hard", Tags: []string{}}},
		Roots: []api.LeetDailyRoot{},
	})
}

const slug1606 = "find-servers-that-handled-most-number-of-requests"

func TestProblemGolden(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	uitest.ForEachSize(t, leetSizes, func(t *testing.T, h *uitest.Harness) {
		leetMe(h, api.LeetMe{Current: 3, Best: 5})
		dailyReport(h, "2026-11-05", slug1606)
		openToday(t, h)
		s := h.Screen()
		if h.Context().Mode >= layout.ModeCompact && !strings.Contains(s, "다음 문제까지 21:55") {
			t.Fatalf("no countdown header:\n%s", s)
		}
		if !strings.Contains(s, "1606") {
			t.Fatalf("no problem:\n%s", s)
		}
		h.Check()
		h.Golden("leet_problem")
	})
}

func TestReportedOnce(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	h := uitest.New(t)
	dailyReport(h, "2026-11-05", slug1606)
	openToday(t, h)
	reopen(h)
	got := apiRequests(h, "PUT", "/v1/leet/daily/2026-11-05")
	if len(got) != 1 {
		t.Fatalf("reports = %d %q, want 1", len(got), got)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(got[0]), &body); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"content", "snippets", "code_snippets", "content_html"} {
		if _, ok := body[k]; ok {
			t.Fatalf("report carries %q: %s", k, got[0])
		}
	}
	if body["slug"] != slug1606 || body["frontend_id"] != "1606" {
		t.Fatalf("report = %s", got[0])
	}
}

func TestMismatchRemembered(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	h := uitest.New(t)
	dailyReport(h, "2026-11-05", "two-sum")
	openToday(t, h)
	if got := leet.MismatchForTest(h.Context(), "2026-11-05"); got != "two-sum" {
		t.Fatalf("mismatch = %q", got)
	}
}

func TestPremiumNoBody(t *testing.T) {
	e := setup(t)
	e.lc.SetDaily("premium.json")
	e.lc.SetDailyDate("2026-11-05")
	h := uitest.New(t)
	openToday(t, h)
	if s := h.Screen(); !strings.Contains(s, "🔒 프리미엄 · o 브라우저에서 열기") || !strings.Contains(s, "253") {
		t.Fatalf("no premium lock:\n%s", s)
	}
	if n := lcRequests(e, "codeSnippets"); n != 0 {
		t.Fatalf("asked for the premium body %d times", n)
	}
}

func TestBlockedShowsHint(t *testing.T) {
	e := setup(t)
	e.lc.Fail("/graphql/", http.StatusForbidden, "text/html", string(leetcodetest.Fixture(t, "cf_challenge.html")))
	h := uitest.New(t)
	openToday(t, h)
	if s := h.Screen(); !strings.Contains(s, "LeetCode 가 지금 이 요청을 막고 있어요 · o 브라우저에서 제출") {
		t.Fatalf("no blocked hint:\n%s", s)
	}
	h.Check()
}

func TestHostileProblemIntegrity(t *testing.T) {
	setup(t)
	uitest.ForEachSize(t, uitest.AllSizes(), func(t *testing.T, h *uitest.Harness) {
		q := &leetcode.Question{QuestionID: "1", FrontendID: "1606", Slug: slug1606,
			Title:       "Evil‮ Title \x1b[31mred\x1b[0m",
			Difficulty:  "Hard",
			Tags:        []string{"Arr‮ay", "\x1b]8;;http://x\x07Heap"},
			ContentHTML: "<p>\x1b[2Jcleared ‮flipped <code>\x1b[5mblink</code></p><pre>\x1b[31mred\nline​2</pre><ul><li>a\tb</li></ul><img src=\"https://assets.leetcode.com/x.png\" alt=\"\x1b[1mimg\">",
			Snippets:    map[string]string{"golang": "func f() {}"}}
		if err := leetcode.OpenCache(h.Context().Paths.Cache).PutDaily("2026-11-05", q); err != nil {
			t.Fatal(err)
		}
		openToday(t, h)
		h.Check()
	})
}

func TestDailyRollsOverOnce(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	h := uitest.New(t)
	h.Advance(21*time.Hour + 54*time.Minute + 30*time.Second) // 23:59:30Z
	openToday(t, h)
	n0 := lcRequests(e, "activeDailyCodingChallengeQuestion")
	if n0 != 1 {
		t.Fatalf("daily queries at open = %d", n0)
	}
	e.lc.SetDailyDate("2026-11-06")
	h.Advance(time.Minute)
	if n := lcRequests(e, "activeDailyCodingChallengeQuestion"); n != n0+1 {
		t.Fatalf("daily queries after the rollover = %d, want %d", n, n0+1)
	}
	if s := h.Screen(); !strings.Contains(s, "2026-11-06 (UTC)") {
		t.Fatalf("header not rolled over:\n%s", s)
	}
	h.Advance(5 * time.Minute)
	if n := lcRequests(e, "activeDailyCodingChallengeQuestion"); n != n0+1 {
		t.Fatalf("asked again: %d", n)
	}
}

func TestWidgetSubTabs(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	h := uitest.New(t)
	h.Resize(80, 24)
	openToday(t, h)
	if s := h.Screen(); !strings.Contains(s, "[문제]") {
		t.Fatalf("no sub-tabs:\n%s", s)
	}
	h.Press("right", "right")
	if s := h.Screen(); !strings.Contains(s, "[결과]") {
		t.Fatalf("not on the result tab:\n%s", s)
	}
	h.Press("1")
	if s := h.Screen(); !strings.Contains(s, "[문제]") {
		t.Fatalf("1 does not go back:\n%s", s)
	}
}

func TestOpenInBrowser(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	h := uitest.New(t)
	var opened []string
	t.Cleanup(leet.SetOpenURLForTest(func(u string) error { opened = append(opened, u); return nil }))
	openToday(t, h)
	h.Press("o")
	if len(opened) != 1 || opened[0] != "https://leetcode.com/problems/"+slug1606+"/" {
		t.Fatalf("opened = %q", opened)
	}
}
