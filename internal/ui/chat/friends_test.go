package chat

import (
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/errtext"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// ---- friends test data (this file only) ----

const (
	reqInID  = "01JFREQ0000000000000000001" // 김개발 -> me (incoming)
	reqOutID = "01JFREQ0VT0000000000000001" // me -> 태양 (outgoing)
	userSun  = "01JA00000000000000000000T1" // 태양: known only from my outgoing request
)

// sunUser is the user of my outgoing request (not in the fixture seed).
func sunUser() api.UserSummary {
	return api.UserSummary{ID: userSun, Handle: "taeyang", DisplayName: "태양"}
}

// outRequest is my pending request to u.
func outRequest(id string, u api.UserSummary) api.FriendRequest {
	return api.FriendRequest{
		ID: id, From: api.UserSummary{ID: fixtures.UserSam, Handle: "sam", DisplayName: "sam"}, To: u,
		Status: "pending", CreatedAt: uitest.Start.Add(-2 * time.Hour),
	}
}

// friendsSeed is the bootstrapped fixture seed without the 준호 friendship (so
// friend.add can pick him) and with one incoming request from 김개발.
func friendsSeed(s *store.Store) {
	bootstrappedSeed(s)
	delete(s.Friends, fixtures.UserJunho)
	s.FriendReqIn = []api.FriendRequest{homeRequest(reqInID, *s.User(fixtures.UserDevKim))}
}

// friendsServer is newAPI with GET /v1/friends/requests answering by direction:
// outgoing = *out (read at request time), incoming = the seeded request.
func friendsServer(t testing.TB, out *[]api.FriendRequest) *apitest.Server {
	t.Helper()
	srv := newAPI(t)
	var mu sync.Mutex
	// same shape as fixtures.RegisterAPI's route: replaces it for this test (by direction).
	srv.Handle("GET /v1/friends/requests", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Query().Get("direction") == "outgoing" {
			writeJSON(w, http.StatusOK, map[string]any{"items": append([]api.FriendRequest{}, *out...)})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": []api.FriendRequest{homeRequest(reqInID, userBy(t, seeded(), "dev_kim"))}})
	})
	return srv
}

// openFriendsWith opens RouteFriends on friendsSeed with outgoing requests *out.
func openFriendsWith(t *testing.T, sz uitest.Size, out *[]api.FriendRequest) *uitest.Harness {
	t.Helper()
	return openWith(t, sz, ui.Route{Kind: ui.RouteFriends}, friendsServer(t, out), uitest.WithSeed(friendsSeed))
}

// openFriends opens RouteFriends with one outgoing request (to 태양).
func openFriends(t *testing.T, sz uitest.Size) *uitest.Harness {
	t.Helper()
	out := []api.FriendRequest{outRequest(reqOutID, sunUser())}
	return openFriendsWith(t, sz, &out)
}

// friendsOf is the RouteFriends instance of h.
func friendsOf(t testing.TB, h *uitest.Harness) *friendsView {
	t.Helper()
	v, ok := h.Instance(ui.Route{Kind: ui.RouteFriends}.Key()).(*friendsView)
	if !ok || v == nil {
		t.Fatalf("no friends instance (%T)", h.Instance(ui.Route{Kind: ui.RouteFriends}.Key()))
	}
	return v
}

// friendsSelect moves the cursor with j/k onto the first row whose text contains text.
func friendsSelect(t testing.TB, h *uitest.Harness, text string) {
	t.Helper()
	v := friendsOf(t, h)
	for range 20 {
		h.Press("k")
	}
	for range 20 {
		if it, ok := v.list.Selected(); ok && strings.Contains(it.Label, text) {
			return
		}
		h.Press("j")
	}
	t.Fatalf("no friends row %q:\n%s", text, paneText(t, h, layout.SlotMain))
}

// openPicker opens o (a user picker) as an overlay and returns it.
func openPicker(t testing.TB, h *uitest.Harness, o ui.Overlay) *userPicker {
	t.Helper()
	p, ok := o.(*userPicker)
	if !ok {
		t.Fatalf("not a user picker: %T", o)
	}
	h.Send(ui.OpenOverlayMsg{Overlay: p})
	if overlays(h) != 1 {
		t.Fatalf("picker not open:\n%s", h.Screen())
	}
	return p
}

// pickerTitle is the title on the top overlay's border (the first row of its box).
func pickerTitle(t testing.TB, h *uitest.Harness) string {
	t.Helper()
	scr := h.Screen()
	ov := h.Model.Frame().Overlays
	if len(ov) == 0 {
		t.Fatalf("no overlay:\n%s", scr)
	}
	return strings.Join(cropRect(scr, ov[len(ov)-1].Rect)[:1], "")
}

// overlayRect is the top overlay's box (after a fresh View).
func overlayRect(t testing.TB, h *uitest.Harness) layout.Rect {
	t.Helper()
	scr := h.Screen()
	ov := h.Model.Frame().Overlays
	if len(ov) == 0 {
		t.Fatalf("no overlay:\n%s", scr)
	}
	return ov[len(ov)-1].Rect
}

// overlayText is the text of the top overlay's box.
func overlayText(t testing.TB, h *uitest.Harness) string {
	t.Helper()
	return columnText(h.Screen(), overlayRect(t, h))
}

// overlayLineWith is lineWith inside the top overlay's box (screen coordinates).
func overlayLineWith(t testing.TB, h *uitest.Harness, s string) (row, col int) {
	t.Helper()
	r := overlayRect(t, h)
	for i, l := range cropRect(h.Screen(), r) {
		if j := strings.Index(l, s); j >= 0 {
			return r.Y + i, r.X + textwidth.Width(l[:j])
		}
	}
	t.Fatalf("%q not in the overlay:\n%s", s, overlayText(t, h))
	return -1, -1
}

// ---- tests ----

func TestFriendsGolden(t *testing.T) {
	for i, name := range []string{"friends", "incoming", "outgoing"} {
		t.Run(name, func(t *testing.T) {
			forEachSize(t, uitest.AllSizes(), openFriends, func(t *testing.T, h *uitest.Harness) {
				for range i {
					h.Press("l")
				}
				h.Golden(name)
			})
		})
	}
}

func TestFriendsTabsAndRows(t *testing.T) {
	h := openFriends(t, uitest.Size{W: 120, H: 40})
	main := paneText(t, h, layout.SlotMain)
	mustContainInOrder(t, main, "친구 2", "받은 요청 1", "보낸 요청 1", "＋ 친구 추가", "● 민지 @minji", "● 하나 @hana")
	if strings.Contains(main, "준호") {
		t.Fatalf("준호 is no friend in this seed:\n%s", main)
	}
	if v := friendsOf(t, h); v.tab != 0 {
		t.Fatalf("starts on the friends tab, got %d", v.tab)
	}
	if n := len(reqs(h.API, "GET", "/v1/friends/requests")); n != 1 || !strings.Contains(lastQuery(h.API, "/v1/friends/requests"), "direction=outgoing") {
		t.Fatalf("one outgoing fetch on entry, got %d (%q)", n, lastQuery(h.API, "/v1/friends/requests"))
	}

	h.Press("l")
	mustContain(t, paneText(t, h, layout.SlotMain), "김개발 @dev_kim · 1시간")
	h.Press("right")
	mustContain(t, paneText(t, h, layout.SlotMain), "태양 @taeyang · 2시간")
	h.Press("l") // last tab: stays
	if v := friendsOf(t, h); v.tab != 2 {
		t.Fatalf("tab %d", v.tab)
	}
	h.Press("h", "left")
	if v := friendsOf(t, h); v.tab != 0 {
		t.Fatalf("h/left go back, tab %d", v.tab)
	}

	// a click on a tab label switches too
	clickText(t, h, layout.SlotMain, "보낸 요청")
	if v := friendsOf(t, h); v.tab != 2 {
		t.Fatalf("click on 보낸 요청: tab %d", v.tab)
	}
	if title, _ := friendsOf(t, h).Title(h.Context()); title != "친구" {
		t.Fatalf("title %q", title)
	}
}

func TestFriendsEmptyTabs(t *testing.T) {
	seed := func(s *store.Store) {
		bootstrappedSeed(s)
		s.FriendReqIn = nil
	}
	out := []api.FriendRequest{}
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteFriends}, friendsServer(t, &out), uitest.WithSeed(seed))
	h.Press("l")
	mustContain(t, paneText(t, h, layout.SlotMain), "받은 친구 요청이 없어요")
	h.Press("l")
	mustContain(t, paneText(t, h, layout.SlotMain), "보낸 친구 요청이 없어요")
}

