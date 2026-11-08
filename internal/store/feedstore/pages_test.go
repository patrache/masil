package feedstore_test

import (
	"net/http"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/store/feedstore"
)

// An item_created that arrives before the first page must not count as a
// loaded timeline (the feed opened later still fetches its first page).
func TestTimelineLoadedOnlyByPage(t *testing.T) {
	s, f := newFeed(t)
	s.Apply(ev(t, "timeline.item_created", map[string]any{"item": itemAt("T9", 90)}))
	if f.TimelineLoaded(feedstore.KindFollowing) {
		t.Fatal("an event alone marked the following timeline loaded")
	}
	next := "c2"
	f.SetTimelinePage(feedstore.KindFollowing, &api.TimelinePage{Items: []api.TimelineItem{itemAt("T2", 20), itemAt("T1", 10)}, NextCursor: &next})
	if !f.TimelineLoaded(feedstore.KindFollowing) || f.TimelineLoaded(feedstore.KindDiscover) {
		t.Fatal("first page must mark only its kind loaded")
	}
	if c := f.TimelineCursor(feedstore.KindFollowing); c == nil || *c != "c2" {
		t.Fatalf("cursor = %v, want c2", c)
	}
	if got := itemIDs(f.Timeline(feedstore.KindFollowing).Items()); !eqStrings(got, []string{"T1", "T2", "T9"}) {
		t.Fatalf("window = %v, want the page plus the event item", got)
	}
	last := "c3"
	f.AddOlderTimeline(feedstore.KindFollowing, &api.TimelinePage{Items: []api.TimelineItem{itemAt("T0", 5)}, NextCursor: &last})
	if c := f.TimelineCursor(feedstore.KindFollowing); c == nil || *c != "c3" {
		t.Fatalf("cursor after older page = %v, want c3", c)
	}
	f.AddOlderTimeline(feedstore.KindFollowing, &api.TimelinePage{NextCursor: &last})
	if c := f.TimelineCursor(feedstore.KindFollowing); c != nil {
		t.Fatalf("an empty older page must end paging, cursor = %v", *c)
	}
	f.Reset()
	if f.TimelineLoaded(feedstore.KindFollowing) || f.TimelineCursor(feedstore.KindFollowing) != nil {
		t.Fatal("Reset must clear the loaded flag and cursor")
	}
}

// events.md 5.3 step 5: an item_created applied while the gap restart's first
// page is in flight survives ApplyResync; the restart sets cursor and loaded.
func TestResyncRestartKeepsRacedEvent(t *testing.T) {
	s, f := newFeed(t)
	f.SetTimelinePage(feedstore.KindFollowing, &api.TimelinePage{Items: []api.TimelineItem{itemAt("T1", 0), itemAt("T2", 3), itemAt("T3", 9)}})
	f.SetOpen("timeline:following", true)

	next := "cur2"
	srv := apitest.New(t)
	srv.Handle("GET /v1/timeline/following", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("since") != "" {
			writeJSON(t, w, api.TimelinePage{Gap: true})
			return
		}
		writeJSON(t, w, api.TimelinePage{Items: []api.TimelineItem{itemAt("T8", 100), itemAt("T7", 90)}, NextCursor: &next})
	})
	m := byKey(t, resyncMsgs(t, f.Resync(s, srv.Client())), "timeline:following")
	s.Apply(ev(t, "timeline.item_created", map[string]any{"item": itemAt("T9", 110)}))
	f.ApplyResync(m)
	if got := itemIDs(f.Timeline(feedstore.KindFollowing).Items()); !eqStrings(got, []string{"T7", "T8", "T9"}) {
		t.Fatalf("window = %v, want the page plus the raced T9", got)
	}
	if c := f.TimelineCursor(feedstore.KindFollowing); c == nil || *c != "cur2" {
		t.Fatalf("restart cursor = %v, want cur2", c)
	}
	if !f.TimelineLoaded(feedstore.KindFollowing) {
		t.Fatal("restart must mark the timeline loaded")
	}
}

// Items only a page brought (not events) are dropped by a later first page,
// also an empty one; event items below the new page's range are dropped too.
func TestTimelinePageDropsStaleItems(t *testing.T) {
	s, f := newFeed(t)
	f.SetTimelinePage(feedstore.KindFollowing, &api.TimelinePage{Items: []api.TimelineItem{itemAt("T1", 0)}})
	s.Apply(ev(t, "timeline.item_created", map[string]any{"item": itemAt("T2", 5)}))
	f.SetTimelinePage(feedstore.KindFollowing, &api.TimelinePage{Items: []api.TimelineItem{itemAt("T5", 50), itemAt("T4", 40)}})
	if got := itemIDs(f.Timeline(feedstore.KindFollowing).Items()); !eqStrings(got, []string{"T4", "T5"}) {
		t.Fatalf("window = %v, want only the page (T2 is below its range)", got)
	}
	s.Apply(ev(t, "timeline.item_created", map[string]any{"item": itemAt("T6", 60)}))
	f.SetTimelinePage(feedstore.KindFollowing, &api.TimelinePage{})
	if got := itemIDs(f.Timeline(feedstore.KindFollowing).Items()); !eqStrings(got, []string{"T6"}) {
		t.Fatalf("empty page: window = %v, want only the event item T6", got)
	}
}

// A reply_created applied while the post's resync is in flight survives the
// reply window's replace; so does one racing the screen's first reply page.
func TestRepliesReplaceKeepsRacedEvent(t *testing.T) {
	s, f := newFeed(t)
	p := post("P", hana, "원글", 0)
	f.ObservePosts(p)
	f.SetRepliesPage("P", &api.Page[api.Post]{Items: []api.Post{post("R0", minji, "옛 답글", 1)}})
	f.SetOpen("post:P", true)

	srv := apitest.New(t)
	srv.JSON("GET /v1/posts/{id}", 200, map[string]any{"post": p})
	srv.JSON("GET /v1/posts/{id}/replies", 200, api.Page[api.Post]{Items: []api.Post{post("R1", minji, "답글 1", 2), post("R2", devKim, "답글 2", 3)}})
	m := byKey(t, resyncMsgs(t, f.Resync(s, srv.Client())), "post:P")
	r9 := post("R9", hana, "실시간 답글", 9)
	r9.ReplyToID = strp("P")
	s.Apply(ev(t, "post.reply_created", map[string]any{"parent_id": "P", "post": r9}))
	f.ApplyResync(m)
	if got := postIDs(f.Replies("P").Items()); !eqStrings(got, []string{"R1", "R2", "R9"}) {
		t.Fatalf("replies after resync = %v, want R1 R2 R9", got)
	}

	ra := post("RA", minji, "또 답글", 10)
	ra.ReplyToID = strp("P")
	s.Apply(ev(t, "post.reply_created", map[string]any{"parent_id": "P", "post": ra}))
	f.SetRepliesPage("P", &api.Page[api.Post]{Items: []api.Post{post("R1", minji, "답글 1", 2), post("R2", devKim, "답글 2", 3)}})
	if got := postIDs(f.Replies("P").Items()); !eqStrings(got, []string{"R1", "R2", "R9", "RA"}) {
		t.Fatalf("replies after first page = %v, want R1 R2 R9 RA", got)
	}
	if f.Post("R1") == nil {
		t.Fatal("SetRepliesPage must cache the page's posts")
	}
}
