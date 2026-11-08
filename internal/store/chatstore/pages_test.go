package chatstore

import (
	"net/http"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
)

// newStore: me = ME, C1 known to core, nothing loaded.
func newStore(t *testing.T) (*store.Store, *ChatStore) {
	t.Helper()
	s := store.New()
	s.Me = &api.Me{UserSummary: api.UserSummary{ID: "ME"}}
	s.Channels["C1"] = &api.Channel{ID: "C1", Kind: "text"}
	c := Get(s)
	c.SetClock(func() time.Time { return t0 })
	return s, c
}

func mine(id, nonce string) api.Message {
	m := msg(id, "ME")
	m.ClientNonce = strp(nonce)
	return m
}

// A message created while the first page is in flight is not in it (the page
// was read before the commit): it is replayed over the page.
func TestFirstPageReplaysEventsInFlight(t *testing.T) {
	s, c := newStore(t)
	tk := c.Ticket("C1")
	apply(t, s, "message.created", realtime.MessageEvent{Message: msg("m95", "U2")})
	if !c.ApplyPageFor("C1", tk, api.MessageQuery{Limit: 50}, page(true, false, msg("m80", "U1"), msg("m90", "U1"))) {
		t.Fatal("first page applied")
	}
	if got := idStr(c.Window("C1").Items()); got != "m80,m90,m95" {
		t.Fatalf("items %s", got)
	}
	// later pages do not replay what was journaled before their own ticket
	tk = c.Ticket("C1")
	c.ApplyPageFor("C1", tk, api.MessageQuery{Limit: 50}, page(true, false, msg("m80", "U1"), msg("m90", "U1")))
	if got := idStr(c.Window("C1").Items()); got != "m80,m90" {
		t.Fatalf("a newer snapshot is not patched with older events: %s", got)
	}
}

// A reply created while the thread's first page is in flight survives its Replace.
func TestThreadFirstPageReplaysEventsInFlight(t *testing.T) {
	s, c := newStore(t)
	c.SetThreadOpen("R1", true)
	tk := c.ThreadTicket("R1")
	apply(t, s, "message.created", realtime.MessageEvent{Message: reply("m52", "U2", "R1")})
	if !c.ApplyThreadPageFor("R1", tk, api.ThreadQuery{Limit: 50}, &api.ThreadWindow{Root: msg("R1", "U1"), Items: []api.Message{reply("m51", "U1", "R1")}}) {
		t.Fatal("thread page applied")
	}
	if _, replies := c.Thread("R1"); idStr(replies.Items()) != "m51,m52" {
		t.Fatalf("replies %s", idStr(replies.Items()))
	}
}

// Sending while scrolled up: the send result and its event are past the
// window's end, the latest page was read before the commit — the message must
// still end up shown, and its pending row must stay until it is.
func TestSendWhileScrolledUpSurvivesLatestPage(t *testing.T) {
	s, c := newStore(t)
	c.ApplyPage("C1", api.MessageQuery{Around: "m20"}, page(true, true, msg("m10", "U1"), msg("m20", "U1"), msg("m30", "U1")))
	c.AddPending("C1", PendingMessage{Nonce: "N1", Body: "body m99", Kind: "text", SentAt: t0})
	tk := c.Ticket("C1") // afterSend: GET latest, alongside the POST
	c.UpsertMessage(mine("m99", "N1"))
	if len(c.Pending("C1")) != 1 {
		t.Fatal("an unplaced send keeps its pending row until a page shows it")
	}
	apply(t, s, "message.created", realtime.MessageEvent{Message: mine("m99", "N1")})
	if len(c.Pending("C1")) != 1 {
		t.Fatal("an unplaced created event keeps the pending row too")
	}
	c.ApplyPageFor("C1", tk, api.MessageQuery{Limit: 50}, page(true, false, msg("m80", "U1"), msg("m90", "U1")))
	if got := idStr(c.Window("C1").Items()); got != "m80,m90,m99" {
		t.Fatalf("my message is shown: %s", got)
	}
	if len(c.Pending("C1")) != 0 {
		t.Fatalf("pending dropped once shown: %+v", c.Pending("C1"))
	}
}

// Same without the realtime event (offline socket): the REST result alone is replayed.
func TestSendResultReplayedWithoutEvent(t *testing.T) {
	_, c := newStore(t)
	c.ApplyPage("C1", api.MessageQuery{Around: "m20"}, page(true, true, msg("m10", "U1"), msg("m20", "U1")))
	c.AddPending("C1", PendingMessage{Nonce: "N1", Body: "body m99", Kind: "text", SentAt: t0})
	tk := c.Ticket("C1")
	c.UpsertMessage(mine("m99", "N1"))
	c.ApplyPageFor("C1", tk, api.MessageQuery{Limit: 50}, page(true, false, msg("m80", "U1")))
	if got := idStr(c.Window("C1").Items()); got != "m80,m99" || len(c.Pending("C1")) != 0 {
		t.Fatalf("items %s pending %+v", got, c.Pending("C1"))
	}
}

