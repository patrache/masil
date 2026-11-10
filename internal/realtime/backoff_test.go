package realtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestNewBackoff: the exported sequence (internal/collab's document socket)
// is backoffDelay over consecutive failures, and Reset starts it again.
func TestNewBackoff(t *testing.T) {
	b := NewBackoff(func() float64 { return 0.5 })
	for n, want := range []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second} {
		if got := b.Next(); got != want {
			t.Errorf("Next #%d = %v, want %v", n, got, want)
		}
	}
	b.Reset()
	if got := b.Next(); got != 250*time.Millisecond {
		t.Errorf("after Reset: %v", got)
	}
	for range 20 {
		b.Next()
	}
	if got := b.Next(); got != backoffCap/2 {
		t.Errorf("capped: %v, want %v", got, backoffCap/2)
	}
	if NewBackoff(nil).Next() > backoffBase {
		t.Errorf("nil rand: first delay over the base")
	}
}

// TestDialConnReadLimit: DialConn applies the caller's read limit (the
// document socket reads up to 2,162,688 bytes, collab.md 4) and refuses a
// bigger frame (closing with 1009).
func TestDialConnReadLimit(t *testing.T) {
	const limit = 2_162_688
	sizes := make(chan int, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		n := <-sizes
		_ = c.Write(context.Background(), websocket.MessageText, []byte(strings.Repeat("x", n)))
		_, _, _ = c.Read(context.Background())
	}))
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sizes <- limit
	conn, err := DialConn(ctx, url, http.Header{}, limit)
	if err != nil {
		t.Fatal(err)
	}
	b, err := conn.Read(ctx)
	if err != nil || len(b) != limit {
		t.Fatalf("read %d bytes, err %v; want %d", len(b), err, limit)
	}
	conn.Close(1000, "")

	sizes <- limit + 1
	conn, err = DialConn(ctx, url, http.Header{}, limit)
	if err != nil {
		t.Fatal(err)
	}
	// coder/websocket closes with 1009 and reports the local limit as the error
	if _, err := conn.Read(ctx); err == nil || !strings.Contains(err.Error(), "message too big") {
		t.Fatalf("over the limit: err = %v, want message too big", err)
	}
}
