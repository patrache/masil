package feedstore_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/store/feedstore"
)

// drain runs cmd and flattens tea.BatchMsg recursively.
func drain(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, drain(t, c)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

// resyncMsgs drains cmd and asserts every message is a feedstore.ResyncMsg.
func resyncMsgs(t *testing.T, cmd tea.Cmd) []feedstore.ResyncMsg {
	t.Helper()
	var out []feedstore.ResyncMsg
	for _, m := range drain(t, cmd) {
		rm, ok := m.(feedstore.ResyncMsg)
		if !ok {
			t.Fatalf("unexpected message %T", m)
		}
		out = append(out, rm)
	}
	return out
}

func byKey(t *testing.T, ms []feedstore.ResyncMsg, key string) feedstore.ResyncMsg {
	t.Helper()
	for _, m := range ms {
		if m.Key == key {
			return m
		}
	}
	t.Fatalf("no ResyncMsg for %q in %d messages", key, len(ms))
	return feedstore.ResyncMsg{}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("encode: %v", err)
	}
}

func at(sec int) time.Time { return base.Add(time.Duration(sec) * time.Second) }

func itemAt(id string, sec int) api.TimelineItem {
	p := post(id, devKim, "글 "+id, 0)
	p.CreatedAt = at(sec)
	return api.TimelineItem{ID: id, Type: "post", Post: p, At: at(sec)}
}

func ids[T any](items []T, idOf func(T) string) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = idOf(it)
	}
	return out
}

func itemIDs(items []api.TimelineItem) []string {
	return ids(items, func(it api.TimelineItem) string { return it.ID })
}

func postIDs(ps []api.Post) []string { return ids(ps, func(p api.Post) string { return p.ID }) }

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func query(t *testing.T, raw string) url.Values {
	t.Helper()
	v, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatalf("parse query %q: %v", raw, err)
	}
	return v
}

func TestResyncNothingOpenReturnsNil(t *testing.T) {
	s, f := newFeed(t)
	srv := apitest.New(t)
	if f.Resync(s, srv.Client()) != nil {
		t.Fatal("want nil cmd")
	}
	// Opened then closed is nothing open too.
	f.SetOpen("timeline:following", true)
	f.SetOpen("timeline:following", false)
	if f.Resync(s, srv.Client()) != nil {
		t.Fatal("want nil cmd after close")
	}
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("requests sent: %d", n)
	}
}

func TestResyncNilClientReturnsNil(t *testing.T) {
	s, f := newFeed(t)
	f.SetOpen("timeline:following", true)
	if f.Resync(s, nil) != nil {
		t.Fatal("want nil cmd without a client")
	}
}

func TestResyncFollowingUsesOverlapAnchor(t *testing.T) {
	s, f := newFeed(t)
	a, b, c := itemAt("T1", 0), itemAt("T2", 3), itemAt("T3", 9)
	f.ObservePosts(a.Post, b.Post, c.Post)
	f.Timeline(feedstore.KindFollowing).Replace([]api.TimelineItem{a, b, c}, true, false)
	f.SetOpen("timeline:following", true)

	d := itemAt("T4", 20)
	c2 := c
	c2.Post.Body = "글 T3 고침"
	srv := apitest.New(t)
	srv.JSON("GET /v1/timeline/following", 200, api.TimelinePage{Items: []api.TimelineItem{d, c2}, Gap: false})

	ms := resyncMsgs(t, f.Resync(s, srv.Client()))
	if len(ms) != 1 {
		t.Fatalf("messages = %d, want 1", len(ms))
	}
	reqs := srv.Requests()
	if len(reqs) != 1 || reqs[0].Path != "/v1/timeline/following" {
		t.Fatalf("requests = %+v", reqs)
	}
	if got := query(t, reqs[0].Query).Get("since"); got != b.ID {
		t.Fatalf("since = %q, want %q (overlap anchor)", got, b.ID)
	}
	m := ms[0]
	if m.Key != "timeline:following" || m.Restart || m.Err != nil || m.Timeline == nil || m.Seq == 0 {
		t.Fatalf("msg = %+v", m)
	}

	f.ApplyResync(m)
	w := f.Timeline(feedstore.KindFollowing)
	if got := itemIDs(w.Items()); !eqStrings(got, []string{"T1", "T2", "T3", "T4"}) {
		t.Fatalf("window = %v", got)
	}
	if !w.HasMoreBefore() {
		t.Fatal("upsert must keep the older-page flag")
	}
	if p := f.Post("T4"); p == nil || p.Body != "글 T4" {
		t.Fatalf("new post not cached: %+v", p)
	}
	if p := f.Post("T3"); p == nil || p.Body != "글 T3 고침" {
		t.Fatalf("overlapping post not refreshed: %+v", p)
	}
}

