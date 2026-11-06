package chat

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// ---- search helpers (Task 21) ----

const searchPath = "/v1/search/messages"

var size120x40 = uitest.Size{W: 120, H: 40}

// openSearch opens RouteSearch{Arg: q} (global scope) on the bootstrapped seed.
func openSearch(t *testing.T, sz uitest.Size, q string) *uitest.Harness {
	t.Helper()
	return open(t, sz, ui.Route{Kind: ui.RouteSearch, Arg: q}, uitest.WithSeed(bootstrappedSeed))
}

func openSearchWithResults(t *testing.T, sz uitest.Size) *uitest.Harness {
	t.Helper()
	return openSearch(t, sz, "배포")
}

func openSearchEmpty(t *testing.T, sz uitest.Size) *uitest.Harness {
	t.Helper()
	return openSearch(t, sz, "없는낱말")
}

// searchOf is the search instance on top of the active tab.
func searchOf(t testing.TB, h *uitest.Harness) *searchView {
	t.Helper()
	r := top(h)
	v, ok := h.Instance(r.Key()).(*searchView)
	if !ok || r.Kind != ui.RouteSearch {
		t.Fatalf("top is %+v (%T), not a search", r, h.Instance(r.Key()))
	}
	return v
}

// searchQueries are the parsed queries of every search request so far.
func searchQueries(t testing.TB, srv *apitest.Server) []url.Values {
	t.Helper()
	var out []url.Values
	for _, r := range reqs(srv, "GET", searchPath) {
		q, err := url.ParseQuery(r.Query)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, q)
	}
	return out
}

// lastSearch is the last search query (fails without one).
func lastSearch(t testing.TB, srv *apitest.Server) url.Values {
	t.Helper()
	qs := searchQueries(t, srv)
	if len(qs) == 0 {
		t.Fatal("no search request")
	}
	return qs[len(qs)-1]
}

// devRef is the ChannelRef of #개발 (노바 개발방).
func devRef() api.ChannelRef {
	s := seeded()
	ch := s.Channels[fixtures.ChDev]
	return api.ChannelRef{ID: ch.ID, Kind: ch.Kind, Name: ch.Name, SpaceID: ch.SpaceID, SpaceName: ptrTo(s.Spaces[fixtures.SpaceNova].Name)}
}

// searchError answers the search with a 422 limit_exceeded of details.what = what.
func searchError(what string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": map[string]any{
			"code": "limit_exceeded", "message": "too many", "details": map[string]any{"what": what, "limit": 500},
		}})
	}
}

// ---- tests ----

