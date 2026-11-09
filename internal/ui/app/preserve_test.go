package app_test

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
	"github.com/patrache/masil/internal/ui/uitest/stub"
)

// selectedID is the selected message ID of a stub chat ("" when none).
func selectedID(c *stub.Chat) string {
	m, ok := c.List.Selected()
	if !ok {
		return ""
	}
	return m.ID
}

// listSelected is the selected row of a stub list.
func listSelected(l *stub.List) string {
	s, _ := l.List.Selected()
	return s
}

// anchorText is a short, width-safe piece of the body of the message the scroll
// anchor points at (the selection when the view is pinned to the bottom).
func anchorText(t *testing.T, c *stub.Chat) string {
	t.Helper()
	id := c.List.Anchor().ItemID
	if id == "" || c.List.Anchor().AtBottom {
		id = selectedID(c)
	}
	for _, m := range fixtures.Messages(fixtures.ChDev) {
		if m.ID == id {
			first, _, _ := strings.Cut(textwidth.Sanitize(m.Body), "\n")
			r := []rune(first)
			return string(r[:min(5, len(r))])
		}
	}
	t.Fatalf("anchor %q is not a #개발 message", id)
	return ""
}

// uniqueRouteKeys fails when one route key is projected into two slots.
func uniqueRouteKeys(t *testing.T, h *uitest.Harness) {
	t.Helper()
	seen := map[string]layout.SlotID{}
	for s, r := range h.Model.Frame().Routes {
		if prev, dup := seen[r.Key()]; dup {
			t.Fatalf("route %q in two slots: %s and %s", r.Key(), prev, s)
		}
		seen[r.Key()] = s
	}
}

// preservationScenario opens #개발, types a draft, leaves the input, scrolls up and
// selects a message (CA 10.3 core version).
func preservationScenario(t *testing.T) (*uitest.Harness, *stub.Chat) {
	t.Helper()
	h := uitest.New(t, uitest.WithRoute(ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChDev}))
	h.Resize(200, 60)
	h.Press("i")
	h.Type("배포 몇 시")
	h.Press("esc")
	h.Press("ctrl+u") // scroll up
	h.Press("k")      // select a message
	return h, devChat(t, h)
}

// assertPreserved checks (a) draft (b) selection (c) anchor visible (e) same instance.
func assertPreserved(t *testing.T, h *uitest.Harness, ch *stub.Chat, serial uint64, sel, anchor string) {
	t.Helper()
	now, ok := h.Instance(devRoute.Key()).(*stub.Chat)
	if !ok || now != ch || now.Serial != serial {
		t.Fatalf("(e) instance changed: %T serial %d, want %d", h.Instance(devRoute.Key()), now.Serial, serial)
	}
	if v := ch.Composer.Value(); v != "배포 몇 시" {
		t.Fatalf("(a) draft = %q", v)
	}
	if got := selectedID(ch); got != sel {
		t.Fatalf("(b) selection = %q, want %q", got, sel)
	}
	if !strings.Contains(h.Screen(), anchor) {
		t.Fatalf("(c) anchor text %q not on screen:\n%s", anchor, h.Screen())
	}
}

func TestModeSwitchPreservation(t *testing.T) { // CA 10.3 core version
	h, ch := preservationScenario(t)
	serial, sel, anchor := ch.Serial, selectedID(ch), anchorText(t, ch)
	if sel == "" {
		t.Fatal("no selection")
	}
	for _, sz := range []uitest.Size{{W: 200, H: 60}, {W: 120, H: 40}, {W: 120, H: 30}, {W: 80, H: 24}, {W: 50, H: 20}, {W: 200, H: 60}} {
		t.Run(sizeName(sz), func(t *testing.T) {
			h.Resize(sz.W, sz.H)
			h.Check() // (d)
			assertPreserved(t, h, ch, serial, sel, anchor)
			if f := h.Model.Focus(); f.Slot != layout.SlotMain || f.RouteKey != devRoute.Key() {
				t.Fatalf("focus = %+v, want main/#개발", f)
			}
		})
	}

	// Pin (ctrl+w 1): applied at its minimum size, fallback notice below it.
	h.Press("ctrl+w", "1")
	h.Resize(160, 40)
	if m := h.Model.Frame().Layout.Mode; m != layout.ModeCommand {
		t.Fatalf("pinned command at 160x40: mode = %v", m)
	}
	if s := statusRow(h); !strings.Contains(s, "📌") || strings.Contains(s, "창이 작아") {
		t.Fatalf("pinned command at 160x40: status = %q", s)
	}
	h.Check()
	h.Resize(150, 40)
	if m := h.Model.Frame().Layout.Mode; m != layout.ModeStandard {
		t.Fatalf("pinned command at 150x40: mode = %v, want the auto fallback (standard)", m)
	}
	if s := statusRow(h); !strings.Contains(s, "📌 지휘소 고정 · 창이 작아 표준으로 표시") {
		t.Fatalf("fallback notice missing: status = %q", s)
	}
	h.Check()
	assertPreserved(t, h, ch, serial, sel, anchor)
}

