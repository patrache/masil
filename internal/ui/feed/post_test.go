package feed

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/store/feedstore"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// postOpts is feedOpts for RoutePost{ID: id}.
func postOpts(t *testing.T, id string, hostile bool, so ...snsOpt) []uitest.Option {
	t.Helper()
	return postOptsWith(snsAPI(t, hostile, so...), id, hostile)
}

// postOptsWith is postOpts on a server the test already set up.
func postOptsWith(srv *apitest.Server, id string, hostile bool) []uitest.Option {
	seed := fixtures.Seed
	if hostile {
		seed = func(s *store.Store) { fixtures.Seed(s); fixtures.Hostile(s) }
	}
	return []uitest.Option{uitest.WithAPI(srv), uitest.WithSeed(seed), uitest.WithRoute(ui.Route{Kind: ui.RoutePost, ID: id})}
}

// postRouteKey is the route key of RoutePost{ID: id}.
func postRouteKey(id string) string { return ui.Route{Kind: ui.RoutePost, ID: id}.Key() }

// postInst is the RoutePost instance of id.
func postInst(t *testing.T, h *uitest.Harness, id string) *postView {
	t.Helper()
	p, ok := h.Instance(postRouteKey(id)).(*postView)
	if !ok {
		t.Fatalf("no RoutePost instance for %s", id)
	}
	return p
}

// selectRow presses j/k until the row with kind and post id is selected.
func selectRow(t *testing.T, h *uitest.Harness, root string, kind rowKind, id string) {
	t.Helper()
	p := postInst(t, h, root)
	for _, k := range []string{"j", "k"} {
		for range 40 {
			if r, ok := p.list.Selected(); ok && r.Kind == kind && r.PostID == id {
				return
			}
			h.Press(k)
		}
	}
	t.Fatalf("row %v %s not reachable; rows %+v", kind, id, p.rows)
}

func TestPostGolden(t *testing.T) {
	sweep(t, uitest.AllSizes(), "hana", postOpts(t, postBy("hana").ID, false), nil)
}

func TestPostHostile(t *testing.T) {
	sweep(t, uitest.AllSizes(), "", postOpts(t, fixtures.HostileTimeline()[0].Post.ID, true), nil)
}

func TestPostLoadsContextAndReplies(t *testing.T) {
	hana := postBy("hana").ID
	h := uitest.New(t, postOpts(t, hana, false)...)
	if _, ok := lastReq(h, "GET", "/v1/posts/"+hana+"/context"); !ok {
		t.Fatal("no GET context")
	}
	if _, ok := lastReq(h, "GET", "/v1/posts/"+hana+"/replies"); !ok {
		t.Fatal("no GET replies")
	}
	s := h.Screen()
	for _, want := range []string{"오늘 첫 눈", "── 답글 4 ──", "├─", "└─", "우와 벌써?", "사진 예쁘다"} {
		if !strings.Contains(s, want) {
			t.Fatalf("screen lacks %q:\n%s", want, s)
		}
	}
	if !feedstore.Get(h.Context().Store).IsOpen("post:" + hana) {
		t.Fatal("post window not marked open for resync")
	}
	if got, _ := postInst(t, h, hana).Title(h.Context()); got != "글" {
		t.Fatalf("Title = %q", got)
	}
}

func TestPostAncestors(t *testing.T) {
	top, children := replyTree()
	child := children[top[1].ID][0] // hana "고마워요" under junho under hana's post
	h := uitest.New(t, postOpts(t, child.ID, false)...)
	p := postInst(t, h, child.ID)
	var kinds []rowKind
	var ids []string
	for _, r := range p.rows {
		kinds = append(kinds, r.Kind)
		ids = append(ids, r.PostID)
	}
	want := []string{postBy("hana").ID, top[1].ID, child.ID}
	if len(ids) < 3 || !slices.Equal(ids[:3], want) || kinds[0] != rowAncestor || kinds[1] != rowAncestor || kinds[2] != rowMain {
		t.Fatalf("rows = %v %v, want ancestors then main %v", kinds, ids, want)
	}
	s := h.Screen()
	if !strings.Contains(s, "│ ") || !strings.Contains(s, "사진 예쁘다") || !strings.Contains(s, "고마워요") {
		t.Fatalf("ancestors not drawn:\n%s", s)
	}
	if sel, _ := p.list.Selected(); sel.Kind != rowMain {
		t.Fatalf("selected row = %+v, want the main post", sel)
	}
	// enter on an ancestor opens it
	h.Press("k")
	h.Press("enter")
	if top := feedTop(h); top.Kind != ui.RoutePost || top.ID != want[1] {
		t.Fatalf("enter on ancestor: feed top = %+v", top)
	}
}

