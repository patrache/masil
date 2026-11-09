package chat

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/humanize"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// DM tab / DM surface (CA 5.3.3 "DM 표면", 5.4 "탭"; owner request 2026-11-09).

const groupDMID = "01JB000000000000000000GDM1"

// dmSeed is the bootstrapped fixture seed plus: 민지's DM 2 unread, a group DM with
// 민지 and 하나 (newest activity, 4 unread, a mention), 준호 blocked with 5 unread on
// his DM (hidden: never listed, never counted).
func dmSeed(s *store.Store) {
	bootstrappedSeed(s)
	s.Channels[fixtures.DMMinji].My.UnreadCount = 2
	s.Channels[fixtures.DMJunho].My.UnreadCount = 5
	s.Blocks[fixtures.UserJunho] = uitest.Start.Add(-time.Hour)
	var members []api.UserSummary
	for _, id := range []string{fixtures.UserSam, fixtures.UserMinji, fixtures.UserHana} {
		members = append(members, *s.User(id))
	}
	last := uitest.Start.Add(-90 * time.Second)
	s.ObserveChannel(api.Channel{
		ID: groupDMID, Kind: api.ChannelGroupDM, MemberCount: 3, Members: members,
		CreatedAt: uitest.Start.Add(-24 * time.Hour), LastMessageAt: &last,
		My: api.ChannelMyState{Notify: "all", UnreadCount: 4, MentionCount: 1},
	})
}

func openDMs(t *testing.T, sz uitest.Size, r ui.Route) *uitest.Harness {
	t.Helper()
	return open(t, sz, r, uitest.WithSeed(dmSeed))
}

var (
	widgetSz  = uitest.Size{W: 80, H: 30}
	tinySz    = uitest.Size{W: 40, H: 14}
	compactSz = uitest.Size{W: 120, H: 30}
	dmsRoute  = ui.Route{Kind: ui.RouteDMs}
)

func tabBarLine(h *uitest.Harness) string { return strings.SplitN(h.Screen(), "\n", 2)[0] }

func mainKey(h *uitest.Harness) string { return h.Model.Frame().Routes[layout.SlotMain].Key() }

func TestDMTabBar(t *testing.T) {
	cases := []struct {
		sz   uitest.Size
		want []string // in order
		mode layout.Mode
	}{
		{widgetSz, []string{"채팅", "DM", "피드", "알림"}, layout.ModeWidget},
		{tinySz, []string{"챗", "DM", "피", "알"}, layout.ModeTiny},
		{compactSz, []string{"홈", "DM", "피드", "알림"}, layout.ModeCompact},
	}
	for _, c := range cases {
		t.Run(c.mode.ID(), func(t *testing.T) {
			h := openDMs(t, c.sz, ui.Route{Kind: ui.RouteHome})
			if h.Model.Frame().Layout.Mode != c.mode {
				t.Fatalf("mode %s", h.Model.Frame().Layout.Mode.ID())
			}
			bar := tabBarLine(h)
			mustContainInOrder(t, bar, c.want...)
			// DM badge = 민지 2 + group 4 (blocked 준호's 5 never counts)
			if !regexp.MustCompile(`DM\s+6\b`).MatchString(bar) {
				t.Fatalf("DM badge 6 missing: %q", bar)
			}
			if c.mode != layout.ModeCompact && !regexp.MustCompile(c.want[0]+`\s+3\b`).MatchString(bar) {
				t.Fatalf("the 채팅 badge counts text channels only (#일반 3): %q", bar)
			}
			// [ ] cycle 채팅 → DM → 피드 → 알림 → 채팅
			type pos struct {
				inDM  bool
				front app.Front
			}
			for i, want := range []pos{{true, app.FrontTab}, {true, app.FrontFeed}, {true, app.FrontNotifications}, {false, app.FrontTab}} {
				h.Press("]")
				n := h.Nav()
				if n.InDM != want.inDM || n.Front != want.front {
					t.Fatalf("] #%d: in DM %v front %v", i+1, n.InDM, n.Front)
				}
				if i == 0 && mainKey(h) != dmsRoute.Key() {
					t.Fatalf("the DM tab shows %s", mainKey(h))
				}
			}
			h.Press("[")
			if n := h.Nav(); n.Front != app.FrontNotifications {
				t.Fatalf("[ from 채팅: front %v", n.Front)
			}
			h.Check()
		})
	}
}

