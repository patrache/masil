package feedstore_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/store/feedstore"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// ev builds a realtime event whose data is json.Marshal(data).
func ev(t *testing.T, typ string, data any) realtime.Event {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal %s: %v", typ, err)
	}
	return realtime.Event{Type: typ, Data: raw, TS: base}
}

var base = time.Date(2026, 11, 5, 2, 0, 0, 0, time.UTC)

func newFeed(t *testing.T) (*store.Store, *feedstore.FeedStore) {
	t.Helper()
	s := store.New()
	f := feedstore.Get(s)
	if f == nil {
		t.Fatal("feed reducer not registered")
	}
	return s, f
}

func user(id, handle, name string) api.UserSummary {
	return api.UserSummary{ID: id, Handle: handle, DisplayName: name}
}

var (
	hana   = user(fixtures.UserHana, "hana", "하나")
	devKim = user(fixtures.UserDevKim, "dev_kim", "김개발")
	minji  = user(fixtures.UserMinji, "minji", "민지")
)

func post(id string, author api.UserSummary, body string, min int) api.Post {
	return api.Post{
		ID: id, Author: author, Body: body,
		Media: []api.Media{}, Hashtags: []string{}, Mentions: []api.MentionRef{},
		CreatedAt: base.Add(time.Duration(min) * time.Minute),
	}
}

func item(p api.Post) api.TimelineItem {
	return api.TimelineItem{ID: p.ID, Type: "post", Post: p, At: p.CreatedAt}
}

func repost(id string, p api.Post, by api.UserSummary, min int) api.TimelineItem {
	b := by
	return api.TimelineItem{ID: id, Type: "repost", Post: p, RepostedBy: &b, At: base.Add(time.Duration(min) * time.Minute)}
}

func strp(s string) *string { return &s }

func TestGetReturnsRegisteredReducer(t *testing.T) {
	s := store.New()
	if f := feedstore.Get(s); f == nil || f.Name() != "feed" {
		t.Fatalf("feed reducer not registered: %v", f)
	}
	if feedstore.Name != "feed" {
		t.Fatalf("Name = %q", feedstore.Name)
	}
}

func TestTimelineItemCreatedInserts(t *testing.T) {
	s, f := newFeed(t)
	it := fixtures.Timeline()[0]
	s.Apply(ev(t, "timeline.item_created", map[string]any{"item": it}))
	if _, ok := f.Timeline("following").Get(it.ID); !ok {
		t.Fatal("item not in following timeline")
	}
	p := f.Post(it.Post.ID)
	if p == nil || p.Body != it.Post.Body {
		t.Fatalf("Post(%s) = %#v", it.Post.ID, p)
	}
	if n := f.Timeline("discover").Len(); n != 0 {
		t.Fatalf("discover Len = %d, want 0", n)
	}
}

func TestPostStatsUpdatesCachedPost(t *testing.T) {
	s, f := newFeed(t)
	p := post("01JEPST0000000000000000001", hana, "hi", 0)
	p.LikeCount, p.ReplyCount = 12, 4
	f.ObservePosts(p)
	held := f.Post(p.ID)
	s.Apply(ev(t, "post.stats", map[string]any{"post_id": p.ID, "like_count": 20, "repost_count": 3, "reply_count": 7, "quote_count": 1}))
	got := f.Post(p.ID)
	if got != held {
		t.Fatal("post.stats must update the cached post in place")
	}
	if got.LikeCount != 20 || got.RepostCount != 3 || got.ReplyCount != 7 || got.QuoteCount != 1 {
		t.Fatalf("counts = %d/%d/%d/%d", got.LikeCount, got.RepostCount, got.ReplyCount, got.QuoteCount)
	}
	// unknown post: no panic, nothing cached
	s.Apply(ev(t, "post.stats", map[string]any{"post_id": "01JEPST0000000000000000099", "like_count": 1}))
	if f.Post("01JEPST0000000000000000099") != nil {
		t.Fatal("post.stats must not create posts")
	}
}

