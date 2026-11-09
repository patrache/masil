package chat

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

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
	// C1 (v0.1.1): the requests row opens the friends screen on 받은 요청
	if v := friendsOf(t, h); v.tab != tabIncoming {
		t.Fatalf("requests row opened the friends screen on tab %d, want 받은 요청", v.tab)
	}
	// the cached friends screen switched back to 친구 by hand, then 친구 목록 /
	// 친구 요청 from home again land on their own tab
	h.Press("h")
	h.Send(ui.BackMsg{})
	homeSelect(t, h, "친구 목록")
	h.Press("enter")
	if v := friendsOf(t, h); v.tab != tabFriends {
		t.Fatalf("친구 목록 opened tab %d", v.tab)
	}
	h.Send(ui.BackMsg{})
	homeSelect(t, h, "친구 요청 1")
	h.Press("enter")
	if v := friendsOf(t, h); v.tab != tabIncoming {
		t.Fatalf("친구 요청 on a cached friends screen opened tab %d", v.tab)
	}
	if !strings.Contains(h.Screen(), "dev_kim") {
		t.Fatalf("받은 요청 tab must list the request:\n%s", h.Screen())
	}
	// the same Anchor twice in a row: 친구 요청 → h (친구) → back → 친구 요청
	// again lands on 받은 요청, though the cached route already says "incoming"
	h.Press("h")
	if v := friendsOf(t, h); v.tab != tabFriends {
		t.Fatalf("h moved to tab %d", v.tab)
	}
	h.Send(ui.BackMsg{})
	homeSelect(t, h, "친구 요청 1")
	h.Press("enter")
	if v := friendsOf(t, h); v.tab != tabIncoming {
		t.Fatalf("친구 요청 opened twice with the same Anchor stays on tab %d", v.tab)
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
		return open(t, sz, ui.Route{Kind: ui.RouteHome}, uitest.WithSeed(func(s *store.Store) { s.Sync.Bootstrapped = true }))
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

// ---- joined spaces (home-channels) ----

// homeTextChan is a text channel of space for the home tests.
func homeTextChan(id, space, name string, pos int, my api.ChannelMyState) api.Channel {
	if my.Notify == "" {
		my.Notify = "all"
	}
	return api.Channel{ID: id, Kind: api.ChannelText, SpaceID: ptrTo(space), Name: ptrTo(name), Position: pos,
		MemberCount: 3, CreatedAt: uitest.Start.Add(-24 * time.Hour), My: my}
}

// homeChanID is the n-th test channel id.
func homeChanID(n int) string { return fmt.Sprintf("01JBH0ME%018d", n) }

// homeView renders a fresh home on h's context at w x hgt (styled lines: the app's
// compositor re-encodes SGR, the component output does not).
func homeViewLines(t *testing.T, h *uitest.Harness, w, hgt int) []string {
	t.Helper()
	ctx := h.Context()
	v := newHome(ctx, ui.Route{Kind: ui.RouteHome}).(*homeView)
	v.Resize(ctx, ui.Size{W: w, H: hgt})
	return strings.Split(v.View(ctx, false), "\n")
}

// homeRowEnds fails unless the main-pane row containing text ends with suffix.
func homeRowEnds(t *testing.T, main, text, suffix string) {
	t.Helper()
	for _, row := range strings.Split(main, "\n") {
		if strings.Contains(row, text) {
			if !strings.HasSuffix(strings.TrimRight(row, " "), suffix) {
				t.Fatalf("row %q must end with %q", row, suffix)
			}
			return
		}
	}
	t.Fatalf("no row %q:\n%s", text, main)
}

func TestHomeSpaceSection(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	main := paneText(t, h, layout.SlotMain)
	// after the entry rows, before the DMs, in the sidebar's channel order
	mustContainInOrder(t, main, "피드 보기", "친구 목록", "─ 노바 개발방", "# 일반", "# 잡담", "# 개발", "# 배포알림", "─ DM", "● 민지", "─ 접속 중인 친구")
	homeRowEnds(t, main, "# 일반", "3")
	for _, name := range []string{"# 잡담", "# 개발", "# 배포알림"} {
		homeRowEnds(t, main, name, name) // read: no badge
	}
	// the space label is no row the cursor stops on
	v := homeOf(t, h)
	i := v.index(homeSpacePrefix + fixtures.SpaceNova)
	if i < 0 || v.items[i].selectable() {
		t.Fatalf("space label row %d must exist and not be selectable", i)
	}
}

func TestHomeSpaceOrderFollowsRail(t *testing.T) {
	board := "01JBH0MESPACE0000000000001"
	seed := func(s *store.Store) {
		bootstrappedSeed(s)
		sd := api.SpaceDetail{Space: api.Space{SpacePreview: api.SpacePreview{ID: board, Name: "보드게임 모임", MemberCount: 3}, MyRole: api.RoleMember}}
		s.Spaces[board] = &sd
		s.SpaceOrder = []string{board, fixtures.SpaceNova} // the rail's order, not the id order
		// positions out of id order: the sidebar sorts by position
		s.ObserveChannel(homeTextChan(homeChanID(1), board, "셋째", 2, api.ChannelMyState{}))
		s.ObserveChannel(homeTextChan(homeChanID(2), board, "첫째", 0, api.ChannelMyState{}))
		s.ObserveChannel(homeTextChan(homeChanID(3), board, "둘째", 1, api.ChannelMyState{}))
	}
	h := open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteHome}, uitest.WithSeed(seed))
	mustContainInOrder(t, paneText(t, h, layout.SlotMain),
		"─ 보드게임 모임", "# 첫째", "# 둘째", "# 셋째", "─ 노바 개발방", "# 일반", "# 배포알림", "─ DM")
}

