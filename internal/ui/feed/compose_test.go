package feed

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/slash"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/store/feedstore"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// composeHandlers answers the composer's writes on srv: POST /v1/uploads (201,
// upload ids "01JDUPL…" counting up), DELETE /v1/uploads/{id} (204), POST
// /v1/posts (201, the post built from the request, authored by sam) and POST
// /v1/stories (201). Tests replace a route with srv.Handle / srv.Error.
func composeHandlers(srv *apitest.Server) {
	var uploads, posts atomic.Int64
	srv.Handle("POST /v1/uploads", func(w http.ResponseWriter, r *http.Request) {
		n := uploads.Add(1)
		id := fmt.Sprintf("01JDUPL%019d", n)
		up := api.Upload{
			Media:   api.Media{ID: id, URL: "/v1/media/" + id, ContentType: r.Header.Get("Content-Type"), Size: int(r.ContentLength), Width: ptr(64), Height: ptr(48)},
			Purpose: r.URL.Query().Get("purpose"), Status: "pending", CreatedAt: fixtureNow,
		}
		writeStatus(w, http.StatusCreated, map[string]any{"upload": up})
	})
	srv.JSON("DELETE /v1/uploads/{id}", http.StatusNoContent, nil)
	srv.Handle("POST /v1/posts", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		n := posts.Add(1)
		body, _ := req["body"].(string)
		p := mkPost(900+int(n), "sam", body, 0)
		if s, ok := req["reply_to_id"].(string); ok && s != "" {
			p.ReplyToID = ptr(s)
			p.RootID = ptr(s)
		}
		writeStatus(w, http.StatusCreated, map[string]any{"post": p})
	})
	srv.Handle("POST /v1/stories", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		body, _ := req["body"].(string)
		bg, _ := req["bg"].(string)
		st := api.Story{ID: sid(990), Author: userOf("sam"), Body: body, BG: bg, CreatedAt: fixtureNow, ExpiresAt: storyExpiry}
		writeStatus(w, http.StatusCreated, map[string]any{"story": st})
	})
}

func writeStatus(w http.ResponseWriter, status int, v any) {
	b, _ := json.Marshal(v)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(b)
}

// composeOpts opens r on the sns server plus composeHandlers.
func composeOpts(t *testing.T, r ui.Route) []uitest.Option {
	t.Helper()
	srv := snsAPI(t, false)
	composeHandlers(srv)
	return []uitest.Option{uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed), uitest.WithRoute(r)}
}

// writePNG writes fixtures.TestImage() as <TempDir>/photo.png.
func writePNG(t testing.TB) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "photo.png")
	if err := os.WriteFile(p, testImagePNG(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// composeAt is the RoutCompose instance with route key k.
func composeAt(t *testing.T, h *uitest.Harness, k string) *compose {
	t.Helper()
	c, ok := h.Instance(k).(*compose)
	if !ok {
		t.Fatalf("no compose instance %q (got %T)", k, h.Instance(k))
	}
	return c
}

// attach attaches path through ctrl+o (from the input) and the feed.attach overlay.
func attach(h *uitest.Harness, path string) {
	h.Press("ctrl+o")
	h.Type(path)
	h.Press("enter")
}

// postBody decodes the JSON body of a recorded request.
func postBody(t *testing.T, r apitest.Recorded) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.Body, &m); err != nil {
		t.Fatalf("request body %q: %v", r.Body, err)
	}
	return m
}

const (
	keyComposePost  = "compose//post"
	keyComposeStory = "compose//story"
)

func TestComposeGolden(t *testing.T) {
	// testdata/TestComposeGolden/<case>/<WxH>/<case>.golden
	t.Run("post", func(t *testing.T) {
		sweep(t, uitest.AllSizes(), "post", composeOpts(t, ui.Route{Kind: ui.RouteCompose, Arg: "post"}), func(h *uitest.Harness) {
			h.Type("오늘 첫 눈 ❄️ 사무실 창밖 #첫눈")
			h.Press("ctrl+o")
			h.Type(writePNG(h.T))
			h.Press("enter")
		})
	})
	t.Run("story", func(t *testing.T) {
		sweep(t, uitest.Required, "story", composeOpts(t, ui.Route{Kind: ui.RouteCompose, Arg: "story"}), func(h *uitest.Harness) { h.Type("점심 국밥") })
	})
	t.Run("quote", func(t *testing.T) {
		sweep(t, uitest.Required, "quote", composeOpts(t, composeRoute(postBy("hana").ID, "quote")), nil)
	})
	t.Run("reply_poll", func(t *testing.T) {
		sweep(t, uitest.Required, "reply_poll", composeOpts(t, composeRoute(postBy("hana").ID, "reply")), func(h *uitest.Harness) {
			h.Type(`/poll "점심" "국밥" "돈까스"`)
		})
	})
}

