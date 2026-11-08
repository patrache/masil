package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/notify"
	"github.com/patrache/masil/internal/realtime"
)

type sinkCall struct {
	A  api.Alert
	In notify.Input
}

type fakeSink struct {
	calls chan sinkCall
	err   error
}

func (f *fakeSink) Handle(a api.Alert, in notify.Input) (notify.Decision, bool, error) {
	f.calls <- sinkCall{a, in}
	return notify.Popup, f.err == nil, f.err
}

// alertEvent builds a logged "alert" event; created is alert.created_at.
func alertEvent(seq int64, id string, created time.Time, replayed bool, suppressed bool, kind string) realtime.Event {
	data, err := json.Marshal(map[string]any{"alert": api.Alert{
		ID:         id,
		Kind:       api.AlertKind(kind),
		Title:      "title-" + id,
		Body:       "body-" + id,
		Target:     api.AlertTarget{Type: "message", ID: "M" + id},
		CreatedAt:  created,
		Suppressed: suppressed,
	}})
	if err != nil {
		panic(err)
	}
	return realtime.Event{Type: "alert", Seq: &seq, TS: created, Data: data, Replayed: replayed}
}

// startHandler wires a handler with a fake sink, presence(probe), desktop flag, a manual
// after() whose channels the test fires via the returned trigger, and a mark recorder.
func startHandler(t *testing.T, probe func() (bool, error), desktop bool) (h *handler, sink *fakeSink, pr *presence, fire func(), marks func() []int64) {
	t.Helper()
	h, sink, pr, fire, marks, _ = startHandlerLog(t, probe, desktop)
	return h, sink, pr, fire, marks
}

// startHandlerLog is startHandler that also returns the captured log text.
func startHandlerLog(t *testing.T, probe func() (bool, error), desktop bool) (h *handler, sink *fakeSink, pr *presence, fire func(), marks func() []int64, logs func() string) {
	t.Helper()
	clk := &fakeClock{now: t0}
	pr = newPresence(probe, clk.Now)
	sink = &fakeSink{calls: make(chan sinkCall, 64)}

	var amu sync.Mutex
	var pending []chan time.Time
	after := func(time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		amu.Lock()
		pending = append(pending, ch)
		amu.Unlock()
		return ch
	}
	fire = func() {
		t.Helper()
		amu.Lock()
		ps := pending
		pending = nil
		amu.Unlock()
		if len(ps) == 0 {
			t.Fatal("fire: no pending after() timer")
		}
		for _, ch := range ps {
			ch <- clk.Now()
		}
	}

	var mmu sync.Mutex
	var seqs []int64
	mark := func(seq int64) { mmu.Lock(); seqs = append(seqs, seq); mmu.Unlock() }
	marks = func() []int64 { mmu.Lock(); defer mmu.Unlock(); return slices.Clone(seqs) }

	var lmu sync.Mutex
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&lockedWriter{mu: &lmu, w: &buf}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	logs = func() string { lmu.Lock(); defer lmu.Unlock(); return buf.String() }

	h = newHandler(sink, pr, func() bool { return desktop }, clk.Now, after, mark, log)
	go h.run(t.Context())
	return h, sink, pr, fire, marks, logs
}

type lockedWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func TestHandlerFillsInput(t *testing.T) {
	h, sink, _, _, marks := startHandler(t, func() (bool, error) { return false, nil }, true)
	h.onEvent(alertEvent(7, "A1", t0.Add(-time.Minute), true, false, "dm"))
	c := recv(t, sink.calls)
	want := notify.Input{Role: notify.RoleDaemon, Now: t0, Replayed: true, DesktopEnabled: true, TUIPresent: false}
	if c.A.ID != "A1" || c.In != want {
		t.Fatalf("got %+v / %+v", c.A.ID, c.In)
	}
	waitFor(t, func() bool { return slices.Equal(marks(), []int64{7}) })
}

func TestHandlerTUIPresentAndDesktopOff(t *testing.T) {
	h, sink, _, _, _ := startHandler(t, func() (bool, error) { return true, nil }, false)
	h.onEvent(alertEvent(8, "A2", t0, false, false, "mention"))
	if c := recv(t, sink.calls); !c.In.TUIPresent || c.In.DesktopEnabled {
		t.Fatalf("input %+v", c.In)
	}
}