func TestHighlightSnippet(t *testing.T) {
	th := theme.Get(theme.Amber)
	raw := highlightSnippet(th, "오늘 배포 몇 시에 해요?", []api.SearchHighlight{{Start: 3, Length: 2}}, 30)
	if got := ansi.Strip(raw); got != "오늘 배포 몇 시에 해요?" {
		t.Fatalf("%q", got)
	}
	if !strings.Contains(raw, th.S.Selected.Render("배포")) {
		t.Fatalf("highlight not in Selected: %q", raw)
	}
	long := strings.Repeat("가", 40) + "배포" + strings.Repeat("나", 40)
	got := ansi.Strip(highlightSnippet(th, long, []api.SearchHighlight{{Start: 40, Length: 2}}, 20))
	if !strings.HasPrefix(got, "…") || !strings.Contains(got, "배포") || textwidth.Width(got) > 20 {
		t.Fatalf("%q", got)
	}
	// a highlight already visible keeps the start; the end is cut with "…"
	got = ansi.Strip(highlightSnippet(th, long, []api.SearchHighlight{{Start: 1, Length: 2}}, 20))
	if !strings.HasPrefix(got, "가가") || !strings.HasSuffix(got, "…") || textwidth.Width(got) > 20 {
		t.Fatalf("%q", got)
	}
	// newline in body becomes a space; out-of-range highlights are ignored without panic
	got = ansi.Strip(highlightSnippet(th, "첫 줄\n둘째 줄", []api.SearchHighlight{{Start: -3, Length: 2}, {Start: 99, Length: 2}, {Start: 2, Length: 0}}, 30))
	if got != "첫 줄 둘째 줄" {
		t.Fatalf("%q", got)
	}
	got = ansi.Strip(highlightSnippet(th, "배포 완료", []api.SearchHighlight{{Start: 3, Length: 1 << 20}}, 30))
	if got != "배포 완료" {
		t.Fatalf("clamped end: %q", got)
	}
	// controls are sanitized; tiny widths stay within width
	got = ansi.Strip(highlightSnippet(th, "a\x1b[31mb\x07c", nil, 30))
	if strings.ContainsAny(got, "\x1b\x07") {
		t.Fatalf("not sanitized: %q", got)
	}
	for w := 0; w <= 4; w++ {
		if got := ansi.Strip(highlightSnippet(th, long, []api.SearchHighlight{{Start: 40, Length: 2}}, w)); textwidth.Width(got) > w {
			t.Fatalf("width %d: %q", w, got)
		}
	}
	// a highlight edge inside a grapheme cluster snaps outward to whole clusters:
	// the row shows the same text as the message and the cluster is highlighted whole
	for _, c := range []struct {
		body, cluster string
		h             api.SearchHighlight
	}{
		{"❤️ 좋아요", "❤️", api.SearchHighlight{Start: 0, Length: 1}},      // VS16
		{"👍🏽 좋아", "👍🏽", api.SearchHighlight{Start: 0, Length: 1}},       // skin tone
		{"👨‍👩‍👧 가족", "👨‍👩‍👧", api.SearchHighlight{Start: 0, Length: 1}}, // ZWJ start
		{"가족 👨‍👩‍👧", "👨‍👩‍👧", api.SearchHighlight{Start: 5, Length: 2}}, // ZWJ middle
		{"🇰🇷 한국", "🇰🇷", api.SearchHighlight{Start: 0, Length: 1}},       // flag
		{"한국 🇰🇷", "🇰🇷", api.SearchHighlight{Start: 4, Length: 1}},       // flag tail
	} {
		raw := highlightSnippet(th, c.body, []api.SearchHighlight{c.h}, 30)
		want := textwidth.Sanitize(c.body)
		if got := ansi.Strip(raw); got != want {
			t.Errorf("%q %+v: got %q want %q", c.body, c.h, got, want)
		}
		if !strings.Contains(raw, th.S.Selected.Render(textwidth.Sanitize(c.cluster))) {
			t.Errorf("%q %+v: cluster not highlighted whole: %q", c.body, c.h, raw)
		}
	}
}

func TestSearchGolden(t *testing.T) {
	// too_small draws only its notice (no route); the golden still pins it.
	// Separate parents keep the "WxH" subtest names (and golden dirs) unique.
	t.Run("results", func(t *testing.T) {
		forEachSize(t, uitest.AllSizes(), openSearchWithResults, func(t *testing.T, h *uitest.Harness) {
			if h.Context().Mode != layout.ModeTooSmall {
				mustContain(t, h.Screen(), "배포")
			}
			h.Golden("search")
		})
	})
	t.Run("empty", func(t *testing.T) {
		forEachSize(t, uitest.Required, openSearchEmpty, func(t *testing.T, h *uitest.Harness) {
			if h.Context().Mode != layout.ModeTooSmall {
				mustContain(t, h.Screen(), "검색 결과가 없어요")
			}
			h.Golden("search_empty")
		})
	})
}

func TestSearchFromChannel(t *testing.T) {
	h := devChannel(t, size120x40)
	h.Press("/")
	if top(h).Kind != ui.RouteSearch || top(h).ID != devID(t, h) {
		t.Fatalf("scoped to channel: %+v", top(h))
	}
	if title, _ := searchOf(t, h).Title(h.Context()); title != "검색 · #개발" {
		t.Fatalf("title %q", title)
	}
	h.Type("배포")
	h.Press("enter")
	r := reqs(h.API, "GET", searchPath)
	if len(r) != 1 {
		t.Fatalf("requests %d", len(r))
	}
	q, _ := url.ParseQuery(r[0].Query)
	if q.Get("q") != "배포" || q.Get("channel_id") != devID(t, h) || q.Has("space_id") || q.Get("limit") != "20" {
		t.Fatalf("%v", q)
	}
	scr := h.Screen()
	mustContain(t, scr, "오늘 배포 몇 시에 해요?", "#개발 · 노바 개발방 · 민지 · 11:02")
	if strings.Contains(scr, "prod 배포 완료") {
		t.Fatal("other channel's hit in a channel search")
	}
	if searchOf(t, h).CapturesInput() {
		t.Fatal("enter moves focus to the results")
	}
	h.Press("esc")
	if top(h).Kind != ui.RouteChannel || top(h).ID != devID(t, h) {
		t.Fatalf("esc on the results goes back: %+v", top(h))
	}
}

