package chat

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

func TestSidebarGolden(t *testing.T) {
	forEachSize(t, uitest.AllSizes(), devChannel, func(t *testing.T, h *uitest.Harness) { paneGolden(t, h, layout.SlotSidebar, "sidebar") })
}

func TestSidebarHidesBlockedDM(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	s := h.Context().Store
	junho := userBy(t, s, "junho")
	mustContain(t, paneText(t, h, layout.SlotSidebar), "준호")
	s.Blocks[junho.ID] = h.Context().Now()
	h.Event("block.updated", realtime.BlockUpdated{UserID: junho.ID, Blocked: true})
	side := paneText(t, h, layout.SlotSidebar)
	if strings.Contains(side, "준호") || !strings.Contains(side, "민지") {
		t.Fatalf("blocked 1:1 DM must be hidden at once:\n%s", side)
	}
	delete(s.Blocks, junho.ID)
	h.Event("block.updated", realtime.BlockUpdated{UserID: junho.ID, Blocked: false})
	mustContain(t, paneText(t, h, layout.SlotSidebar), "준호")
}

func TestSidebarCommandVsStandard(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	side := paneText(t, h, layout.SlotSidebar)
	mustContainInOrder(t, side, "노바 개발방", "─ 채널", "# 일반", "# 잡담", "# 개발", "# 배포알림", "─ DM", "● 민지", "○ 준호")
	if strings.Contains(side, "▾") {
		t.Fatal("command: the rail switches spaces, no dropdown")
	}
	h.Resize(120, 40)
	side = paneText(t, h, layout.SlotSidebar)
	mustContainInOrder(t, side, "노바 개발방 ▾", "# 일반", "─ DM")
	if strings.Contains(side, "─ 채널") {
		t.Fatal("standard: dropdown header replaces the section label")
	}
}

// sidebarView renders a fresh sidebar on h's context at the sidebar pane's inner size
// (styled lines: the app's compositor re-encodes SGR, the component output does not).
func sidebarView(t *testing.T, h *uitest.Harness, focused bool) []string {
	t.Helper()
	p, ok := h.Model.Frame().Layout.Pane(layout.SlotSidebar)
	if !ok {
		t.Fatal("no sidebar")
	}
	ctx := h.Context()
	sb := newSidebar(ctx)
	sb.Resize(ctx, ui.Size{W: p.Rect.Inner().W, H: p.Rect.Inner().H})
	return strings.Split(sb.View(ctx, focused), "\n")
}

// rowWith is the first styled line whose plain text contains text.
func rowWith(t *testing.T, lines []string, text string) string {
	t.Helper()
	for _, l := range lines {
		if strings.Contains(ansi.Strip(l), text) {
			return l
		}
	}
	t.Fatalf("%q not in:\n%s", text, ansi.Strip(strings.Join(lines, "\n")))
	return ""
}

// sgr is the opening SGR sequence st puts before its text.
func sgr(st lipgloss.Style) string {
	s := st.Render("Z")
	return s[:strings.Index(s, "Z")]
}

func TestSidebarBadges(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	s := h.Context().Store
	th := h.Context().Theme
	for _, row := range strings.Split(paneText(t, h, layout.SlotSidebar), "\n") {
		if strings.Contains(row, "# 일반") && !strings.HasSuffix(strings.TrimRight(row, " "), "3") {
			t.Fatalf("# 일반 row must end with its unread badge: %q", row)
		}
	}
	if r := rowWith(t, sidebarView(t, h, false), "# 일반"); !strings.Contains(r, sgr(th.S.Badge)) || !strings.Contains(r, sgr(th.S.Bold)) {
		t.Fatalf("unread row: badge + bold: %q", r)
	}

	s.Channels[chanID(t, s, "개발")].My.MentionCount = 2
	if r := rowWith(t, sidebarView(t, h, false), "# 개발"); !strings.Contains(r, th.S.BadgeMention.Render(" 2 ")) {
		t.Fatalf("mention badge missing: %q", r)
	}

	chat := s.Channels[chanID(t, s, "잡담")]
	chat.My.Muted = true
	chat.My.UnreadCount = 5
	r := rowWith(t, sidebarView(t, h, false), "# 잡담")
	if !strings.Contains(r, sgr(th.S.Dim)) || strings.Contains(ansi.Strip(r), "5") {
		t.Fatalf("muted channel: dim, no unread badge: %q", r)
	}
	chat.My.MentionCount = 1
	if r := rowWith(t, sidebarView(t, h, false), "# 잡담"); !strings.Contains(r, th.S.BadgeMention.Render(" 1 ")) {
		t.Fatalf("muted channel still shows mentions: %q", r)
	}

	ro := s.Channels[dmWith(t, s, "junho")]
	ro.ReadOnly = true
	if r := rowWith(t, sidebarView(t, h, false), "준호"); !strings.Contains(r, sgr(th.S.Dim)) {
		t.Fatalf("read-only DM is dim: %q", r)
	}

	hh := hostileOpen(t, uitest.Size{W: 200, H: 60})
	if !strings.Contains(paneText(t, hh, layout.SlotSidebar), "99+") {
		t.Fatalf("9999 unread must read 99+:\n%s", paneText(t, hh, layout.SlotSidebar))
	}
}

