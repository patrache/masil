package leet_test

import (
	"time"

	"net/http"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/secrets/secrettest"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/leet"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

const cookieWarning = "이 값은 LeetCode 계정 전체 권한이에요. 이 PC(키체인)에만 저장하고 masil 서버로는 보내지 않아요."

// hasValue reports whether any stored secret equals v.
func hasValue(vals map[string]string, v string) bool {
	for _, x := range vals {
		if x == v {
			return true
		}
	}
	return false
}

func TestLoginMasksInput(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	e.lc.SetUserStatus(true, "sam_lc", false)
	e.lc.QueueCheck(runID, "check_run_success.json")
	st := secrettest.NewStore(t, true)
	h := uitest.New(t, uitest.WithSecrets(st))
	openToday(t, h)
	h.Press("r") // no cookies yet: the login dialog
	if s := h.Screen(); !strings.Contains(s, "LEETCODE_SESSION") || !strings.Contains(overlayText(t, h), compact(cookieWarning)) {
		t.Fatalf("no login dialog with its warning:\n%s", s)
	}
	h.Paste(" LEETCODE_SESSION=sess-SECRET-abcd;\n")
	h.Press("tab")
	h.Paste("csrftoken=csrf-XYZ-9876")
	s := h.Screen()
	if strings.Contains(s, "sess-SECRET") || strings.Contains(s, "csrf-XYZ") {
		t.Fatalf("a cookie on screen:\n%s", s)
	}
	if !strings.Contains(s, "••••abcd") || !strings.Contains(s, "••••9876") {
		t.Fatalf("no masks:\n%s", s)
	}
	h.Press("enter")
	vals := secrettest.Values(st)
	if !hasValue(vals, "sess-SECRET-abcd") || !hasValue(vals, "csrf-XYZ-9876") {
		t.Fatalf("stored = %v", vals)
	}
	if s := h.Screen(); strings.Contains(s, "sess-SECRET") {
		t.Fatalf("a cookie on screen:\n%s", s)
	}
	// the run that asked for the login goes on
	if n := len(sent(e, "interpret_solution")); n != 1 {
		t.Fatalf("interpret after login = %d", n)
	}
	var stt struct {
		Accounts map[string]struct {
			Username string `json:"username"`
		} `json:"accounts"`
	}
	h.Context().ToolState.Section("leet", &stt)
	if len(stt.Accounts) != 1 {
		t.Fatalf("accounts = %+v", stt.Accounts)
	}
	for _, a := range stt.Accounts {
		if a.Username != "sam_lc" {
			t.Fatalf("username = %q", a.Username)
		}
	}
}

func TestLoginRejectedStaysOpen(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	st := secrettest.NewStore(t, true)
	h := uitest.New(t, uitest.WithSecrets(st))
	openToday(t, h)
	h.Press("r")
	h.Paste("sess-bad-1111")
	h.Press("tab")
	h.Paste("csrf-bad-2222")
	h.Press("enter") // the fake signs nobody in
	if s := h.Screen(); !strings.Contains(s, "LEETCODE_SESSION") {
		t.Fatalf("dialog closed:\n%s", s)
	}
	if vals := secrettest.Values(st); len(vals) != 0 {
		t.Fatalf("stored rejected cookies: %v", vals)
	}
}

func TestLoginConsentWhenNoKeychain(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	e.lc.SetUserStatus(true, "sam_lc", false)
	e.lc.QueueCheck(runID, "check_run_success.json")
	st := secrettest.NewStore(t, false)
	h := uitest.New(t, uitest.WithSecrets(st))
	openToday(t, h)
	h.Press("r")
	h.Paste("sess-SECRET-abcd")
	h.Press("tab")
	h.Paste("csrf-XYZ-9876")
	h.Press("enter")
	if s := h.Screen(); !strings.Contains(s, "이번 실행") {
		t.Fatalf("no consent dialog:\n%s", s)
	}
	h.Press("enter") // the default choice: this run only
	if n := len(sent(e, "interpret_solution")); n != 1 {
		t.Fatalf("interpret after consent = %d:\n%s", n, h.Screen())
	}
	// a new run of masil (a new store, nothing on disk) asks again
	st2 := secrettest.NewStore(t, false)
	h2 := uitest.New(t, uitest.WithSecrets(st2))
	h2.Send(ui.CloseToolMsg{InstanceID: "leet:today"})
	openToday(t, h2)
	h2.Press("r")
	if s := h2.Screen(); !strings.Contains(s, "LEETCODE_SESSION") {
		t.Fatalf("no login after a restart:\n%s", s)
	}
}

func TestToolLoginForget(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	st := secrettest.NewStore(t, true)
	h := uitest.New(t, uitest.WithSecrets(st))
	signIn(t, e, h)
	firstRunDone(t, h)
	leet.SetLoginAccountForTest(h.Context(), "sam_lc", false)
	h.API.JSON("GET /v1/me/settings", http.StatusOK, map[string]any{"settings": map[string]any{"dm_policy": "friends_and_spaces", "focus_default_min": 25}})
	h.API.JSON("GET /v1/blocks", http.StatusOK, map[string]any{"items": []any{}})
	h.Resize(200, 60)
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteSettings, Anchor: "tools"}})
	if !strings.Contains(h.Screen(), "오늘의 문제 · sam_lc") {
		t.Fatalf("no tool login row:\n%s", h.Screen())
	}
	for range 80 {
		if sel := selectedRow(h); strings.Contains(sel, "오늘의 문제 · sam_lc") {
			break
		}
		h.Press("j")
	}
	h.Press("enter", "y")
	for k := range secrettest.Values(st) {
		if strings.HasPrefix(k, "leet/") {
			t.Fatalf("leet value left: %s", k)
		}
	}
	var stt struct {
		Accounts map[string]any `json:"accounts"`
	}
	h.Context().ToolState.Section("leet", &stt)
	if len(stt.Accounts) != 0 {
		t.Fatalf("username kept: %v", stt.Accounts)
	}
	if name, ok := leet.DescribeLoginForTest(h.Context()); ok {
		t.Fatalf("still signed in as %q", name)
	}
}

