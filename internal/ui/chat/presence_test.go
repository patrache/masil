package chat

import (
	tea "charm.land/bubbletea/v2"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// ---- typing ----

func TestTypingText(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	ctx := h.Context()
	s := ctx.Store
	minji, junho, hana := userBy(t, s, "minji"), userBy(t, s, "junho"), userBy(t, s, "hana")
	cases := []struct {
		ids  []string
		want string
	}{
		{nil, ""},
		{[]string{minji.ID}, "민지 입력 중…"},
		{[]string{minji.ID, junho.ID}, "민지, 준호 입력 중…"},
		{[]string{minji.ID, junho.ID, hana.ID}, "여러 명이 입력 중…"},
	}
	for _, c := range cases {
		if got := typingText(ctx, c.ids); got != c.want {
			t.Fatalf("typingText(%v) = %q, want %q", c.ids, got, c.want)
		}
	}
	// names are sanitized and cut to 6 cells each
	long := api.UserSummary{ID: "01JATYPELONG00000000000000", Handle: "long", DisplayName: "아주아주긴\x1b[31m이름"}
	s.ObserveUser(long)
	got := typingText(ctx, []string{long.ID})
	if strings.Contains(got, "\x1b") || got != "아주… 입력 중…" {
		t.Fatalf("long name: %q", got)
	}
}

func TestTypingLine(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	s := h.Context().Store
	minji, junho := userBy(t, s, "minji"), userBy(t, s, "junho")
	h.Event("typing", realtime.Typing{ChannelID: devID(t, h), UserID: minji.ID, ExpiresInMS: 6000})
	mustContain(t, h.Screen(), "민지 입력 중…")
	h.Event("typing", realtime.Typing{ChannelID: devID(t, h), UserID: junho.ID, ExpiresInMS: 6000})
	mustContain(t, h.Screen(), "민지, 준호 입력 중…")
	// the line sits right above the composer box
	row, _ := lineWith(t, h.Screen(), "민지, 준호 입력 중…")
	box, _ := lastLineWith(t, h.Screen(), "› # 개발에 메시지 보내기")
	if box != row+2 {
		t.Fatalf("typing line at row %d, composer text at %d (want the line right above the box)", row, box)
	}
	h.Advance(6 * time.Second) // expiry = local receipt time (chatOf → SetClock(ctx.Now) = FakeClock) + 6s; the event's TS is not used
	if strings.Contains(h.Screen(), "입력 중") {
		t.Fatal("expired after 6s")
	}
	// empty: the line takes no row (the composer box moves back down)
	if b2, _ := lastLineWith(t, h.Screen(), "› # 개발에 메시지 보내기"); b2 != box {
		t.Fatalf("composer moved: %d -> %d", box, b2)
	}
}

func TestTypingTickRedraws(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	minji := userBy(t, h.Context().Store, "minji")
	h.Event("typing", realtime.Typing{ChannelID: devID(t, h), UserID: minji.ID, ExpiresInMS: 6000})
	if !slices.Contains(h.Clock().Pending(), 6050*time.Millisecond) {
		t.Fatalf("a redraw is armed right after expiry (expires_in_ms + 50ms): %v", h.Clock().Pending())
	}
}

func TestTypingClearedByMessage(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	minji := userBy(t, h.Context().Store, "minji")
	ch := devID(t, h)
	h.Event("typing", realtime.Typing{ChannelID: ch, UserID: minji.ID, ExpiresInMS: 6000})
	mustContain(t, h.Screen(), "민지 입력 중…")
	h.Event("message.created", realtime.MessageEvent{Message: mkMsg(ch, 1, minji, "다 됐어요", h.Context().Now())})
	if sc := h.Screen(); strings.Contains(sc, "입력 중") || !strings.Contains(sc, "다 됐어요") {
		t.Fatalf("a message from the typer clears her line at once:\n%s", sc)
	}
}

func TestTypingThreadScoped(t *testing.T) {
	root := devRootMsg(t)
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	minji, junho := userBy(t, h.Context().Store, "minji"), userBy(t, h.Context().Store, "junho")
	selectText(t, h, "오늘 배포")
	h.Press("t")
	if _, ok := threadRoute(h); !ok {
		t.Fatal("thread placed")
	}
	rootID := root.ID
	h.Event("typing", realtime.Typing{ChannelID: root.ChannelID, ThreadRootID: &rootID, UserID: minji.ID, ExpiresInMS: 6000})
	if th := paneText(t, h, layout.SlotThread); !strings.Contains(th, "민지 입력 중…") {
		t.Fatalf("thread view shows its typer:\n%s", th)
	}
	if main := paneText(t, h, layout.SlotMain); strings.Contains(main, "입력 중") {
		t.Fatalf("the channel view does not show a thread typer:\n%s", main)
	}
	h.Event("typing", realtime.Typing{ChannelID: root.ChannelID, UserID: junho.ID, ExpiresInMS: 6000})
	if main := paneText(t, h, layout.SlotMain); !strings.Contains(main, "준호 입력 중…") {
		t.Fatalf("the channel view shows a channel typer:\n%s", main)
	}
	if th := paneText(t, h, layout.SlotThread); strings.Contains(th, "준호 입력") {
		t.Fatalf("the thread view does not show a channel typer:\n%s", th)
	}
}

func TestTypingExcludesBlocked(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	s := h.Context().Store
	minji, junho := userBy(t, s, "minji"), userBy(t, s, "junho")
	s.Blocks[minji.ID] = h.Context().Now()
	h.Event("block.updated", realtime.BlockUpdated{UserID: minji.ID, Blocked: true})
	h.Event("typing", realtime.Typing{ChannelID: devID(t, h), UserID: minji.ID, ExpiresInMS: 6000})
	if strings.Contains(h.Screen(), "입력 중") {
		t.Fatal("a blocked user's typing is not shown")
	}
	h.Event("typing", realtime.Typing{ChannelID: devID(t, h), UserID: junho.ID, ExpiresInMS: 6000})
	sc := h.Screen()
	if !strings.Contains(sc, "준호 입력 중…") || strings.Contains(sc, "민지, 준호") {
		t.Fatalf("only junho types:\n%s", sc)
	}
}

// ---- members pane ----

func TestMembersPaneGoldenAndPolling(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	mustContain(t, paneText(t, h, layout.SlotMembers), "민지", "준호")
	n0 := len(reqs(h.API, "GET", "/v1/presence"))
	if n0 == 0 {
		t.Fatal("presence fetched as soon as the pane shows")
	}
	h.Advance(60 * time.Second)
	if len(reqs(h.API, "GET", "/v1/presence")) != n0+1 {
		t.Fatal("re-poll every 60s while visible")
	}
	h.Resize(120, 40)
	h.Advance(120 * time.Second) // ModeChangedMsg: SlotMembers gone → the pending tick finds no pane and stops
	if len(reqs(h.API, "GET", "/v1/presence")) != n0+1 {
		t.Fatal("no polling while hidden")
	}
	h.Resize(200, 60) // ModeChangedMsg: pane back → poll restarts immediately (no View-timing dependency)
	if len(reqs(h.API, "GET", "/v1/presence")) != n0+2 {
		t.Fatal("restart on show")
	}
	h.Advance(59 * time.Second)
	if len(reqs(h.API, "GET", "/v1/presence")) != n0+2 {
		t.Fatal("next poll only at 60s")
	}
	h.Advance(time.Second)
	if len(reqs(h.API, "GET", "/v1/presence")) != n0+3 {
		t.Fatal("re-armed after restart")
	}
}

func TestMembersPaneListAndPresence(t *testing.T) {
	srv := newAPI(t)
	// the server says junho came online since the seed
	srv.JSON("GET /v1/presence", http.StatusOK, map[string]any{"items": []api.PresenceItem{
		{UserID: fixtures.UserMinji, Status: api.PresenceOnline},
		{UserID: fixtures.UserJunho, Status: api.PresenceDND},
	}}) // replaces the whole presence route for this test
	h := openWith(t, uitest.Size{W: 200, H: 60}, ui.Route{Kind: ui.RouteChannel, ID: chanID(t, seeded(), "개발")}, srv, uitest.WithSeed(bootstrappedSeed))
	s := h.Context().Store
	if e := s.Presence[fixtures.UserJunho]; e.Status != api.PresenceDND || !e.FetchedAt.Equal(h.Context().Now()) {
		t.Fatalf("presence result lands in Store.Presence: %+v", e)
	}
	r := reqs(srv, "GET", "/v1/presence")
	if len(r) == 0 || strings.Contains(r[len(r)-1].Query, fixtures.UserSam) || !strings.Contains(r[len(r)-1].Query, fixtures.UserJunho) {
		t.Fatalf("presence asks the members but me: %+v", r)
	}
	pane := paneText(t, h, layout.SlotMembers)
	// online (by name) → away → dnd → offline; the owner is marked 방장
	mustContainInOrder(t, pane, "● sam · 방장", "● 민지", "● 하나", "◐ 김개발", "⊘ 준호")
	if strings.Contains(pane, "준호 ·") {
		t.Fatalf("no role known for a member: %s", pane)
	}
	sc := h.Screen()
	mustContain(t, sc, "멤버 5")
	if n := len(reqs(srv, "GET", "/v1/channels/"+devID(t, h)+"/members")); n != 1 {
		t.Fatalf("members fetched once, got %d", n)
	}
	h.Advance(60 * time.Second)
	if n := len(reqs(srv, "GET", "/v1/channels/"+devID(t, h)+"/members")); n != 1 {
		t.Fatalf("polling refreshes presence, not the member list (%d)", n)
	}
}

func TestMembersPaneMoreLine(t *testing.T) {
	srv := newAPI(t)
	dev := chanID(t, seeded(), "개발")
	st := seeded()
	cur := "next"
	srv.JSON("GET /v1/channels/"+dev+"/members", http.StatusOK, api.Page[api.ChannelMember]{
		Items:      []api.ChannelMember{{User: *st.User(fixtures.UserMinji)}, {User: *st.User(fixtures.UserJunho)}},
		NextCursor: &cur,
	})
	seed := func(s *store.Store) {
		bootstrappedSeed(s)
		s.Channels[dev].MemberCount = 130
	}
	h := openWith(t, uitest.Size{W: 200, H: 60}, ui.Route{Kind: ui.RouteChannel, ID: dev}, srv, uitest.WithSeed(seed))
	mustContain(t, paneText(t, h, layout.SlotMembers), "+ 128명 더")
	mustContain(t, h.Screen(), "멤버 130")
}

func TestMembersPaneDM(t *testing.T) {
	h := open(t, uitest.Size{W: 200, H: 60}, ui.Route{Kind: ui.RouteChannel, ID: fixtures.DMMinji}, uitest.WithSeed(bootstrappedSeed))
	pane := paneText(t, h, layout.SlotMembers)
	mustContainInOrder(t, pane, "● sam", "● 민지")
	mustContain(t, h.Screen(), "멤버 2")
	if n := len(reqs(h.API, "GET", "/v1/channels/"+fixtures.DMMinji+"/members")); n != 0 {
		t.Fatalf("a DM lists Channel.Members, no request (%d)", n)
	}
	if len(reqs(h.API, "GET", "/v1/presence")) == 0 {
		t.Fatal("a DM's presence is polled too")
	}
}

func TestMembersPaneFollowsViewedChannel(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	n0 := len(reqs(h.API, "GET", "/v1/presence"))
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteChannel, ID: fixtures.DMMinji}})
	h.Screen()
	if len(reqs(h.API, "GET", "/v1/presence")) != n0+1 {
		t.Fatal("a newly viewed channel is polled at once")
	}
	mustContain(t, h.Screen(), "멤버 2")
	h.Advance(60 * time.Second)
	if len(reqs(h.API, "GET", "/v1/presence")) != n0+2 {
		t.Fatal("one timer per channel: the old arming is dropped")
	}
}

