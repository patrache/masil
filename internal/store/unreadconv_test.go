package store

import (
	"reflect"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
)

// convStore is meStore plus a group DM and a mentions-only channel, every
// channel read, with distinct last-message times (C0GEN oldest .. G1 newest).
func convStore(t *testing.T) *Store {
	t.Helper()
	s := meStore(t)
	s.ObserveChannel(groupChannel("G1", sp("점심팟"), uMe, uMinji, uJunho))
	quiet := textChannel("C2DEP", "S1", "배포", 2)
	quiet.My.Notify = "mentions"
	s.ObserveChannel(quiet)
	for i, id := range []string{"C0GEN", "C1DEV", "C2DEP", "D2JH", "D1MJ", "G1"} {
		at := testTS.Add(time.Duration(i) * time.Minute)
		s.Channels[id].LastMessageAt = &at
	}
	return s
}

func convIDs(cs []UnreadConv) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.ChannelID)
	}
	return out
}

func unread(s *Store, id string, n, mentions int, lastRead string) {
	ch := s.Channels[id]
	ch.My.UnreadCount, ch.My.MentionCount = n, mentions
	if lastRead != "" {
		ch.My.LastReadMessageID = sp(lastRead)
	}
}

func TestUnreadConversationsPicks(t *testing.T) {
	s := convStore(t)
	if got := s.UnreadConversations(); len(got) != 0 {
		t.Fatalf("everything read: %v", convIDs(got))
	}
	unread(s, "D1MJ", 2, 0, "M10")  // 1:1 dm
	unread(s, "G1", 1, 0, "")       // group dm, never read
	unread(s, "C1DEV", 4, 0, "M20") // notify all
	unread(s, "C2DEP", 3, 1, "M30") // mentions-only with a mention
	unread(s, "C0GEN", 0, 0, "")    // read
	got := s.UnreadConversations()
	want := []UnreadConv{
		{ChannelID: "G1", Kind: api.ChannelGroupDM, Unread: 1, LastAt: testTS.Add(5 * time.Minute), Badged: true},
		{ChannelID: "D1MJ", Kind: api.ChannelDM, Unread: 2, LastAt: testTS.Add(4 * time.Minute), Anchor: "M10", Badged: true},
		{ChannelID: "C2DEP", Kind: api.ChannelText, Unread: 3, Mentions: 1, LastAt: testTS.Add(2 * time.Minute), Anchor: "M30"},
		{ChannelID: "C1DEV", Kind: api.ChannelText, Unread: 4, LastAt: testTS.Add(1 * time.Minute), Anchor: "M20", Badged: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UnreadConversations =\n%+v\nwant\n%+v", got, want)
	}
	// a mentions-only channel without a mention is not listed
	unread(s, "C2DEP", 3, 0, "")
	if ids := convIDs(s.UnreadConversations()); containsStr(ids, "C2DEP") {
		t.Fatalf("mentions-only channel without a mention listed: %v", ids)
	}
	// InboxBadge: notifications + the conversations no notification row covers
	s.NotifUnread = 2
	if got := s.InboxBadge(); got != 2+3 {
		t.Fatalf("InboxBadge = %d, want 5 (2 notifications + G1, D1MJ, C1DEV)", got)
	}
}

func containsStr(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func TestUnreadConversationsSkips(t *testing.T) {
	s := convStore(t)
	unread(s, "D1MJ", 2, 0, "")
	unread(s, "D2JH", 1, 0, "")
	unread(s, "C1DEV", 5, 2, "")
	unread(s, "G1", 3, 0, "")
	s.Channels["D1MJ"].My.Muted = true  // muted dm
	s.Channels["C1DEV"].My.Muted = true // muted channel, even with mentions
	s.Channels["G1"].My.Notify = "none" // notify none
	s.Blocks[uJunho.ID] = testTS        // a 1:1 dm with a blocked peer is hidden
	if got := s.UnreadConversations(); len(got) != 0 {
		t.Fatalf("muted / none / blocked listed: %v", convIDs(got))
	}
	if got := s.InboxBadge(); got != s.NotifUnread {
		t.Fatalf("InboxBadge = %d, want NotifUnread %d", got, s.NotifUnread)
	}
}

// A conversation leaves the list as soon as it is read (read_state.updated),
// and a new message from someone else brings it back.
func TestUnreadConversationsLive(t *testing.T) {
	s := convStore(t)
	m := msg("M500", "D1MJ", uMinji)
	m.Body = "저녁 뭐 먹지?"
	s.Apply(ev(t, "message.created", map[string]any{"message": m}))
	got := s.UnreadConversations()
	if len(got) != 1 || got[0].ChannelID != "D1MJ" || got[0].Unread != 1 {
		t.Fatalf("after message.created: %+v", got)
	}
	if p, ok := s.LastMessage("D1MJ"); !ok || p.ID != "M500" || p.Body != "저녁 뭐 먹지?" {
		t.Fatalf("LastMessage = %+v %v", p, ok)
	}
	st := api.UnreadState{ChannelID: "D1MJ", LastMessageID: sp("M500"), LastReadMessageID: sp("M500")}
	s.Apply(ev(t, "read_state.updated", map[string]any{"state": st}))
	if got := s.UnreadConversations(); len(got) != 0 {
		t.Fatalf("read conversation still listed: %+v", got)
	}
	// edits refresh the preview, a delete drops it, thread replies never set it
	m.Body = "저녁은 김밥"
	s.Apply(ev(t, "message.updated", map[string]any{"message": m}))
	if p, _ := s.LastMessage("D1MJ"); p.Body != "저녁은 김밥" {
		t.Fatalf("edit not applied: %+v", p)
	}
	reply := msg("M501", "D1MJ", uMinji)
	reply.ThreadRootID = sp("M500")
	s.Apply(ev(t, "message.created", map[string]any{"message": reply}))
	if p, _ := s.LastMessage("D1MJ"); p.ID != "M500" {
		t.Fatalf("thread reply became the preview: %+v", p)
	}
	s.Apply(ev(t, "message.deleted", map[string]any{"channel_id": "D1MJ", "message_id": "M500", "tombstone": true}))
	if p, ok := s.LastMessage("D1MJ"); ok {
		t.Fatalf("deleted message kept as preview: %+v", p)
	}
}
