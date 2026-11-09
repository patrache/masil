package collab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
)

func TestMain(m *testing.M) {
	backoffRand = func() float64 { return 0 } // reconnect at once
	rateLimitedDelay = 50 * time.Millisecond
	os.Exit(m.Run())
}

const wait = 3 * time.Second

// ---- a scripted document socket ----

type fconn struct {
	toClient   chan []byte
	fromClient chan []byte
	done       chan struct{}
	once       sync.Once
	mu         sync.Mutex
	err        error
	writes     []time.Time // when each client frame arrived (presence throttle)
	kinds      []string
}

func newFconn() *fconn {
	return &fconn{toClient: make(chan []byte, 256), fromClient: make(chan []byte, 4096), done: make(chan struct{})}
}

func (c *fconn) closeErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *fconn) Read(ctx context.Context) ([]byte, error) {
	select { // frames sent before a close are read first
	case b := <-c.toClient:
		return b, nil
	default:
	}
	select {
	case b := <-c.toClient:
		return b, nil
	case <-c.done:
		return nil, c.closeErr()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *fconn) Write(ctx context.Context, b []byte) error {
	select {
	case <-c.done:
		return c.closeErr()
	default:
	}
	var f struct {
		T string `json:"t"`
	}
	_ = json.Unmarshal(b, &f)
	c.mu.Lock()
	c.writes = append(c.writes, time.Now())
	c.kinds = append(c.kinds, f.T)
	c.mu.Unlock()
	c.fromClient <- append([]byte(nil), b...)
	return nil
}

func (c *fconn) shut(code websocket.StatusCode) {
	c.once.Do(func() {
		c.mu.Lock()
		c.err = websocket.CloseError{Code: code}
		c.mu.Unlock()
		close(c.done)
	})
}

func (c *fconn) Close(code int, reason string) error {
	c.shut(websocket.StatusCode(code))
	return nil
}

// send pushes one server frame.
func (c *fconn) send(t *testing.T, frame string) {
	t.Helper()
	c.toClient <- []byte(frame)
}

// next is the next client frame (any kind), parsed.
func (c *fconn) next(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	select {
	case b := <-c.fromClient:
		var m map[string]json.RawMessage
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("client frame %s: %v", b, err)
		}
		return m
	case <-time.After(wait):
		t.Fatalf("no client frame")
		return nil
	}
}

// expect is the next client frame, which must be of kind kind.
func (c *fconn) expect(t *testing.T, kind string) map[string]json.RawMessage {
	t.Helper()
	m := c.next(t)
	if got := strings.Trim(string(m["t"]), `"`); got != kind {
		t.Fatalf("client frame t = %s, want %s: %v", got, kind, m)
	}
	return m
}

func (c *fconn) quiet(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case b := <-c.fromClient:
		t.Fatalf("unexpected client frame %s", b)
	case <-time.After(d):
	}
}

type fdial struct {
	mu    sync.Mutex
	conns chan *fconn
	errs  []error // returned by the next dials, in order
	dials int
	hdrs  []http.Header
	urls  []string
}

func (d *fdial) Dial(ctx context.Context, url string, h http.Header) (realtime.Conn, error) {
	d.mu.Lock()
	d.dials++
	d.hdrs = append(d.hdrs, h)
	d.urls = append(d.urls, url)
	if len(d.errs) > 0 {
		e := d.errs[0]
		d.errs = d.errs[1:]
		d.mu.Unlock()
		return nil, e
	}
	d.mu.Unlock()
	c := newFconn()
	d.conns <- c
	return c, nil
}

func (d *fdial) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.dials
}

func (d *fdial) accept(t *testing.T) *fconn {
	t.Helper()
	select {
	case c := <-d.conns:
		return c
	case <-time.After(wait):
		t.Fatalf("no dial")
		return nil
	}
}

type recorder struct {
	ch   chan Event
	mu   sync.Mutex
	seen []Event
}

func (r *recorder) add(e Event) {
	r.mu.Lock()
	r.seen = append(r.seen, e)
	r.mu.Unlock()
	r.ch <- e
}

