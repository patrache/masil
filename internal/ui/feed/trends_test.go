package feed

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

const keyTrendsRoute = "trends//"

func trendsOptsWith(srv *apitest.Server, hostile bool) []uitest.Option {
	seed := fixtures.Seed
	if hostile {
		seed = func(s *store.Store) { fixtures.Seed(s); fixtures.Hostile(s) }
	}
	return []uitest.Option{uitest.WithAPI(srv), uitest.WithSeed(seed), uitest.WithRoute(ui.Route{Kind: ui.RouteTrends})}
}

// searchSrv is snsAPI plus canned hashtag and post search answers.
func searchSrv(t *testing.T, tags []api.HashtagSuggestion, hits []api.PostSearchHit) *apitest.Server {
	t.Helper()
	srv := snsAPI(t, false)
	if tags == nil {
		tags = []api.HashtagSuggestion{}
	}
	if hits == nil {
		hits = []api.PostSearchHit{}
	}
	srv.JSON("GET /v1/hashtags/search", http.StatusOK, map[string]any{"items": tags})
	srv.JSON("GET /v1/search/posts", http.StatusOK, map[string]any{"items": hits, "next_cursor": nil})
	return srv
}

func snowTags() []api.HashtagSuggestion {
	return []api.HashtagSuggestion{{Tag: "첫눈", PostCount24h: 42}, {Tag: "첫눈사진", PostCount24h: 3}}
}

func devKimHits() []api.PostSearchHit {
	return []api.PostSearchHit{{Post: postBy("dev_kim"), Highlights: []api.SearchHighlight{}}}
}

// query parses a recorded request's query string.
func query(t *testing.T, r apitest.Recorded) url.Values {
	t.Helper()
	v, err := url.ParseQuery(r.Query)
	if err != nil {
		t.Fatalf("bad query %q: %v", r.Query, err)
	}
	return v
}

func TestTrendsGolden(t *testing.T) {
	sweep(t, uitest.AllSizes(), "trends", trendsOptsWith(snsAPI(t, false), false), nil)
}

func TestTrendsHostile(t *testing.T) {
	srv := snsAPI(t, true)
	tr := trendsTop10()
	tr.Items[0].Tag = "태그\x1b[31m빨강\u202e" + strings.Repeat("긴태그", 20)
	tr.Items[1].Tag = "👨‍👩‍👧" + strings.Repeat("e\u0301", 30)
	tr.Items[2].PostCount, tr.Items[2].AuthorCount = 987654321, 123456789
	srv.JSON("GET /v1/trends", http.StatusOK, tr)
	srv.JSON("GET /v1/hashtags/search", http.StatusOK, map[string]any{"items": []api.HashtagSuggestion{
		{Tag: tr.Items[0].Tag, PostCount24h: 99999999}, {Tag: tr.Items[1].Tag, PostCount24h: 1},
	}})
	opts := trendsOptsWith(srv, true)
	sweep(t, uitest.AllSizes(), "", opts, nil)
	sweep(t, uitest.AllSizes(), "", opts, func(h *uitest.Harness) {
		h.Press("/")
		h.Type("#태그\x1b[31m")
		h.Advance(300 * time.Millisecond)
	})
}

func TestTrendsRefreshEvery5m(t *testing.T) {
	h := uitest.New(t, trendsOptsWith(snsAPI(t, false), false)...)
	if n := len(reqsTo(h, "GET", "/v1/trends")); n != 1 {
		t.Fatalf("initial trends requests = %d, want 1", n)
	}
	h.Advance(5*time.Minute - time.Second)
	if n := len(reqsTo(h, "GET", "/v1/trends")); n != 1 {
		t.Fatalf("trends requests before 5m = %d, want 1", n)
	}
	h.Advance(time.Second)
	if n := len(reqsTo(h, "GET", "/v1/trends")); n != 2 {
		t.Fatalf("trends requests after 5m = %d, want 2", n)
	}
	h.Check()
}

func TestTrendsEnterOpensHashtag(t *testing.T) {
	h := uitest.New(t, trendsOptsWith(snsAPI(t, false), false)...)
	s := h.Screen()
	for _, want := range []string{"#첫눈", "42개 글 · 30명", "#주말", "11:00 기준"} {
		if !strings.Contains(s, want) {
			t.Fatalf("trends lack %q:\n%s", want, s)
		}
	}
	h.Press("j", "enter")
	if top := feedTop(h); top.Kind != ui.RouteHashtag || top.ID != "golang" {
		t.Fatalf("enter opened %+v, want RouteHashtag golang", top)
	}
}