func TestResyncGapRestarts(t *testing.T) {
	s, f := newFeed(t)
	a, b, c := itemAt("T1", 0), itemAt("T2", 3), itemAt("T3", 9)
	f.Timeline(feedstore.KindFollowing).Replace([]api.TimelineItem{a, b, c}, false, false)
	f.SetOpen("timeline:following", true)

	x, y := itemAt("T8", 100), itemAt("T9", 110)
	next := "cur2"
	srv := apitest.New(t)
	srv.Handle("GET /v1/timeline/following", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("since") != "" {
			writeJSON(t, w, api.TimelinePage{Items: []api.TimelineItem{y}, Gap: true})
			return
		}
		writeJSON(t, w, api.TimelinePage{Items: []api.TimelineItem{y, x}, NextCursor: &next})
	})

	ms := resyncMsgs(t, f.Resync(s, srv.Client()))
	if len(ms) != 1 {
		t.Fatalf("messages = %d", len(ms))
	}
	reqs := srv.Requests()
	if len(reqs) != 2 {
		t.Fatalf("want since request then first page, got %+v", reqs)
	}
	if query(t, reqs[0].Query).Get("since") == "" || query(t, reqs[1].Query).Has("since") {
		t.Fatalf("queries = %q, %q", reqs[0].Query, reqs[1].Query)
	}
	m := ms[0]
	if !m.Restart || m.Timeline == nil || m.Timeline.Gap {
		t.Fatalf("want Restart with the first page, got %+v", m)
	}
	f.ApplyResync(m)
	w := f.Timeline(feedstore.KindFollowing)
	if got := itemIDs(w.Items()); !eqStrings(got, []string{"T8", "T9"}) {
		t.Fatalf("window = %v, want page items only", got)
	}
	if !w.HasMoreBefore() {
		t.Fatal("first page with next_cursor must mark older items")
	}
}

func TestResyncDiscoverReplacesAndClearsBanner(t *testing.T) {
	s, f := newFeed(t)
	f.Timeline(feedstore.KindDiscover).Replace([]api.TimelineItem{itemAt("T1", 0)}, false, false)
	f.SetDiscoverNew(7)
	f.SetOpen("timeline:discover", true)

	srv := apitest.New(t)
	srv.JSON("GET /v1/timeline/discover", 200, api.TimelinePage{Items: []api.TimelineItem{itemAt("T5", 50)}})
	ms := resyncMsgs(t, f.Resync(s, srv.Client()))
	if reqs := srv.Requests(); len(reqs) != 1 || reqs[0].Query != "" {
		t.Fatalf("want one since-less first page request, got %+v", reqs)
	}
	f.ApplyResync(byKey(t, ms, "timeline:discover"))
	if got := itemIDs(f.Timeline(feedstore.KindDiscover).Items()); !eqStrings(got, []string{"T5"}) {
		t.Fatalf("discover = %v", got)
	}
	if f.NewDiscoverCount() != 0 {
		t.Fatalf("banner = %d", f.NewDiscoverCount())
	}
}

func TestResyncRefetchesOpenPost(t *testing.T) {
	s, f := newFeed(t)
	p := post("P", hana, "원글", 0)
	p.Viewer.Liked = true
	p.LikeCount = 3
	f.ObservePosts(p)
	held := f.Post("P")
	f.Replies("P").Replace([]api.Post{post("R0", minji, "옛 답글", 1)}, false, false)
	f.SetOpen("post:P", true)

	fresh := post("P", hana, "원글", 0)
	fresh.LikeCount = 2 // unliked on another device
	r1, r2 := post("R1", minji, "답글 1", 2), post("R2", devKim, "답글 2", 3)
	srv := apitest.New(t)
	srv.JSON("GET /v1/posts/{id}", 200, map[string]any{"post": fresh})
	srv.JSON("GET /v1/posts/{id}/replies", 200, api.Page[api.Post]{Items: []api.Post{r1, r2}})

	ms := resyncMsgs(t, f.Resync(s, srv.Client()))
	var paths []string
	for _, r := range srv.Requests() {
		paths = append(paths, r.Method+" "+r.Path)
	}
	if !eqStrings(paths, []string{"GET /v1/posts/P", "GET /v1/posts/P/replies"}) {
		t.Fatalf("requests = %v", paths)
	}
	m := byKey(t, ms, "post:P")
	if m.Post == nil || m.Replies == nil || m.Err != nil {
		t.Fatalf("msg = %+v", m)
	}
	f.ApplyResync(m)
	if f.Post("P") != held {
		t.Fatal("cached post pointer changed")
	}
	if held.Viewer.Liked || held.LikeCount != 2 {
		t.Fatalf("REST result must be authoritative: %+v", held)
	}
	if got := postIDs(f.Replies("P").Items()); !eqStrings(got, []string{"R1", "R2"}) {
		t.Fatalf("replies = %v", got)
	}
	if f.Post("R1") == nil {
		t.Fatal("replies not cached")
	}
}