func (r *recorder) wait(t *testing.T, what string, pred func(Event) bool) Event {
	t.Helper()
	deadline := time.After(wait)
	for {
		select {
		case e := <-r.ch:
			if pred(e) {
				return e
			}
		case <-deadline:
			r.mu.Lock()
			defer r.mu.Unlock()
			t.Fatalf("no %s event; saw %+v", what, r.seen)
			return nil
		}
	}
}

func (r *recorder) all() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Event(nil), r.seen...)
}

func isState(connected bool) func(Event) bool {
	return func(e Event) bool { s, ok := e.(EventState); return ok && s.Connected == connected }
}

type rig struct {
	d   *fdial
	ev  *recorder
	s   *Session
	api *api.Client
}

func open(t *testing.T) *rig {
	t.Helper()
	r := &rig{d: &fdial{conns: make(chan *fconn, 16)}, ev: &recorder{ch: make(chan Event, 4096)}, api: api.New("https://masil.test", api.WithToken("msl_tok"))}
	r.api.SetLanguage("ko")
	r.s = Open(context.Background(), Options{API: r.api, DocID: "D1", OnEvent: r.ev.add, Dial: r.d.Dial})
	t.Cleanup(r.s.Close)
	return r
}

// connect accepts the next dial, checks hello{since} and answers it.
func (r *rig) connect(t *testing.T, since string, reply string) *fconn {
	t.Helper()
	c := r.d.accept(t)
	h := c.expect(t, "hello")
	if got := string(h["since"]); got != since {
		t.Fatalf("hello since = %s, want %s", got, since)
	}
	c.send(t, reply)
	r.ev.wait(t, "connected", isState(true))
	return c
}

const snap4 = `{"t":"hello","seq":4,"snapshot":{"seq":4,"objects":[{"id":"n1","type":"node","fields":{"body":"base","cpu":2}}]}}`

func setOp(id, field, value string) Op {
	return Op{Op: OpSet, ID: id, Field: field, Value: json.RawMessage(value)}
}

func field(s *Session, id, f string) string { return string(s.View().Field(id, f)) }

func opFrame(t *testing.T, m map[string]json.RawMessage) (id string, base int64, ops []Op) {
	t.Helper()
	if err := json.Unmarshal(m["client_op_id"], &id); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(m["base_seq"], &base); err != nil {
		t.Fatal(err)
	}
	var err error
	if ops, err = ParseOps(m["ops"]); err != nil {
		t.Fatalf("ops %s: %v", m["ops"], err)
	}
	return id, base, ops
}

// ---- tests ----

func TestSessionDialsTheDocSocket(t *testing.T) {
	r := open(t)
	r.connect(t, "null", snap4)
	r.d.mu.Lock()
	defer r.d.mu.Unlock()
	if r.d.urls[0] != "wss://masil.test/v1/docs/D1/ws" {
		t.Errorf("url = %s", r.d.urls[0])
	}
	if h := r.d.hdrs[0]; h.Get("Authorization") != "Bearer msl_tok" || h.Get("Accept-Language") != "ko" {
		t.Errorf("headers = %v", h)
	}
	if got := field(r.s, "n1", "body"); got != `"base"` || r.s.View().Seq() != 4 {
		t.Errorf("view after hello = %s seq %d", got, r.s.View().Seq())
	}
}