func TestPostDeletedRemovesFromTimelines(t *testing.T) {
	s, f := newFeed(t)
	p := post("01JEPST0000000000000000001", devKim, "bubbletea", 0)
	p.Media = []api.Media{{ID: "m1"}}
	other := post("01JEPST0000000000000000002", minji, "keep me", 1)
	rp := repost("01JEHRP0000000000000000001", p, hana, 2)
	f.ObservePosts(p, other)
	f.Timeline("following").Upsert(item(p), rp, item(other))
	f.Timeline("discover").Upsert(item(p), item(other))

	s.Apply(ev(t, "post.deleted", map[string]any{"post_id": p.ID}))

	for _, kind := range []string{"following", "discover"} {
		w := f.Timeline(kind)
		if _, ok := w.Get(p.ID); ok {
			t.Fatalf("%s still has the deleted post", kind)
		}
		if _, ok := w.Get(other.ID); !ok {
			t.Fatalf("%s lost an unrelated post", kind)
		}
	}
	if _, ok := f.Timeline("following").Get(rp.ID); ok {
		t.Fatal("repost item of the deleted post remains")
	}
	got := f.Post(p.ID)
	if got == nil || !got.Deleted || got.Body != "" || len(got.Media) != 0 {
		t.Fatalf("Post(deleted) = %#v", got)
	}
}

// TestMarkDeletedMatchesPostDeletedEvent: the UI's local apply after its own
// DELETE /v1/posts/{id} is exactly what the post.deleted event does.
func TestMarkDeletedMatchesPostDeletedEvent(t *testing.T) {
	build := func() (*store.Store, *feedstore.FeedStore, api.Post, api.Post) {
		s, f := newFeed(t)
		p := post("01JEPST0000000000000000001", devKim, "bubbletea", 0)
		quote := post("01JEPST0000000000000000002", minji, "look", 1)
		quote.Quote = &api.QuotedPost{Available: true, Post: &p}
		f.ObservePosts(p, quote)
		f.Timeline("following").Upsert(item(p), item(quote), repost("01JEHRP0000000000000000001", p, hana, 2))
		return s, f, p, quote
	}
	sA, fA, p, quote := build()
	sA.Apply(ev(t, "post.deleted", map[string]any{"post_id": p.ID}))
	_, fB, _, _ := build()
	fB.MarkDeleted(p.ID)

	ids := func(f *feedstore.FeedStore) []string {
		var out []string
		for _, it := range f.Timeline("following").Items() {
			out = append(out, it.ID)
		}
		return out
	}
	if a, b := ids(fA), ids(fB); !reflect.DeepEqual(a, b) {
		t.Fatalf("MarkDeleted timeline %v, post.deleted %v", b, a)
	}
	if got := fB.Post(p.ID); got == nil || !got.Deleted {
		t.Fatalf("Post after MarkDeleted = %#v", got)
	}
	if q := fB.Post(quote.ID).Quote; q == nil || q.Available || q.Reason != "deleted" {
		t.Fatalf("quote after MarkDeleted = %#v", q)
	}
	fB.MarkDeleted("") // no-op
}

func TestPostDeletedKeepsReplyPlaceholder(t *testing.T) {
	s, f := newFeed(t)
	parent := post("01JEPST0000000000000000001", devKim, "parent", 0)
	withKids := post("01JEPST0000000000000000002", hana, "has replies", 1)
	withKids.ReplyToID, withKids.ReplyCount = strp(parent.ID), 2
	leaf := post("01JEPST0000000000000000003", minji, "leaf", 2)
	leaf.ReplyToID = strp(parent.ID)
	f.ObservePosts(parent, withKids, leaf)
	f.Replies(parent.ID).Upsert(withKids, leaf)

	s.Apply(ev(t, "post.deleted", map[string]any{"post_id": withKids.ID}))
	s.Apply(ev(t, "post.deleted", map[string]any{"post_id": leaf.ID}))

	w := f.Replies(parent.ID)
	r, ok := w.Get(withKids.ID)
	if !ok || !r.Deleted || r.Body != "" {
		t.Fatalf("reply with children must stay as placeholder: %#v ok=%v", r, ok)
	}
	if !f.Post(withKids.ID).Deleted {
		t.Fatal("cached reply not marked deleted")
	}
	if _, ok := w.Get(leaf.ID); ok {
		t.Fatal("reply without children must be removed")
	}
}