func TestResyncOpenPostGoneIsDeleted(t *testing.T) {
	s, f := newFeed(t)
	p := post("P", hana, "원글", 0)
	f.ObservePosts(p)
	f.Timeline(feedstore.KindFollowing).Replace([]api.TimelineItem{item(p)}, false, false)
	f.SetOpen("post:P", true)

	srv := apitest.New(t)
	srv.Error("GET /v1/posts/{id}", 404, "not_found")
	srv.Error("GET /v1/posts/{id}/replies", 404, "not_found")
	f.ApplyResync(byKey(t, resyncMsgs(t, f.Resync(s, srv.Client())), "post:P"))
	if got := f.Post("P"); got == nil || !got.Deleted || got.Body != "" {
		t.Fatalf("post = %+v, want deleted placeholder", got)
	}
	if f.Timeline(feedstore.KindFollowing).Len() != 0 {
		t.Fatal("deleted post still in the timeline")
	}
}

func TestResyncStoriesAndNotifications(t *testing.T) {
	s, f := newFeed(t)
	f.Notifications().Replace([]api.Notification{{ID: "N0", CreatedAt: at(0)}}, false, false)
	f.SetOpen("stories", true)
	f.SetOpen("notifications", true)

	g := api.StoryGroup{User: hana, Stories: []api.Story{{ID: "S1", Author: hana, ExpiresAt: at(3600)}}, HasUnseen: true}
	next := "c"
	srv := apitest.New(t)
	srv.JSON("GET /v1/stories/feed", 200, map[string]any{"items": []api.StoryGroup{g}})
	srv.JSON("GET /v1/notifications", 200, api.NotificationPage{Items: []api.Notification{{ID: "N2", CreatedAt: at(20)}, {ID: "N1", CreatedAt: at(10)}}, NextCursor: &next})

	ms := resyncMsgs(t, f.Resync(s, srv.Client()))
	if len(ms) != 2 {
		t.Fatalf("messages = %d", len(ms))
	}
	for _, m := range ms {
		f.ApplyResync(m)
	}
	if st := f.Stories(); len(st) != 1 || st[0].User.ID != hana.ID || len(st[0].Stories) != 1 {
		t.Fatalf("stories = %+v", st)
	}
	w := f.Notifications()
	if got := ids(w.Items(), func(n api.Notification) string { return n.ID }); !eqStrings(got, []string{"N1", "N2"}) {
		t.Fatalf("notifications = %v", got)
	}
	if !w.HasMoreBefore() {
		t.Fatal("next_cursor must mark older notifications")
	}
}

func TestSetOpenKeepsLastEightPosts(t *testing.T) {
	s, f := newFeed(t)
	for i := 1; i <= 9; i++ {
		f.SetOpen("post:P"+string(rune('0'+i)), true)
	}
	srv := apitest.New(t)
	srv.Handle("GET /v1/posts/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"post": post(r.PathValue("id"), hana, "글", 0)})
	})
	srv.JSON("GET /v1/posts/{id}/replies", 200, api.Page[api.Post]{Items: []api.Post{}})

	ms := resyncMsgs(t, f.Resync(s, srv.Client()))
	if len(ms) != 8 {
		t.Fatalf("messages = %d, want 8", len(ms))
	}
	var got []string
	for _, r := range srv.Requests() {
		if !strings.HasSuffix(r.Path, "/replies") {
			got = append(got, strings.TrimPrefix(r.Path, "/v1/posts/"))
		}
	}
	if !eqStrings(got, []string{"P2", "P3", "P4", "P5", "P6", "P7", "P8", "P9"}) {
		t.Fatalf("refetched posts = %v (oldest P1 must be dropped)", got)
	}
}

