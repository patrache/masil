package feed

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// write503 answers a request with a transient server error.
func write503(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(`{"error":{"code":"unavailable","message":"down","details":{}}}`))
}

// failingTimeline makes the first `fails` first-page GETs of kind's timeline
// answer 503; later ones answer the fixture timeline.
func failingTimeline(t *testing.T, kind string, fails int32) (*uitest.Harness, *atomic.Int32) {
	t.Helper()
	srv := snsAPI(t, false)
	var left atomic.Int32
	left.Store(fails)
	srv.Handle("GET /v1/timeline/"+kind, func(w http.ResponseWriter, r *http.Request) {
		if left.Add(-1) >= 0 {
			write503(w)
			return
		}
		writeJSON(w, api.TimelinePage{Items: fixtures.Timeline()})
	})
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: kind}))
	return h, &left // the harness's 120x40 (a Resize would already retry)
}

// A failed first page shows a failure notice with the retry key, never the
// empty state (which would claim there is nothing to read), and r retries.
func TestTimelineFirstPageFailureShowsRetry(t *testing.T) {
	for _, kind := range []string{"following", "discover"} {
		t.Run(kind, func(t *testing.T) {
			h, _ := failingTimeline(t, kind, 1)
			s := h.Screen()
			for _, bad := range []string{textEmptyFollow, hintEmptyFollow, textEmptyDiscover, hintEmptyDiscover} {
				if strings.Contains(s, bad) {
					t.Fatalf("failed load must not show the empty state %q:\n%s", bad, s)
				}
			}
			if !strings.Contains(s, "피드를 불러오지 못했어요") || !strings.Contains(s, "r 다시 시도") {
				t.Fatalf("failed load must show the retry notice:\n%s", s)
			}
			h.Check()
			h.Press("r")
			if n := len(reqsTo(h, "GET", "/v1/timeline/"+kind)); n != 2 {
				t.Fatalf("timeline requests after r = %d, want 2", n)
			}
			if s := h.Screen(); !strings.Contains(s, "@hana") || strings.Contains(s, "불러오지 못했어요") {
				t.Fatalf("retry must show the timeline:\n%s", s)
			}
		})
	}
}

// Being resized (a mode change) retries a failed first page.
func TestTimelineFailureRetriesOnResize(t *testing.T) {
	h, _ := failingTimeline(t, "following", 1)
	h.Resize(80, 24)
	if n := len(reqsTo(h, "GET", "/v1/timeline/following")); n != 2 {
		t.Fatalf("requests after a resize = %d, want 2", n)
	}
	if s := h.Screen(); !strings.Contains(s, "@hana") {
		t.Fatalf("resize retry must show the timeline:\n%s", s)
	}
}

// Coming back to a feed whose first page failed retries it; so does a resync.
func TestTimelineFailureRetriesOnReshowAndResync(t *testing.T) {
	h, _ := failingTimeline(t, "following", 2)
	path := "/v1/timeline/following"
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChDev}})
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteFeed, Arg: "following"}})
	if n := len(reqsTo(h, "GET", path)); n != 2 {
		t.Fatalf("requests after coming back = %d, want 2 (retry on reshow)", n)
	}
	if s := h.Screen(); !strings.Contains(s, "피드를 불러오지 못했어요") {
		t.Fatalf("second failure must keep the notice:\n%s", s)
	}
	h.Send(ui.ResyncDoneMsg{})
	if n := len(reqsTo(h, "GET", path)); n != 3 {
		t.Fatalf("requests after a resync = %d, want 3", n)
	}
	if s := h.Screen(); !strings.Contains(s, "@hana") {
		t.Fatalf("resync retry must show the timeline:\n%s", s)
	}
	// loaded: further reshows and resyncs do not refetch the first page
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChDev}})
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteFeed, Arg: "following"}})
	h.Send(ui.ResyncDoneMsg{})
	if n := len(reqsTo(h, "GET", path)); n != 3 {
		t.Fatalf("requests once loaded = %d, want 3", n)
	}
}