func TestSidebarViewedHighlightAndOpen(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	s := h.Context().Store
	th := h.Context().Theme
	// Viewing is what the app projects (RouteChannel's ChannelViewer); set it directly
	// for this direct render so the check does not depend on the conversation route.
	s.SetViewing(chanID(t, s, "개발"))
	lines := sidebarView(t, h, false)
	if r := rowWith(t, lines, "# 개발"); !strings.Contains(r, sgr(th.S.Selected)) {
		t.Fatalf("viewed channel row must be S.Selected: %q", r)
	}
	if r := rowWith(t, lines, "# 잡담"); strings.Contains(r, sgr(th.S.Selected)) {
		t.Fatalf("other rows are not selected: %q", r)
	}
	if strings.Contains(ansi.Strip(strings.Join(lines, "\n")), "›") {
		t.Fatal("unfocused: no cursor")
	}
	ctx := h.Context()
	sb := newSidebar(ctx)
	sb.Resize(ctx, ui.Size{W: 24, H: 30})
	sb.View(ctx, false) // unfocused: the cursor follows the viewed channel
	if r := rowWith(t, strings.Split(sb.View(ctx, true), "\n"), "# 개발"); !strings.Contains(ansi.Strip(r), "›# 개발") {
		t.Fatalf("focused: the cursor starts on the viewed channel: %q", r)
	}

	start := top(h)
	clickText(t, h, layout.SlotSidebar, "─ DM") // a label row: focuses the pane, runs nothing
	if top(h).Key() != start.Key() || len(h.Model.Frame().Overlays) != 0 {
		t.Fatalf("label click ran something: %+v", top(h))
	}
	h.Press("home", "j", "enter")
	if r := top(h); r.Kind != ui.RouteChannel || r.ID != chanID(t, s, "잡담") {
		t.Fatalf("home j enter: %+v", r)
	}

	clickText(t, h, layout.SlotSidebar, "# 일반") // click = select
	if top(h).ID != chanID(t, s, "잡담") {
		t.Fatalf("first click only selects: %+v", top(h))
	}
	mustContain(t, paneText(t, h, layout.SlotSidebar), "›# 일반")
	clickText(t, h, layout.SlotSidebar, "# 일반") // re-click = open
	if top(h).ID != chanID(t, s, "일반") {
		t.Fatalf("re-click opens: %+v", top(h))
	}

	clickText(t, h, layout.SlotSidebar, "─ DM")
	h.Press("G", "k", "enter")
	if r := top(h); r.Kind != ui.RouteChannel || r.ID != dmWith(t, s, "minji") {
		t.Fatalf("enter on ● 민지 opens the DM: %+v", r)
	}
}

func TestSidebarKeysSkipLabels(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	clickText(t, h, layout.SlotSidebar, "─ 채널")
	h.Press("home")
	mustContain(t, paneText(t, h, layout.SlotSidebar), "›# 일반") // the title and "─ 채널" are not selectable
	h.Press("k")
	mustContain(t, paneText(t, h, layout.SlotSidebar), "›# 일반")
	h.Press("j", "j", "j", "j")
	mustContain(t, paneText(t, h, layout.SlotSidebar), "›＋ 새 채널") // the owner's last channel-section row
	h.Press("j")
	mustContain(t, paneText(t, h, layout.SlotSidebar), "›● 민지") // "─ DM" skipped
	h.Press("end")
	mustContain(t, paneText(t, h, layout.SlotSidebar), "›○ 준호")
	if side := paneText(t, h, layout.SlotSidebar); strings.Count(side, "›") != 1 {
		t.Fatalf("one cursor:\n%s", side)
	}

	// standard: the dropdown header is selectable and enter opens the switcher.
	h.Resize(120, 40)
	clickText(t, h, layout.SlotSidebar, "─ DM")
	h.Press("home")
	mustContain(t, paneText(t, h, layout.SlotSidebar), "›노바 개발방 ▾")
	h.Press("enter")
	if len(h.Model.Frame().Overlays) != 1 || !strings.Contains(h.Screen(), "방 만들기") {
		t.Fatal("enter on the dropdown opens the space switcher")
	}
}

