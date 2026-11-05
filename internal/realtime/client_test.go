package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/patrache/masil/internal/api"
)

// Contract timings (events.md 1-5, CA 4.4), pinned here rather than taken
// from the implementation's constants.
const (
	window      = 10 * time.Second  // outbox: 25 frames per 10s
	ackEvery    = 5 * time.Second   // ack at most once per 5s
	pongWait    = 10 * time.Second  // ping -> any frame
	typingEvery = 3 * time.Second   // typing per (channel, thread)
	watchEvery  = 120 * time.Second // watch refresh
)

// waitLimit bounds every real-time wait in these tests (the client runs on its
// own goroutine; the fake clock decides every protocol delay).
const waitLimit = 3 * time.Second

// ---------------------------------------------------------------- fake clock

type fakeTimer struct {
	at    time.Time
	d     time.Duration
	ch    chan time.Time
	fired bool
}

// fakeClock drives Options.Now and Client.after. Advance moves Now and fires
// every timer that is due, in deadline order.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)}
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := &fakeTimer{at: f.now.Add(d), d: d, ch: make(chan time.Time, 1)}
	f.timers = append(f.timers, t)
	if d <= 0 {
		t.fired = true
		t.ch <- f.now
	}
	return t.ch
}

func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
	due := []*fakeTimer{}
	for _, t := range f.timers {
		if !t.fired && !t.at.After(f.now) {
			due = append(due, t)
		}
	}
	sort.SliceStable(due, func(i, j int) bool { return due[i].at.Before(due[j].at) })
	for _, t := range due {
		t.fired = true
		t.ch <- t.at
	}
}

// count returns how many timers of duration d were ever requested.
func (f *fakeClock) count(d time.Duration) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, t := range f.timers {
		if t.d == d {
			n++
		}
	}
	return n
}

// waitAfter blocks until at least n timers of duration d were requested.
func (f *fakeClock) waitAfter(t *testing.T, d time.Duration, n int) {
	t.Helper()
	deadline := time.Now().Add(waitLimit)
	for f.count(d) < n {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for after(%v) #%d (have %d)", d, n, f.count(d))
		}
		time.Sleep(time.Millisecond)
	}
}

// ---------------------------------------------------------------- fake conn

type readItem struct {
	b   []byte
	err error
}

type fakeConn struct {
	in     chan readItem // server -> client, in order
	out    chan []byte   // client -> server
	closed chan int      // close code passed to Close
	done   chan struct{}
	once   sync.Once
}

func newFakeConn() *fakeConn {
	return &fakeConn{
		in:     make(chan readItem, 1000),
		out:    make(chan []byte, 1000),
		closed: make(chan int, 1),
		done:   make(chan struct{}),
	}
}

func (f *fakeConn) Read(ctx context.Context) ([]byte, error) {
	select {
	case it := <-f.in:
		return it.b, it.err
	case <-f.done:
		return nil, net.ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *fakeConn) Write(ctx context.Context, data []byte) error {
	select {
	case <-f.done:
		return net.ErrClosed
	default:
	}
	f.out <- append([]byte(nil), data...)
	return nil
}

func (f *fakeConn) Close(code int, reason string) error {
	f.once.Do(func() {
		f.closed <- code
		close(f.done)
	})
	return nil
}

func i64(v int64) *int64   { return &v }
func str(v string) *string { return &v }

// send pushes one server frame.
func (f *fakeConn) send(t *testing.T, typ string, seq *int64, data any) {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(struct {
		Type string          `json:"type"`
		Seq  *int64          `json:"seq"`
		TS   string          `json:"ts"`
		Data json.RawMessage `json:"data"`
	}{typ, seq, "2026-11-05T02:05:00Z", raw})
	if err != nil {
		t.Fatal(err)
	}
	f.in <- readItem{b: b}
}

func (f *fakeConn) sendRaw(b string) { f.in <- readItem{b: []byte(b)} }

// closeWith makes the next Read fail like a server close frame with code.
func (f *fakeConn) closeWith(code int) {
	f.in <- readItem{err: websocket.CloseError{Code: websocket.StatusCode(code), Reason: "test"}}
}

func (f *fakeConn) hello(t *testing.T, epoch string, seq int64, interval int, localTUI bool) {
	t.Helper()
	f.send(t, "hello", nil, map[string]any{
		"connection_id": "CONN", "session_id": "S", "user_id": "U", "mode": "full", "device_id": "D1",
		"epoch": epoch, "seq": seq, "oldest_seq": 1, "heartbeat_interval_ms": interval,
		"heartbeat_timeout_ms": 120000, "local_tui_connected": localTUI, "server_time": "2026-11-05T02:05:00Z",
		"limits": map[string]int{"max_frame_bytes": 8192, "watch_topics_max": 50, "typing_min_interval_ms": 3000},
	})
}

func (f *fakeConn) ready(t *testing.T, resumed, resync bool, epoch string, seq int64) {
	t.Helper()
	f.send(t, "ready", nil, map[string]any{"resumed": resumed, "resync_required": resync, "replayed": 0, "seq": seq, "epoch": epoch})
}

// next returns the next client frame.
func (f *fakeConn) next(t *testing.T) string {
	t.Helper()
	select {
	case b := <-f.out:
		return string(b)
	case <-time.After(waitLimit):
		t.Fatalf("timed out waiting for a client frame")
		return ""
	}
}

// nextNonPing skips heartbeat pings.
func (f *fakeConn) nextNonPing(t *testing.T) string {
	t.Helper()
	for {
		s := f.next(t)
		if s != string(pingFrame) {
			return s
		}
	}
}

// none asserts that no client frame (other than ping) is written within d of real time.
func (f *fakeConn) none(t *testing.T, d time.Duration) {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case b := <-f.out:
			if string(b) == string(pingFrame) {
				continue
			}
			t.Fatalf("unexpected client frame %s", b)
		case <-deadline:
			return
		}
	}
}

