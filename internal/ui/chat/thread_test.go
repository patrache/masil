package chat

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// ---- helpers (Task 13) ----

// devRootMsg is the canned #개발 thread root ("오늘 배포 몇 시에 해요?").
func devRootMsg(t testing.TB) api.Message {
	t.Helper()
	for _, m := range fixtures.Messages(fixtures.ChDev) {
		if m.Thread != nil && m.Thread.ReplyCount > 0 {
			return m
		}
	}
	t.Fatal("no #개발 thread root")
	return api.Message{}
}

// threadOfH is the thread view placed in some slot of the last frame.
func threadOfH(t testing.TB, h *uitest.Harness) *threadView {
	t.Helper()
	r, ok := threadRoute(h)
	if !ok {
		t.Fatalf("no thread placed: %+v", h.Model.Frame().Routes)
	}
	v, ok := h.Instance(r.Key()).(*threadView)
	if !ok {
		t.Fatalf("thread instance is %T", h.Instance(r.Key()))
	}
	return v
}

// threadSelected is the id of the selected row of the thread view ("" = none).
func threadSelected(t testing.TB, h *uitest.Harness) string {
	t.Helper()
	r, ok := threadOfH(t, h).pane.selected()
	if !ok {
		return ""
	}
	return r.ID
}

// threadRowVisible reports whether row id is drawn on some line of the thread list.
func threadRowVisible(t testing.TB, h *uitest.Harness, id string) bool {
	t.Helper()
	h.Screen()
	v := threadOfH(t, h)
	for y := 0; y < v.pane.listSize.H; y++ {
		if rid, _, ok := v.pane.lineAt(h.Context(), y); ok && rid == id {
			return true
		}
	}
	return false
}

// genReplies is genMsgs as replies of root.
func genReplies(ch, root string, from, to int) []api.Message {
	out := genMsgs(ch, from, to)
	for i := range out {
		r := root
		out[i].ThreadRootID = &r
	}
	return out
}

// pagedThread serves GET /v1/messages/<root>/thread (exact path, ONE handler
// switching on the query): the latest page msgID(51..100) with has_more_before,
// before=msgID(51) -> msgID(1..50) without.
func pagedThread(t testing.TB, srv *apitest.Server, root api.Message) {
	t.Helper()
	srv.Handle("GET /v1/messages/"+root.ID+"/thread", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var out api.ThreadWindow
		switch {
		case q.Get("before") == msgID(51):
			out = api.ThreadWindow{Root: root, Items: genReplies(root.ChannelID, root.ID, 1, 50)}
		case q.Get("before") == "" && q.Get("after") == "":
			out = api.ThreadWindow{Root: root, Items: genReplies(root.ChannelID, root.ID, 51, 100), HasMoreBefore: true}
		default:
			out = api.ThreadWindow{Root: root, Items: []api.Message{}}
		}
		writeJSON(w, http.StatusOK, out)
	})
}

// threadQueries are the raw queries of every GET of root's thread, in order.
func threadQueries(srv *apitest.Server, root string) []string {
	var out []string
	for _, r := range reqs(srv, "GET", "/v1/messages/"+root+"/thread") {
		out = append(out, r.Query)
	}
	return out
}

// ---- tests ----