func TestComposeHostile(t *testing.T) {
	srv := snsAPI(t, true)
	composeHandlers(srv)
	id := fixtures.HostileTimeline()[0].Post.ID
	for _, mode := range []string{"reply", "quote"} {
		opts := []uitest.Option{uitest.WithAPI(srv), uitest.WithSeed(func(s *store.Store) { fixtures.Seed(s); fixtures.Hostile(s) }), uitest.WithRoute(composeRoute(id, mode))}
		t.Run(mode, func(t *testing.T) {
			sweep(t, uitest.AllSizes(), "", opts, func(h *uitest.Harness) { h.Type("안녕 ‮​👨‍👩‍👧") })
		})
	}
}

func TestComposeCursorAtInputEnd(t *testing.T) {
	h := uitest.New(t, composeOpts(t, ui.Route{Kind: ui.RouteCompose, Arg: "post"})...)
	h.Type("안녕")
	cur := h.Cursor()
	if cur == nil {
		t.Fatal("no cursor while typing")
	}
	x, y := cellOf(t, h.Screen(), "안녕")
	if cur.Position.Y != y || cur.Position.X != x+4 {
		t.Fatalf("cursor = %v, want (%d,%d) after %q", cur.Position, x+4, y, "안녕")
	}
}

func TestComposeCounterBlocksOver500(t *testing.T) {
	h := uitest.New(t, composeOpts(t, ui.Route{Kind: ui.RouteCompose, Arg: "post"})...)
	h.Type(strings.Repeat("가", 501))
	if !strings.Contains(h.Screen(), "501/500") {
		t.Fatalf("counter not shown:\n%s", h.Screen())
	}
	h.Press("enter")
	if rs := reqsTo(h, "POST", "/v1/posts"); len(rs) != 0 {
		t.Fatalf("POST sent for 501 chars: %d", len(rs))
	}
	if !strings.Contains(toastText(h), "500자를넘었어요") {
		t.Fatalf("toast = %q", toastText(h))
	}
}

func TestComposeEmptyRejected(t *testing.T) {
	h := uitest.New(t, composeOpts(t, ui.Route{Kind: ui.RouteCompose, Arg: "post"})...)
	h.Type("   ")
	h.Press("enter")
	if rs := reqsTo(h, "POST", "/v1/posts"); len(rs) != 0 {
		t.Fatalf("POST sent for an empty post")
	}
	if !strings.Contains(toastText(h), "내용을입력해주세요") {
		t.Fatalf("toast = %q", toastText(h))
	}
}

func TestComposeSendsPost(t *testing.T) {
	h := uitest.New(t, composeOpts(t, ui.Route{Kind: ui.RouteCompose, Arg: "post"})...)
	if feedTop(h).Kind != ui.RouteCompose {
		t.Fatalf("feed top = %+v", feedTop(h))
	}
	h.Type("안녕")
	h.Press("enter")
	r, ok := lastReq(h, "POST", "/v1/posts")
	if !ok {
		t.Fatal("no POST /v1/posts")
	}
	b := postBody(t, r)
	if n, _ := b["client_nonce"].(string); n == "" || b["body"] != "안녕" {
		t.Fatalf("body = %v", b)
	}
	if _, has := b["reply_to_id"]; has {
		t.Fatalf("new post carries reply_to_id: %v", b)
	}
	if top := feedTop(h); top.Kind != ui.RouteFeed {
		t.Fatalf("after send feed top = %+v", top)
	}
	if !strings.Contains(toastText(h), "올렸어요") {
		t.Fatalf("toast = %q", toastText(h))
	}
	if c := composeAt(t, h, keyComposePost); c.input.Value() != "" || len(c.atts) != 0 || c.nonce != "" {
		t.Fatalf("composer not reset: %q %v %q", c.input.Value(), c.atts, c.nonce)
	}
	fs := feedstore.Get(h.Context().Store)
	if p := fs.Post(sid(901)); p == nil || p.Body != "안녕" {
		t.Fatalf("sent post not cached: %+v", p)
	}
}