func (f *fakeConn) waitClosed(t *testing.T) int {
	t.Helper()
	select {
	case c := <-f.closed:
		return c
	case <-time.After(waitLimit):
		t.Fatalf("timed out waiting for conn.Close")
		return 0
	}
}

// ---------------------------------------------------------------- fake dial

type dialCall struct {
	url string
	h   http.Header
}

type dialResult struct {
	conn Conn
	err  error
}

type fakeDialer struct {
	results chan dialResult
	calls   chan dialCall
}

func newFakeDialer() *fakeDialer {
	return &fakeDialer{results: make(chan dialResult, 100), calls: make(chan dialCall, 100)}
}

func (d *fakeDialer) Dial(ctx context.Context, url string, h http.Header) (Conn, error) {
	d.calls <- dialCall{url, h.Clone()}
	select {
	case r := <-d.results:
		if r.err != nil {
			return nil, r.err
		}
		return r.conn, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// accept queues a fresh connection for the next dial.
func (d *fakeDialer) accept() *fakeConn {
	c := newFakeConn()
	d.results <- dialResult{conn: c}
	return c
}

func (d *fakeDialer) fail(err error) { d.results <- dialResult{err: err} }

func (d *fakeDialer) waitCall(t *testing.T) dialCall {
	t.Helper()
	select {
	case c := <-d.calls:
		return c
	case <-time.After(waitLimit):
		t.Fatalf("timed out waiting for Dial")
		return dialCall{}
	}
}

func (d *fakeDialer) noCall(t *testing.T, wait time.Duration) {
	t.Helper()
	select {
	case c := <-d.calls:
		t.Fatalf("unexpected Dial %s", c.url)
	case <-time.After(wait):
	}
}

// ---------------------------------------------------------------- recorder

type recorder struct {
	mu     sync.Mutex
	events []Event
	states []State
	evc    chan Event
	stc    chan State
}

func newRecorder() *recorder {
	return &recorder{evc: make(chan Event, 1000), stc: make(chan State, 1000)}
}

func (r *recorder) onEvent(e Event) {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
	r.evc <- e
}

func (r *recorder) onState(s State) {
	r.mu.Lock()
	r.states = append(r.states, s)
	r.mu.Unlock()
	r.stc <- s
}

func (r *recorder) nextEvent(t *testing.T) Event {
	t.Helper()
	select {
	case e := <-r.evc:
		return e
	case <-time.After(waitLimit):
		t.Fatalf("timed out waiting for an event")
		return Event{}
	}
}

// waitEvent skips events until one of type typ arrives.
func (r *recorder) waitEvent(t *testing.T, typ string) Event {
	t.Helper()
	for {
		if e := r.nextEvent(t); e.Type == typ {
			return e
		}
	}
}

func (r *recorder) noEvent(t *testing.T, wait time.Duration) {
	t.Helper()
	select {
	case e := <-r.evc:
		t.Fatalf("unexpected event %s", e.Type)
	case <-time.After(wait):
	}
}

// waitPhase skips states until one with phase p arrives.
func (r *recorder) waitPhase(t *testing.T, p Phase) State {
	t.Helper()
	deadline := time.After(waitLimit)
	for {
		select {
		case s := <-r.stc:
			if s.Phase == p {
				return s
			}
		case <-deadline:
			t.Fatalf("timed out waiting for phase %d", p)
			return State{}
		}
	}
}

// drainStates discards every state reported so far.
func (r *recorder) drainStates() {
	for {
		select {
		case <-r.stc:
		default:
			return
		}
	}
}

// phases returns the recorded phases with consecutive duplicates collapsed.
func (r *recorder) phases() []Phase {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Phase
	for _, s := range r.states {
		if len(out) == 0 || out[len(out)-1] != s.Phase {
			out = append(out, s.Phase)
		}
	}
	return out
}

func (r *recorder) lastState() State {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.states) == 0 {
		return State{}
	}
	return r.states[len(r.states)-1]
}

// ---------------------------------------------------------------- harness

type harness struct {
	c      *Client
	clk    *fakeClock
	d      *fakeDialer
	rec    *recorder
	cancel context.CancelFunc
	done   chan error
}

type hopt struct {
	mode   Mode
	resume ResumePoint
	rnd    float64
}

func start(t *testing.T, o hopt) *harness {
	t.Helper()
	if o.mode == "" {
		o.mode = ModeFull
	}
	h := &harness{clk: newFakeClock(), d: newFakeDialer(), rec: newRecorder(), done: make(chan error, 1)}
	rnd := o.rnd
	h.c = New(Options{
		API:      api.New("http://example.test", api.WithToken("msl_test")),
		DeviceID: "D1",
		Mode:     o.mode,
		Resume:   o.resume,
		OnEvent:  h.rec.onEvent,
		OnState:  h.rec.onState,
		Now:      h.clk.Now,
		Rand:     func() float64 { return rnd },
		Dial:     h.d.Dial,
	})
	h.c.after = h.clk.After
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	go func() { h.done <- h.c.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-h.done:
		case <-time.After(waitLimit):
			t.Errorf("Run did not return after cancel")
		}
	})
	return h
}