func TestSearchScopeSwitch(t *testing.T) {
	h := devChannel(t, size120x40)
	h.Press("/")
	h.Type("배포")
	h.Press("enter")
	h.Press("l")
	q := lastSearch(t, h.API)
	if q.Get("space_id") != fixtures.SpaceNova || q.Has("channel_id") || q.Get("q") != "배포" {
		t.Fatalf("이 방: %v", q)
	}
	if title, _ := searchOf(t, h).Title(h.Context()); title != "검색 · 노바 개발방" {
		t.Fatalf("title %q", title)
	}
	mustContain(t, h.Screen(), "prod 배포 완료", "오늘 배포 몇 시에 해요?")
	h.Press("right")
	q = lastSearch(t, h.API)
	if q.Has("space_id") || q.Has("channel_id") {
		t.Fatalf("전체: %v", q)
	}
	if title, _ := searchOf(t, h).Title(h.Context()); title != "검색 · 전체" {
		t.Fatalf("title %q", title)
	}
	n := len(searchQueries(t, h.API))
	h.Press("l") // already the widest scope
	if len(searchQueries(t, h.API)) != n {
		t.Fatal("no request past the last scope")
	}
	h.Press("h")
	if q = lastSearch(t, h.API); q.Get("space_id") != fixtures.SpaceNova {
		t.Fatalf("h back to 이 방: %v", q)
	}
	clickText(t, h, layout.SlotMain, "이 채널")
	if q = lastSearch(t, h.API); q.Get("channel_id") != fixtures.ChDev || q.Has("space_id") {
		t.Fatalf("chip click: %v", q)
	}

	// a space-scoped search has no channel chip; a DM search has no space chip
	h2 := open(t, size120x40, ui.Route{Kind: ui.RouteSearch, ID: fixtures.SpaceNova, Arg: "배포"}, uitest.WithSeed(bootstrappedSeed))
	if q := lastSearch(t, h2.API); q.Get("space_id") != fixtures.SpaceNova {
		t.Fatalf("space route: %v", q)
	}
	pane := paneText(t, h2, layout.SlotMain)
	if strings.Contains(pane, "이 채널") || !strings.Contains(pane, "이 방") {
		t.Fatalf("chips:\n%s", pane)
	}
	h3 := open(t, size120x40, ui.Route{Kind: ui.RouteSearch, ID: fixtures.DMMinji, Arg: "점심"}, uitest.WithSeed(bootstrappedSeed))
	pane = paneText(t, h3, layout.SlotMain)
	if strings.Contains(pane, "이 방") || !strings.Contains(pane, "이 채널") {
		t.Fatalf("DM chips:\n%s", pane)
	}
	h3.Press("l")
	if q := lastSearch(t, h3.API); q.Has("space_id") || q.Has("channel_id") {
		t.Fatalf("DM → 전체: %v", q)
	}
}

func TestSearchOpenResult(t *testing.T) {
	root := devRootMsg(t)
	h := openSearch(t, size120x40, "배포")
	h.Press("enter") // newest first: the #개발 root
	want := ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChDev, Anchor: root.ID}
	if got := top(h); got.Key() != want.Key() || got.Anchor != want.Anchor {
		t.Fatalf("top %+v, want %+v", got, want)
	}

	srv := newAPI(t)
	tw, err := srv.Client().GetThread(t.Context(), root.ID, api.ThreadQuery{})
	if err != nil || len(tw.Items) < 2 {
		t.Fatalf("thread: %+v %v", tw, err)
	}
	reply := tw.Items[1]
	srv.JSON("GET "+searchPath, http.StatusOK, map[string]any{ // same shape: replaces the default search for this test
		"items":       []api.MessageSearchHit{{Message: reply, Channel: devRef(), Highlights: []api.SearchHighlight{{Start: 0, Length: 2}}}},
		"next_cursor": nil,
	})
	h = openWith(t, size120x40, ui.Route{Kind: ui.RouteSearch, Arg: "ㅇㅋ"}, srv, uitest.WithSeed(bootstrappedSeed))
	h.Press("enter")
	got, ok := threadRoute(h)
	wantT := ui.Route{Kind: ui.RouteThread, ID: root.ID, Arg: fixtures.ChDev, Anchor: reply.ID}
	if !ok || got.Key() != wantT.Key() || got.Anchor != wantT.Anchor {
		t.Fatalf("thread %+v (%v), want %+v", got, ok, wantT)
	}
	if sel := threadSelected(t, h); sel != reply.ID {
		t.Fatalf("thread selects the reply, got %q", sel)
	}
}