func TestReplyTreeExpand(t *testing.T) {
	hana := postBy("hana").ID
	top, _ := replyTree()
	junho := top[1].ID
	h := uitest.New(t, postOpts(t, hana, false)...)
	selectRow(t, h, hana, rowReply, junho)
	h.Press("space")
	if _, ok := lastReq(h, "GET", "/v1/posts/"+junho+"/replies"); !ok {
		t.Fatal("space sent no GET replies of the junho reply")
	}
	s := h.Screen()
	if !strings.Contains(s, "│  ├─") || !strings.Contains(s, "│  └─") {
		t.Fatalf("children not drawn under junho:\n%s", s)
	}
	if !strings.Contains(s, "고마워요") {
		t.Fatalf("child body missing:\n%s", s)
	}
	// space again collapses; the second expand reuses the window (no new request)
	n := len(reqsTo(h, "GET", "/v1/posts/"+junho+"/replies"))
	h.Press("space")
	if strings.Contains(h.Screen(), "고마워요") {
		t.Fatalf("space did not collapse:\n%s", h.Screen())
	}
	h.Press("right")
	if !strings.Contains(h.Screen(), "고마워요") {
		t.Fatalf("→ did not expand:\n%s", h.Screen())
	}
	if got := len(reqsTo(h, "GET", "/v1/posts/"+junho+"/replies")); got != n {
		t.Fatalf("re-expand fetched again (%d -> %d requests)", n, got)
	}
	h.Press("left")
	if strings.Contains(h.Screen(), "고마워요") {
		t.Fatalf("← did not collapse:\n%s", h.Screen())
	}
}

func TestReplyTreeMoreRow(t *testing.T) {
	hana := postBy("hana").ID
	top, _ := replyTree()
	extra := reply(36, "minji", "다음 쪽 답글", 0, postBy("hana"), hana)
	srv := snsAPI(t, false)
	srv.Handle("GET /v1/posts/{id}/replies", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.PathValue("id") != hana:
			writeJSON(w, pageOf([]api.Post{}))
		case r.URL.Query().Get("cursor") == "r2":
			writeJSON(w, pageOf([]api.Post{extra}))
		default:
			writeJSON(w, map[string]any{"items": top, "next_cursor": "r2"})
		}
	})
	h := uitest.New(t, postOptsWith(srv, hana, false)...)
	if !strings.Contains(h.Screen(), "답글 더 보기") {
		t.Fatalf("no more row:\n%s", h.Screen())
	}
	h.Press("G")
	if r, _ := postInst(t, h, hana).list.Selected(); r.Kind != rowMore || r.Cursor != "r2" {
		t.Fatalf("G selected %+v, want the more row with cursor r2", r)
	}
	h.Press("enter")
	req, ok := lastReq(h, "GET", "/v1/posts/"+hana+"/replies")
	if !ok || !strings.Contains(req.Query, "cursor=r2") {
		t.Fatalf("enter on more row: last replies request %+v", req)
	}
	s := h.Screen()
	if !strings.Contains(s, "다음 쪽 답글") || strings.Contains(s, "답글 더 보기") {
		t.Fatalf("next page not appended / more row still shown:\n%s", s)
	}
}

func TestReplyCreatedEventInserts(t *testing.T) {
	hana := postBy("hana").ID
	h := uitest.New(t, postOpts(t, hana, false)...)
	r := reply(36, "minji", "방금 단 답글", 0, postBy("hana"), hana)
	h.Event("post.reply_created", map[string]any{"parent_id": hana, "post": r})
	h.Press("G")
	if s := h.Screen(); !strings.Contains(s, "방금 단 답글") {
		t.Fatalf("live reply not inserted:\n%s", s)
	}
}

func TestOpenPostDeletedEvent(t *testing.T) {
	// Review Focus 5
	hana := postBy("hana").ID
	h := uitest.New(t, postOpts(t, hana, false)...)
	h.Event("post.deleted", map[string]any{"post_id": hana})
	h.Check()
	if s := h.Screen(); !strings.Contains(s, "삭제된 글이에요") || strings.Contains(s, "사무실 창밖") {
		t.Fatalf("deleted main post still drawn:\n%s", s)
	}
	selectRow(t, h, hana, rowMain, hana)
	h.Press("l")
	if _, ok := lastReq(h, "PUT", "/v1/posts/"+hana+"/like"); ok {
		t.Fatal("l on a deleted post sent PUT like")
	}
	if _, ok := lastReq(h, "DELETE", "/v1/posts/"+hana+"/like"); ok {
		t.Fatal("l on a deleted post sent DELETE like")
	}
	if !strings.Contains(toastText(h), squash("삭제된 글이에요")) {
		t.Fatalf("no deleted toast:\n%s", h.Screen())
	}
}

