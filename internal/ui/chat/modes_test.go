package chat

import (
	"math/rand/v2"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// ---- cross-mode tests (Task 23) ----

func TestChatModeGoldens(t *testing.T) {
	// one golden per layout mode at the FIRST ModeSizes entry, three states each: channel, channel+thread, home
	for _, mode := range []layout.Mode{layout.ModeCommand, layout.ModeStandard, layout.ModeCompact, layout.ModeWidget, layout.ModeTiny, layout.ModeTooSmall} {
		sz := uitest.ModeSizes[mode][0]
		t.Run(mode.ID(), func(t *testing.T) {
			h := devChannel(t, sz)
			if h.Context().Mode != mode {
				t.Fatalf("size %v resolved to %v", sz, h.Context().Mode)
			}
			h.Golden("channel")
			if mode != layout.ModeTooSmall {
				selectText(t, h, "오늘 배포")
				h.Press("t")
				h.Golden("thread")
				h.Press("esc")
			}
			h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteHome}})
			h.Golden("home")
		})
	}
}

func TestChatModeSwitchPreserves(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	h.Press("i")
	h.Type("배포 몇 시")
	h.Press("esc")
	h.Press("k", "k", "k", "k", "k")
	sel := selectedID(t, h)
	inst := channelOf(t, h)
	if sel == "" {
		t.Fatal("k selects a message")
	}
	for _, sz := range []uitest.Size{{W: 200, H: 60}, {W: 120, H: 40}, {W: 120, H: 30}, {W: 80, H: 24}, {W: 50, H: 20}, {W: 200, H: 60}} {
		h.Resize(sz.W, sz.H)
		if channelOf(t, h) != inst || inst.composer.c.Value() != "배포 몇 시" || selectedID(t, h) != sel {
			t.Fatalf("%v lost state: same=%v draft=%q sel=%q want %q", sz, channelOf(t, h) == inst, inst.composer.c.Value(), selectedID(t, h), sel)
		}
		// visibility via the list itself (row ids per line), plus a short ellipsis-free text prefix
		if !rowVisible(t, h, sel) || !strings.Contains(h.Screen(), plainOf(t, h, sel)) {
			t.Fatalf("%v selected message not visible:\n%s", sz, h.Screen())
		}
		h.Check()
	}
	h.Resize(120, 40)
	h.Press("ctrl+w", "1") // pin command: too small → fallback notice
	mustContain(t, h.Screen(), "창이 작아")
}

func TestChatModeSwitchMarksRead(t *testing.T) { // CR-2 outcome (Task 8 two-phase read): no key press needed after a mode switch
	h := devChannel(t, uitest.Size{W: 120, H: 30}) // compact: Main only
	if h.Context().Mode != layout.ModeCompact {
		t.Fatalf("120x30 resolved to %v", h.Context().Mode)
	}
	ch := devID(t, h)
	put := func() []apitest.Recorded { return reqs(h.API, "PUT", "/v1/channels/"+ch+"/read") }
	h.Press("G")
	h.Advance(time.Second)
	n0 := len(put())
	root := fixtureMsg(t, h, "오늘 배포 몇 시에 해요?").ID
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteThread, ID: root, Arg: ch}}) // compact: thread replaces Main, channel instance cached but hidden
	if h.Context().Store.Viewing(ch) {
		t.Fatal("channel is hidden behind the thread in compact")
	}
	m := mkMsg(ch, 950, userBy(t, h.Context().Store, "minji"), "가려진 동안 온 글", h.Context().Now())
	h.Event("message.created", realtime.MessageEvent{Message: m})
	h.Advance(2 * time.Second)
	if len(put()) != n0 {
		t.Fatal("hidden channel must not be marked read")
	}
	h.Resize(200, 60) // command: channel back in Main, thread in SlotThread; ModeChangedMsg arms, SetViewing runs at the end of this Update
	if !h.Context().Store.Viewing(ch) {
		t.Fatal("core sets Viewing at the end of the resize Update")
	}
	h.Advance(time.Second) // no key press
	r := put()
	if len(r) != n0+1 || jsonField(r[len(r)-1].Body, "message_id") != m.ID {
		t.Fatalf("want exactly one read of %s, got %d", m.ID, len(r)-n0)
	}
	if h.Context().Store.Channels[ch].My.UnreadCount != 0 {
		t.Fatal("local badge cleared")
	}
}

func TestChatRandomResize(t *testing.T) {
	for _, seed := range []int64{1, 2, 3} {
		rng := rand.New(rand.NewPCG(uint64(seed), 0))
		h := devChannel(t, uitest.Size{W: 200, H: 60})
		h.Press("i")
		h.Type("초안")
		h.Press("esc")
		selectText(t, h, "오늘 배포")
		h.Press("t")
		h.Press("i")
		h.Type("답글 초안")
		h.Press("esc")
		ch, th := channelOf(t, h), threadOfH(t, h)
		if ch.composer.c.Value() != "초안" || th.composer.c.Value() != "답글 초안" {
			t.Fatalf("seed %d: setup drafts %q / %q", seed, ch.composer.c.Value(), th.composer.c.Value())
		}
		for i := 0; i < 200; i++ {
			h.Resize(1+rng.IntN(330), 1+rng.IntN(100))
			h.Check()
		}
		h.Resize(200, 60)
		// channel draft "초안", thread draft "답글 초안", same instances
		if got := channelOf(t, h); got != ch || got.composer.c.Value() != "초안" {
			t.Fatalf("seed %d: channel instance or draft lost (same=%v draft=%q)", seed, got == ch, got.composer.c.Value())
		}
		if got := threadOfH(t, h); got != th || got.composer.c.Value() != "답글 초안" {
			t.Fatalf("seed %d: thread instance or draft lost (same=%v draft=%q)", seed, got == th, got.composer.c.Value())
		}
		h.Check()
	}
}