func TestHomeChannelBadges(t *testing.T) {
	h := open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteHome}, uitest.WithSeed(bootstrappedSeed))
	s := h.Context().Store
	th := h.Context().Theme

	if r := rowWith(t, homeViewLines(t, h, 60, 30), "# 일반"); !strings.Contains(r, th.S.Badge.Render(" 3 ")) || !strings.Contains(r, sgr(th.S.Bold)) {
		t.Fatalf("unread row: sideBadge + bold: %q", r)
	}

	s.Channels[fixtures.ChDeploy].My.MentionCount = 1
	if r := rowWith(t, homeViewLines(t, h, 60, 30), "# 배포알림"); !strings.Contains(r, th.S.BadgeMention.Render(" 1 ")) {
		t.Fatalf("mention badge missing: %q", r)
	}

	chat := s.Channels[fixtures.ChChat]
	chat.My.Muted = true
	chat.My.UnreadCount = 5
	r := rowWith(t, homeViewLines(t, h, 60, 30), "# 잡담")
	if !strings.Contains(r, sgr(th.S.Dim)) || strings.Contains(ansi.Strip(r), "5") {
		t.Fatalf("muted channel: dim, no unread badge: %q", r)
	}
	chat.My.MentionCount = 2
	if r := rowWith(t, homeViewLines(t, h, 60, 30), "# 잡담"); !strings.Contains(r, th.S.BadgeMention.Render(" 2 ")) {
		t.Fatalf("muted channel still shows mentions: %q", r)
	}
}

func TestHomePrivateChannelLock(t *testing.T) {
	seed := func(s *store.Store) {
		bootstrappedSeed(s)
		ch := homeTextChan(homeChanID(1), fixtures.SpaceNova, "비밀", 9, api.ChannelMyState{})
		ch.IsPrivate = true
		s.ObserveChannel(ch)
	}
	h := open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteHome}, uitest.WithSeed(seed))
	main := paneText(t, h, layout.SlotMain)
	mustContainInOrder(t, main, "─ 노바 개발방", "# 배포알림", "🔒비밀", "─ DM")
	if strings.Contains(main, "# 비밀") {
		t.Fatalf("a private channel reads 🔒, like the sidebar:\n%s", main)
	}
}

