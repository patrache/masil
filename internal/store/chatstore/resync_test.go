package chatstore

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
)

// runCmd runs cmd, flattening tea.BatchMsg, and returns the first non-nil message.
func runCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("nil cmd")
	}
	m := cmd()
	if b, ok := m.(tea.BatchMsg); ok {
		for _, c := range b {
			if c == nil {
				continue
			}
			if r := runCmd(t, c); r != nil {
				return r
			}
		}
		return nil
	}
	return m
}

// queriesOf maps request path -> raw query.
func queriesOf(rs []apitest.Recorded) map[string]string {
	out := map[string]string{}
	for _, r := range rs {
		out[r.Path] = r.Query
	}
	return out
}

func msgAt(id, channelID string) api.Message {
	m := msg(id, "U1")
	m.ChannelID = channelID
	return m
}

func idStr(ms []api.Message) string { return strings.Join(ids(ms), ",") }

func TestResyncRefetchesOpenWindows(t *testing.T) {
	srv := apitest.New(t)
	srv.JSON("GET /v1/channels/C1/messages", 200, map[string]any{"items": []api.Message{msgAt("m50", "C1")}, "has_more_before": true, "has_more_after": false})
	srv.JSON("GET /v1/channels/C2/messages", 200, map[string]any{"items": []api.Message{msgAt("m20", "C2")}, "has_more_before": true, "has_more_after": true})
	srv.JSON("GET /v1/messages/R1/thread", 200, map[string]any{"root": msgAt("R1", "C1"), "items": []api.Message{}, "has_more_before": false, "has_more_after": false})
	s := store.New()
	c := Get(s)
	c.Window("C1")
	c.SetOpen("C1", "", true) // bottom => latest page
	c.Window("C2")
	c.SetOpen("C2", "m20", false) // scrolled => ?around=m20
	c.Window("C3")                // not open => dropped immediately
	c.SetThreadOpen("R1", true)
	msg := runCmd(t, c.Resync(s, srv.Client())).(ResyncResultMsg)
	if c.HasWindow("C3") {
		t.Fatal("closed cache must be dropped at Resync time")
	}
	q := queriesOf(srv.Requests()) // path -> raw query (api.Client encodes with url.Values.Encode: keys sorted)
	if q["/v1/channels/C1/messages"] != "limit=50" || q["/v1/channels/C2/messages"] != "around=m20&limit=50" || q["/v1/messages/R1/thread"] != "limit=50" {
		t.Fatalf("%v", q)
	}
	c.ApplyResync(msg)
	c.ApplyResync(msg) // idempotent
	if idStr(c.Window("C1").Items()) != "m50" || !c.Loaded("C2") {
		t.Fatal("windows replaced")
	}
	if root, _ := c.Thread("R1"); root == nil || root.ID != "R1" {
		t.Fatalf("thread root replaced: %+v", root)
	}
}

func TestResyncDropsClosedCaches(t *testing.T) {
	s, c := loadedStore(t)
	c.Window("C2").Replace([]api.Message{msgAt("m5", "C2")}, false, false)
	c.loaded["C2"] = true
	c.SetPins("C1", []api.Message{msg("m20", "U2")})
	c.SetPins("C2", []api.Message{msgAt("m5", "C2")})
	c.AddPending("C2", PendingMessage{Nonce: "N1", Body: "in flight", Kind: "text", SentAt: t0})
	c.SetOpen("C1", "", true)
	c.SetThreadOpen("m30", true)
	c.thread("m10") // a closed thread cache
	if cmd := c.Resync(s, nil); cmd != nil {
		t.Fatal("no client => nothing to fetch")
	}
	if c.HasWindow("C2") || c.Loaded("C2") {
		t.Fatal("closed channel window dropped")
	}
	if !c.HasWindow("C1") || !c.Loaded("C1") {
		t.Fatal("open window kept until its page lands")
	}
	if _, ok := c.threads["m10"]; ok {
		t.Fatal("closed thread dropped")
	}
	if _, ok := c.threads["m30"]; !ok {
		t.Fatal("open thread kept")
	}
	if _, loaded := c.Pins("C1"); loaded {
		t.Fatal("pin lists are message caches too: refetched on next open")
	}
	if _, loaded := c.Pins("C2"); loaded {
		t.Fatal("pin list of a closed channel dropped")
	}
	if len(c.Pending("C2")) != 1 {
		t.Fatal("optimistic sends survive resync")
	}
}