func TestCompactThreadToCommandAndBack(t *testing.T) { // CA 10.3 bullet 1
	h := uitest.New(t, uitest.WithRoute(devRoute))
	h.Resize(120, 30)
	ch := devChat(t, h)
	h.Press("k", "k", "k") // the thread root (민지 11:02)
	sel := selectedID(ch)
	h.Press("enter")
	n := h.Nav()
	thr := n.Tabs[n.Active].Thread
	if thr == nil || thr.ID != sel {
		t.Fatalf("enter: thread = %+v, want root %s", thr, sel)
	}
	if got := h.Model.Frame().Routes[layout.SlotMain].Key(); got != thr.Key() {
		t.Fatalf("compact Main = %q, want the thread", got)
	}
	tc, ok := h.Instance(thr.Key()).(*stub.Chat)
	if !ok {
		t.Fatalf("thread instance = %T", h.Instance(thr.Key()))
	}
	h.Press("i")
	h.Type("2시?")
	serial := tc.Serial

	h.Resize(200, 60)
	h.Check()
	routes := h.Model.Frame().Routes
	if routes[layout.SlotMain].Key() != devRoute.Key() || routes[layout.SlotThread].Key() != thr.Key() {
		t.Fatalf("command routes main %q thread %q", routes[layout.SlotMain].Key(), routes[layout.SlotThread].Key())
	}
	if now := h.Instance(thr.Key()).(*stub.Chat); now != tc || now.Serial != serial || now.Composer.Value() != "2시?" {
		t.Fatalf("thread instance not preserved: serial %d draft %q", now.Serial, now.Composer.Value())
	}
	if focusSlot(h) != layout.SlotThread {
		t.Fatalf("command focus = %s, want thread", focusSlot(h))
	}
	uniqueRouteKeys(t, h)

	h.Resize(120, 30)
	h.Check()
	if got := h.Model.Frame().Routes[layout.SlotMain].Key(); got != thr.Key() || h.Instance(thr.Key()).(*stub.Chat).Serial != serial {
		t.Fatalf("compact Main = %q, want the same thread instance", got)
	}
	if focusSlot(h) != layout.SlotMain {
		t.Fatalf("compact focus = %s", focusSlot(h))
	}
	h.Press("esc") // leave the thread composer
	h.Press("esc") // back: closes the thread
	if got := h.Model.Frame().Routes[layout.SlotMain].Key(); got != devRoute.Key() {
		t.Fatalf("after esc: Main = %q, want #개발", got)
	}
	if now := devChat(t, h); now != ch || selectedID(now) != sel {
		t.Fatalf("channel selection not kept: %q, want %q", selectedID(now), sel)
	}
	h.Check()
}

