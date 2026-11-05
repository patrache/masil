package feed

import (
	"fmt"
	"image"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/termcaps"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

const (
	keyFollowingRoute = "feed//following"
	keyDiscoverRoute  = "feed//discover"
)

func feedOpts(t *testing.T, arg string, hostile bool, so ...snsOpt) []uitest.Option {
	t.Helper()
	srv := snsAPI(t, hostile, so...)
	seed := fixtures.Seed
	if hostile {
		seed = func(s *store.Store) { fixtures.Seed(s); fixtures.Hostile(s) }
	}
	return []uitest.Option{uitest.WithAPI(srv), uitest.WithSeed(seed), uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: arg})}
}

// feedTop is the top of the feed surface stack.
func feedTop(h *uitest.Harness) ui.Route {
	f := h.Nav().Feed
	return f[len(f)-1]
}

func TestTimelineGolden(t *testing.T) {
	// One "WxH" subtest per size holding both goldens
	// (testdata/TestTimelineGolden/<WxH>/{following,discover}.golden).
	following, discover := feedOpts(t, "following", false), feedOpts(t, "discover", false)
	for _, s := range uitest.AllSizes() {
		t.Run(fmt.Sprintf("%dx%d", s.W, s.H), func(t *testing.T) {
			for _, c := range []struct {
				name string
				opts []uitest.Option
			}{{"following", following}, {"discover", discover}} {
				h := uitest.New(t, c.opts...)
				h.Resize(s.W, s.H)
				h.Golden(c.name)
			}
		})
	}
}

func TestTimelineHostile(t *testing.T) {
	sweep(t, uitest.AllSizes(), "", feedOpts(t, "following", true), nil)
}

func TestCommandModeShowsFeedColumnBesideChat(t *testing.T) {
	h := uitest.New(t, feedOpts(t, "following", false)...)
	h.Resize(200, 60)
	f := h.Model.Frame()
	if k := f.Routes[layout.SlotFeed].Kind; k != ui.RouteFeed {
		t.Fatalf("SlotFeed route = %q, want feed", k)
	}
	if k := f.Routes[layout.SlotMain].Kind; k == ui.RouteFeed {
		t.Fatalf("SlotMain shows the feed; want the chat side")
	}
	p, ok := f.Layout.Pane(layout.SlotFeed)
	if !ok {
		t.Fatal("no feed pane at 200x60")
	}
	joined := strings.Join(region(h.Screen(), p.Rect), "\n")
	for _, want := range []string{"피드", "팔로잉", "#트렌드", "@hana", "♥ 12  ↻ 3  💬 4"} {
		if !strings.Contains(joined, want) {
			t.Errorf("feed column lacks %q:\n%s", want, joined)
		}
	}
}

func TestTimelineInfiniteScroll(t *testing.T) {
	srv := snsAPI(t, false)
	srv.Handle("GET /v1/timeline/following", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "c2" {
			writeJSON(w, api.TimelinePage{Items: []api.TimelineItem{itemOf(myPost())}})
			return
		}
		writeJSON(w, api.TimelinePage{Items: fixtures.Timeline(), NextCursor: ptr("c2")})
	})
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: "following"}))
	h.Press("G")
	r, ok := lastReq(h, "GET", "/v1/timeline/following")
	if !ok || !strings.Contains(r.Query, "cursor=c2") {
		t.Fatalf("last following request = %+v (ok=%v), want cursor=c2", r, ok)
	}
	if !strings.Contains(h.Screen(), "점심 먹고 배포합니다") {
		t.Fatalf("second page not shown:\n%s", h.Screen())
	}
}