func TestPostReplyCreatedOnlyIntoLoadedWindow(t *testing.T) {
	s, f := newFeed(t)
	parent := post("01JEPST0000000000000000001", devKim, "parent", 0)
	f.ObservePosts(parent)
	r1 := post("01JEPST0000000000000000005", hana, "first", 1)
	r1.ReplyToID = strp(parent.ID)

	s.Apply(ev(t, "post.reply_created", map[string]any{"parent_id": parent.ID, "post": r1}))
	if f.HasReplies(parent.ID) {
		t.Fatal("reply_created must not create a replies window")
	}
	if f.Post(r1.ID) == nil {
		t.Fatal("event reply should still be cached")
	}

	w := f.Replies(parent.ID)
	if !f.HasReplies(parent.ID) {
		t.Fatal("Replies must create the window")
	}
	r0 := post("01JEPST0000000000000000003", minji, "zero", 1)
	r2 := post("01JEPST0000000000000000009", minji, "second", 2)
	s.Apply(ev(t, "post.reply_created", map[string]any{"parent_id": parent.ID, "post": r2}))
	s.Apply(ev(t, "post.reply_created", map[string]any{"parent_id": parent.ID, "post": r0}))
	var ids []string
	for _, p := range w.Items() {
		ids = append(ids, p.ID)
	}
	if want := []string{r0.ID, r2.ID}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("replies = %v, want %v", ids, want)
	}
}

func TestPollUpdatedKeepsMyVotes(t *testing.T) {
	s, f := newFeed(t)
	p := post("01JEPST0000000000000000001", devKim, "점심?", 0)
	f.ObservePosts(p)
	f.SetPoll(api.Poll{
		ID: "01JGPQ00000000000000000001", ContextType: "post", ContextID: p.ID, Question: "점심?",
		Options: []api.PollOption{{Idx: 0, Text: "국밥", VoteCount: 4}, {Idx: 1, Text: "라멘", VoteCount: 2}},
		MyVotes: []int{1},
	})
	if got := f.Post(p.ID).Poll; got == nil || !reflect.DeepEqual(got.MyVotes, []int{1}) {
		t.Fatalf("SetPoll not applied: %#v", got)
	}
	s.Apply(ev(t, "poll.updated", map[string]any{"poll": api.Poll{
		ID: "01JGPQ00000000000000000001", ContextType: "post", ContextID: p.ID, Question: "점심?",
		Options:    []api.PollOption{{Idx: 0, Text: "국밥", VoteCount: 5}, {Idx: 1, Text: "라멘", VoteCount: 2}},
		VoterCount: 7, MyVotes: []int{},
	}}))
	got := f.Post(p.ID).Poll
	if got.Options[0].VoteCount != 5 || got.VoterCount != 7 {
		t.Fatalf("counts not updated: %#v", got)
	}
	if !reflect.DeepEqual(got.MyVotes, []int{1}) {
		t.Fatalf("MyVotes = %v, want [1]", got.MyVotes)
	}

	// a message poll with the same context id is ignored
	s.Apply(ev(t, "poll.updated", map[string]any{"poll": api.Poll{
		ID: "01JGPQ00000000000000000002", ContextType: "message", ContextID: p.ID,
		Options: []api.PollOption{{Idx: 0, Text: "x", VoteCount: 99}},
	}}))
	if got := f.Post(p.ID).Poll; got.ID != "01JGPQ00000000000000000001" || got.Options[0].VoteCount != 5 {
		t.Fatalf("message poll touched the post: %#v", got)
	}

	// SetPoll (REST) is authoritative for my_votes
	f.SetPoll(api.Poll{ID: "01JGPQ00000000000000000001", ContextType: "post", ContextID: p.ID,
		Options: []api.PollOption{{Idx: 0, VoteCount: 4}, {Idx: 1, VoteCount: 2}}, MyVotes: []int{}})
	if got := f.Post(p.ID).Poll.MyVotes; len(got) != 0 {
		t.Fatalf("REST SetPoll must lower my_votes, got %v", got)
	}
}

type blockSeed struct {
	hanaPost, devPost, hanaReply api.Post
	hanaRepost                   api.TimelineItem
	parentID                     string
	notifHana, notifMinji        api.Notification
}

