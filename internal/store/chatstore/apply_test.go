package chatstore

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
)

var t0 = time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)

func strp(s string) *string { return &s }

// msg builds a top-level text message in C1; ID order matches creation order.
func msg(id, author string) api.Message {
	n := 0
	for _, r := range id {
		if r >= '0' && r <= '9' {
			n = n*10 + int(r-'0')
		}
	}
	return api.Message{
		ID:        id,
		ChannelID: "C1",
		Author:    api.UserSummary{ID: author, Handle: strings.ToLower(author)},
		Kind:      "text",
		Body:      "body " + id,
		CreatedAt: t0.Add(time.Duration(n) * time.Second),
	}
}

func reply(id, author, root string) api.Message {
	m := msg(id, author)
	m.ThreadRootID = strp(root)
	return m
}

// loadedStore: me = ME, clock = t0, C1 known to core, C1 window [m10 m20 m30] loaded.
func loadedStore(t *testing.T) (*store.Store, *ChatStore) {
	t.Helper()
	s := store.New()
	s.Me = &api.Me{UserSummary: api.UserSummary{ID: "ME"}}
	s.Channels["C1"] = &api.Channel{ID: "C1", Kind: "text"}
	c := Get(s)
	c.SetClock(func() time.Time { return t0 })
	c.Window("C1").Replace([]api.Message{msg("m10", "U1"), msg("m20", "U2"), msg("m30", "U1")}, false, false)
	c.loaded["C1"] = true
	return s, c
}

func apply(t *testing.T, s *store.Store, typ string, data any) {
	t.Helper()
	s.Apply(realtime.Event{Type: typ, TS: t0, Data: mustJSON(t, data)})
}

func ids(ms []api.Message) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.ID
	}
	return out
}

func get(t *testing.T, c *ChatStore, ch, id string) api.Message {
	t.Helper()
	m, ok := c.Window(ch).Get(id)
	if !ok {
		t.Fatalf("%s not in %s window", id, ch)
	}
	return m
}

func TestCreatedInsertsByIDOrder(t *testing.T) {
	s, c := loadedStore(t)
	apply(t, s, "message.created", realtime.MessageEvent{Message: msg("m25", "U2")})
	if got := ids(c.Window("C1").Items()); !slices.Equal(got, []string{"m10", "m20", "m25", "m30"}) {
		t.Fatalf("got %v", got)
	}
	w := c.Window("C1")
	w.Replace(w.Items(), true, false) // older history exists on the server
	apply(t, s, "message.created", realtime.MessageEvent{Message: msg("m05", "U2")})
	if _, ok := w.Get("m05"); ok {
		t.Fatal("older than the loaded window must be ignored")
	}
}

func TestCreatedAppendsWhenNoMoreAfter(t *testing.T) {
	s, c := loadedStore(t)
	apply(t, s, "message.created", realtime.MessageEvent{Message: msg("m40", "U1")})
	if got := ids(c.Window("C1").Items()); !slices.Equal(got, []string{"m10", "m20", "m30", "m40"}) {
		t.Fatalf("got %v", got)
	}
	w := c.Window("C1")
	w.Replace(w.Items(), false, true) // scrolled into history: newer pages not loaded
	apply(t, s, "message.created", realtime.MessageEvent{Message: msg("m50", "U1")})
	if _, ok := w.Get("m50"); ok {
		t.Fatal("newer than the window while has_more_after must be ignored")
	}
}

func TestCreatedNotLoadedIgnored(t *testing.T) {
	s, c := loadedStore(t)
	c.Window("C2") // created on demand, no page yet
	m := msg("m40", "U1")
	m.ChannelID = "C2"
	apply(t, s, "message.created", realtime.MessageEvent{Message: m})
	if c.Window("C2").Len() != 0 {
		t.Fatal("a window without a loaded page must not take live messages")
	}
	if c.HasWindow("C9") {
		t.Fatal("precondition")
	}
	m.ChannelID = "C9"
	apply(t, s, "message.created", realtime.MessageEvent{Message: m})
	if c.HasWindow("C9") {
		t.Fatal("events must not create windows")
	}
}