func TestDiscoverNewPostsBanner(t *testing.T) {
	h := uitest.New(t, feedOpts(t, "discover", false)...)
	h.Advance(30 * time.Second)
	r, ok := lastReq(h, "GET", "/v1/timeline/discover")
	if !ok || !strings.Contains(r.Query, "since=") {
		t.Fatalf("last discover request = %+v (ok=%v), want since=", r, ok)
	}
	if s := h.Screen(); !strings.Contains(s, "새 글 2개 · enter") {
		t.Fatalf("banner missing (overlap must not count):\n%s", s)
	}
	inst := h.Instance(keyDiscoverRoute)
	h.Press("g", "g")
	if id := SelectedID(inst); id != "" {
		t.Fatalf("gg selected %q, want the banner", id)
	}
	n := len(reqsTo(h, "GET", "/v1/timeline/discover"))
	h.Press("enter")
	reqs := reqsTo(h, "GET", "/v1/timeline/discover")
	if len(reqs) != n+1 || strings.Contains(reqs[len(reqs)-1].Query, "since") {
		t.Fatalf("enter on banner: discover requests %+v, want one more without since", reqs)
	}
	if s := h.Screen(); strings.Contains(s, "새 글 2개") {
		t.Fatalf("banner still shown after refresh:\n%s", s)
	}
	if id := SelectedID(inst); id != fixtures.Timeline()[0].ID {
		t.Fatalf("selection after refresh = %q, want first card %q", id, fixtures.Timeline()[0].ID)
	}
}

func TestDiscoverBannerKeys(t *testing.T) {
	srv := snsAPI(t, false)
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: "discover"}))
	h.Advance(30 * time.Second)
	if !strings.Contains(h.Screen(), "새 글 2개 · enter") {
		t.Fatalf("banner missing:\n%s", h.Screen())
	}
	inst := h.Instance(keyDiscoverRoute)
	first := fixtures.Timeline()[0].ID
	if id := SelectedID(inst); id != first {
		t.Fatalf("initial selection %q, want first card %q", id, first)
	}
	h.Press("k")
	if id := SelectedID(inst); id != "" {
		t.Fatalf("k from the first card selected %q, want the banner", id)
	}
	h.Press("l")
	for _, r := range h.API.Requests() {
		if r.Method == "PUT" {
			t.Fatalf("l on the banner sent %s %s", r.Method, r.Path)
		}
	}
	h.Press("j")
	if id := SelectedID(inst); id != first {
		t.Fatalf("j from the banner selected %q, want first card %q", id, first)
	}
}

func TestDiscoverBannerGap(t *testing.T) {
	h := uitest.New(t, feedOpts(t, "discover", false, withDiscoverSince(api.TimelinePage{Gap: true}))...)
	h.Advance(30 * time.Second)
	if s := h.Screen(); !strings.Contains(s, "새 글 100개+") {
		t.Fatalf("gap banner missing:\n%s", s)
	}
}

func TestSnsHarnessNoStrayRequests(t *testing.T) {
	h := uitest.New(t, feedOpts(t, "following", false)...)
	h.Resize(120, 40)
	h.Advance(6 * time.Second)
	if h.Model.Frame().Toast != nil {
		t.Fatalf("unexpected toast:\n%s", h.Screen())
	}
	known := []string{
		"/v1/timeline/following", "/v1/timeline/discover", "/v1/stories/feed", "/v1/trends", "/v1/notifications",
		"/v1/me/follow-requests", "/v1/me", "/v1/spaces", "/v1/dms", "/v1/me/unreads", "/v1/notifications/unread-count",
		"/v1/friends", "/v1/friends/requests", "/v1/me/direct-invites", "/v1/blocks",
	}
	prefixes := []string{"/v1/posts/", "/v1/users/", "/v1/hashtags/", "/v1/media/", "/v1/spaces/"}
	for _, r := range h.API.Requests() {
		if r.Method != "GET" {
			t.Errorf("unexpected %s %s", r.Method, r.Path)
			continue
		}
		ok := slices.Contains(known, r.Path)
		for _, p := range prefixes {
			ok = ok || strings.HasPrefix(r.Path, p)
		}
		if !ok {
			t.Errorf("request outside the snsAPI table: %s %s", r.Method, r.Path)
		}
	}
}

// rawSince returns the kitty transmissions recorded after index n.
func rawSince(h *uitest.Harness, n int) []string {
	var out []string
	for _, m := range h.TeaMsgs()[n:] {
		if r, ok := m.(tea.RawMsg); ok {
			if s := fmt.Sprint(r.Msg); strings.HasPrefix(s, "\x1b_G") && strings.Contains(s, "a=T") {
				out = append(out, s)
			}
		}
	}
	return out
}

// rawWithCols reports whether one of raws transmits a cols-wide placement.
func rawWithCols(raws []string, cols int) bool {
	for _, r := range raws {
		if strings.Contains(r, fmt.Sprintf("c=%d,", cols)) || strings.Contains(r, fmt.Sprintf("c=%d;", cols)) {
			return true
		}
	}
	return false
}