func TestTrendsClickOpensHashtag(t *testing.T) {
	h := uitest.New(t, trendsOptsWith(snsAPI(t, false), false)...)
	x, y := cellOf(t, h.Screen(), "#점심")
	h.Click(x, y)
	if top := feedTop(h); top.Kind != ui.RouteHashtag || top.ID != "점심" {
		t.Fatalf("click opened %+v, want RouteHashtag 점심", top)
	}
}

func TestTrendsEmpty(t *testing.T) {
	srv := snsAPI(t, false)
	srv.JSON("GET /v1/trends", http.StatusOK, api.Trends{Items: []api.Trend{}})
	h := uitest.New(t, trendsOptsWith(srv, false)...)
	if s := h.Screen(); !strings.Contains(s, "아직 트렌드가 없어요") {
		t.Fatalf("empty trends state missing:\n%s", s)
	}
	h.Check()
}

func TestTrendsHashtagSearch(t *testing.T) {
	h := uitest.New(t, trendsOptsWith(searchSrv(t, snowTags(), nil), false)...)
	h.Resize(120, 40)
	h.Press("/")
	h.Type("#첫")
	h.Advance(299 * time.Millisecond)
	if n := len(reqsTo(h, "GET", "/v1/hashtags/search")); n != 0 {
		t.Fatalf("hashtag search before the debounce: %d requests", n)
	}
	h.Advance(time.Millisecond)
	reqs := reqsTo(h, "GET", "/v1/hashtags/search")
	if len(reqs) != 1 {
		t.Fatalf("hashtag search requests = %d, want 1", len(reqs))
	}
	if q := query(t, reqs[0]); q.Get("q") != "첫" || q.Get("limit") != "20" {
		t.Fatalf("hashtag search query = %v, want q=첫 limit=20", q)
	}
	s := h.Screen()
	for _, want := range []string{"#첫눈", "글 42개", "#첫눈사진", "#첫 태그 피드 열기"} {
		if !strings.Contains(s, want) {
			t.Fatalf("hashtag results lack %q:\n%s", want, s)
		}
	}
	if n := len(reqsTo(h, "GET", "/v1/search/posts")); n != 0 {
		t.Fatalf("#query sent %d post searches", n)
	}
	h.Press("down", "enter")
	if top := feedTop(h); top.Kind != ui.RouteHashtag || top.ID != "첫눈" {
		t.Fatalf("enter opened %+v, want RouteHashtag 첫눈", top)
	}
}

func TestTrendsHashtagSearchOpenRow(t *testing.T) {
	h := uitest.New(t, trendsOptsWith(searchSrv(t, nil, nil), false)...)
	h.Press("/")
	h.Type("#새태그")
	h.Advance(300 * time.Millisecond)
	if s := h.Screen(); !strings.Contains(s, "#새태그 태그 피드 열기") {
		t.Fatalf("open row missing for an empty result:\n%s", s)
	}
	h.Press("down", "enter")
	if top := feedTop(h); top.Kind != ui.RouteHashtag || top.ID != "새태그" {
		t.Fatalf("open row opened %+v, want RouteHashtag 새태그", top)
	}
}

func TestTrendsHashtagSearchExactNoOpenRow(t *testing.T) {
	h := uitest.New(t, trendsOptsWith(searchSrv(t, snowTags(), nil), false)...)
	h.Press("/")
	h.Type("#첫눈")
	h.Advance(300 * time.Millisecond)
	if s := h.Screen(); strings.Contains(s, "태그 피드 열기") {
		t.Fatalf("open row shown although #첫눈 is a result:\n%s", s)
	}
}

func TestTrendsHashtagTooLong(t *testing.T) {
	h := uitest.New(t, trendsOptsWith(searchSrv(t, snowTags(), nil), false)...)
	h.Press("/")
	h.Type("#" + strings.Repeat("가", 51))
	h.Advance(300 * time.Millisecond)
	if n := len(reqsTo(h, "GET", "/v1/hashtags/search")); n != 0 {
		t.Fatalf("51-char tag sent %d requests", n)
	}
	if s := h.Screen(); !strings.Contains(s, "태그는 50자까지예요") {
		t.Fatalf("too-long note missing:\n%s", s)
	}
}