func TestHomeChannelOpens(t *testing.T) {
	for _, sz := range []uitest.Size{{W: 80, H: 30}, {W: 40, H: 14}, {W: 120, H: 30}, {W: 200, H: 60}} {
		t.Run(fmt.Sprintf("%dx%d", sz.W, sz.H), func(t *testing.T) {
			h := open(t, sz, ui.Route{Kind: ui.RouteHome}, uitest.WithSeed(bootstrappedSeed))
			home := mainKey(h)
			homeSelect(t, h, "# 개발")
			h.Press("enter")
			dev := ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChDev}
			if got := top(h); got.Key() != dev.Key() || h.Nav().InDM {
				t.Fatalf("channel row opened %+v (in DM %v)", got, h.Nav().InDM)
			}
			if mainKey(h) != dev.Key() {
				t.Fatalf("main shows %s", mainKey(h))
			}
			nav := h.Nav()
			if nav.Tabs[nav.Active].Stack[0].Key() != dev.Key() {
				t.Fatalf("the channel is a tab root: %+v", nav.Tabs[nav.Active].Stack)
			}
			if c := chatOf(h.Context()); c == nil || c.SelectedSpace() != fixtures.SpaceNova {
				t.Fatal("opening a space channel selects its space (as the sidebar does)")
			}
			h.Press("ctrl+o") // recorded in history: back to the home
			if mainKey(h) != home {
				t.Fatalf("ctrl+o: main %s, want home %s", mainKey(h), home)
			}
			h.Press("alt+right")
			if mainKey(h) != dev.Key() {
				t.Fatalf("alt+right: main %s, want %s", mainKey(h), dev.Key())
			}
		})
	}
}

func TestHomeChannelClickOpens(t *testing.T) {
	h := open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteHome}, uitest.WithSeed(bootstrappedSeed))
	clickText(t, h, layout.SlotMain, "# 배포알림")
	if top(h).Kind != ui.RouteHome {
		t.Fatalf("first click only selects, got %+v", top(h))
	}
	clickText(t, h, layout.SlotMain, "# 배포알림")
	if got := top(h); got.Key() != (ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChDeploy}).Key() {
		t.Fatalf("second click opened %+v", got)
	}
	// a click on the space label does nothing
	h = open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteHome}, uitest.WithSeed(bootstrappedSeed))
	clickText(t, h, layout.SlotMain, "─ 노바 개발방")
	clickText(t, h, layout.SlotMain, "─ 노바 개발방")
	if top(h).Kind != ui.RouteHome {
		t.Fatalf("space label clicked open %+v", top(h))
	}
}

func TestHomeChannelsLive(t *testing.T) {
	h := open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteHome}, uitest.WithSeed(bootstrappedSeed))

	// a new channel
	h.Event("channel.created", realtime.ChannelEvent{Channel: homeTextChan(homeChanID(1), fixtures.SpaceNova, "새채널", 4, api.ChannelMyState{})})
	mustContainInOrder(t, paneText(t, h, layout.SlotMain), "─ 노바 개발방", "# 배포알림", "# 새채널", "─ DM")

	// removed from a private channel: it leaves at once
	priv := homeTextChan(homeChanID(2), fixtures.SpaceNova, "비밀", 5, api.ChannelMyState{})
	priv.IsPrivate = true
	h.Event("channel.created", realtime.ChannelEvent{Channel: priv})
	mustContain(t, paneText(t, h, layout.SlotMain), "🔒비밀")
	h.Event("channel.deleted", realtime.ChannelDeleted{ChannelID: priv.ID, SpaceID: ptrTo(fixtures.SpaceNova), Reason: "removed"})
	if main := paneText(t, h, layout.SlotMain); strings.Contains(main, "비밀") {
		t.Fatalf("removed from a private channel: the row must leave:\n%s", main)
	}

	// unread changes
	h.Event("read_state.updated", realtime.ReadStateUpdated{State: api.UnreadState{ChannelID: fixtures.ChDev, UnreadCount: 5}})
	h.Event("read_state.updated", realtime.ReadStateUpdated{State: api.UnreadState{ChannelID: fixtures.ChGeneral}})
	main := paneText(t, h, layout.SlotMain)
	homeRowEnds(t, main, "# 개발", "5")
	homeRowEnds(t, main, "# 일반", "# 일반")

	// muted live: the unread badge goes
	h.Event("channel.prefs_updated", realtime.ChannelPrefsUpdated{ChannelID: fixtures.ChDev, My: api.ChannelMyState{Notify: "all", Muted: true}})
	homeRowEnds(t, paneText(t, h, layout.SlotMain), "# 개발", "# 개발")

	// a joined space gets its section, after the rail's last space
	h.Event("space.joined", realtime.SpaceJoined{Space: joinedSpace()})
	mustContainInOrder(t, paneText(t, h, layout.SlotMain), "─ 노바 개발방", "# 새채널", "─ 보드게임 모임", "# 일반", "─ DM")

	// kicked from a space: its section leaves at once
	h.Event("space.removed", realtime.SpaceRemoved{SpaceID: fixtures.SpaceNova, Reason: "kicked"})
	main = paneText(t, h, layout.SlotMain)
	if strings.Contains(main, "노바 개발방") || strings.Contains(main, "# 개발") {
		t.Fatalf("a space I left must leave the home:\n%s", main)
	}
	mustContainInOrder(t, main, "친구 목록", "─ 보드게임 모임", "# 일반", "─ DM")
}