func TestComposeRetryKeepsNonce(t *testing.T) {
	h := uitest.New(t, composeOpts(t, ui.Route{Kind: ui.RouteCompose, Arg: "post"})...)
	var calls atomic.Int64
	h.API.Handle("POST /v1/posts", func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			writeStatus(w, http.StatusServiceUnavailable, map[string]any{"error": map[string]any{"code": "service_unavailable", "message": "x", "details": map[string]any{}}})
			return
		}
		writeStatus(w, http.StatusCreated, map[string]any{"post": mkPost(950, "sam", "안녕", 0)})
	})
	h.Type("안녕")
	h.Press("enter")
	if top := feedTop(h); top.Kind != ui.RouteCompose {
		t.Fatalf("failed send left the composer: %+v", top)
	}
	if c := composeAt(t, h, keyComposePost); c.input.Value() != "안녕" {
		t.Fatalf("draft lost after failure: %q", c.input.Value())
	}
	h.Press("enter")
	rs := reqsTo(h, "POST", "/v1/posts")
	if len(rs) != 2 {
		t.Fatalf("POSTs = %d, want 2", len(rs))
	}
	n1, n2 := postBody(t, rs[0])["client_nonce"], postBody(t, rs[1])["client_nonce"]
	if n1 == "" || n1 != n2 {
		t.Fatalf("nonces %v / %v, want identical", n1, n2)
	}
	if top := feedTop(h); top.Kind != ui.RouteFeed {
		t.Fatalf("after the retry feed top = %+v", top)
	}
}

func TestComposeAttachLimit(t *testing.T) {
	h := uitest.New(t, composeOpts(t, ui.Route{Kind: ui.RouteCompose, Arg: "post"})...)
	path := writePNG(t)
	for range 4 {
		attach(h, path)
	}
	ups := reqsTo(h, "POST", "/v1/uploads")
	if len(ups) != 4 {
		t.Fatalf("uploads = %d, want 4", len(ups))
	}
	for _, u := range ups {
		if u.Query != "purpose=post" {
			t.Fatalf("upload query = %q", u.Query)
		}
	}
	h.Press("ctrl+o")
	if !strings.Contains(toastText(h), "사진은4장까지예요") {
		t.Fatalf("5th attach toast = %q", toastText(h))
	}
	if len(h.Model.Frame().Overlays) != 0 {
		t.Fatal("attach overlay opened past the limit")
	}
	if n := len(reqsTo(h, "POST", "/v1/uploads")); n != 4 {
		t.Fatalf("uploads = %d after the 5th try", n)
	}
	if !strings.Contains(h.Screen(), "photo.png") || !strings.Contains(h.Screen(), "✓") {
		t.Fatalf("chips missing:\n%s", h.Screen())
	}
	h.Type("사진들")
	h.Press("enter")
	r, ok := lastReq(h, "POST", "/v1/posts")
	if !ok {
		t.Fatal("no POST /v1/posts")
	}
	ids, _ := postBody(t, r)["media_upload_ids"].([]any)
	if len(ids) != 4 {
		t.Fatalf("media_upload_ids = %v", ids)
	}
}

func TestComposeRemoveLastAttachment(t *testing.T) {
	h := uitest.New(t, composeOpts(t, ui.Route{Kind: ui.RouteCompose, Arg: "post"})...)
	path := writePNG(t)
	attach(h, path)
	attach(h, path)
	h.Press("esc", "x")
	c := composeAt(t, h, keyComposePost)
	if len(c.atts) != 1 {
		t.Fatalf("attachments after x = %d", len(c.atts))
	}
	if _, ok := lastReq(h, "DELETE", "/v1/uploads/01JDUPL0000000000000000002"); !ok {
		t.Fatalf("no DELETE of the last upload: %+v", h.API.Requests())
	}
}