func TestMembersPaneStopsPollingWithoutViewedChannel(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteHome}})
	h.Screen()
	if _, ok := h.Context().Layout.Pane(layout.SlotMembers); !ok {
		t.Fatal("precondition: the members pane stays on screen on Home")
	}
	mustContain(t, paneText(t, h, layout.SlotMembers), membersNone)
	n0 := len(reqs(h.API, "GET", "/v1/presence"))
	h.Advance(120 * time.Second)
	if n := len(reqs(h.API, "GET", "/v1/presence")); n != n0 {
		t.Fatalf("no presence poll for the last channel while none is viewed (%d -> %d)", n0, n)
	}
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteChannel, ID: devID(t, h)}})
	h.Screen()
	if n := len(reqs(h.API, "GET", "/v1/presence")); n != n0+1 {
		t.Fatalf("viewing a channel again restarts the poll at once (%d -> %d)", n0, n)
	}
}

func TestMembersPaneActions(t *testing.T) {
	srv := newAPI(t)
	st := seeded()
	srv.JSON("POST /v1/dms", http.StatusOK, map[string]any{"channel": st.Channels[fixtures.DMJunho]})
	h := openWith(t, uitest.Size{W: 200, H: 60}, ui.Route{Kind: ui.RouteChannel, ID: chanID(t, seeded(), "개발")}, srv, uitest.WithSeed(bootstrappedSeed))
	clickText(t, h, layout.SlotMembers, "준호") // focus + select
	mustContain(t, paneText(t, h, layout.SlotMembers), "›○ 준호")
	h.Press("m")
	want := []string{"DM 보내기", "프로필 보기", "사용자 신고", "차단"}
	if labels := menuLabels(t, h); !slices.Equal(labels, want) {
		t.Fatalf("items %q, want %q", labels, want)
	}
	h.Press("enter") // DM 보내기
	r := reqs(srv, "POST", "/v1/dms")
	if len(r) != 1 {
		t.Fatalf("one POST /v1/dms, got %d", len(r))
	}
	mustJSONObject(t, r[0].Body, `{"users":["@junho"]}`)
	if tr := top(h); tr.Kind != ui.RouteChannel || tr.ID != fixtures.DMJunho {
		t.Fatalf("opens the DM: %+v", tr)
	}

	// enter opens the menu too; 사용자 신고 morphs into the report dialog
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteChannel, ID: chanID(t, seeded(), "개발")}})
	clickText(t, h, layout.SlotMembers, "김개발") // select (준호 was still selected: a click on it would open its menu)
	h.Press("j", "enter")
	clickOn(t, h, "사용자 신고", tea.MouseLeft)
	if overlays(h) != 1 || !strings.Contains(h.Screen(), "신고 · 준호") {
		t.Fatalf("사용자 신고 → report dialog:\n%s", h.Screen())
	}
	h.Press("esc")

	// my own row: only the profile
	clickText(t, h, layout.SlotMembers, "sam")
	h.Press("m")
	// (a one-item menu draws its title inside the box: compare the items only)
	if labels := menuLabels(t, h); labels[len(labels)-1] != "프로필 보기" || slices.ContainsFunc(labels, func(l string) bool {
		return l == "DM 보내기" || l == "사용자 신고" || l == "차단"
	}) {
		t.Fatalf("my row: %q", labels)
	}
	h.Press("enter")
	feed := h.Nav().Feed // a profile is feed content: it opens in the feed column
	if len(feed) == 0 || feed[len(feed)-1].Kind != ui.RouteProfile || feed[len(feed)-1].ID != fixtures.UserSam {
		t.Fatalf("프로필 보기 navigates to the profile: %+v", feed)
	}
}