func TestHomeKeepsSelectionOnChannelRow(t *testing.T) {
	h := open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteHome}, uitest.WithSeed(bootstrappedSeed))
	homeSelect(t, h, "# 개발")
	h.Event("channel.created", realtime.ChannelEvent{Channel: homeTextChan(homeChanID(1), fixtures.SpaceNova, "가장앞", -1, api.ChannelMyState{})})
	h.Screen()
	if it, _ := homeOf(t, h).list.Selected(); it.ID != homeChanPrefix+fixtures.ChDev {
		t.Fatalf("selection moved to %+v", it)
	}
}

// bigSpace seeds Nova with n channels in all: the four fixture ones plus n-4 more
// ("채널05".."채널NN"); unread lists the extra channel numbers with 2 unread each.
func bigSpace(n int, unread ...int) func(*store.Store) {
	return func(s *store.Store) {
		bootstrappedSeed(s)
		for i := 5; i <= n; i++ {
			my := api.ChannelMyState{}
			if slices.Contains(unread, i) {
				my.UnreadCount = 2
			}
			s.ObserveChannel(homeTextChan(homeChanID(i), fixtures.SpaceNova, fmt.Sprintf("채널%02d", i), i, my))
		}
	}
}

func TestHomeLargeSpaceCollapses(t *testing.T) {
	// 12 channels: all shown
	h := open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteHome}, uitest.WithSeed(bigSpace(12)))
	main := paneText(t, h, layout.SlotMain)
	mustContainInOrder(t, main, "─ 노바 개발방", "# 일반", "# 잡담", "# 개발", "# 배포알림", "# 채널05", "# 채널12", "─ DM")
	if strings.Contains(main, "개 더") {
		t.Fatalf("12 channels are not collapsed:\n%s", main)
	}

	// 13 channels: only the unread ones (일반 3, 채널07, 채널13) + "…10개 더"
	h = open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteHome}, uitest.WithSeed(bigSpace(13, 7, 13)))
	main = paneText(t, h, layout.SlotMain)
	mustContainInOrder(t, main, "─ 노바 개발방", "# 일반", "# 채널07", "# 채널13", "…10개 더", "─ DM")
	for _, hidden := range []string{"# 잡담", "# 개발", "# 배포알림", "# 채널05", "# 채널12"} {
		if strings.Contains(main, hidden) {
			t.Fatalf("collapsed space shows read channel %q:\n%s", hidden, main)
		}
	}

	// live: 채널05 gets a mention, 채널07 is read
	h.Event("read_state.updated", realtime.ReadStateUpdated{State: api.UnreadState{ChannelID: homeChanID(5), UnreadCount: 1, MentionCount: 1}})
	h.Event("read_state.updated", realtime.ReadStateUpdated{State: api.UnreadState{ChannelID: homeChanID(7)}})
	main = paneText(t, h, layout.SlotMain)
	mustContainInOrder(t, main, "# 일반", "# 채널05", "# 채널13", "…10개 더")
	if strings.Contains(main, "채널07") {
		t.Fatalf("a read channel leaves the collapsed section:\n%s", main)
	}

	// enter on "…N개 더" expands the space in place; the cursor lands on the first
	// channel it revealed
	homeSelect(t, h, "개 더")
	h.Press("enter")
	if top(h).Kind != ui.RouteHome {
		t.Fatalf("expanding navigated to %+v", top(h))
	}
	main = paneText(t, h, layout.SlotMain)
	mustContainInOrder(t, main, "─ 노바 개발방", "# 일반", "# 잡담", "# 개발", "# 배포알림", "# 채널05", "# 채널07", "# 채널13", "─ DM")
	if strings.Contains(main, "개 더") {
		t.Fatalf("expanded space still has the more row:\n%s", main)
	}
	if it, _ := homeOf(t, h).list.Selected(); it.ID != homeChanPrefix+fixtures.ChChat {
		t.Fatalf("after expanding the cursor is on %+v, want # 잡담", it)
	}

	// the expansion survives SaveState / LoadState
	st := homeOf(t, h).SaveState()
	ctx := h.Context()
	v := newHome(ctx, ui.Route{Kind: ui.RouteHome}).(*homeView)
	v.LoadState(st)
	v.Resize(ctx, ui.Size{W: 60, H: 40})
	if out := ansi.Strip(v.View(ctx, true)); strings.Contains(out, "개 더") || !strings.Contains(out, "# 채널12") {
		t.Fatalf("restored home lost the expansion:\n%s", out)
	}

	// a click on the more row expands at once
	h = open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteHome}, uitest.WithSeed(bigSpace(14)))
	mustContain(t, paneText(t, h, layout.SlotMain), "…13개 더") // only 일반 is unread
	clickText(t, h, layout.SlotMain, "…13개 더")
	mustContain(t, paneText(t, h, layout.SlotMain), "# 채널14")
}