// seedBlock: hana's post in following+discover, hana's repost of dev_kim's post,
// dev_kim's own post item, a reply by hana under dev_kim's post, notifications by hana and minji,
// stories by hana and minji.
func seedBlock(t *testing.T, f *feedstore.FeedStore) blockSeed {
	t.Helper()
	var b blockSeed
	b.hanaPost = post("01JEPST0000000000000000001", hana, "hana post", 0)
	b.devPost = post("01JEPST0000000000000000002", devKim, "dev post", 1)
	b.hanaRepost = repost("01JEHRP0000000000000000001", b.devPost, hana, 2)
	b.parentID = b.devPost.ID
	b.hanaReply = post("01JEPST0000000000000000003", hana, "hana reply", 3)
	b.hanaReply.ReplyToID = strp(b.devPost.ID)
	minjiReply := post("01JEPST0000000000000000004", minji, "minji reply", 4)
	minjiReply.ReplyToID = strp(b.devPost.ID)
	f.ObservePosts(b.hanaPost, b.devPost, b.hanaReply, minjiReply)
	f.Timeline("following").Upsert(item(b.hanaPost), item(b.devPost), b.hanaRepost)
	f.Timeline("discover").Upsert(item(b.hanaPost), item(b.devPost))
	f.Replies(b.devPost.ID).Upsert(b.hanaReply, minjiReply)
	h, m := hana, minji
	b.notifHana = api.Notification{ID: "01JFNTF0000000000000000001", Type: "post_like", Actor: &h, CreatedAt: base}
	b.notifMinji = api.Notification{ID: "01JFNTF0000000000000000002", Type: "post_like", Actor: &m, CreatedAt: base.Add(time.Minute)}
	f.Notifications().Upsert(b.notifHana, b.notifMinji)
	f.SetStories([]api.StoryGroup{
		{User: hana, Stories: []api.Story{{ID: "01JSTRY000000000000000001", Author: hana, ExpiresAt: base.Add(24 * time.Hour)}}, HasUnseen: true},
		{User: minji, Stories: []api.Story{{ID: "01JSTRY000000000000000002", Author: minji, ExpiresAt: base.Add(24 * time.Hour)}}, HasUnseen: true},
	})
	return b
}

type windowsSnap struct {
	Following, Discover, Replies []string
	Notifs                       []string
	Stories                      []string
}

func snap(f *feedstore.FeedStore, parentID string) windowsSnap {
	var w windowsSnap
	for _, it := range f.Timeline("following").Items() {
		w.Following = append(w.Following, it.ID)
	}
	for _, it := range f.Timeline("discover").Items() {
		w.Discover = append(w.Discover, it.ID)
	}
	for _, p := range f.Replies(parentID).Items() {
		w.Replies = append(w.Replies, p.ID)
	}
	for _, n := range f.Notifications().Items() {
		w.Notifs = append(w.Notifs, n.ID)
	}
	for _, g := range f.Stories() {
		w.Stories = append(w.Stories, g.User.ID)
	}
	return w
}

func TestBlockHidesAuthorEverywhere(t *testing.T) {
	s, f := newFeed(t)
	b := seedBlock(t, f)
	s.Apply(ev(t, "block.updated", map[string]any{"user_id": hana.ID, "blocked": true}))
	if !s.IsBlocked(hana.ID) {
		t.Fatal("core must apply block.updated first")
	}
	got := snap(f, b.parentID)
	want := windowsSnap{
		Following: []string{b.devPost.ID},
		Discover:  []string{b.devPost.ID},
		Replies:   []string{"01JEPST0000000000000000004"},
		Notifs:    []string{b.notifMinji.ID},
		Stories:   []string{minji.ID},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("after block:\n got %+v\nwant %+v", got, want)
	}
	if f.Post(b.devPost.ID) == nil {
		t.Fatal("dev_kim's original post must stay cached")
	}

	// later events from the blocked author are not inserted
	late := post("01JEPST0000000000000000009", hana, "late", 9)
	s.Apply(ev(t, "timeline.item_created", map[string]any{"item": item(late)}))
	late2 := repost("01JEHRP0000000000000000009", b.devPost, hana, 9)
	s.Apply(ev(t, "timeline.item_created", map[string]any{"item": late2}))
	lateReply := post("01JEPST0000000000000000008", hana, "late reply", 9)
	s.Apply(ev(t, "post.reply_created", map[string]any{"parent_id": b.parentID, "post": lateReply}))
	ha := hana
	s.Apply(ev(t, "notification.created", map[string]any{"notification": api.Notification{ID: "01JFNTF0000000000000000009", Actor: &ha, CreatedAt: base}, "unread_count": 1}))
	if got2 := snap(f, b.parentID); !reflect.DeepEqual(got2, want) {
		t.Fatalf("blocked author's events inserted:\n got %+v\nwant %+v", got2, want)
	}

	// unblocking does not resurrect anything
	s.Apply(ev(t, "block.updated", map[string]any{"user_id": hana.ID, "blocked": false}))
	if got3 := snap(f, b.parentID); !reflect.DeepEqual(got3, want) {
		t.Fatalf("unblock changed windows: %+v", got3)
	}
}