func TestThreadPlacementPerMode(t *testing.T) {
	cases := []struct {
		sz       uitest.Size
		slot     layout.SlotID
		mainKind ui.RouteKind
	}{
		{uitest.Size{W: 200, H: 60}, layout.SlotThread, ui.RouteChannel}, // command: top right
		{uitest.Size{W: 120, H: 40}, layout.SlotThread, ui.RouteChannel}, // standard: split below
		{uitest.Size{W: 120, H: 30}, layout.SlotMain, ui.RouteThread},    // compact: replaces
		{uitest.Size{W: 80, H: 24}, layout.SlotMain, ui.RouteThread},     // widget: replaces
		{uitest.Size{W: 50, H: 20}, layout.SlotMain, ui.RouteThread},     // tiny: replaces
	}
	for _, c := range cases {
		h := devChannel(t, c.sz)
		selectText(t, h, "오늘 배포")
		h.Press("t")
		f := h.Model.Frame()
		if f.Routes[c.slot].Kind != ui.RouteThread || f.Routes[layout.SlotMain].Kind != c.mainKind {
			t.Fatalf("%v: %+v", c.sz, f.Routes)
		}
		if c.sz == (uitest.Size{W: 200, H: 60}) {
			if _, ok := f.Layout.Pane(layout.SlotMembers); ok {
				t.Fatal("thread replaces members")
			}
		}
		if c.sz == (uitest.Size{W: 120, H: 40}) {
			m, _ := f.Layout.Pane(layout.SlotMain)
			th, _ := f.Layout.Pane(layout.SlotThread)
			if th.Rect.Y <= m.Rect.Y || th.Rect.X != m.Rect.X {
				t.Fatal("standard: thread below main")
			}
		}
		if c.slot == layout.SlotMain {
			h.Press("esc")
			if h.Model.Frame().Routes[layout.SlotMain].Kind != ui.RouteChannel {
				t.Fatal("esc back to channel")
			}
		}
	}
}

func TestThreadGolden(t *testing.T) {
	forEachSize(t, uitest.AllSizes(), devChannel, func(t *testing.T, h *uitest.Harness) {
		selectText(t, h, "오늘 배포")
		h.Press("enter")
		if _, placed := threadRoute(h); placed { // TooSmall places nothing
			mustContain(t, h.Screen(), "스레드")
		}
		h.Golden("thread")
	})
}

func TestThreadTitleAndDensity(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	selectText(t, h, "오늘 배포")
	h.Press("t")
	v := threadOfH(t, h)
	title, _ := v.Title(h.Context())
	if !strings.HasPrefix(title, "스레드 · 민지: 오늘 배포") || !strings.HasSuffix(title, "…") {
		t.Fatalf("title %q", title)
	}
	mustContain(t, paneText(t, h, layout.SlotThread), "답글 3개", "2시?", "ㅇㅋ", "› 답글")

	// command mode: the right column is narrower than 48 cells => one-line rows
	h.Resize(200, 60)
	th, ok := h.Model.Frame().Layout.Pane(layout.SlotThread)
	if !ok || th.Rect.W >= 48 {
		t.Fatalf("command thread pane %+v", th)
	}
	if d := threadOfH(t, h).density(h.Context()); d != ui.DensityLine {
		t.Fatalf("narrow full => line, got %v", d)
	}
	mustContain(t, paneText(t, h, layout.SlotThread), "준호 2시?")
}

func TestThreadReply(t *testing.T) {
	root := devRootMsg(t)
	srv := newAPI(t)
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: root.ChannelID}, srv, uitest.WithSeed(bootstrappedSeed))
	selectText(t, h, "오늘 배포")
	h.Press("t", "i")
	h.Type("다음 주?")
	h.Press("enter")
	r := reqs(srv, "POST", "/v1/messages/"+root.ID+"/thread")
	if len(r) != 1 {
		t.Fatalf("%d reply posts", len(r))
	}
	mustJSONField(t, r[0].Body, "client_nonce", func(s string) bool { return len(s) == 26 })
	mustJSONEq(t, r[0].Body, "kind", "text")
	mustJSONEq(t, r[0].Body, "body", "다음 주?")
	if n := strings.Count(paneText(t, h, layout.SlotThread), "다음 주?"); n != 1 {
		t.Fatalf("reply shown %d times:\n%s", n, paneText(t, h, layout.SlotThread))
	}
	if len(pendingFor(h.Context(), root.ChannelID, root.ID)) != 0 {
		t.Fatal("pending dropped after the echo")
	}
	if strings.Contains(paneText(t, h, layout.SlotMain), "다음 주?") {
		t.Fatal("a reply never lands in the channel list")
	}
}