func TestApplyResyncOncePerSeq(t *testing.T) {
	s, f := newFeed(t)
	p := post("P", hana, "원글", 0)
	f.ObservePosts(p)
	f.SetOpen("post:P", true)

	fresh := p
	fresh.LikeCount = 10
	srv := apitest.New(t)
	srv.JSON("GET /v1/posts/{id}", 200, map[string]any{"post": fresh})
	srv.JSON("GET /v1/posts/{id}/replies", 200, api.Page[api.Post]{Items: []api.Post{post("R1", minji, "답글", 1)}})

	m := byKey(t, resyncMsgs(t, f.Resync(s, srv.Client())), "post:P")
	f.ApplyResync(m)
	if f.Post("P").LikeCount != 10 {
		t.Fatalf("first apply: like_count = %d", f.Post("P").LikeCount)
	}
	s.Apply(ev(t, "post.stats", map[string]any{"post_id": "P", "like_count": 30, "repost_count": 0, "reply_count": 1, "quote_count": 0}))
	f.Replies("P").Upsert(post("R9", devKim, "실시간 답글", 5))

	f.ApplyResync(m) // a second forwarder
	if got := f.Post("P").LikeCount; got != 30 {
		t.Fatalf("second apply of the same Seq changed like_count to %d", got)
	}
	if got := postIDs(f.Replies("P").Items()); !eqStrings(got, []string{"R1", "R9"}) {
		t.Fatalf("second apply of the same Seq replaced replies: %v", got)
	}

	// Two separate Resync runs => different Seqs, both applied (also out of order).
	m1 := byKey(t, resyncMsgs(t, f.Resync(s, srv.Client())), "post:P")
	m2 := byKey(t, resyncMsgs(t, f.Resync(s, srv.Client())), "post:P")
	if m1.Seq == m.Seq || m2.Seq == m1.Seq {
		t.Fatalf("seqs not distinct: %d %d %d", m.Seq, m1.Seq, m2.Seq)
	}
	f.ApplyResync(m2)
	if got := f.Post("P").LikeCount; got != 10 {
		t.Fatalf("m2 not applied: like_count = %d", got)
	}
	f.Post("P").LikeCount = 31
	f.ApplyResync(m1)
	if got := f.Post("P").LikeCount; got != 10 {
		t.Fatalf("m1 not applied after m2: like_count = %d", got)
	}
	f.Post("P").LikeCount = 32
	f.ApplyResync(m1)
	f.ApplyResync(m2)
	if got := f.Post("P").LikeCount; got != 32 {
		t.Fatalf("re-applied: like_count = %d", got)
	}
}

func TestApplyResyncIgnoresResultsFromBeforeReset(t *testing.T) {
	s, f := newFeed(t)
	f.SetOpen("timeline:following", true)
	srv := apitest.New(t)
	srv.JSON("GET /v1/timeline/following", 200, api.TimelinePage{Items: []api.TimelineItem{itemAt("T1", 0)}})
	cmd := f.Resync(s, srv.Client())

	f.Reset() // logout / user switch while the fetch is in flight
	for _, m := range resyncMsgs(t, cmd) {
		f.ApplyResync(m)
	}
	if n := f.Timeline(feedstore.KindFollowing).Len(); n != 0 {
		t.Fatalf("stale result applied after Reset: %d items", n)
	}
	if f.Post("T1") != nil {
		t.Fatal("stale post cached after Reset")
	}
	// An unassigned Seq (hand-built message) is ignored too.
	f.ApplyResync(feedstore.ResyncMsg{Key: "timeline:following", Timeline: &api.TimelinePage{Items: []api.TimelineItem{itemAt("T2", 0)}}, Restart: true})
	if n := f.Timeline(feedstore.KindFollowing).Len(); n != 0 {
		t.Fatalf("Seq 0 message applied: %d items", n)
	}
}

func TestResyncErrorKeepsWindow(t *testing.T) {
	s, f := newFeed(t)
	f.Timeline(feedstore.KindFollowing).Replace([]api.TimelineItem{itemAt("T1", 0)}, false, false)
	f.SetOpen("timeline:following", true)
	srv := apitest.New(t)
	srv.Error("GET /v1/timeline/following", 500, "internal_error")
	m := byKey(t, resyncMsgs(t, f.Resync(s, srv.Client())), "timeline:following")
	if m.Err == nil {
		t.Fatal("want Err")
	}
	f.ApplyResync(m)
	if got := itemIDs(f.Timeline(feedstore.KindFollowing).Items()); !eqStrings(got, []string{"T1"}) {
		t.Fatalf("window = %v", got)
	}
}