func TestSessionOptimisticThenAck(t *testing.T) {
	r := open(t)
	c := r.connect(t, "null", snap4)
	id := r.s.Submit([]Op{setOp("n1", "cpu", "8")})
	if got := field(r.s, "n1", "cpu"); got != "8" {
		t.Fatalf("optimistic view cpu = %s", got)
	}
	if ch := r.ev.wait(t, "local change", func(e Event) bool { c, ok := e.(EventChanged); return ok && !c.Remote }).(EventChanged); ch.Author != "" || len(ch.Changes) != 1 {
		t.Errorf("local change = %+v", ch)
	}
	gotID, base, ops := opFrame(t, c.expect(t, "op"))
	if gotID != id || base != 4 || len(ops) != 1 || ops[0].Field != "cpu" {
		t.Fatalf("op frame = %s %d %+v", gotID, base, ops)
	}
	if p := r.s.Pending(); len(p) != 1 {
		t.Fatalf("pending = %+v", p)
	}
	c.send(t, fmt.Sprintf(`{"t":"ack","client_op_id":%q,"seq":5}`, id))
	waitFor(t, func() bool { return len(r.s.Pending()) == 0 })
	if v := r.s.View(); string(v.Field("n1", "cpu")) != "8" || v.Seq() != 5 {
		t.Errorf("view after ack = %s seq %d", v.Field("n1", "cpu"), v.Seq())
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(wait)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("condition never held")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestSessionRebaseOnRemote(t *testing.T) {
	r := open(t)
	c := r.connect(t, "null", snap4)
	r.s.Submit([]Op{setOp("n1", "cpu", "8")})
	c.expect(t, "op")
	c.send(t, `{"t":"op","seq":5,"author":"U2","ops":[{"op":"set","id":"n1","field":"mem","value":16}]}`)
	ch := r.ev.wait(t, "remote change", func(e Event) bool { c, ok := e.(EventChanged); return ok && c.Remote && c.Author == "U2" }).(EventChanged)
	if len(ch.Changes) != 1 || ch.Changes[0] != (Change{ID: "n1", Field: "mem"}) {
		t.Errorf("changes = %+v", ch.Changes)
	}
	v := r.s.View()
	if string(v.Field("n1", "cpu")) != "8" || string(v.Field("n1", "mem")) != "16" || v.Seq() != 5 {
		t.Errorf("view = cpu %s mem %s seq %d", v.Field("n1", "cpu"), v.Field("n1", "mem"), v.Seq())
	}
	// someone's write to the same field before mine is sequenced: mine still shows (it will win)
	c.send(t, `{"t":"op","seq":6,"author":"U2","ops":[{"op":"set","id":"n1","field":"cpu","value":2}]}`)
	r.ev.wait(t, "remote change", func(e Event) bool { c, ok := e.(EventChanged); return ok && c.Remote })
	if got := field(r.s, "n1", "cpu"); got != "8" {
		t.Errorf("pending write lost to an earlier one: cpu = %s", got)
	}
	for _, e := range r.ev.all() {
		if _, ok := e.(EventOverwritten); ok {
			t.Errorf("an earlier remote write is not an overwrite: %+v", e)
		}
	}
}

func TestSessionOverwritten(t *testing.T) {
	r := open(t)
	c := r.connect(t, "null", snap4)
	id := r.s.Submit([]Op{setOp("n1", "body", `"A"`)})
	c.expect(t, "op")
	c.send(t, fmt.Sprintf(`{"t":"ack","client_op_id":%q,"seq":5}`, id))
	c.send(t, `{"t":"op","seq":6,"author":"U2","ops":[{"op":"set","id":"n1","field":"body","value":"B"}]}`)
	ow := r.ev.wait(t, "overwritten", func(e Event) bool { _, ok := e.(EventOverwritten); return ok }).(EventOverwritten)
	if ow.ID != "n1" || ow.Field != "body" || string(ow.Mine) != `"A"` || ow.By != "U2" {
		t.Errorf("overwritten = %+v", ow)
	}
	if got := field(r.s, "n1", "body"); got != `"B"` {
		t.Errorf("view body = %s, want B", got)
	}
}

// TestSessionOverwrittenBy: the losing write was sequenced (seq 5) but its
// ack was lost with the socket; the catch-up carries it and U2's later write
// (seq 6), and the resent frame's duplicate ack reports the overwrite.
func TestSessionOverwrittenBy(t *testing.T) {
	r := open(t)
	c := r.connect(t, "null", snap4)
	id := r.s.Submit([]Op{setOp("n1", "body", `"A"`)})
	c.expect(t, "op")
	c.shut(1006)
	c2 := r.connect(t, "4", `{"t":"hello","seq":6,"ops":[`+
		`{"seq":5,"author":"U1","ops":[{"op":"set","id":"n1","field":"body","value":"A"}]},`+
		`{"seq":6,"author":"U2","ops":[{"op":"set","id":"n1","field":"body","value":"B"}]}]}`)
	ch := r.ev.wait(t, "U2 change", func(e Event) bool { c, ok := e.(EventChanged); return ok && c.Author == "U2" }).(EventChanged)
	if len(ch.Changes) != 1 || ch.Changes[0].Field != "body" {
		t.Errorf("U2 change = %+v", ch)
	}
	gotID, _, _ := opFrame(t, c2.expect(t, "op"))
	if gotID != id {
		t.Fatalf("resent id %s, want %s", gotID, id)
	}
	c2.send(t, fmt.Sprintf(`{"t":"ack","client_op_id":%q,"seq":5}`, id))
	ow := r.ev.wait(t, "overwritten", func(e Event) bool { _, ok := e.(EventOverwritten); return ok }).(EventOverwritten)
	if ow.By != "U2" || string(ow.Mine) != `"A"` || ow.Field != "body" {
		t.Errorf("overwritten = %+v", ow)
	}
	if got := field(r.s, "n1", "body"); got != `"B"` || len(r.s.Pending()) != 0 {
		t.Errorf("view body = %s pending %d", got, len(r.s.Pending()))
	}
}

func TestSessionReconnectResends(t *testing.T) {
	r := open(t)
	c := r.connect(t, "null", snap4)
	a := r.s.Submit([]Op{setOp("n1", "cpu", "8")})
	b := r.s.Submit([]Op{setOp("n1", "mem", "32")})
	c.expect(t, "op")
	c.expect(t, "op")
	c.shut(1006) // network drop: no acks
	r.ev.wait(t, "dropped", isState(false))
	// catch up from seq 4: someone else's op 5 and my first op (sequenced as 6 before the drop)
	c2 := r.connect(t, "4", `{"t":"hello","seq":6,"ops":[`+
		`{"seq":5,"author":"U2","ops":[{"op":"set","id":"n1","field":"label","value":"api"}]},`+
		`{"seq":6,"author":"U1","ops":[{"op":"set","id":"n1","field":"cpu","value":8}]}]}`)
	id1, base, _ := opFrame(t, c2.expect(t, "op"))
	id2, _, _ := opFrame(t, c2.expect(t, "op"))
	if id1 != a || id2 != b || base != 6 {
		t.Fatalf("resent %s %s base %d, want %s %s base 6", id1, id2, base, a, b)
	}
	c2.send(t, fmt.Sprintf(`{"t":"ack","client_op_id":%q,"seq":6}`, a)) // duplicate: already applied
	c2.send(t, fmt.Sprintf(`{"t":"ack","client_op_id":%q,"seq":6}`, a)) // and again: ignored
	c2.send(t, fmt.Sprintf(`{"t":"ack","client_op_id":%q,"seq":7}`, b))
	waitFor(t, func() bool { return len(r.s.Pending()) == 0 })
	v := r.s.View()
	if v.Seq() != 7 || string(v.Field("n1", "cpu")) != "8" || string(v.Field("n1", "mem")) != "32" || string(v.Field("n1", "label")) != `"api"` {
		t.Errorf("view = %+v", v.Snapshot())
	}
	for _, e := range r.ev.all() {
		if _, ok := e.(EventOverwritten); ok {
			t.Errorf("unexpected %+v", e)
		}
	}
	c2.quiet(t, 100*time.Millisecond) // nothing is sent again
}

func TestSessionRejectDropsOps(t *testing.T) {
	for _, code := range []string{"locked", "forbidden"} {
		t.Run(code, func(t *testing.T) {
			r := open(t)
			c := r.connect(t, "null", snap4)
			id := r.s.Submit([]Op{setOp("n1", "cpu", "8"), {Op: OpPut, ID: "n2", Type: "node", Fields: map[string]json.RawMessage{}}})
			c.expect(t, "op")
			c.send(t, fmt.Sprintf(`{"t":"reject","client_op_id":%q,"code":%q}`, id, code))
			rj := r.ev.wait(t, "rejected", func(e Event) bool { _, ok := e.(EventRejected); return ok }).(EventRejected)
			if rj.ClientOpID != id || rj.Code != code || rj.Detail != "" || len(rj.Ops) != 2 {
				t.Errorf("rejected = %+v", rj)
			}
			v := r.s.View()
			if string(v.Field("n1", "cpu")) != "2" {
				t.Errorf("rejected edit still in the view: cpu = %s", v.Field("n1", "cpu"))
			}
			if _, ok := v.Object("n2"); ok {
				t.Errorf("rejected put still in the view")
			}
			if p := r.s.Pending(); len(p) != 0 {
				t.Errorf("pending = %+v", p)
			}
			c.quiet(t, 100*time.Millisecond) // not resent
		})
	}
}

func TestRejectDetail(t *testing.T) {
	r := open(t)
	c := r.connect(t, "null", snap4)
	id := r.s.Submit([]Op{setOp("n1", "secret", `"x"`)})
	c.expect(t, "op")
	c.send(t, fmt.Sprintf(`{"t":"reject","client_op_id":%q,"code":"invalid","detail":"secret_value"}`, id))
	rj := r.ev.wait(t, "rejected", func(e Event) bool { _, ok := e.(EventRejected); return ok }).(EventRejected)
	if rj.Code != "invalid" || rj.Detail != "secret_value" {
		t.Errorf("rejected = %+v", rj)
	}
}

func TestRejectRateLimitedIsResent(t *testing.T) {
	r := open(t)
	c := r.connect(t, "null", snap4)
	id := r.s.Submit([]Op{setOp("n1", "cpu", "8")})
	c.expect(t, "op")
	c.send(t, fmt.Sprintf(`{"t":"reject","client_op_id":%q,"code":"rate_limited"}`, id))
	again, _, _ := opFrame(t, c.expect(t, "op"))
	if again != id {
		t.Errorf("resent %s, want %s", again, id)
	}
	if got := field(r.s, "n1", "cpu"); got != "8" {
		t.Errorf("a rate-limited edit left the view: %s", got)
	}
}

func TestSubmitRefusesBadFramesLocally(t *testing.T) {
	r := open(t)
	c := r.connect(t, "null", snap4)
	bad := r.s.Submit([]Op{setOp("n1", "__proto__", "1")})
	rj := r.ev.wait(t, "rejected", func(e Event) bool { _, ok := e.(EventRejected); return ok }).(EventRejected)
	if rj.ClientOpID != bad || rj.Code != "invalid" {
		t.Errorf("rejected = %+v", rj)
	}
	big := r.s.Submit([]Op{setOp("n1", "body", `"`+strings.Repeat("x", MaxClientFrame)+`"`)})
	rj = r.ev.wait(t, "rejected", func(e Event) bool { _, ok := e.(EventRejected); return ok }).(EventRejected)
	if rj.ClientOpID != big || rj.Code != "too_large" {
		t.Errorf("rejected = %+v", rj)
	}
	c.quiet(t, 100*time.Millisecond)
	if len(r.s.Pending()) != 0 || field(r.s, "n1", "body") != `"base"` {
		t.Errorf("a refused frame reached the view or pending")
	}
}

func TestSessionKickedKeepsPending(t *testing.T) {
	r := open(t)
	c := r.connect(t, "null", snap4)
	r.s.Submit([]Op{setOp("n1", "cpu", "8")})
	r.s.Submit([]Op{setOp("n1", "mem", "16")})
	c.expect(t, "op")
	c.expect(t, "op")
	c.shut(1006) // dropped with both unconfirmed
	c2 := r.connect(t, "4", `{"t":"hello","seq":4,"ops":[]}`)
	c2.expect(t, "op")
	c2.expect(t, "op")
	c2.send(t, `{"t":"kicked","reason":"doc_deleted"}`)
	c2.shut(CloseKicked)
	k := r.ev.wait(t, "kicked", func(e Event) bool { _, ok := e.(EventKicked); return ok }).(EventKicked)
	if k.Reason != "doc_deleted" || len(k.Pending) != 2 {
		t.Errorf("kicked = %+v", k)
	}
	st := r.ev.wait(t, "stopped", isState(false)).(EventState)
	if !errors.Is(st.Err, ErrKicked) {
		t.Errorf("last state err = %v", st.Err)
	}
	time.Sleep(200 * time.Millisecond)
	if n := r.d.count(); n != 2 {
		t.Errorf("dials after kick = %d, want 2 (no reconnect loop)", n)
	}
	if p := r.s.Pending(); len(p) != 2 || p[0].Field != "cpu" || p[1].Field != "mem" {
		t.Errorf("pending = %+v", p)
	}
	if got := field(r.s, "n1", "cpu"); got != "8" {
		t.Errorf("view lost the unsent edit: %s", got)
	}
}

func TestSessionPresenceThrottle(t *testing.T) {
	r := open(t)
	c := r.connect(t, "null", snap4)
	start := time.Now()
	for i := range 50 {
		r.s.SetPresence(Presence{Cursor: json.RawMessage(fmt.Sprintf(`{"x":%d}`, i))})
		time.Sleep(time.Until(start.Add(time.Duration(i+1) * 20 * time.Millisecond)))
	}
	time.Sleep(250 * time.Millisecond)
	c.mu.Lock()
	var at []time.Time
	for i, k := range c.kinds {
		if k == "presence" {
			at = append(at, c.writes[i])
		}
	}
	c.mu.Unlock()
	if len(at) < 2 || len(at) > 12 {
		t.Fatalf("presence frames = %d, want about 10 for 1s", len(at))
	}
	for i := 0; i+10 < len(at); i++ {
		if d := at[i+10].Sub(at[i]); d < 950*time.Millisecond {
			t.Errorf("11 presence frames within %v (over 10 a second)", d)
		}
	}
	var last map[string]json.RawMessage
	for {
		select {
		case b := <-c.fromClient:
			_ = json.Unmarshal(b, &last)
			continue
		default:
		}
		break
	}
	if string(last["cursor"]) != `{"x":49}` {
		t.Errorf("last presence = %s, want the latest cursor", last["cursor"])
	}
}

func TestSessionPeers(t *testing.T) {
	r := open(t)
	c := r.connect(t, "null", snap4)
	c.send(t, `{"t":"presence","user":"U3","cursor":{"x":1},"selection":["n1"],"dragging":null}`)
	c.send(t, `{"t":"presence","user":"U2","cursor":null,"selection":[],"dragging":{"id":"n1"}}`)
	r.ev.wait(t, "presence", func(e Event) bool { p, ok := e.(EventPresence); return ok && len(p.Peers) == 2 })
	p := r.s.Peers()
	if len(p) != 2 || p[0].User != "U2" || p[1].User != "U3" || p[1].Selection[0] != "n1" || string(p[0].Dragging) != `{"id":"n1"}` {
		t.Errorf("peers = %+v", p)
	}
}

func TestResetAuthor(t *testing.T) {
	r := open(t)
	c := r.connect(t, "null", snap4)
	c.send(t, `{"t":"op","seq":5,"author":"U2","ops":[{"op":"reset","snapshot":{"seq":1,"objects":[{"id":"z","type":"node","fields":{"v":1}}]}}]}`)
	ch := r.ev.wait(t, "reset", func(e Event) bool { c, ok := e.(EventChanged); return ok && c.Remote && c.Author == "U2" }).(EventChanged)
	if len(ch.Changes) != 2 || ch.Changes[0] != (Change{ID: "n1", Removed: true}) || ch.Changes[1] != (Change{ID: "z"}) {
		t.Errorf("changes = %+v", ch.Changes)
	}
	v := r.s.View()
	if v.Seq() != 5 {
		t.Errorf("seq after reset = %d, want the frame seq 5", v.Seq())
	}
	if _, ok := v.Object("n1"); ok || string(v.Field("z", "v")) != "1" {
		t.Errorf("view after reset = %+v", v.Snapshot())
	}
}

func TestSessionCloseCodes(t *testing.T) {
	for _, code := range []int{CloseUnauthorized, CloseAccountSuspended, CloseKicked} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			r := open(t)
			c := r.connect(t, "null", snap4)
			c.shut(websocket.StatusCode(code))
			st := r.ev.wait(t, "stopped", isState(false)).(EventState)
			if code != CloseKicked && websocket.CloseStatus(st.Err) != websocket.StatusCode(code) {
				t.Errorf("err = %v", st.Err)
			}
			time.Sleep(150 * time.Millisecond)
			if n := r.d.count(); n != 1 {
				t.Errorf("dials = %d, want 1 (no reconnect)", n)
			}
		})
	}
	for _, code := range []int{int(websocket.StatusMessageTooBig), CloseRateLimited, int(websocket.StatusGoingAway)} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			r := open(t)
			c := r.connect(t, "null", snap4)
			c.shut(websocket.StatusCode(code))
			r.ev.wait(t, "dropped", isState(false))
			r.connect(t, "4", `{"t":"hello","seq":4,"ops":[]}`) // backoff, then again
		})
	}
	t.Run("upgrade 404", func(t *testing.T) {
		r := &rig{d: &fdial{conns: make(chan *fconn, 4), errs: []error{&api.Error{Status: 404, Code: "not_found"}}}, ev: &recorder{ch: make(chan Event, 64)}}
		r.s = Open(context.Background(), Options{API: api.New("https://masil.test"), DocID: "D1", OnEvent: r.ev.add, Dial: r.d.Dial})
		defer r.s.Close()
		st := r.ev.wait(t, "stopped", isState(false)).(EventState)
		if !api.IsCode(st.Err, "not_found") {
			t.Errorf("err = %v", st.Err)
		}
		time.Sleep(150 * time.Millisecond)
		if n := r.d.count(); n != 1 {
			t.Errorf("dials = %d, want 1", n)
		}
	})
	t.Run("upgrade 503 retries", func(t *testing.T) {
		r := &rig{d: &fdial{conns: make(chan *fconn, 4), errs: []error{&api.Error{Status: 503, Code: "internal_error"}}}, ev: &recorder{ch: make(chan Event, 64)}}
		r.s = Open(context.Background(), Options{API: api.New("https://masil.test"), DocID: "D1", OnEvent: r.ev.add, Dial: r.d.Dial})
		defer r.s.Close()
		r.connect(t, "null", snap4)
	})
}

