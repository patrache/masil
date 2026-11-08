package chat

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// ---- space / invite test data (this file only) ----

const (
	dinvBoard = "01JK0000000000000000DINV01" // direct invite to 보드게임 모임 from 민지
	dinvLunch = "01JK0000000000000000DINV02" // direct invite to 점심 탐험대 from 준호
)

// directInvite is a pending direct invite of me to space from inviter, created ago before the test clock.
func directInvite(id string, space api.SpacePreview, inviter api.UserSummary, ago time.Duration) api.DirectInvite {
	return api.DirectInvite{
		ID: id, Space: space, Inviter: inviter, Invitee: seeded().Me.UserSummary,
		Status: "pending", CreatedAt: uitest.Start.Add(-ago),
	}
}

// lunchPreview is the preview of 점심 탐험대 (a space I am not in).
func lunchPreview() api.SpacePreview {
	return api.SpacePreview{ID: "01JB00000000000000LUNCH001", Name: "점심 탐험대", Description: "회사 근처 맛집", IconEmoji: ptrTo("🍚"), MemberCount: 3}
}

// withDirectInvites is the bootstrapped seed plus the two pending direct invites.
func withDirectInvites(s *store.Store) {
	bootstrappedSeed(s)
	s.DirectInvites = []api.DirectInvite{
		directInvite(dinvBoard, joinedSpace().SpacePreview, *s.User(fixtures.UserMinji), 3*time.Hour),
		directInvite(dinvLunch, lunchPreview(), *s.User(fixtures.UserJunho), 26*time.Hour),
	}
}

// openInvite opens RouteInvite for code on srv.
func openInvite(t *testing.T, sz uitest.Size, code string, srv *apitest.Server) *uitest.Harness {
	t.Helper()
	return openWith(t, sz, ui.Route{Kind: ui.RouteInvite, ID: code}, srv, uitest.WithSeed(bootstrappedSeed))
}

// openInviteLink opens the AB12CD34 link preview (노바 개발방, invited by 민지).
func openInviteLink(t *testing.T, sz uitest.Size) *uitest.Harness {
	t.Helper()
	return openInvite(t, sz, testInviteCode, newAPI(t))
}

// openDirectList opens the list of pending direct invites.
func openDirectList(t *testing.T, sz uitest.Size) *uitest.Harness {
	t.Helper()
	return open(t, sz, ui.Route{Kind: ui.RouteInvite, Arg: "direct"}, uitest.WithSeed(withDirectInvites))
}

// openDirectOne opens the direct invite to 보드게임 모임.
func openDirectOne(t *testing.T, sz uitest.Size) *uitest.Harness {
	t.Helper()
	return open(t, sz, ui.Route{Kind: ui.RouteInvite, Arg: "direct", ID: dinvBoard}, uitest.WithSeed(withDirectInvites))
}

// inviteOf is the RouteInvite instance of r.
func inviteOf(t testing.TB, h *uitest.Harness, r ui.Route) *inviteView {
	t.Helper()
	v, ok := h.Instance(r.Key()).(*inviteView)
	if !ok || v == nil {
		t.Fatalf("no invite instance for %q (%T)", r.Key(), h.Instance(r.Key()))
	}
	return v
}

// hasMain reports whether this size lays out the screens (not the too-small notice).
func hasMain(h *uitest.Harness) bool {
	h.Screen()
	l := h.Model.Frame().Layout
	if l.Mode == layout.ModeTooSmall {
		return false
	}
	_, ok := l.Pane(layout.SlotMain)
	return ok
}

// inviteInvalidBody is a 410 invite_invalid envelope with details.reason.
func inviteInvalidBody(reason string) json.RawMessage {
	return json.RawMessage(`{"error":{"code":"invite_invalid","message":"invite invalid","details":{"reason":"` + reason + `"}}}`)
}

// ---- parseInviteCode / inviteErrorText ----