func (h *harness) runResult(t *testing.T) error {
	t.Helper()
	select {
	case err := <-h.done:
		h.done <- err // keep it for Cleanup
		return err
	case <-time.After(waitLimit):
		t.Fatalf("Run did not return")
		return nil
	}
}

func (h *harness) stillRunning(t *testing.T, wait time.Duration) {
	t.Helper()
	select {
	case err := <-h.done:
		h.done <- err
		t.Fatalf("Run returned early: %v", err)
	case <-time.After(wait):
	}
}

// connect accepts the next dial and completes hello/resume/ready on it.
func (h *harness) connect(t *testing.T, epoch string, seq int64, resumed bool) *fakeConn {
	t.Helper()
	conn := h.d.accept()
	h.d.waitCall(t)
	conn.hello(t, epoch, seq, 3_600_000, false)
	if got := conn.next(t); !strings.HasPrefix(got, `{"type":"resume"`) {
		t.Fatalf("first client frame = %s, want resume", got)
	}
	conn.ready(t, resumed, false, epoch, seq)
	h.rec.waitEvent(t, "ready")
	return conn
}

func seqs(evs []Event) []string {
	out := []string{}
	for _, e := range evs {
		s := e.Type
		if e.Seq != nil {
			s = fmt.Sprintf("%d", *e.Seq)
		}
		if e.Replayed {
			s += "R"
		}
		out = append(out, s)
	}
	return out
}

// ---------------------------------------------------------------- tests

func TestFirstConnectHandshake(t *testing.T) {
	h := start(t, hopt{})
	conn := h.d.accept()
	call := h.d.waitCall(t)
	if call.url != "ws://example.test/v1/ws?device_id=D1&mode=full" {
		t.Errorf("dial url = %s", call.url)
	}
	if got := call.h.Get("Authorization"); got != "Bearer msl_test" {
		t.Errorf("Authorization = %q", got)
	}
	conn.hello(t, "E", 10, 25000, false)
	if got := conn.next(t); got != `{"type":"resume","data":{"epoch":null,"last_seq":null}}` {
		t.Fatalf("resume = %s", got)
	}
	conn.ready(t, false, false, "E", 10)
	ev := h.rec.nextEvent(t)
	if ev.Type != "ready" || ev.Replayed {
		t.Fatalf("first event = %s (replayed %v), want ready (hello is not delivered)", ev.Type, ev.Replayed)
	}
	h.rec.noEvent(t, 30*time.Millisecond)
	want := []Phase{PhaseConnecting, PhaseSyncing, PhaseLive}
	if got := h.rec.phases(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("phases = %v, want %v", got, want)
	}
	st := h.rec.lastState()
	if st.Epoch != "E" || st.Phase != PhaseLive {
		t.Errorf("state = %+v", st)
	}
	p := h.c.Position()
	if p.Epoch == nil || *p.Epoch != "E" || p.LastSeq == nil || *p.LastSeq != 10 {
		t.Errorf("Position = %+v", p)
	}
}

func TestReplayFlagAndDedupe(t *testing.T) {
	h := start(t, hopt{resume: ResumePoint{Epoch: str("E"), LastSeq: i64(5)}})
	conn := h.d.accept()
	h.d.waitCall(t)
	conn.hello(t, "E", 7, 3_600_000, false)
	if got := conn.next(t); got != `{"type":"resume","data":{"epoch":"E","last_seq":5}}` {
		t.Fatalf("resume = %s", got)
	}
	for _, s := range []int64{4, 6, 7} {
		conn.send(t, "message.created", i64(s), map[string]any{})
	}
	conn.ready(t, true, false, "E", 7)
	conn.send(t, "message.created", i64(8), map[string]any{})
	conn.send(t, "message.created", i64(8), map[string]any{})
	conn.send(t, "typing", nil, map[string]any{"channel_id": "C"})
	h.rec.waitEvent(t, "typing")
	h.rec.mu.Lock()
	got := seqs(h.rec.events)
	h.rec.mu.Unlock()
	want := []string{"6R", "7R", "ready", "8", "typing"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("events = %v, want %v", got, want)
	}
}