// TestSessionReadsBigSnapshot: the real dial (Options.Dial nil) reads a
// 1 MiB+ hello snapshot in one frame (collab.md 4: 2,162,688 bytes).
func TestSessionReadsBigSnapshot(t *testing.T) {
	big := strings.Repeat("x", 1_500_000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/docs/D1/ws" || r.Header.Get("Authorization") != "Bearer msl_tok" {
			http.Error(w, `{"error":{"code":"not_found","message":"x","details":{}}}`, 404)
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx := context.Background()
		if _, _, err := c.Read(ctx); err != nil {
			return
		}
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"t":"hello","seq":1,"snapshot":{"seq":1,"objects":[{"id":"r1","type":"request","fields":{"body":"`+big+`"}}]}}`))
		_, _, _ = c.Read(ctx)
	}))
	defer srv.Close()
	ev := &recorder{ch: make(chan Event, 64)}
	s := Open(context.Background(), Options{API: api.New(srv.URL, api.WithToken("msl_tok")), DocID: "D1", OnEvent: ev.add})
	defer s.Close()
	ev.wait(t, "connected", isState(true))
	if got := len(s.View().Field("r1", "body")); got != len(big)+2 {
		t.Errorf("body = %d bytes, want %d", got, len(big)+2)
	}
}

func TestSessionCloseStopsEvents(t *testing.T) {
	r := open(t)
	c := r.connect(t, "null", snap4)
	r.s.Submit([]Op{setOp("n1", "cpu", "8")})
	r.s.Close()
	n := len(r.ev.all())
	c.send(t, `{"t":"op","seq":5,"author":"U2","ops":[{"op":"set","id":"n1","field":"mem","value":1}]}`)
	time.Sleep(100 * time.Millisecond)
	if len(r.ev.all()) != n {
		t.Errorf("events after Close")
	}
	if len(r.s.Pending()) != 1 {
		t.Errorf("Close dropped pending")
	}
}