func TestComposePoll(t *testing.T) {
	h := uitest.New(t, composeOpts(t, ui.Route{Kind: ui.RouteCompose, Arg: "post"})...)
	h.Type(`/poll "점심" "국밥" "돈까스"`)
	s := h.Screen()
	if !strings.Contains(s, "국밥") || !strings.Contains(s, "돈까스") || !strings.Contains(s, "0명 참여") {
		t.Fatalf("poll preview missing:\n%s", s)
	}
	h.Press("enter")
	r, ok := lastReq(h, "POST", "/v1/posts")
	if !ok {
		t.Fatal("no POST /v1/posts")
	}
	b := postBody(t, r)
	poll, _ := b["poll"].(map[string]any)
	opts, _ := poll["options"].([]any)
	if poll["question"] != "점심" || len(opts) != 2 || opts[0] != "국밥" || opts[1] != "돈까스" {
		t.Fatalf("poll = %v", poll)
	}
	if v, has := b["body"]; has && v != "" {
		t.Fatalf("body = %v, want empty", v)
	}
}

func TestComposePollUsageWarning(t *testing.T) {
	h := uitest.New(t, composeOpts(t, ui.Route{Kind: ui.RouteCompose, Arg: "post"})...)
	h.Type(`/poll "점심"`)
	before := h.Screen()
	h.Press("enter")
	if rs := reqsTo(h, "POST", "/v1/posts"); len(rs) != 0 {
		t.Fatal("POST sent for a bad /poll")
	}
	_, _, err := slash.Parse(`/poll "점심"`)
	if err == nil {
		t.Fatal("slash.Parse accepted a one-option poll")
	}
	if !strings.Contains(squash(before), squash(err.Error())) {
		t.Fatalf("usage %q not shown:\n%s", err, before)
	}
	if !strings.Contains(toastText(h), squash(err.Error())) {
		t.Fatalf("toast = %q", toastText(h))
	}
}

func TestComposeReplyPollRejected(t *testing.T) {
	h := uitest.New(t, composeOpts(t, composeRoute(postBy("hana").ID, "reply"))...)
	h.Type(`/poll "점심" "국밥" "돈까스"`)
	if !strings.Contains(h.Screen(), "답글에는 투표를 붙일 수 없어요") {
		t.Fatalf("no reply-poll warning:\n%s", h.Screen())
	}
	h.Press("enter")
	if rs := reqsTo(h, "POST", "/v1/posts"); len(rs) != 0 {
		t.Fatal("POST sent for a reply poll")
	}
	if !strings.Contains(toastText(h), "답글에는투표를붙일수없어요") {
		t.Fatalf("toast = %q", toastText(h))
	}
}

func TestComposeQuoteAndReplyFields(t *testing.T) {
	id := postBy("hana").ID
	h := uitest.New(t, composeOpts(t, composeRoute(id, "quote"))...)
	if s := h.Screen(); !strings.Contains(s, "인용") || !strings.Contains(s, "오늘 첫 눈") {
		t.Fatalf("quote header/card missing:\n%s", s)
	}
	h.Type("이거 봐")
	h.Press("enter")
	r, ok := lastReq(h, "POST", "/v1/posts")
	if !ok {
		t.Fatal("no POST")
	}
	if b := postBody(t, r); b["quote_of_id"] != id || b["reply_to_id"] != nil {
		t.Fatalf("quote body = %v", b)
	}

	h = uitest.New(t, composeOpts(t, composeRoute(id, "reply"))...)
	if s := h.Screen(); !strings.Contains(s, "답글 · @hana") {
		t.Fatalf("reply header missing:\n%s", s)
	}
	h.Type("나도!")
	h.Press("enter")
	r, ok = lastReq(h, "POST", "/v1/posts")
	if !ok {
		t.Fatal("no POST")
	}
	if b := postBody(t, r); b["reply_to_id"] != id || b["quote_of_id"] != nil {
		t.Fatalf("reply body = %v", b)
	}
	fs := feedstore.Get(h.Context().Store)
	if !fs.HasReplies(id) {
		t.Fatal("no reply window for the parent")
	}
	if p, ok := fs.Replies(id).Get(sid(901)); !ok || p.Body != "나도!" {
		t.Fatalf("new reply not in Replies(%s): %+v", id, fs.Replies(id).Items())
	}
}

