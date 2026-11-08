package feed

import (
	"fmt"
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// unlikedTimeline is fixtures.Timeline() with hana's post not liked by me
// (the fixture has viewer.liked=true, so "l" would unlike it).
func unlikedTimeline() []api.TimelineItem {
	items := fixtures.Timeline()
	items[0].Post.Viewer.Liked = false
	return items
}

// toastText is the toast box text, rows joined, borders and spaces dropped
// (a long toast wraps inside its box).
func toastText(h *uitest.Harness) string {
	s := h.Screen()
	var b *layout.Rect
	if tb := h.Model.Frame().Toast; tb != nil {
		b = &tb.Rect
	}
	if b == nil {
		return squash(s) // tiny: shown in the status line
	}
	return squash(strings.Join(region(s, b.Inner()), ""))
}

// squash drops spaces and box-drawing characters.
func squash(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || (r >= 0x2500 && r <= 0x257F) {
			return -1
		}
		return r
	}, s)
}

// overlayText is the ansi-stripped text of the top overlay box.
func overlayText(h *uitest.Harness) string {
	f := h.Model.Frame()
	if len(f.Overlays) == 0 {
		return ""
	}
	return strings.Join(region(h.Screen(), f.Overlays[len(f.Overlays)-1].Rect), "\n")
}

// menuPick opens the post menu of the selected card and enters item label.
func menuPick(t *testing.T, h *uitest.Harness, label string) {
	t.Helper()
	h.Press("m")
	for range 10 {
		for _, l := range strings.Split(overlayText(h), "\n") {
			if strings.Contains(l, "› "+label) {
				h.Press("enter")
				return
			}
		}
		h.Press("j")
	}
	t.Fatalf("menu item %q not reachable:\n%s", label, overlayText(h))
}

func TestLikeOptimistic(t *testing.T) {
	// unit: the post flips before the request runs
	p := postBy("hana")
	p.Viewer.Liked = false
	cmd := toggleLike(&ui.Context{Now: time.Now}, &p)
	if cmd == nil || !p.Viewer.Liked || p.LikeCount != 13 {
		t.Fatalf("toggleLike: cmd=%v liked=%v count=%d, want optimistic like 13", cmd != nil, p.Viewer.Liked, p.LikeCount)
	}

	srv := snsAPI(t, false, withFollowing(unlikedTimeline()))
	id := postBy("hana").ID
	srv.JSON("PUT /v1/posts/{id}/like", http.StatusOK, api.LikeResult{PostID: id, Liked: true, LikeCount: 20})
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: "following"}))
	h.Press("l")
	if _, ok := lastReq(h, "PUT", "/v1/posts/"+id+"/like"); !ok {
		t.Fatal("l sent no PUT like")
	}
	if s := h.Screen(); !strings.Contains(s, "♥ 20") {
		t.Fatalf("server like_count not shown:\n%s", s)
	}
}

func TestUnlikeSendsDelete(t *testing.T) {
	srv := snsAPI(t, false)
	id := postBy("hana").ID
	srv.JSON("DELETE /v1/posts/{id}/like", http.StatusOK, api.LikeResult{PostID: id, Liked: false, LikeCount: 11})
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: "following"}))
	h.Press("l")
	if _, ok := lastReq(h, "DELETE", "/v1/posts/"+id+"/like"); !ok {
		t.Fatal("l on a liked post sent no DELETE like")
	}
	if s := h.Screen(); !strings.Contains(s, "♥ 11") {
		t.Fatalf("unlike count not shown:\n%s", s)
	}
}

func TestLikeFailureReverts(t *testing.T) {
	srv := snsAPI(t, false, withFollowing(unlikedTimeline()))
	srv.Error("PUT /v1/posts/{id}/like", http.StatusTooManyRequests, "rate_limited")
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: "following"}))
	h.Press("l")
	s := h.Screen()
	if !strings.Contains(s, "♥ 12") || strings.Contains(s, "♥ 13") {
		t.Fatalf("like not reverted:\n%s", s)
	}
	want := []rune(api.Describe(&api.Error{Status: http.StatusTooManyRequests, Code: "rate_limited"}))
	if len(want) > 20 {
		want = want[:20]
	}
	if !strings.Contains(toastText(h), squash(string(want))) {
		t.Fatalf("no error toast %q:\n%s", string(want), s)
	}
}

