package feed

import (
	"fmt"
	"testing"

	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// notifSelectedID is the selected notification's ID ("" when c is not the inbox).
func notifSelectedID(c ui.Component) string {
	n, ok := c.(*notifications)
	if !ok {
		return ""
	}
	if it, ok := n.list.Selected(); ok {
		return it.id()
	}
	return ""
}

// TestFeedSurfaceAcrossModes: the feed route instance (and its selection) is the
// one widget's 피드 tab, the command-mode feed column and standard's swapped Main
// all show (CA 5.3.3 projection; no per-mode copies).
func TestFeedSurfaceAcrossModes(t *testing.T) {
	h := uitest.New(t, feedOpts(t, "following", false)...)
	h.Resize(80, 24)
	if f := h.Nav().Front; f != app.FrontFeed {
		t.Fatalf("80x24 Front = %v, want FrontFeed", f)
	}
	if k := h.Model.Frame().Routes[layout.SlotMain].Key(); k != keyFollowingRoute {
		t.Fatalf("80x24 Main = %q, want %q", k, keyFollowingRoute)
	}
	h.Check()
	inst := h.Instance(keyFollowingRoute)
	if inst == nil {
		t.Fatal("no following timeline instance")
	}
	first := SelectedID(inst)
	h.Press("j", "j", "j")
	id := SelectedID(inst)
	if id == "" || id == first {
		t.Fatalf("selection after j j j = %q (first %q), want a later card", id, first)
	}
	h.Check()

	h.Resize(200, 60)
	f := h.Model.Frame()
	if k := f.Routes[layout.SlotFeed].Key(); k != keyFollowingRoute {
		t.Fatalf("200x60 SlotFeed = %q, want %q", k, keyFollowingRoute)
	}
	if got := h.Instance(keyFollowingRoute); got != inst {
		t.Fatal("200x60: a different timeline instance")
	}
	if got := SelectedID(inst); got != id {
		t.Fatalf("200x60 selection = %q, want %q", got, id)
	}
	if s := h.Model.Focus().Slot; s != layout.SlotFeed {
		t.Fatalf("200x60 focus = %q, want %q", s, layout.SlotFeed)
	}
	h.Check()

	h.Resize(120, 40)
	if r := h.Model.Frame().Routes[layout.SlotMain]; r.Kind != ui.RouteFeed || r.Key() != keyFollowingRoute {
		t.Fatalf("120x40 Main = %+v, want the following feed (FeedSwapped)", r)
	}
	if got := h.Instance(keyFollowingRoute); got != inst || SelectedID(inst) != id {
		t.Fatalf("120x40: instance same=%v selection %q, want %q", got == inst, SelectedID(inst), id)
	}
	h.Check()

	h.Resize(80, 24)
	if f := h.Nav().Front; f != app.FrontFeed {
		t.Fatalf("back at 80x24 Front = %v, want FrontFeed", f)
	}
	if got := h.Instance(keyFollowingRoute); got != inst || SelectedID(inst) != id {
		t.Fatalf("back at 80x24: instance same=%v selection %q, want %q", got == inst, SelectedID(inst), id)
	}
	h.Check()
}

// TestNotificationsAcrossModes: compact's 알림 Front and the command-mode
// notifications pane show the same inbox instance and selection.
func TestNotificationsAcrossModes(t *testing.T) {
	h := uitest.New(t, notifOpts(t, false, nil)...)
	h.Resize(120, 30)
	if f := h.Nav().Front; f != app.FrontNotifications {
		t.Fatalf("120x30 Front = %v, want FrontNotifications", f)
	}
	inst := notifInstance(h)
	if inst == nil {
		t.Fatal("no notifications instance")
	}
	ids := NotifRowIDs(inst)
	if len(ids) < 2 {
		t.Fatalf("inbox ids = %v, want at least 2", ids)
	}
	h.Press("j")
	if got := notifSelectedID(inst); got != ids[1] {
		t.Fatalf("120x30 selection = %q, want the 2nd %q", got, ids[1])
	}
	h.Check()

	h.Resize(200, 60)
	if r := h.Model.Frame().Routes[layout.SlotNotifications]; r.Key() != app.NotifRoute.Key() {
		t.Fatalf("200x60 SlotNotifications = %+v", r)
	}
	if got := notifInstance(h); got != inst {
		t.Fatal("200x60: a different notifications instance")
	}
	if got := notifSelectedID(inst); got != ids[1] {
		t.Fatalf("200x60 selection = %q, want %q", got, ids[1])
	}
	if s := h.Model.Focus().Slot; s != layout.SlotNotifications {
		t.Fatalf("200x60 focus = %q, want %q", s, layout.SlotNotifications)
	}
	h.Check()

	h.Resize(120, 30)
	if f := h.Nav().Front; f != app.FrontNotifications {
		t.Fatalf("back at 120x30 Front = %v, want FrontNotifications", f)
	}
	if r := h.Model.Frame().Routes[layout.SlotMain]; r.Key() != app.NotifRoute.Key() {
		t.Fatalf("back at 120x30 Main = %+v, want the inbox", r)
	}
	if got := notifInstance(h); got != inst || notifSelectedID(inst) != ids[1] {
		t.Fatalf("back at 120x30: instance same=%v selection %q, want %q", got == inst, notifSelectedID(inst), ids[1])
	}
	h.Check()
}

// TestAllSnsRoutesHostileSweep resizes ONE harness per sns route (Hostile seed)
// through every size, so mode changes on a live instance never break a frame.
func TestAllSnsRoutesHostileSweep(t *testing.T) {
	hostileID := fixtures.HostileTimeline()[0].Post.ID
	composeSrv := snsAPI(t, true)
	composeHandlers(composeSrv)
	hostileSeed := uitest.WithSeed(seedStoreFn(true))
	routes := []struct {
		name string
		opts func() []uitest.Option
	}{
		{"feed_following", func() []uitest.Option { return feedOpts(t, "following", true) }},
		{"feed_discover", func() []uitest.Option { return feedOpts(t, "discover", true) }},
		{"post", func() []uitest.Option { return postOpts(t, hostileID, true) }},
		{"profile", func() []uitest.Option { return profileOpts(t, "@"+hostileProfileHandle, true, nil) }},
		{"hashtag", func() []uitest.Option { return hashtagOptsWith(snsAPI(t, true), "첫눈", true) }},
		{"trends", func() []uitest.Option { return trendsOptsWith(snsAPI(t, true), true) }},
		{"notifications", func() []uitest.Option { return notifOpts(t, true, nil) }},
		{"stories", func() []uitest.Option {
			return storyOptsWith(snsAPI(t, true), hostileStoryGroups()[0].User.ID, true, hostileStoryGroups)
		}},
		{"compose", func() []uitest.Option {
			return []uitest.Option{uitest.WithAPI(composeSrv), hostileSeed, uitest.WithRoute(composeRoute(hostileID, "reply"))}
		}},
		{"compose_story", func() []uitest.Option {
			return []uitest.Option{uitest.WithAPI(composeSrv), hostileSeed, uitest.WithRoute(ui.Route{Kind: ui.RouteCompose, Arg: "story"})}
		}},
	}
	for _, r := range routes {
		t.Run(r.name, func(t *testing.T) {
			h := uitest.New(t, r.opts()...)
			for _, s := range uitest.AllSizes() {
				t.Run(fmt.Sprintf("%dx%d", s.W, s.H), func(st *testing.T) {
					h.T = st
					defer func() { h.T = t }()
					h.Resize(s.W, s.H)
					h.Check()
				})
			}
		})
	}
}

// seedStoreFn is the WithSeed func of seedStore (fixtures.Seed, + Hostile).
func seedStoreFn(hostile bool) func(*store.Store) {
	return func(s *store.Store) {
		fixtures.Seed(s)
		if hostile {
			fixtures.Hostile(s)
		}
	}
}
