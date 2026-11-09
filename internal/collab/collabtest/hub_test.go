package collabtest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/collab"
	"github.com/patrache/masil/internal/realtime"
)

type vector struct {
	Name    string          `json:"name"`
	Start   json.RawMessage `json:"start"`
	Batches []struct {
		Seq    *int64          `json:"seq"`
		Ops    json.RawMessage `json:"ops"`
		Reject string          `json:"reject"`
	} `json:"batches"`
	Want json.RawMessage `json:"want"`
}

func same(t *testing.T, got, want []byte) bool {
	t.Helper()
	var a, b any
	if json.Unmarshal(got, &a) != nil || json.Unmarshal(want, &b) != nil {
		t.Fatalf("not JSON: %s / %s", got, want)
	}
	return reflect.DeepEqual(a, b)
}

func hdr(user string) http.Header { return http.Header{"Authorization": {"Bearer " + user}} }

const docURL = "ws://hub.test/v1/docs/D1/ws"

func read(t *testing.T, c realtime.Conn) map[string]json.RawMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func write(t *testing.T, c realtime.Conn, s string) {
	t.Helper()
	if err := c.Write(context.Background(), []byte(s)); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func kind(m map[string]json.RawMessage) string {
	var s string
	_ = json.Unmarshal(m["t"], &s)
	return s
}

// TestHubVectors: the hub sequences the shared vectors to the server's
// snapshot — client batches over its socket (ack with the vector's seq,
// reject invalid where the vector says so), reset batches by Inject.
func TestHubVectors(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "contract", "collab-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var vs []vector
	if err := json.Unmarshal(b, &vs); err != nil {
		t.Fatal(err)
	}
	if len(vs) < 10 {
		t.Fatalf("only %d vectors", len(vs))
	}
	for _, v := range vs {
		t.Run(v.Name, func(t *testing.T) {
			start, err := collab.DecodeSnapshot(v.Start)
			if err != nil {
				t.Fatal(err)
			}
			h := New(t)
			h.Seed("D1", start)
			c, err := h.Dial(context.Background(), docURL, hdr("U1"))
			if err != nil {
				t.Fatal(err)
			}
			write(t, c, `{"t":"hello","since":null}`)
			if m := read(t, c); kind(m) != "hello" || m["snapshot"] == nil {
				t.Fatalf("hello: %v", m)
			}
			for i, bt := range v.Batches {
				var ops []collab.Op
				_ = json.Unmarshal(bt.Ops, &ops)
				if bt.Reject == "" && len(ops) > 0 && ops[0].Op == collab.OpReset {
					h.Inject("D1", "U9", ops)
					if m := read(t, c); kind(m) != "op" || string(m["seq"]) != jsonInt(*bt.Seq) {
						t.Fatalf("batch %d: reset broadcast %v", i, m)
					}
					continue
				}
				id := "c" + jsonInt(int64(i))
				write(t, c, `{"t":"op","client_op_id":"`+id+`","base_seq":0,"ops":`+string(bt.Ops)+`}`)
				m := read(t, c)
				if bt.Reject != "" {
					if kind(m) != "reject" || string(m["code"]) != `"`+bt.Reject+`"` {
						t.Fatalf("batch %d: want reject %s, got %v", i, bt.Reject, m)
					}
					continue
				}
				if kind(m) != "ack" || string(m["seq"]) != jsonInt(*bt.Seq) {
					t.Fatalf("batch %d: want ack seq %d, got %v", i, *bt.Seq, m)
				}
			}
			got, _ := json.Marshal(h.Snapshot("D1"))
			if !same(t, got, v.Want) {
				t.Fatalf("snapshot\n got %s\nwant %s", got, v.Want)
			}
		})
	}
}

func jsonInt(n int64) string { b, _ := json.Marshal(n); return string(b) }

// TestHubSocket: hello since (ops catch-up), op forwarding with author, the
// idempotent ack, presence to the others, RejectNext, a too-big frame (1009)
// and a missing document (404).
func TestHubSocket(t *testing.T) {
	h := New(t)
	h.Seed("D1", collab.Snapshot{Seq: 1, Objects: []collab.Object{{ID: "n1", Type: "node", Fields: map[string]json.RawMessage{"cpu": json.RawMessage("2")}}}})
	a, _ := h.Dial(context.Background(), docURL, hdr("U1"))
	bc, _ := h.Dial(context.Background(), docURL, hdr("U2"))
	write(t, a, `{"t":"hello","since":null}`)
	read(t, a)
	write(t, bc, `{"t":"hello","since":1}`)
	if m := read(t, bc); kind(m) != "hello" || string(m["ops"]) != "[]" || m["snapshot"] != nil {
		t.Fatalf("catch-up hello: %v", m)
	}

	op := `{"t":"op","client_op_id":"x1","base_seq":1,"ops":[{"op":"set","id":"n1","field":"cpu","value":8}]}`
	write(t, a, op)
	if m := read(t, a); kind(m) != "ack" || string(m["seq"]) != "2" {
		t.Fatalf("ack: %v", m)
	}
	if m := read(t, bc); kind(m) != "op" || string(m["author"]) != `"U1"` || string(m["client_op_id"]) != `"x1"` {
		t.Fatalf("forwarded op: %v", m)
	}
	write(t, a, op) // resend: the same ack, nothing sequenced
	if m := read(t, a); kind(m) != "ack" || string(m["seq"]) != "2" {
		t.Fatalf("idempotent ack: %v", m)
	}
	if n := len(h.Ops("D1")); n != 1 {
		t.Fatalf("Ops = %d, want 1", n)
	}

	write(t, a, `{"t":"presence","cursor":{"x":1,"y":2}}`)
	if m := read(t, bc); kind(m) != "presence" || string(m["user"]) != `"U1"` || string(m["cursor"]) != `{"x":1,"y":2}` || string(m["selection"]) != "[]" {
		t.Fatalf("presence: %v", m)
	}

	h.RejectNext("D1", "locked", "")
	write(t, a, `{"t":"op","client_op_id":"x2","base_seq":2,"ops":[{"op":"del","id":"n1"}]}`)
	if m := read(t, a); kind(m) != "reject" || string(m["code"]) != `"locked"` {
		t.Fatalf("RejectNext: %v", m)
	}
	h.RejectNext("D1", "rate_limited", "ops")
	write(t, a, `{"t":"op","client_op_id":"x3","base_seq":2,"ops":[{"op":"del","id":"n1"}]}`)
	if m := read(t, a); string(m["detail"]) != `"ops"` {
		t.Fatalf("reject detail: %v", m)
	}
	write(t, a, `{"t":"op","client_op_id":"x4","base_seq":2,"ops":[{"op":"del","id":"n1"}]}`)
	if m := read(t, a); kind(m) != "ack" || string(m["seq"]) != "3" {
		t.Fatalf("after the rejects: %v", m)
	}
	read(t, bc)

	big := `{"t":"presence","cursor":"` + strings.Repeat("x", collab.MaxClientFrame) + `"}`
	_ = bc.Write(context.Background(), []byte(big))
	_, err := bc.Read(context.Background())
	if websocket.CloseStatus(err) != websocket.StatusMessageTooBig {
		t.Fatalf("too big: %v", err)
	}
	if h.Conns("D1", "") != 1 {
		t.Fatalf("Conns = %d after the 1009", h.Conns("D1", ""))
	}

	var ae *api.Error
	if _, err := h.Dial(context.Background(), "ws://hub.test/v1/docs/NOPE/ws", hdr("U1")); !errors.As(err, &ae) || ae.Status != 404 {
		t.Fatalf("missing doc dial: %v", err)
	}
}

type events struct {
	mu sync.Mutex
	ev []collab.Event
}

func (e *events) add(ev collab.Event) { e.mu.Lock(); e.ev = append(e.ev, ev); e.mu.Unlock() }

func (e *events) wait(t *testing.T, what string, ok func(collab.Event) bool) collab.Event {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		e.mu.Lock()
		for _, ev := range e.ev {
			if ok(ev) {
				e.mu.Unlock()
				return ev
			}
		}
		e.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no %s event", what)
	return nil
}

func (e *events) reset() { e.mu.Lock(); e.ev = nil; e.mu.Unlock() }

func connected(ev collab.Event) bool {
	s, ok := ev.(collab.EventState)
	return ok && s.Connected
}

// TestHubKickAndOffline: two real Sessions converge through the hub; Inject reaches
// them; SetOnline drops and later readmits a user (the session reconnects
// and resends); Kick ends a session with its pending ops.
func TestHubKickAndOffline(t *testing.T) {
	defer collab.SetBackoffForTest(func() float64 { return 0 })()
	h := New(t)
	h.Seed("D1", collab.Snapshot{})
	srv := apitest.New(t)
	h.MapToken("msl_test", "U1")
	open := func(user string) (*collab.Session, *events) {
		ev := &events{}
		s := collab.Open(context.Background(), collab.Options{API: srv.Client(), DocID: "D1", OnEvent: ev.add,
			Dial: func(ctx context.Context, url string, _ http.Header) (realtime.Conn, error) {
				return h.Dial(ctx, url, hdr(user))
			}})
		t.Cleanup(s.Close)
		ev.wait(t, user+" connected", connected)
		return s, ev
	}
	s1, ev1 := open("U1")
	s2, ev2 := open("U2")

	s1.Submit([]collab.Op{{Op: collab.OpPut, ID: "n1", Type: "node", Fields: map[string]json.RawMessage{"label": json.RawMessage(`"api"`)}}})
	ev2.wait(t, "remote put", func(ev collab.Event) bool {
		c, ok := ev.(collab.EventChanged)
		return ok && c.Remote && c.Author == "U1"
	})
	if o, ok := s2.View().Object("n1"); !ok || string(o.Fields["label"]) != `"api"` {
		t.Fatalf("U2 view: %+v %v", o, ok)
	}

	h.Inject("D1", "U3", []collab.Op{{Op: collab.OpSet, ID: "n1", Field: "cpu", Value: json.RawMessage("4")}})
	ev1.wait(t, "injected", func(ev collab.Event) bool { c, ok := ev.(collab.EventChanged); return ok && c.Author == "U3" })

	ev1.reset()
	h.SetOnline("U1", false)
	ev1.wait(t, "offline", func(ev collab.Event) bool { s, ok := ev.(collab.EventState); return ok && !s.Connected })
	s1.Submit([]collab.Op{{Op: collab.OpSet, ID: "n1", Field: "mem", Value: json.RawMessage("16")}})
	h.SetOnline("U1", true)
	ev1.wait(t, "back online", connected)
	ev2.wait(t, "resent op", func(ev collab.Event) bool {
		c, ok := ev.(collab.EventChanged)
		return ok && c.Author == "U1" && len(c.Changes) == 1 && c.Changes[0].Field == "mem"
	})
	if got := len(s1.Pending()); got != 0 {
		t.Fatalf("pending after resend = %d", got)
	}

	h.RejectNext("D1", "locked", "")
	h.RejectNext("D1", "locked", "")
	s2.Submit([]collab.Op{{Op: collab.OpDel, ID: "n1"}})
	ev2.wait(t, "rejected", func(ev collab.Event) bool { r, ok := ev.(collab.EventRejected); return ok && r.Code == "locked" })

	h.Kick("D1", "U2", "removed")
	k := ev2.wait(t, "kicked", func(ev collab.Event) bool { _, ok := ev.(collab.EventKicked); return ok }).(collab.EventKicked)
	if k.Reason != "removed" {
		t.Fatalf("kick reason %q", k.Reason)
	}
	if h.Conns("D1", "U2") != 0 {
		t.Fatal("U2 still connected after Kick")
	}
}

// TestHubREST: the room-document routes on an apitest server, through the
// real api.Client methods.
func TestHubREST(t *testing.T) {
	h := New(t)
	srv := apitest.New(t)
	h.Register(srv)
	h.MapToken("msl_test", "U1")
	c := srv.Client()
	ctx := context.Background()

	d, err := c.CreateDoc(ctx, "S1", api.CreateDocRequest{Type: "arch", Title: "결제", Snapshot: json.RawMessage(`{"objects":[{"id":"n1","type":"node","fields":{}}]}`)})
	if err != nil || d.SpaceID != "S1" || d.CreatedBy.ID != "U1" || d.HeadSeq != 1 {
		t.Fatalf("CreateDoc: %+v %v", d, err)
	}
	if _, err := c.CreateDoc(ctx, "S1", api.CreateDocRequest{Type: "nope", Title: "x"}); !isStatus(err, 400) {
		t.Fatalf("bad type: %v", err)
	}
	list, err := c.ListDocs(ctx, "S1", "arch", api.PageOpts{})
	if err != nil || len(list.Items) != 1 || list.Items[0].ID != d.ID || !list.Done() {
		t.Fatalf("ListDocs: %+v %v", list, err)
	}
	if l, _ := c.ListDocs(ctx, "S1", "apicol", api.PageOpts{}); len(l.Items) != 0 {
		t.Fatalf("type filter: %+v", l)
	}
	locked := true
	if u, err := c.UpdateDoc(ctx, d.ID, api.UpdateDocRequest{Locked: &locked}); err != nil || !u.Locked {
		t.Fatalf("UpdateDoc: %+v %v", u, err)
	}

	v1, err := c.CreateDocVersion(ctx, d.ID, "v1")
	if err != nil || v1.No != 1 || v1.Seq != 1 {
		t.Fatalf("CreateDocVersion: %+v %v", v1, err)
	}
	h.Inject(d.ID, "U2", []collab.Op{{Op: collab.OpDel, ID: "n1"}})
	vs, err := c.GetDocVersion(ctx, d.ID, 1)
	if err != nil || !strings.Contains(string(vs.Snapshot), `"n1"`) || strings.Contains(string(vs.Snapshot), "deleted") {
		t.Fatalf("GetDocVersion: %s %v", vs.Snapshot, err)
	}
	rv, err := c.RestoreDocVersion(ctx, d.ID, 1)
	if err != nil || rv.RestoredFrom == nil || *rv.RestoredFrom != 1 || rv.No != 2 {
		t.Fatalf("RestoreDocVersion: %+v %v", rv, err)
	}
	ops := h.Ops(d.ID)
	if len(ops) != 2 || ops[1].Op != collab.OpReset {
		t.Fatalf("restore ops: %+v", ops)
	}
	if o, ok := collab.NewDoc(h.Snapshot(d.ID)).Object("n1"); !ok || o.Deleted {
		t.Fatalf("restored n1: %+v %v", o, ok)
	}
	if vl, _ := c.ListDocVersions(ctx, d.ID); len(vl) != 2 || vl[0].No != 2 {
		t.Fatalf("ListDocVersions newest first: %+v", vl)
	}

	cm, err := c.CreateDocComment(ctx, d.ID, api.CreateDocCommentRequest{Anchor: "n1", Body: "@U2 봐 주세요"})
	if err != nil || cm.Author.ID != "U1" || cm.Anchor != "n1" {
		t.Fatalf("CreateDocComment: %+v %v", cm, err)
	}
	yes := true
	if u, err := c.UpdateDocComment(ctx, d.ID, cm.ID, api.UpdateDocCommentRequest{Resolved: &yes}); err != nil || u.ResolvedAt == nil {
		t.Fatalf("resolve: %+v %v", u, err)
	}
	if cl, _ := c.ListDocComments(ctx, d.ID, api.PageOpts{}); len(cl.Items) != 1 {
		t.Fatalf("ListDocComments: %+v", cl)
	}
	if err := c.DeleteDocComment(ctx, d.ID, cm.ID); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteDocComment(ctx, d.ID, cm.ID); !isStatus(err, 404) {
		t.Fatalf("second delete: %v", err)
	}

	if err := c.DeleteDoc(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetDoc(ctx, d.ID); !isStatus(err, 404) {
		t.Fatalf("deleted doc: %v", err)
	}
	if _, err := h.Dial(ctx, c.DocSocketURL(d.ID), c.AuthHeader()); !isStatus(err, 404) {
		t.Fatalf("deleted doc dial: %v", err)
	}
}

func isStatus(err error, status int) bool {
	var ae *api.Error
	return errors.As(err, &ae) && ae.Status == status
}

// TestHubSeedDoc: a seeded document with REST metadata (space, creator) is
// listed by the space's docs route and opens by socket.
func TestHubSeedDoc(t *testing.T) {
	h := New(t)
	srv := apitest.New(t)
	h.Register(srv)
	h.SeedDoc(api.Doc{ID: "D9", SpaceID: "S1", Type: "arch", CreatedBy: summary("U1")},
		collab.Snapshot{Seq: 3, Objects: []collab.Object{{ID: "n1", Type: "node", Fields: map[string]json.RawMessage{}}}})
	ctx := context.Background()
	list, err := srv.Client().ListDocs(ctx, "S1", "arch", api.PageOpts{})
	if err != nil || len(list.Items) != 1 {
		t.Fatalf("ListDocs: %+v %v", list, err)
	}
	if d := list.Items[0]; d.ID != "D9" || d.Title != "D9" || d.CreatedBy.ID != "U1" || d.HeadSeq != 3 || d.CreatedAt.IsZero() {
		t.Fatalf("seeded doc = %+v", d)
	}
	c, err := h.Dial(ctx, "ws://hub.test/v1/docs/D9/ws", hdr("U1"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(1000, "")
	if err := c.Write(ctx, []byte(`{"t":"hello","since":null}`)); err != nil {
		t.Fatal(err)
	}
	if m := read(t, c); string(m["seq"]) != "3" {
		t.Fatalf("hello = %v", m)
	}
}