func TestMembersPaneBlockAsks(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	clickText(t, h, layout.SlotMembers, "준호")
	h.Press("m")
	clickOn(t, h, "차단", tea.MouseLeft)
	if overlays(h) != 1 || !strings.Contains(h.Screen(), "차단할까요?") {
		t.Fatalf("차단 asks first:\n%s", h.Screen())
	}
	h.Press("y")
	if n := len(reqs(h.API, "PUT", "/v1/blocks/@junho")); n != 1 {
		t.Fatalf("PUT /v1/blocks/@junho (%d)", n)
	}
	// a blocked member's menu has no 차단
	clickText(t, h, layout.SlotMembers, "준호")
	h.Press("m")
	if labels := menuLabels(t, h); slices.Contains(labels, "차단") {
		t.Fatalf("already blocked: %q", labels)
	}
}

func TestMembersPaneExactSize(t *testing.T) {
	forEachSize(t, uitest.ModeSizes[layout.ModeCommand], devChannel, func(t *testing.T, h *uitest.Harness) {
		p, ok := h.Model.Frame().Layout.Pane(layout.SlotMembers)
		if !ok {
			t.Fatal("command mode places the members pane")
		}
		in := p.Rect.Inner()
		ctx := h.Context()
		mp := newMembers(ctx)
		mp.Resize(ctx, ui.Size{W: in.W, H: in.H})
		for _, focused := range []bool{false, true} {
			lines := strings.Split(mp.View(ctx, focused), "\n")
			if len(lines) != in.H {
				t.Fatalf("%d lines, want %d", len(lines), in.H)
			}
		}
	})
}

