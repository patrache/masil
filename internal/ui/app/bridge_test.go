package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/auth"
	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/notify"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// Internal bridge tests (plan 03 Task 30). They do not import uitest (import cycle):
// the clock is the recording recTimers below plus a fixed Now, the API an apitest
// server with the fixture routes.

// bridgeNow is the fixed test clock (CA 10.1).
var bridgeNow = time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)

// recTimers records every ui.After request and never delivers it.
type recTimers struct {
	mu   sync.Mutex
	ds   []time.Duration
	msgs []tea.Msg
}

func (r *recTimers) After(d time.Duration, msg tea.Msg) tea.Cmd {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ds = append(r.ds, d)
	r.msgs = append(r.msgs, msg)
	return nil
}

// Pending returns the recorded durations, in request order.
func (r *recTimers) Pending() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.ds)
}

// bridgeConn is a scripted realtime.Conn: in = server frames, out = client frames.
type bridgeConn struct {
	in   chan []byte
	out  chan []byte
	done chan struct{}
	once sync.Once
}

func newBridgeConn() *bridgeConn {
	return &bridgeConn{in: make(chan []byte, 100), out: make(chan []byte, 100), done: make(chan struct{})}
}

func (c *bridgeConn) Read(ctx context.Context) ([]byte, error) {
	select {
	case b := <-c.in:
		return b, nil
	case <-c.done:
		return nil, net.ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *bridgeConn) Write(ctx context.Context, data []byte) error {
	select {
	case <-c.done:
		return net.ErrClosed
	default:
	}
	c.out <- append([]byte(nil), data...)
	return nil
}

func (c *bridgeConn) Close(code int, reason string) error {
	c.once.Do(func() { close(c.done) })
	return nil
}

// push sends one server frame.
func (c *bridgeConn) push(t *testing.T, typ string, seq *int64, data any) {
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
	c.in <- b
}

// waitFrame waits for a client frame of type typ (pings and others skipped).
func (c *bridgeConn) waitFrame(t *testing.T, typ string) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case b := <-c.out:
			var f struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(b, &f) == nil && f.Type == typ {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for a %q client frame", typ)
		}
	}
}

func helloData() map[string]any {
	return map[string]any{
		"connection_id": "CONN", "session_id": "S", "user_id": fixtures.UserSam, "mode": "full", "device_id": "D1",
		"epoch": "E1", "seq": 0, "oldest_seq": 1, "heartbeat_interval_ms": 25000,
		"heartbeat_timeout_ms": 120000, "local_tui_connected": false, "server_time": "2026-11-05T02:05:00Z",
		"limits": map[string]int{"max_frame_bytes": 8192, "watch_topics_max": 50, "typing_min_interval_ms": 3000},
	}
}

func readyData(resumed, resync bool) map[string]any {
	return map[string]any{"resumed": resumed, "resync_required": resync, "replayed": 0, "seq": 0, "epoch": "E1"}
}

// bridgeModel builds a logged-in Model with realtime enabled, the recording timers,
// an apitest server (fixture routes) and a sender collecting into the returned channel.
func bridgeModel(t *testing.T) (*Model, *apitest.Server, *recTimers, chan tea.Msg) {
	t.Helper()
	t.Setenv("MASIL_CONFIG_DIR", t.TempDir())
	t.Setenv("MASIL_CACHE_DIR", t.TempDir())
	srv := apitest.New(t)
	fixtures.RegisterAPI(srv)
	t.Setenv("MASIL_SERVER", srv.URL)
	paths, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.EnsureDir(paths); err != nil {
		t.Fatal(err)
	}
	timers := &recTimers{}
	m := New(Options{
		Paths:       paths,
		Config:      config.Defaults(),
		Credentials: &auth.Credentials{Version: 1, Server: srv.URL, Token: "msl_test", UserID: fixtures.UserSam, Handle: "sam"},
		Now:         func() time.Time { return bridgeNow },
		Loc:         time.UTC,
		Notifier:    notify.New(&notify.Recorder{}, notify.OpenLedger(paths), slog.New(slog.DiscardHandler)),
		Timers:      timers,
	})
	got := make(chan tea.Msg, 256)
	m.send = func(msg tea.Msg) { got <- msg }
	t.Cleanup(m.stopRealtime)
	return m, srv, timers, got
}

// drive runs Update(msg) and every resulting Cmd synchronously (tea.BatchMsg
// expanded), feeding the produced msgs back, breadth first.
func drive(t *testing.T, m *Model, msg tea.Msg) {
	t.Helper()
	queue := []tea.Msg{msg}
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 200 {
			t.Fatal("drive: message loop")
		}
		next := queue[0]
		queue = queue[1:]
		_, cmd := m.Update(next)
		queue = append(queue, runCmd(cmd)...)
	}
}

func runCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range msg {
			out = append(out, runCmd(c)...)
		}
		return out
	default:
		return []tea.Msg{msg}
	}
}

// meRequests counts GET /v1/me requests (one per bootstrap).
func meRequests(srv *apitest.Server) int {
	n := 0
	for _, r := range srv.Requests() {
		if r.Method == http.MethodGet && r.Path == "/v1/me" {
			n++
		}
	}
	return n
}

func TestBridgeDeliversEventsAndState(t *testing.T) {
	m, srv, _, got := bridgeModel(t)
	conn := newBridgeConn()
	var dialURL string
	m.rtDial = func(ctx context.Context, url string, h http.Header) (realtime.Conn, error) {
		dialURL = url
		return conn, nil
	}
	m.startRealtime()
	if _, ok := m.ctx.RT.(noopRealtime); ok {
		t.Fatal("startRealtime must install the live realtime in Context.RT")
	}

	conn.push(t, "hello", nil, helloData())
	conn.waitFrame(t, "resume")
	conn.push(t, "ready", nil, readyData(false, false))
	seq := int64(1)
	conn.push(t, "message.created", &seq, realtime.MessageEvent{Message: api.Message{
		ID: "01JDNEW0000000000000000001", ChannelID: fixtures.ChGeneral, Kind: "text", Body: "안녕",
		Author: api.UserSummary{ID: fixtures.UserMinji, Handle: "minji"}, CreatedAt: bridgeNow,
	}})

	var phases []realtime.Phase
	var events []string
	var msgs []tea.Msg
	deadline := time.After(3 * time.Second)
	for !slices.Contains(events, "message.created") {
		select {
		case msg := <-got:
			msgs = append(msgs, msg)
			switch msg := msg.(type) {
			case ui.RealtimeStateMsg:
				if len(phases) == 0 || phases[len(phases)-1] != msg.State.Phase {
					phases = append(phases, msg.State.Phase)
				}
			case ui.EventMsg:
				events = append(events, msg.Event.Type)
			}
		case <-deadline:
			t.Fatalf("timed out: phases %v, events %v", phases, events)
		}
	}
	if !strings.Contains(dialURL, "mode=full") || !strings.Contains(dialURL, "device_id=") {
		t.Errorf("dial url = %q, want mode=full and a device_id", dialURL)
	}
	want := []realtime.Phase{realtime.PhaseConnecting, realtime.PhaseSyncing, realtime.PhaseLive}
	if !slices.Equal(phases, want) {
		t.Errorf("phases = %v, want %v", phases, want)
	}
	if !slices.Equal(events, []string{"ready", "message.created"}) {
		t.Errorf("events = %v, want [ready message.created]", events)
	}

	// The collected msgs drive the app: state lands in Context.RT, ready bootstraps.
	for _, msg := range msgs {
		drive(t, m, msg)
	}
	if ph := m.ctx.RT.State().Phase; ph != realtime.PhaseLive {
		t.Errorf("Context.RT phase = %v, want live", ph)
	}
	if !m.ctx.Store.Sync.Ready || !m.ctx.Store.Sync.Bootstrapped {
		t.Errorf("sync = %+v, want ready and bootstrapped", m.ctx.Store.Sync)
	}
	if n := meRequests(srv); n != 1 {
		t.Errorf("GET /v1/me = %d, want 1 (bootstrap on the first ready)", n)
	}
	if pos := m.rt.Position(); pos.LastSeq == nil || *pos.LastSeq != 1 {
		t.Errorf("position = %+v, want seq 1 marked processed", pos)
	}
}

