package chat

import (
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// homeOf is the RouteHome instance of h.
func homeOf(t testing.TB, h *uitest.Harness) *homeView {
	t.Helper()
	v, ok := h.Instance(ui.Route{Kind: ui.RouteHome}.Key()).(*homeView)
	if !ok || v == nil {
		t.Fatalf("no home instance (%T)", h.Instance(ui.Route{Kind: ui.RouteHome}.Key()))
	}
	return v
}

// homeSelect moves the home cursor with j/k onto the first row whose label contains text.
func homeSelect(t testing.TB, h *uitest.Harness, text string) {
	t.Helper()
	v := homeOf(t, h)
	for range 30 {
		h.Press("k")
	}
	for range 30 {
		if it, ok := v.list.Selected(); ok && strings.Contains(it.Label, text) {
			return
		}
		h.Press("j")
	}
	t.Fatalf("no home row %q:\n%s", text, paneText(t, h, layout.SlotMain))
}

// homeInvite is a pending direct invite to a space.
func homeInvite() api.DirectInvite {
	return api.DirectInvite{
		ID:      "01JHINVITE0000000000000001",
		Space:   api.SpacePreview{ID: "01JBINVSPACE00000000000001", Name: "초대방", MemberCount: 3},
		Inviter: api.UserSummary{ID: fixtures.UserHana, Handle: "hana", DisplayName: "하나"},
		Invitee: api.UserSummary{ID: fixtures.UserSam, Handle: "sam", DisplayName: "sam"},
		Status:  "pending", CreatedAt: uitest.Start.Add(-time.Hour),
	}
}

// homeRequest is a pending incoming friend request from u.
func homeRequest(id string, u api.UserSummary) api.FriendRequest {
	return api.FriendRequest{
		ID: id, From: u, To: api.UserSummary{ID: fixtures.UserSam, Handle: "sam", DisplayName: "sam"},
		Status: "pending", CreatedAt: uitest.Start.Add(-time.Hour),
	}
}

func TestHomeGolden(t *testing.T) {
	forEachSize(t, uitest.AllSizes(), openHome, func(t *testing.T, h *uitest.Harness) {
		// too_small draws only its notice (no route), the golden still pins it.
		if h.Context().Mode != layout.ModeTooSmall {
			mustContain(t, h.Screen(), "─ DM", "민지")
		}
		h.Golden("home")
	})
}

func TestHomeRows(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	main := paneText(t, h, layout.SlotMain)
	mustContainInOrder(t, main, "피드 보기", "친구 목록", "─ DM", "● 민지", "○ 준호", "─ 접속 중인 친구", "● 민지", "● 하나")
	if strings.Contains(main, "받은 방 초대") || strings.Contains(main, "친구 요청") {
		t.Fatalf("no invites / requests seeded:\n%s", main)
	}
	if strings.Contains(main[strings.Index(main, "─ 접속 중인 친구"):], "준호") {
		t.Fatalf("offline friends are not listed as online:\n%s", main)
	}
	if title, _ := homeOf(t, h).Title(h.Context()); title != "홈" {
		t.Fatalf("title %q", title)
	}
}

func TestHomeFriendStatus(t *testing.T) {
	seed := func(s *store.Store) {
		fixtures.Seed(s)
		f := s.Friends[fixtures.UserHana]
		emoji := "🎧"
		f.User.Status = &api.UserStatus{Emoji: &emoji, Text: "듣는 중…"}
		s.Friends[fixtures.UserHana] = f
		s.ObserveUser(f.User)
		s.Presence[fixtures.UserHana] = store.PresenceEntry{Status: api.PresenceAway}
	}
	h := open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteHome}, uitest.WithSeed(seed))
	mustContain(t, paneText(t, h, layout.SlotMain), "◐ 하나 · 🎧 듣는 중…")
}

