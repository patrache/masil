package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/patrache/masil/internal/realtime"
)

// helloFrame is what the fake hub sends first on every connection (events.md 3.2).
const helloFrame = `{"type":"hello","seq":null,"ts":"2026-11-05T02:05:00Z","data":{` +
	`"connection_id":"01JC0000000000000000000CON","session_id":"01JA7000000000000000000000",` +
	`"user_id":"01JA0000000000000000000SAM","mode":"notify","device_id":"01JC000000000000000000DEV",` +
	`"epoch":"E1","seq":0,"oldest_seq":1,"heartbeat_interval_ms":600000,"heartbeat_timeout_ms":1200000,` +
	`"local_tui_connected":false,"server_time":"2026-11-05T02:05:00Z",` +
	`"limits":{"max_frame_bytes":65536,"watch_topics_max":50,"typing_min_interval_ms":3000}}}`

// readyFrame answers every resume (events.md 3.3).
const readyFrame = `{"type":"ready","seq":null,"ts":"2026-11-05T02:05:00Z","data":` +
	`{"resumed":false,"resync_required":false,"replayed":0,"seq":0,"epoch":"E1"}}`

// dialRecord is one realtime dial seen by the hub.
type dialRecord struct {
	URL  string
	Auth string
}

// fakeHub stands in for the server's /v1/ws: dial hands out fakeConns that
// send hello, record the resume frame's data and answer it with ready.
type fakeHub struct {
	t      *testing.T
	dialCh chan dialRecord

	mu      sync.Mutex
	dials   []dialRecord
	resumes []json.RawMessage
	cur     *fakeConn
}

func newFakeHub(t *testing.T) *fakeHub {
	return &fakeHub{t: t, dialCh: make(chan dialRecord, 64)}
}

// dial is deps.dial.
func (h *fakeHub) dial(_ context.Context, url string, hdr http.Header) (realtime.Conn, error) {
	rec := dialRecord{URL: url, Auth: hdr.Get("Authorization")}
	c := &fakeConn{hub: h, in: make(chan []byte, 256), closeCh: make(chan int, 1), done: make(chan struct{})}
	c.in <- []byte(helloFrame)
	h.mu.Lock()
	h.dials = append(h.dials, rec)
	h.cur = c
	h.mu.Unlock()
	select {
	case h.dialCh <- rec:
	default:
		h.t.Error("fakeHub: too many dials")
	}
	return c, nil
}

// Dials returns every dial so far.
func (h *fakeHub) Dials() []dialRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.dials)
}

// Resumes returns the data object of every resume frame so far.
func (h *fakeHub) Resumes() []json.RawMessage {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.resumes)
}

func (h *fakeHub) current() *fakeConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cur == nil {
		h.t.Fatal("fakeHub: no connection yet")
	}
	return h.cur
}

// Push sends frame on the current connection.
func (h *fakeHub) Push(frame string) {
	h.current().in <- []byte(frame)
}

// CloseWith makes the current connection's next read fail with close code.
func (h *fakeHub) CloseWith(code int) {
	h.current().closeCh <- code
}

// fakeConn is one server-side connection as seen through realtime.Conn.
type fakeConn struct {
	hub     *fakeHub
	in      chan []byte
	closeCh chan int
	done    chan struct{}
	once    sync.Once
}

var errFakeClosed = errors.New("fakeConn: closed")

func (c *fakeConn) Read(ctx context.Context) ([]byte, error) {
	select {
	case code := <-c.closeCh:
		return nil, websocket.CloseError{Code: websocket.StatusCode(code)}
	default:
	}
	select {
	case b := <-c.in:
		return b, nil
	case code := <-c.closeCh:
		return nil, websocket.CloseError{Code: websocket.StatusCode(code)}
	case <-c.done:
		return nil, errFakeClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *fakeConn) Write(_ context.Context, data []byte) error {
	select {
	case <-c.done:
		return errFakeClosed
	default:
	}
	var f struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		c.hub.t.Errorf("fakeConn: client sent bad frame %q: %v", data, err)
		return nil
	}
	if f.Type == "resume" {
		// Queue ready before recording, so a test that saw the resume can
		// push frames that are sure to arrive after ready.
		c.in <- []byte(readyFrame)
		c.hub.mu.Lock()
		c.hub.resumes = append(c.hub.resumes, slices.Clone(f.Data))
		c.hub.mu.Unlock()
	}
	return nil
}

func (c *fakeConn) Close(int, string) error {
	c.once.Do(func() { close(c.done) })
	return nil
}

// waitDial returns the next dial, failing after 2s.
func waitDial(t *testing.T, h *fakeHub) dialRecord {
	t.Helper()
	select {
	case d := <-h.dialCh:
		return d
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a dial")
		return dialRecord{}
	}
}

// expectNoDial fails if a new dial happens within d.
func expectNoDial(t *testing.T, h *fakeHub, d time.Duration) {
	t.Helper()
	select {
	case rec := <-h.dialCh:
		t.Fatalf("unexpected dial %+v", rec)
	case <-time.After(d):
	}
}