func TestDeepReplyOpensNewRoute(t *testing.T) {
	hana := postBy("hana").ID
	deep := deepChain()
	// hana's replies gain the head of the 7-deep chain
	edits := &postEdits{ExtraReplies: map[string][]api.Post{hana: {deep[0]}}}
	h := uitest.New(t, postOpts(t, hana, false, withPostEdits(edits))...)
	for d := 0; d < 5; d++ { // expand depth 1..5; depth 6 is the last drawn level
		selectRow(t, h, hana, rowReply, deep[d].ID)
		h.Press("space")
	}
	p := postInst(t, h, hana)
	for _, r := range p.rows {
		if r.PostID == deep[6].ID && r.Kind == rowReply {
			t.Fatalf("depth-7 reply drawn as a card: %+v", r)
		}
		if r.Depth > maxReplyDepth+1 {
			t.Fatalf("row deeper than the cap: %+v", r)
		}
	}
	h.Press("G")
	if !strings.Contains(h.Screen(), "↳ 이어서 보기") {
		t.Fatalf("no continue row:\n%s", h.Screen())
	}
	selectRow(t, h, hana, rowDeep, deep[5].ID)
	h.Press("enter")
	if top := feedTop(h); top.Kind != ui.RoutePost || top.ID != deep[5].ID {
		t.Fatalf("continue: feed top = %+v, want RoutePost %s", top, deep[5].ID)
	}
}

func TestResyncRefreshesPostWhenOnlyPostAlive(t *testing.T) {
	hana := postBy("hana").ID
	edits := &postEdits{}
	h := uitest.New(t, postOpts(t, hana, false, withPostEdits(edits))...)
	h.Resize(120, 30)
	if h.Instance(keyFollowingRoute) != nil {
		t.Fatal("precondition: no timeline instance may exist (RoutePost alone must forward the resync)")
	}
	_ = h.Screen()
	nr := mkPost(35, "junho", "새 답글", 0)
	nr.ReplyToID, nr.RootID = ptr(hana), ptr(hana)
	edits.mu.Lock()
	edits.Body[hana] = "오늘 첫 눈 (수정)"
	edits.ExtraReplies[hana] = []api.Post{nr}
	edits.mu.Unlock()
	ctx := h.Context()
	sendCmd(h, feedstore.Get(ctx.Store).Resync(ctx.Store, ctx.API))
	if s := h.Screen(); !strings.Contains(s, "오늘 첫 눈 (수정)") {
		t.Fatalf("resynced body not shown:\n%s", s)
	}
	h.Press("G")
	if s := h.Screen(); !strings.Contains(s, "새 답글") {
		t.Fatalf("resynced reply not shown:\n%s", s)
	}
}

func TestPostBlockHidesReplies(t *testing.T) {
	hana := postBy("hana").ID
	h := uitest.New(t, postOpts(t, hana, false)...)
	minji := userOf("minji").ID
	if h.Context().Store.Blocks == nil {
		h.Context().Store.Blocks = map[string]time.Time{}
	}
	h.Context().Store.Blocks[minji] = time.Now()
	h.Event("block.updated", map[string]any{"user_id": minji, "blocked": true})
	if s := h.Screen(); strings.Contains(s, "우와 벌써?") {
		t.Fatalf("blocked user's reply still shown:\n%s", s)
	}
	for _, r := range postInst(t, h, hana).rows {
		if r.Kind == rowReply {
			if p := feedstore.Get(h.Context().Store).Post(r.PostID); p != nil && p.Author.ID == minji {
				t.Fatalf("blocked author row kept: %+v", r)
			}
		}
	}
}

func TestReplyEnterAndClick(t *testing.T) {
	hana := postBy("hana").ID
	top, _ := replyTree()
	h := uitest.New(t, postOpts(t, hana, false)...)
	// click on a reply body selects it (no navigation)
	x, y := cellOf(t, h.Screen(), "우와 벌써?")
	h.Click(x+1, y)
	if r, _ := postInst(t, h, hana).list.Selected(); r.PostID != top[0].ID {
		t.Fatalf("click selected %+v, want %s", r, top[0].ID)
	}
	if top := feedTop(h); top.Kind != ui.RoutePost || top.ID != hana {
		t.Fatalf("body click navigated: %+v", top)
	}
	// click on the reply's author opens the profile
	x, y = cellOf(t, h.Screen(), "@junho")
	h.Click(x+1, y)
	if ft := feedTop(h); ft.Kind != ui.RouteProfile || ft.ID != userOf("junho").ID {
		t.Fatalf("author click: feed top = %+v", ft)
	}
	h.Press("esc")
	selectRow(t, h, hana, rowReply, top[2].ID)
	h.Press("enter")
	if ft := feedTop(h); ft.Kind != ui.RoutePost || ft.ID != top[2].ID {
		t.Fatalf("enter on reply: feed top = %+v", ft)
	}
}

func TestPostWatchTopics(t *testing.T) {
	hana := postBy("hana").ID
	top, _ := replyTree()
	h := uitest.New(t, postOpts(t, hana, false)...)
	_ = h.Screen()
	got := postInst(t, h, hana).WatchTopics()
	if !slices.Contains(got, "post:"+hana) || !slices.Contains(got, "post:"+top[0].ID) {
		t.Fatalf("WatchTopics = %v", got)
	}
}
