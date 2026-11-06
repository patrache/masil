package chatstore

import (
	"slices"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
)

// events.md 6.4: events of different requests can cross each other and REST
// results; none of these orderings may regress what we hold.

func editedAt(id, body string, d time.Duration) api.Message {
	m := msg(id, "U2")
	m.Body = body
	at := t0.Add(d)
	m.EditedAt = &at
	return m
}

func TestUpdatedAfterTombstoneKeepsTombstone(t *testing.T) {
	s, c := loadedStore(t)
	root := get(t, c, "C1", "m20")
	c.thread("m20").root = &root
	c.SetPins("C1", []api.Message{root})
	apply(t, s, "message.deleted", realtime.MessageDeleted{ChannelID: "C1", MessageID: "m20", Tombstone: true})
	apply(t, s, "message.updated", realtime.MessageEvent{Message: editedAt("m20", "edited", time.Minute)})
	if got := get(t, c, "C1", "m20"); !got.Deleted || got.Body != "" {
		t.Fatalf("late message.updated revived a tombstone: %+v", got)
	}
	if r, _ := c.Thread("m20"); r == nil || !r.Deleted || r.Body != "" {
		t.Fatalf("thread root copy revived: %+v", r)
	}
	if p, _ := c.Pins("C1"); len(p) != 0 {
		t.Fatalf("pin list revived: %v", ids(p))
	}
}

func TestOlderEditNeverRollsBack(t *testing.T) {
	s, c := loadedStore(t)
	root := get(t, c, "C1", "m20")
	c.thread("m20").root = &root
	c.SetPins("C1", []api.Message{root})
	apply(t, s, "message.updated", realtime.MessageEvent{Message: editedAt("m20", "v2", 2*time.Minute)})
	apply(t, s, "message.updated", realtime.MessageEvent{Message: editedAt("m20", "v1", time.Minute)})
	if got := get(t, c, "C1", "m20"); got.Body != "v2" {
		t.Fatalf("older edit event rolled the window copy back: %q", got.Body)
	}
	if r, _ := c.Thread("m20"); r.Body != "v2" {
		t.Fatalf("older edit rolled the thread root copy back: %q", r.Body)
	}
	if p, _ := c.Pins("C1"); p[0].Body != "v2" {
		t.Fatalf("older edit rolled the pin copy back: %q", p[0].Body)
	}

	// the REST edit result first, the event of an older edit after it
	c.UpsertMessage(editedAt("m30", "v2", 2*time.Minute))
	apply(t, s, "message.updated", realtime.MessageEvent{Message: editedAt("m30", "v1", time.Minute)})
	if got := get(t, c, "C1", "m30"); got.Body != "v2" {
		t.Fatalf("older edit event rolled the REST result back: %q", got.Body)
	}
	// a same-version update (e.g. a poll attachment change) still applies
	same := editedAt("m30", "v2", 2*time.Minute)
	same.Kind = "poll"
	apply(t, s, "message.updated", realtime.MessageEvent{Message: same})
	if got := get(t, c, "C1", "m30"); got.Kind != "poll" {
		t.Fatalf("an update of the same edit must apply: %+v", got)
	}
}

// A page read after an edit replays the older edit event logged while it was in flight.
func TestPageReplayDoesNotRollBackEdit(t *testing.T) {
	s, c := newStore(t)
	tk := c.Ticket("C1")
	apply(t, s, "message.updated", realtime.MessageEvent{Message: editedAt("m20", "v1", time.Minute)})
	c.ApplyPageFor("C1", tk, api.MessageQuery{Limit: 50}, page(false, false, msg("m10", "U1"), editedAt("m20", "v2", 2*time.Minute)))
	if got := get(t, c, "C1", "m20"); got.Body != "v2" {
		t.Fatalf("replay rolled the page copy back: %q", got.Body)
	}
}

func TestDeletedBeforeCreatedStaysDeleted(t *testing.T) {
	for _, tomb := range []bool{false, true} {
		s, c := loadedStore(t)
		m := msg("m40", "U1")
		m.ClientNonce = strp("N1")
		c.AddPending("C1", PendingMessage{Nonce: "N1", Body: "body m40", Kind: "text", SentAt: t0})
		apply(t, s, "message.deleted", realtime.MessageDeleted{ChannelID: "C1", MessageID: "m40", Tombstone: tomb})
		apply(t, s, "message.created", realtime.MessageEvent{Message: m})
		got, held := c.Window("C1").Get("m40")
		switch {
		case !tomb && held:
			t.Fatalf("tombstone=false: late creation inserted a removed message: %+v", got)
		case tomb && (!held || !got.Deleted || got.Body != ""):
			t.Fatalf("tombstone=true: late creation must show the placeholder, got %+v held=%v", got, held)
		}
		if len(c.Pending("C1")) != 0 {
			t.Fatalf("tombstone=%v: the pending send is still settled", tomb)
		}
		// a later update or REST copy of the deleted id never revives it either
		apply(t, s, "message.updated", realtime.MessageEvent{Message: editedAt("m40", "edited", time.Minute)})
		c.UpsertMessage(msg("m40", "U1"))
		got, held = c.Window("C1").Get("m40")
		switch {
		case !tomb && held:
			t.Fatalf("tombstone=false: revived by update / REST: %+v", got)
		case tomb && (!held || !got.Deleted || got.Body != ""):
			t.Fatalf("tombstone=true: revived by update / REST: %+v", got)
		}
		if m := c.TakeMissing("C1"); slices.Contains(m, "m40") {
			t.Fatalf("tombstone=%v: a deleted id is never fetched as missing: %v", tomb, m)
		}
	}
}

