package chat

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/errtext"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ids"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

func TestComposerGolden(t *testing.T) {
	forEachSize(t, uitest.AllSizes(), devChannel, func(t *testing.T, h *uitest.Harness) {
		h.Press("i")
		h.Type("배포 몇 시")
		h.Golden("composer")
		// too_small draws only the "창을 조금만 키워 주세요" notice (no route, no
		// input); the golden still pins it.
		if h.Context().Mode == layout.ModeTooSmall {
			return
		}
		row, col := lastLineWith(t, h.Screen(), "배포 몇 시") // the fixture message "오늘 배포 몇 시에 해요?" is above
		if c := h.Cursor(); c == nil || c.Y != row || c.X != col+textwidth.Width("배포 몇 시") {
			t.Fatalf("IME cursor %+v, want (%d,%d)", c, col+textwidth.Width("배포 몇 시"), row)
		}
	})
}

func TestComposerSendNoDuplicate(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	h.Press("i")
	h.Type("안녕하세요")
	h.Press("enter")
	r := reqs(h.API, "POST", "/v1/channels/"+devID(t, h)+"/messages")
	if len(r) != 1 {
		t.Fatalf("one POST, got %d", len(r))
	}
	mustJSONField(t, r[0].Body, "client_nonce", func(v string) bool { return ids.Valid(v) })
	mustJSONEq(t, r[0].Body, "kind", "text")
	mustJSONEq(t, r[0].Body, "body", "안녕하세요")
	if channelOf(t, h).composer.c.Value() != "" {
		t.Fatal("cleared")
	}
	if strings.Count(h.Screen(), "안녕하세요") != 1 {
		t.Fatal("pending merged with server echo by nonce")
	} // echo of the posted client_nonce comes from chatRoutes (Task 4), not fixtures
	if p := chatOf(h.Context()).Pending(devID(t, h)); len(p) != 0 {
		t.Fatalf("no pending left: %+v", p)
	}
	if !channelOf(t, h).composer.focused() {
		t.Fatal("the composer keeps the focus after sending")
	}
}

func TestComposerSendFailureRetrySameNonce(t *testing.T) {
	srv := newAPI(t)
	ch := chanID(t, seeded(), "개발")
	fail := true
	// exact path: more specific than chatRoutes' {id} pattern, so it wins without a conflict
	srv.Handle("POST /v1/channels/"+ch+"/messages", func(w http.ResponseWriter, r *http.Request) {
		if fail {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(503)
			io.WriteString(w, `{"error":{"code":"service_unavailable","message":"x","details":{}}}`)
			return
		}
		r.SetPathValue("id", ch)
		echoSend(false)(w, r) // echo 201 with the posted nonce
	})
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: ch}, srv, uitest.WithSeed(bootstrappedSeed))
	h.Press("i")
	h.Type("실패할 글")
	h.Press("enter")
	p := chatOf(h.Context()).Pending(ch)
	if len(p) != 1 || !api.IsCode(p[0].Failed, "service_unavailable") {
		t.Fatalf("one failed pending: %+v", p)
	}
	mustContain(t, h.Screen(), "실패할 글", "전송 실패", errtext.Describe(i18n.KoT(), p[0].Failed))
	fail = false
	h.Press("esc", "G", "enter") // failed pending row is last & selected; enter = resend
	r := reqs(srv, "POST", "/v1/channels/"+ch+"/messages")
	if len(r) != 2 || jsonField(r[0].Body, "client_nonce") != jsonField(r[1].Body, "client_nonce") {
		t.Fatal("same nonce on retry")
	}
	if jsonField(r[1].Body, "body") != "실패할 글" || jsonField(r[1].Body, "kind") != "text" {
		t.Fatalf("same body and kind on retry: %s", r[1].Body)
	}
	scr := h.Screen()
	if strings.Contains(scr, "전송 실패") {
		t.Fatal("cleared after success")
	}
	if strings.Count(scr, "실패할 글") != 1 {
		t.Fatal("exactly one copy after the resend")
	}
}

