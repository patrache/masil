package chatstore

import (
	"slices"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
)

func page(hasBefore, hasAfter bool, ms ...api.Message) *api.MessageWindow {
	return &api.MessageWindow{Items: ms, HasMoreBefore: hasBefore, HasMoreAfter: hasAfter}
}

func TestApplyPageModes(t *testing.T) {
	c := Get(store.New())
	if c.Loaded("C1") {
		t.Fatal("not loaded before a page")
	}
	c.ApplyPage("C1", api.MessageQuery{Limit: 50}, page(true, false, msg("m30", "U1"), msg("m40", "U2")))
	w := c.Window("C1")
	if !c.Loaded("C1") || !slices.Equal(ids(w.Items()), []string{"m30", "m40"}) || !w.HasMoreBefore() || w.HasMoreAfter() {
		t.Fatalf("latest => Replace: %v %v %v", ids(w.Items()), w.HasMoreBefore(), w.HasMoreAfter())
	}

	c.ApplyPage("C1", api.MessageQuery{Before: "m30", Limit: 50}, page(false, true, msg("m10", "U1"), msg("m20", "U2")))
	if !slices.Equal(ids(w.Items()), []string{"m10", "m20", "m30", "m40"}) || w.HasMoreBefore() || w.HasMoreAfter() {
		t.Fatalf("Before => PrependOlder (has_more_before from page, has_more_after kept): %v %v %v", ids(w.Items()), w.HasMoreBefore(), w.HasMoreAfter())
	}

	up := msg("m40", "U2")
	up.Body = "edited"
	c.ApplyPage("C1", api.MessageQuery{After: "m40", Limit: 50}, page(true, true, up, msg("m50", "U1")))
	if !slices.Equal(ids(w.Items()), []string{"m10", "m20", "m30", "m40", "m50"}) || w.HasMoreBefore() || !w.HasMoreAfter() {
		t.Fatalf("After => AppendNewer deduped by ID: %v %v %v", ids(w.Items()), w.HasMoreBefore(), w.HasMoreAfter())
	}
	if get(t, c, "C1", "m40").Body != "edited" {
		t.Fatal("overlapping item takes the REST copy")
	}

	c.ApplyPage("C1", api.MessageQuery{Around: "m20", Limit: 50}, page(true, true, msg("m15", "U1"), msg("m20", "U2")))
	if !slices.Equal(ids(w.Items()), []string{"m15", "m20"}) || !w.HasMoreBefore() || !w.HasMoreAfter() {
		t.Fatalf("Around => Replace: %v", ids(w.Items()))
	}

	c.ApplyPage("C2", api.MessageQuery{Before: "m90", Limit: 50}, page(true, true, msg("m80", "U1")))
	if w2 := c.Window("C2"); !c.Loaded("C2") || !w2.HasMoreAfter() {
		t.Fatal("an anchored page into an unloaded window replaces it (flags from the page)")
	}

	c.ApplyPage("C3", api.MessageQuery{}, nil)
	if c.Loaded("C3") {
		t.Fatal("nil page is ignored")
	}
}

func TestApplyPageMergesPending(t *testing.T) {
	c := Get(store.New())
	c.AddPending("C1", PendingMessage{Nonce: "N1", Body: "hi", Kind: "text", SentAt: t0})
	c.AddPending("C1", PendingMessage{Nonce: "N2", Body: "later", Kind: "text", SentAt: t0})
	m := msg("m10", "ME")
	m.ClientNonce = strp("N1")
	c.ApplyPage("C1", api.MessageQuery{Limit: 50}, page(false, false, m))
	if p := c.Pending("C1"); len(p) != 1 || p[0].Nonce != "N2" {
		t.Fatalf("pending %+v", p)
	}
}

