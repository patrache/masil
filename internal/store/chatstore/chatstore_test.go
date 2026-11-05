package chatstore

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
)

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestGetRegistered(t *testing.T) {
	s := store.New()
	c := Get(s)
	if c == nil || c.Name() != "chat" {
		t.Fatalf("chat reducer not registered")
	}
	if Get(s) != c {
		t.Fatal("Get must return the same instance")
	}
}

func TestWindowCreatedOnDemand(t *testing.T) {
	c := Get(store.New())
	if c.HasWindow("C1") {
		t.Fatal("no window yet")
	}
	w := c.Window("C1")
	if w == nil || !c.HasWindow("C1") || c.Loaded("C1") {
		t.Fatal("window created, not loaded")
	}
	if c.Window("C1") != w {
		t.Fatal("same window on second call")
	}
}

func TestThreadNeverOpened(t *testing.T) {
	c := Get(store.New())
	root, replies := c.Thread("R1")
	if root != nil || replies == nil || replies.Len() != 0 {
		t.Fatalf("want (nil, empty window), got (%v, %v)", root, replies)
	}
}

func TestPendingLifecycle(t *testing.T) {
	c := Get(store.New())
	at := time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)
	c.AddPending("C1", PendingMessage{Nonce: "N1", Body: "안녕", Kind: "text", SentAt: at})
	c.AddPending("C1", PendingMessage{Nonce: "N2", Body: "둘", Kind: "text", SentAt: at.Add(time.Second)})
	c.FailPending("C1", "N1", errors.New("boom"))
	p := c.Pending("C1")
	if len(p) != 2 || p[0].Nonce != "N1" || p[0].Failed == nil {
		t.Fatalf("got %+v", p)
	}
	c.RetryPending("C1", "N1", at.Add(2*time.Second))
	if got := c.Pending("C1")[0]; got.Failed != nil || !got.SentAt.Equal(at.Add(2*time.Second)) {
		t.Fatalf("retry clears failure and restamps, got %+v", got)
	}
	c.DropPending("C1", "N1")
	if got := c.Pending("C1"); len(got) != 1 || got[0].Nonce != "N2" {
		t.Fatalf("got %+v", got)
	}
}

func TestPendingReturnsCopy(t *testing.T) {
	c := Get(store.New())
	c.AddPending("C1", PendingMessage{Nonce: "N1", Body: "a"})
	p := c.Pending("C1")
	p[0].Body = "mutated"
	if c.Pending("C1")[0].Body != "a" {
		t.Fatal("Pending must return a copy")
	}
}

func TestPendingPollKeptUntilDropped(t *testing.T) {
	c := Get(store.New())
	poll := &api.PollInput{Question: "점심", Options: []string{"국밥", "돈까스"}}
	c.AddPending("C1", PendingMessage{Nonce: "P1", Kind: "poll", Body: "📊 점심"})
	c.SetPendingPoll("C1", "P1", poll)
	c.FailPending("C1", "P1", errors.New("503"))
	if c.PendingPoll("C1", "P1") != poll {
		t.Fatal("poll input survives failure (needed for resend)")
	}
	c.DropPending("C1", "P1")
	if c.PendingPoll("C1", "P1") != nil {
		t.Fatal("dropped with the pending item")
	}
}

func TestTypingExpiryAndFilter(t *testing.T) {
	s := store.New()
	s.Me = &api.Me{UserSummary: api.UserSummary{ID: "ME"}}
	s.Blocks = map[string]time.Time{"BAD": {}}
	c := Get(s)
	t0 := time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)
	now := t0
	c.SetClock(func() time.Time { return now })
	skewed := t0.Add(-time.Hour) // server clock far behind: must not matter (expiry is local receipt time + expires_in_ms)
	for _, u := range []string{"U1", "ME", "BAD"} {
		c.Apply(s, realtime.Event{Type: "typing", TS: skewed, Data: mustJSON(t, realtime.Typing{ChannelID: "C1", UserID: u, ExpiresInMS: 6000})})
	}
	if got := c.Typing("C1", nil, t0.Add(5*time.Second)); !slices.Equal(got, []string{"U1"}) {
		t.Fatalf("got %v", got)
	}
	if got := c.Typing("C1", nil, t0.Add(6*time.Second)); len(got) != 0 {
		t.Fatalf("expired, got %v", got)
	}
	now = t0.Add(10 * time.Second)
	c.Apply(s, realtime.Event{Type: "typing", Data: mustJSON(t, realtime.Typing{ChannelID: "C1", UserID: "U1", ExpiresInMS: 6000})}) // zero TS is fine
	if got := c.Typing("C1", nil, now.Add(time.Second)); !slices.Equal(got, []string{"U1"}) {
		t.Fatalf("zero TS, got %v", got)
	}
	root := "R1"
	if got := c.Typing("C1", &root, t0); len(got) != 0 {
		t.Fatal("thread scope is separate")
	}
}