func TestHideAuthorMatchesBlockEvent(t *testing.T) {
	sA, fA := newFeed(t)
	b := seedBlock(t, fA)
	sA.Apply(ev(t, "block.updated", map[string]any{"user_id": hana.ID, "blocked": true}))

	_, fB := newFeed(t)
	seedBlock(t, fB)
	fB.HideAuthor(hana.ID)

	if a, bb := snap(fA, b.parentID), snap(fB, b.parentID); !reflect.DeepEqual(a, bb) {
		t.Fatalf("HideAuthor differs from block.updated:\n event %+v\n local %+v", a, bb)
	}
}

func TestNotificationsRead(t *testing.T) {
	ids := []string{"01JFNTF0000000000000000001", "01JFNTF0000000000000000002", "01JFNTF0000000000000000003", "01JFNTF0000000000000000004"}
	seed := func(t *testing.T) (*store.Store, *feedstore.FeedStore) {
		s, f := newFeed(t)
		for i, id := range ids {
			f.Notifications().Upsert(api.Notification{ID: id, Type: "follow", CreatedAt: base.Add(time.Duration(i) * time.Minute)})
		}
		return s, f
	}
	readOf := func(f *feedstore.FeedStore) []bool {
		var out []bool
		for _, n := range f.Notifications().Items() {
			out = append(out, n.Read)
		}
		return out
	}
	cases := []struct {
		name string
		data map[string]any
		want []bool
	}{
		{"ids", map[string]any{"ids": []string{ids[1], ids[3]}, "all": false, "before": nil, "unread_count": 2}, []bool{false, true, false, true}},
		{"all", map[string]any{"ids": nil, "all": true, "before": nil, "unread_count": 0}, []bool{true, true, true, true}},
		{"before", map[string]any{"ids": nil, "all": false, "before": ids[2], "unread_count": 1}, []bool{true, true, true, false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, f := seed(t)
			s.Apply(ev(t, "notifications.read", tc.data))
			if got := readOf(f); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("read = %v, want %v", got, tc.want)
			}
			if s.NotifUnread != tc.data["unread_count"].(int) {
				t.Fatalf("core NotifUnread = %d", s.NotifUnread)
			}
		})
	}
	// notification.created inserts into the window
	s, f := seed(t)
	m := minji
	s.Apply(ev(t, "notification.created", map[string]any{"notification": api.Notification{ID: "01JFNTF0000000000000000009", Actor: &m, CreatedAt: base}, "unread_count": 5}))
	if _, ok := f.Notifications().Get("01JFNTF0000000000000000009"); !ok {
		t.Fatal("notification.created not inserted")
	}
}