func TestHomeInvitesAndRequests(t *testing.T) {
	seed := func(s *store.Store) {
		fixtures.Seed(s)
		s.DirectInvites = []api.DirectInvite{homeInvite()}
		s.FriendReqIn = []api.FriendRequest{homeRequest("01JFREQ0000000000000000001", userBy(t, s, "dev_kim"))}
	}
	h := open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteHome}, uitest.WithSeed(seed))
	mustContainInOrder(t, paneText(t, h, layout.SlotMain), "받은 방 초대 1", "친구 요청 1", "피드 보기", "친구 목록", "─ DM")

	homeSelect(t, h, "받은 방 초대 1")
	h.Press("enter")
	if got := top(h); got.Key() != (ui.Route{Kind: ui.RouteInvite, Arg: "direct"}).Key() {
		t.Fatalf("invites row opened %+v", got)
	}

	h = open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteHome}, uitest.WithSeed(seed))
	homeSelect(t, h, "친구 요청 1")
	h.Press("enter")
	if got := top(h); got.Key() != (ui.Route{Kind: ui.RouteFriends}).Key() {
		t.Fatalf("requests row opened %+v", got)
	}

	h = open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteHome}, uitest.WithSeed(seed))
	homeSelect(t, h, "친구 목록")
	h.Press("enter")
	if got := top(h); got.Key() != (ui.Route{Kind: ui.RouteFriends}).Key() {
		t.Fatalf("friends row opened %+v", got)
	}
}

func TestHomeOpenDMAndFeed(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	homeSelect(t, h, "민지")
	if it, _ := homeOf(t, h).list.Selected(); it.Route.Kind != ui.RouteChannel {
		t.Fatalf("first 민지 row must be the DM, got %+v", it)
	}
	h.Press("enter")
	if got := top(h); got.Key() != (ui.Route{Kind: ui.RouteChannel, ID: fixtures.DMMinji}).Key() {
		t.Fatalf("DM row opened %+v", got)
	}

	h = openHome(t, uitest.Size{W: 120, H: 40})
	homeSelect(t, h, "피드 보기")
	h.Press("enter")
	if nav := h.Nav(); nav.Front != app.FrontFeed || len(nav.Feed) == 0 || nav.Feed[0].Key() != (ui.Route{Kind: ui.RouteFeed, Arg: "following"}).Key() {
		t.Fatalf("feed row: front=%v feed=%+v", nav.Front, nav.Feed)
	}
}

func TestHomeOnlineFriendOpensDM(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	homeSelect(t, h, "하나") // 하나 has no DM: the friend row opens the friends route
	h.Press("enter")
	if got := top(h); got.Kind != ui.RouteFriends {
		t.Fatalf("friend without DM opened %+v", got)
	}

	h = openHome(t, uitest.Size{W: 120, H: 40})
	v := homeOf(t, h)
	v.list.Select(homeFriendPrefix + fixtures.UserMinji)
	h.Press("enter")
	if got := top(h); got.Key() != (ui.Route{Kind: ui.RouteChannel, ID: fixtures.DMMinji}).Key() {
		t.Fatalf("friend with a DM opened %+v", got)
	}
}

func TestHomeSeparatorsNotSelectable(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	v := homeOf(t, h)
	for range 30 {
		h.Press("k")
	}
	for range 12 {
		it, ok := v.list.Selected()
		if !ok {
			t.Fatal("nothing selected")
		}
		if !it.selectable() {
			t.Fatalf("cursor on separator %+v", it)
		}
		h.Press("j")
	}
}

func TestHomeClickOpens(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	clickText(t, h, layout.SlotMain, "준호")
	if top(h).Kind != ui.RouteHome {
		t.Fatalf("first click only selects, got %+v", top(h))
	}
	if it, _ := homeOf(t, h).list.Selected(); !strings.Contains(it.Label, "준호") {
		t.Fatalf("click selected %+v", it)
	}
	clickText(t, h, layout.SlotMain, "준호")
	if got := top(h); got.Key() != (ui.Route{Kind: ui.RouteChannel, ID: fixtures.DMJunho}).Key() {
		t.Fatalf("second click opened %+v", got)
	}
}

func TestHomeEmptyGolden(t *testing.T) {
	forEachSize(t, uitest.Required, func(t *testing.T, sz uitest.Size) *uitest.Harness {
		return open(t, sz, ui.Route{Kind: ui.RouteHome}, uitest.WithSeed(func(*store.Store) {}))
	}, func(t *testing.T, h *uitest.Harness) {
		mustContain(t, h.Screen(), "아직 대화가 없어요")
		h.Golden("home_empty")
	})
}