func TestHandlerGraceDefersThenRejudges(t *testing.T) {
	tru, fal := true, false
	for _, back := range []bool{false, true} { // server says TUI came back during grace?
		t.Run(fmt.Sprintf("tui_back=%v", back), func(t *testing.T) {
			h, sink, pr, fire, marks := startHandler(t, func() (bool, error) { return false, errors.New("x") }, true)
			pr.observe(&tru)
			pr.observe(&fal) // flip at t0 -> in grace
			h.onEvent(alertEvent(9, "A3", t0, false, false, "dm"))
			expectNone(t, sink.calls, 100*time.Millisecond)
			waitFor(t, func() bool { return slices.Equal(marks(), []int64{9}) })
			if back {
				pr.observe(&tru)
			}
			fire()
			if c := recv(t, sink.calls); c.In.TUIPresent != back {
				t.Fatalf("TUIPresent=%v", c.In.TUIPresent)
			}
			expectNone(t, sink.calls, 50*time.Millisecond) // judged again only once
		})
	}
}

func TestHandlerOrderAndIgnoredTypes(t *testing.T) {
	h, sink, _, _, marks := startHandler(t, func() (bool, error) { return false, nil }, true)
	h.onEvent(realtime.Event{Type: "notification.created", Seq: ptr(int64(10)), Data: json.RawMessage(`{}`)})
	h.onEvent(realtime.Event{Type: "focus.started", Seq: ptr(int64(11)), Data: json.RawMessage(`{}`)})
	h.onEvent(realtime.Event{Type: "pong"}) // ephemeral, no seq
	h.onEvent(alertEvent(12, "A4", t0, false, false, "invite"))
	recv(t, sink.calls)
	expectNone(t, sink.calls, 50*time.Millisecond)
	waitFor(t, func() bool { return slices.Equal(marks(), []int64{10, 11, 12}) })
}

// Uses the daemon's real sink + core ledger + Recorder backend (events.md 4.7 / 7).
// daemon.log must say what actually happened: an alert the ledger already had
// (the TUI showed it, or a replay) is "alert skipped" decision=duplicate, never
// "alert shown", so counting "alert shown" lines matches shown notifications.
func TestHandlerReplayAgeAndLedger(t *testing.T) {
	p := testPaths(t)
	rec := &lockedRecorder{} // mutex-wrapped notify.Backend
	var logs syncBuffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	now := func() time.Time { return t0 }
	h := newHandler(newLedgerSink(rec, notify.OpenLedger(p), log), newPresence(func() (bool, error) { return false, nil }, now),
		func() bool { return true }, now, time.After, func(int64) {}, log)
	go h.run(t.Context())

	h.onEvent(alertEvent(1, "F1", t0.Add(-time.Minute), false, true, "dm")) // suppressed (focus)
	h.onEvent(alertEvent(2, "S1", t0.Add(-time.Minute), false, false, "focus_summary"))
	h.onEvent(alertEvent(3, "S1", t0.Add(-time.Minute), true, false, "focus_summary")) // replay, same id
	h.onEvent(alertEvent(4, "R9", t0.Add(-9*time.Minute), true, false, "mention"))     // replay 9 min
	h.onEvent(alertEvent(5, "R11", t0.Add(-11*time.Minute), true, false, "mention"))   // replay 11 min
	if isNew, _ := notify.OpenLedger(p).MarkIfNew("T1"); !isNew {                      // TUI already showed T1
		t.Fatal("T1 should be new")
	}
	h.onEvent(alertEvent(6, "T1", t0, false, false, "dm"))

	waitFor(t, func() bool { return len(rec.ids()) == 2 })
	expectStable(t, rec.ids, 100*time.Millisecond)
	if got := rec.ids(); !slices.Equal(got, []string{"S1", "R9"}) {
		t.Fatalf("shown %v", got)
	}
	if all := rec.all(); all[0].Title == "" || all[0].Body == "" {
		t.Fatalf("notification not formatted: %+v", all[0])
	}
	waitFor(t, func() bool { return strings.Contains(logs.String(), "alert_id=T1") })
	out := logs.String()
	if n := strings.Count(out, `msg="alert shown"`); n != len(rec.ids()) {
		t.Fatalf("%d \"alert shown\" lines for %d shown notifications:\n%s", n, len(rec.ids()), out)
	}
	for _, want := range []string{
		`level=INFO msg="alert shown" alert_id=S1 kind=focus_summary decision=popup`,
		`level=INFO msg="alert skipped" alert_id=S1 kind=focus_summary decision=duplicate`,
		`level=INFO msg="alert shown" alert_id=R9 kind=mention decision=popup`,
		`level=INFO msg="alert skipped" alert_id=T1 kind=dm decision=duplicate`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("log missing %q:\n%s", want, out)
		}
	}
}