func TestFriendsActions(t *testing.T) {
	sz := uitest.Size{W: 120, H: 40}

	// incoming: enter accepts
	h := openFriends(t, sz)
	h.Press("l", "enter")
	if n := len(reqs(h.API, "POST", "/v1/friends/requests/"+reqInID+"/accept")); n != 1 {
		t.Fatalf("accept: %d", n)
	}
	s := h.Context().Store
	if _, ok := s.Friends[fixtures.UserDevKim]; !ok || len(s.FriendReqIn) != 0 {
		t.Fatalf("accepted at once: friends %v, incoming %v", s.Friends, s.FriendReqIn)
	}
	mustContain(t, paneText(t, h, layout.SlotMain), "친구 3", "받은 요청 0", "받은 친구 요청이 없어요")

	// incoming: m -> 거절
	h = openFriends(t, sz)
	h.Press("l", "m")
	if got, want := menuLabels(t, h), []string{"수락", "거절", "사용자 신고", "차단"}; !slices.Equal(got[len(got)-len(want):], want) {
		t.Fatalf("incoming menu %q, want %q", got, want)
	}
	clickOn(t, h, "거절", tea.MouseLeft)
	if n := len(reqs(h.API, "POST", "/v1/friends/requests/"+reqInID+"/decline")); n != 1 {
		t.Fatalf("decline: %d", n)
	}
	if n := len(h.Context().Store.FriendReqIn); n != 0 {
		t.Fatalf("declined at once: %d left", n)
	}

	// outgoing: m -> 요청 취소 -> DELETE, then refetched
	out := []api.FriendRequest{outRequest(reqOutID, sunUser())}
	h = openFriendsWith(t, sz, &out)
	h.Press("l", "l", "m")
	if got := menuLabels(t, h); got[len(got)-1] != "요청 취소" {
		t.Fatalf("outgoing menu %q", got)
	}
	out = nil
	before := len(reqs(h.API, "GET", "/v1/friends/requests"))
	clickOn(t, h, "요청 취소", tea.MouseLeft)
	if n := len(reqs(h.API, "DELETE", "/v1/friends/requests/"+reqOutID)); n != 1 {
		t.Fatalf("cancel: %d", n)
	}
	if n := len(reqs(h.API, "GET", "/v1/friends/requests")); n != before+1 {
		t.Fatalf("outgoing refetched after cancel (%d -> %d)", before, n)
	}
	mustContain(t, paneText(t, h, layout.SlotMain), "보낸 요청 0", "보낸 친구 요청이 없어요")

	// friends: enter on 민지 opens the DM
	h = openFriends(t, sz)
	friendsSelect(t, h, "민지")
	h.Press("enter")
	r := reqs(h.API, "POST", "/v1/dms")
	if len(r) != 1 {
		t.Fatalf("one POST /v1/dms, got %d", len(r))
	}
	mustJSONObject(t, r[0].Body, `{"users":["@minji"]}`)
	if tr := top(h); tr.Kind != ui.RouteChannel || tr.ID != fixtures.DMMinji {
		t.Fatalf("opens the DM: %+v", tr)
	}

	// friends: m -> 친구 끊기 asks, then DELETE /v1/friends/@minji
	h = openFriends(t, sz)
	friendsSelect(t, h, "민지")
	h.Press("m")
	if got, want := menuLabels(t, h), []string{"DM 보내기", "방에 초대", "친구 끊기", "사용자 신고", "차단"}; !slices.Equal(got[len(got)-len(want):], want) {
		t.Fatalf("friend menu %q, want %q", got, want)
	}
	clickOn(t, h, "친구 끊기", tea.MouseLeft)
	if overlays(h) != 1 || !strings.Contains(h.Screen(), "친구를 끊을까요?") {
		t.Fatalf("친구 끊기 asks first:\n%s", h.Screen())
	}
	h.Press("y")
	if n := len(reqs(h.API, "DELETE", "/v1/friends/@minji")); n != 1 {
		t.Fatalf("remove friend: %d", n)
	}
	if _, ok := h.Context().Store.Friends[fixtures.UserMinji]; ok {
		t.Fatal("removed at once")
	}

	// friends: 차단 asks, then PUT /v1/blocks/@hana
	h = openFriends(t, sz)
	friendsSelect(t, h, "하나")
	h.Press("m")
	clickOn(t, h, "차단", tea.MouseLeft)
	if overlays(h) != 1 || !strings.Contains(h.Screen(), "차단할까요?") {
		t.Fatalf("차단 asks first:\n%s", h.Screen())
	}
	h.Press("y")
	if n := len(reqs(h.API, "PUT", "/v1/blocks/@hana")); n != 1 {
		t.Fatalf("block: %d", n)
	}

	// friends: 사용자 신고 morphs into the report dialog
	h = openFriends(t, sz)
	friendsSelect(t, h, "민지")
	h.Press("m")
	clickOn(t, h, "사용자 신고", tea.MouseLeft)
	if overlays(h) != 1 || !strings.Contains(h.Screen(), "신고 · 민지") {
		t.Fatalf("사용자 신고 → report dialog:\n%s", h.Screen())
	}
}