func TestNewEpochResetsBaseline(t *testing.T) {
	h := start(t, hopt{resume: ResumePoint{Epoch: str("E1"), LastSeq: i64(100)}})
	conn := h.d.accept()
	h.d.waitCall(t)
	conn.hello(t, "E2", 3, 3_600_000, false)
	if got := conn.next(t); got != `{"type":"resume","data":{"epoch":"E1","last_seq":100}}` {
		t.Fatalf("resume = %s", got)
	}
	// Until ready confirms the new epoch, Position still names the old one
	// (a drop here must resume E1 again and get resync_required again).
	if p := h.c.Position(); p.Epoch == nil || *p.Epoch != "E1" || *p.LastSeq != 100 {
		t.Fatalf("Position before ready = %+v", p)
	}
	conn.ready(t, false, true, "E2", 3)
	r := h.rec.waitEvent(t, "ready")
	dec, err := r.Decode()
	if err != nil || !dec.(Ready).ResyncRequired {
		t.Fatalf("ready decode = %+v, %v", dec, err)
	}
	conn.send(t, "message.created", i64(4), map[string]any{})
	ev := h.rec.nextEvent(t)
	if ev.Seq == nil || *ev.Seq != 4 || ev.Replayed {
		t.Fatalf("event = %+v", ev)
	}
	p := h.c.Position()
	if p.Epoch == nil || *p.Epoch != "E2" || p.LastSeq == nil || *p.LastSeq != 3 {
		t.Errorf("Position after ready = %+v", p)
	}
	h.c.MarkProcessed(4)
	if p := h.c.Position(); *p.LastSeq != 4 {
		t.Errorf("Position after MarkProcessed(4) = %d", *p.LastSeq)
	}
	if st := h.rec.lastState(); st.Epoch != "E2" {
		t.Errorf("State.Epoch = %q", st.Epoch)
	}
}

func TestPositionUsesProcessed(t *testing.T) {
	h := start(t, hopt{})
	conn := h.connect(t, "E", 0, false)
	for s := int64(1); s <= 5; s++ {
		conn.send(t, "message.created", i64(s), map[string]any{})
	}
	for range 5 {
		h.rec.nextEvent(t)
	}
	h.c.MarkProcessed(3)
	p := h.c.Position()
	if p.Epoch == nil || *p.Epoch != "E" || p.LastSeq == nil || *p.LastSeq != 3 || p.UseDeviceAck {
		t.Fatalf("Position = %+v", p)
	}
	// A seq never delivered cannot be processed.
	h.c.MarkProcessed(99)
	if p := h.c.Position(); *p.LastSeq != 3 {
		t.Fatalf("MarkProcessed(99) moved Position to %d", *p.LastSeq)
	}
	conn.closeWith(1001)
	conn2 := h.d.accept()
	h.d.waitCall(t)
	conn2.hello(t, "E", 5, 3_600_000, false)
	if got := conn2.next(t); got != `{"type":"resume","data":{"epoch":"E","last_seq":3}}` {
		t.Fatalf("resume after reconnect = %s", got)
	}
	// 4 and 5 are replayed again (never marked processed) and delivered.
	conn2.send(t, "message.created", i64(4), map[string]any{})
	conn2.send(t, "message.created", i64(5), map[string]any{})
	conn2.ready(t, true, false, "E", 5)
	if e := h.rec.nextEvent(t); *e.Seq != 4 || !e.Replayed {
		t.Errorf("replayed event = %+v", e)
	}
}

func TestPositionDeviceAck(t *testing.T) {
	h := start(t, hopt{mode: ModeNotify, resume: ResumePoint{UseDeviceAck: true}})
	if p := h.c.Position(); !p.UseDeviceAck || p.Epoch != nil {
		t.Fatalf("initial Position = %+v", p)
	}
	conn := h.d.accept()
	call := h.d.waitCall(t)
	if !strings.Contains(call.url, "mode=notify") {
		t.Errorf("url = %s", call.url)
	}
	conn.hello(t, "N", 20, 3_600_000, false)
	if got := conn.next(t); got != `{"type":"resume","data":{"use_device_ack":true}}` {
		t.Fatalf("resume = %s", got)
	}
	conn.send(t, "alert", i64(18), map[string]any{})
	conn.send(t, "alert", i64(19), map[string]any{})
	conn.ready(t, true, false, "N", 20)
	h.rec.waitEvent(t, "ready")
	// Nothing processed yet: resume point is just before the first replay.
	if p := h.c.Position(); p.Epoch == nil || *p.Epoch != "N" || *p.LastSeq != 17 || p.UseDeviceAck {
		t.Fatalf("Position = %+v", p)
	}
	h.c.MarkProcessed(19)
	if p := h.c.Position(); *p.LastSeq != 19 {
		t.Fatalf("Position after mark = %+v", p)
	}
}