func TestStoryEvents(t *testing.T) {
	s, f := newFeed(t)
	exp := base.Add(24 * time.Hour)
	f.SetStories([]api.StoryGroup{
		{User: hana, Stories: []api.Story{{ID: "01JSTRY000000000000000001", Author: hana, ExpiresAt: exp, Seen: true}}, HasUnseen: false, LatestAt: base},
		{User: minji, Stories: []api.Story{
			{ID: "01JSTRY000000000000000002", Author: minji, ExpiresAt: base.Add(time.Hour), Seen: true},
			{ID: "01JSTRY000000000000000003", Author: minji, ExpiresAt: exp, Seen: false},
		}, HasUnseen: true, LatestAt: base},
	})
	if f.StoriesStale() {
		t.Fatal("SetStories must leave the ring fresh")
	}

	// story.deleted: removes the story, drops the group when empty
	s.Apply(ev(t, "story.deleted", map[string]any{"user_id": hana.ID, "story_id": "01JSTRY000000000000000001"}))
	if gs := f.Stories(); len(gs) != 1 || gs[0].User.ID != minji.ID {
		t.Fatalf("after delete: %+v", gs)
	}

	// MarkStorySeen recomputes has_unseen
	f.MarkStorySeen("01JSTRY000000000000000003")
	if g := f.Stories()[0]; g.HasUnseen || !g.Stories[1].Seen {
		t.Fatalf("MarkStorySeen: %+v", g)
	}

	// story.created for a known group => has_unseen
	s.Apply(ev(t, "story.created", map[string]any{"user_id": minji.ID, "story_id": "01JSTRY000000000000000004", "expires_at": exp}))
	if g := f.Stories()[0]; !g.HasUnseen {
		t.Fatalf("story.created for known group must set has_unseen: %+v", g)
	}

	// story.created for an unknown group => stale ring
	s.Apply(ev(t, "story.created", map[string]any{"user_id": devKim.ID, "story_id": "01JSTRY000000000000000005", "expires_at": exp}))
	if !f.StoriesStale() {
		t.Fatal("unknown group must mark stories stale")
	}
	f.SetStories(f.Stories())
	if f.StoriesStale() {
		t.Fatal("SetStories must clear stale")
	}

	// PruneStories drops expired stories, then empty groups
	f.PruneStories(base.Add(2 * time.Hour))
	if g := f.Stories()[0]; len(g.Stories) != 1 || g.Stories[0].ID != "01JSTRY000000000000000003" {
		t.Fatalf("prune: %+v", g.Stories)
	}
	f.PruneStories(exp)
	if gs := f.Stories(); len(gs) != 0 {
		t.Fatalf("prune at expires_at must drop all: %+v", gs)
	}
}

func TestUserUpdatedRefreshesAuthors(t *testing.T) {
	s, f := newFeed(t)
	p := post("01JEPST0000000000000000001", hana, "snow", 0)
	dp := post("01JEPST0000000000000000002", devKim, "dev", 1)
	rp := repost("01JEHRP0000000000000000001", dp, hana, 2)
	f.ObservePosts(p, dp)
	f.Timeline("following").Upsert(item(p), rp)
	h := hana
	f.Notifications().Upsert(api.Notification{ID: "01JFNTF0000000000000000001", Actor: &h, CreatedAt: base})
	f.SetStories([]api.StoryGroup{{User: hana, Stories: []api.Story{{ID: "01JSTRY000000000000000001", Author: hana}}}})

	upd := hana
	upd.DisplayName = "하나2"
	s.Apply(ev(t, "user.updated", map[string]any{"user": upd}))

	if got := f.Post(p.ID).Author.DisplayName; got != "하나2" {
		t.Fatalf("post author = %q", got)
	}
	if got, _ := f.Timeline("following").Get(rp.ID); got.RepostedBy == nil || got.RepostedBy.DisplayName != "하나2" {
		t.Fatalf("reposted_by = %+v", got.RepostedBy)
	}
	if got, _ := f.Notifications().Get("01JFNTF0000000000000000001"); got.Actor.DisplayName != "하나2" {
		t.Fatalf("notification actor = %q", got.Actor.DisplayName)
	}
	if g := f.Stories()[0]; g.User.DisplayName != "하나2" || g.Stories[0].Author.DisplayName != "하나2" {
		t.Fatalf("story group = %+v", g)
	}
	if got := f.Post(dp.ID).Author.DisplayName; got != "김개발" {
		t.Fatalf("other author changed: %q", got)
	}
}

func TestReset(t *testing.T) {
	s, f := newFeed(t)
	b := seedBlock(t, f)
	replies := f.Replies(b.parentID)
	following := f.Timeline("following")
	f.SetTrends(&api.Trends{})
	f.SetDiscoverNew(3)
	f.SetOpen("timeline:following", true)
	f.SetOpen("post:"+b.devPost.ID, true)

	s.Reset()

	if following.Len() != 0 || f.Timeline("following").Len() != 0 || f.Timeline("discover").Len() != 0 {
		t.Fatal("timelines not cleared")
	}
	if replies.Len() != 0 || f.HasReplies(b.parentID) {
		t.Fatal("replies not cleared")
	}
	if f.Notifications().Len() != 0 {
		t.Fatal("notifications not cleared")
	}
	if f.Stories() != nil || f.Trends() != nil || f.NewDiscoverCount() != 0 || f.StoriesStale() {
		t.Fatalf("stories/trends/discover not cleared: %v %v %d", f.Stories(), f.Trends(), f.NewDiscoverCount())
	}
	if f.Post(b.devPost.ID) != nil {
		t.Fatal("post cache not cleared")
	}
	if f.IsOpen("timeline:following") || f.IsOpen("post:"+b.devPost.ID) {
		t.Fatal("open set not cleared")
	}
}

