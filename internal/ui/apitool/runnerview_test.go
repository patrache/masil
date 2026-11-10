package apitool

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/ui/uitest"
)

// runRequests are three requests to srv: the second's check fails (it wants
// 200, created() answers 201).
func runRequests(srv *httptest.Server) []*apitool.Request {
	pay := paymentRequest(srv)
	bad := paymentRequest(srv)
	bad.ID, bad.Name = "bad", "실패하는 검사"
	bad.Asserts = []apitool.Assertion{{ID: "s", Target: "status", Op: "eq", Value: "200"}}
	third := paymentRequest(srv)
	third.ID, third.Name = "third", "세 번째"
	return []*apitool.Request{pay, bad, third}
}

// waitRun advances until the run ends.
func waitRun(t *testing.T, h *uitest.Harness, tl *Tool) {
	t.Helper()
	for i, start := 0, time.Now(); i < 400 || time.Since(start) < 10*time.Second; i++ {
		if tl.run != nil && tl.run.done {
			return
		}
		// the fake clock moves at most 20s (as before); after that only real time
		// passes, so slow machines (Windows CI) get up to 10s more for real I/O.
		if i < 400 {
			h.Advance(50 * time.Millisecond)
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("the run never ended:\n%s", h.Screen())
}

func TestRunnerDialogAndProgress(t *testing.T) {
	srv, hits := hitServer(t, created)
	h := openAPI(t)
	tl := seedRequests(h, nil, runRequests(srv)...)
	h.Press("r")
	if !strings.Contains(overlayText(h), ko.RunTitle()) {
		t.Fatalf("r did not open the runner dialog:\n%s", h.Screen())
	}
	h.Press("j") // iterations
	h.Press("backspace")
	h.Type("2")
	h.Press("enter")
	waitRun(t, h, tl)
	if len(tl.run.rows) != 6 || hits.Load() != 6 {
		t.Fatalf("rows = %d, hits = %d, want 6", len(tl.run.rows), hits.Load())
	}
	s := overlayText(h)
	if !strings.Contains(s, ko.RunProgress(6, 6, 4, 2)) || strings.Count(s, "결제 만들기") != 2 {
		t.Fatalf("progress table:\n%s", h.Screen())
	}
	h.Check()
	rows, err := tl.hist.List(10)
	if err != nil {
		t.Fatal(err)
	}
	runs := 0
	for _, e := range rows {
		if e.Kind == apitool.HistoryRun {
			runs++
			if e.Run.Requests != 6 || e.Run.Failed != 2 {
				t.Fatalf("run summary = %+v", e.Run)
			}
		}
	}
	if runs != 1 {
		t.Fatalf("history runs = %d, want 1", runs)
	}
}

func TestRunnerFailRowOpensResponse(t *testing.T) {
	srv, _ := hitServer(t, created)
	h := openAPI(t)
	tl := seedRequests(h, nil, runRequests(srv)...)
	h.Press("r", "enter")
	waitRun(t, h, tl)
	if r := tl.run.rows[tl.run.sel]; !r.failed {
		t.Fatalf("the selection is not on the failed row: %+v", r)
	}
	h.Press("enter")
	if tl.req == nil || tl.req.ID != "bad" || tl.focus != paneResponse {
		t.Fatalf("enter did not open the failed response: req=%v focus=%v", tl.req, tl.focus)
	}
	if s := h.Screen(); !strings.Contains(s, "201 Created") || !strings.Contains(s, "✕") {
		t.Fatalf("the failed response is not shown:\n%s", s)
	}
}

func TestRunnerSharedHostPreview(t *testing.T) {
	srv, hits := hitServer(t, created)
	h := openAPI(t)
	tl := seedRequests(h, nil, runRequests(srv)...)
	tl.shared[tl.colName] = true
	h.Press("r", "enter")
	s := overlayText(h)
	if !strings.Contains(s, ko.RunHostsTitle()) || !strings.Contains(s, "127.0.0.1") {
		t.Fatalf("no host list before a shared run:\n%s", h.Screen())
	}
	h.Press("down", "enter") // 취소
	h.Advance(time.Second)
	if hits.Load() != 0 || tl.run != nil {
		t.Fatalf("canceled preview sent %d requests", hits.Load())
	}
	h.Press("r", "enter", "enter")
	waitRun(t, h, tl)
	if hits.Load() != 3 {
		t.Fatalf("confirmed run hits = %d", hits.Load())
	}
}

func TestTinyResendAndRun(t *testing.T) {
	srv, hits := hitServer(t, created)
	h := openAPI(t)
	tl := seedRequests(h, nil, runRequests(srv)...)
	h.Resize(uitest.Tiny.W, uitest.Tiny.H)
	if tl.mode(h.Context()) != vmTiny {
		t.Fatalf("40x14 is not tiny")
	}
	h.Press("enter")
	waitScreen(t, h, "201 ·")
	if hits.Load() != 1 {
		t.Fatalf("resend hits = %d", hits.Load())
	}
	h.Press("r")
	waitRun(t, h, tl)
	waitScreen(t, h, ko.RunProgressShort(3, 3, 2, 1))
	h.Press("a")
	if !strings.Contains(h.Screen(), ko.NeedBigger()) {
		t.Fatalf("an edit key in tiny did not ask for a bigger window:\n%s", h.Screen())
	}
	h.Check()
}