func TestCreatedMergesPendingByNonce(t *testing.T) {
	s, c := loadedStore(t)
	poll := &api.PollInput{Question: "점심", Options: []string{"a", "b"}}
	c.AddPending("C1", PendingMessage{Nonce: "N1", Body: "body m41", Kind: "poll", SentAt: t0})
	c.AddPending("C1", PendingMessage{Nonce: "N2", Body: "other", Kind: "text", SentAt: t0})
	c.SetPendingPoll("C1", "N1", poll)
	m := msg("m41", "ME")
	m.ClientNonce = strp("N1")
	apply(t, s, "message.created", realtime.MessageEvent{Message: m})
	apply(t, s, "message.created", realtime.MessageEvent{Message: m}) // echo to the sending connection too
	if p := c.Pending("C1"); len(p) != 1 || p[0].Nonce != "N2" {
		t.Fatalf("pending %+v", p)
	}
	if c.PendingPoll("C1", "N1") != nil {
		t.Fatal("poll input dropped with the pending item")
	}
	n := 0
	for _, it := range c.Window("C1").Items() {
		if it.ID == "m41" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("m41 present %d times", n)
	}
}

func TestCreatedClearsAuthorTyping(t *testing.T) {
	s, c := loadedStore(t)
	for _, u := range []string{"U1", "U2"} {
		apply(t, s, "typing", realtime.Typing{ChannelID: "C1", UserID: u, ExpiresInMS: 6000})
	}
	apply(t, s, "typing", realtime.Typing{ChannelID: "C1", ThreadRootID: strp("m20"), UserID: "U1", ExpiresInMS: 6000})
	apply(t, s, "message.created", realtime.MessageEvent{Message: msg("m40", "U1")})
	if got := c.Typing("C1", nil, t0); !slices.Equal(got, []string{"U2"}) {
		t.Fatalf("channel typing %v", got)
	}
	if got := c.Typing("C1", strp("m20"), t0); !slices.Equal(got, []string{"U1"}) {
		t.Fatalf("a channel message must not clear thread typing, got %v", got)
	}
	apply(t, s, "message.created", realtime.MessageEvent{Message: reply("m41", "U1", "m20")})
	if got := c.Typing("C1", strp("m20"), t0); len(got) != 0 {
		t.Fatalf("thread typing cleared by a reply, got %v", got)
	}
}

func TestThreadReplyGoesToOpenThreadOnly(t *testing.T) {
	s, c := loadedStore(t)
	c.SetThreadOpen("m20", true)
	apply(t, s, "message.created", realtime.MessageEvent{Message: reply("m35", "U1", "m20")})
	_, replies := c.Thread("m20")
	if got := ids(replies.Items()); !slices.Equal(got, []string{"m35"}) {
		t.Fatalf("replies %v", got)
	}
	if got := ids(c.Window("C1").Items()); !slices.Equal(got, []string{"m10", "m20", "m30"}) {
		t.Fatalf("channel window must not take replies, got %v", got)
	}
	c.SetThreadOpen("m20", false)
	apply(t, s, "message.created", realtime.MessageEvent{Message: reply("m36", "U1", "m20")})
	if _, ok := replies.Get("m36"); ok {
		t.Fatal("closed thread must ignore replies")
	}
	apply(t, s, "message.created", realtime.MessageEvent{Message: reply("m37", "U1", "m10")})
	if _, r := c.Thread("m10"); r.Len() != 0 {
		t.Fatal("never-opened thread ignores replies")
	}
}