func TestObservePostsRESTIsAuthoritative(t *testing.T) {
	_, f := newFeed(t)
	p := post("01JEPST0000000000000000001", hana, "snow", 0)
	p.Viewer.Liked, p.LikeCount = true, 13
	f.ObservePosts(p)
	held := f.Post(p.ID)

	p2 := p
	p2.Viewer.Liked, p2.LikeCount = false, 12 // REST refetch after unliking on another device
	f.ObservePosts(p2)

	if f.Post(p.ID) != held {
		t.Fatal("ObservePosts must update the cached post in place")
	}
	if held.Viewer.Liked || held.LikeCount != 12 {
		t.Fatalf("held = liked %v count %d", held.Viewer.Liked, held.LikeCount)
	}

	// quote.post is cached too
	q := post("01JEPST0000000000000000005", devKim, "quoted", 1)
	qp := post("01JEPST0000000000000000006", minji, "quoting", 2)
	qp.Quote = &api.QuotedPost{Available: true, Post: &q}
	f.ObservePosts(qp)
	if got := f.Post(q.ID); got == nil || got.Body != "quoted" {
		t.Fatalf("quote.post not cached: %#v", got)
	}
}

func TestEventPostKeepsViewer(t *testing.T) {
	s, f := newFeed(t)
	p := post("01JEPST0000000000000000001", hana, "poll post", 0)
	p.Viewer.Liked, p.LikeCount = true, 12
	p.Poll = &api.Poll{ID: "01JGPQ00000000000000000001", ContextType: "post", ContextID: p.ID,
		Options: []api.PollOption{{Idx: 0, VoteCount: 1}, {Idx: 1}}, MyVotes: []int{0}}
	f.ObservePosts(p)
	held := f.Post(p.ID)

	evp := p
	evp.Viewer = api.PostViewer{} // events.md 4.4 default values
	evp.LikeCount = 20
	evp.Body = "poll post (edited)"
	evp.Poll = &api.Poll{ID: "01JGPQ00000000000000000001", ContextType: "post", ContextID: p.ID,
		Options: []api.PollOption{{Idx: 0, VoteCount: 3}, {Idx: 1}}, MyVotes: []int{}}
	s.Apply(ev(t, "timeline.item_created", map[string]any{"item": item(evp)}))

	if f.Post(p.ID) != held {
		t.Fatal("event post must update the cached post in place")
	}
	if !held.Viewer.Liked || held.LikeCount != 20 || held.Body != "poll post (edited)" {
		t.Fatalf("held = %+v", held)
	}
	if !reflect.DeepEqual(held.Poll.MyVotes, []int{0}) || held.Poll.Options[0].VoteCount != 3 {
		t.Fatalf("poll = %+v", held.Poll)
	}

	// post.reply_created keeps the viewer too
	r := post("01JEPST0000000000000000002", minji, "reply", 1)
	r.Viewer.Reposted = true
	f.ObservePosts(r)
	evr := r
	evr.Viewer = api.PostViewer{}
	s.Apply(ev(t, "post.reply_created", map[string]any{"parent_id": p.ID, "post": evr}))
	if !f.Post(r.ID).Viewer.Reposted {
		t.Fatal("post.reply_created overwrote viewer")
	}

	// an event post not yet cached is stored as given
	n := post("01JEPST0000000000000000003", devKim, "new", 2)
	s.Apply(ev(t, "timeline.item_created", map[string]any{"item": item(n)}))
	if got := f.Post(n.ID); got == nil || got.Body != "new" {
		t.Fatalf("new event post = %#v", got)
	}
}

