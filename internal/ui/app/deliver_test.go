package app

import (
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/ui"
)

// sink records delivered msgs; gate (when set) blocks send until closed.
type sink struct {
	mu   sync.Mutex
	got  []tea.Msg
	gate chan struct{}
	seen chan struct{} // one tick per delivered msg (buffered)
}

func newSink(gated bool) *sink {
	s := &sink{seen: make(chan struct{}, 100000)}
	if gated {
		s.gate = make(chan struct{})
	}
	return s
}

func (s *sink) send(msg tea.Msg) {
	if s.gate != nil {
		<-s.gate
	}
	s.mu.Lock()
	s.got = append(s.got, msg)
	s.mu.Unlock()
	s.seen <- struct{}{}
}

func (s *sink) wait(t *testing.T, n int) []tea.Msg {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		s.mu.Lock()
		got := slices.Clone(s.got)
		s.mu.Unlock()
		if len(got) >= n {
			return got
		}
		select {
		case <-s.seen:
		case <-deadline:
			t.Fatalf("delivered %d msgs, want %d", len(got), n)
		}
	}
}

func ev(i int) ui.EventMsg {
	return ui.EventMsg{Event: realtime.Event{Type: fmt.Sprintf("e%d", i)}}
}

func evName(m tea.Msg) string {
	switch m := m.(type) {
	case ui.EventMsg:
		return m.Event.Type
	case ui.RealtimeStateMsg:
		return fmt.Sprintf("state:%d", m.State.Phase)
	case resyncNeededMsg:
		return "resync"
	case string:
		return m
	}
	return fmt.Sprintf("%T", m)
}

func names(ms []tea.Msg) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = evName(m)
	}
	return out
}

// A producer (the realtime goroutine) never waits for the UI.
func TestQueueNeverBlocksProducer(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	q := newEventQueue(func(tea.Msg) { <-block }, bridgeQueueLimit)
	t.Cleanup(q.close)
	done := make(chan struct{})
	go func() {
		for i := range 10000 {
			q.push(ev(i))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("10000 pushes against a blocked UI took > 100ms")
	}
}

func TestQueueKeepsOrder(t *testing.T) {
	s := newSink(false)
	q := newEventQueue(s.send, bridgeQueueLimit)
	t.Cleanup(q.close)
	var want []string
	for i := 1; i <= 1000; i++ {
		q.push(ev(i))
		want = append(want, fmt.Sprintf("e%d", i))
	}
	if got := names(s.wait(t, 1000)); !slices.Equal(got, want) {
		t.Fatalf("order broken: first diff at %v", firstDiff(got, want))
	}
}

func firstDiff(a, b []string) int {
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

// Past the limit the queued events are dropped: the last state survives and one
// resyncNeededMsg follows; events keep being dropped until that marker went out,
// then pushes are normal again.
func TestQueueOverflowResyncsOnce(t *testing.T) {
	s := newSink(true)
	q := newEventQueue(s.send, 8)
	t.Cleanup(q.close)
	q.push("in-flight") // taken by the delivery goroutine, blocked in send
	waitTaken(t, q)
	for i := 1; i <= 20; i++ {
		switch i {
		case 5:
			q.push(ui.RealtimeStateMsg{State: realtime.State{Phase: realtime.PhaseSyncing}})
		case 15:
			q.push(ui.RealtimeStateMsg{State: realtime.State{Phase: realtime.PhaseLive}})
		default:
			q.push(ev(i))
		}
	}
	close(s.gate)
	got := names(s.wait(t, 3))
	if want := []string{"in-flight", fmt.Sprintf("state:%d", realtime.PhaseLive), "resync"}; !slices.Equal(got, want) {
		t.Fatalf("delivered %v, want %v", got, want)
	}
	q.push(ev(21))
	q.push(ev(22))
	if got := names(s.wait(t, 5)); !slices.Equal(got[3:], []string{"e21", "e22"}) {
		t.Fatalf("after the resync: %v", got)
	}
	s.mu.Lock()
	n := len(s.got)
	s.mu.Unlock()
	if n != 5 {
		t.Fatalf("delivered %d msgs, want exactly 5", n)
	}
}

// waitTaken waits until the delivery goroutine popped every queued item.
func waitTaken(t *testing.T, q *eventQueue) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for q.len() > 0 {
		if time.Now().After(deadline) {
			t.Fatal("delivery goroutine never took the item")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestPosterCoalesce(t *testing.T) {
	s := newSink(true)
	q := newEventQueue(s.send, bridgeQueueLimit)
	t.Cleanup(q.close)
	q.Post("in-flight")
	waitTaken(t, q)
	q.Post("a")
	q.Coalesce("dirty", "dirty-1")
	q.Post("b")
	q.Coalesce("dirty", "dirty-2")
	q.Coalesce("other", "other-1")
	q.Post("c")
	close(s.gate)
	got := names(s.wait(t, 6))
	if want := []string{"in-flight", "a", "dirty-2", "b", "other-1", "c"}; !slices.Equal(got, want) {
		t.Fatalf("delivered %v, want %v", got, want)
	}
	// Once delivered, the key starts over.
	q.Coalesce("dirty", "dirty-3")
	if got := names(s.wait(t, 7)); got[6] != "dirty-3" {
		t.Fatalf("after delivery: %v", got)
	}
}

// Posts and bridge msgs share one delivery goroutine and keep their mutual order.
func TestPostAndBridgeShareOrder(t *testing.T) {
	s := newSink(false)
	q := newEventQueue(s.send, bridgeQueueLimit)
	t.Cleanup(q.close)
	q.push(ev(1))
	q.Post("p1")
	q.push(ev(2))
	q.Post("p2")
	if got := names(s.wait(t, 4)); !slices.Equal(got, []string{"e1", "p1", "e2", "p2"}) {
		t.Fatalf("delivered %v", got)
	}
}

// A full post lane drops its oldest plain post (coalesced ones stay); bridge
// overflow never touches posts.
func TestPostOverflowDropsOldest(t *testing.T) {
	s := newSink(true)
	q := newEventQueue(s.send, 3)
	t.Cleanup(q.close)
	q.Post("in-flight")
	waitTaken(t, q)
	q.Coalesce("k", "keep")
	q.Post("p1")
	q.Post("p2")
	q.Post("p3") // lane full (3): p1 is dropped
	for i := range 5 {
		q.push(ev(i)) // bridge overflow
	}
	close(s.gate)
	got := names(s.wait(t, 5))
	if want := []string{"in-flight", "keep", "p2", "p3", "resync"}; !slices.Equal(got, want) {
		t.Fatalf("delivered %v, want %v", got, want)
	}
}

// After close nothing more is delivered and push / Post do not block or panic.
func TestQueueClose(t *testing.T) {
	s := newSink(false)
	q := newEventQueue(s.send, bridgeQueueLimit)
	q.close()
	q.push(ev(1))
	q.Post("x")
	q.close()
	time.Sleep(20 * time.Millisecond)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.got) != 0 {
		t.Fatalf("delivered after close: %v", names(s.got))
	}
}