func TestFriendsInviteToSpace(t *testing.T) {
	srv := friendsServer(t, &[]api.FriendRequest{})
	nova := fixtures.SpaceNova
	srv.JSON("POST /v1/spaces/"+nova+"/direct-invites", http.StatusCreated, directInviteAnswer(t, nova))
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteFriends}, srv, uitest.WithSeed(friendsSeed))
	friendsSelect(t, h, "하나")
	h.Press("m")
	clickOn(t, h, "방에 초대", tea.MouseLeft)
	if got := menuLabels(t, h); !slices.Contains(got, "노바 개발방") {
		t.Fatalf("space submenu %q", got)
	}
	clickOn(t, h, "노바 개발방", tea.MouseLeft)
	r := reqs(srv, "POST", "/v1/spaces/"+nova+"/direct-invites")
	if len(r) != 1 {
		t.Fatalf("one direct invite, got %d", len(r))
	}
	mustJSONObject(t, r[0].Body, `{"user":"@hana"}`)
	mustContain(t, toastText(t, h), "하나님을 노바 개발방에 초대했어요")

	// no space: no 방에 초대
	seed := func(s *store.Store) {
		friendsSeed(s)
		s.SpaceOrder = nil
		s.Spaces = map[string]*api.SpaceDetail{}
	}
	h = openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteFriends}, friendsServer(t, &[]api.FriendRequest{}), uitest.WithSeed(seed))
	friendsSelect(t, h, "하나")
	h.Press("m")
	if got := menuLabels(t, h); slices.Contains(got, "방에 초대") {
		t.Fatalf("no space, no invite item: %q", got)
	}
}