func TestUpdatedKeepsLocalReactionMe(t *testing.T) {
	s, c := loadedStore(t)
	c.Window("C1").Update("m20", func(m *api.Message) {
		m.Reactions = []api.ReactionSummary{{Emoji: "👍", Count: 2, Me: true}}
	})
	up := msg("m20", "U2")
	up.Body = "edited"
	up.EditedAt = &t0
	up.Reactions = []api.ReactionSummary{{Emoji: "👍", Count: 3}, {Emoji: "👀", Count: 1}}
	apply(t, s, "message.updated", realtime.MessageEvent{Message: up})
	got := get(t, c, "C1", "m20")
	want := []api.ReactionSummary{{Emoji: "👍", Count: 3, Me: true}, {Emoji: "👀", Count: 1}}
	if got.Body != "edited" || got.EditedAt == nil || !slices.Equal(got.Reactions, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestUpdatedKeepsLocalMyVotesAndThreadRootCopy(t *testing.T) {
	s, c := loadedStore(t)
	p := &api.Poll{ID: "P1", ContextType: "message", ContextID: "m20", Options: []api.PollOption{{}, {}}, MyVotes: []int{1}}
	c.Window("C1").Update("m20", func(m *api.Message) { m.Kind = "poll"; m.Poll = p })
	root := get(t, c, "C1", "m20")
	c.SetThreadOpen("m20", true)
	c.threads["m20"].root = &root
	up := msg("m20", "U2")
	up.Kind = "poll"
	up.Body = "edited"
	up.Poll = &api.Poll{ID: "P1", ContextType: "message", ContextID: "m20", Options: []api.PollOption{{}, {}}, VoterCount: 4, MyVotes: []int{}}
	apply(t, s, "message.updated", realtime.MessageEvent{Message: up})
	got := get(t, c, "C1", "m20")
	if got.Poll.VoterCount != 4 || !slices.Equal(got.Poll.MyVotes, []int{1}) {
		t.Fatalf("poll %+v", got.Poll)
	}
	r, _ := c.Thread("m20")
	if r == nil || r.Body != "edited" || !slices.Equal(r.Poll.MyVotes, []int{1}) {
		t.Fatalf("thread root copy %+v", r)
	}
}

func TestUpdatedUnknownInRangeRecordsMissing(t *testing.T) {
	s, c := loadedStore(t)
	apply(t, s, "message.updated", realtime.MessageEvent{Message: msg("m15", "U1")})
	apply(t, s, "message.updated", realtime.MessageEvent{Message: msg("m15", "U1")})
	if _, ok := c.Window("C1").Get("m15"); ok {
		t.Fatal("unknown messages are fetched, not inserted from updated")
	}
	if got := c.TakeMissing("C1"); !slices.Equal(got, []string{"m15"}) {
		t.Fatalf("missing %v", got)
	}
	if got := c.TakeMissing("C1"); len(got) != 0 {
		t.Fatalf("taken, got %v", got)
	}
	w := c.Window("C1")
	w.Replace(w.Items(), true, true)
	apply(t, s, "message.updated", realtime.MessageEvent{Message: msg("m05", "U1")})
	apply(t, s, "message.updated", realtime.MessageEvent{Message: msg("m99", "U1")})
	if got := c.TakeMissing("C1"); len(got) != 0 {
		t.Fatalf("out of range must be ignored, got %v", got)
	}
}

func TestDeletedTombstoneVsRemove(t *testing.T) {
	s, c := loadedStore(t)
	c.Window("C1").Update("m20", func(m *api.Message) {
		m.Reactions = []api.ReactionSummary{{Emoji: "👍", Count: 1}}
		m.Pinned = true
	})
	c.SetPins("C1", []api.Message{get(t, c, "C1", "m20")})
	apply(t, s, "message.deleted", realtime.MessageDeleted{ChannelID: "C1", MessageID: "m20", Tombstone: true})
	got := get(t, c, "C1", "m20")
	if !got.Deleted || got.Body != "" || len(got.Reactions) != 0 || got.Pinned {
		t.Fatalf("tombstone %+v", got)
	}
	if pins, _ := c.Pins("C1"); len(pins) != 0 {
		t.Fatalf("deleted message leaves the pin list, got %v", ids(pins))
	}
	apply(t, s, "message.deleted", realtime.MessageDeleted{ChannelID: "C1", MessageID: "m30", Tombstone: false})
	if got := ids(c.Window("C1").Items()); !slices.Equal(got, []string{"m10", "m20"}) {
		t.Fatalf("removed, got %v", got)
	}
}

func TestDeletedThreadReply(t *testing.T) {
	s, c := loadedStore(t)
	c.SetThreadOpen("m20", true)
	apply(t, s, "message.created", realtime.MessageEvent{Message: reply("m35", "U1", "m20")})
	apply(t, s, "message.created", realtime.MessageEvent{Message: reply("m36", "U1", "m20")})
	apply(t, s, "message.deleted", realtime.MessageDeleted{ChannelID: "C1", MessageID: "m35", ThreadRootID: strp("m20")})
	apply(t, s, "message.deleted", realtime.MessageDeleted{ChannelID: "C1", MessageID: "m36", ThreadRootID: strp("m20"), Tombstone: true})
	_, replies := c.Thread("m20")
	if got := ids(replies.Items()); !slices.Equal(got, []string{"m36"}) {
		t.Fatalf("replies %v", got)
	}
	if m, _ := replies.Get("m36"); !m.Deleted {
		t.Fatal("tombstoned reply")
	}
}

func TestThreadUpdatedSetsSummary(t *testing.T) {
	s, c := loadedStore(t)
	root := get(t, c, "C1", "m20")
	c.SetThreadOpen("m20", true)
	c.threads["m20"].root = &root
	apply(t, s, "thread.updated", realtime.ThreadUpdated{ChannelID: "C1", RootID: "m20", Thread: api.ThreadSummary{ReplyCount: 4, ParticipantIDs: []string{"U1"}}})
	if got := get(t, c, "C1", "m20"); got.Thread == nil || got.Thread.ReplyCount != 4 {
		t.Fatalf("window root %+v", got.Thread)
	}
	if r, _ := c.Thread("m20"); r == nil || r.Thread == nil || r.Thread.ReplyCount != 4 {
		t.Fatalf("thread root %+v", r)
	}
}

func TestReactionAddedRemoved(t *testing.T) {
	s, c := loadedStore(t)
	c.Window("C1").Update("m20", func(m *api.Message) {
		m.Reactions = []api.ReactionSummary{{Emoji: "👍", Count: 2}}
	})
	react := func(typ, emoji, user string, count int) {
		apply(t, s, typ, realtime.ReactionEvent{ChannelID: "C1", MessageID: "m20", Emoji: emoji, UserID: user, Count: count})
	}
	react("reaction.added", "👍", "U1", 3)
	if got := get(t, c, "C1", "m20").Reactions; !slices.Equal(got, []api.ReactionSummary{{Emoji: "👍", Count: 3}}) {
		t.Fatalf("other user's add: %+v", got)
	}
	react("reaction.added", "👍", "ME", 4)
	react("reaction.added", "👀", "U3", 1)
	if got := get(t, c, "C1", "m20").Reactions; !slices.Equal(got, []api.ReactionSummary{{Emoji: "👍", Count: 4, Me: true}, {Emoji: "👀", Count: 1}}) {
		t.Fatalf("my add + new emoji at end: %+v", got)
	}
	react("reaction.removed", "👍", "ME", 3)
	if got := get(t, c, "C1", "m20").Reactions; !slices.Equal(got, []api.ReactionSummary{{Emoji: "👍", Count: 3}, {Emoji: "👀", Count: 1}}) {
		t.Fatalf("my remove: %+v", got)
	}
	react("reaction.removed", "👀", "U3", 0)
	if got := get(t, c, "C1", "m20").Reactions; !slices.Equal(got, []api.ReactionSummary{{Emoji: "👍", Count: 3}}) {
		t.Fatalf("count 0 removes the emoji: %+v", got)
	}
	apply(t, s, "reaction.added", realtime.ReactionEvent{ChannelID: "C1", MessageID: "m15", Emoji: "👍", UserID: "U1", Count: 1})
	if got := c.TakeMissing("C1"); !slices.Equal(got, []string{"m15"}) {
		t.Fatalf("unknown in range => missing, got %v", got)
	}
}

func TestReactionOnThreadReply(t *testing.T) {
	s, c := loadedStore(t)
	c.SetThreadOpen("m20", true)
	apply(t, s, "message.created", realtime.MessageEvent{Message: reply("m35", "U1", "m20")})
	apply(t, s, "reaction.added", realtime.ReactionEvent{ChannelID: "C1", MessageID: "m35", ThreadRootID: strp("m20"), Emoji: "🎉", UserID: "ME", Count: 1})
	_, replies := c.Thread("m20")
	if m, _ := replies.Get("m35"); !slices.Equal(m.Reactions, []api.ReactionSummary{{Emoji: "🎉", Count: 1, Me: true}}) {
		t.Fatalf("reply reactions %+v", m.Reactions)
	}
	if got := get(t, c, "C1", "m20").Reactions; len(got) != 0 {
		t.Fatalf("root untouched, got %+v", got)
	}
}

func TestPinnedUpdatesFlagAndPins(t *testing.T) {
	s, c := loadedStore(t)
	if _, loaded := c.Pins("C1"); loaded {
		t.Fatal("pins not loaded yet")
	}
	c.SetPins("C1", []api.Message{})
	apply(t, s, "message.pinned", map[string]any{"channel_id": "C1", "message_id": "m10", "pinned_by": "U1"})
	apply(t, s, "message.pinned", map[string]any{"channel_id": "C1", "message_id": "m20", "pinned_by": "U1"})
	if !get(t, c, "C1", "m20").Pinned {
		t.Fatal("pinned flag")
	}
	pins, loaded := c.Pins("C1")
	if !loaded || !slices.Equal(ids(pins), []string{"m20", "m10"}) || !pins[0].Pinned {
		t.Fatalf("pins %v loaded=%v", ids(pins), loaded)
	}
	apply(t, s, "message.unpinned", map[string]any{"channel_id": "C1", "message_id": "m20", "unpinned_by": "U1"})
	if get(t, c, "C1", "m20").Pinned {
		t.Fatal("unpinned flag")
	}
	if pins, _ := c.Pins("C1"); !slices.Equal(ids(pins), []string{"m10"}) {
		t.Fatalf("pins %v", ids(pins))
	}
	// Pinning a message we do not hold: the list can no longer be trusted.
	apply(t, s, "message.pinned", map[string]any{"channel_id": "C1", "message_id": "m05", "pinned_by": "U1"})
	if _, loaded := c.Pins("C1"); loaded {
		t.Fatal("unknown pinned message invalidates the pin list")
	}
}

func TestPinsCopyAndEditsFollow(t *testing.T) {
	s, c := loadedStore(t)
	c.SetPins("C1", []api.Message{get(t, c, "C1", "m20")})
	pins, _ := c.Pins("C1")
	pins[0].Body = "mutated"
	up := msg("m20", "U2")
	up.Body = "edited"
	apply(t, s, "message.updated", realtime.MessageEvent{Message: up})
	if pins, _ := c.Pins("C1"); pins[0].Body != "edited" {
		t.Fatalf("pin copy follows edits, got %q", pins[0].Body)
	}
}

func TestPollUpdatedKeepsMyVotes(t *testing.T) {
	s, c := loadedStore(t)
	c.Window("C1").Update("m30", func(m *api.Message) {
		m.Kind = "poll"
		m.Poll = &api.Poll{ID: "P1", ContextType: "message", ContextID: "m30", Options: []api.PollOption{{VoteCount: 1}, {VoteCount: 0}}, VoterCount: 1, MyVotes: []int{0}}
	})
	apply(t, s, "poll.updated", realtime.PollUpdated{Poll: api.Poll{ID: "P1", ContextType: "message", ContextID: "m30", Options: []api.PollOption{{VoteCount: 1}, {VoteCount: 2}}, VoterCount: 3, MyVotes: []int{}}})
	p := get(t, c, "C1", "m30").Poll
	if p.VoterCount != 3 || p.Options[1].VoteCount != 2 || !slices.Equal(p.MyVotes, []int{0}) {
		t.Fatalf("poll %+v", p)
	}
	apply(t, s, "poll.updated", realtime.PollUpdated{Poll: api.Poll{ID: "P9", ContextType: "post", ContextID: "m30", VoterCount: 99}})
	if get(t, c, "C1", "m30").Poll.VoterCount != 3 {
		t.Fatal("post polls are not chat polls")
	}
}

func TestFindMessage(t *testing.T) {
	s, c := loadedStore(t)
	c.SetThreadOpen("m20", true)
	apply(t, s, "message.created", realtime.MessageEvent{Message: reply("m35", "U1", "m20")})
	root := msg("m90", "U1")
	root.ChannelID = "C2"
	c.thread("m90").root = &root
	for _, id := range []string{"m10", "m35", "m90"} {
		if m, ok := c.FindMessage(id); !ok || m.ID != id {
			t.Fatalf("FindMessage(%s) = %v %v", id, m.ID, ok)
		}
	}
	if _, ok := c.FindMessage("nope"); ok {
		t.Fatal("unknown id")
	}
}

func TestChannelDeletedDropsCaches(t *testing.T) {
	s, c := loadedStore(t)
	c.AddPending("C1", PendingMessage{Nonce: "N1", Kind: "poll"})
	c.SetPendingPoll("C1", "N1", &api.PollInput{Question: "q"})
	c.SetPins("C1", nil)
	c.SetOpen("C1", "", true)
	c.SetThreadOpen("m20", true)
	apply(t, s, "message.created", realtime.MessageEvent{Message: reply("m35", "U1", "m20")})
	apply(t, s, "message.updated", realtime.MessageEvent{Message: msg("m15", "U1")})
	apply(t, s, "typing", realtime.Typing{ChannelID: "C1", UserID: "U1", ExpiresInMS: 6000})
	c.SetActiveChannel("C1")
	apply(t, s, "channel.deleted", realtime.ChannelDeleted{ChannelID: "C1", Reason: "deleted"})
	if _, ok := s.Channels["C1"]; ok {
		t.Fatal("precondition: core drops the channel first")
	}
	if c.HasWindow("C1") || c.Loaded("C1") || len(c.Pending("C1")) != 0 || c.PendingPoll("C1", "N1") != nil {
		t.Fatal("window and pending cleared")
	}
	if _, loaded := c.Pins("C1"); loaded {
		t.Fatal("pins cleared")
	}
	if len(c.Typing("C1", nil, t0)) != 0 || len(c.open) != 0 || len(c.threads) != 0 || len(c.TakeMissing("C1")) != 0 {
		t.Fatal("typing, open state, threads and missing cleared")
	}
	if c.ActiveChannel() != "C1" {
		t.Fatal("the active channel id stays so the view can say the channel is gone")
	}
}

func TestSpaceRemovedDropsGoneChannels(t *testing.T) {
	s, c := loadedStore(t)
	s.Channels["C2"] = &api.Channel{ID: "C2", Kind: "text", SpaceID: strp("S1")}
	c.Window("C2")
	c.loaded["C2"] = true
	c.AddPending("C2", PendingMessage{Nonce: "N2"})
	c.SetOpen("C2", "", true)
	apply(t, s, "space.removed", realtime.SpaceRemoved{SpaceID: "S1", Reason: "kicked"})
	if c.HasWindow("C2") || len(c.Pending("C2")) != 0 || c.Loaded("C2") {
		t.Fatal("C2 left with its space")
	}
	if !c.HasWindow("C1") || c.Window("C1").Len() != 3 || !c.Loaded("C1") {
		t.Fatal("C1 kept")
	}
	s.Channels["C3"] = &api.Channel{ID: "C3", Kind: "text", SpaceID: strp("S2")}
	c.Window("C3")
	apply(t, s, "space.deleted", realtime.SpaceDeleted{SpaceID: "S2"})
	if c.HasWindow("C3") || !c.HasWindow("C1") {
		t.Fatal("space.deleted drops its channels only")
	}
}

func TestUnknownEventIgnored(t *testing.T) {
	s, c := loadedStore(t)
	before := c.Window("C1").Items()
	apply(t, s, "post.stats", realtime.PostStats{PostID: "P1", LikeCount: 3})
	s.Apply(realtime.Event{Type: "no.such.event", Data: []byte(`{}`)})
	s.Apply(realtime.Event{Type: "message.created", Data: []byte(`null`)}) // malformed known type
	if got := c.Window("C1").Items(); !slices.EqualFunc(got, before, func(a, b api.Message) bool { return a.ID == b.ID && a.Body == b.Body }) {
		t.Fatalf("state changed: %v", ids(got))
	}
}

func TestResetClearsPinsAndMissing(t *testing.T) {
	s, c := loadedStore(t)
	c.SetPins("C1", []api.Message{get(t, c, "C1", "m20")})
	apply(t, s, "message.updated", realtime.MessageEvent{Message: msg("m15", "U1")})
	s.Reset()
	if _, loaded := c.Pins("C1"); loaded || len(c.TakeMissing("C1")) != 0 {
		t.Fatal("pins and missing cleared on reset")
	}
}

// events.md 6.4: events from different requests can arrive out of order, so a
// late creation payload must not regress a copy we already hold.
func TestCreatedAfterUpdatedKeepsEdit(t *testing.T) {
	s, c := loadedStore(t)
	orig := msg("m40", "ME")
	orig.ClientNonce = strp("N1")
	edited := orig
	edited.Body = "edited"
	at := t0.Add(time.Minute)
	edited.EditedAt = &at
	apply(t, s, "message.updated", realtime.MessageEvent{Message: edited}) // unknown: recorded missing
	if got := c.TakeMissing("C1"); !slices.Equal(got, []string{"m40"}) {
		t.Fatalf("missing %v", got)
	}
	c.UpsertMessage(edited) // the UI's GetMessage of the missing id
	c.AddPending("C1", PendingMessage{Nonce: "N1", Body: "body m40", Kind: "text", SentAt: t0})
	apply(t, s, "message.created", realtime.MessageEvent{Message: orig}) // late creation
	got := get(t, c, "C1", "m40")
	if got.Body != "edited" || got.EditedAt == nil {
		t.Fatalf("late message.created regressed the edit: %+v", got)
	}
	if len(c.Pending("C1")) != 0 {
		t.Fatal("pending send still dropped by nonce")
	}
}

func TestCreatedAfterTombstoneKeepsTombstone(t *testing.T) {
	s, c := loadedStore(t)
	apply(t, s, "message.deleted", realtime.MessageDeleted{ChannelID: "C1", MessageID: "m20", Tombstone: true})
	apply(t, s, "message.created", realtime.MessageEvent{Message: msg("m20", "U2")})
	if got := get(t, c, "C1", "m20"); !got.Deleted || got.Body != "" {
		t.Fatalf("late message.created revived a tombstone: %+v", got)
	}
}