func TestChatHostileSweep(t *testing.T) {
	routes := hostileRoutes(t) // channel, thread(9,999 replies), search hits, invite preview, space settings, friends, home
	for name, r := range routes {
		t.Run(name, func(t *testing.T) {
			forEachSize(t, uitest.AllSizes(), func(t *testing.T, sz uitest.Size) *uitest.Harness { return hostileRoute(t, sz, r) },
				func(t *testing.T, h *uitest.Harness) { h.Check() })
		})
	}
}

func TestChatSmallWidthSweep(t *testing.T) {
	if testing.Short() {
		t.Skip("full sweep")
	}
	h := devChannel(t, uitest.Size{W: 200, H: 60})
	for w := 1; w <= 260; w++ {
		for _, ht := range []int{6, 15, 16, 23, 24, 39, 40, 49, 50, 60} {
			h.Resize(w, ht)
			h.Check()
		}
	}
}

// ---- hostile route builders (Task 23) ----

// hostileBuilder opens one chat route on the fixture seed plus fixtures.Hostile.
type hostileBuilder func(t *testing.T, sz uitest.Size) *uitest.Harness

// hostileRoute builds the harness of r at sz.
func hostileRoute(t *testing.T, sz uitest.Size, r hostileBuilder) *uitest.Harness {
	t.Helper()
	return r(t, sz)
}

// hostileRoutes lists every chat route seeded with hostile data: channel,
// thread (9,999 replies), search hits, invite preview, space settings,
// friends, home.
func hostileRoutes(t *testing.T) map[string]hostileBuilder {
	t.Helper()
	return map[string]hostileBuilder{
		"channel":        hostileOpen,
		"thread":         hostileThread,
		"search":         hostileSearch,
		"invite":         hostileInvitePreview,
		"space_settings": hostileSettings,
		"friends":        hostileFriends,
		"home":           hostileHome,
	}
}

// hostileSeed is the bootstrapped fixture seed plus fixtures.Hostile.
func hostileSeed(s *store.Store) {
	bootstrappedSeed(s)
	fixtures.Hostile(s)
}

// hostileThread opens the hostile thread root with 9,999 replies.
func hostileThread(t *testing.T, sz uitest.Size) *uitest.Harness {
	t.Helper()
	s := seeded()
	fixtures.Hostile(s)
	ch := hostileChannel(t, s)
	var root api.Message
	for _, m := range fixtures.HostileMessages(ch) {
		if m.Thread != nil && m.Thread.ReplyCount == 9999 {
			root = m
		}
	}
	if root.ID == "" {
		t.Fatal("no hostile root")
	}
	return open(t, sz, ui.Route{Kind: ui.RouteThread, ID: root.ID, Arg: ch}, uitest.WithSeed(hostileSeed))
}

// hostileSearch opens RouteSearch answered with hostile hits (bad highlight
// ranges, an unknown DM channel).
func hostileSearch(t *testing.T, sz uitest.Size) *uitest.Harness {
	t.Helper()
	s := seeded()
	fixtures.Hostile(s)
	ch := s.Channels[hostileChannel(t, s)]
	ref := api.ChannelRef{ID: ch.ID, Kind: ch.Kind, Name: ch.Name, SpaceID: ch.SpaceID, SpaceName: ptrTo(s.Spaces[fixtures.SpaceHostile].Name)}
	var items []api.MessageSearchHit
	for i, m := range fixtures.HostileMessages(ch.ID) {
		hs := []api.SearchHighlight{{Start: i, Length: 3}, {Start: -1, Length: 4}, {Start: 1 << 20, Length: 1}, {Start: 2, Length: 1 << 20}}
		items = append(items, api.MessageSearchHit{Message: m, Channel: ref, Highlights: hs})
	}
	dm := api.ChannelRef{ID: "01JB0000000000000000NODM01", Kind: api.ChannelDM}
	items = append(items, api.MessageSearchHit{Message: fixtures.HostileMessages(ch.ID)[0], Channel: dm})
	srv := newAPI(t)
	srv.JSON("GET "+searchPath, http.StatusOK, map[string]any{"items": items, "next_cursor": "c2"}) // same shape: replaces the default search for this harness
	return openWith(t, sz, ui.Route{Kind: ui.RouteSearch, ID: ch.ID, Arg: "x"}, srv, uitest.WithSeed(hostileSeed))
}

// hostileInvitePreview opens the invite-link preview of the hostile space by a
// hostile inviter.
func hostileInvitePreview(t *testing.T, sz uitest.Size) *uitest.Harness {
	t.Helper()
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
	srv := newAPI(t)
	exp := uitest.Start.Add(400 * 24 * time.Hour)
	srv.JSON("GET /v1/invites/"+testInviteCode, http.StatusOK, map[string]any{"invite": api.InvitePreview{
		Code: testInviteCode, Space: s.Spaces[fixtures.SpaceHostile].SpacePreview, CreatedBy: inviter, ExpiresAt: &exp,
	}})
	return openWith(t, sz, ui.Route{Kind: ui.RouteInvite, ID: testInviteCode}, srv, uitest.WithSeed(hostileSeed))
}