func TestHeartbeat(t *testing.T) {
	h := start(t, hopt{})
	conn := h.d.accept()
	h.d.waitCall(t)
	conn.hello(t, "E", 0, 20, false)
	conn.next(t) // resume
	conn.ready(t, false, false, "E", 0)
	h.rec.waitEvent(t, "ready")

	h.clk.waitAfter(t, 20*time.Millisecond, 1)
	h.clk.Advance(20 * time.Millisecond)
	if got := conn.next(t); got != string(pingFrame) || len(got) != 15 {
		t.Fatalf("heartbeat frame = %q", got)
	}
	// Any frame within 10s keeps the connection.
	h.clk.waitAfter(t, pongWait, 1)
	conn.send(t, "reply", nil, map[string]any{"ref": "x", "ok": true, "error": nil})
	h.rec.waitEvent(t, "reply")
	h.clk.Advance(pongWait)
	select {
	case code := <-conn.closed:
		t.Fatalf("closed (%d) although a frame arrived after the ping", code)
	case <-time.After(50 * time.Millisecond):
	}
	// Now nothing answers the next ping for 10s: the connection is dead.
	h.clk.waitAfter(t, pongWait, 2)
	h.clk.Advance(pongWait)
	conn.waitClosed(t)
	h.d.waitCall(t)
}

func TestAckThrottle(t *testing.T) {
	h := start(t, hopt{})
	conn := h.connect(t, "E", 0, false)
	for s := int64(1); s <= 50; s++ {
		conn.send(t, "message.created", i64(s), map[string]any{})
	}
	for range 50 {
		h.rec.nextEvent(t)
	}
	for s := int64(1); s <= 25; s++ {
		h.c.MarkProcessed(s)
	}
	h.clk.waitAfter(t, ackEvery, 1)
	conn.none(t, 30*time.Millisecond)
	h.clk.Advance(time.Second)
	for s := int64(26); s <= 50; s++ {
		h.c.MarkProcessed(s)
	}
	conn.none(t, 30*time.Millisecond)
	if n := h.clk.count(ackEvery); n != 1 {
		t.Fatalf("ack timers = %d, want 1", n)
	}
	h.clk.Advance(ackEvery - time.Second)
	if got := conn.nextNonPing(t); got != `{"type":"ack","data":{"seq":50}}` {
		t.Fatalf("ack = %s", got)
	}
	conn.none(t, 30*time.Millisecond)
	h.clk.Advance(ackEvery)
	conn.none(t, 30*time.Millisecond)
	// A later mark is acked in the next window.
	conn.send(t, "message.created", i64(51), map[string]any{})
	h.rec.nextEvent(t)
	h.c.MarkProcessed(51)
	h.clk.waitAfter(t, ackEvery, 2)
	h.clk.Advance(ackEvery)
	if got := conn.nextNonPing(t); got != `{"type":"ack","data":{"seq":51}}` {
		t.Fatalf("second ack = %s", got)
	}
}