// selectedRow is the screen line with the selection marker "› ".
func selectedRow(h *uitest.Harness) string {
	for _, l := range strings.Split(h.Screen(), "\n") {
		if strings.Contains(l, "› ") {
			return l
		}
	}
	return ""
}

func TestLoginGolden(t *testing.T) {
	setup(t)
	uitest.ForEachSize(t, leetSizes, func(t *testing.T, h *uitest.Harness) {
		h.Send(ui.OpenOverlayMsg{ID: leet.OverlayLogin})
		if !strings.Contains(overlayText(t, h), compact(cookieWarning)) {
			t.Fatalf("warning cut:\n%s", h.Screen())
		}
		h.Check()
		h.Golden("leet_login")
	})
}

// switchAccount makes h's masil account another user (the cookie namespace
// and the tool's account memory follow it).
func switchAccount(h *uitest.Harness, userID string) {
	me := *h.Context().Store.Me
	me.ID = userID
	h.Context().Store.Me = &me
}

// loginViaDialog signs in through the dialog (r opens it).
func loginViaDialog(h *uitest.Harness) {
	h.Press("r")
	h.Paste("sess-SECRET-abcd")
	h.Press("tab")
	h.Paste("csrf-XYZ-9876")
	h.Press("enter")
}

// A "this run only" login is not shown as signed in after a restart: the
// settings row follows where the session cookie is kept.
func TestLoginRowAfterRunOnlyLogin(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	e.lc.SetUserStatus(true, "sam_lc", false)
	e.lc.QueueCheck(runID, "check_run_success.json")
	h := uitest.New(t, uitest.WithSecrets(secrettest.NewStore(t, false)))
	openToday(t, h)
	loginViaDialog(h)
	h.Press("enter") // consent: this run only
	if name, ok := leet.DescribeLoginForTest(h.Context()); !ok || name != "sam_lc" {
		t.Fatalf("describe after login = %q %v", name, ok)
	}
	// a restart: this run's memory and the memory-only store are gone
	leet.ForgetRunForTest()
	h.Context().Secrets = secrettest.NewStore(t, false)
	if name, ok := leet.DescribeLoginForTest(h.Context()); ok {
		t.Fatalf("signed in after a restart without cookies: %q", name)
	}
}

// The LeetCode user and premium belong to the masil account: another masil
// account on the same device is not signed in, its [지우기] does not touch
// the first one, and it is paced as a free account.
func TestLoginPerMasilAccount(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	e.lc.SetUserStatus(true, "sam_lc", true) // premium
	e.lc.QueueCheck(runID, "check_run_success.json")
	h := uitest.New(t, uitest.WithSecrets(secrettest.NewStore(t, true)))
	openToday(t, h)
	loginViaDialog(h)
	if name, ok := leet.DescribeLoginForTest(h.Context()); !ok || name != "sam_lc" {
		t.Fatalf("A describe = %q %v", name, ok)
	}
	if d := leet.PacerIntervalForTest(h.Context()); d != time.Second {
		t.Fatalf("A pacer = %v, want 1s (premium)", d)
	}
	leet.ForgetRunForTest() // the next start of masil, signed in as B
	switchAccount(h, "01JDB0000000000000000USERB")
	if name, ok := leet.DescribeLoginForTest(h.Context()); ok {
		t.Fatalf("B shown signed in as %q", name)
	}
	if d := leet.PacerIntervalForTest(h.Context()); d != 10*time.Second {
		t.Fatalf("B pacer = %v, want 10s (free)", d)
	}
	leet.ForgetLoginForTest(h.Context()) // B's [지우기]
	switchAccount(h, fixtures.UserSam)
	if name, ok := leet.DescribeLoginForTest(h.Context()); !ok || name != "sam_lc" {
		t.Fatalf("A after B's forget = %q %v", name, ok)
	}
}