// A Before / After page built from one window never lands in the window that replaced it.
func TestStalePageOfReplacedWindowDiscarded(t *testing.T) {
	s, c := newStore(t)
	c.ApplyPage("C1", api.MessageQuery{Around: "m20"}, page(true, true, msg("m10", "U1"), msg("m20", "U1"), msg("m30", "U1")))
	after := c.Ticket("C1")
	before := c.Ticket("C1")
	c.ApplyPage("C1", api.MessageQuery{Limit: 50}, page(true, false, msg("m80", "U1"), msg("m90", "U1"))) // latest replaces it
	if c.Current("C1", after) {
		t.Fatal("the ticket of a replaced window is not current")
	}
	if c.ApplyPageFor("C1", after, api.MessageQuery{After: "m20", Limit: 50}, page(true, true, msg("m30", "U1"), msg("m40", "U1"))) {
		t.Fatal("stale after page discarded")
	}
	if c.ApplyPageFor("C1", before, api.MessageQuery{Before: "m10", Limit: 50}, page(false, true, msg("m05", "U1"))) {
		t.Fatal("stale before page discarded")
	}
	w := c.Window("C1")
	if idStr(w.Items()) != "m80,m90" || w.HasMoreAfter() || !w.HasMoreBefore() {
		t.Fatalf("window untouched: %s after=%v before=%v", idStr(w.Items()), w.HasMoreAfter(), w.HasMoreBefore())
	}
	apply(t, s, "message.created", realtime.MessageEvent{Message: msg("m95", "U2")})
	if idStr(w.Items()) != "m80,m90,m95" {
		t.Fatalf("still live at the bottom: %s", idStr(w.Items()))
	}
	// a page of the current window still merges
	cur := c.Ticket("C1")
	if !c.ApplyPageFor("C1", cur, api.MessageQuery{Before: "m80", Limit: 50}, page(false, false, msg("m70", "U1"))) || idStr(w.Items()) != "m70,m80,m90,m95" {
		t.Fatalf("current before page merged: %s", idStr(w.Items()))
	}
}

func TestStaleThreadPageDiscarded(t *testing.T) {
	_, c := newStore(t)
	c.SetThreadOpen("R1", true)
	root := msg("R1", "U1")
	c.ApplyThreadPage("R1", api.ThreadQuery{}, &api.ThreadWindow{Root: root, Items: []api.Message{reply("m51", "U1", "R1")}, HasMoreAfter: true})
	after := c.ThreadTicket("R1")
	c.ApplyThreadPage("R1", api.ThreadQuery{}, &api.ThreadWindow{Root: root, Items: []api.Message{reply("m80", "U1", "R1")}})
	if c.ApplyThreadPageFor("R1", after, api.ThreadQuery{After: "m51"}, &api.ThreadWindow{Root: root, Items: []api.Message{reply("m52", "U1", "R1")}, HasMoreAfter: true}) {
		t.Fatal("stale thread page discarded")
	}
	if _, replies := c.Thread("R1"); idStr(replies.Items()) != "m80" || replies.HasMoreAfter() {
		t.Fatalf("replies %s", idStr(replies.Items()))
	}
}

// Every Replace (and a drop) changes the window generation.
func TestWindowGenerations(t *testing.T) {
	s, c := newStore(t)
	g0 := c.Ticket("C1")
	c.ApplyPage("C1", api.MessageQuery{}, page(false, false, msg("m10", "U1")))
	g1 := c.Ticket("C1")
	if c.Current("C1", g0) || !c.Current("C1", g1) {
		t.Fatal("replace bumps")
	}
	c.ApplyPage("C1", api.MessageQuery{Before: "m10"}, page(false, false, msg("m05", "U1")))
	if !c.Current("C1", g1) {
		t.Fatal("merging pages keep the generation")
	}
	delete(s.Channels, "C1")
	apply(t, s, "channel.deleted", realtime.ChannelDeleted{ChannelID: "C1"})
	if c.Current("C1", g1) {
		t.Fatal("drop bumps")
	}
}