func TestRemoveStory(t *testing.T) {
	s, f := newFeed(t)
	exp := base.Add(24 * time.Hour)
	f.RemoveStory("01JSTRY000000000000000001") // nil ring: no-op
	if f.Stories() != nil {
		t.Fatal("RemoveStory on a nil ring must keep it nil")
	}
	f.SetStories([]api.StoryGroup{
		{User: hana, Stories: []api.Story{{ID: "01JSTRY000000000000000001", Author: hana, ExpiresAt: exp, Seen: true}}, LatestAt: base},
		{User: minji, Stories: []api.Story{
			{ID: "01JSTRY000000000000000002", Author: minji, ExpiresAt: exp, Seen: false},
			{ID: "01JSTRY000000000000000003", Author: minji, ExpiresAt: exp, Seen: true},
		}, HasUnseen: true, LatestAt: base},
	})
	s.Apply(ev(t, "story.created", map[string]any{"user_id": devKim.ID, "story_id": "01JSTRY000000000000000009", "expires_at": exp}))
	if !f.StoriesStale() {
		t.Fatal("story.created for an unknown group must mark the ring stale")
	}

	// a removed story leaves its group (has_unseen recomputed) and keeps the stale flag
	f.RemoveStory("01JSTRY000000000000000002")
	if gs := f.Stories(); len(gs) != 2 || len(gs[1].Stories) != 1 || gs[1].HasUnseen {
		t.Fatalf("after removing minji's unseen story: %+v", gs)
	}
	if !f.StoriesStale() {
		t.Fatal("RemoveStory must not clear the stale flag")
	}
	// the last story of a group drops the group
	f.RemoveStory("01JSTRY000000000000000001")
	if gs := f.Stories(); len(gs) != 1 || gs[0].User.ID != minji.ID {
		t.Fatalf("after removing hana's only story: %+v", gs)
	}
}

func notif(id string, min int) api.Notification {
	a := hana
	return api.Notification{ID: id, Type: "post_like", Actor: &a, CreatedAt: base.Add(time.Duration(min) * time.Minute)}
}

func notifIDs(f *feedstore.FeedStore) []string {
	var out []string
	for _, n := range f.Notifications().Items() {
		out = append(out, n.ID)
	}
	return out
}

// An inbox window filled only by notification.created is not loaded; a first
// page replaces it, keeps event items the page does not hold yet, and keeps
// the next cursor so a rebuilt inbox can still page older items.
func TestNotificationsPageStateSurvivesEvents(t *testing.T) {
	s, f := newFeed(t)
	s.Apply(ev(t, "notification.created", map[string]any{"notification": notif("N9", 9), "unread_count": 1}))
	if f.NotificationsLoaded() || f.NotificationsCursor() != nil {
		t.Fatalf("event-only window: loaded=%v cursor=%v, want false/nil", f.NotificationsLoaded(), f.NotificationsCursor())
	}
	f.SetNotificationsPage(&api.NotificationPage{Items: []api.Notification{notif("N5", 5), notif("N4", 4)}, NextCursor: strp("c4")})
	if !f.NotificationsLoaded() {
		t.Fatal("NotificationsLoaded = false after a first page")
	}
	if c := f.NotificationsCursor(); c == nil || *c != "c4" {
		t.Fatalf("cursor = %v, want c4", c)
	}
	if got, want := notifIDs(f), []string{"N4", "N5", "N9"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("window = %v, want %v (event item kept)", got, want)
	}
	if !f.Notifications().HasMoreBefore() {
		t.Fatal("HasMoreBefore = false with a next cursor")
	}
	f.AddOlderNotifications(&api.NotificationPage{Items: []api.Notification{notif("N3", 3)}})
	if f.NotificationsCursor() != nil || f.Notifications().HasMoreBefore() {
		t.Fatalf("after the last page: cursor=%v more=%v, want nil/false", f.NotificationsCursor(), f.Notifications().HasMoreBefore())
	}
	if got, want := notifIDs(f), []string{"N3", "N4", "N5", "N9"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("window = %v, want %v", got, want)
	}
	f.Reset()
	if f.NotificationsLoaded() || f.NotificationsCursor() != nil || f.Notifications().Len() != 0 {
		t.Fatal("Reset kept notification page state")
	}
}

// A first page drops window items older than the page (they belong to older
// pages) and keeps everything when the page is empty.
func TestNotificationsPageDropsOlderKeepsAllOnEmpty(t *testing.T) {
	_, f := newFeed(t)
	f.Notifications().Upsert(notif("N1", 1), notif("N9", 9))
	f.SetNotificationsPage(&api.NotificationPage{Items: []api.Notification{notif("N5", 5)}})
	if got, want := notifIDs(f), []string{"N5", "N9"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("window = %v, want %v", got, want)
	}
	f.SetNotificationsPage(&api.NotificationPage{Items: []api.Notification{}})
	if got, want := notifIDs(f), []string{"N5", "N9"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("empty page: window = %v, want %v", got, want)
	}
}