func TestFriendsAddRowOpensPicker(t *testing.T) {
	h := openFriends(t, uitest.Size{W: 120, H: 40})
	friendsSelect(t, h, "친구 추가")
	h.Press("enter")
	if title := pickerTitle(t, h); !strings.Contains(title, "친구 추가") {
		t.Fatalf("picker title row %q", title)
	}
}

func TestFriendAddSearchDebounce(t *testing.T) {
	h := openFriends(t, uitest.Size{W: 120, H: 40})
	h.Send(ui.RunCommandMsg{ID: "friend.add"})
	h.Type("jun")
	if len(reqs(h.API, "GET", "/v1/users/search")) != 0 {
		t.Fatal("debounced")
	}
	h.Advance(300 * time.Millisecond)
	if r := reqs(h.API, "GET", "/v1/users/search"); len(r) != 1 || !strings.Contains(r[0].Query, "q=jun") {
		t.Fatalf("%v", r)
	}
	mustContain(t, h.Screen(), "준호 @junho")
	h.Press("enter", "enter") // pick 준호, submit
	r := reqs(h.API, "POST", "/v1/friends/requests")
	if len(r) != 1 {
		t.Fatalf("one friend request, got %d", len(r))
	}
	mustJSONEq(t, r[0].Body, "user", "@junho")
	if overlays(h) != 0 {
		t.Fatal("the picker closes on success")
	}
	mustContain(t, toastText(t, h), "친구 요청을 보냈어요")
}