// A resume after a long sleep replays ~1000 notify-mode events while the sink is slow
// (osascript, D-Bus, WinRT toast). onEvent must never block, nothing may be dropped, marks stay in seq order.
func TestHandlerBurstNoLossInOrder(t *testing.T) {
	slow := &slowSink{delay: 2 * time.Millisecond} // records alert IDs in call order, sleeps per call
	var mu sync.Mutex
	var marks []int64
	h := newHandler(slow, newPresence(func() (bool, error) { return false, nil }, func() time.Time { return t0 }),
		func() bool { return true }, func() time.Time { return t0 }, time.After,
		func(seq int64) { mu.Lock(); marks = append(marks, seq); mu.Unlock() }, slog.New(slog.DiscardHandler))
	go h.run(t.Context())

	const n = 1000
	var wantAlerts []string
	start := time.Now()
	for seq := int64(1); seq <= n; seq++ {
		switch seq % 3 {
		case 0:
			id := fmt.Sprintf("B%04d", seq)
			wantAlerts = append(wantAlerts, id)
			h.onEvent(alertEvent(seq, id, t0, true, false, "dm"))
		case 1:
			h.onEvent(realtime.Event{Type: "notification.created", Seq: ptr(seq), Data: json.RawMessage(`{}`)})
		default:
			h.onEvent(realtime.Event{Type: "focus.started", Seq: ptr(seq), Data: json.RawMessage(`{}`)})
		}
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Fatalf("onEvent blocked: %v for %d events", d, n)
	}

	waitForTimeout(t, 10*time.Second, func() bool { mu.Lock(); defer mu.Unlock(); return len(marks) == n })
	mu.Lock()
	defer mu.Unlock()
	for i, s := range marks {
		if s != int64(i+1) {
			t.Fatalf("marks[%d] = %d, want %d (lost or out of order)", i, s, i+1)
		}
	}
	if got := slow.ids(); !slices.Equal(got, wantAlerts) {
		t.Fatalf("sink saw %d alerts, want %d in order", len(got), len(wantAlerts))
	}
	waitFor(t, func() bool { h.mu.Lock(); defer h.mu.Unlock(); return len(h.queue) == 0 }) // queue drained
}

// Log lines are fixed (plan 06 Global Constraints) and never carry titles or bodies.
func TestHandlerLogs(t *testing.T) {
	h, sink, _, _, marks, logs := startHandlerLog(t, func() (bool, error) { return false, nil }, true)
	h.onEvent(realtime.Event{Type: "ready", Data: json.RawMessage(`{"resumed":true,"resync_required":false,"replayed":3,"seq":20,"epoch":"E1"}`)})
	h.onEvent(alertEvent(21, "L1", t0, false, false, "dm"))
	recv(t, sink.calls)
	waitFor(t, func() bool { return slices.Equal(marks(), []int64{21}) })

	out := logs()
	for _, want := range []string{
		`level=INFO msg="realtime ready" resumed=true resync_required=false replayed=3`,
		`level=INFO msg="alert shown" alert_id=L1 kind=dm decision=popup`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("log missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "title-") || strings.Contains(out, "body-") {
		t.Fatalf("log leaks alert text:\n%s", out)
	}
}

func TestHandlerDisplayFailedWarns(t *testing.T) {
	h, sink, _, _, marks, logs := startHandlerLog(t, func() (bool, error) { return false, nil }, true)
	sink.err = errors.New("osascript exploded")
	h.onEvent(alertEvent(30, "W1", t0, false, false, "mention"))
	h.onEvent(alertEvent(31, "W2", t0, false, false, "mention"))
	recv(t, sink.calls)
	recv(t, sink.calls) // the failure does not stop the queue
	waitFor(t, func() bool { return slices.Equal(marks(), []int64{30, 31}) })
	out := logs()
	if want := `level=WARN msg="alert display failed" alert_id=W1 kind=mention err="osascript exploded"`; !strings.Contains(out, want) {
		t.Fatalf("log missing %q:\n%s", want, out)
	}
	if strings.Contains(out, "title-") || strings.Contains(out, "body-") {
		t.Fatalf("log leaks alert text:\n%s", out)
	}
}