func TestDMListRows(t *testing.T) {
	for _, sz := range []uitest.Size{widgetSz, tinySz, {W: 200, H: 60}} {
		t.Run(fmt.Sprintf("%dx%d", sz.W, sz.H), func(t *testing.T) {
			h := openDMs(t, sz, dmsRoute)
			s := h.Context().Store
			text := paneText(t, h, layout.SlotMain)
			if strings.Contains(text, "준호") {
				t.Fatalf("the blocked peer's DM must be hidden:\n%s", text)
			}
			group := "◇ " + s.ChannelLabel(groupDMID)
			gRow, mRow := -1, -1
			for i, l := range strings.Split(text, "\n") {
				switch {
				case strings.Contains(l, "◇") && gRow < 0:
					gRow = i
				case strings.Contains(l, "민지") && mRow < 0:
					mRow = i
				}
			}
			if gRow < 0 || mRow < 0 || gRow > mRow { // most recent first; the group DM is labelled ◇
				t.Fatalf("rows: group %d, 민지 %d:\n%s", gRow, mRow, text)
			}
			if sz.W >= 80 {
				for _, want := range []string{group, humanize.Relative(uitest.Start, uitest.Start.Add(-90*time.Second))} {
					if !strings.Contains(text, want) {
						t.Fatalf("%q missing:\n%s", want, text)
					}
				}
			}
			if !regexp.MustCompile(`민지.*\b2\b`).MatchString(text) || !regexp.MustCompile(`◇.*\b4\b`).MatchString(text) {
				t.Fatalf("unread badges missing:\n%s", text)
			}
			h.Check()
		})
	}
}

func TestDMListPreviewAfterOpening(t *testing.T) {
	h := openDMs(t, widgetSz, dmsRoute)
	h.Press("j", "enter") // 민지 (second row)
	n := h.Nav()
	if !n.InDM || len(n.DM.Stack) < 2 || n.DM.Stack[1].ID != fixtures.DMMinji {
		t.Fatalf("enter must open 민지's DM on the DM surface: %+v", n.DM)
	}
	h.Press("esc")
	if mainKey(h) != dmsRoute.Key() {
		t.Fatalf("esc from the DM shows %s", mainKey(h))
	}
	if text := paneText(t, h, layout.SlotMain); !strings.Contains(text, "점심 국밥") {
		t.Fatalf("the cached last message must preview:\n%s", text)
	}
}