func TestCloseCodes(t *testing.T) {
	const rnd = 0.5
	first := backoffDelay(0, rnd)

	for _, code := range []int{1001, 4002} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			h := start(t, hopt{rnd: rnd})
			h.d.fail(errors.New("network down")) // Attempt becomes 1
			h.d.waitCall(t)
			if st := h.rec.waitPhase(t, PhaseBackoff); st.Attempt != 1 {
				t.Fatalf("backoff state = %+v", st)
			}
			h.clk.waitAfter(t, first, 1)
			h.clk.Advance(first)
			conn := h.connect(t, "E", 0, false)
			h.rec.drainStates()
			conn.closeWith(code)
			h.d.accept()
			h.d.waitCall(t) // immediate: no clock advance needed
			st := h.rec.waitPhase(t, PhaseConnecting)
			if st.Attempt != 0 || st.LastClose != code {
				t.Errorf("state after %d = %+v, want Attempt 0", code, st)
			}
			if n := h.clk.count(first); n != 1 {
				t.Errorf("backoff timers = %d, want 1 (none after %d)", n, code)
			}
		})
	}

	for _, code := range []int{1009, 1011, 4000, 1000} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			h := start(t, hopt{rnd: rnd})
			conn := h.connect(t, "E", 0, false)
			conn.closeWith(code)
			st := h.rec.waitPhase(t, PhaseBackoff)
			if st.Attempt != 1 || st.LastClose != code || st.NextRetry != h.clk.Now().Add(first) {
				t.Errorf("backoff state = %+v", st)
			}
			h.clk.waitAfter(t, first, 1)
			h.d.noCall(t, 30*time.Millisecond)
			h.clk.Advance(first)
			h.d.waitCall(t)
		})
	}

	for code, want := range map[int]error{4001: ErrUnauthorized, 4003: ErrSuspended, 4010: ErrUpgradeRequired} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			h := start(t, hopt{rnd: rnd})
			conn := h.connect(t, "E", 0, false)
			conn.closeWith(code)
			if err := h.runResult(t); !errors.Is(err, want) {
				t.Fatalf("Run = %v, want %v", err, want)
			}
			st := h.rec.lastState()
			if st.Phase != PhaseStopped || !errors.Is(st.Err, want) || st.LastClose != code {
				t.Errorf("final state = %+v", st)
			}
			h.d.noCall(t, 30*time.Millisecond)
		})
	}

	t.Run("4008", func(t *testing.T) {
		h := start(t, hopt{rnd: rnd})
		conn := h.connect(t, "E", 0, false)
		conn.closeWith(4008)
		h.clk.waitAfter(t, 10*time.Second, 1)
		h.d.noCall(t, 30*time.Millisecond)
		h.clk.Advance(10 * time.Second)
		h.d.waitCall(t)
	})

	t.Run("4009", func(t *testing.T) {
		h := start(t, hopt{rnd: 0})
		conn := h.connect(t, "E", 0, false)
		conn.closeWith(4009)
		conn2 := h.connect(t, "E", 0, true) // reconnects once
		h.clk.Advance(30 * time.Second)
		conn2.closeWith(4009) // second within 60s
		st := h.rec.waitPhase(t, PhaseStopped)
		if st.Err == nil || !strings.Contains(st.Err.Error(), "4009") || st.LastClose != 4009 {
			t.Errorf("stopped state = %+v", st)
		}
		h.d.noCall(t, 30*time.Millisecond)
		h.stillRunning(t, 30*time.Millisecond)
		h.cancel()
		if err := h.runResult(t); err != nil {
			t.Errorf("Run after cancel = %v, want nil", err)
		}
	})

	t.Run("4009 spaced out", func(t *testing.T) {
		h := start(t, hopt{rnd: 0})
		conn := h.connect(t, "E", 0, false)
		conn.closeWith(4009)
		conn2 := h.connect(t, "E", 0, true)
		h.clk.Advance(61 * time.Second)
		conn2.closeWith(4009)
		h.connect(t, "E", 0, true) // still reconnects
	})
}

func TestHTTPErrorsBeforeUpgrade(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{"401", &api.Error{Status: 401, Code: "unauthorized"}, ErrUnauthorized},
		{"403 suspended", &api.Error{Status: 403, Code: "account_suspended"}, ErrSuspended},
		{"426", &api.Error{Status: 426, Code: "upgrade_required"}, ErrUpgradeRequired},
		{"wrapped 401", fmt.Errorf("dial: %w", &api.Error{Status: 401, Code: "unauthorized"}), ErrUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := start(t, hopt{rnd: 0.5})
			h.d.fail(tc.err)
			if err := h.runResult(t); !errors.Is(err, tc.want) {
				t.Fatalf("Run = %v, want %v", err, tc.want)
			}
			if st := h.rec.lastState(); st.Phase != PhaseStopped || !errors.Is(st.Err, tc.want) {
				t.Errorf("final state = %+v", st)
			}
		})
	}

	t.Run("429", func(t *testing.T) {
		h := start(t, hopt{rnd: 0.5})
		h.d.fail(&api.Error{Status: 429, Code: "rate_limited", RetryAfter: 7 * time.Second})
		h.d.waitCall(t)
		h.clk.waitAfter(t, 7*time.Second, 1)
		h.d.noCall(t, 30*time.Millisecond)
		h.clk.Advance(7 * time.Second)
		h.d.waitCall(t)
	})

	t.Run("403 other and 5xx back off", func(t *testing.T) {
		h := start(t, hopt{rnd: 0.5})
		h.d.fail(&api.Error{Status: 403, Code: "forbidden"})
		h.d.waitCall(t)
		h.clk.waitAfter(t, backoffDelay(0, 0.5), 1)
		h.clk.Advance(backoffDelay(0, 0.5))
		h.d.fail(&api.Error{Status: 503, Code: "internal_error"})
		h.d.waitCall(t)
		h.clk.waitAfter(t, backoffDelay(1, 0.5), 1)
	})
}

func TestBackoffDelay(t *testing.T) {
	const r = 0.9999
	near := func(got, want time.Duration) bool {
		d := got - want
		if d < 0 {
			d = -d
		}
		return d <= want/1000+time.Millisecond
	}
	for _, n := range []int{0, 1, 5, 30, 1000} {
		if got := backoffDelay(n, 0); got != 0 {
			t.Errorf("backoffDelay(%d, 0) = %v", n, got)
		}
	}
	for _, tc := range []struct {
		n    int
		want time.Duration
	}{{0, 500 * time.Millisecond}, {1, time.Second}, {3, 4 * time.Second}, {5, 16 * time.Second}, {6, 30 * time.Second}, {20, 30 * time.Second}, {64, 30 * time.Second}, {1 << 20, 30 * time.Second}} {
		if got := backoffDelay(tc.n, r); !near(got, time.Duration(float64(tc.want)*r)) || got > tc.want {
			t.Errorf("backoffDelay(%d, %v) = %v, want ~%v", tc.n, r, got, tc.want)
		}
	}
	if got := backoffDelay(2, 0.5); got != time.Second {
		t.Errorf("backoffDelay(2, 0.5) = %v", got)
	}
	if got := backoffDelay(-1, 0.5); got != 250*time.Millisecond {
		t.Errorf("backoffDelay(-1, 0.5) = %v", got)
	}
}