func TestFriendAddDebounceRestarts(t *testing.T) {
	h := openFriends(t, uitest.Size{W: 120, H: 40})
	h.Send(ui.RunCommandMsg{ID: "friend.add"})
	h.Type("j") // one rune: no search at all
	h.Advance(time.Second)
	if n := len(reqs(h.API, "GET", "/v1/users/search")); n != 0 {
		t.Fatalf("1 rune searches nothing (%d)", n)
	}
	h.Type("u")
	h.Advance(200 * time.Millisecond)
	h.Type("n") // restarts the 300ms
	h.Advance(200 * time.Millisecond)
	if n := len(reqs(h.API, "GET", "/v1/users/search")); n != 0 {
		t.Fatalf("still typing (%d)", n)
	}
	h.Advance(100 * time.Millisecond)
	if r := reqs(h.API, "GET", "/v1/users/search"); len(r) != 1 || !strings.Contains(r[0].Query, "q=jun") || !strings.Contains(r[0].Query, "limit=10") {
		t.Fatalf("one search for the last query: %v", r)
	}
}

func TestFriendAddExcludes(t *testing.T) {
	h := openFriends(t, uitest.Size{W: 120, H: 40})
	h.Send(ui.RunCommandMsg{ID: "friend.add"})
	h.Type("mi") // 민지 is a friend already
	h.Advance(300 * time.Millisecond)
	box := overlayText(t, h)
	if strings.Contains(box, "@minji") {
		t.Fatalf("friends are excluded:\n%s", box)
	}
	mustContain(t, box, "찾는 사람이 없어요")
	h.Press("ctrl+c") // clears the query
	h.Type("sa")      // me
	h.Advance(300 * time.Millisecond)
	if box := overlayText(t, h); strings.Contains(box, "@sam") {
		t.Fatalf("I am excluded:\n%s", box)
	}
}

func TestFriendAddErrors(t *testing.T) {
	// 409 already_exists: the error shows in the picker, which stays open
	out := []api.FriendRequest{}
	srv := friendsServer(t, &out)
	srv.Error("POST /v1/friends/requests", http.StatusConflict, "already_exists")
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteFriends}, srv, uitest.WithSeed(friendsSeed))
	p := openPicker(t, h, newFriendAdd(h.Context(), "jun"))
	h.Advance(300 * time.Millisecond)
	h.Press("enter", "enter")
	if overlays(h) != 1 {
		t.Fatal("the picker stays open on an error")
	}
	mustContain(t, h.Screen(), errtext.Describe(i18n.KoT(), &api.Error{Code: "already_exists"}))
	if len(p.chips) != 1 || p.chips[0].Handle != "junho" {
		t.Fatalf("the pick is kept: %+v", p.chips)
	}

	// 200 = auto-accepted (김개발 had asked me): toast, friends at once
	srv = friendsServer(t, &out)
	st := seeded()
	dk := userBy(t, st, "dev_kim")
	srv.JSON("POST /v1/friends/requests", http.StatusOK, map[string]any{
		"request": api.FriendRequest{ID: reqInID, From: dk, To: st.Me.UserSummary, Status: "accepted", CreatedAt: uitest.Start},
		"friend":  api.Friend{User: dk, Since: uitest.Start, Presence: api.PresenceAway},
	})
	h = openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteFriends}, srv, uitest.WithSeed(friendsSeed))
	h.Send(ui.RunCommandMsg{ID: "friend.add", Args: "dev"})
	h.Advance(300 * time.Millisecond)
	h.Press("enter", "enter")
	mustJSONEq(t, reqs(srv, "POST", "/v1/friends/requests")[0].Body, "user", "@dev_kim")
	mustContain(t, toastText(t, h), "이제 친구예요")
	s := h.Context().Store
	if _, ok := s.Friends[fixtures.UserDevKim]; !ok || len(s.FriendReqIn) != 0 {
		t.Fatalf("auto-accepted: friends %v incoming %v", s.Friends, s.FriendReqIn)
	}
}

