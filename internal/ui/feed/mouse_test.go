package feed

// CA 5.5 mouse: a click selects an item; a click on the already-selected item
// (outside every hit region) opens it, like enter.

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// clickText clicks one cell into the first on-screen occurrence of s.
func clickText(t *testing.T, h *uitest.Harness, s string) {
	t.Helper()
	x, y := cellOf(t, h.Screen(), s)
	h.Click(x+1, y)
}

// clickInOverlay clicks one cell into s inside the top overlay box.
func clickInOverlay(t *testing.T, h *uitest.Harness, s string) {
	t.Helper()
	f := h.Model.Frame()
	if len(f.Overlays) == 0 {
		t.Fatalf("clickInOverlay: no overlay open")
	}
	r := f.Overlays[len(f.Overlays)-1].Rect
	for i, line := range region(h.Screen(), r) {
		if j := strings.Index(line, s); j >= 0 {
			h.Click(r.X+ansi.StringWidth(line[:j])+1, r.Y+i)
			return
		}
	}
	t.Fatalf("clickInOverlay: %q not in the overlay:\n%s", s, overlayText(h))
}

// selectThenOpen clicks s twice: the first click must only select (the feed
// top stays at stay), the second must open want.
func selectThenOpen(t *testing.T, h *uitest.Harness, s string, stay ui.RouteKind, want ui.Route) {
	t.Helper()
	clickText(t, h, s)
	if top := feedTop(h); top.Kind != stay {
		t.Fatalf("first click on %q navigated: %+v", s, top)
	}
	clickText(t, h, s)
	if top := feedTop(h); top.Kind != want.Kind || top.ID != want.ID {
		t.Fatalf("second click on %q: feed top = %+v, want %v %s", s, top, want.Kind, want.ID)
	}
}

func TestTimelineClickSelectedOpens(t *testing.T) {
	h := uitest.New(t, feedOpts(t, "following", false)...)
	h.Resize(120, 40)
	inst := h.Instance(keyFollowingRoute)
	dev := postBy("dev_kim")
	if SelectedID(inst) == dev.ID {
		t.Fatal("precondition: dev_kim's card must not start selected")
	}
	selectThenOpen(t, h, "bubbletea v2", ui.RouteFeed, ui.Route{Kind: ui.RoutePost, ID: dev.ID})
}

func TestHashtagClickSelectedOpens(t *testing.T) {
	h := uitest.New(t, uitest.WithAPI(snsAPI(t, false)), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteHashtag, ID: "첫눈"}))
	h.Resize(120, 40)
	// minji's "#첫눈 왔다" is the second card; "왔다" is plain body text.
	selectThenOpen(t, h, "왔다", ui.RouteHashtag, ui.Route{Kind: ui.RoutePost, ID: hashtagPosts("첫눈")[1].Post.ID})
}

func TestProfileClickSelectedOpens(t *testing.T) {
	h := uitest.New(t, uitest.WithAPI(snsAPI(t, false)), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteProfile, ID: "@dev_kim"}))
	h.Resize(120, 40)
	// the second card is dev_kim's repost of hana's post
	selectThenOpen(t, h, "사무실 창밖", ui.RouteProfile, ui.Route{Kind: ui.RoutePost, ID: postBy("hana").ID})
}

func TestTrendsSearchResultClickSelectedOpens(t *testing.T) {
	hits := []api.PostSearchHit{{Post: postBy("hana"), Highlights: []api.SearchHighlight{}}, {Post: postBy("dev_kim"), Highlights: []api.SearchHighlight{}}}
	h := uitest.New(t, trendsOptsWith(searchSrv(t, nil, hits), false)...)
	h.Resize(120, 40)
	h.Press("/")
	h.Type("눈")
	h.Advance(300 * time.Millisecond)
	// The first result is the list's selection, but the input has the focus:
	// the first click only moves there.
	selectThenOpen(t, h, "사무실 창밖", ui.RouteTrends, ui.Route{Kind: ui.RoutePost, ID: postBy("hana").ID})
	h.Press("esc")
	selectThenOpen(t, h, "bubbletea v2", ui.RouteTrends, ui.Route{Kind: ui.RoutePost, ID: postBy("dev_kim").ID})
}

func TestReplyRowClickSelectedOpens(t *testing.T) {
	hana := postBy("hana").ID
	top, _ := replyTree()
	h := uitest.New(t, postOpts(t, hana, false)...)
	h.Resize(120, 40)
	selectThenOpen(t, h, "우와 벌써?", ui.RoutePost, ui.Route{Kind: ui.RoutePost, ID: top[0].ID})
}

func TestUserListAndQuotesClickSelectedOpens(t *testing.T) {
	srv := snsAPI(t, false)
	second := mkPost(13, "junho", "나도 인용", 4)
	second.Quote = &api.QuotedPost{Available: true, Post: ptr(postBy("hana"))}
	srv.JSON("GET /v1/posts/{id}/likes", http.StatusOK, pageOf([]api.UserSummary{userOf("minji"), userOf("junho")}))
	srv.JSON("GET /v1/posts/{id}/quotes", http.StatusOK, pageOf([]api.Post{quotePost(), second}))
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: "following"}))
	h.Resize(120, 40)

	menuPick(t, h, "좋아요 누른 사람")
	clickInOverlay(t, h, "@junho")
	if n := len(h.Model.Frame().Overlays); n != 1 || feedTop(h).Kind != ui.RouteFeed {
		t.Fatalf("first click on junho: overlays %d top %+v", n, feedTop(h))
	}
	clickInOverlay(t, h, "@junho")
	if top := feedTop(h); top.Kind != ui.RouteProfile || top.ID != userOf("junho").ID {
		t.Fatalf("second click on junho: feed top = %+v", top)
	}
	if n := len(h.Model.Frame().Overlays); n != 0 {
		t.Fatalf("user list still open (%d)", n)
	}

	h.Press("esc")
	menuPick(t, h, "인용한 글")
	clickInOverlay(t, h, "나도 인용")
	if n := len(h.Model.Frame().Overlays); n != 1 || feedTop(h).Kind != ui.RouteFeed {
		t.Fatalf("first click on a quote: overlays %d top %+v", n, feedTop(h))
	}
	clickInOverlay(t, h, "나도 인용")
	if top := feedTop(h); top.Kind != ui.RoutePost || top.ID != second.ID {
		t.Fatalf("second click on a quote: feed top = %+v, want RoutePost %s", top, second.ID)
	}
	if n := len(h.Model.Frame().Overlays); n != 0 {
		t.Fatalf("quotes still open (%d)", n)
	}
}

// A click outside every row (blank space under the list) never opens the
// selected item.
func TestClickBelowListDoesNotOpen(t *testing.T) {
	h := uitest.New(t, uitest.WithAPI(snsAPI(t, false)), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteHashtag, ID: "golang"}))
	h.Resize(120, 40)
	lines := strings.Split(h.Screen(), "\n")
	h.Click(10, len(lines)-3)
	h.Click(10, len(lines)-3)
	if top := feedTop(h); top.Kind != ui.RouteHashtag {
		t.Fatalf("click under the only card navigated: %+v", top)
	}
}