func TestAttemptResetsAfter60sLive(t *testing.T) {
	for _, tc := range []struct {
		live time.Duration
		n    int
	}{{61 * time.Second, 0}, {30 * time.Second, 2}} {
		t.Run(tc.live.String(), func(t *testing.T) {
			const rnd = 0.5
			h := start(t, hopt{rnd: rnd})
			h.d.fail(errors.New("down"))
			h.d.waitCall(t)
			h.clk.waitAfter(t, backoffDelay(0, rnd), 1)
			h.clk.Advance(backoffDelay(0, rnd))
			h.d.fail(errors.New("down"))
			h.d.waitCall(t)
			h.clk.waitAfter(t, backoffDelay(1, rnd), 1)
			h.clk.Advance(backoffDelay(1, rnd))
			conn := h.connect(t, "E", 0, false)
			h.clk.Advance(tc.live)
			h.rec.drainStates()
			conn.closeWith(1011)
			st := h.rec.waitPhase(t, PhaseBackoff)
			want := backoffDelay(tc.n, rnd)
			if st.NextRetry != h.clk.Now().Add(want) || st.Attempt != tc.n+1 {
				t.Errorf("backoff state = %+v, want delay %v attempt %d", st, want, tc.n+1)
			}
			h.clk.waitAfter(t, want, 1)
		})
	}
}

func TestOutboxRateAndThrottles(t *testing.T) {
	t.Run("rate", func(t *testing.T) {
		h := start(t, hopt{})
		conn := h.connect(t, "E", 0, false) // resume counts as 1 frame of the window
		for i := range 60 {
			h.c.Typing(fmt.Sprintf("C%02d", i), nil)
		}
		var got []string
		read := func(n int) {
			for range n {
				got = append(got, conn.nextNonPing(t))
			}
			conn.none(t, 30*time.Millisecond)
		}
		read(24)
		h.clk.waitAfter(t, window, 1)
		h.clk.Advance(window)
		read(25)
		h.clk.waitAfter(t, window, 2)
		h.clk.Advance(window)
		read(11)
		for i, f := range got {
			if want := string(encodeTyping(fmt.Sprintf("C%02d", i), nil)); f != want {
				t.Fatalf("frame %d = %s, want %s", i, f, want)
			}
		}
	})

	t.Run("typing throttle", func(t *testing.T) {
		h := start(t, hopt{})
		conn := h.connect(t, "E", 0, false)
		for range 5 {
			h.c.Typing("C", nil)
			h.clk.Advance(500 * time.Millisecond)
		}
		if got := conn.nextNonPing(t); got != `{"type":"typing","data":{"channel_id":"C","thread_root_id":null}}` {
			t.Fatalf("typing = %s", got)
		}
		h.c.Typing("C", str("R")) // other thread is another key
		if got := conn.nextNonPing(t); got != `{"type":"typing","data":{"channel_id":"C","thread_root_id":"R"}}` {
			t.Fatalf("thread typing = %s", got)
		}
		conn.none(t, 30*time.Millisecond)
		h.clk.Advance(typingEvery)
		h.c.Typing("C", nil)
		if got := conn.nextNonPing(t); !strings.Contains(got, `"channel_id":"C"`) {
			t.Fatalf("typing after 3s = %s", got)
		}
	})

	t.Run("typing while offline is dropped", func(t *testing.T) {
		h := start(t, hopt{})
		h.c.Typing("C", nil)
		conn := h.connect(t, "E", 0, false)
		conn.none(t, 30*time.Millisecond)
	})

	t.Run("idle", func(t *testing.T) {
		h := start(t, hopt{})
		h.c.SetIdle(false) // initial value equals the server's default: nothing to send
		conn := h.connect(t, "E", 0, false)
		h.c.SetIdle(false)
		conn.none(t, 30*time.Millisecond)
		h.c.SetIdle(true)
		h.c.SetIdle(true)
		if got := conn.nextNonPing(t); got != `{"type":"idle","data":{"idle":true}}` {
			t.Fatalf("idle = %s", got)
		}
		conn.none(t, 30*time.Millisecond)
		conn.closeWith(1001)
		conn2 := h.connect(t, "E", 0, true)
		if got := conn2.nextNonPing(t); got != `{"type":"idle","data":{"idle":true}}` {
			t.Fatalf("idle after reconnect = %s", got)
		}
		h.c.SetIdle(false)
		if got := conn2.nextNonPing(t); got != `{"type":"idle","data":{"idle":false}}` {
			t.Fatalf("idle false = %s", got)
		}
	})

	t.Run("idle set while offline is sent on ready", func(t *testing.T) {
		h := start(t, hopt{})
		h.c.SetIdle(true)
		conn := h.connect(t, "E", 0, false)
		if got := conn.nextNonPing(t); got != `{"type":"idle","data":{"idle":true}}` {
			t.Fatalf("idle = %s", got)
		}
	})

	t.Run("watch", func(t *testing.T) {
		h := start(t, hopt{})
		conn := h.connect(t, "E", 0, false)
		topics := make([]string, 60)
		for i := range topics {
			topics[i] = fmt.Sprintf("post:P%02d", i)
		}
		want := string(encodeWatch(topics[:50]))
		h.c.Watch(topics)
		if got := conn.nextNonPing(t); got != want {
			t.Fatalf("watch = %s", got)
		}
		h.c.Watch(append([]string(nil), topics...)) // same list: nothing new
		conn.none(t, 30*time.Millisecond)
		h.clk.waitAfter(t, watchEvery, 1)
		h.clk.Advance(watchEvery)
		if got := conn.nextNonPing(t); got != want {
			t.Fatalf("watch refresh = %s", got)
		}
		conn.closeWith(1001)
		conn2 := h.connect(t, "E", 0, true)
		if got := conn2.nextNonPing(t); got != want {
			t.Fatalf("watch after reconnect = %s", got)
		}
		h.c.Watch(nil)
		if got := conn2.nextNonPing(t); got != `{"type":"watch","data":{"topics":[]}}` {
			t.Fatalf("unwatch = %s", got)
		}
	})
}