func TestRepostOptimistic(t *testing.T) {
	srv := snsAPI(t, false)
	id := postBy("hana").ID
	srv.JSON("PUT /v1/posts/{id}/repost", http.StatusOK, api.RepostResult{PostID: id, Reposted: true, RepostCount: 9})
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: "following"}))
	h.Press("b")
	if _, ok := lastReq(h, "PUT", "/v1/posts/"+id+"/repost"); !ok {
		t.Fatal("b sent no PUT repost")
	}
	if s := h.Screen(); !strings.Contains(s, "↻ 9") {
		t.Fatalf("server repost_count not shown:\n%s", s)
	}
}

func TestRepostQuotePrivateBlocked(t *testing.T) {
	// Review Focus 1: no request, no composer — a toast right away.
	srv := snsAPI(t, false, withFollowing([]api.TimelineItem{itemOf(privatePost())}))
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: "following"}))
	n := len(h.API.Requests())
	for _, k := range []string{"b", "q"} {
		h.Press(k)
		if !strings.Contains(toastText(h), squash("비공개 계정의 글은 리포스트·인용할 수 없어요")) {
			t.Fatalf("%s: no private toast:\n%s", k, h.Screen())
		}
		if top := feedTop(h); top.Kind != ui.RouteFeed {
			t.Fatalf("%s navigated to %+v", k, top)
		}
	}
	for _, r := range h.API.Requests()[n:] {
		if r.Method != "GET" {
			t.Fatalf("private post: sent %s %s", r.Method, r.Path)
		}
	}
}

func TestDeletedPostActionsToast(t *testing.T) {
	srv := snsAPI(t, false, withFollowing([]api.TimelineItem{itemOf(deletedPost())}))
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: "following"}))
	n := len(h.API.Requests())
	for _, k := range []string{"l", "b", "m"} {
		h.Press(k)
		if len(h.Model.Frame().Overlays) != 0 {
			t.Fatalf("%s on a deleted post opened an overlay", k)
		}
	}
	if !strings.Contains(toastText(h), squash("삭제된 글이에요")) {
		t.Fatalf("no deleted toast:\n%s", h.Screen())
	}
	for _, r := range h.API.Requests()[n:] {
		if r.Method != "GET" {
			t.Fatalf("deleted post: sent %s %s", r.Method, r.Path)
		}
	}
}

func TestQuoteAndReplyNavigate(t *testing.T) {
	h := uitest.New(t, feedOpts(t, "following", false)...)
	id := postBy("hana").ID
	h.Press("q")
	want := ui.Route{Kind: ui.RouteCompose, Arg: "post", ID: id, Query: map[string]string{"mode": "quote"}}
	if top := feedTop(h); top.Kind != want.Kind || top.Arg != want.Arg || top.ID != want.ID || !maps.Equal(top.Query, want.Query) {
		t.Fatalf("q: feed top = %+v, want %+v", top, want)
	}
	inst := h.Instance("compose/" + id + "/post")
	if inst == nil {
		t.Fatal("no compose instance after q")
	}
	// esc leaves the input, esc on an empty draft goes back (one esc is enough
	// while the composer is not registered yet).
	for range 2 {
		if feedTop(h).Kind != ui.RouteFeed {
			h.Press("esc")
		}
	}
	if top := feedTop(h); top.Kind != ui.RouteFeed {
		t.Fatalf("esc esc: feed top = %+v", top)
	}
	h.Press("r")
	top := feedTop(h)
	if top.Kind != ui.RouteCompose || top.ID != id || top.Query["mode"] != "reply" {
		t.Fatalf("r: feed top = %+v", top)
	}
	if got := h.Instance("compose/" + id + "/post"); got != inst {
		t.Fatal("q and r must share one composer instance")
	}
}

func TestAuthorAndHashtagNavigate(t *testing.T) {
	h := uitest.New(t, feedOpts(t, "following", false)...)
	h.Press("j")
	h.Press("#")
	if top := feedTop(h); top.Kind != ui.RouteHashtag || top.ID != "golang" {
		t.Fatalf("#: feed top = %+v", top)
	}
	h.Press("esc")
	h.Press("u")
	if top := feedTop(h); top.Kind != ui.RouteProfile || top.ID != userOf("dev_kim").ID {
		t.Fatalf("u: feed top = %+v", top)
	}
}