func TestThreadReplyFailureStaysInThread(t *testing.T) {
	root := devRootMsg(t)
	srv := newAPI(t)
	// exact path: more specific than chatRoutes' {id} pattern, so it wins for this root only
	srv.Handle("POST /v1/messages/"+root.ID+"/thread", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(503)
		io.WriteString(w, `{"error":{"code":"service_unavailable","message":"x","details":{}}}`)
	})
	h := openWith(t, uitest.Size{W: 200, H: 60}, ui.Route{Kind: ui.RouteChannel, ID: root.ChannelID}, srv, uitest.WithSeed(bootstrappedSeed))
	selectText(t, h, "오늘 배포")
	h.Press("t", "i")
	h.Type("다음 주?")
	h.Press("enter")
	if p := pendingFor(h.Context(), root.ChannelID, root.ID); len(p) != 1 || p[0].Failed == nil {
		t.Fatalf("one failed pending reply: %+v", p)
	}
	mustContain(t, paneText(t, h, layout.SlotThread), "다음 주?", "전송 실패")
	if strings.Contains(paneText(t, h, layout.SlotMain), "다음 주?") {
		t.Fatal("the pending reply stays out of the channel list")
	}
}

func TestThreadReplyRejectsPoll(t *testing.T) {
	root := devRootMsg(t)
	srv := newAPI(t)
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: root.ChannelID}, srv, uitest.WithSeed(bootstrappedSeed))
	selectText(t, h, "오늘 배포")
	h.Press("t", "i")
	h.Type(`/poll "점심 메뉴" "국밥" "돈까스"`)
	h.Press("enter")
	if n := len(reqs(srv, "POST", "/v1/messages/"+root.ID+"/thread")); n != 0 {
		t.Fatalf("a poll is never posted in a thread (%d)", n)
	}
	if threadOfH(t, h).composer.c.Value() == "" {
		t.Fatal("draft kept for a fix")
	}
}

func TestThreadTypingCarriesRoot(t *testing.T) {
	root := devRootMsg(t)
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	rt := &recordRT{}
	h.Context().RT = rt
	selectText(t, h, "오늘 배포")
	h.Press("t", "i")
	h.Type("가")
	if len(rt.Typed) == 0 || rt.Typed[len(rt.Typed)-1] != root.ChannelID+"|"+root.ID {
		t.Fatalf("typing %v", rt.Typed)
	}
}

func TestThreadAnchorJump(t *testing.T) {
	root := devRootMsg(t)
	srv := newAPI(t)
	pagedThread(t, srv, root)
	r := ui.Route{Kind: ui.RouteThread, ID: root.ID, Arg: root.ChannelID, Anchor: msgID(7)}
	h := openWith(t, uitest.Size{W: 120, H: 30}, r, srv, uitest.WithSeed(bootstrappedSeed))
	q := threadQueries(srv, root.ID)
	if len(q) != 2 || q[0] != "limit=50" || q[1] != "before="+msgID(51)+"&limit=50" {
		t.Fatalf("queries %q", q)
	}
	if threadSelected(t, h) != msgID(7) || !threadRowVisible(t, h, msgID(7)) {
		t.Fatalf("selected %q", threadSelected(t, h))
	}

	r.Anchor = msgID(90)
	h.Send(ui.RouteUpdatedMsg{Route: r})
	if n := len(threadQueries(srv, root.ID)); n != 2 {
		t.Fatalf("already in the window: no new request (%d)", n)
	}
	if threadSelected(t, h) != msgID(90) || !threadRowVisible(t, h, msgID(90)) {
		t.Fatalf("selected %q", threadSelected(t, h))
	}

	r.Anchor = msgID(900) // never shows up: the pages are exhausted
	h.Send(ui.RouteUpdatedMsg{Route: r})
	if n := len(threadQueries(srv, root.ID)); n != 2 {
		t.Fatalf("nothing left to page (%d)", n)
	}
	if threadSelected(t, h) != root.ID {
		t.Fatalf("selection on the root, got %q", threadSelected(t, h))
	}
	if tt := toastText(t, h); !strings.Contains(tt, "답글을 찾을 수 없어요") {
		t.Fatalf("toast %q", tt)
	}

	// the same anchor again does not drag the selection back
	h.Press("j")
	sel := threadSelected(t, h)
	h.Send(ui.RouteUpdatedMsg{Route: r})
	if threadSelected(t, h) != sel {
		t.Fatal("a handled anchor is not applied twice")
	}
}