func TestFriendsLiveEvents(t *testing.T) {
	out := []api.FriendRequest{outRequest(reqOutID, sunUser())}
	h := openFriendsWith(t, uitest.Size{W: 120, H: 40}, &out)
	mustContain(t, paneText(t, h, layout.SlotMain), "받은 요청 1", "보낸 요청 1")

	st := seeded()
	h.Event("friend.request_received", realtime.FriendRequestEvent{Request: homeRequest("01JFREQ0000000000000000009", userBy(t, st, "junho"))})
	mustContain(t, paneText(t, h, layout.SlotMain), "받은 요청 2")

	before := len(reqs(h.API, "GET", "/v1/friends/requests"))
	out = nil
	acc := outRequest(reqOutID, sunUser())
	acc.Status = "accepted"
	h.Event("friend.request_accepted", realtime.FriendRequestEvent{Request: acc})
	if n := len(reqs(h.API, "GET", "/v1/friends/requests")); n != before+1 || !strings.Contains(lastQuery(h.API, "/v1/friends/requests"), "direction=outgoing") {
		t.Fatalf("outgoing refetched (%d -> %d)", before, n)
	}
	mustContain(t, paneText(t, h, layout.SlotMain), "보낸 요청 0")

	// a request I sent from the picker shows up in 보낸 요청
	out = []api.FriendRequest{outRequest("01JFREQ0VT0000000000000002", userBy(t, st, "junho"))}
	h.Send(ui.RunCommandMsg{ID: "friend.add", Args: "jun"})
	h.Advance(300 * time.Millisecond)
	h.Press("enter", "enter")
	mustContain(t, paneText(t, h, layout.SlotMain), "보낸 요청 1")
}

func TestFriendsHidesBlocked(t *testing.T) {
	h := openFriends(t, uitest.Size{W: 120, H: 40})
	s := h.Context().Store
	s.Blocks[fixtures.UserDevKim] = h.Context().Now()
	h.Event("block.updated", realtime.BlockUpdated{UserID: fixtures.UserDevKim, Blocked: true})
	h.Press("l")
	if main := paneText(t, h, layout.SlotMain); strings.Contains(main, "김개발") || !strings.Contains(main, "받은 요청 0") {
		t.Fatalf("a blocked requester is hidden:\n%s", main)
	}
}

func TestFriendsKeepSelectionPerTab(t *testing.T) {
	h := openFriends(t, uitest.Size{W: 120, H: 40})
	friendsSelect(t, h, "하나")
	h.Press("l", "h")
	if it, _ := friendsOf(t, h).list.Selected(); !strings.Contains(it.Label, "하나") {
		t.Fatalf("selection per tab kept, got %+v", it)
	}
}

func TestFriendsPersistent(t *testing.T) {
	h := openFriends(t, uitest.Size{W: 120, H: 40})
	h.Press("l")
	st := friendsOf(t, h).SaveState()
	ctx := h.Context()
	v := newFriends(ctx, ui.Route{Kind: ui.RouteFriends}).(*friendsView)
	v.LoadState(st)
	if v.tab != 1 {
		t.Fatalf("restored tab %d", v.tab)
	}
	v.LoadState("junk")
}

func TestUserPickerGolden(t *testing.T) {
	forEachSize(t, overlaySizes, openHome, func(t *testing.T, h *uitest.Harness) {
		h.Send(ui.RunCommandMsg{ID: "friend.add"})
		h.Golden("user_picker")
	})
}

func TestUserPickerChipsGolden(t *testing.T) {
	forEachSize(t, overlaySizes, openHome, func(t *testing.T, h *uitest.Harness) {
		h.Send(ui.OpenOverlayMsg{Overlay: newUserPicker(groupSpec(nil))})
		h.Press("down", "enter", "down", "enter")
		h.Type("ㅎ")
		h.Golden("user_picker_chips")
	})
}

// groupSpec is a multi-pick spec over the friends (Max 3, name field) recording picks.
func groupSpec(got *[]string) pickerSpec {
	return pickerSpec{
		Title: "그룹 DM", Submit: "만들기", Max: 3, NameField: true,
		Candidates: func(ctx *ui.Context) []api.UserSummary {
			var out []api.UserSummary
			for _, f := range ctx.Store.Friends {
				out = append(out, f.User)
			}
			return out
		},
		OnPick: func(_ *ui.Context, users []api.UserSummary, name string) (string, tea.Cmd) {
			if got != nil {
				for _, u := range users {
					*got = append(*got, u.Handle)
				}
				*got = append(*got, "name="+name)
			}
			return "", nil
		},
	}
}