func TestHomeLiveUpdates(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	if strings.Contains(paneText(t, h, layout.SlotMain), "친구 요청") {
		t.Fatal("no request yet")
	}
	h.Event("friend.request_received", realtime.FriendRequestEvent{Request: homeRequest("01JFREQ0000000000000000002", userBy(t, h.Context().Store, "dev_kim"))})
	mustContainInOrder(t, paneText(t, h, layout.SlotMain), "친구 요청 1", "피드 보기")
	if _, b := homeOf(t, h).Title(h.Context()); b.Count != 1 {
		t.Fatalf("title badge %+v", b)
	}
}

func TestHomeKeepsSelectionAcrossUpdates(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	homeSelect(t, h, "준호")
	h.Event("friend.request_received", realtime.FriendRequestEvent{Request: homeRequest("01JFREQ0000000000000000003", userBy(t, h.Context().Store, "dev_kim"))})
	h.Screen()
	if it, _ := homeOf(t, h).list.Selected(); !strings.Contains(it.Label, "준호") {
		t.Fatalf("selection moved to %+v", it)
	}
}

func TestHomeHidesBlockedDM(t *testing.T) {
	seed := func(s *store.Store) {
		fixtures.Seed(s)
		s.Presence[fixtures.UserJunho] = store.PresenceEntry{Status: api.PresenceOnline} // listed as an online friend too
	}
	h := open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteHome}, uitest.WithSeed(seed))
	s := h.Context().Store
	main := paneText(t, h, layout.SlotMain)
	mustContainInOrder(t, main, "─ DM", "준호", "─ 접속 중인 친구", "준호")

	s.Blocks[fixtures.UserJunho] = h.Context().Now()
	h.Event("block.updated", realtime.BlockUpdated{UserID: fixtures.UserJunho, Blocked: true})
	main = paneText(t, h, layout.SlotMain)
	if strings.Contains(main, "준호") || !strings.Contains(main, "민지") {
		t.Fatalf("blocked peer must leave the DM and online friend rows at once:\n%s", main)
	}

	delete(s.Blocks, fixtures.UserJunho)
	h.Event("block.updated", realtime.BlockUpdated{UserID: fixtures.UserJunho, Blocked: false})
	mustContainInOrder(t, paneText(t, h, layout.SlotMain), "─ DM", "준호")
}

func TestHomePersistent(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	homeSelect(t, h, "준호")
	st := homeOf(t, h).SaveState()

	ctx := h.Context()
	v := newHome(ctx, ui.Route{Kind: ui.RouteHome}).(*homeView)
	v.LoadState(st)
	v.Resize(ctx, ui.Size{W: 60, H: 20})
	v.View(ctx, true)
	if it, _ := v.list.Selected(); !strings.Contains(it.Label, "준호") {
		t.Fatalf("restored selection %+v", it)
	}
	v.LoadState("junk") // unknown state is ignored
}

// hostileHome is the fixture seed + fixtures.Hostile, with a hostile online friend
// (long status), a hostile friend request and a direct invite to the hostile space.
func hostileHome(t *testing.T, sz uitest.Size) *uitest.Harness {
	t.Helper()
	seed := func(s *store.Store) {
		bootstrappedSeed(s)
		fixtures.Hostile(s)
		for _, u := range s.Users {
			if strings.HasPrefix(u.Handle, "hostile_") {
				s.Friends[u.ID] = api.Friend{User: *u, Presence: api.PresenceOnline}
				s.Presence[u.ID] = store.PresenceEntry{Status: api.PresenceOnline}
				s.FriendReqIn = append(s.FriendReqIn, homeRequest("01JFREQHOSTILE000000000001", *u))
			}
			if u.Handle == "ctrl_user" {
				s.Friends[u.ID] = api.Friend{User: *u, Presence: api.PresenceOnline}
			}
		}
		in := homeInvite()
		in.Space = s.Spaces[fixtures.SpaceHostile].SpacePreview
		s.DirectInvites = append(s.DirectInvites, in)
	}
	return open(t, sz, ui.Route{Kind: ui.RouteHome}, uitest.WithSeed(seed))
}

func TestHomeHostileIntegrity(t *testing.T) {
	forEachSize(t, uitest.AllSizes(), hostileHome, func(t *testing.T, h *uitest.Harness) { h.Check() })
}