func TestApplyPageClearsMissingAndRefreshesCopies(t *testing.T) {
	s, c := loadedStore(t)
	c.SetPins("C1", []api.Message{msg("m20", "U2")})
	c.SetThreadOpen("m20", true)
	c.ApplyThreadPage("m20", api.ThreadQuery{Limit: 50}, &api.ThreadWindow{Root: msg("m20", "U2"), Items: []api.Message{}})
	apply(t, s, "message.updated", realtime.MessageEvent{Message: msg("m25", "U1")})
	if !slices.Equal(c.missing["C1"], []string{"m25"}) {
		t.Fatalf("setup: missing %v", c.missing["C1"])
	}
	up := msg("m20", "U2")
	up.Body = "edited"
	c.ApplyPage("C1", api.MessageQuery{Limit: 50}, page(false, false, msg("m10", "U1"), up, msg("m25", "U1")))
	if got := c.TakeMissing("C1"); len(got) != 0 {
		t.Fatalf("landed ids are no longer missing: %v", got)
	}
	if root, _ := c.Thread("m20"); root.Body != "edited" {
		t.Fatalf("thread root copy refreshed, got %q", root.Body)
	}
	if pins, _ := c.Pins("C1"); pins[0].Body != "edited" {
		t.Fatalf("pin copy refreshed, got %q", pins[0].Body)
	}
}

func TestApplyThreadPage(t *testing.T) {
	_, c := loadedStore(t)
	c.SetThreadOpen("m20", true)
	root := msg("m20", "U2")
	root.Thread = &api.ThreadSummary{ReplyCount: 3}
	c.AddPending("C1", PendingMessage{Nonce: "N1", Body: "r", Kind: "text", ThreadRootID: strp("m20"), SentAt: t0})
	r23 := reply("m23", "ME", "m20")
	r23.ClientNonce = strp("N1")
	c.ApplyThreadPage("m20", api.ThreadQuery{Limit: 50}, &api.ThreadWindow{Root: root, Items: []api.Message{reply("m22", "U1", "m20"), r23}, HasMoreBefore: true})
	gotRoot, replies := c.Thread("m20")
	if gotRoot == nil || gotRoot.Thread == nil || gotRoot.Thread.ReplyCount != 3 {
		t.Fatalf("root %+v", gotRoot)
	}
	if !slices.Equal(ids(replies.Items()), []string{"m22", "m23"}) || !replies.HasMoreBefore() {
		t.Fatalf("replies %v", ids(replies.Items()))
	}
	if len(c.Pending("C1")) != 0 {
		t.Fatal("reply page merges the pending reply by nonce")
	}
	if get(t, c, "C1", "m20").Thread.ReplyCount != 3 {
		t.Fatal("channel window copy of the root takes the REST root")
	}

	c.ApplyThreadPage("m20", api.ThreadQuery{Before: "m22", Limit: 50}, &api.ThreadWindow{Root: root, Items: []api.Message{reply("m21", "U1", "m20")}})
	if !slices.Equal(ids(replies.Items()), []string{"m21", "m22", "m23"}) || replies.HasMoreBefore() {
		t.Fatalf("Before => PrependOlder: %v", ids(replies.Items()))
	}
	c.ApplyThreadPage("m20", api.ThreadQuery{After: "m23", Limit: 50}, &api.ThreadWindow{Root: root, Items: []api.Message{reply("m24", "U1", "m20")}, HasMoreAfter: true})
	if !slices.Equal(ids(replies.Items()), []string{"m21", "m22", "m23", "m24"}) || !replies.HasMoreAfter() {
		t.Fatalf("After => AppendNewer: %v", ids(replies.Items()))
	}
	c.ApplyThreadPage("m20", api.ThreadQuery{Limit: 50}, &api.ThreadWindow{Root: root, Items: []api.Message{reply("m24", "U1", "m20")}})
	if !slices.Equal(ids(replies.Items()), []string{"m24"}) || replies.HasMoreAfter() {
		t.Fatalf("no anchor => Replace: %v", ids(replies.Items()))
	}
}

func TestUpsertFromSendResult(t *testing.T) {
	c := Get(store.New())
	c.ApplyPage("C1", api.MessageQuery{Limit: 50}, page(false, false))
	c.AddPending("C1", PendingMessage{Nonce: "N1", Body: "body m10", Kind: "text", SentAt: t0})
	m := msg("m10", "ME")
	m.ClientNonce = strp("N1")
	c.UpsertMessage(m)
	if !slices.Equal(ids(c.Window("C1").Items()), []string{"m10"}) || len(c.Pending("C1")) != 0 {
		t.Fatalf("items %v pending %+v", ids(c.Window("C1").Items()), c.Pending("C1"))
	}
}