func TestUserPickerMultiPick(t *testing.T) {
	var got []string
	h := openHome(t, uitest.Size{W: 120, H: 40})
	p := openPicker(t, h, newUserPicker(groupSpec(&got)))
	mustContain(t, h.Screen(), "민지 @minji", "준호 @junho", "하나 @hana") // candidates without a query
	if p.cur != -1 {
		t.Fatalf("no candidate selected before ↓: %d", p.cur)
	}
	h.Press("down", "enter") // first candidate
	if len(p.chips) != 1 {
		t.Fatalf("chips %+v", p.chips)
	}
	first := p.chips[0].Handle
	if slices.ContainsFunc(p.cands, func(u api.UserSummary) bool { return u.Handle == first }) {
		t.Fatalf("a picked user leaves the candidates: %+v", p.cands)
	}

	h.Type("ㅎㄴ") // chosung of 하나
	if len(p.cands) != 1 || p.cands[0].Handle != "hana" || p.cur != 0 {
		t.Fatalf("chosung match: %+v cur=%d", p.cands, p.cur)
	}
	h.Press("enter")
	if len(p.chips) != 2 || p.query.Value() != "" {
		t.Fatalf("second pick clears the query: %+v %q", p.chips, p.query.Value())
	}

	h.Press("backspace") // empty query: removes the last chip
	if len(p.chips) != 1 {
		t.Fatalf("backspace removes the last chip: %+v", p.chips)
	}

	h.Press("tab")
	h.Type("점심팟")
	if p.name.Value() != "점심팟" || p.query.Value() != "" {
		t.Fatalf("tab moves to the name field: name %q query %q", p.name.Value(), p.query.Value())
	}
	row, col := overlayLineWith(t, h, "점심팟")
	if c := h.Cursor(); c == nil || c.Y != row || c.X != col+textwidth.Width("점심팟") {
		t.Fatalf("cursor after the name: %+v (row %d col %d)", c, row, col)
	}
	h.Press("enter")
	if want := []string{first, "name=점심팟"}; !slices.Equal(got, want) {
		t.Fatalf("picked %q, want %q", got, want)
	}
	if overlays(h) != 0 {
		t.Fatal("tag \"\" closes at once")
	}
}

func TestUserPickerMax(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	spec := groupSpec(nil)
	spec.Max = 1
	p := openPicker(t, h, newUserPicker(spec))
	h.Press("down", "enter", "down", "enter")
	if len(p.chips) != 1 {
		t.Fatalf("max 1: %+v", p.chips)
	}
	mustContain(t, h.Screen(), "최대 1명까지 고를 수 있어요")
}

func TestUserPickerQueryCursorAndPaste(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	p := openPicker(t, h, newFriendAdd(h.Context(), ""))
	h.Paste("준\x1b[31m호\n")
	if v := p.query.Value(); v != "준호 " {
		t.Fatalf("pasted query is sanitized to one line: %q", v)
	}
	row, col := overlayLineWith(t, h, "준호")
	if c := h.Cursor(); c == nil || c.Y != row || c.X != col+textwidth.Width("준호 ") {
		t.Fatalf("cursor after the query: %+v (row %d col %d)", c, row, col)
	}
	h.Press("esc")
	if overlays(h) != 0 {
		t.Fatal("esc closes")
	}
}

func TestUserPickerFromPaletteArgs(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	h.Send(ui.RunCommandMsg{ID: "friend.add", Args: "jun"})
	mustContain(t, h.Screen(), "jun")
	h.Advance(300 * time.Millisecond)
	if n := len(reqs(h.API, "GET", "/v1/users/search")); n != 1 {
		t.Fatalf("the first query is searched too (%d)", n)
	}
}

func TestFriendsPaletteCommand(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	h.Send(ui.RunCommandMsg{ID: "friends"})
	if got := top(h); got.Kind != ui.RouteFriends {
		t.Fatalf("friends command opened %+v", got)
	}
}

