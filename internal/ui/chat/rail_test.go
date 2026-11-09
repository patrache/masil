package chat

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
)

func TestRailGolden(t *testing.T) {
	forEachSize(t, uitest.AllSizes(), devChannel, func(t *testing.T, h *uitest.Harness) { paneGolden(t, h, layout.SlotRail, "rail") })
}

func TestRailContent(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	p, _ := h.Context().Layout.Pane(layout.SlotRail)
	col := columnText(h.Screen(), p.Rect.Inner())
	// 노바 개발방 has the icon emoji 🚀, which wins over the initial (CA 9: "방 이니셜(아이콘 이모지 있으면 그것)").
	mustContainInOrder(t, col, "홈", "🚀", "+")

	// Without an icon the space shows the first grapheme of its name.
	noIcon := func(s *store.Store) {
		bootstrappedSeed(s)
		s.Spaces[spaceByName(t, s, "노바 개발방")].IconEmoji = nil
	}
	h = open(t, uitest.Size{W: 200, H: 60}, ui.Route{Kind: ui.RouteChannel, ID: chanID(t, seeded(), "개발")}, uitest.WithSeed(noIcon))
	p, _ = h.Context().Layout.Pane(layout.SlotRail)
	mustContainInOrder(t, columnText(h.Screen(), p.Rect.Inner()), "홈", "노", "+")
}

func TestRailNavigates(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	clickText(t, h, layout.SlotRail, "홈")
	if top(h).Kind != ui.RouteHome {
		t.Fatalf("got %+v", top(h))
	}
	if railCurrent(h.Context()) != railHome {
		t.Fatalf("home click: current %q", railCurrent(h.Context()))
	}
	clickText(t, h, layout.SlotRail, "🚀")
	sid := spaceByName(t, h.Context().Store, "노바 개발방")
	if top(h).Kind != ui.RouteChannel || top(h).ID != h.Context().Store.Spaces[sid].DefaultChannelID {
		t.Fatalf("space click opens default channel, got %+v", top(h))
	}
	if chatOf(h.Context()).SelectedSpace() != sid || railCurrent(h.Context()) != railSpacePrefix+sid {
		t.Fatalf("space click selects the space: selected %q current %q", chatOf(h.Context()).SelectedSpace(), railCurrent(h.Context()))
	}
	clickText(t, h, layout.SlotRail, "+")
	if len(h.Model.Frame().Overlays) != 1 || !strings.Contains(h.Screen(), "초대 코드로 참여") || !strings.Contains(h.Screen(), "방 만들기") {
		t.Fatal("+ opens space add menu")
	}
}

// railRow is the rail's inner row holding glyph.
func railRow(t *testing.T, h *uitest.Harness, glyph string) string {
	t.Helper()
	p, ok := h.Model.Frame().Layout.Pane(layout.SlotRail)
	if !ok {
		t.Fatal("no rail")
	}
	for _, r := range cropRect(h.Screen(), p.Rect.Inner()) {
		if strings.Contains(r, glyph) {
			return r
		}
	}
	t.Fatalf("%q not in the rail", glyph)
	return ""
}

func TestRailUnreadMarker(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	s := h.Context().Store
	general := s.Channels[chanID(t, s, "일반")]
	if general.My.UnreadCount != 3 || general.My.MentionCount != 0 {
		t.Fatalf("fixture changed: %+v", general.My)
	}
	if r := railRow(t, h, "🚀"); !strings.Contains(r, "•") || strings.Contains(r, "●") {
		t.Fatalf("unread space row %q", r)
	}
	if r := railRow(t, h, "DM"); strings.ContainsAny(r, "•●") {
		t.Fatalf("DM with read DMs %q", r)
	}

	general.My.MentionCount = 1
	if r := railRow(t, h, "🚀"); !strings.Contains(r, "●") || strings.Contains(r, "•") {
		t.Fatalf("mention space row %q", r)
	}

	// A muted channel adds no unread, its mentions still count (Store.TotalUnread rule).
	general.My.MentionCount = 0
	general.My.Muted = true
	if r := railRow(t, h, "🚀"); strings.ContainsAny(r, "•●") {
		t.Fatalf("muted unread space row %q", r)
	}
	general.My.Muted = false

	s.Channels[dmWith(t, s, "minji")].My.UnreadCount = 0
	s.Channels[dmWith(t, s, "junho")].My.UnreadCount = 2
	if r := railRow(t, h, "DM"); !strings.Contains(r, "•") {
		t.Fatalf("DM with an unread DM %q", r)
	}
	if r := railRow(t, h, "홈"); strings.ContainsAny(r, "•●") {
		t.Fatalf("DM unread counts on the DM entry, not 홈: %q", r)
	}
	h.Event("block.updated", map[string]any{"user_id": userBy(t, s, "junho").ID, "blocked": true})
	if !s.IsBlocked(userBy(t, s, "junho").ID) {
		t.Fatal("block.updated not applied")
	}
	if r := railRow(t, h, "DM"); strings.ContainsAny(r, "•●") {
		t.Fatalf("a blocked peer's DM still counts: %q", r)
	}
}