func TestUpsertMessageRules(t *testing.T) {
	s, c := loadedStore(t)
	c.Window("C1").Update("m20", func(m *api.Message) {
		m.Reactions = []api.ReactionSummary{{Emoji: "👍", Count: 1, Me: true}}
	})
	up := msg("m20", "U2")
	up.Body = "edited"
	up.Reactions = []api.ReactionSummary{{Emoji: "👍", Count: 1, Me: false}}
	c.UpsertMessage(up)
	if got := get(t, c, "C1", "m20"); got.Body != "edited" || got.Reactions[0].Me {
		t.Fatalf("REST copy wins, reactions[].me included: %+v", got)
	}

	// A window scrolled up (more after) does not take a message past its end.
	c.Window("C1").Replace(c.Window("C1").Items(), false, true)
	c.UpsertMessage(msg("m90", "ME"))
	if _, ok := c.Window("C1").Get("m90"); ok {
		t.Fatal("out of range send result must not open a gap")
	}

	// GetMessage of a missing id lands it and clears the missing record.
	apply(t, s, "message.updated", realtime.MessageEvent{Message: msg("m25", "U1")})
	c.UpsertMessage(msg("m25", "U1"))
	if _, ok := c.Window("C1").Get("m25"); !ok || len(c.TakeMissing("C1")) != 0 {
		t.Fatal("missing message fetched and recorded")
	}

	// Unknown channel: no window is created.
	c.UpsertMessage(api.Message{ID: "x1", ChannelID: "C9"})
	if c.HasWindow("C9") {
		t.Fatal("no window for an unopened channel")
	}

	// Thread reply goes to the open thread.
	c.SetThreadOpen("m30", true)
	c.ApplyThreadPage("m30", api.ThreadQuery{Limit: 50}, &api.ThreadWindow{Root: msg("m30", "U1"), Items: []api.Message{}})
	c.AddPending("C1", PendingMessage{Nonce: "N7", Body: "r", Kind: "text", ThreadRootID: strp("m30"), SentAt: t0})
	r := reply("m31", "ME", "m30")
	r.ClientNonce = strp("N7")
	c.UpsertMessage(r)
	if _, replies := c.Thread("m30"); !slices.Equal(ids(replies.Items()), []string{"m31"}) {
		t.Fatalf("reply %v", ids(replies.Items()))
	}
	if len(c.Pending("C1")) != 0 {
		t.Fatal("reply pending merged")
	}
	if _, ok := c.Window("C1").Get("m31"); ok {
		t.Fatal("replies never enter the channel window")
	}
}

func TestSetMyReactionOptimistic(t *testing.T) {
	_, c := loadedStore(t)
	c.SetPins("C1", []api.Message{msg("m20", "U2")})
	if !c.SetMyReaction("m20", "😀", true) {
		t.Fatal("known message")
	}
	want := []api.ReactionSummary{{Emoji: "😀", Count: 1, Me: true}}
	if got := get(t, c, "C1", "m20").Reactions; !slices.Equal(got, want) {
		t.Fatalf("none -> on: %+v", got)
	}
	if pins, _ := c.Pins("C1"); !slices.Equal(pins[0].Reactions, want) {
		t.Fatalf("pin copy follows: %+v", pins[0].Reactions)
	}
	c.SetMyReaction("m20", "😀", true)
	if got := get(t, c, "C1", "m20").Reactions; !slices.Equal(got, want) {
		t.Fatalf("on again is a no-op: %+v", got)
	}
	c.Window("C1").Update("m20", func(m *api.Message) {
		m.Reactions = []api.ReactionSummary{{Emoji: "😀", Count: 1, Me: true}, {Emoji: "👍", Count: 2}}
	})
	c.SetMyReaction("m20", "👍", true)
	if got := get(t, c, "C1", "m20").Reactions; !slices.Equal(got, []api.ReactionSummary{{Emoji: "😀", Count: 1, Me: true}, {Emoji: "👍", Count: 3, Me: true}}) {
		t.Fatalf("existing chip +1: %+v", got)
	}
	c.SetMyReaction("m20", "😀", false)
	c.SetMyReaction("m20", "👍", false)
	if got := get(t, c, "C1", "m20").Reactions; !slices.Equal(got, []api.ReactionSummary{{Emoji: "👍", Count: 2}}) {
		t.Fatalf("off: removed at 0, else -1: %+v", got)
	}
	c.SetMyReaction("m20", "👍", false)
	if got := get(t, c, "C1", "m20").Reactions; !slices.Equal(got, []api.ReactionSummary{{Emoji: "👍", Count: 2}}) {
		t.Fatalf("off when not mine is a no-op: %+v", got)
	}
	if c.SetMyReaction("nope", "😀", true) {
		t.Fatal("unknown id => false")
	}

	c.SetThreadOpen("m30", true)
	c.ApplyThreadPage("m30", api.ThreadQuery{Limit: 50}, &api.ThreadWindow{Root: msg("m30", "U1"), Items: []api.Message{reply("m31", "U1", "m30")}})
	if !c.SetMyReaction("m31", "😀", true) {
		t.Fatal("thread reply is known")
	}
	if _, replies := c.Thread("m30"); !slices.Equal(replies.Items()[0].Reactions, want) {
		t.Fatalf("reply reaction %+v", replies.Items()[0].Reactions)
	}
}