func TestMembersHostileIntegrity(t *testing.T) {
	forEachSize(t, uitest.ModeSizes[layout.ModeCommand], hostileOpen, func(t *testing.T, h *uitest.Harness) { h.Check() })
}

// ---- membership changes ----

// newbie is a user who joins during a test (not in the seed).
var newbie = api.UserSummary{ID: "01JA0000000000000000NEWB01", Handle: "newbie", DisplayName: "새내기"}

// liveMembers serves GET …/members of channel ch from *users (read per request).
func liveMembers(srv interface {
	Handle(string, http.HandlerFunc)
}, ch string, users *[]api.UserSummary) {
	srv.Handle("GET /v1/channels/"+ch+"/members", func(w http.ResponseWriter, _ *http.Request) {
		items := []api.ChannelMember{}
		for _, u := range *users {
			items = append(items, api.ChannelMember{User: u, JoinedAt: memberJoined})
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": nil})
	})
}

// seededMembers is the seeded channel member list (memberHandles).
func seededMembers(t testing.TB) []api.UserSummary {
	t.Helper()
	s := seeded()
	var out []api.UserSummary
	for _, h := range memberHandles {
		out = append(out, userBy(t, s, h))
	}
	return out
}

func withoutID(us []api.UserSummary, id string) []api.UserSummary {
	return slices.DeleteFunc(slices.Clone(us), func(u api.UserSummary) bool { return u.ID == id })
}

func TestMembersPaneFollowsMembership(t *testing.T) {
	srv := newAPI(t)
	dev := chanID(t, seeded(), "개발")
	users := seededMembers(t)
	liveMembers(srv, dev, &users)
	h := openWith(t, uitest.Size{W: 200, H: 60}, ui.Route{Kind: ui.RouteChannel, ID: dev}, srv, uitest.WithSeed(bootstrappedSeed))
	if sp := h.Context().Store.Channels[dev].SpaceID; sp == nil || *sp != fixtures.SpaceNova {
		t.Fatal("precondition: #개발 is in 노바 개발방")
	}
	membersGets := func() int { return len(reqs(srv, "GET", "/v1/channels/"+dev+"/members")) }
	mustContain(t, paneText(t, h, layout.SlotMembers), "민지")
	n := membersGets()

	// 민지 leaves the space: she leaves the pane at once and the page is refetched
	users = withoutID(users, fixtures.UserMinji)
	h.Event("space.member_left", realtime.SpaceMemberLeft{SpaceID: fixtures.SpaceNova, UserID: fixtures.UserMinji, Reason: "kicked"})
	if pane := paneText(t, h, layout.SlotMembers); strings.Contains(pane, "민지") {
		t.Fatalf("a departed member stays listed:\n%s", pane)
	}
	if got := membersGets(); got != n+1 {
		t.Fatalf("member page refetched %d times, want 1", got-n)
	}
	// and her presence is no longer polled
	h.Advance(61 * time.Second)
	pr := reqs(srv, "GET", "/v1/presence")
	if last := pr[len(pr)-1].Query; strings.Contains(last, fixtures.UserMinji) || !strings.Contains(last, fixtures.UserJunho) {
		t.Fatalf("presence still polls the departed member: %s", last)
	}

	// a newcomer added to the channel appears
	users = append(users, newbie)
	h.Event("channel.member_added", realtime.ChannelMemberAdded{ChannelID: dev, User: newbie})
	mustContain(t, paneText(t, h, layout.SlotMembers), "새내기")
	if got := membersGets(); got != n+2 {
		t.Fatalf("member page refetched %d times, want 2", got-n)
	}

	// another space's or channel's membership leaves the page alone
	h.Event("space.member_joined", realtime.SpaceMemberEvent{SpaceID: "01JB0000000000000000OTHER1", Member: api.SpaceMember{User: newbie, Role: api.RoleMember}})
	h.Event("channel.member_removed", realtime.ChannelMemberRemoved{ChannelID: "01JB0000000000000000OTHER2", UserID: fixtures.UserJunho})
	if got := membersGets(); got != n+2 {
		t.Fatalf("unrelated membership refetched (%d)", got-n)
	}
	mustContain(t, paneText(t, h, layout.SlotMembers), "준호")
}