func TestRailKeys(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	before := top(h)
	p, _ := h.Model.Frame().Layout.Pane(layout.SlotRail)
	in := p.Rect.Inner()
	h.Click(in.X, in.Y+in.H-1) // a blank row: focuses the rail, runs nothing
	if top(h).Key() != before.Key() || len(h.Model.Frame().Overlays) != 0 {
		t.Fatalf("blank-row click ran something: %+v", top(h))
	}
	h.Press("home", "j", "enter") // DM: the DM list
	if top(h).Kind != ui.RouteDMs || !h.Nav().InDM {
		t.Fatalf("home j enter: %+v", top(h))
	}
	h.Click(in.X, in.Y+in.H-1)
	h.Press("j", "enter")
	sid := spaceByName(t, h.Context().Store, "노바 개발방")
	if top(h).ID != h.Context().Store.Spaces[sid].DefaultChannelID || chatOf(h.Context()).SelectedSpace() != sid {
		t.Fatalf("j enter: %+v", top(h))
	}
	h.Click(in.X, in.Y+in.H-1) // opening a channel moves the focus to it (core); refocus the rail
	h.Press("k", "k", "enter")
	if top(h).Kind != ui.RouteHome || chatOf(h.Context()).SelectedSpace() != "" {
		t.Fatalf("k enter: %+v", top(h))
	}
	h.Click(in.X, in.Y+in.H-1)
	h.Press("G", "enter")
	if len(h.Model.Frame().Overlays) != 1 || !strings.Contains(h.Screen(), "초대 코드로 참여") {
		t.Fatal("G enter opens the space add menu")
	}
}

func TestRailCurrent(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	ctx := h.Context()
	s := ctx.Store
	sid := spaceByName(t, s, "노바 개발방")
	c := chatOf(ctx)

	c.SetSelectedSpace("")
	s.SetViewing()
	if got := railCurrent(ctx); got != railHome {
		t.Fatalf("nothing viewed, nothing selected: %q", got)
	}
	c.SetSelectedSpace(sid)
	if got := railCurrent(ctx); got != railSpacePrefix+sid {
		t.Fatalf("selected space: %q", got)
	}
	s.SetViewing(dmWith(t, s, "minji"))
	if got := railCurrent(ctx); got != railDM {
		t.Fatalf("viewing a DM: %q", got)
	}
	c.SetSelectedSpace("")
	s.SetViewing(chanID(t, s, "개발"))
	if got := railCurrent(ctx); got != railSpacePrefix+sid {
		t.Fatalf("viewing a space channel: %q", got)
	}
}

func TestSpaceAddMenuMorphs(t *testing.T) {
	t.Cleanup(ui.SnapshotRegistryForTest())
	// Task 18 registers the real overlays; until then a stand-in proves the morph.
	for _, id := range []string{"chat.space_create", "chat.space_join"} {
		if _, ok := ui.Registered().Overlays[id]; !ok {
			text := id
			ui.RegisterOverlay(id, func(*ui.Context, string) ui.Overlay {
				return newMenu(text, []menuItem{{Label: "stand-in " + text}})
			})
		}
	}
	for _, tc := range []struct {
		keys []string
		id   string
	}{{[]string{"enter"}, "chat.space_create"}, {[]string{"j", "enter"}, "chat.space_join"}} {
		h := devChannel(t, uitest.Size{W: 120, H: 40})
		h.Send(ui.OpenOverlayMsg{Overlay: newSpaceAddMenu()})
		h.Press(tc.keys...)
		if n := len(h.Model.Frame().Overlays); n != 1 {
			t.Fatalf("%s: %d overlays (the menu must morph in place, never close-then-open)", tc.id, n)
		}
		if strings.Contains(h.Screen(), "초대 코드로 참여") {
			t.Fatalf("%s: the menu is still shown", tc.id)
		}
	}
}

func TestRailHostileIntegrity(t *testing.T) {
	forEachSize(t, uitest.AllSizes(), hostileOpen, func(t *testing.T, h *uitest.Harness) { h.Check() })
}

func TestRailViewExactSize(t *testing.T) {
	h := hostileOpen(t, uitest.Size{W: 200, H: 60})
	ctx := h.Context()
	r := newRail(ctx)
	for _, sz := range []ui.Size{{W: 4, H: 57}, {W: 4, H: 3}, {W: 1, H: 1}, {W: 9, H: 7}, {W: 0, H: 0}} {
		r.Resize(ctx, sz)
		for _, focused := range []bool{false, true} {
			out := r.View(ctx, focused)
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
	if _, ok := r.(ui.KeyHandler); !ok {
		t.Fatal("rail must be a KeyHandler")
	}
	if _, ok := r.(ui.MouseHandler); !ok {
		t.Fatal("rail must be a MouseHandler")
	}
	if _, ok := r.(ui.Titled); ok {
		t.Fatal("the rail has no title")
	}
}

func TestRailScrollsToCurrent(t *testing.T) {
	many := func(s *store.Store) {
		bootstrappedSeed(s)
		for i := range 30 {
			r := 'A' + rune(i)
			if i >= 26 {
				r = 'a' + rune(i-26)
			}
			id := fmt.Sprintf("01JB0000000000000000000Z%02d", i)
			sd := &api.SpaceDetail{Space: api.Space{SpacePreview: api.SpacePreview{ID: id, Name: string(r) + " space"}}}
			s.Spaces[id] = sd
			s.SpaceOrder = append(s.SpaceOrder, id)
		}
	}
	h := open(t, uitest.Size{W: 200, H: 50}, ui.Route{Kind: ui.RouteChannel, ID: chanID(t, seeded(), "개발")}, uitest.WithSeed(many))
	p, _ := h.Model.Frame().Layout.Pane(layout.SlotRail)
	col := columnText(h.Screen(), p.Rect.Inner())
	if !strings.Contains(col, "홈") || strings.Contains(col, " d") {
		t.Fatalf("starts at the top:\n%s", col)
	}
	chatOf(h.Context()).SetSelectedSpace("01JB0000000000000000000Z29")
	h.Context().Store.SetViewing()
	col = columnText(h.Screen(), p.Rect.Inner())
	if !strings.Contains(col, " d") {
		t.Fatalf("the current space must be scrolled into view:\n%s", col)
	}
	h.Check()
}