func TestSetPollAuthoritative(t *testing.T) {
	_, c := loadedStore(t)
	c.Window("C1").Update("m30", func(m *api.Message) {
		m.Kind = "poll"
		m.Poll = &api.Poll{ID: "P1", ContextType: "message", ContextID: "m30", Options: []api.PollOption{{VoteCount: 1}, {VoteCount: 0}}, VoterCount: 1, MyVotes: []int{0}}
	})
	c.SetPoll(api.Poll{ID: "P1", ContextType: "message", ContextID: "m30", Options: []api.PollOption{{VoteCount: 1}, {VoteCount: 1}}, VoterCount: 2, MyVotes: []int{}}, false)
	if p := get(t, c, "C1", "m30").Poll; p.VoterCount != 2 || !slices.Equal(p.MyVotes, []int{0}) {
		t.Fatalf("non-authoritative keeps local my_votes: %+v", p)
	}
	c.SetPoll(api.Poll{ID: "P1", ContextType: "message", ContextID: "m30", Options: []api.PollOption{{VoteCount: 0}, {VoteCount: 2}}, VoterCount: 2, MyVotes: []int{1}}, true)
	if p := get(t, c, "C1", "m30").Poll; p.Options[1].VoteCount != 2 || !slices.Equal(p.MyVotes, []int{1}) {
		t.Fatalf("authoritative replaces my_votes: %+v", p)
	}
	c.SetPoll(api.Poll{ID: "P2", ContextType: "post", ContextID: "m30", VoterCount: 99}, true)
	if get(t, c, "C1", "m30").Poll.VoterCount != 2 {
		t.Fatal("post polls are not chat polls")
	}
	c.SetPoll(api.Poll{ID: "P3", ContextType: "message", ContextID: "nope"}, true) // unknown: no panic
}

func TestUpsertDoesNotRegressEditOrTombstone(t *testing.T) {
	s, c := loadedStore(t)
	c.SetPins("C1", []api.Message{msg("m20", "U2")})
	edited := msg("m20", "U2")
	edited.Body = "edited"
	at := t0.Add(time.Minute)
	edited.EditedAt = &at
	c.UpsertMessage(edited)
	c.UpsertMessage(msg("m20", "U2")) // send result landing after the edit
	if got := get(t, c, "C1", "m20"); got.Body != "edited" {
		t.Fatalf("older send result regressed the edit: %+v", got)
	}
	if p, _ := c.Pins("C1"); p[0].Body != "edited" {
		t.Fatalf("pin copy regressed: %+v", p[0])
	}
	newer := edited
	later := t0.Add(2 * time.Minute)
	newer.EditedAt = &later
	newer.Body = "edited again"
	c.UpsertMessage(newer)
	if got := get(t, c, "C1", "m20"); got.Body != "edited again" {
		t.Fatalf("a newer edit result must replace: %+v", got)
	}

	apply(t, s, "message.deleted", realtime.MessageDeleted{ChannelID: "C1", MessageID: "m30", Tombstone: true})
	c.UpsertMessage(msg("m30", "U1"))
	if got := get(t, c, "C1", "m30"); !got.Deleted || got.Body != "" {
		t.Fatalf("send result revived a tombstone: %+v", got)
	}
}