func TestComposerDiscardFailed(t *testing.T) {
	srv := newAPI(t)
	ch := chanID(t, seeded(), "개발")
	srv.Error("POST /v1/channels/"+ch+"/messages", http.StatusServiceUnavailable, "service_unavailable") // exact path
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: ch}, srv, uitest.WithSeed(bootstrappedSeed))
	h.Press("i")
	h.Type("버릴 글")
	h.Press("enter")
	mustContain(t, h.Screen(), "버릴 글", "전송 실패")
	h.Press("esc", "G", "d")
	if strings.Contains(h.Screen(), "버릴 글") {
		t.Fatal("discarded row gone")
	}
	if p := chatOf(h.Context()).Pending(ch); len(p) != 0 {
		t.Fatalf("pending dropped: %+v", p)
	}
	if n := len(reqs(srv, "POST", "/v1/channels/"+ch+"/messages")); n != 1 {
		t.Fatalf("discard sends nothing (%d POSTs)", n)
	}
}

func TestComposerRateLimited(t *testing.T) {
	srv := newAPI(t)
	ch := chanID(t, seeded(), "개발")
	srv.Handle("POST /v1/channels/"+ch+"/messages", func(w http.ResponseWriter, r *http.Request) { // exact path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":{"code":"rate_limited","message":"slow down","details":{"retry_after_sec":30}}}`)
	})
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: ch}, srv, uitest.WithSeed(bootstrappedSeed))
	h.Press("i")
	h.Type("빠른 글")
	h.Press("enter")
	p := chatOf(h.Context()).Pending(ch)
	if len(p) != 1 || p[0].Failed == nil || !api.IsCode(p[0].Failed, "rate_limited") {
		t.Fatalf("pending failed with rate_limited: %+v", p)
	}
	mustContain(t, h.Screen(), "빠른 글", "전송 실패", errtext.Describe(i18n.KoT(), p[0].Failed))
	h.Advance(time.Minute)
	if n := len(reqs(srv, "POST", "/v1/channels/"+ch+"/messages")); n != 1 {
		t.Fatalf("no automatic second POST (%d)", n)
	}
}

func TestComposerReadOnlyDM(t *testing.T) {
	// opened DIRECTLY by route (restored tab / search hit) — the sidebar and home hide a
	// blocked peer's 1:1 DM (TestSidebarHidesBlockedDM), but the route itself must still render
	for _, tc := range []struct {
		reason *string
		want   string
	}{
		{nil, "읽기 전용 대화예요"},
		{ptrTo("not_eligible"), "읽기 전용 대화예요"},
		{ptrTo("blocked"), "차단한 사용자와의 대화예요"},
	} {
		seed := func(s *store.Store) {
			bootstrappedSeed(s)
			dm := s.Channels[fixtures.DMJunho]
			dm.ReadOnly, dm.ReadOnlyReason = true, tc.reason
			if tc.reason != nil && *tc.reason == "blocked" {
				s.Blocks[fixtures.UserJunho] = uitest.Start
			}
		}
		h := open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: fixtures.DMJunho}, uitest.WithSeed(seed))
		mustContain(t, h.Screen(), tc.want)
		h.Press("i")
		v := channelOf(t, h)
		if v.CapturesInput() || v.Cursor() != nil {
			t.Fatalf("%s: read-only conversation must not capture input", tc.want)
		}
		h.Type("x")
		if v.composer.c.Value() != "" {
			t.Fatal("nothing typed into a read-only conversation")
		}
		h.Check()
	}
}