func TestSidebarDropdownOpensSwitcher(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	clickText(t, h, layout.SlotSidebar, "노바 개발방 ▾")
	sc := h.Screen()
	if len(h.Model.Frame().Overlays) != 1 || !strings.Contains(sc, "홈") || !strings.Contains(sc, "방 만들기") || !strings.Contains(sc, "초대 코드로 참여") {
		t.Fatalf("dropdown opens the space switcher:\n%s", sc)
	}
	h.Press("enter") // 홈
	if top(h).Kind != ui.RouteHome || len(h.Model.Frame().Overlays) != 0 || chatOf(h.Context()).SelectedSpace() != "" {
		t.Fatalf("홈: %+v", top(h))
	}

	h.Send(ui.OpenOverlayMsg{Overlay: newSpaceSwitchMenu(h.Context())})
	h.Press("j", "enter") // DM
	if r := top(h); r.Kind != ui.RouteDMs || !h.Nav().InDM {
		t.Fatalf("DM: %+v", r)
	}
	h.Send(ui.OpenOverlayMsg{Overlay: newSpaceSwitchMenu(h.Context())})
	h.Press("j", "j", "enter") // 노바 개발방
	s := h.Context().Store
	sid := spaceByName(t, s, "노바 개발방")
	if r := top(h); r.Kind != ui.RouteChannel || r.ID != s.Spaces[sid].DefaultChannelID || chatOf(h.Context()).SelectedSpace() != sid {
		t.Fatalf("space: %+v", r)
	}

	h.Send(ui.OpenOverlayMsg{ID: "chat.space_switch"})
	if len(h.Model.Frame().Overlays) != 1 || !strings.Contains(h.Screen(), "초대 코드로 참여") {
		t.Fatal("chat.space_switch is a registered overlay")
	}
}

func TestSidebarPresenceLive(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	junho := userBy(t, h.Context().Store, "junho")
	h.Event("presence.updated", realtime.PresenceUpdated{UserID: junho.ID, Status: "online"})
	mustContain(t, paneText(t, h, layout.SlotSidebar), "● 준호")
}

func TestSidebarPresenceFallback(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	s := h.Context().Store
	junho := userBy(t, s, "junho")
	delete(s.Presence, junho.ID)
	f := s.Friends[junho.ID]
	f.Presence = api.PresenceAway
	s.Friends[junho.ID] = f
	mustContain(t, paneText(t, h, layout.SlotSidebar), "◐ 준호") // no cached presence: the friend entry
	delete(s.Friends, junho.ID)
	mustContain(t, paneText(t, h, layout.SlotSidebar), "○ 준호") // neither: offline
}

func TestSidebarGroupDMAndPrivate(t *testing.T) {
	seed := func(s *store.Store) {
		bootstrappedSeed(s)
		g := api.Channel{ID: "01JBGROUPDM000000000000001", Kind: api.ChannelGroupDM, MemberCount: 3,
			Members: []api.UserSummary{*s.User(s.Me.ID), userBy(t, s, "minji"), userBy(t, s, "hana")}}
		s.ObserveChannel(g)
		s.Channels[chanID(t, s, "배포알림")].IsPrivate = true
	}
	h := open(t, uitest.Size{W: 200, H: 60}, ui.Route{Kind: ui.RouteChannel, ID: chanID(t, seeded(), "개발")}, uitest.WithSeed(seed))
	side := paneText(t, h, layout.SlotSidebar)
	mustContain(t, side, "◇ ", "🔒배포알림")
	label := []rune(h.Context().Store.ChannelLabel("01JBGROUPDM000000000000001"))
	if len(label) < 2 || !strings.Contains(side, "◇ "+string(label[:2])) {
		t.Fatalf("group DM row uses the channel label:\n%s", side)
	}
}

func TestSidebarNoSpace(t *testing.T) {
	none := func(s *store.Store) {
		bootstrappedSeed(s)
		for _, id := range s.SpaceOrder {
			for _, ch := range s.SpaceChannels(id) {
				delete(s.Channels, ch.ID)
			}
			delete(s.Spaces, id)
		}
		s.SpaceOrder = nil
	}
	s := seeded()
	h := open(t, uitest.Size{W: 200, H: 60}, ui.Route{Kind: ui.RouteChannel, ID: dmWith(t, s, "minji")}, uitest.WithSeed(none))
	side := paneText(t, h, layout.SlotSidebar)
	if !strings.Contains(strings.Join(strings.Fields(side), " "), "방이 없어요 · + 로 만들기") || strings.Contains(side, "─ 채널") {
		t.Fatalf("no space notice:\n%s", side)
	}
	mustContainInOrder(t, side, "─ DM", "● 민지", "○ 준호")
	h.Check()
	h.Resize(120, 40)
	mustContain(t, paneText(t, h, layout.SlotSidebar), "▾", "─ DM")
	h.Check()
}

