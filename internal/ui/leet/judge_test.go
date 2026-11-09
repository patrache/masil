package leet_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/leetcode"
	"github.com/patrache/masil/internal/leetcode/leetcodetest"
	"github.com/patrache/masil/internal/secrets"
	"github.com/patrache/masil/internal/ui/leet"
	"github.com/patrache/masil/internal/ui/uitest"
)

const (
	runID    = "runcode_1762650000.123456_r1"
	submitID = "1234567890"
)

// credNS is this harness account's LeetCode cookie namespace.
func credNS(t *testing.T, h *uitest.Harness) leetcode.CredNS {
	t.Helper()
	server, user, ok := h.Context().SecretScope()
	if !ok {
		t.Fatal("no secret scope")
	}
	return leetcode.CredNS{ServerHost: server, UserID: user}
}

// signIn stores cookies for the harness account (as a past login did).
func signIn(t *testing.T, e *env, h *uitest.Harness) {
	t.Helper()
	e.lc.SetUserStatus(true, "sam_lc", false)
	c := leetcode.Credentials{Session: secrets.New("sess-SECRET-abcd"), CSRF: secrets.New("csrf-XYZ-9876")}
	if err := leetcode.SaveCredentials(context.Background(), h.Context().Secrets, credNS(t, h), c); err != nil {
		t.Fatal(err)
	}
}

// judgeReady is today's problem open, signed in.
func judgeReady(t *testing.T) (*env, *uitest.Harness) {
	t.Helper()
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	h := uitest.New(t)
	signIn(t, e, h)
	openToday(t, h)
	return e, h
}

// sent are the fake LeetCode's requests to a path containing sub.
func sent(e *env, sub string) []leetcodetest.Recorded {
	var out []leetcodetest.Recorded
	for _, r := range e.lc.Requests() {
		if strings.Contains(r.Path, sub) {
			out = append(out, r)
		}
	}
	return out
}

func TestRunFlowExamples(t *testing.T) {
	e, h := judgeReady(t)
	e.lc.QueueCheck(runID, "check_pending.json", "check_run_success.json")
	h.Press("r")
	if n := len(sent(e, "interpret_solution")); n != 1 {
		t.Fatalf("interpret requests = %d", n)
	}
	h.Advance(time.Second)
	if s := h.Screen(); strings.Contains(s, "✓ 예제") {
		t.Fatalf("result before the check finished:\n%s", s)
	}
	h.Advance(time.Second)
	s := h.Screen()
	if !strings.Contains(s, "✓ 예제 3/3") {
		t.Fatalf("no run summary:\n%s", s)
	}
	if n := len(sent(e, "/check/")); n != 2 {
		t.Fatalf("checks = %d, want 2", n)
	}
	h.Check()
}

func TestRunReadsSavedFile(t *testing.T) {
	e, h := judgeReady(t)
	e.lc.QueueCheck(runID, "check_run_success.json")
	h.Press("e", "i")
	h.Type("// unsaved")
	h.Press("esc", "esc", "r")
	reqs := sent(e, "interpret_solution")
	if len(reqs) != 1 {
		t.Fatalf("interpret requests = %d", len(reqs))
	}
	var body struct {
		TypedCode string `json:"typed_code"`
		Lang      string `json:"lang"`
	}
	if err := json.Unmarshal([]byte(reqs[0].Body), &body); err != nil {
		t.Fatal(err)
	}
	if body.TypedCode != snippet1606 || body.Lang != "golang" {
		t.Fatalf("sent %+v, want the saved file", body)
	}
}

func TestPacerBlocksSecondRun(t *testing.T) {
	e, h := judgeReady(t)
	e.lc.QueueCheck(runID, "check_pending.json", "check_run_success.json")
	h.Press("r")
	h.Advance(time.Second)
	h.Advance(time.Second)
	h.Advance(time.Second)
	h.Press("r")
	if n := len(sent(e, "interpret_solution")); n != 1 {
		t.Fatalf("interpret requests = %d, want 1 (paced)", n)
	}
	if s := h.Screen(); !strings.Contains(s, "7초") {
		t.Fatalf("no wait shown:\n%s", s)
	}
}

func TestSubmitAccepted(t *testing.T) {
	e, h := judgeReady(t)
	e.lc.QueueCheck(submitID, "check_pending.json", "check_submit_accepted.json")
	h.Press("s")
	if n := len(sent(e, "/submit/")); n != 1 {
		t.Fatalf("submit requests = %d", n)
	}
	h.Advance(time.Second)
	h.Advance(time.Second)
	if s := h.Screen(); !strings.Contains(s, "✅ Accepted · 상위 15% · 183 ms") {
		t.Fatalf("no accepted line:\n%s", s)
	}
}

func TestSubmitWrongAnswer(t *testing.T) {
	e, h := judgeReady(t)
	e.lc.QueueCheck(submitID, "check_wrong_answer.json")
	h.Press("s")
	h.Advance(time.Second)
	s := h.Screen()
	if !strings.Contains(s, "✗ Wrong Answer · 57/63") || !strings.Contains(s, "[0,1,2]") {
		t.Fatalf("no wrong answer:\n%s", s)
	}
}