// A refetch result nobody applied leaves the window stale (not Fresh), so the
// next conversation view refetches; a later resync journals as usual.
func TestUnappliedResyncLeavesWindowStale(t *testing.T) {
	srv := apitest.New(t)
	srv.JSON("GET /v1/channels/C1/messages", 200, map[string]any{"items": []api.Message{msg("m50", "U1")}, "has_more_before": false, "has_more_after": false})
	s, c := loadedStore(t)
	c.SetOpen("C1", "", true)
	if !c.Fresh("C1") {
		t.Fatal("precondition: fresh")
	}
	_ = runCmd(t, c.Resync(s, srv.Client())) // the result is dropped (no live view)
	if !c.Loaded("C1") || c.Fresh("C1") {
		t.Fatal("kept to show, but stale until a page replaces it")
	}
	for i := 0; i < maxLog+10; i++ { // no flight left open to overflow
		apply(t, s, "reaction.added", realtime.ReactionEvent{ChannelID: "C1", MessageID: "m10", Emoji: "👍", UserID: "U2", Count: 1})
	}
	res := runCmd(t, c.Resync(s, srv.Client())).(ResyncResultMsg)
	apply(t, s, "message.created", realtime.MessageEvent{Message: msg("m60", "U2")})
	c.ApplyResync(res)
	if idStr(c.Window("C1").Items()) != "m50,m60" || !c.Fresh("C1") {
		t.Fatalf("the later resync replays its in-flight events: %s", idStr(c.Window("C1").Items()))
	}
	// a first page also clears the stale mark
	c.SetOpen("C1", "", true)
	_ = runCmd(t, c.Resync(s, srv.Client()))
	c.ApplyPage("C1", api.MessageQuery{}, page(false, false, msg("m50", "U1")))
	if !c.Fresh("C1") {
		t.Fatal("a replacing page is fresh")
	}
}

// Each refetch has its own deadline: one slow channel fails alone.
func TestResyncPerRequestTimeout(t *testing.T) {
	old := resyncRequestTimeout
	resyncRequestTimeout = 100 * time.Millisecond
	t.Cleanup(func() { resyncRequestTimeout = old })
	srv := apitest.New(t)
	srv.Handle("GET /v1/channels/C1/messages", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	})
	for _, ch := range []string{"C2", "C3", "C4", "C5", "C6"} {
		srv.JSON("GET /v1/channels/"+ch+"/messages", 200, map[string]any{"items": []api.Message{msgAt("m50", ch)}, "has_more_before": false, "has_more_after": false})
	}
	s, c := newStore(t)
	for _, ch := range []string{"C1", "C2", "C3", "C4", "C5", "C6"} {
		c.ApplyPage(ch, api.MessageQuery{}, page(false, false, msgAt("m10", ch)))
		c.SetOpen(ch, "", true)
	}
	start := time.Now()
	res := runCmd(t, c.Resync(s, srv.Client())).(ResyncResultMsg)
	if res.Errs["C1"] == nil || len(res.Channels) != 5 {
		t.Fatalf("only the slow channel fails: errs %v channels %d", res.Errs, len(res.Channels))
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("requests are bounded per request and run side by side: %v", d)
	}
}

// A failed refetch drops the window (no stale window marked loaded) and the thread.
func TestResyncErrorsDropWindow(t *testing.T) {
	srv := apitest.New(t)
	srv.Error("GET /v1/channels/C1/messages", 503, "unavailable")
	srv.Error("GET /v1/messages/m30/thread", 503, "unavailable")
	s, c := loadedStore(t)
	c.SetOpen("C1", "", true)
	c.SetThreadOpen("m30", true)
	c.AddPending("C1", PendingMessage{Nonce: "N1", Body: "x", Kind: "text", SentAt: t0})
	c.ApplyThreadPage("m30", api.ThreadQuery{Limit: 50}, &api.ThreadWindow{Root: msg("m30", "U1"), Items: []api.Message{reply("m31", "U1", "m30")}})
	res := runCmd(t, c.Resync(s, srv.Client())).(ResyncResultMsg)
	if res.ChannelErr("C1") == nil || res.ThreadErr("m30") == nil {
		t.Fatalf("errors %+v", res)
	}
	c.ApplyResync(res)
	if c.Loaded("C1") || c.HasWindow("C1") {
		t.Fatal("a failed refetch drops the window: the view refetches")
	}
	if root, replies := c.Thread("m30"); root != nil || replies.Len() != 0 {
		t.Fatal("a failed thread refetch drops the thread")
	}
	if len(c.Pending("C1")) != 1 {
		t.Fatal("optimistic sends survive")
	}
	apply(t, s, "message.created", realtime.MessageEvent{Message: msg("m40", "U1")})
	if c.HasWindow("C1") {
		t.Fatal("events do not resurrect it")
	}
}

// SetClosed / SetThreadOpen(false) (an instance released) take the window off the refetch list.
func TestClosedWindowsNotRefetched(t *testing.T) {
	srv := apitest.New(t)
	srv.JSON("GET /v1/channels/C1/messages", 200, map[string]any{"items": []api.Message{}, "has_more_before": false, "has_more_after": false})
	s, c := loadedStore(t)
	c.SetOpen("C1", "", true)
	c.SetThreadOpen("m30", true)
	if !c.IsOpen("C1") {
		t.Fatal("open")
	}
	c.SetClosed("C1")
	c.SetThreadOpen("m30", false)
	if c.IsOpen("C1") {
		t.Fatal("closed")
	}
	if cmd := c.Resync(s, srv.Client()); cmd != nil {
		t.Fatal("nothing open: nothing refetched")
	}
	if c.HasWindow("C1") {
		t.Fatal("closed window dropped")
	}
}