func TestHashtagPickForSeveralTags(t *testing.T) {
	p := mkPost(40, "minji", "#고양이 #강아지 산책", 1)
	h := uitest.New(t, feedOpts(t, "following", false, withFollowing([]api.TimelineItem{itemOf(p)}))...)
	h.Press("#")
	if n := len(h.Model.Frame().Overlays); n != 1 {
		t.Fatalf("# with two tags: %d overlays, want the picker", n)
	}
	if s := overlayText(h); !strings.Contains(s, "#고양이") || !strings.Contains(s, "#강아지") {
		t.Fatalf("picker lacks the tags:\n%s", s)
	}
	h.Press("j", "enter")
	if top := feedTop(h); top.Kind != ui.RouteHashtag || top.ID != "강아지" {
		t.Fatalf("pick: feed top = %+v", top)
	}
	if n := len(h.Model.Frame().Overlays); n != 0 {
		t.Fatalf("picker still open (%d overlays)", n)
	}
}

func TestHashtagClick(t *testing.T) {
	h := uitest.New(t, feedOpts(t, "following", false)...)
	x, y := cellOf(t, h.Screen(), "#golang")
	h.Click(x+2, y)
	if top := feedTop(h); top.Kind != ui.RouteHashtag || top.ID != "golang" {
		t.Fatalf("click #golang: feed top = %+v", top)
	}
}

func TestAuthorClick(t *testing.T) {
	h := uitest.New(t, feedOpts(t, "following", false)...)
	x, y := cellOf(t, h.Screen(), "@dev_kim")
	h.Click(x+1, y)
	if top := feedTop(h); top.Kind != ui.RouteProfile || top.ID != userOf("dev_kim").ID {
		t.Fatalf("click @dev_kim: feed top = %+v", top)
	}
}

func TestPostMenuItems(t *testing.T) {
	h := uitest.New(t, feedOpts(t, "following", false)...)
	h.Press("m")
	s := overlayText(h)
	want := []string{"좋아요 누른 사람", "리포스트한 사람", "인용한 글", "작성자 프로필", "글 신고", "작성자 차단"}
	last := -1
	for _, w := range want {
		i := strings.Index(s, w)
		if i < 0 || i < last {
			t.Fatalf("menu lacks %q in order:\n%s", w, s)
		}
		last = i
	}
	if strings.Contains(s, "글 삭제") {
		t.Fatalf("someone else's post offers 글 삭제:\n%s", s)
	}
}

func TestPostMenuReport(t *testing.T) {
	h := uitest.New(t, feedOpts(t, "following", false)...)
	menuPick(t, h, "글 신고")
	s := h.Screen()
	if !strings.Contains(s, "신고") || !strings.Contains(s, "스팸") {
		t.Fatalf("no report dialog:\n%s", s)
	}
	if n := len(h.Model.Frame().Overlays); n != 1 {
		t.Fatalf("overlays = %d, want 1 (the menu is replaced by the dialog)", n)
	}
	h.Press("esc")
	if n := len(h.Model.Frame().Overlays); n != 0 {
		t.Fatalf("esc left %d overlays", n)
	}
}

