package leetcode_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/leetcode"
	"github.com/patrache/masil/internal/leetcode/leetcodetest"
)

func daily1606(t *testing.T, srv *leetcodetest.Server) *leetcode.Question {
	t.Helper()
	q, _, err := newClient(srv, nil, nil).Daily(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func TestInterpretBody(t *testing.T) {
	srv := leetcodetest.New(t)
	q := daily1606(t, srv)
	id, err := newClient(srv, testCreds(), nil).Interpret(t.Context(), q, "golang", "func busiestServers() {}")
	if err != nil {
		t.Fatal(err)
	}
	if id != "runcode_1762650000.123456_r1" {
		t.Fatalf("run id %q", id)
	}
	reqs := srv.Requests()
	r := reqs[len(reqs)-1]
	if r.Method != "POST" || r.Path != "/problems/find-servers-that-handled-most-number-of-requests/interpret_solution/" {
		t.Fatalf("request %s %s", r.Method, r.Path)
	}
	if r.Header.Get("x-csrftoken") != "csrf-SECRET" || !strings.HasSuffix(r.Header.Get("Referer"), "/problems/find-servers-that-handled-most-number-of-requests/") {
		t.Fatalf("headers %v", r.Header)
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(r.Body), &body); err != nil {
		t.Fatal(err)
	}
	if body["question_id"] != "1710" || body["lang"] != "golang" || body["typed_code"] != "func busiestServers() {}" {
		t.Fatalf("body %v", body)
	}
	if lines := strings.Split(body["data_input"], "\n"); body["data_input"] != strings.Join(q.ExampleTestcases, "\n") || len(lines) != 9 {
		t.Fatalf("data_input %q", body["data_input"])
	}
}

func TestCheckPendingThenSuccess(t *testing.T) {
	srv := leetcodetest.New(t)
	srv.QueueCheck("r1", "check_pending.json", "check_run_success.json")
	c := newClient(srv, testCreds(), nil)
	first, err := c.Check(t.Context(), "r1")
	if err != nil || first.Done || first.State != "PENDING" {
		t.Fatalf("first = %+v, %v", first, err)
	}
	second, err := c.Check(t.Context(), "r1")
	if err != nil || !second.Done || second.StatusMsg != "Accepted" {
		t.Fatalf("second = %+v, %v", second, err)
	}
	if len(second.CodeAnswer) != 3 || len(second.ExpectedAnswer) != 3 || second.CodeAnswer[2] != "[0,1,2]" || second.TotalCorrect != 3 {
		t.Fatalf("answers %+v", second)
	}
	if second.RuntimePercentile != nil {
		t.Fatalf("percentile %v", *second.RuntimePercentile)
	}
	reqs := srv.Requests()
	if r := reqs[len(reqs)-1]; r.Method != "GET" || r.Path != "/submissions/detail/r1/check/" {
		t.Fatalf("request %s %s", r.Method, r.Path)
	}
	// the last fixture repeats once the queue is drained
	if again, err := c.Check(t.Context(), "r1"); err != nil || !again.Done {
		t.Fatalf("again = %+v, %v", again, err)
	}
	if _, err := c.Check(t.Context(), "../../x"); err == nil {
		t.Fatal("hostile id accepted")
	}
}

func TestSubmitAccepted(t *testing.T) {
	srv := leetcodetest.New(t)
	q := daily1606(t, srv)
	c := newClient(srv, testCreds(), nil)
	id, err := c.Submit(t.Context(), q, "golang", "code")
	if err != nil || id != "1234567890" {
		t.Fatalf("submit = %q, %v", id, err)
	}
	reqs := srv.Requests()
	r := reqs[len(reqs)-1]
	if r.Path != "/problems/find-servers-that-handled-most-number-of-requests/submit/" {
		t.Fatalf("path %s", r.Path)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(r.Body), &body)
	if body["question_id"] != "1710" || body["typed_code"] != "code" || body["lang"] != "golang" {
		t.Fatalf("body %v", body)
	}
	srv.QueueCheck(id, "check_submit_accepted.json")
	res, err := c.Check(t.Context(), id)
	if err != nil || !res.Done || res.StatusMsg != "Accepted" || res.RuntimePercentile == nil || *res.RuntimePercentile != 85.31 || res.StatusRuntime != "183 ms" {
		t.Fatalf("check = %+v, %v", res, err)
	}
}

func TestSubmitWrongAnswer(t *testing.T) {
	srv := leetcodetest.New(t)
	srv.QueueCheck("1234567890", "check_wrong_answer.json")
	res, err := newClient(srv, testCreds(), nil).Check(t.Context(), "1234567890")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Done || res.StatusMsg != "Wrong Answer" || res.TotalCorrect != 57 || res.TotalTestcases != 63 {
		t.Fatalf("res = %+v", res)
	}
	if res.LastTestcase != "3\n[1,2,3,4]\n[1,2,1,2]" || res.ExpectedOutput != "[0,1,2]" || res.CodeOutput != "[1]" {
		t.Fatalf("res = %+v", res)
	}
}

func TestNoCredsNoRequest(t *testing.T) {
	srv := leetcodetest.New(t)
	q := daily1606(t, srv)
	before := len(srv.Requests())
	c := newClient(srv, nil, nil)
	if _, err := c.Interpret(t.Context(), q, "golang", "x"); !errors.Is(err, leetcode.ErrNotSignedIn) {
		t.Fatalf("interpret: %v", err)
	}
	if _, err := c.Submit(t.Context(), q, "golang", "x"); !errors.Is(err, leetcode.ErrNotSignedIn) {
		t.Fatalf("submit: %v", err)
	}
	if _, err := c.Check(t.Context(), "r1"); !errors.Is(err, leetcode.ErrNotSignedIn) {
		t.Fatalf("check: %v", err)
	}
	if n := len(srv.Requests()) - before; n != 0 {
		t.Fatalf("%d requests sent without cookies", n)
	}
	// a premium problem or an unknown language is never sent either
	signed := newClient(srv, testCreds(), nil)
	if _, err := signed.Interpret(t.Context(), &leetcode.Question{QuestionID: "253", FrontendID: "253", Slug: "meeting-rooms-ii", IsPaidOnly: true}, "golang", "x"); !errors.Is(err, leetcode.ErrPremiumOnly) {
		t.Fatalf("premium: %v", err)
	}
	if _, err := signed.Submit(t.Context(), q, "cobol", "x"); err == nil {
		t.Fatal("unknown language accepted")
	}
	if _, err := signed.Submit(t.Context(), &leetcode.Question{QuestionID: "1", Slug: "../x"}, "golang", "x"); !errors.Is(err, leetcode.ErrBadSlug) {
		t.Fatalf("hostile slug: %v", err)
	}
	if n := len(srv.Requests()) - before; n != 0 {
		t.Fatalf("%d requests sent", n)
	}
}

func TestJudgeErrorsClassified(t *testing.T) {
	srv := leetcodetest.New(t)
	q := daily1606(t, srv)
	srv.Fail("/problems/"+q.Slug+"/submit/", 429, "application/json", `{}`)
	if _, err := newClient(srv, testCreds(), nil).Submit(t.Context(), q, "golang", "x"); !errors.Is(err, leetcode.ErrRateLimited) {
		t.Fatalf("429: %v", err)
	}
	srv.Fail("/problems/"+q.Slug+"/interpret_solution/", 403, "text/html", "<html>cf</html>")
	if _, err := newClient(srv, testCreds(), nil).Interpret(t.Context(), q, "golang", "x"); !errors.Is(err, leetcode.ErrBlocked) {
		t.Fatalf("html: %v", err)
	}
	srv.Fail("/submissions/detail/9/check/", 403, "application/json", `{"detail":"no"}`)
	if _, err := newClient(srv, testCreds(), nil).Check(t.Context(), "9"); !errors.Is(err, leetcode.ErrAuthExpired) {
		t.Fatalf("403: %v", err)
	}
}

func TestPacer(t *testing.T) {
	t0 := time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)
	p := leetcode.NewPacer(false)
	if p.Interval != 10*time.Second || p.Remaining(t0) != 0 {
		t.Fatalf("fresh free pacer: %v %v", p.Interval, p.Remaining(t0))
	}
	p.Mark(t0)
	if got := p.Remaining(t0.Add(3 * time.Second)); got != 7*time.Second {
		t.Fatalf("after 3s: %v", got)
	}
	if got := p.Remaining(t0.Add(10 * time.Second)); got != 0 {
		t.Fatalf("after 10s: %v", got)
	}
	if got := p.Remaining(t0.Add(-time.Minute)); got != 10*time.Second {
		t.Fatalf("clock went back: %v", got)
	}
	pp := leetcode.NewPacer(true)
	pp.Mark(t0)
	if pp.Interval != time.Second || pp.Remaining(t0.Add(500*time.Millisecond)) != 500*time.Millisecond || pp.Remaining(t0.Add(time.Second)) != 0 {
		t.Fatalf("premium pacer %+v", pp)
	}
}