func TestTimelineKittyRawWithoutFollowUp(t *testing.T) {
	// Review Focus 6: the kitty Setup leaves with the Update that got the image.
	h := uitest.New(t, feedOpts(t, "following", false)...)
	h.Resize(120, 40)
	h.Context().Caps.Graphics = termcaps.GraphicsKitty
	m := postBy("hana").Media[0]
	n := len(h.TeaMsgs())
	h.Send(ui.ResultMsg[image.Image]{Tag: "feed.media:" + m.ID, Value: fixtures.TestImage()})
	if raws := rawSince(h, n); !rawWithCols(raws, 48) {
		t.Fatalf("no 48-col kitty transmit (tea.Raw) right after the image result: %d raws", len(raws))
	}
	if !strings.ContainsRune(h.Screen(), kitty.Placeholder) {
		t.Fatalf("screen has no kitty placeholder:\n%s", h.Screen())
	}
	// A mode change re-renders loaded images for the new box with no further msg:
	// widget draws a 30-col box (new transmit); command's feed column is 48 cols
	// again — already transmitted at 120x40, so it shows placeholders at once.
	n = len(h.TeaMsgs())
	h.Resize(80, 24)
	if raws := rawSince(h, n); !rawWithCols(raws, 30) {
		t.Fatalf("80x24: no 30-col kitty transmit after the mode change: %d raws", len(raws))
	}
	h.Resize(200, 60)
	if !strings.ContainsRune(h.Screen(), kitty.Placeholder) {
		t.Fatalf("200x60: no kitty placeholder:\n%s", h.Screen())
	}

	// From widget straight to command: the 48-col box is new and is sent by the
	// mode change itself.
	h2 := uitest.New(t, feedOpts(t, "following", false)...)
	h2.Resize(80, 24)
	h2.Context().Caps.Graphics = termcaps.GraphicsKitty
	h2.Send(ui.ResultMsg[image.Image]{Tag: "feed.media:" + m.ID, Value: fixtures.TestImage()})
	n = len(h2.TeaMsgs())
	h2.Resize(200, 60)
	if raws := rawSince(h2, n); !rawWithCols(raws, 48) {
		t.Fatalf("80x24 -> 200x60: no 48-col kitty transmit after the mode change: %d raws", len(raws))
	}
}

func TestCardClickWhenScrolled(t *testing.T) {
	h := uitest.New(t, feedOpts(t, "following", false, withFollowing(longTimeline()))...)
	h.Resize(80, 24)
	inst := h.Instance(keyFollowingRoute)
	h.Press("G")
	dev := postBy("dev_kim").ID
	if SelectedID(inst) != dev {
		t.Fatalf("G selected %q, want dev_kim %q", SelectedID(inst), dev)
	}
	if AnchorID(inst) == dev {
		t.Fatal("precondition: dev_kim card must not be the first visible card")
	}
	x, y := cellOf(t, h.Screen(), "#golang")
	h.Click(x+1, y)
	if top := feedTop(h); top.Kind != ui.RouteHashtag || top.ID != "golang" {
		t.Fatalf("click on #golang: feed top = %+v", top)
	}
	h.Press("esc")
	if top := feedTop(h); top.Kind != ui.RouteFeed {
		t.Fatalf("esc: feed top = %+v", top)
	}
	// The stats line of the first visible card: selection moves, no navigation.
	screen := h.Screen()
	lines := strings.Split(screen, "\n")
	sy, want := -1, ""
	for i, l := range lines {
		if strings.Contains(l, "채우기 글") {
			for n := 1; n <= 5; n++ {
				if strings.Contains(l, fmt.Sprintf("채우기 글 %d", n)) {
					want = sid(20 + n - 1)
				}
			}
		}
		if strings.Contains(l, "♥") && want != "" {
			sy = i
			break
		}
	}
	if sy < 0 {
		t.Fatalf("no filler stats line on screen:\n%s", screen)
	}
	sx := strings.Index(lines[sy], "♥")
	h.Click(ansi.StringWidth(lines[sy][:sx]), sy)
	if got := SelectedID(inst); got != want {
		t.Fatalf("stats click selected %q, want %q", got, want)
	}
	if top := feedTop(h); top.Kind != ui.RouteFeed {
		t.Fatalf("stats click navigated: %+v", top)
	}
}