func TestSearchPagination(t *testing.T) {
	srv := newAPI(t)
	msgs := genMsgs(fixtures.ChDev, 1, 25)
	hit := func(m api.Message) api.MessageSearchHit {
		return api.MessageSearchHit{Message: m, Channel: devRef(), Highlights: []api.SearchHighlight{{Start: 0, Length: 3}}}
	}
	srv.Handle("GET "+searchPath, func(w http.ResponseWriter, r *http.Request) { // same shape: replaces the default search for this test
		var items []api.MessageSearchHit
		var next *string
		if r.URL.Query().Get("cursor") == "p2" {
			for i := 4; i >= 0; i-- {
				items = append(items, hit(msgs[i]))
			}
		} else {
			for i := 24; i >= 5; i-- {
				items = append(items, hit(msgs[i]))
			}
			next = ptrTo("p2")
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
	})
	h := openWith(t, size120x40, ui.Route{Kind: ui.RouteSearch, Arg: "메시지"}, srv, uitest.WithSeed(bootstrappedSeed))
	if n := len(searchOf(t, h).hits); n != 20 {
		t.Fatalf("first page %d", n)
	}
	h.Press("G")
	if n := len(searchQueries(t, srv)); n != 1 {
		t.Fatalf("G alone does not page (%d)", n)
	}
	h.Press("j")
	qs := searchQueries(t, srv)
	if len(qs) != 2 || qs[1].Get("cursor") != "p2" || qs[1].Get("q") != "메시지" {
		t.Fatalf("queries %v", qs)
	}
	v := searchOf(t, h)
	if len(v.hits) != 25 || v.hits[24].Message.ID != msgs[0].ID {
		t.Fatalf("appended %d", len(v.hits))
	}
	h.Press("G")
	h.Press("j")
	if n := len(searchQueries(t, srv)); n != 2 {
		t.Fatalf("no next page: no request (%d)", n)
	}
	mustContain(t, h.Screen(), "메시지 1")
}

func TestSearchScopeTooWide(t *testing.T) {
	srv := newAPI(t)
	srv.Handle("GET "+searchPath, searchError("search_scope")) // same shape: replaces the default search for this test
	h := openWith(t, size120x40, ui.Route{Kind: ui.RouteSearch, Arg: "배포"}, srv, uitest.WithSeed(bootstrappedSeed))
	mustContain(t, h.Screen(), "검색 범위가 너무 넓어요 — 방이나 채널을 골라 주세요")

	srv.Handle("GET "+searchPath, searchError("something_else")) // same shape: replaces it again
	h = openWith(t, size120x40, ui.Route{Kind: ui.RouteSearch, Arg: "배포"}, srv, uitest.WithSeed(bootstrappedSeed))
	scr := h.Screen()
	if strings.Contains(scr, "검색 범위가 너무 넓어요") {
		t.Fatal("only search_scope is the scope message")
	}
	mustContain(t, scr, "한도를 넘었어요")

	// a narrower scope after the error searches again
	srv.Handle("GET "+searchPath, searchMessages) // same shape: back to the default search
	h.Press("h")
	if q := lastSearch(t, srv); q.Get("space_id") == "" {
		t.Fatalf("narrowed: %v", q)
	}
	mustContain(t, h.Screen(), "prod 배포 완료")
}

func TestSearchQueryLimits(t *testing.T) {
	h := open(t, size120x40, ui.Route{Kind: ui.RouteSearch}, uitest.WithSeed(bootstrappedSeed))
	v := searchOf(t, h)
	if !v.CapturesInput() || h.Cursor() == nil {
		t.Fatal("an empty search starts in the input with a real cursor")
	}
	mustContain(t, h.Screen(), "메시지를 검색해 보세요")
	h.Press("enter")
	h.Type("   ")
	h.Press("enter")
	if n := len(reqs(h.API, "GET", searchPath)); n != 0 {
		t.Fatalf("empty query: no request (%d)", n)
	}
	h.Press("ctrl+c") // clears the blanks
	h.Type(strings.Repeat("가", 120))
	if n := len([]rune(v.input.Value())); n != 100 {
		t.Fatalf("CharLimit 100, got %d", n)
	}
	h.Press("enter")
	if q := lastSearch(t, h.API); len([]rune(q.Get("q"))) != 100 {
		t.Fatalf("q %d runes", len([]rune(q.Get("q"))))
	}

	// i / "/" return to the input; ctrl+c clears it; pasted newlines become spaces
	h.Press("i")
	if !v.CapturesInput() {
		t.Fatal("i focuses the input")
	}
	h.Press("ctrl+c")
	if v.input.Value() != "" {
		t.Fatalf("ctrl+c clears: %q", v.input.Value())
	}
	h.Paste("점심\n메뉴")
	if v.input.Value() != "점심 메뉴" {
		t.Fatalf("paste %q", v.input.Value())
	}
}

func TestSearchPalette(t *testing.T) {
	h := devChannel(t, size120x40)
	h.Send(ui.RunCommandMsg{ID: "search.messages", Args: "배포"})
	if r := top(h); r.Kind != ui.RouteSearch || r.ID != "" || r.Arg != "배포" {
		t.Fatalf("top %+v", r)
	}
	if q := lastSearch(t, h.API); q.Get("q") != "배포" {
		t.Fatalf("%v", q)
	}
}

func TestSearchSavedState(t *testing.T) {
	h := devChannel(t, size120x40)
	h.Press("/")
	h.Type("배포")
	h.Press("enter")
	h.Press("l")
	v := searchOf(t, h)
	st := v.SaveState()
	w := newSearch(h.Context(), top(h)).(*searchView)
	var p ui.Persistent = w
	p.LoadState(st)
	if w.input.Value() != "배포" || w.scope != scopeSpace {
		t.Fatalf("restored %q %v", w.input.Value(), w.scope)
	}
}

func TestSearchHostileIntegrity(t *testing.T) {
	s := seeded()
	fixtures.Hostile(s)
	ch := s.Channels[hostileChannel(t, s)]
	ref := api.ChannelRef{ID: ch.ID, Kind: ch.Kind, Name: ch.Name, SpaceID: ch.SpaceID, SpaceName: ptrTo(s.Spaces[fixtures.SpaceHostile].Name)}
	var items []api.MessageSearchHit
	for i, m := range fixtures.HostileMessages(ch.ID) {
		hs := []api.SearchHighlight{{Start: i, Length: 3}, {Start: -1, Length: 4}, {Start: 1 << 20, Length: 1}, {Start: 2, Length: 1 << 20}}
		items = append(items, api.MessageSearchHit{Message: m, Channel: ref, Highlights: hs})
	}
	dm := api.ChannelRef{ID: "01JB0000000000000000NODM01", Kind: api.ChannelDM}
	items = append(items, api.MessageSearchHit{Message: fixtures.HostileMessages(ch.ID)[0], Channel: dm})
	seed := func(s *store.Store) {
		bootstrappedSeed(s)
		fixtures.Hostile(s)
	}
	forEachSize(t, uitest.AllSizes(), func(t *testing.T, sz uitest.Size) *uitest.Harness {
		srv := newAPI(t)
		srv.JSON("GET "+searchPath, http.StatusOK, map[string]any{"items": items, "next_cursor": "c2"}) // same shape: replaces the default search for this test
		return openWith(t, sz, ui.Route{Kind: ui.RouteSearch, ID: ch.ID, Arg: "x"}, srv, uitest.WithSeed(seed))
	}, func(t *testing.T, h *uitest.Harness) {
		h.Check()
		h.Press("G")
		h.Check()
		h.Press("i")
		h.Type(strings.Repeat("ㄱ", 30))
		h.Check()
	})
}