func TestComposeStory(t *testing.T) {
	h := uitest.New(t, composeOpts(t, ui.Route{Kind: ui.RouteCompose, Arg: "story"})...)
	h.Type("점심")
	h.Press("esc", "b", "i", "enter")
	r, ok := lastReq(h, "POST", "/v1/stories")
	if !ok {
		t.Fatal("no POST /v1/stories")
	}
	b := postBody(t, r)
	if b["body"] != "점심" || b["bg"] != "accent2" {
		t.Fatalf("story body = %v", b)
	}
	if _, has := b["media_upload_id"]; has {
		t.Fatalf("story without media sent media_upload_id: %v", b)
	}
	if !strings.Contains(toastText(h), "스토리를올렸어요") {
		t.Fatalf("toast = %q", toastText(h))
	}
	if _, ok := lastReq(h, "GET", "/v1/stories/feed"); !ok {
		t.Fatal("story feed not refreshed")
	}
	if len(feedstore.Get(h.Context().Store).Stories()) == 0 {
		t.Fatal("SetStories not applied")
	}
	if top := feedTop(h); top.Kind != ui.RouteFeed {
		t.Fatalf("after story feed top = %+v", top)
	}
}

func TestComposeStoryLimitAndPhotoSwap(t *testing.T) {
	h := uitest.New(t, composeOpts(t, ui.Route{Kind: ui.RouteCompose, Arg: "story"})...)
	h.Type(strings.Repeat("가", 201))
	if !strings.Contains(h.Screen(), "201/200") {
		t.Fatalf("story counter:\n%s", h.Screen())
	}
	h.Press("enter")
	if rs := reqsTo(h, "POST", "/v1/stories"); len(rs) != 0 {
		t.Fatal("over-long story sent")
	}
	h.Press("ctrl+c") // clear
	path := writePNG(t)
	attach(h, path)
	attach(h, path)
	c := composeAt(t, h, keyComposeStory)
	if len(c.atts) != 1 || c.atts[0].Upload == nil || c.atts[0].Upload.ID != "01JDUPL0000000000000000002" {
		t.Fatalf("story attachments = %+v", c.atts)
	}
	ups := reqsTo(h, "POST", "/v1/uploads")
	if len(ups) != 2 || ups[1].Query != "purpose=story" {
		t.Fatalf("story uploads = %+v", ups)
	}
	if _, ok := lastReq(h, "DELETE", "/v1/uploads/01JDUPL0000000000000000001"); !ok {
		t.Fatal("swapped-out story photo not deleted")
	}
	h.Press("enter")
	r, ok := lastReq(h, "POST", "/v1/stories")
	if !ok {
		t.Fatal("photo story not sent")
	}
	if b := postBody(t, r); b["media_upload_id"] != "01JDUPL0000000000000000002" {
		t.Fatalf("story = %v", b)
	}
}

func TestComposeDraftSurvivesModesAndNavigation(t *testing.T) { // Review Focus 4
	r := ui.Route{Kind: ui.RouteCompose, Arg: "post"}
	h := uitest.New(t, composeOpts(t, r)...)
	h.Type("초안")
	attach(h, writePNG(t))
	inst := composeAt(t, h, keyComposePost)
	check := func(label string) {
		t.Helper()
		c := composeAt(t, h, keyComposePost)
		if c != inst {
			t.Fatalf("%s: a new compose instance", label)
		}
		if c.input.Value() != "초안" || len(c.atts) != 1 || c.atts[0].Upload == nil {
			t.Fatalf("%s: draft %q atts %+v", label, c.input.Value(), c.atts)
		}
	}
	for _, s := range uitest.AllSizes() {
		h.Resize(s.W, s.H)
		h.Check()
		check(fmt.Sprintf("%dx%d", s.W, s.H))
	}
	h.Resize(120, 40)
	if s := h.Screen(); !strings.Contains(s, "초안") || !strings.Contains(s, "photo.png") {
		t.Fatalf("draft not drawn after the size sweep:\n%s", s)
	}
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteFeed, Arg: "following"}})
	if top := feedTop(h); top.Kind != ui.RouteFeed {
		t.Fatalf("feed top = %+v", top)
	}
	h.Send(ui.NavigateMsg{Route: r})
	check("navigation")
	if s := h.Screen(); !strings.Contains(s, "초안") || !strings.Contains(s, "photo.png") {
		t.Fatalf("draft not drawn after navigation:\n%s", s)
	}
}