func TestResyncReplaysEventsInFlight(t *testing.T) {
	srv := apitest.New(t)
	// The snapshot was read before m35 was created and before U2 reacted on m20.
	srv.JSON("GET /v1/channels/C1/messages", 200, map[string]any{"items": []api.Message{msg("m20", "U2"), msg("m30", "U1")}, "has_more_before": true, "has_more_after": false})
	s, c := loadedStore(t)
	c.SetOpen("C1", "", true)
	cmd := c.Resync(s, srv.Client())
	res := runCmd(t, cmd).(ResyncResultMsg)
	apply(t, s, "message.created", realtime.MessageEvent{Message: msg("m35", "U1")})
	apply(t, s, "reaction.added", realtime.ReactionEvent{ChannelID: "C1", MessageID: "m20", Emoji: "👍", UserID: "U2", Count: 1})
	apply(t, s, "typing", realtime.Typing{ChannelID: "C1", UserID: "U3", ExpiresInMS: 6000})
	c.ApplyResync(res)
	if idStr(c.Window("C1").Items()) != "m20,m30,m35" {
		t.Fatalf("events since Resync are re-applied over the snapshot: %s", idStr(c.Window("C1").Items()))
	}
	if r := get(t, c, "C1", "m20").Reactions; !slices.Equal(r, []api.ReactionSummary{{Emoji: "👍", Count: 1}}) {
		t.Fatalf("reaction re-applied: %+v", r)
	}
	// After the result landed, events apply once as usual.
	apply(t, s, "message.created", realtime.MessageEvent{Message: msg("m40", "U1")})
	if idStr(c.Window("C1").Items()) != "m20,m30,m35,m40" {
		t.Fatalf("live after resync: %s", idStr(c.Window("C1").Items()))
	}
}

func TestResyncStaleGenerationIgnored(t *testing.T) {
	n := 0
	srv := apitest.New(t)
	srv.Handle("GET /v1/channels/C1/messages", func(w http.ResponseWriter, r *http.Request) {
		n++
		id := "m50"
		if n == 2 {
			id = "m60"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []api.Message{msg(id, "U1")}, "has_more_before": true, "has_more_after": false})
	})
	s, c := loadedStore(t)
	c.SetOpen("C1", "", true)
	first := runCmd(t, c.Resync(s, srv.Client())).(ResyncResultMsg)
	second := runCmd(t, c.Resync(s, srv.Client())).(ResyncResultMsg)
	if second.Gen <= first.Gen {
		t.Fatalf("generations %d %d", first.Gen, second.Gen)
	}
	c.ApplyResync(second)
	c.ApplyResync(first) // late older result must not roll back
	if idStr(c.Window("C1").Items()) != "m60" {
		t.Fatalf("got %s", idStr(c.Window("C1").Items()))
	}
}

func TestResyncResultIgnoredAfterReset(t *testing.T) {
	srv := apitest.New(t)
	srv.JSON("GET /v1/channels/C1/messages", 200, map[string]any{"items": []api.Message{msg("m50", "U1")}, "has_more_before": false, "has_more_after": false})
	s, c := loadedStore(t)
	c.SetOpen("C1", "", true)
	res := runCmd(t, c.Resync(s, srv.Client())).(ResyncResultMsg)
	s.Reset() // logout / user switch while the refetch was in flight
	c.ApplyResync(res)
	if c.HasWindow("C1") {
		t.Fatal("a result from before Reset must not repopulate the new session")
	}
}

func TestResyncHandBuiltResultApplies(t *testing.T) {
	// UI tests send a hand-built ResyncResultMsg{Gen: 1}; a Resync that fetched
	// nothing must not consume generation 1.
	s := store.New()
	c := Get(s)
	if cmd := c.Resync(s, nil); cmd != nil {
		t.Fatal("nothing open")
	}
	c.ApplyResync(ResyncResultMsg{Gen: 1, Channels: map[string]*api.MessageWindow{"C1": page(false, false, msg("m10", "U1"))}})
	if idStr(c.Window("C1").Items()) != "m10" || !c.Loaded("C1") {
		t.Fatal("hand-built result applied")
	}
}