func TestThreadAnchorViaNavigate(t *testing.T) {
	root := devRootMsg(t)
	srv := newAPI(t)
	pagedThread(t, srv, root)
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: root.ChannelID}, srv, uitest.WithSeed(bootstrappedSeed))
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteThread, ID: root.ID, Arg: root.ChannelID, Anchor: msgID(60)}})
	if threadSelected(t, h) != msgID(60) {
		t.Fatalf("selected %q", threadSelected(t, h))
	}
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteThread, ID: root.ID, Arg: root.ChannelID, Anchor: msgID(3)}})
	if threadSelected(t, h) != msgID(3) {
		t.Fatalf("re-entry with a new anchor pages back: selected %q (queries %q)", threadSelected(t, h), threadQueries(srv, root.ID))
	}
}

func TestThreadUpdatesChannelSummary(t *testing.T) {
	root := devRootMsg(t)
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	selectText(t, h, "오늘 배포")
	h.Press("t")
	h.Event("thread.updated", realtime.ThreadUpdated{ChannelID: root.ChannelID, RootID: root.ID, Thread: api.ThreadSummary{ReplyCount: 4, ParticipantIDs: []string{fixtures.UserJunho}}})
	mustContain(t, paneText(t, h, layout.SlotMain), "답글 4개")
	mustContain(t, paneText(t, h, layout.SlotThread), "답글 4개")
}

func TestThreadLoadsOlderReplies(t *testing.T) {
	root := devRootMsg(t)
	srv := newAPI(t)
	pagedThread(t, srv, root)
	h := openWith(t, uitest.Size{W: 120, H: 30}, ui.Route{Kind: ui.RouteThread, ID: root.ID, Arg: root.ChannelID}, srv, uitest.WithSeed(bootstrappedSeed))
	if threadSelected(t, h) != msgID(100) {
		t.Fatalf("opens on the latest reply, got %q", threadSelected(t, h))
	}
	h.Press("home")
	q := threadQueries(srv, root.ID)
	if len(q) != 2 || q[1] != "before="+msgID(51)+"&limit=50" {
		t.Fatalf("queries %q", q)
	}
	_, w := chatOf(h.Context()).Thread(root.ID)
	if w.Len() != 100 || w.HasMoreBefore() {
		t.Fatalf("replies %d more=%v", w.Len(), w.HasMoreBefore())
	}
	h.Press("j", "home")
	if n := len(threadQueries(srv, root.ID)); n != 2 {
		t.Fatalf("no more before: no request (%d)", n)
	}
}

func TestThreadDraftAcrossModes(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 30})
	selectText(t, h, "오늘 배포")
	h.Press("enter", "i")
	h.Type("2시?")
	key := h.Model.Frame().Routes[layout.SlotMain].Key()
	inst := h.Instance(key)
	h.Resize(200, 60)
	if h.Model.Frame().Routes[layout.SlotThread].Key() != key || h.Instance(key) != inst || h.Model.Focus().Slot != layout.SlotThread {
		t.Fatal("same instance in SlotThread, focused")
	}
	if inst.(*threadView).composer.c.Value() != "2시?" {
		t.Fatal("draft kept")
	}
	h.Resize(120, 30)
	if h.Model.Frame().Routes[layout.SlotMain].Key() != key {
		t.Fatal("back to Main")
	}
	if inst.(*threadView).composer.c.Value() != "2시?" {
		t.Fatal("draft kept back in Main")
	}
}

func TestThreadSaveLoadState(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	selectText(t, h, "오늘 배포")
	h.Press("t", "i")
	h.Type("초안")
	st := threadOfH(t, h).SaveState()
	r, _ := threadRoute(h)
	v := newThread(h.Context(), r).(*threadView)
	v.LoadState(st)
	if v.composer.c.Value() != "초안" {
		t.Fatalf("draft %q", v.composer.c.Value())
	}
}