func TestReadyTimeoutFallsBackToREST(t *testing.T) {
	m, srv, timers, _ := bridgeModel(t)
	conn := newBridgeConn() // never sends hello
	m.rtDial = func(ctx context.Context, url string, h http.Header) (realtime.Conn, error) { return conn, nil }
	runCmd(m.startRealtime())
	if !slices.Contains(timers.Pending(), 5*time.Second) {
		t.Fatalf("startRealtime must arm the 5s ready timeout, pending = %v", timers.Pending())
	}
	gen := m.rtGen

	drive(t, m, readyTimeoutMsg{gen: gen - 1}) // stale generation: ignored
	if n := meRequests(srv); n != 0 {
		t.Fatalf("stale ready timeout bootstrapped (%d)", n)
	}
	drive(t, m, readyTimeoutMsg{gen: gen})
	if n := meRequests(srv); n != 1 {
		t.Fatalf("ready timeout: GET /v1/me = %d, want 1 (REST bootstrap first)", n)
	}
	if !m.ctx.Store.Sync.Bootstrapped {
		t.Fatalf("sync = %+v, want bootstrapped", m.ctx.Store.Sync)
	}

	// The late ready resyncs once more.
	drive(t, m, ui.EventMsg{Event: realtime.Event{Type: "ready", Data: mustJSON(t, readyData(false, false))}})
	if n := meRequests(srv); n != 2 {
		t.Fatalf("late ready: GET /v1/me = %d, want 2", n)
	}
	// A later resumed ready does nothing.
	drive(t, m, ui.EventMsg{Event: realtime.Event{Type: "ready", Data: mustJSON(t, readyData(true, false))}})
	if n := meRequests(srv); n != 2 {
		t.Fatalf("resumed ready: GET /v1/me = %d, want still 2", n)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A6.3 #1 in short: while the UI is stuck the realtime goroutine keeps reading
// and pinging (it only pushes to the queue), and once the UI is free again every
// event arrives in the order the server sent it.
func TestBridgeKeepsPingingWhileUIBlocked(t *testing.T) {
	m, _, _, _ := bridgeModel(t)
	var mu sync.Mutex
	var delivered []string
	gate := make(chan struct{})
	m.send = func(msg tea.Msg) {
		<-gate
		if e, ok := msg.(ui.EventMsg); ok {
			mu.Lock()
			delivered = append(delivered, e.Event.Type+":"+string(e.Event.Data))
			mu.Unlock()
		}
	}
	conn := newBridgeConn()
	m.rtDial = func(ctx context.Context, url string, h http.Header) (realtime.Conn, error) { return conn, nil }
	m.startRealtime()

	pings := make(chan struct{}, 1000)
	closed := make(chan struct{})
	go func() { // drain the client's frames, counting pings
		for {
			select {
			case b := <-conn.out:
				var f struct {
					Type string `json:"type"`
				}
				if json.Unmarshal(b, &f) == nil && f.Type == "ping" {
					pings <- struct{}{}
				}
			case <-conn.done:
				close(closed)
				return
			}
		}
	}()
	h := helloData()
	h["heartbeat_interval_ms"] = 20
	conn.push(t, "hello", nil, h)
	conn.push(t, "ready", nil, readyData(false, false))

	// UI blocked for 3s (plan Task 10) while the server sends 5 events a second (+ pongs).
	var want []string
	blockedUntil := time.Now().Add(3 * time.Second)
	seen := 0
	for i := 1; time.Now().Before(blockedUntil); i++ {
		data := map[string]any{"user_id": fixtures.UserMinji, "n": i}
		conn.push(t, "typing", nil, data)
		want = append(want, "typing:"+string(mustJSON(t, data)))
		deadline := time.After(200 * time.Millisecond)
	wait:
		for {
			select {
			case <-pings:
				seen++
				conn.push(t, "pong", nil, map[string]any{})
			case <-deadline:
				break wait
			}
		}
	}
	select {
	case <-closed:
		t.Fatal("the connection closed while the UI was blocked")
	default:
	}
	if seen < 10 {
		t.Fatalf("only %d pings while the UI was blocked, want the heartbeat to keep running", seen)
	}
	close(gate)
	deadline := time.Now().Add(3 * time.Second)
	for {
		mu.Lock()
		got := slices.Clone(delivered)
		mu.Unlock()
		var typing []string
		for _, d := range got {
			if strings.HasPrefix(d, "typing:") {
				typing = append(typing, d)
			}
		}
		if len(typing) == len(want) {
			if !slices.Equal(typing, want) {
				t.Fatalf("delivery order %v, want %v", typing, want)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("delivered %d of %d events", len(typing), len(want))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The queue's resyncNeededMsg runs the bootstrap; another one while that
// bootstrap is in flight runs it once more afterwards (bootAgain — events were
// dropped after the snapshot may have been taken), never more.
func TestResyncNeededRunsBootstrapOnce(t *testing.T) {
	m, srv, _, _ := bridgeModel(t)
	m.ctx.Store.Sync.Ready = true
	drive(t, m, resyncNeededMsg{})
	if n := meRequests(srv); n != 1 {
		t.Fatalf("one resync: GET /v1/me = %d, want 1", n)
	}
	// Two more while the next bootstrap is in flight.
	_, boot := m.Update(resyncNeededMsg{})
	_, again := m.Update(resyncNeededMsg{})
	if again != nil {
		t.Fatal("a resync during the flight must not start a second bootstrap now")
	}
	for _, msg := range runCmd(boot) {
		drive(t, m, msg)
	}
	if n := meRequests(srv); n != 3 {
		t.Fatalf("resyncs during a flight: GET /v1/me = %d, want 3 (1 + the flight + one bootAgain)", n)
	}
}