func TestHomeNoSpace(t *testing.T) {
	seed := func(s *store.Store) {
		bootstrappedSeed(s)
		for id, ch := range s.Channels {
			if ch.SpaceID != nil {
				delete(s.Channels, id)
			}
		}
		delete(s.Spaces, fixtures.SpaceNova)
		s.SpaceOrder = nil
	}
	h := open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteHome}, uitest.WithSeed(seed))
	main := paneText(t, h, layout.SlotMain)
	mustContainInOrder(t, main, "피드 보기", "친구 목록", "─ DM", "● 민지")
	if strings.Contains(main, "#") || strings.Contains(main, "노바") {
		t.Fatalf("no space, no space section:\n%s", main)
	}
	for _, it := range homeOf(t, h).items {
		if it.kind == homeSpace || it.kind == homeChan || it.kind == homeMore {
			t.Fatalf("no-space user has a space row %+v", it)
		}
	}
}

func TestHomeOnlySpaces(t *testing.T) {
	// a user with a space but no DM, friend, invite or request: the home lists the
	// space instead of the empty state
	seed := func(s *store.Store) {
		bootstrappedSeed(s)
		for _, id := range s.DMOrder {
			delete(s.Channels, id)
		}
		s.DMOrder = nil
		for id := range s.Friends {
			delete(s.Friends, id)
		}
	}
	h := open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteHome}, uitest.WithSeed(seed))
	main := paneText(t, h, layout.SlotMain)
	mustContainInOrder(t, main, "피드 보기", "친구 목록", "─ 노바 개발방", "# 일반")
	if strings.Contains(main, homeEmptyTitle) || strings.Contains(main, "─ DM") {
		t.Fatalf("home with only a space:\n%s", main)
	}
}

func TestHomeChannelsSmallModes(t *testing.T) {
	for _, c := range []struct {
		sz   uitest.Size
		mode layout.Mode
	}{
		{uitest.Size{W: 80, H: 30}, layout.ModeWidget},
		{uitest.Size{W: 40, H: 14}, layout.ModeTiny},
	} {
		t.Run(fmt.Sprintf("%dx%d", c.sz.W, c.sz.H), func(t *testing.T) {
			h := open(t, c.sz, ui.Route{Kind: ui.RouteHome}, uitest.WithSeed(bootstrappedSeed))
			if h.Context().Mode != c.mode {
				t.Fatalf("mode %v, want %v", h.Context().Mode, c.mode)
			}
			if _, ok := h.Model.Frame().Layout.Pane(layout.SlotSidebar); ok {
				t.Fatal("setup: no sidebar in this mode")
			}
			main := paneText(t, h, layout.SlotMain)
			mustContainInOrder(t, main, "피드 보기", "─ 노바 개발방", "# 일반", "# 잡담", "# 개발")
			homeRowEnds(t, main, "# 일반", "3")
			h.Check()
			h.Golden("home_channels")
			// the list scrolls down to the DMs and the online friends
			homeSelect(t, h, "하나")
			mustContain(t, paneText(t, h, layout.SlotMain), "● 하나")
			h.Check()
		})
	}
}