func TestParseInviteCode(t *testing.T) {
	for in, want := range map[string]string{
		"AB12CD34":                 "AB12CD34",
		" masil join AB12CD34 ":    "AB12CD34",
		"masil   join\tAbC12345":   "AbC12345",
		"\tab12cd34\n":             "ab12cd34",
		"masil join AB12CD34 more": "",
		"abc":                      "",
		"AB12CD345":                "",
		"AB12-D34":                 "",
		"가나다라마바사아":                 "",
		"join AB12CD34":            "",
		"":                         "",
	} {
		got, ok := parseInviteCode(in)
		if ok != (want != "") || got != want {
			t.Errorf("parseInviteCode(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func TestInviteErrorText(t *testing.T) {
	details := func(code, reason string) error {
		return &api.Error{Code: code, Details: json.RawMessage(`{"reason":"` + reason + `"}`)}
	}
	for _, tc := range []struct {
		err  error
		want string
	}{
		{details("invite_invalid", "expired"), "만료된 초대예요"},
		{details("invite_invalid", "exhausted"), "사용 횟수가 다 찬 초대예요"},
		{details("invite_invalid", "revoked"), "취소된 초대예요"},
		{details("invite_invalid", "other"), api.Describe(&api.Error{Code: "invite_invalid"})},
		{&api.Error{Code: "invite_invalid"}, api.Describe(&api.Error{Code: "invite_invalid"})},
		{&api.Error{Code: "not_found"}, "없는 초대 코드예요"},
		{&api.Error{Code: "banned_from_space"}, "이 방에 들어갈 수 없어요"},
		{&api.Error{Code: "internal_error"}, api.Describe(&api.Error{Code: "internal_error"})},
	} {
		if got := inviteErrorText(tc.err); got != tc.want {
			t.Errorf("inviteErrorText(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

// ---- space.create / space.join ----

func TestSpaceCreate(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	h.Send(ui.RunCommandMsg{ID: "space.create"})
	if overlays(h) != 1 {
		t.Fatalf("space.create did not open:\n%s", h.Screen())
	}
	mustContain(t, overlayText(t, h), "이름", "설명", "아이콘")
	h.Press("enter")
	mustContain(t, overlayText(t, h), "이름을 입력해 주세요")
	if n := len(reqs(h.API, "POST", "/v1/spaces")); n != 0 {
		t.Fatalf("an empty name is not sent (%d)", n)
	}
	h.Type("점심 모임")
	h.Press("tab")
	h.Type("밥 먹는 방")
	h.Press("enter")
	r := reqs(h.API, "POST", "/v1/spaces")
	if len(r) != 1 {
		t.Fatalf("one POST /v1/spaces, got %d", len(r))
	}
	mustJSONObject(t, r[0].Body, `{"name":"점심 모임","description":"밥 먹는 방"}`)
	s := h.Context().Store
	if n := len(s.SpaceOrder); n == 0 || s.SpaceOrder[n-1] != newSpaceID {
		t.Fatalf("the new space is adopted last: %q", s.SpaceOrder)
	}
	if s.Channels[newSpaceChannelID] == nil {
		t.Fatal("the new space's channels are observed")
	}
	if tr := top(h); tr.Kind != ui.RouteChannel || tr.ID != newSpaceChannelID {
		t.Fatalf("opens the default channel: %+v", tr)
	}
	if got := chatOf(h.Context()).SelectedSpace(); got != newSpaceID {
		t.Fatalf("selected space %q", got)
	}
	if overlays(h) != 0 {
		t.Fatal("the form closes on success")
	}
}

func TestSpaceCreateIcon(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	h.Send(ui.RunCommandMsg{ID: "space.create"})
	h.Type("게임방")
	h.Press("tab", "tab", "right", "right", "right", "enter") // 없음 → 🚀 → 🍚 → 🎮
	r := reqs(h.API, "POST", "/v1/spaces")
	if len(r) != 1 {
		t.Fatalf("one POST /v1/spaces, got %d", len(r))
	}
	mustJSONObject(t, r[0].Body, `{"name":"게임방","icon_emoji":"🎮"}`)
}

func TestSpaceCreateError(t *testing.T) {
	srv := newAPI(t)
	srv.Error("POST /v1/spaces", http.StatusUnprocessableEntity, "limit_exceeded") // same shape: replaces the echo for this test
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteHome}, srv)
	h.Send(ui.RunCommandMsg{ID: "space.create"})
	h.Type("하나 더")
	h.Press("enter")
	if overlays(h) != 1 {
		t.Fatal("the form stays open on an error")
	}
	mustContain(t, overlayText(t, h), api.Describe(&api.Error{Code: "limit_exceeded"}))
	if top(h).Kind != ui.RouteHome {
		t.Fatalf("no navigation on an error: %+v", top(h))
	}
}

func TestSpaceJoinParsesCode(t *testing.T) {
	for _, in := range []string{"AB12CD34", " masil join AB12CD34 "} {
		h := openHome(t, uitest.Size{W: 120, H: 40})
		h.Send(ui.RunCommandMsg{ID: "space.join"})
		if overlays(h) != 1 {
			t.Fatalf("space.join did not open:\n%s", h.Screen())
		}
		before := len(h.API.Requests())
		h.Paste(in)
		h.Press("enter")
		if tr := top(h); tr.Kind != ui.RouteInvite || tr.ID != testInviteCode || tr.Arg != "" {
			t.Fatalf("%q: top %+v", in, tr)
		}
		if overlays(h) != 0 {
			t.Fatalf("%q: the form closes", in)
		}
		for _, r := range h.API.Requests()[before:] {
			if r.Method != "GET" || r.Path != "/v1/invites/"+testInviteCode {
				t.Fatalf("%q: joining only previews (GET), got %s %s", in, r.Method, r.Path)
			}
		}
	}

	h := openHome(t, uitest.Size{W: 120, H: 40})
	h.Send(ui.RunCommandMsg{ID: "space.join"})
	h.Type("abc")
	h.Press("enter")
	if overlays(h) != 1 || top(h).Kind != ui.RouteHome {
		t.Fatal("a bad code stays in the form")
	}
	mustContain(t, overlayText(t, h), "초대 코드는 8자예요")
}

func TestSpaceJoinArgsPrefill(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	h.Send(ui.RunCommandMsg{ID: "space.join", Args: "masil join AB12CD34"})
	h.Press("enter")
	if tr := top(h); tr.Kind != ui.RouteInvite || tr.ID != testInviteCode {
		t.Fatalf("args prefill the code: %+v", tr)
	}
}

func TestSpaceAddRegistered(t *testing.T) {
	for _, id := range []string{"chat.space_create", "chat.space_join", "chat.space_add"} {
		if _, ok := ui.Registered().Overlays[id]; !ok {
			t.Fatalf("%s not registered", id)
		}
	}
	// the rail "+" menu morphs into the real dialogs
	for _, tc := range []struct {
		keys  []string
		title string
	}{{[]string{"enter"}, "방 만들기"}, {[]string{"j", "enter"}, "방 참여"}} {
		h := devChannel(t, uitest.Size{W: 120, H: 40})
		h.Send(ui.OpenOverlayMsg{ID: "chat.space_add"})
		mustContain(t, overlayText(t, h), "방 만들기", "초대 코드로 참여")
		h.Press(tc.keys...)
		if overlays(h) != 1 {
			t.Fatalf("%v: %d overlays", tc.keys, overlays(h))
		}
		mustContain(t, pickerTitle(t, h), tc.title)
	}
}

func TestSpacePaletteCommands(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	h.Send(ui.RunCommandMsg{ID: "invites.inbox"})
	if tr := top(h); tr.Key() != (ui.Route{Kind: ui.RouteInvite, Arg: "direct"}).Key() {
		t.Fatalf("invites.inbox opens the received invites: %+v", tr)
	}
}

// ---- RouteInvite: link preview ----

func TestInviteGolden(t *testing.T) {
	for _, tc := range []struct {
		name string
		mk   func(t *testing.T, sz uitest.Size) *uitest.Harness
		want []string
	}{
		{"invite_link", openInviteLink, []string{"노바 개발방", "초대한 사람"}},
		{"invite_direct_list", openDirectList, []string{"보드게임 모임"}},
		{"invite_direct_one", openDirectOne, []string{"보드게임 모임", "초대한 사람"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			forEachSize(t, uitest.AllSizes(), tc.mk, func(t *testing.T, h *uitest.Harness) {
				if hasMain(h) {
					mustContain(t, h.Screen(), tc.want...)
				}
				h.Golden(tc.name)
			})
		})
	}
}

func TestInviteLinkCard(t *testing.T) {
	h := openInviteLink(t, uitest.Size{W: 120, H: 40})
	main := paneText(t, h, layout.SlotMain)
	mustContainInOrder(t, main, "🚀 노바 개발방", "노바 팀 개발 이야기", "멤버 5명", "초대한 사람: 민지", "11월 12일 11:05까지", "enter 참여하기 · m 더보기 · esc 닫기")
	if r := reqs(h.API, "GET", "/v1/invites/"+testInviteCode); len(r) != 1 {
		t.Fatalf("one preview fetch, got %d", len(r))
	}

	// no expiry
	srv := newAPI(t)
	p := invitePreview(testInviteCode)
	p.ExpiresAt = nil
	srv.JSON("GET /v1/invites/"+testInviteCode, http.StatusOK, map[string]any{"invite": p})
	h = openInvite(t, uitest.Size{W: 120, H: 40}, testInviteCode, srv)
	mustContain(t, paneText(t, h, layout.SlotMain), "만료 없음")
}

func TestInviteCardCentered(t *testing.T) {
	for _, sz := range []uitest.Size{{W: 200, H: 60}, {W: 120, H: 40}, {W: 80, H: 24}, {W: 50, H: 20}} {
		h := openInviteLink(t, sz)
		p, ok := h.Model.Frame().Layout.Pane(layout.SlotMain)
		if !ok {
			t.Fatalf("%v: no main", sz)
		}
		in := p.Rect
		if p.Bordered {
			in = in.Inner()
		}
		rows := cropRect(h.Screen(), in)
		first, last := -1, -1
		for i, l := range rows {
			if strings.TrimSpace(l) != "" {
				if first < 0 {
					first = i
				}
				last = i
			}
		}
		if first < 0 {
			t.Fatalf("%v: empty main", sz)
		}
		above, below := first, len(rows)-1-last
		if d := above - below; d < -1 || d > 1 {
			t.Fatalf("%v: card not vertically centered (%d above, %d below):\n%s", sz, above, below, strings.Join(rows, "\n"))
		}
		minLeft, maxRight := in.W, 0
		for _, l := range rows[first : last+1] {
			body := strings.TrimSpace(l)
			if body == "" {
				continue
			}
			left := textwidth.Width(l) - textwidth.Width(strings.TrimLeft(l, " "))
			minLeft = min(minLeft, left)
			maxRight = max(maxRight, left+textwidth.Width(body))
		}
		if minLeft < 1 || maxRight > in.W-minLeft+1 {
			t.Fatalf("%v: card not horizontally centered (left %d, right edge %d of %d):\n%s", sz, minLeft, maxRight, in.W, strings.Join(rows, "\n"))
		}
	}
}

func TestInviteAccept(t *testing.T) {
	srv := newAPI(t)
	h := openInvite(t, uitest.Size{W: 120, H: 40}, joinInviteCode, srv)
	mustContain(t, paneText(t, h, layout.SlotMain), "보드게임 모임", "enter 참여하기")
	h.Press("enter")
	r := reqs(srv, "POST", "/v1/invites/"+joinInviteCode+"/accept")
	if len(r) != 1 {
		t.Fatalf("one accept, got %d", len(r))
	}
	s := h.Context().Store
	if s.Spaces[joinSpaceID] == nil || s.SpaceOrder[len(s.SpaceOrder)-1] != joinSpaceID || s.Channels[joinChannelID] == nil {
		t.Fatalf("the joined space is adopted: %q", s.SpaceOrder)
	}
	if tr := top(h); tr.Kind != ui.RouteChannel || tr.ID != joinChannelID {
		t.Fatalf("opens the default channel: %+v", tr)
	}
	if got := chatOf(h.Context()).SelectedSpace(); got != joinSpaceID {
		t.Fatalf("selected space %q", got)
	}
}

func TestInviteAcceptOnce(t *testing.T) {
	srv := newAPI(t)
	h := openInvite(t, uitest.Size{W: 120, H: 40}, joinInviteCode, srv)
	v := inviteOf(t, h, ui.Route{Kind: ui.RouteInvite, ID: joinInviteCode})
	v.busy = tagInviteAcceptPrefix + joinInviteCode // an accept in flight ignores more enters
	h.Press("enter")
	if n := len(reqs(srv, "POST", "/v1/invites/"+joinInviteCode+"/accept")); n != 0 {
		t.Fatalf("enter while busy sent %d accepts", n)
	}
}

func TestInviteAcceptFails(t *testing.T) {
	srv := newAPI(t)
	srv.JSON("POST /v1/invites/"+joinInviteCode+"/accept", http.StatusGone, inviteInvalidBody("exhausted"))
	h := openInvite(t, uitest.Size{W: 120, H: 40}, joinInviteCode, srv)
	h.Press("enter")
	mustContain(t, paneText(t, h, layout.SlotMain), "사용 횟수가 다 찬 초대예요")
	if top(h).Kind != ui.RouteInvite {
		t.Fatalf("stays on the invite: %+v", top(h))
	}

	// already a member (409): opens the space instead
	srv = newAPI(t)
	srv.Error("POST /v1/invites/"+testInviteCode+"/accept", http.StatusConflict, "already_member")
	h = openInvite(t, uitest.Size{W: 120, H: 40}, testInviteCode, srv)
	h.Press("enter")
	if tr := top(h); tr.Kind != ui.RouteChannel || tr.ID != fixtures.ChGeneral {
		t.Fatalf("409 already_member opens the space: %+v", tr)
	}

	// a transient failure: stays, the app toasts, enter tries again
	srv = newAPI(t)
	srv.Error("POST /v1/invites/"+joinInviteCode+"/accept", http.StatusServiceUnavailable, "service_unavailable")
	h = openInvite(t, uitest.Size{W: 120, H: 40}, joinInviteCode, srv)
	h.Press("enter")
	if top(h).Kind != ui.RouteInvite {
		t.Fatalf("stays on the invite: %+v", top(h))
	}
	mustContain(t, paneText(t, h, layout.SlotMain), "보드게임 모임", "enter 참여하기")
	if h.Model.Frame().Toast == nil {
		t.Fatalf("a transient error is toasted:\n%s", h.Screen())
	}
	h.Press("enter")
	if n := len(reqs(srv, "POST", "/v1/invites/"+joinInviteCode+"/accept")); n < 2 {
		t.Fatalf("enter retries: %d", n)
	}
}

func TestInviteAlreadyMember(t *testing.T) {
	srv := newAPI(t)
	p := invitePreview(testInviteCode)
	p.AlreadyMember = true
	srv.JSON("GET /v1/invites/"+testInviteCode, http.StatusOK, map[string]any{"invite": p})
	h := openInvite(t, uitest.Size{W: 120, H: 40}, testInviteCode, srv)
	mustContain(t, paneText(t, h, layout.SlotMain), "이미 참여한 방이에요 · enter 열기")
	h.Press("enter")
	if n := len(reqs(srv, "POST", "/v1/invites/"+testInviteCode+"/accept")); n != 0 {
		t.Fatalf("already a member: no accept (%d)", n)
	}
	if tr := top(h); tr.Kind != ui.RouteChannel || tr.ID != fixtures.ChGeneral {
		t.Fatalf("opens the space's default channel: %+v", tr)
	}
}

func TestInviteAlreadyMemberUnknownSpace(t *testing.T) {
	// a member of a space the Store does not hold yet: it is fetched, then opened
	srv := newAPI(t)
	p := invitePreview(joinInviteCode)
	p.AlreadyMember = true
	srv.JSON("GET /v1/invites/"+joinInviteCode, http.StatusOK, map[string]any{"invite": p})
	srv.JSON("GET /v1/spaces/"+joinSpaceID, http.StatusOK, map[string]any{"space": joinedSpace()})
	h := openInvite(t, uitest.Size{W: 120, H: 40}, joinInviteCode, srv)
	h.Press("enter")
	if n := len(reqs(srv, "POST", "/v1/invites/"+joinInviteCode+"/accept")); n != 0 {
		t.Fatalf("already a member: no accept (%d)", n)
	}
	if tr := top(h); tr.Kind != ui.RouteChannel || tr.ID != joinChannelID {
		t.Fatalf("opens the fetched space: %+v", tr)
	}
}

func TestInviteInvalid(t *testing.T) {
	for reason, text := range map[string]string{"expired": "만료된 초대예요", "exhausted": "사용 횟수가 다 찬 초대예요", "revoked": "취소된 초대예요"} {
		srv := newAPI(t)
		srv.JSON("GET /v1/invites/"+testInviteCode, http.StatusGone, inviteInvalidBody(reason))
		h := openInvite(t, uitest.Size{W: 120, H: 40}, testInviteCode, srv)
		main := paneText(t, h, layout.SlotMain)
		mustContain(t, main, text)
		if strings.Contains(main, "참여하기") {
			t.Fatalf("%s: no join on an invalid invite:\n%s", reason, main)
		}
		h.Press("enter")
		if n := len(reqs(srv, "POST", "/v1/invites/"+testInviteCode+"/accept")); n != 0 {
			t.Fatalf("%s: enter sent an accept", reason)
		}
		if h.Model.Frame().Toast != nil {
			t.Fatalf("%s: the error is shown in place, not toasted", reason)
		}
	}
	h := openInvite(t, uitest.Size{W: 120, H: 40}, "ZZ99ZZ99", newAPI(t))
	mustContain(t, paneText(t, h, layout.SlotMain), "없는 초대 코드예요")
}

func TestInviteRetry(t *testing.T) {
	srv := newAPI(t)
	srv.Error("GET /v1/invites/"+testInviteCode, http.StatusServiceUnavailable, "service_unavailable")
	h := openInvite(t, uitest.Size{W: 120, H: 40}, testInviteCode, srv)
	mustContain(t, paneText(t, h, layout.SlotMain), api.Describe(&api.Error{Code: "service_unavailable"}), "r 다시 시도")
	srv.JSON("GET /v1/invites/"+testInviteCode, http.StatusOK, map[string]any{"invite": invitePreview(testInviteCode)})
	h.Press("r")
	mustContain(t, paneText(t, h, layout.SlotMain), "노바 개발방", "enter 참여하기")
}

func TestInviteReportSpace(t *testing.T) {
	h := openInviteLink(t, uitest.Size{W: 120, H: 40})
	h.Press("m")
	if overlays(h) != 1 {
		t.Fatalf("m opens the menu:\n%s", h.Screen())
	}
	if got := menuLabels(t, h); !slices.Contains(got, "방 신고") {
		t.Fatalf("menu %q", got)
	}
	clickOn(t, h, "방 신고", tea.MouseLeft)
	if overlays(h) != 1 || !strings.Contains(h.Screen(), "신고 · 노바 개발방") {
		t.Fatalf("방 신고 → report dialog:\n%s", h.Screen())
	}
	h.Press("enter", "enter") // reason, then the note
	r := reqs(h.API, "POST", "/v1/reports")
	if len(r) != 1 {
		t.Fatalf("one report, got %d", len(r))
	}
	mustJSONEq(t, r[0].Body, "target_type", "space")
	mustJSONEq(t, r[0].Body, "target_id", fixtures.SpaceNova)
}

func TestInviteHostileIntegrity(t *testing.T) {
	s := seeded()
	fixtures.Hostile(s)
	var inviter api.UserSummary
	for _, u := range s.Users {
		if strings.HasPrefix(u.Handle, "hostile_") {
			inviter = *u
		}
	}
	if inviter.ID == "" {
		t.Fatal("no hostile user")
	}
	space := s.Spaces[fixtures.SpaceHostile].SpacePreview
	seed := func(s *store.Store) {
		bootstrappedSeed(s)
		fixtures.Hostile(s)
		s.DirectInvites = []api.DirectInvite{directInvite(dinvBoard, space, inviter, time.Minute)}
	}
	link := func(t *testing.T, sz uitest.Size) *uitest.Harness {
		srv := newAPI(t)
		exp := uitest.Start.Add(400 * 24 * time.Hour)
		srv.JSON("GET /v1/invites/"+testInviteCode, http.StatusOK, map[string]any{"invite": api.InvitePreview{
			Code: testInviteCode, Space: space, CreatedBy: inviter, ExpiresAt: &exp,
		}})
		return openWith(t, sz, ui.Route{Kind: ui.RouteInvite, ID: testInviteCode}, srv, uitest.WithSeed(seed))
	}
	direct := func(id string) func(t *testing.T, sz uitest.Size) *uitest.Harness {
		return func(t *testing.T, sz uitest.Size) *uitest.Harness {
			return open(t, sz, ui.Route{Kind: ui.RouteInvite, Arg: "direct", ID: id}, uitest.WithSeed(seed))
		}
	}
	for name, mk := range map[string]func(t *testing.T, sz uitest.Size) *uitest.Harness{
		"link": link, "list": direct(""), "one": direct(dinvBoard),
	} {
		t.Run(name, func(t *testing.T) {
			forEachSize(t, uitest.AllSizes(), mk, func(t *testing.T, h *uitest.Harness) { h.Check() })
		})
	}
}

// ---- RouteInvite: direct invites ----

func TestDirectInviteList(t *testing.T) {
	h := openDirectList(t, uitest.Size{W: 120, H: 40})
	main := paneText(t, h, layout.SlotMain)
	mustContainInOrder(t, main, "보드게임 모임", "민지", "3시간", "점심 탐험대", "준호", "어제")
	h.Press("enter")
	want := ui.Route{Kind: ui.RouteInvite, Arg: "direct", ID: dinvBoard}
	if tr := top(h); tr.Key() != want.Key() {
		t.Fatalf("enter opens the invite: %+v", tr)
	}
	if st := h.Nav().Tabs[h.Nav().Active].Stack; len(st) < 2 || st[len(st)-2].Key() != (ui.Route{Kind: ui.RouteInvite, Arg: "direct"}).Key() {
		t.Fatalf("pushed over the list: %+v", st)
	}

	// empty
	h = open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteInvite, Arg: "direct"}, uitest.WithSeed(bootstrappedSeed))
	mustContain(t, paneText(t, h, layout.SlotMain), "받은 방 초대가 없어요")

	// a removed invite leaves the list at once
	h = openDirectList(t, uitest.Size{W: 120, H: 40})
	h.Event("space.invite_removed", map[string]any{"invite_id": dinvBoard, "reason": "canceled"})
	if main := paneText(t, h, layout.SlotMain); strings.Contains(main, "보드게임 모임") || !strings.Contains(main, "점심 탐험대") {
		t.Fatalf("removed invite still listed:\n%s", main)
	}
}

func TestDirectInviteAcceptDecline(t *testing.T) {
	h := openDirectOne(t, uitest.Size{W: 120, H: 40})
	mustContain(t, paneText(t, h, layout.SlotMain), "보드게임 모임", "초대한 사람: 민지", "enter 수락 · d 거절 · m 더보기")
	h.Press("enter")
	if r := reqs(h.API, "POST", "/v1/direct-invites/"+dinvBoard+"/accept"); len(r) != 1 {
		t.Fatalf("one accept, got %d", len(r))
	}
	s := h.Context().Store
	if s.Spaces[joinSpaceID] == nil {
		t.Fatal("the joined space is adopted")
	}
	if slices.ContainsFunc(s.DirectInvites, func(in api.DirectInvite) bool { return in.ID == dinvBoard }) {
		t.Fatal("an accepted invite leaves the pending list")
	}
	if tr := top(h); tr.Kind != ui.RouteChannel || tr.ID != joinChannelID {
		t.Fatalf("opens the default channel: %+v", tr)
	}

	h = openDirectOne(t, uitest.Size{W: 120, H: 40})
	h.Press("d")
	if r := reqs(h.API, "POST", "/v1/direct-invites/"+dinvBoard+"/decline"); len(r) != 1 {
		t.Fatalf("one decline, got %d", len(r))
	}
	if tr := top(h); tr.Kind != ui.RouteHome {
		t.Fatalf("decline goes home: %+v", tr)
	}
	if slices.ContainsFunc(h.Context().Store.DirectInvites, func(in api.DirectInvite) bool { return in.ID == dinvBoard }) {
		t.Fatal("a declined invite leaves the pending list")
	}

	// opened from the received list, declining returns to it
	h = openDirectList(t, uitest.Size{W: 120, H: 40})
	h.Press("enter", "d")
	if tr := top(h); tr.Key() != (ui.Route{Kind: ui.RouteInvite, Arg: "direct"}).Key() {
		t.Fatalf("decline returns to the list: %+v", tr)
	}
	if main := paneText(t, h, layout.SlotMain); strings.Contains(main, "보드게임 모임") || !strings.Contains(main, "점심 탐험대") {
		t.Fatalf("the declined invite left the list:\n%s", main)
	}

	h = openDirectOne(t, uitest.Size{W: 120, H: 40})
	h.Event("space.invite_removed", map[string]any{"invite_id": dinvBoard, "reason": "canceled"})
	main := paneText(t, h, layout.SlotMain)
	mustContain(t, main, "더 이상 유효하지 않은 초대예요")
	h.Press("enter", "d")
	if n := len(reqs(h.API, "POST", "/v1/direct-invites/"+dinvBoard+"/accept")) + len(reqs(h.API, "POST", "/v1/direct-invites/"+dinvBoard+"/decline")); n != 0 {
		t.Fatalf("a removed invite takes no action (%d)", n)
	}
}

func TestDirectInviteMenu(t *testing.T) {
	h := openDirectOne(t, uitest.Size{W: 120, H: 40})
	h.Press("m")
	if got, want := menuLabels(t, h), []string{"수락", "거절", "방 신고"}; !slices.Equal(got[len(got)-len(want):], want) {
		t.Fatalf("menu %q", got)
	}
	clickOn(t, h, "거절", tea.MouseLeft)
	if r := reqs(h.API, "POST", "/v1/direct-invites/"+dinvBoard+"/decline"); len(r) != 1 {
		t.Fatalf("menu 거절 declines, got %d", len(r))
	}
}

func TestSpaceDialogsGolden(t *testing.T) {
	for _, id := range []string{"chat.space_create", "chat.space_join", "chat.space_add"} {
		t.Run(strings.TrimPrefix(id, "chat."), func(t *testing.T) {
			forEachSize(t, overlaySizes, openHome, func(t *testing.T, h *uitest.Harness) {
				h.Send(ui.OpenOverlayMsg{ID: id})
				h.Golden(strings.TrimPrefix(id, "chat."))
			})
		})
	}
}

// C3 (v0.1.1): the card's "멤버 N명" is the preview's only until I join; then it
// is the joined space's count, kept live by the space.member_* events.
func TestInviteCardMemberCountAfterJoin(t *testing.T) {
	srv := newAPI(t)
	joined := joinedSpace()
	joined.MemberCount = 6 // me included
	srv.JSON("POST /v1/invites/"+joinInviteCode+"/accept", http.StatusOK, map[string]any{"space": joined})
	h := openInvite(t, uitest.Size{W: 120, H: 40}, joinInviteCode, srv)
	r := ui.Route{Kind: ui.RouteInvite, ID: joinInviteCode}
	v := inviteOf(t, h, r)
	card := func() string { return ansi.Strip(v.View(h.Context(), false)) }
	mustContain(t, card(), "멤버 5명")
	h.Press("enter")
	if h.Context().Store.Spaces[joinSpaceID] == nil {
		t.Fatal("the joined space is adopted")
	}
	if c := card(); !strings.Contains(c, "멤버 6명") {
		t.Fatalf("after joining the card keeps the preview count:\n%s", c)
	}
	newcomer := api.UserSummary{ID: "01JA0000000000000000NEW001", Handle: "newbie", DisplayName: "새친구"}
	h.Event("space.member_joined", realtime.SpaceMemberEvent{SpaceID: joinSpaceID, Member: api.SpaceMember{User: newcomer, Role: api.RoleMember, JoinedAt: uitest.Start}})
	if c := card(); !strings.Contains(c, "멤버 7명") {
		t.Fatalf("space.member_joined does not reach the card:\n%s", c)
	}
	h.Event("space.member_left", realtime.SpaceMemberLeft{SpaceID: joinSpaceID, UserID: newcomer.ID})
	if c := card(); !strings.Contains(c, "멤버 6명") {
		t.Fatalf("space.member_left does not reach the card:\n%s", c)
	}
}