func TestDMSurfaceKeepsPlaces(t *testing.T) {
	for _, sz := range []uitest.Size{widgetSz, tinySz, compactSz} {
		t.Run(fmt.Sprintf("%dx%d", sz.W, sz.H), func(t *testing.T) {
			h := openDMs(t, sz, ui.Route{Kind: ui.RouteChannel, ID: devIDOf(t, h0(t))})
			dev := mainKey(h)
			tabs := len(h.Nav().Tabs)
			h.Send(ui.NavigateMsg{Route: dmsRoute})
			h.Press("enter") // the group DM (first row)
			n := h.Nav()
			if !n.InDM || n.DM.Stack[len(n.DM.Stack)-1].ID != groupDMID || len(n.Tabs) != tabs {
				t.Fatalf("enter: %+v (tabs %d → %d)", n.DM, tabs, len(n.Tabs))
			}
			bar := tabBarLine(h)
			if !strings.Contains(bar, "DM") || strings.Contains(bar, "◇") {
				t.Fatalf("a DM is never a tab of its own: %q", bar)
			}
			back := func() {
				for range len(h.Nav().Tabs) + 3 {
					if !h.Nav().InDM && h.Nav().Front == app.FrontTab {
						return
					}
					h.Press("[")
				}
			}
			back()
			if mainKey(h) != dev {
				t.Fatalf("back to 채팅 shows %s, want %s", mainKey(h), dev)
			}
			for range len(h.Nav().Tabs) + 3 {
				if h.Nav().InDM && h.Nav().Front == app.FrontTab {
					break
				}
				h.Press("]")
			}
			if mainKey(h) != (ui.Route{Kind: ui.RouteChannel, ID: groupDMID}).Key() {
				t.Fatalf("back to DM shows %s, want the open group DM", mainKey(h))
			}
			// mode switches keep the DM surface
			for _, other := range []uitest.Size{{W: 200, H: 60}, {W: 120, H: 40}, widgetSz, tinySz, sz} {
				h.Resize(other.W, other.H)
				h.Check()
				if n := h.Nav(); !n.InDM || n.DM.Stack[len(n.DM.Stack)-1].ID != groupDMID {
					t.Fatalf("%v: DM surface lost: %+v", other, n.DM)
				}
				if mainKey(h) != (ui.Route{Kind: ui.RouteChannel, ID: groupDMID}).Key() {
					t.Fatalf("%v: main shows %s", other, mainKey(h))
				}
			}
		})
	}
}

// h0 is a throwaway harness to read fixture ids.
func h0(t *testing.T) *uitest.Harness { return uitest.New(t, uitest.WithSeed(dmSeed)) }

func devIDOf(t testing.TB, h *uitest.Harness) string { return chanID(t, h.Context().Store, "개발") }

func TestDMThreadStaysOnDMSurface(t *testing.T) {
	for _, sz := range []uitest.Size{widgetSz, tinySz, compactSz, {W: 200, H: 60}} {
		t.Run(fmt.Sprintf("%dx%d", sz.W, sz.H), func(t *testing.T) {
			h := openDMs(t, sz, ui.Route{Kind: ui.RouteHome})
			tabs := h.Nav().Tabs
			root := fixtures.Messages(fixtures.DMMinji)[0].ID
			thread := ui.Route{Kind: ui.RouteThread, ID: root, Arg: fixtures.DMMinji}
			h.Send(ui.NavigateMsg{Route: thread})
			n := h.Nav()
			if !n.InDM || n.DM.Thread == nil || n.DM.Thread.Key() != thread.Key() || len(n.Tabs) != len(tabs) {
				t.Fatalf("thread: %+v tabs %d", n.DM, len(n.Tabs))
			}
			h.Check()
			h.Press("c")
			n = h.Nav()
			if !n.InDM || mainKey(h) != (ui.Route{Kind: ui.RouteChannel, ID: fixtures.DMMinji}).Key() {
				t.Fatalf("c: main %s in DM %v", mainKey(h), n.InDM)
			}
			if sz.W < 200 && n.DM.Thread != nil {
				t.Fatal("c closes a thread shown in Main")
			}
			if sz.W >= 200 {
				return // the thread pane stays open; esc there is TestThread's
			}
			h.Send(ui.NavigateMsg{Route: thread})
			h.Press("esc")
			if n = h.Nav(); !n.InDM || n.DM.Thread != nil || mainKey(h) != (ui.Route{Kind: ui.RouteChannel, ID: fixtures.DMMinji}).Key() {
				t.Fatalf("esc: main %s %+v", mainKey(h), n.DM)
			}
		})
	}
}