func TestWidgetFeedToCommand(t *testing.T) { // CA 10.3 bullet 2
	h := uitest.New(t, uitest.WithRoute(devRoute))
	h.Resize(80, 24)
	h.Press("[", "[") // 알림, then the 피드 tab (not through DM: Main keeps the 채팅 tab)
	if h.Nav().Front != app.FrontFeed {
		t.Fatalf("] at widget: front = %v", h.Nav().Front)
	}
	feed := stubList(t, h, "feed//following")
	h.Press("j", "j", "j")
	if s := listSelected(feed); s != "row 4" {
		t.Fatalf("feed selection = %q, want row 4", s)
	}
	serial := feed.Serial

	h.Resize(200, 60)
	h.Check()
	routes := h.Model.Frame().Routes
	if routes[layout.SlotFeed].Key() != "feed//following" || routes[layout.SlotMain].Key() != devRoute.Key() {
		t.Fatalf("command routes feed %q main %q", routes[layout.SlotFeed].Key(), routes[layout.SlotMain].Key())
	}
	if now := stubList(t, h, "feed//following"); now != feed || now.Serial != serial || listSelected(now) != "row 4" {
		t.Fatalf("feed instance not preserved (selection %q)", listSelected(now))
	}
	if focusSlot(h) != layout.SlotFeed {
		t.Fatalf("command focus = %s, want feed", focusSlot(h))
	}
	uniqueRouteKeys(t, h)

	h.Resize(80, 24)
	h.Check()
	if h.Nav().Front != app.FrontFeed || h.Model.Frame().Routes[layout.SlotMain].Key() != "feed//following" {
		t.Fatalf("back at widget: front %v main %q", h.Nav().Front, h.Model.Frame().Routes[layout.SlotMain].Key())
	}
	if listSelected(feed) != "row 4" || focusSlot(h) != layout.SlotMain {
		t.Fatalf("widget: selection %q focus %s", listSelected(feed), focusSlot(h))
	}
}

func TestStandardFeedSwappedAndNotificationsToCommand(t *testing.T) { // CA 10.3 bullet 3
	h := uitest.New(t, uitest.WithRoute(devRoute))
	h.Resize(120, 40)
	h.Press("f") // standard: feed swapped into Main
	if h.Nav().Front != app.FrontFeed || h.Model.Frame().Routes[layout.SlotMain].Key() != "feed//following" {
		t.Fatalf("f at standard: front %v main %q", h.Nav().Front, h.Model.Frame().Routes[layout.SlotMain].Key())
	}
	feed := stubList(t, h, "feed//following")
	h.Press("j", "j")
	h.Resize(200, 60)
	h.Check()
	uniqueRouteKeys(t, h)
	if now := stubList(t, h, "feed//following"); now != feed || listSelected(now) != "row 3" ||
		h.Model.Frame().Routes[layout.SlotFeed].Key() != "feed//following" || focusSlot(h) != layout.SlotFeed {
		t.Fatalf("feed in command: same %v selection %q focus %s", now == feed, listSelected(now), focusSlot(h))
	}
	h.Resize(120, 40)
	if h.Model.Frame().Routes[layout.SlotMain].Key() != "feed//following" || listSelected(feed) != "row 3" {
		t.Fatal("standard again: feed must be back in Main with its selection")
	}

	// Notifications in compact Main -> command SlotNotifications.
	h.Resize(120, 30)
	h.Press("ctrl+n")
	notif := stubList(t, h, app.NotifRoute.Key())
	h.Press("j")
	serial := notif.Serial
	h.Resize(200, 60)
	h.Check()
	uniqueRouteKeys(t, h)
	if now := stubList(t, h, app.NotifRoute.Key()); now != notif || now.Serial != serial || listSelected(now) != "row 2" {
		t.Fatalf("notifications in command: selection %q", listSelected(now))
	}
	if h.Model.Frame().Routes[layout.SlotNotifications].Key() != app.NotifRoute.Key() || focusSlot(h) != layout.SlotNotifications {
		t.Fatalf("command: notifications slot %q focus %s", h.Model.Frame().Routes[layout.SlotNotifications].Key(), focusSlot(h))
	}
	h.Resize(120, 30)
	if h.Model.Frame().Routes[layout.SlotMain].Key() != app.NotifRoute.Key() || listSelected(notif) != "row 2" {
		t.Fatal("compact again: notifications must be back in Main with its selection")
	}
}

func TestRandomResizeKeepsState(t *testing.T) {
	for _, seed := range []uint64{1, 2, 3} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			h, ch := preservationScenario(t)
			serial, sel, anchor := ch.Serial, selectedID(ch), anchorText(t, ch)
			rng := rand.New(rand.NewPCG(seed, seed))
			for i := range 200 {
				w, hh := 1+rng.IntN(330), 1+rng.IntN(100)
				h.Resize(w, hh)
				h.Check()
				if t.Failed() {
					t.Fatalf("step %d: %dx%d", i, w, hh)
				}
			}
			h.Resize(200, 60)
			h.Check()
			assertPreserved(t, h, ch, serial, sel, anchor)
		})
	}
}