func TestComposePersistentState(t *testing.T) {
	h := uitest.New(t, composeOpts(t, ui.Route{Kind: ui.RouteCompose, Arg: "story"})...)
	h.Type("메모")
	attach(h, writePNG(t))
	h.Press("esc", "b")
	c := composeAt(t, h, keyComposeStory)
	c.nonce = "N"
	st := c.SaveState()
	fresh := newCompose(h.Context(), ui.Route{Kind: ui.RouteCompose, Arg: "story"}).(*compose)
	fresh.LoadState(st)
	if fresh.input.Value() != "메모" || fresh.bg != "accent2" || len(fresh.atts) != 1 || fresh.nonce != "N" {
		t.Fatalf("restored %q %q %+v %q", fresh.input.Value(), fresh.bg, fresh.atts, fresh.nonce)
	}
}

func TestComposeEscConfirmsDiscard(t *testing.T) {
	h := uitest.New(t, composeOpts(t, ui.Route{Kind: ui.RouteCompose, Arg: "post"})...)
	h.Type("버릴까")
	attach(h, writePNG(t))
	h.Press("esc", "esc")
	if s := overlayText(h); !strings.Contains(s, "작성 중인 글을 버릴까요?") {
		t.Fatalf("no confirm:\n%s", s)
	}
	h.Press("n")
	c := composeAt(t, h, keyComposePost)
	if len(h.Model.Frame().Overlays) != 0 || c.input.Value() != "버릴까" || feedTop(h).Kind != ui.RouteCompose {
		t.Fatalf("n: overlays %d draft %q top %+v", len(h.Model.Frame().Overlays), c.input.Value(), feedTop(h))
	}
	h.Press("esc", "esc", "y")
	if top := feedTop(h); top.Kind != ui.RouteFeed {
		t.Fatalf("y: feed top = %+v", top)
	}
	if c.input.Value() != "" || len(c.atts) != 0 {
		t.Fatalf("draft kept after discard: %q %+v", c.input.Value(), c.atts)
	}
	if _, ok := lastReq(h, "DELETE", "/v1/uploads/01JDUPL0000000000000000001"); !ok {
		t.Fatal("discarded upload not deleted")
	}
}

func TestComposeEscEmptyGoesBack(t *testing.T) {
	h := uitest.New(t, composeOpts(t, ui.Route{Kind: ui.RouteCompose, Arg: "post"})...)
	h.Press("esc", "esc")
	if len(h.Model.Frame().Overlays) != 0 {
		t.Fatal("confirm for an empty draft")
	}
	if top := feedTop(h); top.Kind != ui.RouteFeed {
		t.Fatalf("feed top = %+v", top)
	}
}

func TestComposeRouteUpdatedSwitchesMode(t *testing.T) {
	id := postBy("hana").ID
	quote := composeRoute(id, "quote")
	h := uitest.New(t, composeOpts(t, quote)...)
	var fail atomic.Bool
	fail.Store(true)
	h.API.Handle("POST /v1/posts", func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			writeStatus(w, http.StatusServiceUnavailable, map[string]any{"error": map[string]any{"code": "service_unavailable", "message": "x", "details": map[string]any{}}})
			return
		}
		writeStatus(w, http.StatusCreated, map[string]any{"post": mkPost(951, "sam", "안녕", 0)})
	})
	key := "compose/" + id + "/post"
	h.Type("안녕")
	h.Press("enter")
	rs := reqsTo(h, "POST", "/v1/posts")
	if len(rs) != 1 {
		t.Fatalf("POSTs = %d", len(rs))
	}
	n1 := postBody(t, rs[0])["client_nonce"]
	inst := composeAt(t, h, key)

	h.Send(ui.NavigateMsg{Route: composeRoute(id, "reply")})
	if composeAt(t, h, key) != inst {
		t.Fatal("reply route built a new instance")
	}
	s := h.Screen()
	if !strings.Contains(s, "답글 · @hana") || strings.Contains(s, "인용") {
		t.Fatalf("header not switched to reply:\n%s", s)
	}
	if inst.input.Value() != "안녕" {
		t.Fatalf("draft lost on mode switch: %q", inst.input.Value())
	}
	if inst.nonce != "" {
		t.Fatalf("nonce kept across modes: %q", inst.nonce)
	}
	// same mode again: nothing changes (the nonce made by a failed reply stays)
	h.Press("enter")
	rs = reqsTo(h, "POST", "/v1/posts")
	if len(rs) != 2 {
		t.Fatalf("POSTs = %d", len(rs))
	}
	n2 := postBody(t, rs[1])["client_nonce"]
	if n2 == "" || n2 == n1 {
		t.Fatalf("reply reused the quote nonce: %v", n2)
	}
	h.Send(ui.NavigateMsg{Route: composeRoute(id, "reply")})
	if inst.nonce != n2 {
		t.Fatalf("same-mode navigation changed the nonce: %q", inst.nonce)
	}
	fail.Store(false)
	h.Press("enter")
	rs = reqsTo(h, "POST", "/v1/posts")
	b := postBody(t, rs[len(rs)-1])
	if b["reply_to_id"] != id || b["quote_of_id"] != nil || b["client_nonce"] != n2 {
		t.Fatalf("reply body = %v (n1 %v n2 %v)", b, n1, n2)
	}
}