func TestPostMenuUserListAndQuotes(t *testing.T) {
	srv := snsAPI(t, false)
	id := postBy("hana").ID
	srv.JSON("GET /v1/posts/{id}/likes", http.StatusOK, pageOf([]api.UserSummary{userOf("minji"), userOf("junho")}))
	srv.JSON("GET /v1/posts/{id}/quotes", http.StatusOK, pageOf([]api.Post{quotePost()}))
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: "following"}))
	menuPick(t, h, "좋아요 누른 사람")
	if _, ok := lastReq(h, "GET", "/v1/posts/"+id+"/likes"); !ok {
		t.Fatal("no GET likes")
	}
	if s := overlayText(h); !strings.Contains(s, "좋아요 누른 사람") || !strings.Contains(s, "@minji") || !strings.Contains(s, "@junho") {
		t.Fatalf("likes list:\n%s", s)
	}
	if n := len(h.Model.Frame().Overlays); n != 1 {
		t.Fatalf("overlays = %d, want 1", n)
	}
	h.Press("j", "enter")
	if top := feedTop(h); top.Kind != ui.RouteProfile || top.ID != userOf("junho").ID {
		t.Fatalf("enter on junho: feed top = %+v", top)
	}
	if n := len(h.Model.Frame().Overlays); n != 0 {
		t.Fatalf("user list still open (%d)", n)
	}
	h.Press("esc")
	menuPick(t, h, "인용한 글")
	if s := overlayText(h); !strings.Contains(s, "이거 보세요") {
		t.Fatalf("quotes list:\n%s", s)
	}
	h.Press("enter")
	if top := feedTop(h); top.Kind != ui.RoutePost || top.ID != quotePost().ID {
		t.Fatalf("enter on a quote: feed top = %+v", top)
	}
}

func TestPostMenuBlockAuthor(t *testing.T) {
	// Review Focus 3: hidden right after the 204, with no block.updated event.
	srv := snsAPI(t, false)
	srv.Handle("PUT /v1/blocks/{user}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: "following"}))
	hana := userOf("hana").ID
	menuPick(t, h, "작성자 차단")
	if s := overlayText(h); !strings.Contains(s, "차단할까요?") {
		t.Fatalf("no block confirm:\n%s", s)
	}
	h.Press("y")
	if _, ok := lastReq(h, "PUT", "/v1/blocks/"+hana); !ok {
		t.Fatal("no PUT /v1/blocks/<hana>")
	}
	if !h.Context().Store.IsBlocked(hana) {
		t.Fatal("Store.IsBlocked(hana) is false right after the 204")
	}
	s := h.Screen()
	if strings.Contains(s, "@hana") || strings.Contains(s, "오늘 첫 눈") {
		t.Fatalf("hana still on screen after blocking:\n%s", s)
	}
	if !strings.Contains(toastText(h), "차단했어요") {
		t.Fatalf("no block toast:\n%s", s)
	}
}

func TestPostMenuBlockToastOnce(t *testing.T) {
	// Two ui/feed instances get the same broadcast result: one local apply, one toast.
	srv := snsAPI(t, false)
	srv.Handle("PUT /v1/blocks/{user}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: "discover"}))
	h.Press("2") // following instance too
	menuPick(t, h, "작성자 차단")
	h.Press("y")
	if n := strings.Count(toastText(h), "차단했어요"); n != 1 {
		t.Fatalf("block toasts = %d, want 1:\n%s", n, h.Screen())
	}
}

func TestPostMenuDeleteMine(t *testing.T) {
	mine := myPost()
	srv := snsAPI(t, false, withFollowing(append([]api.TimelineItem{itemOf(mine)}, fixtures.Timeline()...)))
	srv.Handle("DELETE /v1/posts/{id}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: "following"}))
	if id := SelectedID(h.Instance(keyFollowingRoute)); id != mine.ID {
		t.Fatalf("selected %q, want my post", id)
	}
	h.Press("m")
	s := overlayText(h)
	if !strings.Contains(s, "글 삭제") || strings.Contains(s, "글 신고") || strings.Contains(s, "작성자 차단") {
		t.Fatalf("my post menu:\n%s", s)
	}
	h.Press("esc")
	menuPick(t, h, "글 삭제")
	if s := overlayText(h); !strings.Contains(s, "이 글을 삭제할까요?") {
		t.Fatalf("no delete confirm:\n%s", s)
	}
	h.Press("y")
	if _, ok := lastReq(h, "DELETE", "/v1/posts/"+mine.ID); !ok {
		t.Fatal("no DELETE /v1/posts/<mine>")
	}
	if s := h.Screen(); strings.Contains(s, "점심 먹고 배포합니다") {
		t.Fatalf("deleted card still shown:\n%s", s)
	}
}

func TestUserListRemoveFollower(t *testing.T) {
	srv := snsAPI(t, false)
	srv.JSON("GET /v1/users/{user}/followers", http.StatusOK, pageOf([]api.UserSummary{userOf("junho"), userOf("hana")}))
	srv.Handle("DELETE /v1/users/{user}/follower", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: "following"}))
	h.Send(ui.OpenOverlayMsg{ID: "feed.user_list", Args: "followers @me"})
	if _, ok := lastReq(h, "GET", "/v1/users/@me/followers"); !ok {
		t.Fatal("no GET /v1/users/@me/followers")
	}
	if s := overlayText(h); !strings.Contains(s, "팔로워") || !strings.Contains(s, "@junho") {
		t.Fatalf("followers list:\n%s", s)
	}
	h.Press("x")
	if s := overlayText(h); !strings.Contains(s, "@junho님을 팔로워에서 뺄까요?") {
		t.Fatalf("no remove confirm:\n%s", s)
	}
	h.Press("y")
	if _, ok := lastReq(h, "DELETE", "/v1/users/"+userOf("junho").ID+"/follower"); !ok {
		t.Fatal("no DELETE /v1/users/<junho>/follower")
	}
	if n := len(h.Model.Frame().Overlays); n != 1 {
		t.Fatalf("overlays = %d, want the user list back", n)
	}
	if s := overlayText(h); strings.Contains(s, "@junho") || !strings.Contains(s, "@hana") {
		t.Fatalf("junho row not removed:\n%s", s)
	}
	if !strings.Contains(toastText(h), squash("팔로워에서 뺐어요")) {
		t.Fatalf("no toast:\n%s", h.Screen())
	}
}

func TestUserListRemoveFollowerOnlyMine(t *testing.T) {
	srv := snsAPI(t, false)
	srv.JSON("GET /v1/users/{user}/followers", http.StatusOK, pageOf([]api.UserSummary{userOf("junho")}))
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: "following"}))
	h.Send(ui.OpenOverlayMsg{ID: "feed.user_list", Args: "followers @hana"})
	h.Press("x")
	if n := len(h.Model.Frame().Overlays); n != 1 {
		t.Fatalf("x on someone else's followers: %d overlays", n)
	}
	for _, r := range h.API.Requests() {
		if r.Method == "DELETE" {
			t.Fatalf("sent %s %s", r.Method, r.Path)
		}
	}
}

