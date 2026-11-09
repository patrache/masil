package leetcode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrNotSignedIn: a run, submit or check without cookies; nothing was sent.
var ErrNotSignedIn = errors.New("leetcode: not signed in")

// judgeIDRe is a run or submission id as LeetCode returns it.
var judgeIDRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,80}$`)

// CheckResult is one GET /submissions/detail/<id>/check/ answer. Done =
// State == "SUCCESS" (the judge finished; StatusMsg says how). Text fields are
// LeetCode's own (not translated) and must be sanitized before drawing.
type CheckResult struct {
	State                        string
	Done                         bool
	StatusMsg                    string // "Accepted", "Wrong Answer" …
	CodeAnswer, ExpectedAnswer   []string
	TotalCorrect, TotalTestcases int
	RuntimePercentile            *float64
	StatusRuntime                string
	LastTestcase                 string
	ExpectedOutput, CodeOutput   string
	CompileError, RuntimeError   string
}

// judgeBody is the interpret / submit request body.
type judgeBody struct {
	Lang       string  `json:"lang"`
	QuestionID string  `json:"question_id"`
	TypedCode  string  `json:"typed_code"`
	DataInput  *string `json:"data_input,omitempty"`
}

// judgeable checks everything before a judge request: cookies, a usable
// problem (not premium) and a known language.
func (c *Client) judgeable(q *Question, lang string) error {
	if !c.signedIn() {
		return ErrNotSignedIn
	}
	if q == nil || !ValidSlug(q.Slug) || q.QuestionID == "" {
		return fmt.Errorf("%w: no usable problem", ErrBadSlug)
	}
	if q.IsPaidOnly {
		return ErrPremiumOnly
	}
	if _, ok := LangBySlug(lang); !ok {
		return fmt.Errorf("leetcode: unknown language %q", lang)
	}
	return nil
}

// Interpret runs code on LeetCode against the problem's examples (POST
// /problems/<slug>/interpret_solution/). Poll the result with Check(runID).
func (c *Client) Interpret(ctx context.Context, q *Question, lang, code string) (runID string, err error) {
	if err := c.judgeable(q, lang); err != nil {
		return "", err
	}
	input := strings.Join(q.ExampleTestcases, "\n")
	var out struct {
		InterpretID string `json:"interpret_id"`
	}
	body := judgeBody{Lang: lang, QuestionID: q.QuestionID, TypedCode: code, DataInput: &input}
	if err := c.do(ctx, "POST", "/problems/"+q.Slug+"/interpret_solution/", q.Slug, body, &out); err != nil {
		return "", err
	}
	if !judgeIDRe.MatchString(out.InterpretID) {
		return "", fmt.Errorf("%w: interpret id %q", ErrUnexpected, out.InterpretID)
	}
	return out.InterpretID, nil
}

// Submit submits code (POST /problems/<slug>/submit/). Poll the result with
// Check(submissionID).
func (c *Client) Submit(ctx context.Context, q *Question, lang, code string) (submissionID string, err error) {
	if err := c.judgeable(q, lang); err != nil {
		return "", err
	}
	var out struct {
		SubmissionID json.Number `json:"submission_id"`
	}
	body := judgeBody{Lang: lang, QuestionID: q.QuestionID, TypedCode: code}
	if err := c.do(ctx, "POST", "/problems/"+q.Slug+"/submit/", q.Slug, body, &out); err != nil {
		return "", err
	}
	id := out.SubmissionID.String()
	if !judgeIDRe.MatchString(id) {
		return "", fmt.Errorf("%w: submission id %q", ErrUnexpected, id)
	}
	return id, nil
}

// Check reads a run's or a submission's state once (GET
// /submissions/detail/<id>/check/). It never waits or polls: the UI calls it
// again after a ui.After while Done is false.
func (c *Client) Check(ctx context.Context, id string) (*CheckResult, error) {
	if !c.signedIn() {
		return nil, ErrNotSignedIn
	}
	if !judgeIDRe.MatchString(id) {
		return nil, fmt.Errorf("%w: id %q", ErrUnexpected, id)
	}
	var w struct {
		State              string          `json:"state"`
		StatusMsg          string          `json:"status_msg"`
		CodeAnswer         []string        `json:"code_answer"`
		ExpectedCodeAnswer []string        `json:"expected_code_answer"`
		TotalCorrect       *int            `json:"total_correct"`
		TotalTestcases     *int            `json:"total_testcases"`
		RuntimePercentile  *float64        `json:"runtime_percentile"`
		StatusRuntime      string          `json:"status_runtime"`
		LastTestcase       string          `json:"last_testcase"`
		ExpectedOutput     string          `json:"expected_output"`
		CodeOutput         json.RawMessage `json:"code_output"`
		FullCompileError   string          `json:"full_compile_error"`
		CompileError       string          `json:"compile_error"`
		FullRuntimeError   string          `json:"full_runtime_error"`
		RuntimeError       string          `json:"runtime_error"`
	}
	if err := c.do(ctx, "GET", "/submissions/detail/"+id+"/check/", "", nil, &w); err != nil {
		return nil, err
	}
	r := &CheckResult{
		State: w.State, Done: w.State == "SUCCESS", StatusMsg: w.StatusMsg,
		CodeAnswer: w.CodeAnswer, ExpectedAnswer: w.ExpectedCodeAnswer, RuntimePercentile: w.RuntimePercentile,
		StatusRuntime: w.StatusRuntime, LastTestcase: w.LastTestcase, ExpectedOutput: w.ExpectedOutput,
		CodeOutput: textOrLines(w.CodeOutput), CompileError: firstNonEmpty(w.FullCompileError, w.CompileError),
		RuntimeError: firstNonEmpty(w.FullRuntimeError, w.RuntimeError),
	}
	if w.TotalCorrect != nil {
		r.TotalCorrect = *w.TotalCorrect
	}
	if w.TotalTestcases != nil {
		r.TotalTestcases = *w.TotalTestcases
	}
	return r, nil
}

// textOrLines reads code_output: a string for a submission, the stdout lines
// for a run.
func textOrLines(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var lines []string
	if json.Unmarshal(raw, &lines) == nil {
		return strings.Join(lines, "\n")
	}
	return ""
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