func TestDMOpenedFromElsewhereLandsOnDMSurface(t *testing.T) {
	want := ui.Route{Kind: ui.RouteChannel, ID: fixtures.DMMinji}.Key()
	onDM := func(t *testing.T, h *uitest.Harness, how string) {
		t.Helper()
		n := h.Nav()
		if !n.InDM || n.Front != app.FrontTab || n.DM.Stack[len(n.DM.Stack)-1].Key() != want || mainKey(h) != want {
			t.Fatalf("%s: in DM %v DM %+v main %s", how, n.InDM, n.DM, mainKey(h))
		}
		for _, tb := range n.Tabs {
			if tb.Stack[0].Key() == want {
				t.Fatalf("%s: the DM is also a tab", how)
			}
		}
	}
	t.Run("palette", func(t *testing.T) {
		h := openDMs(t, widgetSz, ui.Route{Kind: ui.RouteHome})
		h.Press("ctrl+k")
		h.Type("민지")
		h.Press("enter")
		onDM(t, h, "palette")
	})
	t.Run("home", func(t *testing.T) {
		h := openDMs(t, widgetSz, ui.Route{Kind: ui.RouteHome})
		v := homeOf(t, h)
		for range 30 {
			if it, ok := v.list.Selected(); ok && it.ID == homeDMPrefix+fixtures.DMMinji {
				break
			}
			h.Press("j")
		}
		h.Press("enter")
		onDM(t, h, "home")
	})
	t.Run("notification", func(t *testing.T) {
		h := openDMs(t, tinySz, ui.Route{Kind: ui.RouteHome})
		h.Press("ctrl+n")
		r := ui.Route{Kind: ui.RouteChannel, ID: fixtures.DMMinji, Anchor: fixtures.Messages(fixtures.DMMinji)[0].ID}
		h.Send(ui.NavigateMsg{Route: r}) // what a DM notification / alert / link opens
		onDM(t, h, "notification")
	})
}

func TestDMHistoryAcrossTabs(t *testing.T) {
	h := openDMs(t, widgetSz, ui.Route{Kind: ui.RouteHome})
	dev := ui.Route{Kind: ui.RouteChannel, ID: devIDOf(t, h)}
	h.Send(ui.NavigateMsg{Route: dev})
	h.Press("]")     // the DM tab: the list
	h.Press("enter") // the group DM
	group := ui.Route{Kind: ui.RouteChannel, ID: groupDMID}.Key()
	if mainKey(h) != group {
		t.Fatalf("setup: main %s", mainKey(h))
	}
	steps := []struct {
		key  string
		main string
		inDM bool
	}{
		{"ctrl+o", dmsRoute.Key(), true},
		{"ctrl+o", dev.Key(), false},
		{"alt+right", dmsRoute.Key(), true},
		{"alt+right", group, true},
		{"ctrl+o", dmsRoute.Key(), true},
	}
	for i, s := range steps {
		h.Press(s.key)
		if mainKey(h) != s.main || h.Nav().InDM != s.inDM {
			t.Fatalf("step %d %s: main %s in DM %v, want %s %v", i+1, s.key, mainKey(h), h.Nav().InDM, s.main, s.inDM)
		}
	}
	// a DM thread, then c, then ctrl+o returns to the thread
	thread := ui.Route{Kind: ui.RouteThread, ID: fixtures.Messages(fixtures.DMMinji)[0].ID, Arg: fixtures.DMMinji}
	h.Send(ui.NavigateMsg{Route: thread})
	h.Press("c")
	h.Press("ctrl+o")
	if mainKey(h) != thread.Key() || !h.Nav().InDM {
		t.Fatalf("ctrl+o after c: main %s", mainKey(h))
	}
}