func TestTypingFirstSeenOrderAndThreadScope(t *testing.T) {
	s := store.New()
	c := Get(s)
	t0 := time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)
	c.SetClock(func() time.Time { return t0 })
	root := "R1"
	for _, u := range []string{"U2", "U1", "U2"} {
		c.Apply(s, realtime.Event{Type: "typing", Data: mustJSON(t, realtime.Typing{ChannelID: "C1", UserID: u, ExpiresInMS: 6000})})
	}
	c.Apply(s, realtime.Event{Type: "typing", Data: mustJSON(t, realtime.Typing{ChannelID: "C1", ThreadRootID: &root, UserID: "U3", ExpiresInMS: 6000})})
	if got := c.Typing("C1", nil, t0); !slices.Equal(got, []string{"U2", "U1"}) {
		t.Fatalf("first-seen order, got %v", got)
	}
	if got := c.Typing("C1", &root, t0); !slices.Equal(got, []string{"U3"}) {
		t.Fatalf("thread scope, got %v", got)
	}
	// Blocking after the fact hides the typer at read time.
	s.Blocks["U1"] = time.Time{}
	if got := c.Typing("C1", nil, t0); !slices.Equal(got, []string{"U2"}) {
		t.Fatalf("blocked filtered at read, got %v", got)
	}
}

func TestOpenStateAndSelection(t *testing.T) {
	c := Get(store.New())
	c.SetSelectedSpace("S1")
	c.SetActiveChannel("C1")
	if c.SelectedSpace() != "S1" || c.ActiveChannel() != "C1" {
		t.Fatalf("got %q %q", c.SelectedSpace(), c.ActiveChannel())
	}
	c.SetOpen("C1", "m20", false)
	if st, ok := c.open["C1"]; !ok || st.anchor != "m20" || st.atBottom {
		t.Fatalf("open state %+v %v", st, ok)
	}
	c.SetClosed("C1")
	if _, ok := c.open["C1"]; ok {
		t.Fatal("closed")
	}
	c.SetThreadOpen("R1", true)
	if th := c.threads["R1"]; th == nil || !th.open {
		t.Fatal("thread open")
	}
	c.SetThreadOpen("R1", false)
	if th := c.threads["R1"]; th != nil && th.open {
		t.Fatal("thread closed")
	}
}

func TestResetClearsEverything(t *testing.T) {
	s := store.New()
	c := Get(s)
	t0 := time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)
	c.SetClock(func() time.Time { return t0 })
	c.AddPending("C1", PendingMessage{Nonce: "P1", Kind: "poll"})
	c.SetPendingPoll("C1", "P1", &api.PollInput{Question: "q"})
	c.Window("C1")
	c.SetOpen("C1", "", true)
	c.SetThreadOpen("R1", true)
	c.SetSelectedSpace("S1")
	c.SetActiveChannel("C1")
	c.Apply(s, realtime.Event{Type: "typing", Data: mustJSON(t, realtime.Typing{ChannelID: "C1", UserID: "U1", ExpiresInMS: 6000})})
	s.Reset() // store reset notifies every reducer
	if c.HasWindow("C1") || len(c.Pending("C1")) != 0 || c.PendingPoll("C1", "P1") != nil {
		t.Fatal("windows and pending cleared")
	}
	if c.SelectedSpace() != "" || c.ActiveChannel() != "" {
		t.Fatal("selection cleared")
	}
	if len(c.Typing("C1", nil, t0)) != 0 || len(c.open) != 0 || len(c.threads) != 0 {
		t.Fatal("typing, open and threads cleared")
	}
	if Get(s) != c {
		t.Fatal("instance kept across reset")
	}
}