func TestComposerEditLast(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	h.Press("i", "up")
	if v := channelOf(t, h).composer.c.Value(); v != "@minji 점심 먹고 ㄱㄱ" {
		t.Fatalf("edit-last loads my last body, got %q", v)
	}
	mustContain(t, h.Screen(), "수정 중", "esc 취소")
	h.Type(" 2시")
	h.Press("enter")
	r := reqs(h.API, "PATCH", "/v1/messages/"+myLastID(t, h))
	if len(r) != 1 {
		t.Fatal("PATCH sent")
	}
	mustJSONEq(t, r[0].Body, "body", "@minji 점심 먹고 ㄱㄱ 2시")
	if n := len(reqs(h.API, "POST", "/v1/channels/"+devID(t, h)+"/messages")); n != 0 {
		t.Fatalf("an edit never sends a new message (%d)", n)
	}
	cc := channelOf(t, h).composer
	if cc.editing != nil || cc.c.Value() != "" || strings.Contains(h.Screen(), "수정 중") {
		t.Fatal("edit finished: draft restored, label gone")
	}
	if m, ok := chatOf(h.Context()).FindMessage(myLastID(t, h)); !ok || m.Body != "@minji 점심 먹고 ㄱㄱ 2시" || m.EditedAt == nil {
		t.Fatalf("edited copy stored: %+v", m)
	}
	h.Press("up", "esc") // cancel edit restores the previous (empty) draft
	if cc.editing != nil || cc.c.Value() != "" || !cc.focused() {
		t.Fatalf("esc cancels the edit, keeps the composer: editing=%v value=%q", cc.editing, cc.c.Value())
	}
	if n := len(reqs(h.API, "PATCH", "/v1/messages/"+myLastID(t, h))); n != 1 {
		t.Fatal("cancel sends nothing")
	}
}

func TestComposerEditCancelRestoresDraft(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	h.Press("i")
	h.Type("쓰던 글")
	cc := channelOf(t, h).composer
	m, _ := chatOf(h.Context()).FindMessage(myLastID(t, h))
	cc.startEdit(m)
	if cc.c.Value() != m.Body {
		t.Fatalf("editing loads the body, got %q", cc.c.Value())
	}
	h.Press("esc")
	if cc.editing != nil || cc.c.Value() != "쓰던 글" || !cc.focused() {
		t.Fatalf("esc restores the draft, got %q", cc.c.Value())
	}
	h.Press("up") // a non-empty draft: up is a cursor move, never edit-last
	if cc.editing != nil {
		t.Fatal("edit-last only with an empty input")
	}
	h.Press("esc")
	if cc.focused() || channelOf(t, h).CapturesInput() {
		t.Fatal("esc without an edit leaves the composer")
	}
	if cc.c.Value() != "쓰던 글" {
		t.Fatal("leaving keeps the draft")
	}
}

func TestComposerTypingEmits(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	rt := &recordRT{}
	h.Context().RT = rt
	h.Press("i")
	h.Type("가")
	if len(rt.Typed) == 0 || rt.Typed[0] != devID(t, h)+"|" {
		t.Fatalf("%v", rt.Typed)
	}
	n := len(rt.Typed)
	h.Paste("나다")
	if len(rt.Typed) != n+1 {
		t.Fatalf("a paste emits typing too: %v", rt.Typed)
	}
	h.Press("ctrl+c") // clears the input: nothing to announce
	if len(rt.Typed) != n+1 {
		t.Fatalf("an empty input emits nothing: %v", rt.Typed)
	}
}

func TestComposerMultilineGrows(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	h.Press("i")
	h.Type("a")
	h.Press("shift+enter")
	h.Type("b")
	cc := channelOf(t, h).composer
	if cc.c.Value() != "a\nb" {
		t.Fatalf("value %q", cc.c.Value())
	}
	h.Screen()
	if got := cc.c.Height(channelOf(t, h).size.W, composerMaxRows(ui.DensityFull)); got != 2+2 {
		t.Fatalf("composer grows to 2 rows (+border), height %d", got)
	}
	h.Press("ctrl+j")
	h.Type("c")
	if cc.c.Value() != "a\nb\nc" {
		t.Fatalf("ctrl+j is a newline too, value %q", cc.c.Value())
	}
	h.Check()
	h.Press("enter")
	r := reqs(h.API, "POST", "/v1/channels/"+devID(t, h)+"/messages")
	if len(r) != 1 || jsonField(r[0].Body, "body") != "a\nb\nc" {
		t.Fatalf("enter sends the whole multi-line body: %+v", r)
	}
}