func TestNotifyModeIgnoresFullOnlyFrames(t *testing.T) {
	h := start(t, hopt{mode: ModeNotify})
	conn := h.connect(t, "E", 0, false)
	h.c.Typing("C", nil)
	h.c.SetIdle(true)
	h.c.Watch([]string{"post:P1"})
	conn.none(t, 50*time.Millisecond)
	conn.closeWith(1001)
	conn2 := h.connect(t, "E", 0, true)
	conn2.none(t, 50*time.Millisecond)
}

func TestTUIOnDevice(t *testing.T) {
	h := start(t, hopt{mode: ModeNotify})
	conn := h.d.accept()
	h.d.waitCall(t)
	conn.hello(t, "E", 0, 3_600_000, true)
	st := h.rec.waitPhase(t, PhaseSyncing)
	if st.TUIOnDevice == nil || !*st.TUIOnDevice {
		t.Fatalf("syncing state TUIOnDevice = %v", st.TUIOnDevice)
	}
	conn.next(t)
	conn.ready(t, false, false, "E", 0)
	h.rec.waitEvent(t, "ready")
	conn.send(t, "device.tui_state", nil, map[string]any{"device_id": "D1", "tui_connected": false})
	ev := h.rec.nextEvent(t)
	if ev.Type != "device.tui_state" {
		t.Fatalf("event = %s", ev.Type)
	}
	if st := h.rec.lastState(); st.TUIOnDevice == nil || *st.TUIOnDevice {
		t.Errorf("state TUIOnDevice after device.tui_state = %v", st.TUIOnDevice)
	}
}

func TestPongAndBadFramesIgnored(t *testing.T) {
	h := start(t, hopt{})
	conn := h.connect(t, "E", 0, false)
	conn.sendRaw(`{"type":"pong"}`)
	conn.sendRaw(`garbage`)
	conn.send(t, "future.thing", nil, map[string]any{})
	if ev := h.rec.nextEvent(t); ev.Type != "future.thing" {
		t.Fatalf("event = %s (pong must not be delivered)", ev.Type)
	}
}

func TestCloseStops(t *testing.T) {
	h := start(t, hopt{})
	conn := h.connect(t, "E", 0, false)
	h.c.Close()
	if code := conn.waitClosed(t); code != 1000 {
		t.Errorf("close code = %d, want 1000", code)
	}
	if err := h.runResult(t); err != nil {
		t.Errorf("Run = %v", err)
	}
	if st := h.rec.lastState(); st.Phase != PhaseStopped || st.Err != nil {
		t.Errorf("final state = %+v", st)
	}
	h.c.Close() // idempotent

	// Close before Run: Run returns at once without dialing.
	c := New(Options{API: api.New("http://example.test"), DeviceID: "D1", Dial: func(context.Context, string, http.Header) (Conn, error) {
		t.Error("dialed after Close")
		return nil, errors.New("no")
	}})
	c.Close()
	if err := c.Run(context.Background()); err != nil {
		t.Errorf("Run after Close = %v", err)
	}
}

func TestCancelClosesNormally(t *testing.T) {
	h := start(t, hopt{})
	conn := h.connect(t, "E", 0, false)
	h.cancel()
	if code := conn.waitClosed(t); code != 1000 {
		t.Errorf("close code = %d, want 1000", code)
	}
	if err := h.runResult(t); err != nil {
		t.Errorf("Run = %v", err)
	}
}