func TestTrendsSearchDebounceLatestOnly(t *testing.T) {
	h := uitest.New(t, trendsOptsWith(searchSrv(t, snowTags(), nil), false)...)
	h.Press("/")
	h.Type("#a")
	h.Advance(100 * time.Millisecond)
	h.Type("b")
	h.Advance(300 * time.Millisecond)
	reqs := reqsTo(h, "GET", "/v1/hashtags/search")
	if len(reqs) != 1 {
		t.Fatalf("hashtag search requests = %d, want exactly 1", len(reqs))
	}
	if q := query(t, reqs[0]); q.Get("q") != "ab" {
		t.Fatalf("q = %q, want ab", q.Get("q"))
	}
}

func TestTrendsPostSearch(t *testing.T) {
	srv := searchSrv(t, nil, devKimHits())
	id := postBy("dev_kim").ID
	srv.JSON("PUT /v1/posts/{id}/like", http.StatusOK, api.LikeResult{PostID: id, Liked: true, LikeCount: 6})
	h := uitest.New(t, trendsOptsWith(srv, false)...)
	h.Resize(120, 40)
	h.Press("/")
	h.Type("bubbletea")
	h.Advance(300 * time.Millisecond)
	reqs := reqsTo(h, "GET", "/v1/search/posts")
	if len(reqs) != 1 {
		t.Fatalf("post search requests = %d, want 1", len(reqs))
	}
	if q := query(t, reqs[0]); q.Get("q") != "bubbletea" || q.Get("limit") != "20" {
		t.Fatalf("post search query = %v, want q=bubbletea limit=20", q)
	}
	if s := h.Screen(); !strings.Contains(s, "@dev_kim") || !strings.Contains(s, "bubbletea v2") {
		t.Fatalf("post result card missing:\n%s", s)
	}
	h.Press("down", "l")
	if _, ok := lastReq(h, "PUT", "/v1/posts/"+id+"/like"); !ok {
		t.Fatalf("l on the result sent no like; requests %+v", h.API.Requests())
	}
	h.Press("enter")
	if top := feedTop(h); top.Kind != ui.RoutePost || top.ID != id {
		t.Fatalf("enter opened %+v, want RoutePost %s", top, id)
	}
}

func TestTrendsPostSearchBlockFilter(t *testing.T) {
	hits := []api.PostSearchHit{{Post: postBy("hana")}, {Post: postBy("dev_kim")}}
	h := uitest.New(t, trendsOptsWith(searchSrv(t, nil, hits), false)...)
	h.Resize(120, 40)
	h.Press("/")
	h.Type("눈")
	h.Advance(300 * time.Millisecond)
	if s := h.Screen(); !strings.Contains(s, "@hana") {
		t.Fatalf("hana's result missing:\n%s", s)
	}
	h.Event("block.updated", map[string]any{"user_id": userOf("hana").ID, "blocked": true})
	if s := h.Screen(); strings.Contains(s, "@hana") || !strings.Contains(s, "@dev_kim") {
		t.Fatalf("blocked hana still among the results:\n%s", s)
	}
}

func TestTrendsSearchError(t *testing.T) {
	srv := searchSrv(t, nil, nil)
	srv.Error("GET /v1/search/posts", http.StatusTooManyRequests, "rate_limited")
	h := uitest.New(t, trendsOptsWith(srv, false)...)
	h.Press("/")
	h.Type("bubbletea")
	h.Advance(300 * time.Millisecond)
	if n := len(reqsTo(h, "GET", "/v1/search/posts")); n != 1 {
		t.Fatalf("post search requests = %d, want 1", n)
	}
	h.Check()
}

func TestTrendsSearchEmptyAndEsc(t *testing.T) {
	h := uitest.New(t, trendsOptsWith(searchSrv(t, nil, nil), false)...)
	h.Press("/")
	h.Type("없는말")
	h.Advance(300 * time.Millisecond)
	if s := h.Screen(); !strings.Contains(s, "검색 결과가 없어요") {
		t.Fatalf("empty result state missing:\n%s", s)
	}
	n := len(h.API.Requests())
	inst := h.Instance(keyTrendsRoute)
	h.Press("esc")
	if q := TrendsQuery(inst); q != "" || !TrendsSearchOpen(inst) {
		t.Fatalf("first esc: query %q open %v, want cleared and open", q, TrendsSearchOpen(inst))
	}
	h.Press("esc")
	if TrendsSearchOpen(inst) {
		t.Fatal("second esc left the search line open")
	}
	s := h.Screen()
	if strings.Contains(s, "검색 결과가 없어요") || !strings.Contains(s, "#첫눈") || !strings.Contains(s, "42개 글") {
		t.Fatalf("Top 10 not back after esc esc:\n%s", s)
	}
	h.Advance(time.Second)
	if m := len(h.API.Requests()); m != n {
		t.Fatalf("esc esc sent %d more requests", m-n)
	}
	if top := feedTop(h); top.Kind != ui.RouteTrends {
		t.Fatalf("esc esc left trends: top %+v", top)
	}
}