func TestDiscoverPollingStopsWhenHidden(t *testing.T) {
	h := uitest.New(t, feedOpts(t, "discover", false)...)
	h.Resize(120, 40)
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChGeneral}})
	if k := h.Model.Frame().Routes[layout.SlotMain].Kind; k != ui.RouteChannel {
		t.Fatalf("main shows %q, want the channel", k)
	}
	since := func() int {
		n := 0
		for _, r := range reqsTo(h, "GET", "/v1/timeline/discover") {
			if strings.Contains(r.Query, "since=") {
				n++
			}
		}
		return n
	}
	h.Advance(5 * time.Minute)
	if n := since(); n > 1 {
		t.Fatalf("hidden feed kept polling: %d since-requests in 5 minutes", n)
	}
}

func TestTimelineWatchTopics(t *testing.T) {
	h := uitest.New(t, feedOpts(t, "following", false)...)
	h.Screen()
	w, ok := h.Instance(keyFollowingRoute).(ui.Watcher)
	if !ok {
		t.Fatal("timeline is not a ui.Watcher")
	}
	if topics := w.WatchTopics(); !slices.Contains(topics, "post:"+postBy("hana").ID) {
		t.Fatalf("WatchTopics = %v, want post:%s", topics, postBy("hana").ID)
	}
}

func TestTimelineLiveInsert(t *testing.T) {
	h := uitest.New(t, feedOpts(t, "following", false)...)
	h.Screen()
	p := mkPost(90, "minji", "실시간으로 온 글", 0)
	p.ID = discoverNewID(9)
	h.Event("timeline.item_created", map[string]any{"item": itemOf(p)})
	lines := strings.Split(h.Screen(), "\n")
	iNew, iHana := -1, -1
	for i, l := range lines {
		if iNew < 0 && strings.Contains(l, "실시간으로 온 글") {
			iNew = i
		}
		if iHana < 0 && strings.Contains(l, "오늘 첫 눈") {
			iHana = i
		}
	}
	if iNew < 0 || iHana < 0 || iNew > iHana {
		t.Fatalf("new item not the first card (new %d, hana %d):\n%s", iNew, iHana, strings.Join(lines, "\n"))
	}
}

func TestTimelineTabsKeys(t *testing.T) {
	h := uitest.New(t, feedOpts(t, "discover", false)...)
	h.Press("2")
	if f := h.Nav().Feed[0]; f.Kind != ui.RouteFeed || f.Arg != "following" {
		t.Fatalf("2: Feed[0] = %+v, want following", f)
	}
	h.Press("3")
	if top := feedTop(h); top.Kind != ui.RouteTrends {
		t.Fatalf("3: feed top = %+v, want trends", top)
	}
}

func TestTimelineSelectionAcrossModes(t *testing.T) {
	h := uitest.New(t, feedOpts(t, "following", false, withFollowing(longTimeline()))...)
	h.Resize(120, 40)
	inst := h.Instance(keyFollowingRoute)
	h.Press("j", "j")
	id := SelectedID(inst)
	if id == "" || id == longTimeline()[0].ID {
		t.Fatalf("j j selected %q", id)
	}
	for _, s := range []uitest.Size{{W: 200, H: 60}, {W: 120, H: 30}, {W: 80, H: 24}, {W: 50, H: 20}, {W: 120, H: 40}} {
		h.Resize(s.W, s.H)
		h.Check()
		if got := h.Instance(keyFollowingRoute); got != inst {
			t.Fatalf("%dx%d: instance replaced", s.W, s.H)
		}
		if got := SelectedID(inst); got != id {
			t.Fatalf("%dx%d: selection %q, want %q", s.W, s.H, got, id)
		}
	}
}

func TestStoryRingOpensStories(t *testing.T) {
	h := uitest.New(t, feedOpts(t, "following", false)...)
	h.Press("s")
	if top := feedTop(h); top.Kind != ui.RouteStories || top.ID != userOf("hana").ID {
		t.Fatalf("s: feed top = %+v, want stories of hana", top)
	}
}