func TestComposerIgnoresBlank(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	h.Press("i")
	h.Type("   ")
	h.Press("shift+enter", "enter")
	if n := len(reqs(h.API, "POST", "/v1/channels/"+devID(t, h)+"/messages")); n != 0 {
		t.Fatalf("whitespace-only input is never sent (%d)", n)
	}
}

func TestComposerSanitizesInput(t *testing.T) {
	for _, in := range []string{"❄️ 첫눈", "👍🏽", "👨‍👩‍👧 가족", "🇰", "क्षि", "\x1b[31m빨강"} {
		h := devChannel(t, uitest.Size{W: 120, H: 40})
		h.Press("i")
		h.Paste(in)
		if got, want := channelOf(t, h).composer.c.Value(), textwidth.SanitizeInput(in); got != want {
			t.Fatalf("paste %q: value %q, want %q", in, got, want)
		}
		h.Check()
	}
}

func TestComposerDraftSurvivesModeSwitch(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	h.Press("i")
	h.Type("배포 몇 시")
	inst := channelOf(t, h)
	for _, sz := range []uitest.Size{{W: 120, H: 40}, {W: 120, H: 30}, {W: 80, H: 24}, {W: 50, H: 20}, {W: 200, H: 60}} {
		h.Resize(sz.W, sz.H)
		if channelOf(t, h) != inst || inst.composer.c.Value() != "배포 몇 시" {
			t.Fatalf("%v draft lost", sz)
		}
		row, col := lastLineWith(t, h.Screen(), "배포 몇 시") // the fixture message "오늘 배포 몇 시에 해요?" is above
		if c := h.Cursor(); c == nil || c.Y != row || c.X != col+textwidth.Width("배포 몇 시") {
			t.Fatalf("%v cursor %+v", sz, c)
		}
		h.Check()
	}
}

func TestComposerDraftSaved(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	h.Press("i")
	h.Type("저장할 초안")
	st, ok := channelOf(t, h).SaveState().(channelSaved)
	if !ok || st.Draft != "저장할 초안" {
		t.Fatalf("SaveState Draft: %+v", st)
	}
	v := newChannel(h.Context(), ui.Route{Kind: ui.RouteChannel, ID: devID(t, h)}).(*channelView)
	v.LoadState(st)
	if v.composer.c.Value() != "저장할 초안" {
		t.Fatalf("LoadState restores the draft, got %q", v.composer.c.Value())
	}
}

func TestComposerPlaceholder(t *testing.T) {
	for _, tc := range []struct {
		sz   uitest.Size
		want string
	}{
		{uitest.Size{W: 120, H: 40}, "› # 개발에 메시지 보내기"},
		{uitest.Size{W: 120, H: 30}, "› # 개발"},
	} {
		h := devChannel(t, tc.sz)
		mustContain(t, h.Screen(), tc.want)
	}
	h := open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: fixtures.DMMinji}, uitest.WithSeed(bootstrappedSeed))
	mustContain(t, h.Screen(), "› 민지에게 메시지 보내기")
}

func TestComposerFocusBorder(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	th := h.Context().Theme
	top := func() string {
		h.Screen()
		cc := channelOf(t, h).composer
		return strings.Split(cc.view(h.Context(), channelOf(t, h).size.W), "\n")[0]
	}
	acc := sgr(th.S.Base.Foreground(th.S.InputFocused.GetBorderTopForeground()))
	line := sgr(th.S.Base.Foreground(th.S.Input.GetBorderTopForeground()))
	if b := top(); !strings.Contains(b, line) || strings.Contains(b, acc) {
		t.Fatalf("blurred composer border uses the line color: %q", b)
	}
	h.Press("i")
	if b := top(); !strings.Contains(b, acc) {
		t.Fatalf("focused composer border uses acc: %q", b)
	}
}