func TestTrendsSearchKeysReturnToInput(t *testing.T) {
	h := uitest.New(t, trendsOptsWith(searchSrv(t, snowTags(), nil), false)...)
	h.Press("/")
	h.Type("#첫")
	h.Advance(300 * time.Millisecond)
	inst := h.Instance(keyTrendsRoute)
	h.Press("tab")
	if TrendsInputFocused(inst) {
		t.Fatal("tab did not move to the results")
	}
	if id := TrendsSelectedID(inst); id != "첫눈" {
		t.Fatalf("selected after tab = %q, want 첫눈", id)
	}
	h.Press("k")
	if !TrendsInputFocused(inst) {
		t.Fatal("k on the first result did not return to the input")
	}
	if h.Cursor() == nil {
		t.Fatal("no cursor while the input is focused")
	}
	h.Type("눈")
	if q := TrendsQuery(inst); q != "#첫눈" {
		t.Fatalf("query after typing = %q, want #첫눈", q)
	}
}

func TestTrendsSearchSurvivesModes(t *testing.T) {
	h := uitest.New(t, trendsOptsWith(searchSrv(t, nil, devKimHits()), false)...)
	h.Resize(120, 40)
	h.Press("/")
	h.Type("bubbletea")
	h.Advance(300 * time.Millisecond)
	h.Press("down")
	inst := h.Instance(keyTrendsRoute)
	want := postBy("dev_kim").ID
	if id := TrendsSelectedID(inst); id != want {
		t.Fatalf("selected = %q, want %q", id, want)
	}
	for _, s := range []uitest.Size{{W: 80, H: 24}, {W: 200, H: 60}, {W: 120, H: 40}} {
		h.Resize(s.W, s.H)
		if got := h.Instance(keyTrendsRoute); got != inst {
			t.Fatalf("%dx%d: trends instance replaced", s.W, s.H)
		}
		if q := TrendsQuery(inst); q != "bubbletea" {
			t.Fatalf("%dx%d: query = %q", s.W, s.H, q)
		}
		if id := TrendsSelectedID(inst); id != want {
			t.Fatalf("%dx%d: selected = %q, want %q", s.W, s.H, id, want)
		}
		h.Check()
	}
}

func TestTrendsSearchPersistent(t *testing.T) {
	h := uitest.New(t, trendsOptsWith(searchSrv(t, nil, devKimHits()), false)...)
	h.Press("/")
	h.Type("bubbletea")
	h.Advance(300 * time.Millisecond)
	h.Press("down")
	p, ok := h.Instance(keyTrendsRoute).(ui.Persistent)
	if !ok {
		t.Fatal("trends is not ui.Persistent")
	}
	st := p.SaveState()

	h2 := uitest.New(t, trendsOptsWith(searchSrv(t, nil, devKimHits()), false)...)
	fresh := newTrends(h2.Context(), ui.Route{Kind: ui.RouteTrends})
	fresh.(ui.Persistent).LoadState(st)
	if fresh.Init(h2.Context()) == nil {
		t.Fatal("restored search with a query issued no search on Init")
	}
	if q := TrendsQuery(fresh); q != "bubbletea" || !TrendsSearchOpen(fresh) {
		t.Fatalf("restored query %q open %v", q, TrendsSearchOpen(fresh))
	}
}

func TestTrendsSearchGolden(t *testing.T) {
	srv := searchSrv(t, snowTags(), devKimHits())
	for _, s := range uitest.Required {
		t.Run(fmt.Sprintf("%dx%d", s.W, s.H), func(t *testing.T) {
			h := uitest.New(t, trendsOptsWith(srv, false)...)
			h.Resize(s.W, s.H)
			h.Press("/")
			h.Type("#첫")
			h.Advance(300 * time.Millisecond)
			h.Golden("tags")

			h = uitest.New(t, trendsOptsWith(srv, false)...)
			h.Resize(s.W, s.H)
			h.Press("/")
			h.Type("bubbletea")
			h.Advance(300 * time.Millisecond)
			h.Golden("posts")
		})
	}
}
