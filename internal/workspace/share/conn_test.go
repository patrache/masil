package share_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
)

func frame(kind wire.Kind, from, to uint8, payload string) []byte {
	h := wire.Header{Kind: kind, From: from, To: to, Len: uint32(len(payload))}
	b := h.Marshal()
	return append(b[:], payload...)
}

func recv(t *testing.T, c share.Conn) []byte {
	t.Helper()
	select {
	case f, ok := <-c.Recv():
		if !ok {
			t.Fatalf("Recv closed: %v", c.Err())
		}
		return f
	case <-time.After(5 * time.Second):
		t.Fatal("no frame within 5s")
		return nil
	}
}

func waitDone(t *testing.T, c share.Conn) {
	t.Helper()
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("conn not done within 5s")
	}
}

// TestDialerSendsAuthAndBinary: the dial carries the bearer token to
// /v1/workspace-shares/{id}/ws, frames go out as binary messages, a text
// message from the server is ignored, and the server's close code surfaces
// through CloseCodeOf.
func TestDialerSendsAuthAndBinary(t *testing.T) {
	gotType := make(chan websocket.MessageType, 1)
	gotData := make(chan []byte, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workspace-shares/W1/ws" {
			http.Error(w, `{"error":{"code":"not_found","message":"no","details":{}}}`, http.StatusNotFound)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer msl_test" {
			t.Errorf("Authorization = %q", got)
		}
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		ctx := r.Context()
		ws.Write(ctx, websocket.MessageText, []byte(`{"not":"binary"}`))
		ws.Write(ctx, websocket.MessageBinary, frame(wire.KindSrvHello, wire.SeatServer, 1, `{"seat":1}`))
		typ, b, err := ws.Read(ctx)
		if err != nil {
			t.Errorf("server read: %v", err)
			return
		}
		gotType <- typ
		gotData <- b
		ws.Close(websocket.StatusCode(share.CloseKicked), "kicked")
	}))
	defer srv.Close()

	c := api.New(srv.URL, api.WithToken("msl_test"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := share.Dialer(c)(ctx, "W1")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	f := recv(t, conn)
	if h, p, err := wire.Parse(f); err != nil || h.Kind != wire.KindSrvHello || string(p) != `{"seat":1}` {
		t.Fatalf("first frame = %v %q %v; the text message must be skipped", h, p, err)
	}
	out := frame(wire.KindHsCommit, 1, 0, "commit")
	if err := conn.Send(out); err != nil {
		t.Fatalf("send: %v", err)
	}
	if typ := <-gotType; typ != websocket.MessageBinary {
		t.Errorf("server got message type %v, want binary", typ)
	}
	if b := <-gotData; string(b) != string(out) {
		t.Errorf("server got %q", b)
	}
	waitDone(t, conn)
	if code := share.CloseCodeOf(conn.Err()); code != share.CloseKicked {
		t.Errorf("CloseCodeOf = %d (%v), want 4101", code, conn.Err())
	}
	if err := conn.Send(out); err == nil {
		t.Error("Send after close succeeded")
	}
	if _, ok := <-conn.Recv(); ok {
		t.Error("Recv still open after Done")
	}
}

// A refused upgrade is an *api.Error (404 = the share is gone or not mine).
func TestDialerRefusedUpgrade(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":{"code":"not_found","message":"workspace share not found","details":{}}}`))
	}))
	defer srv.Close()
	_, err := share.Dialer(api.New(srv.URL, api.WithToken("msl_test")))(context.Background(), "W1")
	var ae *api.Error
	if !errors.As(err, &ae) || ae.Status != 404 || ae.Code != "not_found" {
		t.Fatalf("err = %v, want *api.Error 404 not_found", err)
	}
	if _, err := share.Dialer(api.New(srv.URL))(context.Background(), ".."); err == nil {
		t.Error("dial of share id \"..\" succeeded")
	}
}

// Close sends the given code, and a frame over wire.MaxFrame ends the conn.
func TestConnCloseAndReadLimit(t *testing.T) {
	closed := make(chan websocket.StatusCode, 1)
	big := make(chan bool, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		if <-big {
			ws.Write(r.Context(), websocket.MessageBinary, make([]byte, wire.MaxFrame+1))
		}
		_, _, err = ws.Read(r.Context())
		closed <- websocket.CloseStatus(err)
	}))
	defer srv.Close()
	dial := share.Dialer(api.New(srv.URL, api.WithToken("msl_test")))

	big <- false
	conn, err := dial(context.Background(), "W1")
	if err != nil {
		t.Fatal(err)
	}
	conn.Close(4000, "bye")
	if code := <-closed; code != 4000 {
		t.Errorf("server saw close %d, want 4000", code)
	}
	waitDone(t, conn)

	big <- true
	conn, err = dial(context.Background(), "W1")
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, conn)
	if code := websocket.CloseStatus(conn.Err()); code != websocket.StatusMessageTooBig && !strings.Contains(conn.Err().Error(), "limit") {
		t.Errorf("oversize frame: err = %v", conn.Err())
	}
	<-closed
}

func TestCloseCodeOf(t *testing.T) {
	if got := share.CloseCodeOf(nil); got != 0 {
		t.Errorf("nil = %d", got)
	}
	if got := share.CloseCodeOf(errors.New("x")); got != 0 {
		t.Errorf("plain = %d", got)
	}
	err := error(websocket.CloseError{Code: websocket.StatusCode(share.CloseEnded), Reason: "share_ended"})
	if got := share.CloseCodeOf(err); got != share.CloseEnded {
		t.Errorf("ended = %d", got)
	}
	for code, want := range map[share.CloseCode]int{share.CloseProtocol: 4000, share.CloseUnauthorized: 4001, share.CloseSuspended: 4003, share.CloseRateLimited: 4008, share.CloseEnded: 4100, share.CloseKicked: 4101, share.CloseReplaced: 4102} {
		if int(code) != want {
			t.Errorf("close code %d, want %d", code, want)
		}
	}
}