// A notifications resync loads the window like a first page: loaded, the next
// cursor kept, event items the page does not hold yet kept.
func TestResyncNotificationsSetsPageState(t *testing.T) {
	s, f := newFeed(t)
	f.SetOpen("notifications", true)
	s.Apply(ev(t, "notification.created", map[string]any{"notification": notif("N9", 9), "unread_count": 1}))
	srv := apitest.New(t)
	srv.JSON("GET /v1/notifications", http.StatusOK, api.NotificationPage{Items: []api.Notification{notif("N5", 5)}, NextCursor: strp("c5"), UnreadCount: 1})
	f.ApplyResync(byKey(t, resyncMsgs(t, f.Resync(s, srv.Client())), "notifications"))
	if !f.NotificationsLoaded() {
		t.Fatal("NotificationsLoaded = false after a resync")
	}
	if c := f.NotificationsCursor(); c == nil || *c != "c5" {
		t.Fatalf("cursor = %v, want c5", c)
	}
	if got, want := notifIDs(f), []string{"N5", "N9"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("window = %v, want %v", got, want)
	}
}

// replyTo is p as a reply to parent.
func replyTo(p api.Post, parent string) api.Post {
	p.ReplyToID = &parent
	return p
}

// Resync also refetches the first page of every reply window loaded deeper in
// an open post's tree (events.md 5.3 step 4: "답글 트리도 다시 받는다"); post
// detail's expand only loads a window that does not exist yet, so a deeper
// window would otherwise stay stale for the session. Windows outside every
// open tree are left alone, and a window that is itself an open post is
// fetched once (by its post refetch).
func TestResyncRefetchesExpandedReplyWindows(t *testing.T) {
	s, f := newFeed(t)
	f.ObservePosts(post("P", hana, "원글", 0), post("Q", minji, "다른 글", 0))
	r1 := replyTo(post("R1", minji, "답글", 1), "P")
	s1 := replyTo(post("S1", devKim, "답글의 답글", 2), "R1")
	page := func(ps ...api.Post) *api.Page[api.Post] { return &api.Page[api.Post]{Items: ps} }
	f.SetRepliesPage("P", page(r1)) // as loaded by post detail (posts cached too)
	f.SetRepliesPage("R1", page(s1))
	f.SetRepliesPage("S1", page(replyTo(post("T0", hana, "지워진 깊은 답글", 3), "S1")))
	f.SetRepliesPage("Q", page(replyTo(post("Q1", hana, "남의 답글", 3), "Q")))
	f.SetOpen("post:P", true)
	f.SetOpen("post:R1", true)

	tNew := replyTo(post("T1", minji, "오프라인 중 단 답글", 4), "S1")
	srv := apitest.New(t)
	srv.Handle("GET /v1/posts/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"post": post(r.PathValue("id"), hana, "글", 0)})
	})
	srv.Handle("GET /v1/posts/{id}/replies", func(w http.ResponseWriter, r *http.Request) {
		items := map[string][]api.Post{"P": {r1}, "R1": {s1}, "S1": {tNew}}[r.PathValue("id")]
		writeJSON(t, w, api.Page[api.Post]{Items: items})
	})

	ms := resyncMsgs(t, f.Resync(s, srv.Client()))
	count := map[string]int{}
	for _, r := range srv.Requests() {
		count[r.Method+" "+r.Path]++
	}
	for _, want := range []string{"GET /v1/posts/P/replies", "GET /v1/posts/R1/replies", "GET /v1/posts/S1/replies"} {
		if count[want] != 1 {
			t.Fatalf("%s requested %d times, want 1 (all: %v)", want, count[want], count)
		}
	}
	if count["GET /v1/posts/Q/replies"] != 0 {
		t.Fatalf("a window outside the open trees was refetched: %v", count)
	}
	for _, m := range ms {
		f.ApplyResync(m)
	}
	if got := postIDs(f.Replies("S1").Items()); !eqStrings(got, []string{"T1"}) {
		t.Fatalf("deep window = %v, want [T1]", got)
	}
	if got := postIDs(f.Replies("Q").Items()); !eqStrings(got, []string{"Q1"}) {
		t.Fatalf("unrelated window = %v", got)
	}
}
