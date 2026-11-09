package app_test

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// fakePanes makes termhost.Running return running and records CloseAll.
func fakePanes(t *testing.T, running []string) *[]time.Duration {
	t.Helper()
	var closed []time.Duration
	t.Cleanup(app.SetPanesForTest(func() []string { return running }, func(g time.Duration) { closed = append(closed, g) }))
	return &closed
}

// screenHas reports every fragment on screen (a dialog body wraps).
func screenHas(h *uitest.Harness, frags ...string) bool {
	sc := h.Screen()
	for _, f := range frags {
		if !strings.Contains(sc, f) {
			return false
		}
	}
	return true
}

func TestQuitWithLivePanesConfirms(t *testing.T) {
	closed := fakePanes(t, []string{"lazygit", "nvim", "lazygit"})
	h := uitest.New(t)
	h.Press("ctrl+c", "ctrl+c")
	if quitSeen(h) {
		t.Fatal("quit without asking while panes run")
	}
	if !screenHas(h, "lazygit 2개, nvim 1개가 실행 중이에요.", "끌까요?", "아니오") {
		t.Fatalf("no quit confirmation:\n%s", h.Screen())
	}
	h.Press("ctrl+c", "ctrl+c") // asking already: no second question on top
	if n := len(h.Model.Frame().Overlays); n != 1 || quitSeen(h) {
		t.Fatalf("ctrl+c twice over the question: %d overlays, quit %v", n, quitSeen(h))
	}
	h.Press("enter") // the default button is 아니오
	if quitSeen(h) || len(*closed) != 0 || len(h.Model.Frame().Overlays) != 0 {
		t.Fatalf("enter on the default quit: quit %v closed %v overlays %v", quitSeen(h), *closed, overlayTypes(h.Model))
	}
	h.Press("ctrl+c", "ctrl+c", "y")
	if !quitSeen(h) || !reflect.DeepEqual(*closed, []time.Duration{2 * time.Second}) {
		t.Fatalf("yes: quit %v closed %v", quitSeen(h), *closed)
	}
}

func TestQuitListSanitized(t *testing.T) {
	h := uitest.New(t)
	got := app.PaneListForTest(h.Model, []string{"lazy\u202egit\x1b[31m", "nvim"})
	if !strings.Contains(got, "nvim 1개") || strings.ContainsAny(got, "\u202e\x1b") {
		t.Fatalf("quit list = %q: the raw program name", got)
	}
}

func TestQuitListFormat(t *testing.T) {
	fakePanes(t, []string{"lazygit", "nvim"})
	h := uitest.New(t)
	runCmd(h, "quit") // the palette's 종료 goes the same way
	if !screenHas(h, "lazygit 1개, nvim 1개가 실행 중이에요.", "끌까요?") {
		t.Fatalf("palette quit: no pane confirmation:\n%s", h.Screen())
	}
	h.Press("n")
	if quitSeen(h) {
		t.Fatal("n quit")
	}
}

func TestQuitWithoutPanesUnchanged(t *testing.T) {
	closed := fakePanes(t, nil)
	h := uitest.New(t)
	h.Press("ctrl+c")
	if quitSeen(h) || !strings.Contains(h.Screen(), "한 번 더 누르면 종료해요") {
		t.Fatal("the first ctrl+c did not arm")
	}
	h.Press("ctrl+c")
	if !quitSeen(h) || len(*closed) != 0 {
		t.Fatalf("quit %v closed %v", quitSeen(h), *closed)
	}
	h2 := uitest.New(t)
	runCmd(h2, "quit")
	if got := overlayTypes(h2.Model); !reflect.DeepEqual(got, []string{"*widgets.confirmDialog"}) || !strings.Contains(h2.Screen(), "종료할까요?") {
		t.Fatalf("palette quit without panes: %v", got)
	}
	h2.Press("y")
	if !quitSeen(h2) {
		t.Fatal("palette quit: y did not quit")
	}
}

func TestNestedPaneWarns(t *testing.T) {
	h := uitest.New(t, uitest.WithNestedPane())
	if !screenHas(h, "패널 안에서 masil 을 켰어요") {
		t.Fatalf("no nested warning:\n%s", h.Screen())
	}
	h.Advance(10 * time.Second)
	h.Resize(100, 40)
	h.Resize(120, 40)
	if screenHas(h, "패널 안에서 masil 을 켰어요") {
		t.Fatal("the warning came back")
	}
	h.Press("j") // nothing is blocked
	if h2 := uitest.New(t); screenHas(h2, "패널 안에서 masil 을 켰어요") {
		t.Fatal("warned outside a pane")
	}
}

// Run reads MASIL_PANE; a model built without it (the test harness) does not,
// so `go test` started inside a masil pane draws the same screens.
func TestNestedPaneEnvOnlyThroughRun(t *testing.T) {
	t.Setenv("MASIL_PANE", "1")
	if h := uitest.New(t); screenHas(h, "패널 안에서 masil 을 켰어요") {
		t.Fatal("the harness read MASIL_PANE from the process env")
	}
	if o := app.OptionsFromEnvForTest(app.Options{}); !o.NestedPane {
		t.Error("Run's options ignore MASIL_PANE=1")
	}
	t.Setenv("MASIL_PANE", "")
	if o := app.OptionsFromEnvForTest(app.Options{}); o.NestedPane {
		t.Error("NestedPane without MASIL_PANE")
	}
}

// The standard mode hides the chat behind the tool surface: the status line
// shows the unread count then (A8.3); without a tool it is the 0.1.2 line.
func TestStatusUnreadBehindTools(t *testing.T) {
	installStubTools(t, "all")
	status := func(h *uitest.Harness) string {
		lines := strings.Split(h.Screen(), "\n")
		return lines[len(lines)-1]
	}
	t.Setenv("MASIL_FEATURES", "")
	off := uitest.New(t)
	t.Setenv("MASIL_FEATURES", "all")
	h := uitest.New(t)
	if got, want := status(h), status(off); got != want {
		t.Fatalf("no tool open: status\n%q\nwant the flags-off\n%q", got, want)
	}
	openTool(h, ui.ToolGit, "git-1")
	unread, _ := h.Context().Store.TotalUnread()
	if s := status(h); !strings.Contains(s, "💬 안 읽음 "+strconv.Itoa(unread)) {
		t.Fatalf("status with a tool = %q (unread %d)", s, unread)
	}
	st := h.Context().Store
	minji := *st.User(fixtures.UserMinji)
	h.Event("message.created", realtime.MessageEvent{Message: api.Message{
		ID: "01JDGEN0000000000000000099", ChannelID: fixtures.ChGeneral, Author: minji,
		Kind: "text", Body: "점심 뭐 먹어요?", CreatedAt: h.Clock().Now(),
	}})
	if s := status(h); !strings.Contains(s, "💬 안 읽음 "+strconv.Itoa(unread+1)) {
		t.Fatalf("status after a message = %q", s)
	}
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteTools}}) // the hub in front: its own status line
	if s := status(h); strings.Contains(s, "💬") {
		t.Fatalf("status on the hub = %q", s)
	}
	h.Press("ctrl+t") // back to the chat: the count goes
	if s := status(h); strings.Contains(s, "💬") {
		t.Fatalf("status on the chat = %q", s)
	}
}