// A successful empty first page still shows the empty state.
func TestTimelineEmptyStateOnlyAfterSuccess(t *testing.T) {
	srv := snsAPI(t, false, withFollowing([]api.TimelineItem{}))
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: "following"}))
	h.Resize(80, 24)
	if s := h.Screen(); !strings.Contains(s, textEmptyFollow) {
		t.Fatalf("empty success must show the empty state:\n%s", s)
	}
}

// A profile whose GET failed once retries with r, and on being shown again.
func TestProfileFailureRetries(t *testing.T) {
	srv := snsAPI(t, false)
	var left atomic.Int32
	left.Store(2)
	srv.Handle("GET /v1/users/{user}", func(w http.ResponseWriter, r *http.Request) {
		if left.Add(-1) >= 0 {
			write503(w)
			return
		}
		writeJSON(w, map[string]any{"user": profileOf("minji", relFollowing())})
	})
	route := ui.Route{Kind: ui.RouteProfile, ID: "@minji"}
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed), uitest.WithRoute(route))
	path := "/v1/users/@minji"
	if s := h.Screen(); !strings.Contains(s, textProfileLoadFailed) || !strings.Contains(s, "r 다시 시도") {
		t.Fatalf("failed profile must offer r:\n%s", s)
	}
	h.Check()
	// leave and come back: the cached instance retries
	h.Press("esc")
	h.Send(ui.NavigateMsg{Route: route})
	if n := len(reqsTo(h, "GET", path)); n != 2 {
		t.Fatalf("profile requests after reopening = %d, want 2", n)
	}
	h.Press("r")
	if n := len(reqsTo(h, "GET", path)); n != 3 {
		t.Fatalf("profile requests after r = %d, want 3", n)
	}
	if s := h.Screen(); strings.Contains(s, textProfileLoadFailed) || !strings.Contains(s, "@minji") {
		t.Fatalf("retry must show the profile:\n%s", s)
	}
	// loaded: reopening does not refetch the profile
	h.Press("esc")
	h.Send(ui.NavigateMsg{Route: route})
	if n := len(reqsTo(h, "GET", path)); n != 3 {
		t.Fatalf("profile requests once loaded = %d, want 3", n)
	}
}

// A resumed ready (short outage, logged events replayed) refetches what the
// transient SNS events would have brought: the following timeline since the
// overlap anchor, the open post (and its replies) and the story ring.
func TestResumedReadyBackfillsFeed(t *testing.T) {
	srv := snsAPI(t, false)
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: "following"}))
	h.Event("ready", realtime.Ready{})
	post := postBy("hana")
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RoutePost, ID: post.ID}, Target: ui.TargetMain})
	tl := len(reqsTo(h, "GET", "/v1/timeline/following"))
	pg := len(reqsTo(h, "GET", "/v1/posts/"+post.ID))
	st := len(reqsTo(h, "GET", "/v1/stories/feed"))

	h.Event("ready", realtime.Ready{Resumed: true})
	r, ok := lastReq(h, "GET", "/v1/timeline/following")
	if n := len(reqsTo(h, "GET", "/v1/timeline/following")); n != tl+1 || !ok || !strings.Contains(r.Query, "since=") {
		t.Fatalf("following after a resumed ready: %d new requests, last %+v; want one since= request", n-tl, r)
	}
	if n := len(reqsTo(h, "GET", "/v1/posts/"+post.ID)); n != pg+1 {
		t.Fatalf("open post refetches = %d, want 1", n-pg)
	}
	if n := len(reqsTo(h, "GET", "/v1/stories/feed")); n != st+1 {
		t.Fatalf("story ring refetches = %d, want 1", n-st)
	}
	if n := len(reqsTo(h, "GET", "/v1/me")); n != 1 {
		t.Fatalf("a resumed ready must not bootstrap again: GET /v1/me = %d, want 1", n)
	}
}