func TestComposeFetchesUncachedTarget(t *testing.T) {
	id := postBy("dev_kim").ID
	h := uitest.New(t, composeOpts(t, composeRoute(id, "reply"))...)
	if _, ok := lastReq(h, "GET", "/v1/posts/"+id); !ok {
		t.Fatal("target post not fetched")
	}
	if s := h.Screen(); !strings.Contains(s, "답글 · @dev_kim") {
		t.Fatalf("header:\n%s", s)
	}
}

func TestAttachOverlayGolden(t *testing.T) {
	sizes := append(append([]uitest.Size{}, uitest.Required...), uitest.Size{W: 44, H: 16}, uitest.Size{W: 20, H: 6})
	sweep(t, sizes, "attach", composeOpts(t, ui.Route{Kind: ui.RouteCompose, Arg: "post"}), func(h *uitest.Harness) {
		h.Press("ctrl+o")
	})
}

func TestAttachOverlayArgs(t *testing.T) {
	ctx := &ui.Context{}
	f := ui.Registered().Overlays["feed.attach"]
	if f == nil {
		t.Fatal("feed.attach not registered")
	}
	for args, want := range map[string]string{"post": keyComposePost, "story": keyComposeStory, "post compose/X/post": "compose/X/post"} {
		o, ok := f(ctx, args).(*attachOverlay)
		if !ok || o.route != want {
			t.Fatalf("args %q: %+v", args, o)
		}
	}
	if o := f(ctx, "avatar"); o != nil {
		t.Fatalf("bad purpose built %T", o)
	}
}

func TestAttachErrors(t *testing.T) {
	h := uitest.New(t, composeOpts(t, ui.Route{Kind: ui.RouteCompose, Arg: "post"})...)
	attach(h, filepath.Join(t.TempDir(), "nope.png"))
	if !strings.Contains(toastText(h), "파일을찾을수없어요") {
		t.Fatalf("missing file toast = %q", toastText(h))
	}
	if n := len(reqsTo(h, "POST", "/v1/uploads")); n != 0 {
		t.Fatalf("uploads = %d for a missing file", n)
	}
	h.Press("esc") // close the overlay that stayed open

	h.API.Error("POST /v1/uploads", http.StatusUnsupportedMediaType, "unsupported_media_type")
	attach(h, writePNG(t))
	if !strings.Contains(toastText(h), squash(api.Describe(&api.Error{Code: "unsupported_media_type"}))) {
		t.Fatalf("415 toast = %q", toastText(h))
	}
	if s := h.Screen(); !strings.Contains(s, "✕ 업로드 실패") {
		t.Fatalf("no failed chip:\n%s", s)
	}
	// a failed chip is not sent
	h.Type("글")
	h.Press("enter")
	r, ok := lastReq(h, "POST", "/v1/posts")
	if !ok {
		t.Fatal("no POST")
	}
	if ids, has := postBody(t, r)["media_upload_ids"]; has {
		t.Fatalf("failed upload sent: %v", ids)
	}
}

func TestAttachExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, "pic.png"), testImagePNG(), 0o600); err != nil {
		t.Fatal(err)
	}
	h := uitest.New(t, composeOpts(t, ui.Route{Kind: ui.RouteCompose, Arg: "post"})...)
	attach(h, "~/pic.png")
	if n := len(reqsTo(h, "POST", "/v1/uploads")); n != 1 {
		t.Fatalf("uploads = %d", n)
	}
	if c := composeAt(t, h, keyComposePost); len(c.atts) != 1 || c.atts[0].Name != "pic.png" {
		t.Fatalf("atts = %+v", c.atts)
	}
}