// The deleted-id memory is bounded per channel.
func TestDeletedIDsBounded(t *testing.T) {
	s, c := loadedStore(t)
	for i := range maxDeleted + 10 {
		apply(t, s, "message.deleted", realtime.MessageDeleted{ChannelID: "C1", MessageID: "x" + time.Duration(i).String()})
	}
	if n := len(c.deleted["C1"].ids); n != maxDeleted {
		t.Fatalf("deleted ids kept %d, want %d", n, maxDeleted)
	}
	if c.deleted["C1"].has("x0s") || !c.deleted["C1"].has("x"+time.Duration(maxDeleted+9).String()) {
		t.Fatal("the oldest ids leave first")
	}
	apply(t, s, "channel.deleted", realtime.ChannelDeleted{ChannelID: "C1", Reason: "deleted"})
	if c.deleted["C1"] != nil {
		t.Fatal("dropped with its channel")
	}
}

func TestOlderThreadUpdatedIgnored(t *testing.T) {
	s, c := loadedStore(t)
	root := get(t, c, "C1", "m20")
	c.thread("m20").root = &root
	at := func(d time.Duration) *time.Time { x := t0.Add(d); return &x }
	apply(t, s, "thread.updated", realtime.ThreadUpdated{ChannelID: "C1", RootID: "m20", Thread: api.ThreadSummary{ReplyCount: 3, LastReplyAt: at(3 * time.Minute)}})
	apply(t, s, "thread.updated", realtime.ThreadUpdated{ChannelID: "C1", RootID: "m20", Thread: api.ThreadSummary{ReplyCount: 2, LastReplyAt: at(2 * time.Minute)}})
	if got := get(t, c, "C1", "m20"); got.Thread == nil || got.Thread.ReplyCount != 3 {
		t.Fatalf("older thread.updated overwrote the window root: %+v", got.Thread)
	}
	if r, _ := c.Thread("m20"); r.Thread == nil || r.Thread.ReplyCount != 3 {
		t.Fatalf("older thread.updated overwrote the thread root: %+v", r.Thread)
	}
	// a deletion keeps last_reply_at (or moves it back with the newest reply gone) and still applies
	apply(t, s, "thread.updated", realtime.ThreadUpdated{ChannelID: "C1", RootID: "m20", Thread: api.ThreadSummary{ReplyCount: 2, LastReplyAt: at(3 * time.Minute)}})
	if got := get(t, c, "C1", "m20"); got.Thread.ReplyCount != 2 {
		t.Fatalf("same last_reply_at applies: %+v", got.Thread)
	}
}

// ---- pins ----

// A pin list snapshot replays the pin events committed while ListPins was in flight.
func TestSetPinsForReplaysPinEvents(t *testing.T) {
	s, c := loadedStore(t)
	tk := c.PinsTicket()
	apply(t, s, "message.unpinned", map[string]any{"channel_id": "C1", "message_id": "m20", "unpinned_by": "U1"})
	pinned := get(t, c, "C1", "m20")
	pinned.Pinned = true
	c.SetPinsFor("C1", tk, []api.Message{pinned})
	if p, loaded := c.Pins("C1"); !loaded || len(p) != 0 {
		t.Fatalf("an unpin in flight left a stale pin: %v", ids(p))
	}

	tk = c.PinsTicket()
	apply(t, s, "message.pinned", map[string]any{"channel_id": "C1", "message_id": "m30", "pinned_by": "U1"})
	c.SetPinsFor("C1", tk, []api.Message{})
	if p, _ := c.Pins("C1"); !slices.Equal(ids(p), []string{"m30"}) || !p[0].Pinned {
		t.Fatalf("a pin in flight never reached the list: %v", ids(p))
	}

	// a message deleted in flight leaves the snapshot; other channels' events do not touch it
	tk = c.PinsTicket()
	apply(t, s, "message.deleted", realtime.MessageDeleted{ChannelID: "C1", MessageID: "m10", Tombstone: true})
	apply(t, s, "message.unpinned", map[string]any{"channel_id": "C2", "message_id": "m30", "unpinned_by": "U1"})
	m10 := msg("m10", "U1")
	m10.Pinned = true
	m30 := get(t, c, "C1", "m30")
	c.SetPinsFor("C1", tk, []api.Message{m30, m10})
	if p, _ := c.Pins("C1"); !slices.Equal(ids(p), []string{"m30"}) {
		t.Fatalf("pins %v", ids(p))
	}
}

// A pin event for a message held only in the pin list moves it to the front
// instead of dropping the whole list.
func TestPinnedHeldOnlyInPinList(t *testing.T) {
	s, c := loadedStore(t)
	old := msg("m05", "U1")
	old.Pinned = true
	c.SetPins("C1", []api.Message{get(t, c, "C1", "m10"), old})
	apply(t, s, "message.pinned", map[string]any{"channel_id": "C1", "message_id": "m05", "pinned_by": "U1"})
	if p, loaded := c.Pins("C1"); !loaded || !slices.Equal(ids(p), []string{"m05", "m10"}) {
		t.Fatalf("pins %v loaded=%v", ids(p), loaded)
	}
}