func TestCheckTimesOut(t *testing.T) {
	e, h := judgeReady(t)
	e.lc.QueueCheck(runID, "check_pending.json")
	h.Press("r")
	h.Advance(61 * time.Second)
	if n := len(sent(e, "/check/")); n != 60 {
		t.Fatalf("checks = %d, want 60", n)
	}
	if s := h.Screen(); !strings.Contains(s, "결과를 받지 못했어요") {
		t.Fatalf("no timeout:\n%s", s)
	}
}

func TestRunBlockedShowsBrowserHint(t *testing.T) {
	e, h := judgeReady(t)
	e.lc.Fail("/problems/"+slug1606+"/interpret_solution/", http.StatusForbidden, "text/html", string(leetcodetest.Fixture(t, "cf_challenge.html")))
	h.Press("r")
	if s := h.Screen(); !strings.Contains(s, "LeetCode 가 지금 이 요청을 막고 있어요 · o 브라우저에서 제출") {
		t.Fatalf("no blocked hint:\n%s", s)
	}
	h.Advance(30 * time.Second)
	if n := len(sent(e, "interpret_solution")); n != 1 {
		t.Fatalf("retried: %d", n)
	}
}

func TestAuthExpiredReopensLogin(t *testing.T) {
	e, h := judgeReady(t)
	e.lc.Fail("/problems/"+slug1606+"/interpret_solution/", http.StatusForbidden, "application/json", `{"error":"forbidden"}`)
	h.Press("r")
	if s := h.Screen(); !strings.Contains(s, "LEETCODE_SESSION") {
		t.Fatalf("no login dialog:\n%s", s)
	}
}

func TestResultGoldens(t *testing.T) {
	t.Run("run", func(t *testing.T) {
		e := setup(t)
		e.lc.SetDailyDate("2026-11-05")
		uitest.ForEachSize(t, leetSizes, func(t *testing.T, h *uitest.Harness) {
			signIn(t, e, h)
			e.lc.QueueCheck(runID, "check_run_success.json")
			openToday(t, h)
			h.Press("r")
			h.Advance(time.Second)
			if h.Context().Mode == 2 { // widget: the result tab
				h.Press("3")
			}
			h.Check()
			h.Golden("leet_result_run")
		})
	})
	t.Run("submit", func(t *testing.T) {
		e := setup(t)
		e.lc.SetDailyDate("2026-11-05")
		uitest.ForEachSize(t, leetSizes, func(t *testing.T, h *uitest.Harness) {
			signIn(t, e, h)
			e.lc.QueueCheck(submitID, "check_wrong_answer.json")
			openToday(t, h)
			h.Press("s")
			h.Advance(time.Second)
			if h.Context().Mode == 2 {
				h.Press("3")
			}
			h.Check()
			h.Golden("leet_result_submit")
		})
	})
}

// A 429 waits 10 seconds even for a premium account (paced 1s otherwise).
func TestRateLimitedWaitsTenSecondsPremium(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	h := uitest.New(t)
	signIn(t, e, h)
	openToday(t, h)
	leet.SetLoginAccountForTest(h.Context(), "sam_lc", true)
	if d := leet.PacerIntervalForTest(h.Context()); d != time.Second {
		t.Fatalf("pacer = %v, want premium 1s", d)
	}
	e.lc.Fail("/problems/"+slug1606+"/interpret_solution/", http.StatusTooManyRequests, "application/json", `{}`)
	h.Press("r")
	if s := h.Screen(); !strings.Contains(s, "10초") {
		t.Fatalf("no 10s wait shown:\n%s", s)
	}
	h.Advance(2 * time.Second)
	h.Press("r")
	if n := len(sent(e, "interpret_solution")); n != 1 {
		t.Fatalf("interpret requests = %d, want 1 (still waiting after a 429)", n)
	}
	h.Advance(9 * time.Second)
	h.Press("r")
	if n := len(sent(e, "interpret_solution")); n != 2 {
		t.Fatalf("interpret requests = %d, want 2 after 10s", n)
	}
}

// When today's problem rolls over at UTC midnight, the editor on yesterday's
// file is closed: r never sends another file than the one being edited.
func TestRolloverDropsEditor(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	h := uitest.New(t)
	signIn(t, e, h)
	h.Advance(21*time.Hour + 54*time.Minute + 30*time.Second) // 23:59:30Z
	openToday(t, h)
	h.Press("e", "i")
	h.Type("// MINE")
	h.Press("esc", ":", "w", "enter")
	e.lc.SetDailyDate("2026-11-06")
	h.Advance(time.Minute)
	if s := h.Screen(); !strings.Contains(s, "2026-11-06 (UTC)") {
		t.Fatalf("not rolled over:\n%s", s)
	}
	if s := h.Screen(); strings.Contains(s, "// MINE") {
		t.Fatalf("yesterday's file still in the editor after the rollover:\n%s", s)
	}
	h.Press("r")
	reqs := sent(e, "interpret_solution")
	if len(reqs) != 1 {
		t.Fatalf("interpret requests = %d:\n%s", len(reqs), h.Screen())
	}
	var body struct {
		TypedCode string `json:"typed_code"`
	}
	if err := json.Unmarshal([]byte(reqs[0].Body), &body); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body.TypedCode, "MINE") {
		t.Fatalf("sent yesterday's code for today's problem")
	}
}