func TestThreadHostileIntegrity(t *testing.T) {
	mk := func(t *testing.T, sz uitest.Size) *uitest.Harness {
		s := seeded()
		fixtures.Hostile(s)
		ch := hostileChannel(t, s)
		var root api.Message
		for _, m := range fixtures.HostileMessages(ch) {
			if m.Thread != nil && m.Thread.ReplyCount == 9999 {
				root = m
			}
		}
		if root.ID == "" {
			t.Fatal("no hostile root")
		}
		seed := func(s *store.Store) {
			bootstrappedSeed(s)
			fixtures.Hostile(s)
		}
		return open(t, sz, ui.Route{Kind: ui.RouteThread, ID: root.ID, Arg: ch}, uitest.WithSeed(seed))
	}
	forEachSize(t, uitest.AllSizes(), mk, func(t *testing.T, h *uitest.Harness) {
		h.Check()
		if _, ok := threadRoute(h); !ok {
			return // TooSmall: nothing placed
		}
		mustContain(t, h.Screen(), "스레드")
		h.Press("home", "m")
		h.Press("esc")
		h.Check()
	})
}

func TestThreadLongRootCapped(t *testing.T) {
	root := devRootMsg(t)
	root.Body = strings.Repeat("긴 줄입니다\n", 20)
	srv := newAPI(t)
	srv.JSON("GET /v1/messages/"+root.ID+"/thread", http.StatusOK, api.ThreadWindow{Root: root, Items: []api.Message{}})
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteThread, ID: root.ID, Arg: root.ChannelID}, srv, uitest.WithSeed(bootstrappedSeed))
	txt := paneText(t, h, layout.SlotThread)
	if n := strings.Count(txt, "긴 줄입니다"); n > rootMaxLines {
		t.Fatalf("root capped at %d lines, got %d:\n%s", rootMaxLines, n, txt)
	}
	mustContain(t, txt, "… 더 보기")
	// enter on the root expands it
	v := threadOfH(t, h)
	v.pane.selectID(root.ID)
	h.Press("enter")
	if strings.Contains(paneText(t, h, layout.SlotThread), "… 더 보기") {
		t.Fatal("expanded")
	}
}

func TestThreadRootDeleted(t *testing.T) {
	root := devRootMsg(t)
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	selectText(t, h, "오늘 배포")
	h.Press("t")
	h.Event("message.deleted", realtime.MessageDeleted{ChannelID: root.ChannelID, MessageID: root.ID, Tombstone: true})
	mustContain(t, paneText(t, h, layout.SlotThread), "삭제된 메시지", "2시?", "ㅇㅋ")
}

func TestThreadGoneShowsNotice(t *testing.T) {
	root := devRootMsg(t)
	srv := newAPI(t)
	srv.Error("GET /v1/messages/"+root.ID+"/thread", http.StatusNotFound, "not_found")
	h := openWith(t, uitest.Size{W: 120, H: 30}, ui.Route{Kind: ui.RouteThread, ID: root.ID, Arg: root.ChannelID}, srv, uitest.WithSeed(bootstrappedSeed))
	mustContain(t, h.Screen(), goneTitle)
	h.Press("i")
	if threadOfH(t, h).CapturesInput() {
		t.Fatal("no input in a gone thread")
	}
	h.Check()
}

func TestThreadChannelReplyEditFromList(t *testing.T) {
	// e on my reply edits it in the thread's composer; enter sends PATCH
	root := devRootMsg(t)
	srv := newAPI(t)
	h := openWith(t, uitest.Size{W: 120, H: 30}, ui.Route{Kind: ui.RouteThread, ID: root.ID, Arg: root.ChannelID}, srv, uitest.WithSeed(bootstrappedSeed))
	v := threadOfH(t, h)
	var mine string
	for _, r := range v.pane.rows {
		if r.Kind == rowMessage && r.Msg != nil && r.Msg.Body == "ㅇㅋ" {
			mine = r.ID
		}
	}
	if !v.pane.selectID(mine) {
		t.Fatal("no reply of mine")
	}
	h.Press("e")
	if v.composer.editing == nil || v.composer.editing.ID != mine {
		t.Fatal("editing my reply")
	}
	h.Press("end")
	h.Type("!")
	h.Press("enter")
	p := reqs(srv, "PATCH", "/v1/messages/"+mine)
	if len(p) != 1 {
		t.Fatalf("%d PATCH", len(p))
	}
	var body map[string]string
	json.Unmarshal(p[0].Body, &body)
	if body["body"] != "ㅇㅋ!" {
		t.Fatalf("body %q", body["body"])
	}
}