// The channel section of a space I own or administer ends with ＋ 새 채널, which
// opens the channel creation dialog for that space; a plain member has none.
func TestSidebarNewChannelRow(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	nova := fixtures.SpaceNova
	mustContainInOrder(t, paneText(t, h, layout.SlotSidebar), "─ 채널", "# 배포알림", "＋ 새 채널", "─ DM", "● 민지")
	h.Resize(120, 40) // standard: the same row under the dropdown header
	mustContainInOrder(t, paneText(t, h, layout.SlotSidebar), "노바 개발방 ▾", "# 배포알림", "＋ 새 채널", "─ DM")
	h.Resize(200, 60)

	setRole(h, api.RoleAdmin)
	mustContain(t, paneText(t, h, layout.SlotSidebar), "＋ 새 채널")
	setRole(h, api.RoleMember)
	if side := paneText(t, h, layout.SlotSidebar); strings.Contains(side, "새 채널") {
		t.Fatalf("a member cannot create channels: no row:\n%s", side)
	}
	setRole(h, api.RoleOwner)

	// it is no channel: it carries the space, not a *api.Channel (no badge, no viewed style)
	n := 0
	for _, it := range sideItems(h.Context(), 30) {
		if it.Kind == sideNewChannel {
			n++
			if it.Ch != nil || it.Space != nova || !it.selectable() {
				t.Fatalf("new-channel row %+v", it)
			}
		}
	}
	if n != 1 {
		t.Fatalf("%d new-channel rows", n)
	}

	// enter on it opens chat.channel_create for the sidebar's space
	clickText(t, h, layout.SlotSidebar, "─ 채널")
	h.Press("home", "j", "j", "j", "j")
	mustContain(t, paneText(t, h, layout.SlotSidebar), "›＋ 새 채널")
	start := top(h)
	h.Press("enter")
	if overlays(h) != 1 || !strings.Contains(overlayText(t, h), nameLabel) {
		t.Fatalf("enter on ＋ 새 채널 opens the creation dialog:\n%s", h.Screen())
	}
	if top(h).Key() != start.Key() {
		t.Fatalf("the row opens a dialog, not a route: %+v", top(h))
	}
	h.Type("배포알림2")
	h.Press("enter")
	if n := len(reqs(h.API, "POST", "/v1/spaces/"+nova+"/channels")); n != 1 {
		t.Fatalf("the dialog creates in the sidebar's space: %d POSTs", n)
	}

	// mouse: a click selects it, a second click opens the dialog
	g := devChannel(t, uitest.Size{W: 200, H: 60})
	clickText(t, g, layout.SlotSidebar, "＋ 새 채널")
	if overlays(g) != 0 {
		t.Fatal("first click only selects")
	}
	mustContain(t, paneText(t, g, layout.SlotSidebar), "›＋ 새 채널")
	clickText(t, g, layout.SlotSidebar, "＋ 새 채널")
	if overlays(g) != 1 || !strings.Contains(overlayText(t, g), nameLabel) {
		t.Fatalf("second click opens the creation dialog:\n%s", g.Screen())
	}
}

func TestSidebarHostileIntegrity(t *testing.T) {
	forEachSize(t, uitest.AllSizes(), hostileOpen, func(t *testing.T, h *uitest.Harness) { h.Check() })
}

func TestSidebarViewExactSize(t *testing.T) {
	h := hostileOpen(t, uitest.Size{W: 200, H: 60})
	ctx := h.Context()
	sb := newSidebar(ctx)
	for _, sz := range []ui.Size{{W: 24, H: 57}, {W: 24, H: 3}, {W: 1, H: 1}, {W: 2, H: 5}, {W: 9, H: 7}, {W: 0, H: 0}} {
		sb.Resize(ctx, sz)
		for _, focused := range []bool{false, true} {
			out := sb.View(ctx, focused)
			if sz.H == 0 {
				continue
			}
			lines := strings.Split(out, "\n")
			if len(lines) != sz.H {
				t.Fatalf("%v: %d lines", sz, len(lines))
			}
			for _, l := range lines {
				if w := textwidth.Width(ansi.Strip(l)); w != sz.W {
					t.Fatalf("%v: line %q is %d wide", sz, l, w)
				}
			}
		}
	}
	if _, ok := sb.(ui.KeyHandler); !ok {
		t.Fatal("sidebar must be a KeyHandler")
	}
	if _, ok := sb.(ui.MouseHandler); !ok {
		t.Fatal("sidebar must be a MouseHandler")
	}
}