func TestUserListHidesBlocked(t *testing.T) {
	srv := snsAPI(t, false)
	srv.JSON("GET /v1/posts/{id}/likes", http.StatusOK, pageOf([]api.UserSummary{userOf("hana"), userOf("minji")}))
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: "following"}))
	h.Send(ui.OpenOverlayMsg{ID: "feed.user_list", Args: "likes " + postBy("dev_kim").ID})
	if s := overlayText(h); !strings.Contains(s, "@hana") || !strings.Contains(s, "@minji") {
		t.Fatalf("likes list:\n%s", s)
	}
	h.Event("block.updated", map[string]any{"user_id": userOf("hana").ID, "blocked": true})
	if s := overlayText(h); strings.Contains(s, "@hana") || !strings.Contains(s, "@minji") {
		t.Fatalf("blocked hana still listed:\n%s", s)
	}
}

func TestPostOverlaysGolden(t *testing.T) {
	// One "WxH" subtest per size holding both goldens
	// (testdata/TestPostOverlaysGolden/<WxH>/{post_actions,likes}.golden).
	sizes := append(append([]uitest.Size{}, uitest.Required...), uitest.Size{W: 44, H: 16}, uitest.Size{W: 20, H: 6})
	srv := snsAPI(t, false)
	srv.JSON("GET /v1/posts/{id}/likes", http.StatusOK, pageOf([]api.UserSummary{userOf("minji"), userOf("junho"), userOf("dev_kim")}))
	opts := []uitest.Option{uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed), uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: "following"})}
	for _, s := range sizes {
		t.Run(fmt.Sprintf("%dx%d", s.W, s.H), func(t *testing.T) {
			h := uitest.New(t, opts...)
			h.Resize(s.W, s.H)
			h.Press("m")
			h.Golden("post_actions")

			h = uitest.New(t, opts...)
			h.Resize(s.W, s.H)
			h.Send(ui.OpenOverlayMsg{ID: "feed.user_list", Args: "likes " + postBy("hana").ID})
			h.Golden("likes")
		})
	}
}
