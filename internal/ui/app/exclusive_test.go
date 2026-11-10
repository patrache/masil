package app

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/notify"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// syncBackend is a notify.Backend safe to read while the bridge goroutine shows.
type syncBackend struct {
	mu    sync.Mutex
	shown []notify.OSNotification
}

func (b *syncBackend) Show(n notify.OSNotification) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.shown = append(b.shown, n)
	return nil
}

func (b *syncBackend) all() []notify.OSNotification {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.shown)
}

func (b *syncBackend) waitN(t *testing.T, n int) []notify.OSNotification {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if got := b.all(); len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("OS notifications = %d, want %d", len(b.all()), n)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// execModel is a live bridge whose UI is "blocked" (the sender holds every msg
// until release) and whose notifier records into a syncBackend.
type execModel struct {
	m       *Model
	conn    *bridgeConn
	backend *syncBackend
	mu      sync.Mutex
	held    []tea.Msg
}

func newExecModel(t *testing.T) *execModel {
	t.Helper()
	m, _, _, _ := bridgeModel(t)
	x := &execModel{m: m, conn: newBridgeConn(), backend: &syncBackend{}}
	m.notifier = notify.New(x.backend, notify.OpenLedger(m.ctx.Paths), nil)
	m.send = func(msg tea.Msg) {
		x.mu.Lock()
		x.held = append(x.held, msg)
		x.mu.Unlock()
	}
	m.rtDial = func(ctx context.Context, url string, h http.Header) (realtime.Conn, error) { return x.conn, nil }
	m.startRealtime()
	x.conn.push(t, "hello", nil, helloData())
	x.conn.waitFrame(t, "resume")
	x.conn.push(t, "ready", nil, readyData(false, false))
	return x
}

// waitHeld waits until the held msgs include n EventMsgs of type typ.
func (x *execModel) waitHeld(t *testing.T, typ string, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		x.mu.Lock()
		c := 0
		for _, msg := range x.held {
			if e, ok := msg.(ui.EventMsg); ok && e.Event.Type == typ {
				c++
			}
		}
		x.mu.Unlock()
		if c >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("held %d %q events, want %d", c, typ, n)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// release hands every held msg to Update (the UI is back).
func (x *execModel) release(t *testing.T) {
	t.Helper()
	x.mu.Lock()
	held := x.held
	x.held = nil
	x.mu.Unlock()
	for _, msg := range held {
		drive(t, x.m, msg)
	}
}

// execStart makes termhost report an exec program owning the terminal.
func execStart(t *testing.T, on bool, req ...termhost.Request) {
	t.Helper()
	t.Cleanup(termhost.SetExclusiveForTest(on, req...))
}

// execEnd is the program giving the terminal back (its name stays readable).
func execEnd(t *testing.T) {
	t.Helper()
	t.Cleanup(termhost.SetExclusiveForTest(false))
}

func mentionEvent(id string) map[string]any {
	ch := fixtures.ChGeneral
	return map[string]any{"alert": api.Alert{
		ID: id, Kind: api.AlertMention, Title: "민지님의 멘션", Body: "봐 주세요",
		Target:    api.AlertTarget{Type: "message", ID: "M1", ChannelID: &ch, MessageID: new("M1")},
		CreatedAt: bridgeNow,
	}}
}

func messageEvent(id, author string) realtime.MessageEvent {
	return realtime.MessageEvent{Message: api.Message{
		ID: id, ChannelID: fixtures.ChGeneral, Kind: "text", Body: "안녕",
		Author: api.UserSummary{ID: author, Handle: "x"}, CreatedAt: bridgeNow,
	}}
}

func toastTexts(m *Model) []string {
	var out []string
	for _, it := range m.toasts.items {
		out = append(out, it.msg.Text)
	}
	return out
}

// While an exec program owns the terminal the UI is stuck, but a mention still
// pops an OS notification from the bridge goroutine; when the UI later handles
// the same alert the shared ledger keeps it at one.
func TestAlertPopsDuringExclusive(t *testing.T) {
	execStart(t, true, termhost.Request{Program: "lazygit", Title: "lazygit"})
	x := newExecModel(t)
	seq := int64(1)
	x.conn.push(t, "alert", &seq, mentionEvent("AX1"))
	got := x.backend.waitN(t, 1)
	if got[0].AlertID != "AX1" || got[0].Title != "민지님의 멘션" {
		t.Fatalf("OS notification = %+v", got[0])
	}
	x.waitHeld(t, "alert", 1)
	execEnd(t)
	x.release(t)
	time.Sleep(20 * time.Millisecond)
	if n := len(x.backend.all()); n != 1 {
		t.Fatalf("OS notifications after the UI handled the alert = %d, want 1 (ledger)", n)
	}
}

// Suppressed alerts and a disabled desktop setting stay quiet in exec mode too.
func TestExclusiveRespectsDecide(t *testing.T) {
	execStart(t, true)
	x := newExecModel(t)
	x.m.ctx.Config.Notify.Desktop = false
	x.m.excl.noteConfig(x.m.ctx)
	seq := int64(1)
	x.conn.push(t, "alert", &seq, mentionEvent("AX2"))
	x.waitHeld(t, "alert", 1)
	if n := len(x.backend.all()); n != 0 {
		t.Fatalf("desktop off: %d OS notifications", n)
	}
}

// After the exec program returns, one toast sums what arrived meanwhile:
// other people's messages (mine are not counted) and mentions.
func TestExclusiveTallyToast(t *testing.T) {
	execStart(t, true, termhost.Request{Program: "/opt/homebrew/bin/lazygit", Title: "lazygit"})
	x := newExecModel(t)
	for i, author := range []string{fixtures.UserMinji, fixtures.UserJunho, fixtures.UserSam, fixtures.UserMinji} {
		seq := int64(i + 1)
		x.conn.push(t, "message.created", &seq, messageEvent("01JDNEW000000000000000000"+string(rune('A'+i)), author))
	}
	seq := int64(5)
	x.conn.push(t, "alert", &seq, mentionEvent("AX3"))
	x.waitHeld(t, "message.created", 4)
	x.waitHeld(t, "alert", 1)
	execEnd(t)
	x.release(t)
	want := "lazygit 쓰는 동안 · 새 메시지 3 · 멘션 1"
	if n := countText(toastTexts(x.m), want); n != 1 {
		t.Fatalf("toasts = %q, want %q once", toastTexts(x.m), want)
	}
	// A later Update does not repeat it.
	drive(t, x.m, ui.FocusChangedMsg{})
	if n := countText(toastTexts(x.m), want); n != 1 {
		t.Fatalf("toast repeated: %q", toastTexts(x.m))
	}
}

func countText(ss []string, s string) int {
	n := 0
	for _, x := range ss {
		if x == s {
			n++
		}
	}
	return n
}

// Without a Title the toast names the program's base name; nothing counted =>
// no toast.
func TestExclusiveTallyTitleFallbackAndQuiet(t *testing.T) {
	execStart(t, true, termhost.Request{Program: "/usr/local/bin/nvim"})
	x := newExecModel(t)
	seq := int64(1)
	x.conn.push(t, "message.created", &seq, messageEvent("01JDNEW0000000000000000001", fixtures.UserMinji))
	x.waitHeld(t, "message.created", 1)
	execEnd(t)
	x.release(t)
	if want := "nvim 쓰는 동안 · 새 메시지 1 · 멘션 0"; countText(toastTexts(x.m), want) != 1 {
		t.Fatalf("toasts = %q, want %q", toastTexts(x.m), want)
	}

	execStart(t, true, termhost.Request{Program: "lazygit", Title: "lazygit"})
	y := newExecModel(t)
	seq2 := int64(1)
	y.conn.push(t, "message.created", &seq2, messageEvent("01JDNEW0000000000000000002", fixtures.UserSam)) // mine
	y.waitHeld(t, "message.created", 1)
	execEnd(t)
	y.release(t)
	for _, s := range toastTexts(y.m) {
		if strings.Contains(s, "쓰는 동안") {
			t.Fatalf("nothing counted, but toast %q", s)
		}
	}
}