func TestDMRailAndSidebarEntry(t *testing.T) {
	t.Run("command rail", func(t *testing.T) {
		h := openDMs(t, uitest.Size{W: 200, H: 60}, ui.Route{Kind: ui.RouteChannel, ID: devIDOf(t, h0(t))})
		rail := paneText(t, h, layout.SlotRail)
		mustContainInOrder(t, rail, "홈", "DM")
		lines := strings.Split(rail, "\n")
		var homeRow, dmRow = -1, -1
		for i, l := range lines {
			switch {
			case strings.Contains(l, "홈") && homeRow < 0:
				homeRow = i
			case strings.Contains(l, "DM") && dmRow < 0:
				dmRow = i
				if !strings.Contains(l, "●") { // the group DM mentions me
					t.Fatalf("rail DM marker missing: %q", l)
				}
			}
		}
		if dmRow != homeRow+2 { // directly under 홈 (one blank separator row)
			t.Fatalf("DM row %d, 홈 row %d:\n%s", dmRow, homeRow, rail)
		}
		clickText(t, h, layout.SlotRail, "DM")
		if mainKey(h) != dmsRoute.Key() || !h.Nav().InDM {
			t.Fatalf("rail DM: main %s", mainKey(h))
		}
		if railCurrent(h.Context()) != railDM {
			t.Fatalf("rail current %q, want the DM entry", railCurrent(h.Context()))
		}
		h.Check()
	})
	t.Run("standard sidebar", func(t *testing.T) {
		h := openDMs(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: devIDOf(t, h0(t))})
		side := strings.Split(paneText(t, h, layout.SlotSidebar), "\n")
		if len(side) < 2 || !regexp.MustCompile(`DM\s+6\b`).MatchString(side[1]) {
			t.Fatalf("the DM entry (badge 6) must be right under the header:\n%s", strings.Join(side, "\n"))
		}
		clickText(t, h, layout.SlotSidebar, "DM")
		clickText(t, h, layout.SlotSidebar, "DM")
		if mainKey(h) != dmsRoute.Key() || !h.Nav().InDM {
			t.Fatalf("sidebar DM: main %s", mainKey(h))
		}
		h.Press("enter") // Main is focused: the group DM opens in Main
		if mainKey(h) != (ui.Route{Kind: ui.RouteChannel, ID: groupDMID}).Key() {
			t.Fatalf("standard enter: main %s", mainKey(h))
		}
		h.Check()
	})
}

// The window fallback previews only a window that reaches the live end: one moved back
// to an old anchor (hasMoreAfter) holds no "last message".
func TestDMPreviewOnlyFromLiveWindow(t *testing.T) {
	h := openDMs(t, widgetSz, dmsRoute)
	ctx := h.Context()
	ch := ctx.Store.Channels[fixtures.DMMinji]
	last := "01JDDMM9999999999999999999" // newest, cached nowhere
	ch.LastMessageID = &last
	old := api.Message{ID: "01JDDMM0000000000000000001", ChannelID: fixtures.DMMinji, Body: "아주 옛날 글",
		Author: *ctx.Store.User(fixtures.UserMinji)}
	w := chatOf(ctx).Window(fixtures.DMMinji)
	w.Replace([]api.Message{old}, false, true)
	if got := dmPreview(ctx, ch); got != "" {
		t.Fatalf("a window behind the live end must not preview: %q", got)
	}
	w.Replace([]api.Message{old}, false, false)
	if got := dmPreview(ctx, ch); !strings.Contains(got, "아주 옛날 글") {
		t.Fatalf("the live window's newest message must preview: %q", got)
	}
}

// A blocked member's message is never previewed on the DM list (same rule as the
// 알림함 "안 읽은 대화" convLast): a group DM keeps the blocked person's text out.
func TestDMPreviewHidesBlockedAuthor(t *testing.T) {
	h := openDMs(t, widgetSz, dmsRoute)
	ctx := h.Context()
	ch := ctx.Store.Channels[groupDMID]
	ch.LastMessageID = nil
	msg := api.Message{ID: "01JDDMM0000000000000000002", ChannelID: groupDMID, Body: "차단된 사람의 글",
		Author: *ctx.Store.User(fixtures.UserHana)}
	chatOf(ctx).Window(groupDMID).Replace([]api.Message{msg}, false, false)
	if got := dmPreview(ctx, ch); !strings.Contains(got, "차단된 사람의 글") {
		t.Fatalf("before the block the message previews: %q", got)
	}
	ctx.Store.Blocks[fixtures.UserHana] = uitest.Start
	if got := dmPreview(ctx, ch); got != "" {
		t.Fatalf("a blocked author's message must not preview: %q", got)
	}
}