func TestOpenDMCmdGroup(t *testing.T) {
	h := openFriends(t, uitest.Size{W: 120, H: 40}) // the friends route lands it
	h.Send(runCmdMsg(openDMCmd(h.Context(), []api.UserRef{"@minji", "@hana"}, "점심팟")))
	r := reqs(h.API, "POST", "/v1/dms")
	if len(r) != 1 {
		t.Fatalf("one POST /v1/dms, got %d", len(r))
	}
	mustJSONObject(t, r[0].Body, `{"users":["@minji","@hana"],"name":"점심팟"}`)
	tr := top(h)
	if tr.Kind != ui.RouteChannel || h.Context().Store.Channels[tr.ID] == nil {
		t.Fatalf("opens the new group DM (observed): %+v", tr)
	}
}

// hostileFriends is friendsSeed + fixtures.Hostile with the hostile users as a
// friend, an incoming and an outgoing request.
func hostileFriends(t *testing.T, sz uitest.Size) *uitest.Harness {
	t.Helper()
	hs := seeded()
	fixtures.Hostile(hs)
	var out []api.FriendRequest
	for _, u := range hs.Users {
		if strings.HasPrefix(u.Handle, "hostile_") || u.Handle == "ctrl_user" {
			out = append(out, outRequest("01JFREQ0VTH000000000000001", *u))
		}
	}
	seed := func(s *store.Store) {
		friendsSeed(s)
		fixtures.Hostile(s)
		for _, u := range s.Users {
			if strings.HasPrefix(u.Handle, "hostile_") || u.Handle == "ctrl_user" {
				s.Friends[u.ID] = api.Friend{User: *u, Presence: api.PresenceOnline}
				s.Presence[u.ID] = store.PresenceEntry{Status: api.PresenceOnline}
				s.FriendReqIn = append(s.FriendReqIn, homeRequest("01JFREQHOSTILE000000000001", *u))
			}
		}
	}
	return openWith(t, sz, ui.Route{Kind: ui.RouteFriends}, friendsServer(t, &out), uitest.WithSeed(seed))
}

func TestFriendsHostileIntegrity(t *testing.T) {
	for i, name := range []string{"friends", "incoming", "outgoing"} {
		t.Run(name, func(t *testing.T) {
			forEachSize(t, uitest.AllSizes(), hostileFriends, func(t *testing.T, h *uitest.Harness) {
				for range i {
					h.Press("l")
				}
				h.Check()
			})
		})
	}
}

// A friend's suspension (or its end) arrives as user.updated (api.md 3.9): the
// open friends list drops the stale status message without a refetch, and shows
// the restored one after unsuspend.
func TestFriendsUserUpdatedOnSuspension(t *testing.T) {
	h := openFriends(t, uitest.Size{W: 120, H: 40})
	st := seeded()
	minji := userBy(t, st, "minji")
	emoji := "🎧"
	withStatus := minji
	withStatus.Status = &api.UserStatus{Emoji: &emoji, Text: "회의 중"}
	h.Event("user.updated", realtime.UserUpdated{User: withStatus})
	mustContain(t, paneText(t, h, layout.SlotMain), "회의 중")

	suspended := minji
	suspended.Suspended = true
	h.Event("user.updated", realtime.UserUpdated{User: suspended})
	main := paneText(t, h, layout.SlotMain)
	if strings.Contains(main, "회의 중") {
		t.Fatalf("a suspended friend's status message is hidden:\n%s", main)
	}
	mustContain(t, main, textwidth.Sanitize(minji.DisplayName))

	h.Event("user.updated", realtime.UserUpdated{User: withStatus})
	mustContain(t, paneText(t, h, layout.SlotMain), "회의 중")
}

// C1: a RouteFriends Anchor wins over the saved tab of an evicted instance.
func TestFriendsAnchorWinsOverSavedTab(t *testing.T) {
	h := openFriends(t, uitest.Size{W: 120, H: 40})
	v := newFriends(h.Context(), ui.Route{Kind: ui.RouteFriends, Anchor: ui.FriendsIncoming}).(*friendsView)
	v.LoadState(friendsSaved{Tab: tabOutgoing})
	if v.tab != tabIncoming {
		t.Fatalf("anchored tab %d, want 받은 요청", v.tab)
	}
	v = newFriends(h.Context(), ui.Route{Kind: ui.RouteFriends}).(*friendsView)
	v.LoadState(friendsSaved{Tab: tabOutgoing})
	if v.tab != tabOutgoing {
		t.Fatalf("unanchored tab %d, want the saved one", v.tab)
	}
}
